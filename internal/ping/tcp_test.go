package ping

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPingStartsFirstProbeImmediately(t *testing.T) {
	ping := NewTCPing()
	ping.SetDialContext(func(context.Context, string, string) (net.Conn, error) {
		return &stubConn{}, nil
	})
	ping.SetTarget(&Target{
		Protocol: TCP,
		Host:     "127.0.0.1",
		Port:     443,
		Counter:  1,
		Interval: 500 * time.Millisecond,
		Timeout:  time.Second,
	})

	started := time.Now()
	<-ping.Start()
	if elapsed := time.Since(started); elapsed >= 400*time.Millisecond {
		t.Fatalf("single probe completed in %s, want less than one interval", elapsed)
	}
	if ping.Result().Counter != 1 || ping.Result().SuccessCounter != 1 {
		t.Fatalf("probe result = %#v, want one successful probe", ping.Result())
	}
}

type stubConn struct{}

func (*stubConn) Read([]byte) (int, error)         { return 0, nil }
func (*stubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*stubConn) Close() error                     { return nil }
func (*stubConn) LocalAddr() net.Addr              { return stubAddr("local") }
func (*stubConn) RemoteAddr() net.Addr             { return stubAddr("remote") }
func (*stubConn) SetDeadline(time.Time) error      { return nil }
func (*stubConn) SetReadDeadline(time.Time) error  { return nil }
func (*stubConn) SetWriteDeadline(time.Time) error { return nil }

type stubAddr string

func (a stubAddr) Network() string { return string(a) }
func (a stubAddr) String() string  { return string(a) }
