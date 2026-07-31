package atrust

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/hopecommon/sii-link/client"
	"github.com/hopecommon/sii-link/log"
	"github.com/hopecommon/sii-link/resolve"
)

type tcpTunnelConn struct {
	conn    net.Conn
	reader  *bufio.Reader
	readBuf []byte
	lease   *tcpTunnelLease

	writeMu       sync.Mutex
	closeWrite    sync.Once
	closeWriteErr error
	stateMu       sync.Mutex
	serverClosed  bool
	writeClosed   bool
	closed        bool
}

func newTCPTunnelConn(conn net.Conn, reader *bufio.Reader, lease *tcpTunnelLease) *tcpTunnelConn {
	if lease == nil {
		lease = &tcpTunnelLease{conn: conn}
	}
	return &tcpTunnelConn{conn: conn, reader: reader, lease: lease}
}

func readTCPProtocolResponse(reader *bufio.Reader) (string, error) {
	lengthBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, lengthBytes); err != nil {
		return "", err
	}
	data := make([]byte, binary.BigEndian.Uint16(lengthBytes))
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func waitForTCPConnect(ctx context.Context, conn net.Conn, reader *bufio.Reader) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		defer close(cancelDone)
		_ = conn.Close()
	})
	defer func() {
		if !stopCancel() {
			<-cancelDone
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
	}()

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil {
			return fmt.Errorf("failed to read tcp tunnel response: %w", err)
		}
		log.DebugPrint("Received header: ", fmt.Sprintf("%02X %02X", header[0], header[1]))
		if header[0] == 0x05 && header[1] == 0x81 {
			continue
		}
		if header[0] != 0x53 || header[1] != 0x00 {
			return fmt.Errorf("unexpected tcp tunnel response: %02X %02X", header[0], header[1])
		}

		response, err := readTCPProtocolResponse(reader)
		if err != nil {
			return fmt.Errorf("failed to read tcp tunnel protocol response: %w", err)
		}
		log.DebugPrint("Received protocol response:")
		log.DebugDumpHex([]byte(response))
		if !strings.Contains(response, "OK") {
			return fmt.Errorf("tcp tunnel setup failed: %s", response)
		}
		break
	}

	probe := []byte{0x01, 0x00, 0x00, 0x00}
	if n, err := conn.Write(probe); err != nil {
		return fmt.Errorf("failed to send tcp tunnel connect probe: %w", err)
	} else if n != len(probe) {
		return fmt.Errorf("failed to send tcp tunnel connect probe: %w", io.ErrShortWrite)
	}
	log.DebugPrint("Sent TCP connect probe")
	log.DebugDumpHex(probe)

	status := make([]byte, 2)
	if _, err := io.ReadFull(reader, status); err != nil {
		return fmt.Errorf("failed to read tcp tunnel connect status: %w", err)
	}
	log.DebugPrint("Received TCP connect status: ", fmt.Sprintf("%02X %02X", status[0], status[1]))
	return finishTCPConnect(reader, status)
}

func tcpConnectStatusError(status []byte) error {
	if len(status) != 2 || status[0] != 0x05 {
		return fmt.Errorf("unexpected tcp tunnel connect status: % X", status)
	}

	switch status[1] {
	case 0x00:
		return nil
	case 0x01:
		return fmt.Errorf("tcp tunnel server failure")
	case 0x02:
		return fmt.Errorf("tcp tunnel connection not allowed")
	case 0x03:
		return fmt.Errorf("network is unreachable")
	case 0x04:
		return fmt.Errorf("host is unreachable")
	case 0x05:
		return fmt.Errorf("connection refused")
	case 0x06:
		return fmt.Errorf("tcp tunnel TTL expired")
	case 0x07:
		return fmt.Errorf("tcp tunnel command not supported")
	case 0x08:
		return fmt.Errorf("tcp tunnel address type not supported")
	default:
		return fmt.Errorf("tcp tunnel connect failed with status 0x%02X", status[1])
	}
}

func finishTCPConnect(reader *bufio.Reader, status []byte) error {
	if err := tcpConnectStatusError(status); err != nil {
		return err
	}

	// A successful short-tunnel connect is followed by an eight-byte bound
	// endpoint record. The first two bytes identify the record and address
	// family; the remaining six bytes carry an IPv4 address and port.
	trailer := make([]byte, 8)
	if _, err := io.ReadFull(reader, trailer); err != nil {
		return fmt.Errorf("failed to read tcp tunnel connect trailer: %w", err)
	}
	if trailer[0] != 0x01 || trailer[1] != 0x01 {
		return fmt.Errorf("unexpected tcp tunnel connect trailer: % X", trailer)
	}
	return nil
}

