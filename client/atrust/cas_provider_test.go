package atrust

import (
	"context"
	"testing"

	"github.com/hopecommon/sii-link/client/atrust/auth"
)

func TestSetCASTicketProvider(t *testing.T) {
	provider := auth.CASTicketProviderFunc(func(context.Context, auth.CASTicketRequest) (string, error) {
		return "ST-test", nil
	})
	client := NewClient("", "", "", "")
	client.SetCASTicketProvider(provider)
	if client.casTicketProvider == nil {
		t.Fatal("CAS ticket provider was not configured")
	}
}

func TestCASTicketProviderPreservesSavedSession(t *testing.T) {
	client := NewClient("", "", "", "")
	saved := []auth.Cookie{{Name: "sid", Value: "saved-session"}}
	if got := client.loginCookies(saved); len(got) != 1 {
		t.Fatalf("login cookies without provider = %d, want 1", len(got))
	}

	client.SetCASTicketProvider(auth.CASTicketProviderFunc(func(context.Context, auth.CASTicketRequest) (string, error) {
		return "ST-test", nil
	}))
	if got := client.loginCookies(saved); len(got) != 1 || got[0].Value != "saved-session" {
		t.Fatalf("login cookies with provider = %#v, want saved session", got)
	}
}

func TestSetVerifyServerTLS(t *testing.T) {
	client := NewClient("", "", "", "")
	client.SetVerifyServerTLS(true)
	if !client.verifyServerTLS {
		t.Fatal("server TLS verification was not enabled")
	}
}
