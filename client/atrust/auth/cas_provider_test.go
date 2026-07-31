package auth

import (
	"context"
	"errors"
	"testing"
)

var errProviderStopped = errors.New("provider stopped")

type recordingCASTicketProvider struct {
	request CASTicketRequest
}

func (p *recordingCASTicketProvider) Ticket(_ context.Context, request CASTicketRequest) (string, error) {
	p.request = request
	return "", errProviderStopped
}

func TestCASLoginResolvesProviderURLs(t *testing.T) {
	provider := &recordingCASTicketProvider{}
	session := &Session{
		baseHost: "vpn.sii.edu.cn",
		baseURL:  "https://vpn.sii.edu.cn",
	}
	method := CASLogin{
		Domain:         "cas.sii.edu.cn",
		TicketProvider: provider,
	}

	err := method.login(session, AuthInfo{
		LoginURL: "/passport/v1/public/casLogin?sfDomain=cas.sii.edu.cn",
	})
	if !errors.Is(err, errProviderStopped) {
		t.Fatalf("CAS login error = %v, want provider error", err)
	}
	if got, want := provider.request.LoginURL, "https://vpn.sii.edu.cn/passport/v1/public/casLogin?sfDomain=cas.sii.edu.cn"; got != want {
		t.Fatalf("provider login URL = %q, want %q", got, want)
	}
	if got, want := provider.request.CallbackURL, "https://vpn.sii.edu.cn/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn"; got != want {
		t.Fatalf("provider callback URL = %q, want %q", got, want)
	}
}

func TestCASLoginRejectsEmptyProviderTicket(t *testing.T) {
	provider := CASTicketProviderFunc(func(context.Context, CASTicketRequest) (string, error) {
		return "", nil
	})
	session := &Session{
		baseHost: "vpn.sii.edu.cn",
		baseURL:  "https://vpn.sii.edu.cn",
	}
	method := CASLogin{
		Domain:         "cas.sii.edu.cn",
		TicketProvider: provider,
	}

	err := method.login(session, AuthInfo{LoginURL: "/passport/v1/public/casLogin?sfDomain=cas.sii.edu.cn"})
	if err == nil || err.Error() != "CAS ticket provider returned an empty ticket" {
		t.Fatalf("CAS login error = %v, want empty-ticket error", err)
	}
}