func (c *Client) waitForTCPConnect(ctx context.Context, conn net.Conn, reader *bufio.Reader) error {
	if c.skipTCPTunnelWait {
		return nil
	}
	return waitForTCPConnect(ctx, conn, reader)
}

func (c *tcpTunnelConn) Read(b []byte) (int, error) {
	c.stateMu.Lock()
	closed := c.closed
	c.stateMu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	if len(c.readBuf) > 0 {
		n := copy(b, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	for {
		header := make([]byte, 2)
		_, err := io.ReadFull(c.reader, header)
		if err != nil {
			c.lease.Discard()
			return 0, err
		}
		log.DebugPrint("Received header: ", fmt.Sprintf("%02X %02X", header[0], header[1]))
		if header[0] == 0x01 && header[1] == 0x00 {
			lengthBytes := make([]byte, 2)
			_, err = io.ReadFull(c.reader, lengthBytes)
			if err != nil {
				c.lease.Discard()
				return 0, err
			}
			length := binary.BigEndian.Uint16(lengthBytes)
			data := make([]byte, length)
			_, err = io.ReadFull(c.reader, data)
			if err != nil {
				c.lease.Discard()
				return 0, err
			}
			log.DebugPrint("Received application data, length:", length)
			log.DebugDumpHex(data)

			n := copy(b, data)
			if n < len(data) {
				c.readBuf = data[n:]
			}

			return n, nil
		} else if header[0] == 0x01 && header[1] == 0x01 {
			header = make([]byte, 2)
			_, err = io.ReadFull(c.reader, header)
			if err != nil {
				c.lease.Discard()
				return 0, err
			}

			if header[0] == 0x30 && header[1] == 0x30 {
				log.DebugPrint("Received close message")
				c.stateMu.Lock()
				c.serverClosed = true
				c.stateMu.Unlock()
				return 0, io.EOF
			}
			c.lease.Discard()
			return 0, fmt.Errorf("unexpected tcp tunnel close status: %02X %02X", header[0], header[1])
		} else if header[0] == 0x53 && header[1] == 0x00 {
			lengthBytes := make([]byte, 2)
			_, err = io.ReadFull(c.reader, lengthBytes)
			if err != nil {
				c.lease.Discard()
				return 0, err
			}
			length := binary.BigEndian.Uint16(lengthBytes)

			data := make([]byte, length)
			_, err = io.ReadFull(c.reader, data)
			if err != nil {
				c.lease.Discard()
				return 0, err
			}

			log.DebugPrint("Received protocol response:")
			log.DebugDumpHex(data)

			if !strings.Contains(string(data), "OK") {
				c.lease.Discard()
				return 0, fmt.Errorf("tcp tunnel setup failed: %s", string(data))
			}
		} else if header[0] == 0x05 && header[1] == 0x81 {
			continue
		} else if header[0] == 0x05 {
			if err := finishTCPConnect(c.reader, header); err != nil {
				c.lease.Discard()
				return 0, err
			}
		} else {
			c.lease.Discard()
			return 0, fmt.Errorf("unexpected tcp tunnel frame header: %02X %02X", header[0], header[1])
		}
	}
}

func (c *tcpTunnelConn) Write(b []byte) (int, error) {
	header := []byte{0x01, 0x00}
	length := len(b)
	if length > 0xFFFF {
		return 0, fmt.Errorf("data too large")
	}
	lengthBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(lengthBytes, uint16(length))
	frame := bytes.Buffer{}
	frame.Write(header)
	frame.Write(lengthBytes)
	frame.Write(b)
	c.writeMu.Lock()
	c.stateMu.Lock()
	closed := c.closed || c.writeClosed
	c.stateMu.Unlock()
	if closed {
		c.writeMu.Unlock()
		return 0, net.ErrClosed
	}
	_, err := io.Copy(c.conn, bytes.NewReader(frame.Bytes()))
	c.writeMu.Unlock()
	log.DebugPrintf("aTrust TCP tunnel sent application data, length=%d", length)
	log.DebugDumpHex(frame.Bytes())
	if err != nil {
		c.lease.Discard()
		return 0, err
	}
	return length, nil
}

func (c *tcpTunnelConn) Close() error {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return nil
	}
	c.closed = true
	serverClosed := c.serverClosed
	c.stateMu.Unlock()
	if serverClosed {
		c.writeMu.Lock()
		c.writeMu.Unlock()
		c.lease.Release()
		return nil
	}

	err := c.CloseWrite()
	c.lease.Discard()
	return err
}

func (c *tcpTunnelConn) CloseWrite() error {
	c.closeWrite.Do(func() {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		c.stateMu.Lock()
		serverClosed := c.serverClosed
		c.writeClosed = true
		c.stateMu.Unlock()
		if serverClosed {
			return
		}

		closeMsg := []byte{0x01, 0x01, 0x00, 0x00}
		_, c.closeWriteErr = io.Copy(c.conn, bytes.NewReader(closeMsg))
		if c.closeWriteErr != nil {
			c.lease.Discard()
			return
		}
		log.DebugPrint("Sent close message")
		log.DebugDumpHex(closeMsg)
	})
	return c.closeWriteErr
}

func (c *tcpTunnelConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *tcpTunnelConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *tcpTunnelConn) SetDeadline(t time.Time) error {
	c.stateMu.Lock()
	closed := c.closed
	c.stateMu.Unlock()
	if closed {
		return net.ErrClosed
	}
	return c.conn.SetDeadline(t)
}

func (c *tcpTunnelConn) SetReadDeadline(t time.Time) error {
	c.stateMu.Lock()
	closed := c.closed
	c.stateMu.Unlock()
	if closed {
		return net.ErrClosed
	}
	return c.conn.SetReadDeadline(t)
}

func (c *tcpTunnelConn) SetWriteDeadline(t time.Time) error {
	c.stateMu.Lock()
	closed := c.closed
	c.stateMu.Unlock()
	if closed {
		return net.ErrClosed
	}
	return c.conn.SetWriteDeadline(t)
}

func randUint64() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return fmt.Sprint(binary.BigEndian.Uint64(b[:]))
}

