package siicas

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	defaultUsernameService = "atrust.username"
	defaultPasswordService = "atrust.password"
)

var errKeychainItemNotFound = errors.New("keychain item not found")

type Credentials struct {
	Username string
	Password string
}

type CredentialSource interface {
	Credentials(context.Context) (Credentials, error)
}

type CredentialSourceFunc func(context.Context) (Credentials, error)

func (f CredentialSourceFunc) Credentials(ctx context.Context) (Credentials, error) {
	return f(ctx)
}

type FileCredentialSource struct {
	UsernameFile string
	PasswordFile string
}

func (s FileCredentialSource) Credentials(_ context.Context) (Credentials, error) {
	username, err := readPrivateSecretFile("username", s.UsernameFile)
	if err != nil {
		return Credentials{}, err
	}
	password, err := readPrivateSecretFile("password", s.PasswordFile)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Username: username, Password: password}, nil
}

func readPrivateSecretFile(label, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("SII CAS %s file is required", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat SII CAS %s file: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("SII CAS %s file is not a regular file", label)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("SII CAS %s file permissions %04o expose credentials; require 0600 or stricter", label, info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read SII CAS %s file: %w", label, err)
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", fmt.Errorf("SII CAS %s file is empty", label)
	}
	return value, nil
}

type keychainRequest struct {
	Account string
	Service string
}

type KeychainCredentialSource struct {
	Account         string
	UsernameService string
	PasswordService string

	lookup func(context.Context, keychainRequest) (string, error)
}

func (s KeychainCredentialSource) Credentials(ctx context.Context) (Credentials, error) {
	usernameService := s.UsernameService
	if usernameService == "" {
		usernameService = defaultUsernameService
	}
	passwordService := s.PasswordService
	if passwordService == "" {
		passwordService = defaultPasswordService
	}
	lookup := s.lookup
	if lookup == nil {
		lookup = lookupKeychainSecret
	}

	username, err := lookup(ctx, keychainRequest{Account: s.Account, Service: usernameService})
	if err != nil {
		return Credentials{}, fmt.Errorf("load SII CAS username from Keychain: %w", err)
	}
	password, err := lookup(ctx, keychainRequest{Account: s.Account, Service: passwordService})
	if err != nil {
		return Credentials{}, fmt.Errorf("load SII CAS password from Keychain: %w", err)
	}
	if username == "" || password == "" {
		return Credentials{}, fmt.Errorf("SII CAS Keychain credentials must not be empty")
	}
	return Credentials{Username: username, Password: password}, nil
}
