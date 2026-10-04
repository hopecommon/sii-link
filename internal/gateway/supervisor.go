package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hopecommon/sii-link/client/atrust/auth"
)

type controller struct {
	mu               sync.Mutex
	changing         sync.Mutex
	dir, binary      string
	state            State
	pid              int
	ready, switching bool
	lastError        string
	cancel           context.CancelFunc
	done             chan struct{}
	wake             chan struct{}
	peer             func(context.Context, string, ...string) (Status, error)
}

func (c *controller) status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.state
	return Status{Protocol: Protocol, Role: s.Role, Peer: s.Peer, Config: s.Config, Epoch: s.Epoch, Attempt: s.Attempt,
		Running: true, ProviderRunning: c.pid != 0, Ready: c.ready, Transitioning: c.switching,
		LoginPending: s.Pending, RenewalAvailable: s.Budget, Pause: s.Pause, Error: c.lastError}
}

func (c *controller) persistLocked(next State) error {
	if err := saveState(c.dir, next); err != nil {
		return err
	}
	c.state = next
	return nil
}

func (c *controller) notify() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *controller) stop(ctx context.Context) error {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.New("provider did not stop; handover remains unconfirmed")
		}
	}
	return nil
}

func (c *controller) selectRole(ctx context.Context, r request) error {
	if !c.changing.TryLock() {
		return errBusy
	}
	defer c.changing.Unlock()
	c.mu.Lock()
	if c.state.Pending && r.Action != "off" {
		c.mu.Unlock()
		return errBusy
	}
	c.switching = true
	next := c.state
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.switching = false; c.mu.Unlock(); c.notify() }()
	if r.Config != "" {
		next.Config = r.Config
	}
	if r.Action != "off" {
		if _, err := readConfig(next.Config); err != nil {
			return err
		}
	}
	if r.Peer != "" {
		if !validPeer(r.Peer) {
			return errors.New("invalid SSH peer")
		}
		next.Peer = r.Peer
	}
	if r.Self != "" {
		if !validPeer(r.Self) {
			return errors.New("invalid SSH self address")
		}
		next.Self = r.Self
	}
	// Stop our old relay before the peer starts forwarding back to this machine.
	if err := c.stop(ctx); err != nil {
		return err
	}
	switch r.Action {
	case "server":
		if next.Self == "" {
			var err error
			next.Self, err = selfAddress(ctx)
			if err != nil {
				return err
			}
		}
		if next.Peer != "" {
			var peer Status
			var err error
			for {
				peer, err = c.peer(ctx, next.Peer, "yield", "--peer", next.Self, "--json")
				if !errors.Is(err, errBusy) {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(300 * time.Millisecond):
				}
			}
			if err != nil && !errors.Is(err, errPeerOffline) {
				return err
			}
			if err == nil && (peer.Role != "client" || peer.LoginPending || peer.Transitioning) {
				return errors.New("peer did not confirm Client handover")
			}
			// An explicit Server operation may take over an unreachable peer.
		}
		next.Role, next.Budget = "server", true
	case "client", "yield":
		if next.Peer == "" {
			return errors.New("client requires an SSH upstream")
		}
		next.Role, next.Budget = "client", false
	case "off":
		next.Role, next.Budget = "off", false
	default:
		return errors.New("invalid role operation")
	}
	next.Epoch++
	if err := ctx.Err(); err != nil {
		return err
	}
	next.Pending, next.Pause, next.Cursor, next.SessionHash = false, "", "", ""
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready, c.lastError = false, ""
	return c.persistLocked(next)
}

