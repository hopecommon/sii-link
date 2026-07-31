package auth

import (
	"net/http"
	"testing"
)

func TestRequireVerifiedTLS(t *testing.T) {
	session := NewSession("vpn.sii.edu.cn")
	transport := session.client.Transport.(*http.Transport)
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("legacy session default unexpectedly verifies TLS")
	}

	session.RequireVerifiedTLS()
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("session still skips TLS verification")
	}
}
