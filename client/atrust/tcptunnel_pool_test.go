package atrust

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientDialTCPReusesCleanTunnel(t *testing.T) {
	var accepted atomic.Int32
	dial := func(context.Context, string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		accepted.Add(1)
		go serveReusableTCPTunnel(t, serverConn, 2)
		return clientConn, nil
	}

	pool := newTCPTunnelPool(1, dial)
	defer pool.Close()
	client := NewClient("test-user", "test-sid", "test-device", strings.Repeat("00", 32))
	defer client.Close()
	client.ConnectionID = "test-connection"
	client.MajorNodeGroup = "primary"
	client.BestNodes = map[string]string{"primary": "relay.example:443"}
	client.tcpTunnelPool = pool

	for i := 0; i < 2; i++ {
		conn, err := client.DialTCP(context.Background(), &net.TCPAddr{IP: net.ParseIP("10.0.0.10"), Port: 443})
		if err != nil {
			t.Fatalf("DialTCP() attempt %d error = %v", i+1, err)
		}
		closeWriter, ok := conn.(interface{ CloseWrite() error })
		if !ok {
			t.Fatalf("DialTCP() connection does not support half-close")
		}
		if err := closeWriter.CloseWrite(); err != nil {
			t.Fatalf("CloseWrite() attempt %d error = %v", i+1, err)
		}
		buf := make([]byte, 1)
		if _, err := conn.Read(buf); err != io.EOF {
			t.Fatalf("Read() attempt %d error = %v, want io.EOF", i+1, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("Close() attempt %d error = %v", i+1, err)
		}
	}

	if got := accepted.Load(); got != 1 {
		t.Fatalf("relay accepted %d TLS transports for two sequential flows, want 1", got)
	}
}

func TestTCPZeroRTTReportsDeferredConnectFailure(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	conn := newTCPTunnelConn(clientConn, bufio.NewReader(clientConn), nil)
	go func() {
		_, _ = serverConn.Write([]byte{0x05, 0x05})
		_ = serverConn.Close()
	}()

	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Read() error = %v, want deferred connection refused status", err)
	}
}

func serveReusableTCPTunnel(t *testing.T, conn net.Conn, flows int) {
	t.Helper()
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for i := 0; i < flows; i++ {
		initHeader := make([]byte, 7)
		if _, err := io.ReadFull(reader, initHeader); err != nil {
			return
		}
		if string(initHeader[:5]) != string([]byte{0x05, 0x01, 0x81, 0x53, 0x03}) {
			t.Errorf("unexpected init header: % X", initHeader[:5])
			return
		}
		initLen := int(binary.BigEndian.Uint16(initHeader[5:7]))
		if _, err := io.CopyN(io.Discard, reader, int64(initLen)); err != nil {
			return
		}

		destHeader := make([]byte, 4)
		if _, err := io.ReadFull(reader, destHeader); err != nil {
			return
		}
		switch destHeader[3] {
		case 0x01:
			if _, err := io.CopyN(io.Discard, reader, 6); err != nil {
				return
			}
		case 0x03:
			domainLen, err := reader.ReadByte()
			if err != nil {
				return
			}
			if _, err := io.CopyN(io.Discard, reader, int64(domainLen)+2); err != nil {
				return
			}
		default:
			t.Errorf("unexpected destination address type: 0x%02X", destHeader[3])
			return
		}

		if _, err := conn.Write([]byte{0x53, 0x00, 0x00, 0x02, 'O', 'K'}); err != nil {
			return
		}
		probe := make([]byte, 4)
		if _, err := io.ReadFull(reader, probe); err != nil {
			return
		}
		if _, err := conn.Write([]byte{
			0x05, 0x00,
			0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}); err != nil {
			return
		}

		closeFrame := make([]byte, 4)
		if _, err := io.ReadFull(reader, closeFrame); err != nil {
			return
		}
		if string(closeFrame) != string([]byte{0x01, 0x01, 0x00, 0x00}) {
			t.Errorf("unexpected close frame: % X", closeFrame)
			return
		}
		if _, err := conn.Write([]byte{0x01, 0x01, 0x30, 0x30}); err != nil {
			return
		}
	}
}
