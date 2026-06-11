package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"nhooyr.io/websocket"

	"github.com/Ruichen-0079/ACBH/agent/internal/playerproxy"
	"github.com/Ruichen-0079/ACBH/agent/internal/relay"
)

func main() {
	hostTarget := flag.String("host-target", "", "Host relay target address (default auto)")
	playerListen := flag.String("player-listen", "", "Player listen address (default auto)")
	timeout := flag.Duration("timeout", 10*time.Second, "Demo timeout")
	flag.Parse()

	log.SetFlags(0)
	log.Println("=== ACBH Relay-Only Demo ===")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runDemo(ctx, *hostTarget, *playerListen)
	}()

	select {
	case err := <-resultCh:
		if err != nil {
			log.Printf("FAIL: %v", err)
			os.Exit(1)
		}
		log.Println("PASS")
	case <-time.After(*timeout):
		stop()
		<-resultCh
		log.Fatal("FAIL: demo timed out")
	}
}

func runDemo(ctx context.Context, hostTarget, playerListen string) error {
	log.Println("1. Starting in-memory relay server...")
	relayServer := startRelayServer()
	defer relayServer.Close()
	log.Printf("   Relay: %s", relayServer.URL)

	log.Println("2. Starting TCP echo server (simulates Minecraft server or Velocity)...")
	echoAddr := startEchoServer()
	log.Printf("   Echo: %s", echoAddr)

	if hostTarget == "" {
		hostTarget = echoAddr
	}

	log.Println("3. Starting Host relay client...")
	hostClient := relay.NewHostRelayClient(relay.HostRelayOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "demo-group",
		SessionID:      "demo-session",
		HostID:         "demo-host",
		HostToken:      "demo-host-token",
		HostGeneration: 1,
		TargetAddress:  hostTarget,
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	hostErrCh := make(chan error, 1)
	go func() { hostErrCh <- hostClient.Run(ctx) }()

	waitForConn(300 * time.Millisecond)

	log.Println("4. Starting Player local proxy...")
	if playerListen == "" {
		playerListen = mustAvailablePort()
	}
	proxy := playerproxy.NewPlayerProxy(playerproxy.PlayerProxyOptions{
		CoordinatorURL: relayServer.URL,
		GroupID:        "demo-group",
		SessionID:      "demo-session",
		PlayerID:       "demo-player",
		PlayerToken:    "demo-player-token",
		ListenAddress:  playerListen,
	})
	log.Printf("   Player listen: %s", playerListen)

	proxyErrCh := make(chan error, 1)
	go func() { proxyErrCh <- proxy.Run(ctx) }()

	waitForConn(200 * time.Millisecond)

	log.Println("5. Connecting test client...")
	clientConn, err := net.DialTimeout("tcp", playerListen, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial player proxy: %w", err)
	}
	defer clientConn.Close()

	waitForConn(100 * time.Millisecond)

	log.Println("6. Testing single write echo...")
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	if _, err := clientConn.Write(payload); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	buffer := make([]byte, 1024)
	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := clientConn.Read(buffer)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if !bytesEqual(buffer[:n], payload) {
		return fmt.Errorf("single echo: expected %v, got %v", payload, buffer[:n])
	}
	log.Printf("   OK: echoed %v", buffer[:n])

	log.Println("7. Testing multi-frame ordering...")
	frames := [][]byte{{0xAA}, {0xBB, 0xCC}, {0xDD, 0xEE, 0xFF}, {0x11, 0x22}}
	for _, f := range frames {
		if _, err := clientConn.Write(f); err != nil {
			return fmt.Errorf("multi write: %w", err)
		}
	}
	var received []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		m, readErr := clientConn.Read(buffer)
		if m > 0 {
			received = append(received, buffer[:m]...)
		}
		if readErr != nil {
			break
		}
		if len(received) >= 8 {
			break
		}
	}
	expected := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}
	if !bytesEqual(received, expected) {
		return fmt.Errorf("multi-frame: expected %v, got %v (len %d)", expected, received, len(received))
	}
	log.Printf("   OK: echoed %v", received)

	log.Println("8. Shutting down...")
	cancel()

	select {
	case err := <-hostErrCh:
		if err != nil {
			log.Printf("   Host relay exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		return fmt.Errorf("host relay did not stop")
	}

	select {
	case err := <-proxyErrCh:
		if err != nil {
			log.Printf("   Player proxy exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		return fmt.Errorf("player proxy did not stop")
	}

	return nil
}

func startRelayServer() *httptest.Server {
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

	go pairRelay(hostCh, playerCh)

	return server
}

func pairRelay(hostCh, playerCh <-chan *websocket.Conn) {
	hostConn := <-hostCh
	playerConn := <-playerCh

	var once sync.Once
	closePair := func() {
		once.Do(func() {
			hostConn.Close(websocket.StatusNormalClosure, "")
			playerConn.Close(websocket.StatusNormalClosure, "")
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer closePair()
		for {
			_, data, err := hostConn.Read(context.Background())
			if err != nil {
				return
			}
			if err := playerConn.Write(context.Background(), websocket.MessageBinary, data); err != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer closePair()
		for {
			_, data, err := playerConn.Read(context.Background())
			if err != nil {
				return
			}
			if err := hostConn.Write(context.Background(), websocket.MessageBinary, data); err != nil {
				return
			}
		}
	}()

	wg.Wait()
}

func startEchoServer() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

func mustAvailablePort() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func waitForConn(d time.Duration) {
	time.Sleep(d)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
