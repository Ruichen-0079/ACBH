package relay

import (
	"bytes"
	"context"
	"encoding/json"
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

	hostrelay "github.com/Ruichen-0079/ACBH/agent/internal/relay"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
)

type e2eCoordinator struct {
	server    *httptest.Server
	mu        sync.Mutex
	sessions  []coordinatorclient.TunnelSession
	leaseTime time.Time
	pairs     map[string]chan *websocket.Conn
}

func newE2ECoordinator(t *testing.T) *e2eCoordinator {
	t.Helper()
	c := &e2eCoordinator{
		leaseTime: time.Now().UTC(),
		pairs:     map[string]chan *websocket.Conn{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "protocolVersion": 2})
	})
	mux.HandleFunc("/v1/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "groupId": "grp", "hostId": "host"})
	})
	mux.HandleFunc("/v1/groups/grp/lease/ensure-active", func(w http.ResponseWriter, r *http.Request) {
		c.leaseTime = time.Now().UTC().Add(45 * time.Second)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "renewed": true,
			"lease": leasePayload(c.leaseTime),
		})
	})
	mux.HandleFunc("/v1/groups/grp/lease/status", func(w http.ResponseWriter, r *http.Request) {
		c.leaseTime = time.Now().UTC().Add(45 * time.Second)
		_ = json.NewEncoder(w).Encode(leasePayload(c.leaseTime))
	})
	mux.HandleFunc("/v1/hosts/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "hostId": "host", "status": "hosting"})
	})
	mux.HandleFunc("/v1/groups/grp/tunnel-sessions", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(c.sessions)
	})
	mux.HandleFunc("/v1/groups/grp/relay/clients/host", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		go holdWS(conn)
	})
	mux.HandleFunc("/v1/groups/grp/relay/tunnel-sessions/", func(w http.ResponseWriter, r *http.Request) {
		sessionID := extractSessionID(r.URL.Path)
		if sessionID == "" {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/host") {
			c.acceptRelaySide(t, sessionID, "host", w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/player") {
			c.acceptRelaySide(t, sessionID, "player", w, r)
			return
		}
		http.NotFound(w, r)
	})
	c.server = httptest.NewServer(mux)
	t.Cleanup(c.server.Close)
	return c
}

func leasePayload(expires time.Time) map[string]any {
	return map[string]any{
		"currentHostId": "host", "currentHostIdMatches": true, "leaseValid": true,
		"leaseExpiresAt": expires.Format(time.RFC3339), "generation": 1,
		"serverTime": time.Now().UTC().Format(time.RFC3339),
	}
}

func extractSessionID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if part == "tunnel-sessions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (c *e2eCoordinator) acceptRelaySide(t *testing.T, sessionID, side string, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	key := sessionID + ":" + side
	c.mu.Lock()
	ch := c.pairs[key]
	if ch == nil {
		ch = make(chan *websocket.Conn, 1)
		c.pairs[key] = ch
	}
	c.mu.Unlock()
	select {
	case ch <- conn:
	case <-time.After(2 * time.Second):
		t.Logf("timeout queueing %s websocket for %s", side, sessionID)
		_ = conn.Close(websocket.StatusInternalError, "pair timeout")
		return
	}
	c.mu.Lock()
	hostCh := c.pairs[sessionID+":host"]
	playerCh := c.pairs[sessionID+":player"]
	c.mu.Unlock()
	if hostCh != nil && playerCh != nil {
		select {
		case hostWS := <-hostCh:
			select {
			case playerWS := <-playerCh:
				go bridgeWS(hostWS, playerWS)
			default:
				hostCh <- hostWS
			}
		default:
		}
	}
}

func holdWS(conn *websocket.Conn) {
	defer conn.Close(websocket.StatusNormalClosure, "")
	for {
		if _, _, err := conn.Read(context.Background()); err != nil {
			return
		}
	}
}

func bridgeWS(a, b *websocket.Conn) {
	ctx := context.Background()
	errCh := make(chan error, 2)
	pump := func(src, dst *websocket.Conn) {
		for {
			_, data, err := src.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			if err := dst.Write(ctx, websocket.MessageBinary, data); err != nil {
				errCh <- err
				return
			}
		}
	}
	go pump(a, b)
	go pump(b, a)
	<-errCh
	_ = a.Close(websocket.StatusNormalClosure, "")
	_ = b.Close(websocket.StatusNormalClosure, "")
}