func calcXRequestSig(key []byte, data []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	sum := h.Sum(nil)
	return strings.ToUpper(hex.EncodeToString(sum))
}

func (c *Client) acquireTCPTunnelTransport(ctx context.Context, nodeAddr string) (*tcpTunnelLease, error) {
	if c.tcpTunnelPool != nil {
		lease, err := c.tcpTunnelPool.Acquire(ctx, nodeAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to aTrust server: %w", err)
		}
		return lease, nil
	}
	return c.dialNewTCPTunnelTransport(ctx, nodeAddr)
}

func (c *Client) dialNewTCPTunnelTransport(ctx context.Context, nodeAddr string) (*tcpTunnelLease, error) {
	conn, err := c.underlayDialer.DialTLSContext(ctx, "tcp", nodeAddr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to aTrust server: %w", err)
	}
	return &tcpTunnelLease{conn: conn}, nil
}

func (c *Client) setupTCPTunnel(ctx context.Context, lease *tcpTunnelLease, initMsg, destMsg []byte) (*tcpTunnelConn, error) {
	if _, err := io.Copy(lease.conn, bytes.NewReader(initMsg)); err != nil {
		return nil, fmt.Errorf("failed to send init message: %w", err)
	}
	log.DebugDumpHex(initMsg)

	if _, err := io.Copy(lease.conn, bytes.NewReader(destMsg)); err != nil {
		return nil, fmt.Errorf("failed to send dest address: %w", err)
	}
	log.DebugDumpHex(destMsg)

	reader := bufio.NewReader(lease.conn)
	if err := c.waitForTCPConnect(ctx, lease.conn, reader); err != nil {
		return nil, err
	}
	return newTCPTunnelConn(lease.conn, reader, lease), nil
}

