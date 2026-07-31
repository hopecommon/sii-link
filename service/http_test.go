package service

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestWriteHTTPResponseCopiesAndClosesBody(t *testing.T) {
	body := &closeTrackingBody{Reader: bytes.NewBufferString("response body")}
	response := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{"X-Test": {"value"}},
		Body:       body,
	}
	recorder := httptest.NewRecorder()

	if err := writeHTTPResponse(recorder, response); err != nil {
		t.Fatalf("writeHTTPResponse() error = %v", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if recorder.Code != http.StatusCreated || recorder.Header().Get("X-Test") != "value" || recorder.Body.String() != "response body" {
		t.Fatalf("response = code %d headers %v body %q", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestBridgeHTTPConnectCopiesBothDirectionsAndCloses(t *testing.T) {
	clientConn, clientPeer := net.Pipe()
	serverConn, serverPeer := net.Pipe()
	done := make(chan struct{})
	go func() {
		bridgeHTTPConnect(clientConn, clientConn, serverConn)
		close(done)
	}()

	assertPipeTransfer(t, clientPeer, serverPeer, []byte("client request"))
	assertPipeTransfer(t, serverPeer, clientPeer, []byte("server response"))

	_ = clientPeer.Close()
	_ = serverPeer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bridgeHTTPConnect did not close after both peers disconnected")
	}
}

func assertPipeTransfer(t *testing.T, src net.Conn, dst net.Conn, want []byte) {
	t.Helper()
	writeDone := make(chan error, 1)
	go func() {
		_, err := src.Write(want)
		writeDone <- err
	}()

	got := make([]byte, len(want))
	if _, err := io.ReadFull(dst, got); err != nil {
		t.Fatalf("read transferred data: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write transferred data: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("transferred data = %q, want %q", got, want)
	}
}
