package playerproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestPlayerSendsAuthHeaders(t *testing.T) {
	var mu sync.Mutex
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "token1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go proxy.Run(ctx)

	conn := dialWithRetry(t, listenAddr)
	conn.Close()

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if receivedHeaders.Get("X-ACBH-Player-ID") != "p1" {
		t.Errorf("expected X-ACBH-Player-ID=p1, got %s", receivedHeaders.Get("X-ACBH-Player-ID"))
	}
	if receivedHeaders.Get("X-ACBH-Player-Token") != "token1" {
		t.Errorf("expected X-ACBH-Player-Token=token1, got %s", receivedHeaders.Get("X-ACBH-Player-Token"))
	}

	cancel()
}

func TestForwardsTCPToWebSocket(t *testing.T) {
	server, _, wsConns := newPlayerRelayServer(t)
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go proxy.Run(ctx)

	clientConn := dialWithRetry(t, listenAddr)
	defer clientConn.Close()

	wsConn := <-wsConns
	defer wsConn.Close(websocket.StatusNormalClosure, "")

	_, err := clientConn.Write([]byte{0xAA, 0xBB, 0xCC})
	if err != nil {
		t.Fatalf("write to client conn: %v", err)
	}

	_, data, err := wsConn.Read(context.Background())
	if err != nil {
		t.Fatalf("read from ws relay: %v", err)
	}
	if !bytes.Equal(data, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("expected [AA BB CC], got %v", data)
	}

	cancel()
}

func TestForwardsWebSocketToTCP(t *testing.T) {
	server, _, wsConns := newPlayerRelayServer(t)
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go proxy.Run(ctx)

	clientConn := dialWithRetry(t, listenAddr)
	defer clientConn.Close()

	wsConn := <-wsConns
	defer wsConn.Close(websocket.StatusNormalClosure, "")

	err := wsConn.Write(context.Background(), websocket.MessageBinary, []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("write to ws relay: %v", err)
	}

	buf := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("read from client conn: %v", err)
	}
	if !bytes.Equal(buf[:n], []byte{0x01, 0x02}) {
		t.Errorf("expected [01 02], got %v", buf[:n])
	}

	cancel()
}

func TestMultipleFramesPreserveOrder(t *testing.T) {
	server, _, wsConns := newPlayerRelayServer(t)
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go proxy.Run(ctx)

	clientConn := dialWithRetry(t, listenAddr)
	defer clientConn.Close()

	wsConn := <-wsConns
	defer wsConn.Close(websocket.StatusNormalClosure, "")

	err := wsConn.Write(context.Background(), websocket.MessageBinary, []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("write frame 1: %v", err)
	}
	err = wsConn.Write(context.Background(), websocket.MessageBinary, []byte{0x03, 0x04, 0x05})
	if err != nil {
		t.Fatalf("write frame 2: %v", err)
	}
	err = wsConn.Write(context.Background(), websocket.MessageBinary, []byte{0x06})
	if err != nil {
		t.Fatalf("write frame 3: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var received []byte
	buf := make([]byte, 1024)
	for time.Now().Before(deadline) {
		n, readErr := clientConn.Read(buf)
		if n > 0 {
			received = append(received, buf[:n]...)
		}
		if readErr != nil {
			break
		}
		if len(received) >= 6 {
			break
		}
	}

	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	if !bytes.Equal(received, expected) {
		t.Errorf("expected %v, got %v", expected, received)
	}

	cancel()
}

func TestClosingTCPClosesWebSocket(t *testing.T) {
	server, _, wsConns := newPlayerRelayServer(t)
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go proxy.Run(ctx)

	clientConn := dialWithRetry(t, listenAddr)

	wsConn := <-wsConns

	clientConn.Close()

	time.Sleep(300 * time.Millisecond)

	_, _, err := wsConn.Read(context.Background())
	if err == nil {
		t.Error("expected websocket read to fail after tcp close")
	}

	wsConn.Close(websocket.StatusNormalClosure, "")
	cancel()
}

func TestClosingWebSocketClosesTCP(t *testing.T) {
	server, _, wsConns := newPlayerRelayServer(t)
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go proxy.Run(ctx)

	clientConn := dialWithRetry(t, listenAddr)

	wsConn := <-wsConns

	wsConn.Close(websocket.StatusNormalClosure, "done")

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tcp to close")
	default:
		buf := make([]byte, 1)
		clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, err := clientConn.Read(buf)
		if err == nil {
			t.Error("expected tcp read to fail after websocket close")
		}
		clientConn.Close()
	}

	cancel()
}

func TestContextCancellationStopsProxy(t *testing.T) {
	server, _, wsConns := newPlayerRelayServer(t)
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- proxy.Run(ctx)
	}()

	clientConn := dialWithRetry(t, listenAddr)
	defer clientConn.Close()

	wsConn := <-wsConns
	defer wsConn.Close(websocket.StatusNormalClosure, "")

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

func TestInvalidListenAddress(t *testing.T) {
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: "http://localhost:8080",
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  "not-a-valid-address",
	})

	err := proxy.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid listen address")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("expected listen error, got: %v", err)
	}
}

