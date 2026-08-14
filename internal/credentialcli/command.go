package credentialcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/hopecommon/sii-link/client/atrust/auth/siicas"
)

const maxCredentialInput = 16 * 1024
const credentialUnavailableExit = 3

type credentialStore interface {
	Put(siicas.Credentials, time.Duration) (siicas.CredentialStatus, error)
	Status(context.Context) (siicas.CredentialStatus, error)
	Revoke() (bool, error)
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(args, stdin, stdout, stderr, siicas.NewKeyringCredentialStore())
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, store credentialStore) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: sii-link credentials {put|status|revoke}")
		return 2
	}

	switch args[0] {
	case "put":
		flags := flag.NewFlagSet("credentials put", flag.ContinueOnError)
		flags.SetOutput(stderr)
		ttlText := flags.String("ttl", "", "Optional credential lifetime, for example 8h")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(stderr, "credentials put does not accept positional arguments")
			return 2
		}
		var ttl time.Duration
		if *ttlText != "" {
			parsed, err := time.ParseDuration(*ttlText)
			if err != nil || parsed <= 0 {
				fmt.Fprintln(stderr, "credential TTL must be a positive duration")
				return 2
			}
			ttl = parsed
		}

		payload, err := io.ReadAll(io.LimitReader(stdin, maxCredentialInput+1))
		if err != nil {
			fmt.Fprintf(stderr, "read credentials: %v\n", err)
			return 1
		}
		defer clear(payload)
		if len(payload) > maxCredentialInput {
			fmt.Fprintln(stderr, "credential input is too large")
			return 1
		}
		var credentials siicas.Credentials
		if err := json.Unmarshal(payload, &credentials); err != nil {
			fmt.Fprintln(stderr, "credential input must be one JSON object with username and password")
			return 1
		}
		status, err := store.Put(credentials, ttl)
		if err != nil {
			fmt.Fprintf(stderr, "store credentials: %v\n", err)
			return 1
		}
		return writeJSON(stdout, map[string]any{
			"stored":     true,
			"storage":    "linux-kernel-keyring",
			"expires_at": status.ExpiresAt,
		}, stderr)

	case "status":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "credentials status does not accept arguments")
			return 2
		}
		status, err := store.Status(context.Background())
		if err != nil {
			fmt.Fprintf(stderr, "read credential status: %v\n", err)
			return 1
		}
		if code := writeJSON(stdout, map[string]any{
			"available":  status.Available,
			"storage":    "linux-kernel-keyring",
			"expires_at": status.ExpiresAt,
		}, stderr); code != 0 {
			return code
		}
		if !status.Available {
			return credentialUnavailableExit
		}
		return 0

	case "revoke":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "credentials revoke does not accept arguments")
			return 2
		}
		revoked, err := store.Revoke()
		if err != nil {
			fmt.Fprintf(stderr, "revoke credentials: %v\n", err)
			return 1
		}
		return writeJSON(stdout, map[string]any{
			"revoked": revoked,
			"storage": "linux-kernel-keyring",
		}, stderr)

	default:
		fmt.Fprintf(stderr, "unknown credentials command %q\n", args[0])
		return 2
	}
}

func writeJSON(stdout io.Writer, value any, stderr io.Writer) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		fmt.Fprintf(stderr, "write credential result: %v\n", err)
		return 1
	}
	return 0
}
