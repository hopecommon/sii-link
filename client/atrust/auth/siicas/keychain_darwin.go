//go:build darwin

package siicas

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func lookupKeychainSecret(ctx context.Context, request keychainRequest) (string, error) {
	args := []string{"find-generic-password"}
	if request.Account != "" {
		args = append(args, "-a", request.Account)
	}
	args = append(args, "-s", request.Service, "-w")
	output, err := exec.CommandContext(ctx, "/usr/bin/security", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 44 {
			return "", errKeychainItemNotFound
		}
		return "", fmt.Errorf("query macOS Keychain service %q: %w", request.Service, err)
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}
