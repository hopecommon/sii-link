package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hopecommon/sii-link/client/atrust/auth"
	"github.com/hopecommon/sii-link/log"
)

func Run(args []string, output, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(output, "Usage: sii {server|client HOST|off|status|doctor|install|run} [--config PATH] [--peer HOST] [--self HOST] [--json]\nserver selects this gateway; client selects an SSH upstream; install/run restore the saved role.")
		return 0
	}
	action := args[0]
	r := request{Action: action}
	args = args[1:]
	if action == "client" && len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		r.Peer, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("sii "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&r.Config, "config", "", "native SII configuration path")
	flags.StringVar(&r.Peer, "peer", r.Peer, "SSH peer or upstream")
	flags.StringVar(&r.Self, "self", "", "SSH address the peer uses for this machine")
	dir := flags.String("state-dir", defaultDir(), "private gateway state directory")
	jsonOutput := flags.Bool("json", false, "emit machine-readable status")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected arguments")
		return 2
	}
	if action == "config" {
		s, err := readState(*dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if s.Config == "" {
			s.Config = installedConfig()
		}
		fmt.Fprintln(output, s.Config)
		return 0
	}
	if action != "server" && action != "client" && action != "yield" && action != "off" && action != "status" && action != "doctor" && action != "install" && action != "run" {
		fmt.Fprintln(stderr, "unknown gateway command; run sii help")
		return 2
	}
	if !filepath.IsAbs(*dir) {
		fmt.Fprintln(stderr, "state-dir must be absolute")
		return 2
	}
	if r.Config != "" {
		var err error
		r.Config, err = filepath.Abs(r.Config)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	binary, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if action == "run" {
		if r.Config == "" {
			r.Config = installedConfig()
		}
		writer, err := log.ConfigureFile(filepath.Join(*dir, "gateway.log"), 5<<20, 3)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer writer.Close()
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := supervise(ctx, *dir, r.Config, binary); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if action == "install" {
		saved, err := readState(*dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if r.Config != "" && saved.Config != "" && r.Config != saved.Config {
			fmt.Fprintln(stderr, "use sii server/client --config to change the saved configuration")
			return 1
		}
		if saved.Config == "" {
			saved.Config = r.Config
		}
		if saved.Config == "" {
			saved.Config = installedConfig()
		}
		if err := initializeState(*dir, saved.Config); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := installService(*dir, binary, saved.Config); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if _, err := waitSupervisor(ctx, *dir); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		action, r.Action = "status", "status"
	}
	status, err := call(ctx, *dir, request{Action: "status"})
	if err != nil {
		saved, readErr := readState(*dir)
		if readErr != nil {
			fmt.Fprintln(stderr, readErr)
			return 1
		}
		status = Status{Protocol: Protocol, Role: saved.Role, Peer: saved.Peer, Config: saved.Config, Epoch: saved.Epoch, Pause: saved.Pause, LoginPending: saved.Pending, RenewalAvailable: saved.Budget}
		if action != "status" && action != "doctor" {
			if r.Config == "" {
				r.Config = saved.Config
			}
			if r.Config == "" {
				r.Config = installedConfig()
			}
			if action != "off" {
				if _, err := readConfig(r.Config); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
			if err := initializeState(*dir, r.Config); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if err := installService(*dir, binary, r.Config); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			status, err = waitSupervisor(ctx, *dir)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
	}
	if action == "doctor" {
		report := struct {
			Status        Status  `json:"status"`
			CachedSession string  `json:"cached_session"`
			Peer          *Status `json:"peer_status,omitempty"`
			PeerError     string  `json:"peer_error,omitempty"`
		}{Status: status, CachedSession: "unknown"}
		if cfg, e := readConfig(status.Config); e == nil {
			if cache, _, e := sessionFile(cfg.ClientData); e == nil && status.Role == "server" {
				session := newSession(cfg)
				_, e := session.Login(nil, auth.LoginOptions{DeviceID: cache.DeviceID, Cookies: cache.Cookies})
				if e == nil {
					report.CachedSession = "valid"
				} else if errors.Is(e, auth.ErrSessionInvalid) {
					report.CachedSession = "invalid"
				}
			}
		}
		if status.Role != "server" {
			report.CachedSession = "not_required"
		}
		if status.Peer != "" {
			peerCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			peer, err := peerCall(peerCtx, status.Peer, "status", "--json")
			cancel()
			if err == nil {
				report.Peer = &peer
			} else {
				report.PeerError = err.Error()
			}
		}
		_ = json.NewEncoder(output).Encode(report)
		return 0
	}
	if action != "status" {
		for {
			status, err = call(ctx, *dir, r)
			if !errors.Is(err, errBusy) || action == "yield" {
				break
			}
			select {
			case <-ctx.Done():
				fmt.Fprintln(stderr, "handover still busy; inspect sii doctor")
				return 1
			case <-time.After(300 * time.Millisecond):
			}
		}
		if err != nil {
			if errors.Is(err, errBusy) && *jsonOutput {
				status.CommandError = "busy"
				_ = json.NewEncoder(output).Encode(status)
				return 3
			}
			fmt.Fprintln(stderr, err)
			return 1
		}
		if action != "off" && action != "yield" {
			for !status.Ready && status.Role == action && status.Pause == "" {
				select {
				case <-ctx.Done():
					fmt.Fprintln(stderr, "role selected; provider is recovering, inspect sii doctor")
					return 1
				case <-time.After(time.Second):
				}
				status, err = call(ctx, *dir, request{Action: "status"})
				if err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
		}
	}
	if *jsonOutput {
		_ = json.NewEncoder(output).Encode(status)
	} else {
		fmt.Fprintf(output, "Role: %s\nSupervisor: %t\nProvider ready: %t\n", status.Role, status.Running, status.Ready)
		if status.Peer != "" {
			fmt.Fprintln(output, "Peer:", status.Peer)
		}
		if status.Pause != "" {
			fmt.Fprintln(output, "New logins paused:", status.Pause)
		}
		if status.Error != "" {
			fmt.Fprintln(output, "Diagnostic:", status.Error)
		}
	}
	if (action == "server" || action == "client") && (status.Role != action || !status.Ready) {
		fmt.Fprintln(stderr, "role selection is not ready; inspect sii doctor")
		return 1
	}
	return 0
}

func waitSupervisor(ctx context.Context, dir string) (Status, error) {
	for {
		status, err := call(ctx, dir, request{Action: "status"})
		if err == nil {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return Status{}, errors.New("supervisor did not start; inspect service manager")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
