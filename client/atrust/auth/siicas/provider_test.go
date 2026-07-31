package siicas

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hopecommon/sii-link/client/atrust/auth"
)

const (
	testRSAExponent = "010001"
	testRSAModulus  = "008aed7e057fe8f14c73550b0e6467b023616ddc8fa91846d2613cdb7f7621e3cada4cd5d812d627af6b87727ade4e26d26208b7326815941492b2204c3167ab2d53df1e3a2c9153bdb7c8c2e968df97a5e7e01cc410f92c4c2c2fba529b3ee988ebc1fca99ff5119e036d732c368acf8beba01aa2fdafa45b21e4de4928d0d403"
	testCiphertext  = "470f39c43cec7711f07ecea18bef15a16695acb8e891ac2d481808b2dc1e0f5cc21892c4e09f30061c28bea30f5214e9e167791723ebf5e17dff24625ee06f8f6f36a30702e2219cf6ae54817c298a3506a1258b950d4d4eba99ce5e71eb9d0f2cebc15fce05cd85c9341278556ef22080b1215134e6962af467c6105a20ce1c"
)

func TestProviderGetsSIICASTicket(t *testing.T) {
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/v1/public/casLogin", func(w http.ResponseWriter, r *http.Request) {
		service := server.URL + "/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn"
		http.Redirect(w, r, "/cas/login?service="+url.QueryEscape(service), http.StatusFound)
	})
	mux.HandleFunc("/cas/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			service := r.URL.Query().Get("service")
			fmt.Fprintf(w, `<!doctype html>
<html><body>
<form id="fm1" action="/cas/login?service=%s" method="post">
  <input name="execution" value="execution-token">
</form>
<script src="/cas/login.js"></script>
</body></html>`, url.QueryEscape(service))
			return
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse CAS form: %v", err)
		}
		wantForm := map[string]string{
			"username":  "student-id",
			"password":  testCiphertext,
			"execution": "execution-token",
			"encrypted": "true",
			"_eventId":  "submit",
			"loginType": "1",
		}
		for key, want := range wantForm {
			if got := r.Form.Get(key); got != want {
				t.Errorf("CAS form %s = %q, want %q", key, got, want)
			}
		}
		http.Redirect(w, r, server.URL+"/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn&ticket=ST-test-123", http.StatusFound)
	})
	mux.HandleFunc("/cas/login.js", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `var key = RSAUtils.getKeyPair("%s", '', "%s");`, testRSAExponent, testRSAModulus)
	})
	server = httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(
		CredentialSourceFunc(func(context.Context) (Credentials, error) {
			return Credentials{Username: "student-id", Password: "p@ssw0rd"}, nil
		}),
		Options{
			CASHost:    serverURL.Hostname(),
			CASPort:    serverURL.Port(),
			HTTPClient: server.Client(),
		},
	)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	ticket, err := provider.Ticket(context.Background(), auth.CASTicketRequest{
		LoginURL:    server.URL + "/passport/v1/public/casLogin?sfDomain=cas.sii.edu.cn",
		CallbackURL: server.URL + "/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn",
	})
	if err != nil {
		t.Fatalf("get ticket: %v", err)
	}
	if ticket != "ST-test-123" {
		t.Fatalf("ticket = %q, want %q", ticket, "ST-test-123")
	}
}

func TestProviderRejectsInsecureLoginURL(t *testing.T) {
	provider, err := NewProvider(
		CredentialSourceFunc(func(context.Context) (Credentials, error) {
			return Credentials{Username: "student-id", Password: "password"}, nil
		}),
		Options{CASHost: "cas.sii.edu.cn"},
	)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	_, err = provider.Ticket(context.Background(), auth.CASTicketRequest{
		LoginURL:    "http://vpn.sii.edu.cn/passport/v1/public/casLogin?sfDomain=cas.sii.edu.cn",
		CallbackURL: "https://vpn.sii.edu.cn/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("ticket error = %v, want HTTPS validation error", err)
	}
}

func TestNewProviderRejectsProxyWithoutHost(t *testing.T) {
	_, err := NewProvider(
		CredentialSourceFunc(func(context.Context) (Credentials, error) {
			return Credentials{Username: "student-id", Password: "password"}, nil
		}),
		Options{CASHost: "cas.sii.edu.cn", ProxyURL: "http://"},
	)
	if err == nil || !strings.Contains(err.Error(), "include a host") {
		t.Fatalf("provider error = %v, want proxy-host validation error", err)
	}
}

func TestProviderRejectsUnexpectedTicketRedirect(t *testing.T) {
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/passport/v1/public/casLogin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cas/login", http.StatusFound)
	})
	mux.HandleFunc("/cas/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<form id="fm1" action="/cas/login" method="post"><input name="execution" value="execution-token"></form><script src="/cas/login.js"></script>`)
			return
		}
		http.Redirect(w, r, "https://attacker.example/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn&ticket=ST-stolen", http.StatusFound)
	})
	mux.HandleFunc("/cas/login.js", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `var key = RSAUtils.getKeyPair("%s", '', "%s");`, testRSAExponent, testRSAModulus)
	})
	server = httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(
		CredentialSourceFunc(func(context.Context) (Credentials, error) {
			return Credentials{Username: "student-id", Password: "p@ssw0rd"}, nil
		}),
		Options{CASHost: serverURL.Hostname(), CASPort: serverURL.Port(), HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Ticket(context.Background(), auth.CASTicketRequest{
		LoginURL:    server.URL + "/passport/v1/public/casLogin",
		CallbackURL: server.URL + "/passport/v1/auth/cas?sfDomain=cas.sii.edu.cn",
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected CAS redirect host") {
		t.Fatalf("ticket error = %v, want redirect-host error", err)
	}
}