func (c *Client) DialTCP(ctx context.Context, addr *net.TCPAddr) (net.Conn, error) {
	appID := ""
	nodeGroupID := ""
	domain := ""
	if res := ctx.Value(resolve.ContextKeyDomainResource); res != nil {
		resource := res.(client.DomainResource)
		appID = resource.AppID
		nodeGroupID = resource.NodeGroupID
		if res = ctx.Value(resolve.ContextKeyResolveHost); res != nil {
			domain = res.(string)
		}
	} else {
		for _, resource := range c.ipResources {
			if bytes.Compare(addr.IP, resource.IPMin) >= 0 && bytes.Compare(addr.IP, resource.IPMax) <= 0 {
				if resource.PortMin <= addr.Port && addr.Port <= resource.PortMax {
					if resource.Protocol == "tcp" || resource.Protocol == "all" {
						appID = resource.AppID
						nodeGroupID = resource.NodeGroupID
					}
				}
			}
		}
	}

	c.BestNodesRWMutex.RLock()
	nodeAddr := c.BestNodes[nodeGroupID]
	if nodeAddr == "" {
		nodeAddr = c.BestNodes[c.MajorNodeGroup]
	}
	c.BestNodesRWMutex.RUnlock()
	if nodeAddr == "" {
		return nil, fmt.Errorf("no available aTrust node for group %q", nodeGroupID)
	}
	procName := "google-chrome-stable"
	procPath := "/usr/bin/google-chrome-stable"
	if addr.Port == 22 {
		procName = "ssh"
		procPath = "/usr/bin/ssh"
	}
	procHash := fmt.Sprintf("%X", sha256.Sum256([]byte(procPath)))

	destAddr := addr.String()
	if domain != "" {
		destAddr = fmt.Sprintf("%s:%d", domain, addr.Port)
	}

	destIP := addr.IP.To4()
	if destIP == nil {
		return nil, fmt.Errorf("invalid IPv4 address")
	}
	destPort := make([]byte, 2)
	binary.BigEndian.PutUint16(destPort, uint16(addr.Port))

	msg := fmt.Sprintf(
		`{"sid":"%s","appId":"%s","url":"tcp://%s","deviceId":"%s","connectionId":"%s","procHash":"%s","userName":"%s","rcAppliedInfo":0,"lang":"en-US","destAddr":"%s","env":{"application":{"runtime":{"process":{"name":"%s","digital_signature":"TrustAppClosed","platform":"Linux","fingerprint":"%s","description":"TrustAppClosed","path":"%s","version":"TrustAppClosed","security_env":"normal"},"process_trusted":"TRUSTED"}}},"xRequestSig":""}`,
		c.SID, appID, destAddr, c.DeviceID, c.ConnectionID, procHash, c.Username, destAddr, procName, procHash, procPath,
	)
	signKeyBytes, err := hex.DecodeString(c.SignKey)
	if err != nil {
		return nil, fmt.Errorf("invalid sign key: %w", err)
	}

	sig := calcXRequestSig(signKeyBytes, []byte(msg))
	msg = msg[:len(msg)-3] + `"` + sig + `"}`
	msgBytes := []byte(msg)
	msgLen := len(msgBytes)
	lenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBytes, uint16(msgLen))
	initHeader := []byte{0x05, 0x01, 0x81, 0x53, 0x03}
	initMsg := append(initHeader, lenBytes...)
	initMsg = append(initMsg, msgBytes...)
	var destMsg []byte
	if domain == "" {
		destHeader := []byte{0x05, 0x01, 0x01, 0x01}
		destMsg = append(destHeader, destIP...)
	} else {
		destHeader := []byte{0x05, 0x01, 0x01, 0x03}
		// For domain, we need to send the length of the domain name
		domainLen := len(domain)
		if domainLen > 255 {
			return nil, fmt.Errorf("domain name too long: %s", domain)
		}
		destHeader = append(destHeader, byte(domainLen))
		destMsg = append(destHeader, []byte(domain)...)
	}
	destMsg = append(destMsg, destPort...)

	lease, err := c.acquireTCPTunnelTransport(ctx, nodeAddr)
	if err != nil {
		return nil, err
	}
	tunnelConn, err := c.setupTCPTunnel(ctx, lease, initMsg, destMsg)
	if err == nil {
		return tunnelConn, nil
	}
	wasReused := lease.reused
	lease.Discard()
	if !wasReused {
		return nil, err
	}

	// An idle transport may have been closed by a relay or a network sleep.
	// Retry setup once on a fresh TLS connection before surfacing the error.
	freshLease, freshErr := c.dialNewTCPTunnelTransport(ctx, nodeAddr)
	if freshErr != nil {
		return nil, freshErr
	}
	tunnelConn, freshErr = c.setupTCPTunnel(ctx, freshLease, initMsg, destMsg)
	if freshErr != nil {
		freshLease.Discard()
		return nil, freshErr
	}
	return tunnelConn, nil
}
