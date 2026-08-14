package credentialcli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hopecommon/sii-link/client/atrust/auth/siicas"
)

type fakeCredentialStore struct {
	credentials siicas.Credentials
	expiresAt   *time.Time
	revoked     bool
}

func (f *fakeCredentialStore) Put(credentials siicas.Credentials, _ time.Duration) (siicas.CredentialStatus, error) {
	f.credentials = credentials
	f.revoked = false
	return siicas.CredentialStatus{Available: true, ExpiresAt: f.expiresAt}, nil
}

func (f *fakeCredentialStore) Status(context.Context) (siicas.CredentialStatus, error) {
	return siicas.CredentialStatus{Available: !f.revoked && f.credentials.Password != "", ExpiresAt: f.expiresAt}, nil
}

func (f *fakeCredentialStore) Revoke() (bool, error) {
	if f.revoked || f.credentials.Password == "" {
		return false, nil
	}
	f.revoked = true
	return true, nil
}

func TestPutReadsOnlyStdinAndDoesNotEchoSecrets(t *testing.T) {
	store := &fakeCredentialStore{}
	stdin := strings.NewReader(`{"username":"student-id","password":"top-secret"}`)
	var stdout, stderr bytes.Buffer

	code := run([]string{"put"}, stdin, &stdout, &stderr, store)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if store.credentials.Username != "student-id" || store.credentials.Password != "top-secret" {
		t.Fatalf("stored credentials = %#v", store.credentials)
	}
	if strings.Contains(stdout.String(), "student-id") || strings.Contains(stdout.String(), "top-secret") {
		t.Fatalf("stdout exposed credentials: %q", stdout.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if output["stored"] != true || output["expires_at"] != nil {
		t.Fatalf("output = %#v, want stored without expiration", output)
	}
}

func TestStatusAndRevokeExposeNoCredentialValues(t *testing.T) {
	store := &fakeCredentialStore{credentials: siicas.Credentials{Username: "student-id", Password: "top-secret"}}
	for _, args := range [][]string{{"status"}, {"revoke"}, {"status"}} {
		var stdout, stderr bytes.Buffer
		code := run(args, strings.NewReader(""), &stdout, &stderr, store)
		if code != 0 {
			t.Fatalf("%v exit code = %d, stderr = %q", args, code, stderr.String())
		}
		if strings.Contains(stdout.String(), "student-id") || strings.Contains(stdout.String(), "top-secret") {
			t.Fatalf("%v stdout exposed credentials: %q", args, stdout.String())
		}
	}
}
