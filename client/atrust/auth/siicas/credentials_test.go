package siicas

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileCredentialSourceReadsPrivateFiles(t *testing.T) {
	directory := t.TempDir()
	usernameFile := filepath.Join(directory, "username")
	passwordFile := filepath.Join(directory, "password")
	if err := os.WriteFile(usernameFile, []byte("student-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordFile, []byte("secret\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	credentials, err := (FileCredentialSource{
		UsernameFile: usernameFile,
		PasswordFile: passwordFile,
	}).Credentials(context.Background())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if credentials.Username != "student-id" || credentials.Password != "secret" {
		t.Fatalf("credentials = %#v, want username and password without trailing newlines", credentials)
	}
}

func TestFileCredentialSourceRejectsBroadPermissions(t *testing.T) {
	directory := t.TempDir()
	usernameFile := filepath.Join(directory, "username")
	passwordFile := filepath.Join(directory, "password")
	if err := os.WriteFile(usernameFile, []byte("student-id"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := (FileCredentialSource{
		UsernameFile: usernameFile,
		PasswordFile: passwordFile,
	}).Credentials(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("credential error = %v, want permissions error", err)
	}
}

func TestKeychainCredentialSourceUsesExplicitAccount(t *testing.T) {
	var requests []keychainRequest
	source := KeychainCredentialSource{
		Account: "local-user",
		lookup: func(_ context.Context, request keychainRequest) (string, error) {
			requests = append(requests, request)
			if request.Service == "atrust.username" {
				return "student-id", nil
			}
			return "secret", nil
		},
	}

	credentials, err := source.Credentials(context.Background())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if credentials.Username != "student-id" || credentials.Password != "secret" {
		t.Fatalf("credentials = %#v, want Keychain values", credentials)
	}
	if len(requests) != 2 {
		t.Fatalf("lookup requests = %d, want one lookup for each credential", len(requests))
	}
	for _, request := range requests {
		if request.Account != "local-user" {
			t.Fatalf("lookup account = %q, want explicit account", request.Account)
		}
	}
}

func TestKeychainCredentialSourceDoesNotFallbackFromExplicitAccount(t *testing.T) {
	var requests []keychainRequest
	source := KeychainCredentialSource{
		Account: "missing-user",
		lookup: func(_ context.Context, request keychainRequest) (string, error) {
			requests = append(requests, request)
			return "", errKeychainItemNotFound
		},
	}

	_, err := source.Credentials(context.Background())
	if err == nil || !strings.Contains(err.Error(), "username") {
		t.Fatalf("credential error = %v, want explicit-account lookup error", err)
	}
	if len(requests) != 1 || requests[0].Account != "missing-user" {
		t.Fatalf("lookup requests = %#v, want no account-less fallback", requests)
	}
}
