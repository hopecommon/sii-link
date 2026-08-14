//go:build !linux

package siicas

import "fmt"

type unsupportedKeyringBackend struct{}

func KernelKeyringSupported() bool {
	return false
}

func newSystemKeyringBackend() keyringBackend {
	return unsupportedKeyringBackend{}
}

func (unsupportedKeyringBackend) Put([]byte) (int, error) {
	return 0, unsupportedKeyringError()
}

func (unsupportedKeyringBackend) Read() ([]byte, error) {
	return nil, unsupportedKeyringError()
}

func (unsupportedKeyringBackend) SetTimeout(int, uint) error {
	return unsupportedKeyringError()
}

func (unsupportedKeyringBackend) Revoke() (bool, error) {
	return false, unsupportedKeyringError()
}

func unsupportedKeyringError() error {
	return fmt.Errorf("Linux kernel keyring credentials are only supported on Linux")
}
