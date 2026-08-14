package siicas

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeKeyringBackend struct {
	payload        []byte
	timeoutSeconds uint
	revoked        bool
}

func (f *fakeKeyringBackend) Put(payload []byte) (int, error) {
	f.payload = append([]byte(nil), payload...)
	f.revoked = false
	return 42, nil
}

func (f *fakeKeyringBackend) Read() ([]byte, error) {
	if len(f.payload) == 0 || f.revoked {
		return nil, ErrCredentialUnavailable
	}
	return append([]byte(nil), f.payload...), nil
}

func (f *fakeKeyringBackend) SetTimeout(_ int, seconds uint) error {
	f.timeoutSeconds = seconds
	return nil
}

func (f *fakeKeyringBackend) Revoke() (bool, error) {
	if len(f.payload) == 0 || f.revoked {
		return false, nil
	}
	f.revoked = true
	return true, nil
}

func TestKeyringCredentialStoreKeepsCredentialsUntilRevokedByDefault(t *testing.T) {
	backend := &fakeKeyringBackend{}
	store := newKeyringCredentialStore(backend)

	status, err := store.Put(Credentials{Username: "student-id", Password: "secret"}, 0)
	if err != nil {
		t.Fatalf("put credentials: %v", err)
	}
	if status.ExpiresAt != nil {
		t.Fatalf("expiration = %v, want no expiration", status.ExpiresAt)
	}
	if backend.timeoutSeconds != 0 {
		t.Fatalf("kernel timeout = %d, want 0", backend.timeoutSeconds)
	}

	credentials, err := store.Credentials(context.Background())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if credentials.Username != "student-id" || credentials.Password != "secret" {
		t.Fatalf("credentials = %#v, want stored values", credentials)
	}

	revoked, err := store.Revoke()
	if err != nil || !revoked {
		t.Fatalf("revoke = %v, %v; want true, nil", revoked, err)
	}
	_, err = store.Credentials(context.Background())
	if !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("credential error = %v, want unavailable", err)
	}
}

func TestKeyringCredentialStoreAppliesOnlyExplicitTTL(t *testing.T) {
	backend := &fakeKeyringBackend{}
	store := newKeyringCredentialStore(backend)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	status, err := store.Put(Credentials{Username: "student-id", Password: "secret"}, 90*time.Minute)
	if err != nil {
		t.Fatalf("put credentials: %v", err)
	}
	if backend.timeoutSeconds != 5400 {
		t.Fatalf("kernel timeout = %d, want 5400", backend.timeoutSeconds)
	}
	wantExpiry := now.Add(90 * time.Minute)
	if status.ExpiresAt == nil || !status.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expiration = %v, want %v", status.ExpiresAt, wantExpiry)
	}
}

func TestKeyringCredentialStoreRejectsEmptyCredentials(t *testing.T) {
	store := newKeyringCredentialStore(&fakeKeyringBackend{})

	_, err := store.Put(Credentials{Username: "student-id"}, 0)
	if err == nil {
		t.Fatal("put credentials succeeded, want empty-password error")
	}
}
