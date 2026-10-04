package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hopecommon/sii-link/client/atrust/auth"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type request struct {
	Action    string `json:"action"`
	Peer      string `json:"peer,omitempty"`
	Self      string `json:"self,omitempty"`
	Config    string `json:"config,omitempty"`
	Epoch     uint64 `json:"epoch,omitempty"`
	Attempt   uint64 `json:"attempt,omitempty"`
	Success   bool   `json:"success,omitempty"`
	PID       int    `json:"pid,omitempty"`
	RetrySafe bool   `json:"retry_safe,omitempty"`
}

type response struct {
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

func call(ctx context.Context, dir string, r request) (Status, error) {
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "gateway.sock"))
	}}}
	defer client.CloseIdleConnections()
	data, _ := json.Marshal(r)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://gateway/control", bytes.NewReader(data))
	if err != nil {
		return Status{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	var result response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result); err != nil {
		return Status{}, err
	}
	if result.Error != "" {
		if result.Code == "busy" {
			return result.Status, errors.Join(errBusy, errors.New(result.Error))
		}
		return result.Status, errors.New(result.Error)
	}
	return result.Status, nil
}

// Gate controls fresh authentication while cached sessions stay reusable.
type Gate struct {
	Dir            string
	epoch, attempt uint64
}

func NativeGate() (*Gate, error) {
	dir := os.Getenv("SII_GATEWAY_DIR")
	if dir == "" {
		dir = defaultDir()
		if _, err := os.Stat(filepath.Join(dir, "gateway.json")); errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := call(ctx, dir, request{Action: "worker", PID: os.Getpid()})
	if err != nil {
		return nil, errors.New("gateway supervisor unavailable; run sii server or sii doctor")
	}
	if status.Role != "server" {
		return nil, errors.New("native login permission is held by the gateway controller; run sii status")
	}
	return &Gate{Dir: dir, epoch: status.Epoch}, nil
}

func (g *Gate) BeforeLogin() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := call(ctx, g.Dir, request{Action: "login", Epoch: g.epoch, PID: os.Getpid()})
	if err == nil {
		g.attempt = s.Attempt
	}
	return err
}

func (g *Gate) AfterLogin(err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = call(ctx, g.Dir, request{Action: "login-result", Epoch: g.epoch, Attempt: g.attempt, Success: err == nil, RetrySafe: errors.Is(err, auth.ErrLoginNotSubmitted), PID: os.Getpid()})
}

func (g *Gate) SessionReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := call(ctx, g.Dir, request{Action: "session-ready", Epoch: g.epoch, Attempt: g.attempt, PID: os.Getpid()})
	if err != nil {
		return fmt.Errorf("confirm managed session: %w", err)
	}
	return nil
}
