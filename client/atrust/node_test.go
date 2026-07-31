package atrust

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetBestNodesUsesRequestedProbeCount(t *testing.T) {
	var calls atomic.Int32
	dial := func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return &nodeStubConn{}, nil
	}

	best := getBestNodes(map[string][]string{
		"group": {"127.0.0.1:441", "127.0.0.2:441"},
	}, dial, 1)
	if best["group"] == "" {
		t.Fatal("best node is empty")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("dial calls = %d, want one probe for each of two nodes", got)
	}
}

func TestGetBestNodesKeepsProbeAddressMappingAfterInvalidNode(t *testing.T) {
	best := getBestNodes(map[string][]string{
		"group": {"invalid-address", "127.0.0.2:441"},
	}, func(context.Context, string, string) (net.Conn, error) {
		return &nodeStubConn{}, nil
	}, 1)

	if got, want := best["group"], "127.0.0.2:441"; got != want {
		t.Fatalf("best node = %q, want %q", got, want)
	}
}

type nodeStubConn struct{}

func (*nodeStubConn) Read([]byte) (int, error)         { return 0, nil }
func (*nodeStubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*nodeStubConn) Close() error                     { return nil }
func (*nodeStubConn) LocalAddr() net.Addr              { return nodeStubAddr("local") }
func (*nodeStubConn) RemoteAddr() net.Addr             { return nodeStubAddr("remote") }
func (*nodeStubConn) SetDeadline(time.Time) error      { return nil }
func (*nodeStubConn) SetReadDeadline(time.Time) error  { return nil }
func (*nodeStubConn) SetWriteDeadline(time.Time) error { return nil }

type nodeStubAddr string

func (a nodeStubAddr) Network() string { return string(a) }
func (a nodeStubAddr) String() string  { return string(a) }
