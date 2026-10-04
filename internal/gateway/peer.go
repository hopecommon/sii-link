package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

var errPeerOffline = errors.New("SSH peer unavailable")

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func peerCall(ctx context.Context, peer string, args ...string) (Status, error) {
	if !validPeer(peer) {
		return Status{}, errors.New("invalid SSH peer")
	}
	command := `b="$HOME/.local/bin/sii-link"; if ! "$b" -version | grep -qx 'Gateway protocol: 1'; then exit 64; fi; exec "$b" gateway`
	for _, arg := range args {
		command += " " + shellQuote(arg)
	}
	output, err := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", peer, command).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 3 {
			var result Status
			if json.Unmarshal(output, &result) == nil && result.Protocol == Protocol && result.CommandError == "busy" {
				return result, errBusy
			}
		}
		if errors.As(err, &exit) && exit.ExitCode() == 255 {
			return Status{}, errPeerOffline
		}
		if ctx.Err() != nil {
			return Status{}, errPeerOffline
		}
		return Status{}, errors.New("peer needs a gateway-capable SII Link supervisor; run sii doctor on the peer")
	}
	var result Status
	if err := json.Unmarshal(output, &result); err != nil || result.Protocol != Protocol {
		return Status{}, errors.New("invalid peer gateway response")
	}
	return result, nil
}

func selfAddress(ctx context.Context) (string, error) {
	account, err := user.Current()
	if err != nil || !validPeer(account.Username) {
		return "", errors.New("set an SSH-reachable address with --self")
	}
	paths := []string{"tailscale", "/Applications/Tailscale.app/Contents/MacOS/Tailscale"}
	for _, binary := range paths {
		output, err := exec.CommandContext(ctx, binary, "status", "--json").Output()
		if err != nil {
			continue
		}
		var status struct{ Self struct{ DNSName string } }
		if json.Unmarshal(output, &status) == nil {
			name := strings.TrimSuffix(status.Self.DNSName, ".")
			if validPeer(name) {
				return account.Username + "@" + name, nil
			}
		}
	}
	name, err := os.Hostname()
	if err != nil || !validPeer(name) {
		return "", fmt.Errorf("set an SSH-reachable address with --self")
	}
	return account.Username + "@" + name, nil
}