func TestTCPEchoEndToEnd(t *testing.T) {
	coord := newE2ECoordinator(t)
	echoAddr, stopEcho := startLocalEcho(t)
	defer stopEcho()

	host, port, err := parseHostPort(echoAddr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.CoordinatorURL = coord.server.URL
	cfg.Listener.LocalHost = host
	cfg.Listener.LocalPort = port
	cfg.Relay.PublicHost = "127.0.0.1"
	cfg.Relay.MinecraftPort = mustFreePort(t)

	client, clientErr := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, coord.server.Client())
	if clientErr != nil {
		t.Fatal(clientErr)
	}
	svc := &Service{Client: client, HTTPClient: coord.server.Client()}
	if _, cfgErr := svc.Configure(context.Background(), cfg, ConfigureRequest{LocalMinecraftHost: host, LocalMinecraftPort: port}); cfgErr != nil {
		t.Fatal(cfgErr)
	}
	rt := svc.runtimeOrCreate()
	rt.prober = alwaysReachable

	coord.mu.Lock()
	coord.sessions = []coordinatorclient.TunnelSession{{
		SessionID: "tun_echo", GroupID: "grp", HostID: "host", Status: "pending", CurrentHostGeneration: 1,
	}}
	coord.mu.Unlock()

	waitUntil(t, 8*time.Second, func() bool {
		state, statusErr := svc.Status(context.Background(), cfg)
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		return state.TunnelConnected && state.SessionPumpRunning
	})

	playerURL := strings.Replace(coord.server.URL, "http://", "ws://", 1) + "/v1/groups/grp/relay/tunnel-sessions/tun_echo/player"
	playerWS, _, err := websocket.Dial(context.Background(), playerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer playerWS.Close(websocket.StatusNormalClosure, "")

	time.Sleep(300 * time.Millisecond)
	payload := []byte("echo-payload-12345")
	if err := playerWS.Write(context.Background(), websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := playerWS.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("echo mismatch: got %q want %q", data, payload)
	}
}

func TestSessionPumpUsesHostRelayClient(t *testing.T) {
	coord := newE2ECoordinator(t)
	echoAddr, stopEcho := startLocalEcho(t)
	defer stopEcho()

	var pumpTarget string
	rt := NewRuntime(nil, coord.server.Client())
	rt.prober = alwaysReachable
	rt.runPump = func(ctx context.Context, opts hostrelay.HostRelayOptions) error {
		pumpTarget = opts.TargetAddress
		return hostrelay.NewHostRelayClient(opts).Run(ctx)
	}
	rt.keepalive = &keepaliveClient{coordinatorURL: coord.server.URL, groupID: "grp", hostID: "host", hostToken: "ht", generation: 1}
	rt.keepalive.mu.Lock()
	rt.keepalive.connected = true
	rt.keepalive.mu.Unlock()

	cfg := testConfig()
	cfg.CoordinatorURL = coord.server.URL
	host, port, err := parseHostPort(echoAddr)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Listener.LocalHost = host
	cfg.Listener.LocalPort = port
	rt.Start(context.Background(), cfg, ConfigureRequest{LocalMinecraftHost: host, LocalMinecraftPort: port}, testCoordIdentity(), 1)
	defer rt.Stop()

	coord.mu.Lock()
	coord.sessions = []coordinatorclient.TunnelSession{{
		SessionID: "tun_pump", GroupID: "grp", HostID: "host", Status: "pending", CurrentHostGeneration: 1,
	}}
	coord.mu.Unlock()

	waitUntil(t, 6*time.Second, func() bool { return pumpTarget == echoAddr })
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func mustFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func parseHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	return host, port, err
}

func TestMinecraftSmokeEchoFromPublicPath(t *testing.T) {
	coord := newE2ECoordinator(t)
	echoAddr, stopEcho := startLocalEcho(t)
	defer stopEcho()
	host, port, err := parseHostPort(echoAddr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.CoordinatorURL = coord.server.URL
	client, clientErr := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, coord.server.Client())
	if clientErr != nil {
		t.Fatal(clientErr)
	}
	svc := &Service{Client: client, HTTPClient: coord.server.Client()}
	if _, cfgErr := svc.Configure(context.Background(), cfg, ConfigureRequest{LocalMinecraftHost: host, LocalMinecraftPort: port}); cfgErr != nil {
		t.Fatal(cfgErr)
	}
	svc.runtimeOrCreate().prober = alwaysReachable

	coord.mu.Lock()
	coord.sessions = []coordinatorclient.TunnelSession{{
		SessionID: "tun_mc", GroupID: "grp", HostID: "host", Status: "pending", CurrentHostGeneration: 1,
	}}
	coord.mu.Unlock()

	playerURL := strings.Replace(coord.server.URL, "http://", "ws://", 1) + "/v1/groups/grp/relay/tunnel-sessions/tun_mc/player"
	waitUntil(t, 8*time.Second, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		ws, _, dialErr := websocket.Dial(ctx, playerURL, nil)
		if dialErr != nil {
			return false
		}
		_ = ws.Close(websocket.StatusNormalClosure, "")
		return true
	})

	playerWS, _, err := websocket.Dial(context.Background(), playerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer playerWS.Close(websocket.StatusNormalClosure, "")
	handshake := []byte{0x10, 0x00, 0x03}
	if err := playerWS.Write(context.Background(), websocket.MessageBinary, handshake); err != nil {
		t.Fatal(err)
	}
	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := playerWS.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, handshake) {
		t.Fatalf("handshake echo mismatch: %v vs %v", data, handshake)
	}
}

func TestReadFullHelper(t *testing.T) {
	// keep io.ReadFull referenced for future TCP-level tests
	buf := make([]byte, 3)
	r := strings.NewReader("abc")
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
}