func (c *controller) beforeLogin(ctx context.Context, r request) error {
	c.mu.Lock()
	s := c.state
	if r.PID != c.pid || c.pid == 0 || r.Epoch != s.Epoch || s.Role != "server" || c.switching {
		c.mu.Unlock()
		return errors.New("gateway login authority changed")
	}
	if s.Pending || s.Pause != "" || !s.Budget {
		if s.Pause == "" {
			s.Pause = "repeated session loss; choose sii server to renew authority"
		}
		err := c.persistLocked(s)
		c.mu.Unlock()
		if err != nil {
			return err
		}
		return errors.New(s.Pause)
	}
	c.mu.Unlock()
	if s.Peer != "" {
		peer, err := c.peer(ctx, s.Peer, "status", "--json")
		if err == nil && peer.Role == "server" && (peer.Ready || peer.LoginPending || peer.Transitioning) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.state.Epoch != s.Epoch {
				return errors.New("gateway login authority changed")
			}
			next := c.state
			next.Role, next.Budget = "client", false
			next.Epoch++
			if err := c.persistLocked(next); err != nil {
				return err
			}
			if c.cancel != nil {
				c.cancel()
			}
			c.notify()
			return errors.New("peer owns or is acquiring the SII session; following peer")
		}
		// Unknown peer state stays unknown. Existing Server authority permits one
		// attempt; the durable budget prevents supervisor-driven login ping-pong.
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Epoch != s.Epoch || c.switching || c.state.Pending || !c.state.Budget || c.state.Role != "server" {
		return errors.New("gateway login authority changed during preflight")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.state
	next.Attempt++
	next.Budget, next.Pending = false, true
	return c.persistLocked(next)
}

func (c *controller) dispatch(ctx context.Context, r request) error {
	switch r.Action {
	case "status":
		return nil
	case "server", "client", "yield", "off":
		return c.selectRole(ctx, r)
	case "login":
		return c.beforeLogin(ctx, r)
	case "worker", "login-result", "session-ready":
		c.mu.Lock()
		defer c.mu.Unlock()
		s := c.state
		if r.PID != c.pid || c.pid == 0 || s.Role != "server" || c.switching {
			return errors.New("native worker is not the selected provider")
		}
		if r.Action == "worker" {
			return nil
		}
		if r.Epoch != s.Epoch || (r.Attempt != 0 && r.Attempt != s.Attempt) {
			return errors.New("stale authentication receipt")
		}
		if r.Action == "session-ready" {
			s.Pending, s.Pause = false, ""
		} else if !r.Success {
			s.Pending = false
			if r.RetrySafe {
				s.Budget, s.Pause = true, ""
			} else {
				s.Pause = "authentication outcome uncertain; cached resume remains available, inspect sii doctor"
			}
		}
		return c.persistLocked(s)
	default:
		return errors.New("unknown gateway operation")
	}
}

func (c *controller) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/control" {
		http.NotFound(w, r)
		return
	}
	var command request
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&command); err != nil {
		http.Error(w, "invalid command", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := c.dispatch(ctx, command)
	result := response{Status: c.status()}
	if err != nil {
		result.Error = err.Error()
		if errors.Is(err, errBusy) {
			result.Code = "busy"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func healthy(ctx context.Context, cfg localConfig) bool {
	proxy, err := url.Parse("http://" + cfg.HTTP)
	if err != nil {
		return false
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, cfg.HealthURL, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (c *controller) runWorker(ctx context.Context, snapshot State) error {
	cfg, err := readConfig(snapshot.Config)
	if err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cmd *exec.Cmd
	if snapshot.Role == "server" {
		cmd = exec.CommandContext(workerCtx, c.binary, "-config", snapshot.Config)
		cmd.Env = append(os.Environ(), "SII_GATEWAY_DIR="+c.dir)
	} else {
		peerCtx, stop := context.WithTimeout(workerCtx, 8*time.Second)
		peer, probeErr := c.peer(peerCtx, snapshot.Peer, "status", "--json")
		stop()
		if probeErr == nil && peer.Role != "server" {
			return errors.New("upstream is not a Server; waiting for role selection")
		}
		cmd = exec.CommandContext(workerCtx, "ssh", "-N", "-T", "-o", "BatchMode=yes", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=5",
			"-L", cfg.HTTP+":"+cfg.HTTP, "-L", cfg.SOCKS+":"+cfg.SOCKS, snapshot.Peer)
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	done := make(chan struct{})
	c.mu.Lock()
	if c.state.Epoch != snapshot.Epoch || c.switching {
		c.mu.Unlock()
		return nil
	}
	c.cancel, c.done = cancel, done
	err = cmd.Start()
	if err == nil {
		c.pid = cmd.Process.Pid
	}
	c.mu.Unlock()
	if err != nil {
		c.mu.Lock()
		c.cancel, c.done = nil, nil
		c.mu.Unlock()
		close(done)
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if snapshot.Role == "server" {
		go c.events(workerCtx, cfg, snapshot.Epoch)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var readySince time.Time
	defer func() {
		cancel()
		c.mu.Lock()
		c.pid, c.ready, c.cancel, c.done = 0, false, nil, nil
		c.mu.Unlock()
		close(done)
	}()
	for {
		select {
		case err := <-exited:
			if workerCtx.Err() != nil {
				return nil
			}
			return err
		case <-ticker.C:
			ok := healthy(workerCtx, cfg)
			if ok {
				ticker.Reset(30 * time.Second)
			} else {
				ticker.Reset(5 * time.Second)
			}
			c.mu.Lock()
			if c.state.Epoch != snapshot.Epoch || c.switching {
				c.mu.Unlock()
				continue
			}
			c.ready = ok
			if ok && readySince.IsZero() {
				readySince = time.Now()
			}
			if !ok {
				readySince = time.Time{}
			}
			if snapshot.Role == "server" && ok && time.Since(readySince) >= stableWindow && !c.state.Pending && c.state.Pause == "" && !c.state.Budget {
				next := c.state
				next.Budget = true
				if err := c.persistLocked(next); err != nil {
					c.lastError = "unable to persist renewal budget"
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *controller) loop(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		c.mu.Lock()
		s, switching := c.state, c.switching
		c.mu.Unlock()
		if s.Role == "off" || switching {
			select {
			case <-ctx.Done():
				return
			case <-c.wake:
				continue
			}
		}
		started := time.Now()
		err := c.runWorker(ctx, s)
		if ctx.Err() != nil {
			return
		}
		c.mu.Lock()
		if err != nil {
			c.lastError = "provider exited; inspect sii doctor, the native log and SSH connectivity"
		}
		changed := c.state.Epoch != s.Epoch
		c.mu.Unlock()
		if changed {
			delay = time.Second
			continue
		}
		if time.Since(started) >= stableWindow {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func supervise(ctx context.Context, dir, config, binary string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := lock(filepath.Join(dir, "gateway.lock"))
	if err != nil {
		return fmt.Errorf("gateway already supervised or lock unavailable: %w", err)
	}
	defer file.Close()
	s, err := readState(dir)
	if err != nil {
		return err
	}
	if s.Config == "" {
		s.Config = config
	}
	if err := saveState(dir, s); err != nil {
		return err
	}
	socket := filepath.Join(dir, "gateway.sock")
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}
	c := &controller{dir: dir, binary: binary, state: s, wake: make(chan struct{}, 1), peer: peerCall}
	server := &http.Server{Handler: http.HandlerFunc(c.handler), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go c.loop(ctx)
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err = c.stop(stopCtx)
	_ = server.Shutdown(stopCtx)
	return err
}

func sessionFile(path string) (auth.ClientAuthData, string, error) {
	var data auth.ClientAuthData
	contents, err := os.ReadFile(path)
	if err != nil {
		return data, "", err
	}
	if err := json.Unmarshal(contents, &data); err != nil {
		return data, "", err
	}
	hash := sha256.Sum256(contents)
	return data, hex.EncodeToString(hash[:]), nil
}

func (c *controller) events(ctx context.Context, cfg localConfig, epoch uint64) {
	for ctx.Err() == nil {
		// Invalid sessions can produce immediate empty batches instead of a long poll.
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		cache, hash, err := sessionFile(cfg.ClientData)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		session := newSession(cfg)
		_, _ = session.Login(nil, auth.LoginOptions{DeviceID: cache.DeviceID, Cookies: cache.Cookies})
		c.mu.Lock()
		attempt := c.state.Attempt
		cursor := c.state.Cursor
		if c.state.SessionHash != hash {
			cursor = "0"
		}
		pending := c.state.Pending
		c.mu.Unlock()
		if pending {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		batch, cursor, err := session.Events(ctx, cursor)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		_, currentHash, err := sessionFile(cfg.ClientData)
		if err != nil || currentHash != hash {
			continue
		}
		for _, event := range batch {
			if event.Name != "logout" {
				continue
			}
			// A logout generated by our own earlier authentication can arrive late.
			if _, err := session.Login(nil, auth.LoginOptions{DeviceID: cache.DeviceID, Cookies: cache.Cookies}); !errors.Is(err, auth.ErrSessionInvalid) {
				continue
			}
			c.observeLogout(epoch, attempt, event.Data.Type)
		}
		c.mu.Lock()
		if c.state.Epoch == epoch && c.state.Attempt == attempt {
			next := c.state
			next.SessionHash, next.Cursor = hash, cursor
			if err := c.persistLocked(next); err != nil {
				c.lastError = "unable to persist event cursor"
			}
		}
		c.mu.Unlock()
	}
}

// The caller has verified that the same cached session is currently invalid.
func (c *controller) observeLogout(epoch, attempt uint64, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Epoch != epoch || c.state.Attempt != attempt || c.state.Pending || c.switching || c.state.Role != "server" {
		return
	}
	next := c.state
	switch reason {
	case "relogin", "trustDevice":
		next.Role, next.Budget = "off", false
		if next.Peer != "" {
			next.Role = "client"
		}
		next.Epoch++
	case "timeout", "timeoutOffline":
	default:
		next.Pause = "logout policy requires inspection; run sii doctor"
	}
	if err := c.persistLocked(next); err != nil {
		c.lastError = "unable to persist logout decision"
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.notify()
}
