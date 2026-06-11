package relay

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/Ruichen-0079/ACBH/agent/internal/playerproxy"
)

func TestRelayE2ESmoke(t *testing.T) {
	relayServer, hostCh, playerCh := newRelayPairServer(t)

	echoAddr := startTCPEcho(t)

	hostClient := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "e2e",
		SessionID:      "e2e-session",
		HostID:         "host-1",
		HostToken:      "host-token-1",
		HostGeneration: 1,
		TargetAddress:  echoAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostDone := make(chan error, 1)
	go func() {
		hostDone <- hostClient.Run(ctx)
	}()

	hostWS := <-hostCh
	defer hostWS.Close(websocket.StatusNormalClosure, "")

	listenAddr := mustAvailablePort(t)
	proxy := playerproxy.NewPlayerProxy(playerproxy.PlayerProxyOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "e2e",
		SessionID:      "e2e-session",
		PlayerID:       "player-1",
		PlayerToken:    "player-token-1",
		ListenAddress:  listenAddr,
	})

	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- proxy.Run(ctx)
	}()

	clientConn := dialWithRetry(t, listenAddr)
	defer clientConn.Close()

	playerWS := <-playerCh
	defer playerWS.Close(websocket.StatusNormalClosure, "")

	var relayOnce sync.Once
	closeRelay := func() {
		relayOnce.Do(func() {
			hostWS.Close(websocket.StatusNormalClosure, "")
			playerWS.Close(websocket.StatusNormalClosure, "")
		})
	}

	go func() {
		defer closeRelay()
		for {
			_, data, err := hostWS.Read(ctx)
			if err != nil {
				return
			}
			if err := playerWS.Write(ctx, websocket.MessageBinary, data); err != nil {
				return
			}
		}
	}()

	go func() {
		defer closeRelay()
		for {
			_, data, err := playerWS.Read(ctx)
			if err != nil {
				return
			}
			if err := hostWS.Write(ctx, websocket.MessageBinary, data); err != nil {
				return
			}
		}
	}()

	sendAndExpect := func(data []byte) {
		t.Helper()
		_, err := clientConn.Write(data)
		if err != nil {
			t.Fatalf("write to proxy: %v", err)
		}
		buf := make([]byte, 1024)
		clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, err := clientConn.Read(buf)
		if err != nil {
			t.Fatalf("read from proxy: %v", err)
		}
		if !bytes.Equal(buf[:n], data) {
			t.Errorf("expected %v, got %v", data, buf[:n])
		}
	}

	sendAndExpect([]byte{0x01, 0x02, 0x03, 0x04})

	frames := [][]byte{{0xAA}, {0xBB, 0xCC}, {0xDD, 0xEE, 0xFF}}
	for _, f := range frames {
		clientConn.Write(f)
	}
	var received []byte
	buf := make([]byte, 1024)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
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
	expected := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	if !bytes.Equal(received, expected) {
		t.Errorf("multiple frames: expected %v, got %v (len %d)", expected, received, len(received))
	}

	cancel()

	select {
	case err := <-hostDone:
		if err != nil {
			t.Logf("host run returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for host relay to stop")
	}

	select {
	case err := <-proxyDone:
		if err != nil {
			t.Logf("proxy run returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for player proxy to stop")
	}
}

func TestRelayE2ENoSecretsInErrors(t *testing.T) {
	relayServer, hostCh, playerCh := newRelayPairServer(t)

	echoAddr := startTCPEcho(t)

	hostClient := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "e2e",
		SessionID:      "e2e-secret",
		HostID:         "host-1",
		HostToken:      "super-secret-host-token",
		HostGeneration: 1,
		TargetAddress:  echoAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostDone := make(chan error, 1)
	go func() {
		hostDone <- hostClient.Run(ctx)
	}()

	<-hostCh

	proxy := playerproxy.NewPlayerProxy(playerproxy.PlayerProxyOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "e2e",
		SessionID:      "e2e-secret",
		PlayerID:       "player-1",
		PlayerToken:    "super-secret-player-token",
		ListenAddress:  mustAvailablePort(t),
	})

	go proxy.Run(ctx)

	clientConn := dialWithRetry(t, proxy.ListenAddress())
	clientConn.Close()

	<-playerCh

	cancel()

	hostErr := <-hostDone

	if hostErr != nil {
		errStr := hostErr.Error()
		if strings.Contains(errStr, "super-secret-host-token") {
			t.Error("host error message must not contain the host token")
		}
	}
}

func TestRelayE2ECancellationNoHang(t *testing.T) {
	relayServer, hostCh, playerCh := newRelayPairServer(t)

	echoAddr := startTCPEcho(t)

	hostClient := NewHostRelayClient(HostRelayOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "e2e",
		SessionID:      "e2e-cancel",
		HostID:         "host-1",
		HostToken:      "host-token-1",
		HostGeneration: 1,
		TargetAddress:  echoAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- hostClient.Run(ctx)
	}()

	<-hostCh

	proxy := playerproxy.NewPlayerProxy(playerproxy.PlayerProxyOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "e2e",
		SessionID:      "e2e-cancel",
		PlayerID:       "player-1",
		PlayerToken:    "player-token-1",
		ListenAddress:  mustAvailablePort(t),
	})

	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- proxy.Run(ctx)
	}()

	clientConn := dialWithRetry(t, proxy.ListenAddress())
	defer clientConn.Close()

	<-playerCh

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: host relay did not return after context cancellation")
	}

	select {
	case err := <-proxyDone:
		if err != nil {
			t.Logf("proxy run returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: player proxy did not return after context cancellation")
	}
}

//
// E2E test helpers
//

func newRelayPairServer(t *testing.T) (*httptest.Server, chan *websocket.Conn, chan *websocket.Conn) {
	t.Helper()
	hostCh := make(chan *websocket.Conn, 1)
	playerCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if strings.Contains(r.URL.Path, "/host") {
			hostCh <- conn
		} else {
			playerCh <- conn
		}
	}))
	t.Cleanup(server.Close)
	return server, hostCh, playerCh
}

func mustAvailablePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
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
	t.Fatalf("timeout connecting to %s", addr)
	return nil
}
