package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"testing"
	"time"
)

func TestGatewayWorkerProcess(t *testing.T) {
	if os.Getenv("SII_GATEWAY_TEST_WORKER") != "1" {
		return
	}
	g, err := NativeGate()
	if err != nil || g == nil {
		os.Exit(10)
	}
	if err := g.BeforeLogin(); err != nil {
		os.Exit(11)
	}
	g.AfterLogin(nil)
	listener, err := net.Listen("tcp", os.Getenv("SII_GATEWAY_TEST_PROXY"))
	if err != nil {
		os.Exit(12)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })}
	go server.Serve(listener)
	if err := g.SessionReady(); err != nil {
		os.Exit(13)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	<-ctx.Done()
	server.Close()
	os.Exit(0)
}

func TestRealWorkerReceiptsAndHandoverStopProxy(t *testing.T) {
	if os.Getenv("SII_GATEWAY_TEST_WORKER") == "1" {
		return
	}
	// Keep the Unix socket path below the macOS sockaddr_un length limit.
	dir, err := os.MkdirTemp("", "sii-gw-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	probe.Close()
	config := filepath.Join(dir, "native.toml")
	if err := os.WriteFile(config, []byte(fmt.Sprintf("protocol='atrust'\nserver_address='vpn.sii.edu.cn'\nserver_port=443\nsii_unattended_cas=true\nclient_data_file='%s'\nsocks_bind='127.0.0.1:1080'\nhttp_bind='%s'\nkeep_alive_url='http://fixture.invalid/'\n", filepath.Join(dir, "unused-cache.json"), address)), 0600); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(dir, "native-worker")
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nexec "+shellQuote(os.Args[0])+" -test.run '^TestGatewayWorkerProcess$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SII_GATEWAY_TEST_WORKER", "1")
	t.Setenv("SII_GATEWAY_TEST_PROXY", address)
	state := State{Role: "server", Config: config, Epoch: 1, Budget: true, Self: "this-host"}
	if err := saveState(dir, state); err != nil {
		t.Fatal(err)
	}
	c := &controller{dir: dir, binary: worker, state: state, wake: make(chan struct{}, 1), peer: func(context.Context, string, ...string) (Status, error) {
		return Status{Protocol: Protocol, Role: "server", Ready: true}, nil
	}}
	listener, err := net.Listen("unix", filepath.Join(dir, "gateway.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(c.handler)}
	go server.Serve(listener)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exited := make(chan error, 1)
	go func() { exited <- c.runWorker(ctx, state) }()
	for {
		s := c.status()
		if s.Attempt == 1 && !s.LoginPending && s.ProviderRunning {
			break
		}
		select {
		case err := <-exited:
			t.Fatalf("worker failed before readiness: %v", err)
		case <-ctx.Done():
			t.Fatal("worker never confirmed cache readiness")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if c.status().RenewalAvailable {
		t.Fatal("worker receipt restored renewal budget")
	}
	if err := c.dispatch(ctx, request{Action: "client", Peer: "peer-host"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("old worker did not drain")
	}
	if s := c.status(); s.Role != "client" || s.ProviderRunning || s.LoginPending {
		t.Fatalf("handover acknowledged wrong provider: %+v", s)
	}
	connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err == nil {
		connection.Close()
		t.Fatal("native proxy listener survived Client handover")
	}
	saved, err := readState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Role != "client" || saved.Budget {
		t.Fatal("Client intent did not persist")
	}
}
