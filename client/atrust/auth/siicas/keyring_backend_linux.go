//go:build linux

package siicas

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	credentialKeyDescription = "sii-link:cas:v1"
	credentialKeyPermissions = 0x3f3f0000
	maxCredentialPayload     = 16 * 1024
)

type systemKeyringBackend struct{}

func KernelKeyringSupported() bool {
	return true
}

func newSystemKeyringBackend() keyringBackend {
	return systemKeyringBackend{}
}

func (systemKeyringBackend) Put(payload []byte) (int, error) {
	ringID, err := persistentUserKeyring()
	if err != nil {
		return 0, err
	}
	keyID, err := searchCredentialKey(ringID)
	if err == nil {
		if _, err := unix.KeyctlBuffer(unix.KEYCTL_UPDATE, keyID, payload, 0); err != nil {
			return 0, fmt.Errorf("update credential key: %w", err)
		}
	} else if errors.Is(normalizeKeyringError(err), ErrCredentialUnavailable) {
		keyID, err = unix.AddKey("user", credentialKeyDescription, payload, ringID)
		if err != nil {
			return 0, err
		}
	} else {
		return 0, err
	}
	if err := unix.KeyctlSetperm(keyID, credentialKeyPermissions); err != nil {
		_, _ = unix.KeyctlInt(unix.KEYCTL_REVOKE, keyID, 0, 0, 0)
		return 0, fmt.Errorf("restrict key permissions: %w", err)
	}
	if err := linkCredentialKey(keyID, ringID); err != nil {
		_, _ = unix.KeyctlInt(unix.KEYCTL_REVOKE, keyID, 0, 0, 0)
		return 0, err
	}
	return keyID, nil
}

func (systemKeyringBackend) Read() ([]byte, error) {
	keyID, err := findCredentialKey()
	if err != nil {
		return nil, err
	}

	size, err := unix.KeyctlBuffer(unix.KEYCTL_READ, keyID, nil, 0)
	if err != nil {
		return nil, normalizeKeyringError(err)
	}
	if size <= 0 || size > maxCredentialPayload {
		return nil, fmt.Errorf("credential payload size %d is invalid", size)
	}
	payload := make([]byte, size)
	n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, keyID, payload, 0)
	if err != nil {
		clear(payload)
		return nil, normalizeKeyringError(err)
	}
	if n > len(payload) {
		clear(payload)
		return nil, fmt.Errorf("credential payload changed while reading")
	}
	return payload[:n], nil
}

func (systemKeyringBackend) SetTimeout(keyID int, seconds uint) error {
	if seconds > uint(^uint(0)>>1) {
		return fmt.Errorf("credential timeout is too large")
	}
	_, err := unix.KeyctlInt(unix.KEYCTL_SET_TIMEOUT, keyID, int(seconds), 0, 0)
	return err
}

func (systemKeyringBackend) Revoke() (bool, error) {
	keyID, err := findCredentialKey()
	if errors.Is(err, ErrCredentialUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = unix.KeyctlInt(unix.KEYCTL_REVOKE, keyID, 0, 0, 0)
	if err != nil {
		return false, normalizeKeyringError(err)
	}
	return true, nil
}

func persistentUserKeyring() (int, error) {
	ringID, err := unix.KeyctlInt(
		unix.KEYCTL_GET_PERSISTENT,
		os.Getuid(),
		unix.KEY_SPEC_USER_KEYRING,
		0,
		0,
	)
	if err != nil {
		return 0, fmt.Errorf("open persistent user keyring: %w", err)
	}
	return ringID, nil
}

func findCredentialKey() (int, error) {
	persistentRingID, err := persistentUserKeyring()
	if err != nil {
		return 0, err
	}
	return searchCredentialKey(persistentRingID)
}

func searchCredentialKey(persistentRingID int) (int, error) {
	for _, ringID := range []int{persistentRingID, unix.KEY_SPEC_USER_KEYRING} {
		keyID, err := unix.KeyctlSearch(ringID, "user", credentialKeyDescription, 0)
		if err == nil {
			return keyID, nil
		}
		normalized := normalizeKeyringError(err)
		if !errors.Is(normalized, ErrCredentialUnavailable) {
			return 0, normalized
		}
	}
	return 0, ErrCredentialUnavailable
}

func linkCredentialKey(keyID, persistentRingID int) error {
	for _, ringID := range []int{persistentRingID, unix.KEY_SPEC_USER_KEYRING} {
		if _, err := unix.KeyctlInt(unix.KEYCTL_LINK, keyID, ringID, 0, 0); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("link credential key: %w", err)
		}
	}
	return nil
}

func normalizeKeyringError(err error) error {
	if errors.Is(err, unix.ENOKEY) || errors.Is(err, unix.EKEYEXPIRED) || errors.Is(err, unix.EKEYREVOKED) {
		return ErrCredentialUnavailable
	}
	return err
}
