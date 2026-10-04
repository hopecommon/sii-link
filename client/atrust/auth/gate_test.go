package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testLoginGate struct {
	before, after int
	deny, result  error
}

func (g *testLoginGate) BeforeLogin() error   { g.before++; return g.deny }
func (g *testLoginGate) AfterLogin(err error) { g.after++; g.result = err }

type fixtureLogin struct{ calls int }

var fixtureAuthFailure = errors.New("fixture authentication failed")

func (*fixtureLogin) AuthType() string                 { return "auth/fixture" }
func (*fixtureLogin) LoginDomain() string              { return "fixture" }
func (m *fixtureLogin) login(*Session, AuthInfo) error { m.calls++; return fixtureAuthFailure }

func gateSession(t *testing.T, online int) *Session {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/passport/v1/public/authConfig":
			fmt.Fprintf(w, `{"data":{"isLogin":%d,"security":{"csrfToken":"fixture"},"authServerInfoList":[{"authType":"auth/fixture","loginDomain":"fixture"}]}}`, online)
		case "/passport/v1/user/onlineInfo":
			fmt.Fprint(w, `{"code":0,"data":{"username":"fixture"}}`)
		case "/controller/v1/public/events":
			if r.URL.Query().Get("fromId") != "fixture-old" || r.Header.Get("x-csrf-token") != "fixture" {
				t.Error("event cursor/session headers missing")
			}
			fmt.Fprint(w, `{"code":0,"data":{"id":"fixture-next","events":[{"event":"logout","data":{"type":"relogin","ignoredPrivateField":"fixture"}}]}}`)
		default:
			t.Error("unexpected authentication request")
			http.Error(w, "unexpected", 500)
		}
	}))
	t.Cleanup(server.Close)
	return NewSession(strings.TrimPrefix(server.URL, "https://"))
}

func TestGateDenialStopsActualAuthentication(t *testing.T) {
	s := gateSession(t, 0)
	method := &fixtureLogin{}
	gate := &testLoginGate{deny: errors.New("permission revoked")}
	_, err := s.Login(method, LoginOptions{Gate: gate})
	if !errors.Is(err, gate.deny) || method.calls != 0 || gate.before != 1 || gate.after != 0 {
		t.Fatalf("authentication escaped gate: method=%d gate=%+v err=%v", method.calls, gate, err)
	}
}

func TestCachedSessionConsumesNoFreshPermission(t *testing.T) {
	s := gateSession(t, 1)
	method := &fixtureLogin{}
	gate := &testLoginGate{deny: errors.New("permission revoked")}
	_, err := s.Login(method, LoginOptions{Gate: gate})
	if err != nil || method.calls != 0 || gate.before != 0 || gate.after != 0 {
		t.Fatalf("cached reuse crossed fresh-login gate: %+v %v", gate, err)
	}
}

func TestGateGetsActualAuthenticationFailure(t *testing.T) {
	s := gateSession(t, 0)
	method := &fixtureLogin{}
	gate := &testLoginGate{}
	_, err := s.Login(method, LoginOptions{Gate: gate})
	if !errors.Is(err, fixtureAuthFailure) || gate.before != 1 || gate.after != 1 || !errors.Is(gate.result, fixtureAuthFailure) {
		t.Fatalf("outcome not accounted for: %+v %v", gate, err)
	}
}

func TestEventsUsesOldSessionAndPreservesCursor(t *testing.T) {
	s := gateSession(t, 0)
	_, err := s.Login(nil, LoginOptions{})
	if !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	events, cursor, err := s.Events(context.Background(), "fixture-old")
	if err != nil || cursor != "fixture-next" || len(events) != 1 || events[0].Name != "logout" || events[0].Data.Type != "relogin" {
		t.Fatalf("incorrect event decode: %+v %q %v", events, cursor, err)
	}
}

func TestEventsCancellationDoesNotEraseCursor(t *testing.T) {
	s := gateSession(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cursor, err := s.Events(ctx, "fixture-old")
	if !errors.Is(err, context.Canceled) || cursor != "fixture-old" {
		t.Fatalf("cancellation lost: %q %v", cursor, err)
	}
}

func TestUnknownSessionStatusCannotTriggerLogin(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"http-denied", 403, `{"data":{"isLogin":0}}`},
		{"missing-status", 200, `{"code":0,"data":{}}`},
		{"business-error", 200, `{"code":403,"data":{"isLogin":0}}`},
		{"unexpected-status", 200, `{"code":0,"data":{"isLogin":2}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			session := NewSession(strings.TrimPrefix(server.URL, "https://"))
			method, gate := &fixtureLogin{}, &testLoginGate{}
			_, err := session.Login(method, LoginOptions{Gate: gate})
			if err == nil || errors.Is(err, ErrSessionInvalid) || method.calls != 0 || gate.before != 0 {
				t.Fatalf("unknown status became an invalid session or new login: %v %+v", err, gate)
			}
		})
	}
}
