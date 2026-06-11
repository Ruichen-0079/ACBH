package relay

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestConnectsWithRequiredHostAuthHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := newRelayTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	})

	tcpAddr := startTCPEcho(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	go client.Run(context.Background())

	time.Sleep(200 * time.Millisecond)

	if receivedHeaders.Get("X-ACBH-Host-ID") != "host_1" {
		t.Errorf("expected X-ACBH-Host-ID=host_1, got %s", receivedHeaders.Get("X-ACBH-Host-ID"))
	}
	if receivedHeaders.Get("X-ACBH-Host-Token") != "token_1" {
		t.Errorf("expected X-ACBH-Host-Token=token_1, got %s", receivedHeaders.Get("X-ACBH-Host-Token"))
	}
	if receivedHeaders.Get("X-ACBH-Host-Generation") != "3" {
		t.Errorf("expected X-ACBH-Host-Generation=3, got %s", receivedHeaders.Get("X-ACBH-Host-Generation"))
	}
}

func TestRejectsOnWebSocketAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	tcpAddr := startTCPEcho(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	err := client.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on auth failure")
	}
	if !strings.Contains(err.Error(), "websocket") {
		t.Errorf("expected websocket-related error, got: %v", err)
	}
}

func TestForwardsWebSocketToTCP(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	tcpAddr := startTCPCapture(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	relayConn := <-connCh
	defer relayConn.Close(websocket.StatusNormalClosure, "")

	err := relayConn.Write(context.Background(), websocket.MessageBinary, []byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Fatalf("write to relay: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
}

func TestForwardsTCPToWebSocket(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	tcpAddr := startTCPEcho(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	relayConn := <-connCh
	defer relayConn.Close(websocket.StatusNormalClosure, "")

	relayConn.Write(context.Background(), websocket.MessageBinary, []byte{0xAA, 0xBB})

	time.Sleep(200 * time.Millisecond)

	_, data, err := relayConn.Read(context.Background())
	if err != nil {
		t.Fatalf("read from relay: %v", err)
	}
	if len(data) != 2 || data[0] != 0xAA || data[1] != 0xBB {
		t.Errorf("expected echo of [0xAA, 0xBB], got %v", data)
	}

	cancel()
}

func TestMultipleFramesPreserveOrder(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	tcpAddr := startTCPEcho(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	relayConn := <-connCh
	defer relayConn.Close(websocket.StatusNormalClosure, "")

	relayConn.Write(context.Background(), websocket.MessageBinary, []byte{0x01, 0x02})
	relayConn.Write(context.Background(), websocket.MessageBinary, []byte{0x03, 0x04, 0x05})
	relayConn.Write(context.Background(), websocket.MessageBinary, []byte{0x06})

	deadline := time.Now().Add(3 * time.Second)
	var received []byte
	for time.Now().Before(deadline) {
		_, data, err := relayConn.Read(context.Background())
		if err != nil {
			break
		}
		received = append(received, data...)
		if len(received) >= 6 {
			break
		}
	}

	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	if len(received) != len(expected) {
		t.Errorf("expected %v, got %v (len %d)", expected, received, len(received))
	}
	for i := range expected {
		if i >= len(received) {
			t.Errorf("missing byte %d: expected %d", i, expected[i])
			break
		}
		if received[i] != expected[i] {
			t.Errorf("byte %d: expected %d, got %d", i, expected[i], received[i])
		}
	}

	cancel()
}

func TestClosingWebSocketClosesTCP(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	tcpAddr, tcpConnCh := startTCPConnServer(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	relayConn := <-connCh

	relayConn.Close(websocket.StatusNormalClosure, "done")

	select {
	case tcpConn := <-tcpConnCh:
		buf := make([]byte, 1)
		tcpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, err := tcpConn.Read(buf)
		if err == nil {
			t.Error("expected tcp read to fail after websocket close")
		}
		tcpConn.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TCP connection to close")
	}

	cancel()
}

func TestClosingTCPClosesWebSocket(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	tcpAddr, tcpConnCh := startTCPConnServer(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	relayConn := <-connCh
	tcpConn := <-tcpConnCh

	tcpConn.Close()

	time.Sleep(300 * time.Millisecond)

	_, _, err := relayConn.Read(context.Background())
	if err == nil {
		t.Error("expected websocket read to fail after tcp close")
	}

	relayConn.Close(websocket.StatusNormalClosure, "")
	cancel()
}

func TestContextCancellationStopsRelay(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	tcpAddr, tcpConnCh := startTCPConnServer(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx)
	}()

	<-connCh
	<-tcpConnCh

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("run returned (expected after cancel): %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: run did not return after context cancellation")
	}
}

func TestTargetDialFailureClosesWebSocketAndReturnsError(t *testing.T) {
	server, connCh := newRelayAcceptServer(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  "127.0.0.1:19999",
	})

	err := client.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on target dial failure")
	}
	if !strings.Contains(err.Error(), "target dial") {
		t.Errorf("expected target dial error, got: %v", err)
	}

	select {
	case relayConn := <-connCh:
		time.Sleep(50 * time.Millisecond)
		_, _, readErr := relayConn.Read(context.Background())
		if readErr == nil {
			t.Error("expected websocket to be closed after target dial failure")
		}
		relayConn.Close(websocket.StatusNormalClosure, "")
	case <-time.After(2 * time.Second):
	}
}

func TestTokenNotInErrorStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Unauthorized","message":"Invalid host token"}`))
	}))
	defer server.Close()

	tcpAddr := startTCPEcho(t)
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: server.URL,
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "secret_token_value",
		HostGeneration: 3,
		TargetAddress:  tcpAddr,
	})

	err := client.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret_token_value") {
		t.Error("error message must not contain the host token")
	}
}

