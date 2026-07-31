//go:build !darwin

package siicas

import (
	"context"
	"fmt"
)

func lookupKeychainSecret(_ context.Context, _ keychainRequest) (string, error) {
	return "", fmt.Errorf("macOS Keychain credential source is not supported on this platform")
}
