package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testController(t *testing.T) *controller {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	contents := "protocol='atrust'\nserver_address='vpn.sii.edu.cn'\nserver_port=443\nsii_unattended_cas=true\nclient_data_file='" + filepath.Join(dir, "cache.json") + "'\nsocks_bind='127.0.0.1:1080'\nhttp_bind='127.0.0.1:8888'\n"
	if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	s := State{Role: "server", Config: config, Epoch: 1, Budget: true, Self: "this-host"}
	if err := saveState(dir, s); err != nil {
		t.Fatal(err)
	}
	return &controller{dir: dir, state: s, pid: 123, wake: make(chan struct{}, 1), peer: func(context.Context, string, ...string) (Status, error) { return Status{}, errPeerOffline }}
}

func TestRenewalBudgetSurvivesWorkerAndSupervisorRestart(t *testing.T) {
	c := testController(t)
	r := request{Action: "login", PID: 123, Epoch: 1}
	if err := c.dispatch(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	saved, err := readState(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Budget || !saved.Pending || saved.Attempt != 1 {
		t.Fatalf("uncommitted permission reservation: %+v", saved)
	}
	restarted := &controller{dir: c.dir, state: saved, pid: 456}
	if err := restarted.dispatch(context.Background(), request{Action: "login", PID: 456, Epoch: 1}); err == nil {
		t.Fatal("restart reset fresh-login permission")
	}
}

func TestClientAndUnownedWorkerCannotCreateSession(t *testing.T) {
	for _, role := range []string{"client", "off", "server"} {
		t.Run(role, func(t *testing.T) {
			c := testController(t)
			c.state.Role = role
			if err := c.dispatch(context.Background(), request{Action: "login", PID: 999, Epoch: 1}); err == nil {
				t.Fatal("unowned native process admitted")
			}
			if role != "server" && c.dispatch(context.Background(), request{Action: "login", PID: 123, Epoch: 1}) == nil {
				t.Fatal("non-Server admitted")
			}
			if c.state.Pending {
				t.Fatal("denial consumed new authentication")
			}
		})
	}
}

func TestPreflightSeesPeerAcquiringSessionAndYields(t *testing.T) {
	c := testController(t)
	c.state.Peer = "peer-host"
	c.peer = func(context.Context, string, ...string) (Status, error) {
		return Status{Role: "server", LoginPending: true}, nil
	}
	if c.dispatch(context.Background(), request{Action: "login", PID: 123, Epoch: 1}) == nil {
		t.Fatal("peer acquisition did not stop fresh authentication")
	}
	if c.state.Role != "client" || c.state.Budget || c.state.Pending {
		t.Fatalf("old grant survived: %+v", c.state)
	}
}

func TestRoleChangeDuringPreflightRevokesLatePermission(t *testing.T) {
	c := testController(t)
	c.state.Peer = "peer-host"
	entered, release := make(chan struct{}), make(chan struct{})
	c.peer = func(context.Context, string, ...string) (Status, error) {
		close(entered)
		<-release
		return Status{}, errPeerOffline
	}
	result := make(chan error, 1)
	go func() { result <- c.dispatch(context.Background(), request{Action: "login", PID: 123, Epoch: 1}) }()
	<-entered
	if err := c.dispatch(context.Background(), request{Action: "off"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if <-result == nil {
		t.Fatal("late preflight restored revoked permission")
	}
	if c.state.Role != "off" || c.state.Pending {
		t.Fatalf("late state mutation: %+v", c.state)
	}
}

func TestHandoverRejectsInFlightLoginAndStaleReceipts(t *testing.T) {
	c := testController(t)
	if err := c.dispatch(context.Background(), request{Action: "login", PID: 123, Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	if c.dispatch(context.Background(), request{Action: "yield", Peer: "new-host"}) == nil {
		t.Fatal("handover acknowledged live authentication")
	}
	if c.dispatch(context.Background(), request{Action: "session-ready", PID: 123, Epoch: 1, Attempt: 99}) == nil {
		t.Fatal("stale receipt accepted")
	}
	if !c.state.Pending {
		t.Fatal("stale receipt cleared pending login")
	}
	if err := c.dispatch(context.Background(), request{Action: "login-result", PID: 123, Epoch: 1, Attempt: 1, Success: true}); err != nil {
		t.Fatal(err)
	}
	if !c.state.Pending {
		t.Fatal("login reply cleared pending before cookie persistence")
	}
	if err := c.dispatch(context.Background(), request{Action: "session-ready", PID: 123, Epoch: 1, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if c.state.Pending || c.state.Budget {
		t.Fatalf("incorrect ready state: %+v", c.state)
	}
	if err := c.dispatch(context.Background(), request{Action: "yield", Peer: "new-host"}); err != nil {
		t.Fatal(err)
	}
	if c.state.Role != "client" {
		t.Fatal("handover failed")
	}
}

func TestConcurrentServerSelectionsCannotCrossAcknowledge(t *testing.T) {
	c := testController(t)
	c.state.Peer = "peer-host"
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	c.peer = func(context.Context, string, ...string) (Status, error) {
		once.Do(func() { close(entered) })
		<-release
		return Status{Role: "client"}, nil
	}
	result := make(chan error, 1)
	go func() { result <- c.dispatch(context.Background(), request{Action: "server"}) }()
	<-entered
	if c.dispatch(context.Background(), request{Action: "yield", Peer: "other-host"}) == nil {
		t.Fatal("simultaneous selection acknowledged")
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if c.state.Role != "server" {
		t.Fatal("selection lost")
	}
}

func TestPeerProtocolFailurePreservesPreviousRole(t *testing.T) {
	c := testController(t)
	c.state.Role = "client"
	c.state.Peer = "peer-host"
	c.state.Budget = false
	c.peer = func(context.Context, string, ...string) (Status, error) {
		return Status{}, errors.New("unsupported peer")
	}
	if c.dispatch(context.Background(), request{Action: "server"}) == nil {
		t.Fatal("unsupported peer accepted")
	}
	if c.state.Role != "client" || c.state.Budget {
		t.Fatal("failed handover acquired permission")
	}
}

func TestStateAndSocketPathRemainPrivate(t *testing.T) {
	c := testController(t)
	for _, path := range []string{c.dir, filepath.Join(c.dir, "gateway.json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("public gateway state %s", path)
		}
	}
}

func TestInstallationPreservesAuthorityAndSpentBudget(t *testing.T) {
	c := testController(t)
	c.state.Budget = false
	c.state.Pending = true
	if err := saveState(c.dir, c.state); err != nil {
		t.Fatal(err)
	}
	if err := initializeState(c.dir, "different-config"); err != nil {
		t.Fatal(err)
	}
	saved, err := readState(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Role != "server" || saved.Budget || !saved.Pending || saved.Config != c.state.Config {
		t.Fatalf("installer rewrote permission: %+v", saved)
	}
}

func TestPreSubmissionFailureCanRetryWithoutGrantingNewAuthority(t *testing.T) {
	c := testController(t)
	if err := c.dispatch(context.Background(), request{Action: "login", PID: 123, Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.dispatch(context.Background(), request{Action: "login-result", PID: 123, Epoch: 1, Attempt: 1, RetrySafe: true}); err != nil {
		t.Fatal(err)
	}
	if c.state.Pending || !c.state.Budget || c.state.Pause != "" {
		t.Fatalf("pre-submission transport failure blocked recovery: %+v", c.state)
	}
	if err := c.dispatch(context.Background(), request{Action: "login", PID: 123, Epoch: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestLogoutDecisionPreservesRecoveryAndRevokesReplacement(t *testing.T) {
	for _, reason := range []string{"relogin", "trustDevice", "timeout", "timeoutOffline", "SessionExpiration", "admin", "new-policy"} {
		t.Run(reason, func(t *testing.T) {
			c := testController(t)
			c.state.Peer = "peer-host"
			cancelled := false
			c.cancel = func() { cancelled = true }
			c.observeLogout(1, 0, reason)
			saved, err := readState(c.dir)
			if err != nil {
				t.Fatal(err)
			}
			if !cancelled {
				t.Fatal("invalid provider continued after logout")
			}
			switch reason {
			case "relogin", "trustDevice":
				if saved.Role != "client" || saved.Budget || saved.Epoch != 2 {
					t.Fatalf("replacement kept authority: %+v", saved)
				}
			case "timeout", "timeoutOffline":
				if saved.Role != "server" || !saved.Budget || saved.Pause != "" {
					t.Fatalf("expiry blocked ordinary recovery: %+v", saved)
				}
			default:
				if saved.Role != "server" || saved.Pause == "" {
					t.Fatalf("unverified policy permitted fresh login: %+v", saved)
				}
			}
		})
	}
}

func TestStaleLogoutCannotChangeNewAuthority(t *testing.T) {
	for _, boundary := range []string{"epoch", "attempt", "pending", "switching"} {
		t.Run(boundary, func(t *testing.T) {
			c := testController(t)
			switch boundary {
			case "epoch":
				c.state.Epoch++
			case "attempt":
				c.state.Attempt++
			case "pending":
				c.state.Pending = true
			case "switching":
				c.switching = true
			}
			before := c.state
			c.observeLogout(1, 0, "relogin")
			if c.state != before {
				t.Fatalf("stale logout changed authority: %+v", c.state)
			}
		})
	}
}