func TestInvalidTargetAddress(t *testing.T) {
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: "http://localhost:8080",
		GroupID:        "group_1",
		SessionID:      "tun_session",
		HostID:         "host_1",
		HostToken:      "token_1",
		HostGeneration: 3,
		TargetAddress:  "invalid-address",
	})

	err := client.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid target address")
	}
}

func TestDefaultBufferSize(t *testing.T) {
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: "http://localhost",
		GroupID:        "g",
		SessionID:      "s",
		HostID:         "h",
		HostToken:      "t",
		HostGeneration: 1,
		TargetAddress:  "127.0.0.1:25565",
	})
	if client.opts.BufferSize != defaultBufferSize {
		t.Errorf("expected default buffer size %d, got %d", defaultBufferSize, client.opts.BufferSize)
	}
}

func TestCustomBufferSize(t *testing.T) {
	client := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: "http://localhost",
		GroupID:        "g",
		SessionID:      "s",
		HostID:         "h",
		HostToken:      "t",
		HostGeneration: 1,
		TargetAddress:  "127.0.0.1:25565",
		BufferSize:     8192,
	})
	if client.opts.BufferSize != 8192 {
		t.Errorf("expected buffer size 8192, got %d", client.opts.BufferSize)
	}
}

func TestWSSchemeHTTP(t *testing.T) {
	result := wsScheme("http://localhost:8080")
	if result != "ws://localhost:8080" {
		t.Errorf("expected ws://, got %s", result)
	}
}

func TestWSSchemeHTTPS(t *testing.T) {
	result := wsScheme("https://localhost:8443")
	if result != "wss://localhost:8443" {
		t.Errorf("expected wss://, got %s", result)
	}
}

func TestWSSchemeInvalid(t *testing.T) {
	result := wsScheme("invalid-url")
	if result != "" {
		t.Errorf("expected empty string for invalid URL, got %s", result)
	}
}

//
// Test helpers
//

func newRelayTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
	}))
}

func newRelayAcceptServer(t *testing.T) (*httptest.Server, chan *websocket.Conn) {
	t.Helper()
	connCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("websocket accept error: %v", err)
			return
		}
		connCh <- conn
	}))
	t.Cleanup(server.Close)
	return server, connCh
}

func startTCPEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 65536)
				for {
					n, readErr := conn.Read(buf)
					if n > 0 {
						conn.Write(buf[:n])
					}
					if readErr != nil {
						return
					}
				}
			}()
		}
	}()

	return ln.Addr().String()
}

func startTCPConnServer(t *testing.T) (string, chan net.Conn) {
	t.Helper()
	connCh := make(chan net.Conn, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		connCh <- conn
	}()

	return ln.Addr().String(), connCh
}

func startTCPCapture(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 65536)
				for {
					_, readErr := conn.Read(buf)
					if readErr != nil {
						return
					}
				}
			}()
		}
	}()

	return ln.Addr().String()
}
