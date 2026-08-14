package siicas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

const credentialEnvelopeVersion = 1

var ErrCredentialUnavailable = errors.New("SII CAS credentials are not available in the Linux kernel keyring")

type CredentialStatus struct {
	Available bool       `json:"available"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type credentialEnvelope struct {
	Version   int    `json:"version"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

type keyringBackend interface {
	Put([]byte) (int, error)
	Read() ([]byte, error)
	SetTimeout(int, uint) error
	Revoke() (bool, error)
}

type KeyringCredentialStore struct {
	backend keyringBackend
	now     func() time.Time
}

func NewKeyringCredentialStore() *KeyringCredentialStore {
	return newKeyringCredentialStore(newSystemKeyringBackend())
}

func newKeyringCredentialStore(backend keyringBackend) *KeyringCredentialStore {
	return &KeyringCredentialStore{backend: backend, now: time.Now}
}

func (s *KeyringCredentialStore) Put(credentials Credentials, ttl time.Duration) (CredentialStatus, error) {
	if credentials.Username == "" || credentials.Password == "" {
		return CredentialStatus{}, fmt.Errorf("SII CAS username and password are required")
	}
	if ttl < 0 {
		return CredentialStatus{}, fmt.Errorf("credential TTL must not be negative")
	}

	envelope := credentialEnvelope{
		Version:  credentialEnvelopeVersion,
		Username: credentials.Username,
		Password: credentials.Password,
	}
	var expiresAt *time.Time
	var timeoutSeconds uint
	if ttl > 0 {
		seconds := ttl / time.Second
		if ttl%time.Second != 0 {
			seconds++
		}
		if seconds > math.MaxInt32 {
			return CredentialStatus{}, fmt.Errorf("credential TTL is too large")
		}
		timestamp := s.now().Add(ttl).UTC()
		envelope.ExpiresAt = timestamp.Unix()
		expiresAt = &timestamp
		timeoutSeconds = uint(seconds)
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return CredentialStatus{}, fmt.Errorf("encode SII CAS credentials: %w", err)
	}
	defer clear(payload)

	keyID, err := s.backend.Put(payload)
	if err != nil {
		return CredentialStatus{}, fmt.Errorf("store SII CAS credentials in Linux kernel keyring: %w", err)
	}
	if err := s.backend.SetTimeout(keyID, timeoutSeconds); err != nil {
		_, _ = s.backend.Revoke()
		return CredentialStatus{}, fmt.Errorf("set Linux kernel keyring credential timeout: %w", err)
	}
	return CredentialStatus{Available: true, ExpiresAt: expiresAt}, nil
}

func (s *KeyringCredentialStore) Credentials(_ context.Context) (Credentials, error) {
	envelope, err := s.load()
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Username: envelope.Username, Password: envelope.Password}, nil
}

func (s *KeyringCredentialStore) Status(_ context.Context) (CredentialStatus, error) {
	envelope, err := s.load()
	if errors.Is(err, ErrCredentialUnavailable) {
		return CredentialStatus{Available: false}, nil
	}
	if err != nil {
		return CredentialStatus{}, err
	}
	return CredentialStatus{Available: true, ExpiresAt: envelope.expiration()}, nil
}

func (s *KeyringCredentialStore) Revoke() (bool, error) {
	revoked, err := s.backend.Revoke()
	if err != nil {
		return false, fmt.Errorf("revoke SII CAS credentials from Linux kernel keyring: %w", err)
	}
	return revoked, nil
}

func (s *KeyringCredentialStore) load() (credentialEnvelope, error) {
	payload, err := s.backend.Read()
	if err != nil {
		if errors.Is(err, ErrCredentialUnavailable) {
			return credentialEnvelope{}, ErrCredentialUnavailable
		}
		return credentialEnvelope{}, fmt.Errorf("read SII CAS credentials from Linux kernel keyring: %w", err)
	}
	defer clear(payload)

	var envelope credentialEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return credentialEnvelope{}, fmt.Errorf("decode SII CAS credentials from Linux kernel keyring: %w", err)
	}
	if envelope.Version != credentialEnvelopeVersion {
		return credentialEnvelope{}, fmt.Errorf("unsupported SII CAS credential version %d", envelope.Version)
	}
	if envelope.Username == "" || envelope.Password == "" {
		return credentialEnvelope{}, fmt.Errorf("SII CAS credentials in Linux kernel keyring are incomplete")
	}
	if envelope.ExpiresAt > 0 && !s.now().Before(time.Unix(envelope.ExpiresAt, 0)) {
		return credentialEnvelope{}, ErrCredentialUnavailable
	}
	return envelope, nil
}

func (e credentialEnvelope) expiration() *time.Time {
	if e.ExpiresAt == 0 {
		return nil
	}
	expiresAt := time.Unix(e.ExpiresAt, 0).UTC()
	return &expiresAt
}