func TestInvalidCoordinatorURL(t *testing.T) {
	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: "invalid-url",
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "t1",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- proxy.Run(ctx)
	}()

	conn := dialWithRetry(t, listenAddr)
	defer conn.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error for bad coordinator URL")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for proxy to fail")
	}
}

func TestPlayerTokenNotInErrorStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	listenAddr := mustAvailablePort()
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: server.URL,
		GroupID:        "g1",
		SessionID:      "s1",
		PlayerID:       "p1",
		PlayerToken:    "secret_player_token",
		ListenAddress:  listenAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- proxy.Run(ctx)
	}()

	conn := dialWithRetry(t, listenAddr)
	defer conn.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on auth failure")
		}
		if strings.Contains(err.Error(), "secret_player_token") {
			t.Error("error message must not contain the player token")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for proxy to fail")
	}
}

func TestListenAddressIsConfigurable(t *testing.T) {
	tests := []struct {
		addr string
	}{
		{"127.0.0.1:25565"},
		{"127.0.0.1:25577"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			proxy := NewPlayerProxy(PlayerProxyOptions{
				ListenAddress: tt.addr,
			})
			if proxy.opts.ListenAddress != tt.addr {
				t.Errorf("expected %s, got %s", tt.addr, proxy.opts.ListenAddress)
			}
		})
	}
}

func TestDefaultBufferSize(t *testing.T) {
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: "http://localhost",
		GroupID:        "g",
		SessionID:      "s",
		PlayerID:       "p",
		PlayerToken:    "t",
		ListenAddress:  "127.0.0.1:25565",
	})
	if proxy.opts.BufferSize != defaultBufferSize {
		t.Errorf("expected default buffer size %d, got %d", defaultBufferSize, proxy.opts.BufferSize)
	}
}

func TestCustomBufferSize(t *testing.T) {
	proxy := NewPlayerProxy(PlayerProxyOptions{
		CoordinatorURL: "http://localhost",
		GroupID:        "g",
		SessionID:      "s",
		PlayerID:       "p",
		PlayerToken:    "t",
		ListenAddress:  "127.0.0.1:25565",
		BufferSize:     8192,
	})
	if proxy.opts.BufferSize != 8192 {
		t.Errorf("expected buffer size 8192, got %d", proxy.opts.BufferSize)
	}
}

func TestIsNormalShutdown(t *testing.T) {
	wrappedEOF := fmt.Errorf("relay: tcp connection closed by client: %w", io.EOF)
	tests := []struct {
		name   string
		err    error
		normal bool
	}{
		{"nil", nil, true},
		{"context.Canceled", context.Canceled, true},
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"net.ErrClosed", net.ErrClosed, true},
		{"wrapped io.EOF", wrappedEOF, true},
		{"random error", errors.New("something broke"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNormalShutdown(tt.err); got != tt.normal {
				t.Errorf("isNormalShutdown(%v) = %v, want %v", tt.err, got, tt.normal)
			}
		})
	}
}

func TestWSScheme(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"http://localhost:8080", "ws://localhost:8080"},
		{"https://localhost:8443", "wss://localhost:8443"},
		{"ftp://localhost", ""},
		{"localhost", ""},
	}
	for _, tt := range tests {
		result := wsScheme(tt.in)
		if result != tt.out {
			t.Errorf("wsScheme(%q) = %q, want %q", tt.in, result, tt.out)
		}
	}
}

//
// Test helpers
//

func mustAvailablePort() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("find free port: " + err.Error())
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func dialWithRetry(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			return conn
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout connecting to proxy at %s", addr)
	return nil
}

func newPlayerRelayServer(t *testing.T) (*httptest.Server, chan http.Header, chan *websocket.Conn) {
	t.Helper()
	headersCh := make(chan http.Header, 1)
	wsConns := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case headersCh <- r.Header.Clone():
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("websocket accept error: %v", err)
			return
		}
		wsConns <- conn
	}))
	t.Cleanup(server.Close)
	return server, headersCh, wsConns
}
