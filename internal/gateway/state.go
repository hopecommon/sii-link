// Package gateway owns the local proxy role and permission to create SII sessions.
package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/hopecommon/sii-link/client/atrust/auth"
	"github.com/hopecommon/sii-link/internal/securefile"
	"github.com/hopecommon/sii-link/internal/underlay"
)

const Protocol = 1

type State struct {
	Role        string `json:"role"`
	Peer        string `json:"peer,omitempty"`
	Self        string `json:"self,omitempty"`
	Config      string `json:"config"`
	Epoch       uint64 `json:"epoch"`
	Attempt     uint64 `json:"attempt"`
	Budget      bool   `json:"renewal_available"`
	Pending     bool   `json:"login_pending"`
	Pause       string `json:"pause_reason,omitempty"`
	Cursor      string `json:"event_cursor,omitempty"`
	SessionHash string `json:"session_hash,omitempty"`
}

type Status struct {
	Protocol         int    `json:"protocol"`
	Role             string `json:"role"`
	Peer             string `json:"peer,omitempty"`
	Config           string `json:"config"`
	Epoch            uint64 `json:"epoch"`
	Attempt          uint64 `json:"attempt"`
	Running          bool   `json:"supervisor_running"`
	ProviderRunning  bool   `json:"provider_running"`
	Ready            bool   `json:"ready"`
	Transitioning    bool   `json:"transitioning"`
	LoginPending     bool   `json:"login_pending"`
	RenewalAvailable bool   `json:"renewal_available"`
	Pause            string `json:"pause_reason,omitempty"`
	Error            string `json:"last_error,omitempty"`
	CommandError     string `json:"command_error,omitempty"`
}

type localConfig struct {
	Protocol      string `toml:"protocol"`
	Server        string `toml:"server_address"`
	Port          int    `toml:"server_port"`
	Unattended    bool   `toml:"sii_unattended_cas"`
	ClientData    string `toml:"client_data_file"`
	SOCKS         string `toml:"socks_bind"`
	HTTP          string `toml:"http_bind"`
	HealthURL     string `toml:"keep_alive_url"`
	SID           string `toml:"sid"`
	Resource      string `toml:"resource_file"`
	BindInterface string `toml:"bind_interface"`
	AutoDetect    bool   `toml:"auto_detect_interface"`
}

func readConfig(path string) (localConfig, error) {
	var c localConfig
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return c, fmt.Errorf("read gateway config: %w", err)
	}
	if c.Protocol != "atrust" || c.Server != "vpn.sii.edu.cn" || c.Port != 443 || !c.Unattended {
		return c, errors.New("gateway requires unattended SII aTrust configuration")
	}
	if !filepath.IsAbs(c.ClientData) || c.SID != "" || c.Resource != "" {
		return c, errors.New("gateway requires an absolute client_data_file and cached-session validation")
	}
	for _, address := range []string{c.SOCKS, c.HTTP} {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return c, errors.New("gateway proxy listeners must use explicit loopback addresses")
		}
	}
	if c.HealthURL == "" {
		c.HealthURL = "https://qz.sii.edu.cn/"
	}
	return c, nil
}

var sshName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@:-]*$`)

func validPeer(name string) bool { return sshName.MatchString(name) }

func defaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "sii-link")
}

func readState(dir string) (State, error) {
	data, err := os.ReadFile(filepath.Join(dir, "gateway.json"))
	if errors.Is(err, os.ErrNotExist) {
		return State{Role: "off"}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("read gateway state: %w", err)
	}
	if s.Role != "server" && s.Role != "client" && s.Role != "off" {
		return s, errors.New("invalid saved gateway role")
	}
	if (s.Peer != "" && !validPeer(s.Peer)) || (s.Self != "" && !validPeer(s.Self)) || (s.Role == "client" && s.Peer == "") {
		return s, errors.New("invalid saved SSH peer")
	}
	return s, nil
}

func saveState(dir string, s State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return securefile.Write(filepath.Join(dir, "gateway.json"), append(data, '\n'))
}

const stableWindow = 2 * time.Minute

var errBusy = errors.New("gateway busy; wait for authentication or handover")

func newSession(cfg localConfig) *auth.Session {
	dialer := underlay.New(net.JoinHostPort(cfg.Server, "443"), underlay.Options{InterfaceName: cfg.BindInterface, AutoDetect: cfg.AutoDetect})
	s := auth.NewSession(cfg.Server, dialer.DialContext)
	s.RequireVerifiedTLS()
	return s
}

// Installation initializes Off once and leaves live permission writes to the
// supervisor, so an updater cannot restore a spent renewal budget.
func initializeState(dir, config string) error {
	if _, err := os.Stat(filepath.Join(dir, "gateway.json")); err == nil {
		_, err = readState(dir)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := lock(filepath.Join(dir, "gateway.lock"))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := os.Stat(filepath.Join(dir, "gateway.json")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return saveState(dir, State{Role: "off", Config: config})
}
