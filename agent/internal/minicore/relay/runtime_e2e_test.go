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

func TestMinecraftStatusPingEndToEnd(t *testing.T) {
	coord := newE2ECoordinator(t)
	mcAddr, stopMC, err := startMockMinecraftStatusServer()
	if err != nil {
		t.Fatal(err)
	}
	defer stopMC()

	host, port, err := mustSplitHostPort(mcAddr)
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
	rt.pingStatus = pingMinecraftStatus

	coord.mu.Lock()
	coord.sessions = []coordinatorclient.TunnelSession{{
		SessionID: "tun_mc", GroupID: "grp", HostID: "host", Status: "pending", CurrentHostGeneration: 1,
	}}
	coord.mu.Unlock()

	waitUntil(t, 8*time.Second, func() bool {
		state, statusErr := svc.Status(context.Background(), cfg)
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		return state.TunnelConnected && state.SessionPumpRunning
	})

	playerURL := strings.Replace(coord.server.URL, "http://", "ws://", 1) + "/v1/groups/grp/relay/tunnel-sessions/tun_mc/player"
	playerWS, _, err := websocket.Dial(context.Background(), playerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer playerWS.Close(websocket.StatusNormalClosure, "")

	time.Sleep(300 * time.Millisecond)

	hostAddr, hostPort, err := mustSplitHostPort(mcAddr)
	if err != nil {
		t.Fatal(err)
	}
	handshake := buildHandshakePacket(hostAddr, uint16(hostPort), 1)
	if err := playerWS.Write(context.Background(), websocket.MessageBinary, handshake); err != nil {
		t.Fatal(err)
	}
	if err := playerWS.Write(context.Background(), websocket.MessageBinary, buildPacket([]byte{0x00})); err != nil {
		t.Fatal(err)
	}

	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := playerWS.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := readPacketFromBuffer(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) < 2 || packet[0] != 0x00 {
		t.Fatalf("unexpected status packet: %v", packet)
	}
	jsonPayload, err := readMCString(packet[1:])
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(jsonPayload), &status); err != nil {
		t.Fatal(err)
	}
	if _, ok := status["version"]; !ok {
		t.Fatalf("status json missing version: %s", jsonPayload)
	}
}

func TestSessionPumpRecordsDiagnostics(t *testing.T) {
	coord := newE2ECoordinator(t)
	mcAddr, stopMC, err := startMockMinecraftStatusServer()
	if err != nil {
		t.Fatal(err)
	}
	defer stopMC()

	host, port, err := mustSplitHostPort(mcAddr)
	if err != nil {
		t.Fatal(err)
	}

	rt := NewRuntime(nil, coord.server.Client())
	rt.prober = alwaysReachable
	rt.pingStatus = func(context.Context, string, time.Duration) (bool, error) { return true, nil }
	rt.runPump = func(ctx context.Context, opts hostrelay.HostRelayOptions) error {
		return hostrelay.NewHostRelayClient(opts).Run(ctx)
	}
	rt.keepalive = &keepaliveClient{coordinatorURL: coord.server.URL, groupID: "grp", hostID: "host", hostToken: "ht", generation: 1}
	rt.keepalive.mu.Lock()
	rt.keepalive.connected = true
	rt.keepalive.mu.Unlock()

	cfg := testConfig()
	cfg.CoordinatorURL = coord.server.URL
	cfg.Listener.LocalHost = host
	cfg.Listener.LocalPort = port
	rt.Start(context.Background(), cfg, ConfigureRequest{LocalMinecraftHost: host, LocalMinecraftPort: port}, testCoordIdentity(), 1)
	defer rt.Stop()

	coord.mu.Lock()
	coord.sessions = []coordinatorclient.TunnelSession{{
		SessionID: "tun_diag", GroupID: "grp", HostID: "host", Status: "pending", CurrentHostGeneration: 1,
	}}
	coord.mu.Unlock()

	playerURL := strings.Replace(coord.server.URL, "http://", "ws://", 1) + "/v1/groups/grp/relay/tunnel-sessions/tun_diag/player"
	waitUntil(t, 6*time.Second, func() bool {
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
	handshake := buildHandshakePacket(host, uint16(port), 1)
	_ = playerWS.Write(context.Background(), websocket.MessageBinary, handshake)
	_ = playerWS.Write(context.Background(), websocket.MessageBinary, buildPacket([]byte{0x00}))
	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, _ = playerWS.Read(readCtx)
	cancel()
	playerWS.Close(websocket.StatusNormalClosure, "")

	waitUntil(t, 6*time.Second, func() bool {
		state := rt.Snapshot(cfg, testLease())
		for _, session := range state.RecentSessions {
			if session.SessionID == "tun_diag" && session.BytesPlayerToLocal > 0 && session.BytesLocalToPlayer > 0 {
				return true
			}
		}
		return false
	})
}

func TestSessionPumpUsesHostRelayClient(t *testing.T) {
	coord := newE2ECoordinator(t)
	mcAddr, stopMC, err := startMockMinecraftStatusServer()
	if err != nil {
		t.Fatal(err)
	}
	defer stopMC()

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
	host, port, err := mustSplitHostPort(mcAddr)
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

	waitUntil(t, 6*time.Second, func() bool { return pumpTarget == mcAddr })
}

func TestMinecraftStatusPingLocal(t *testing.T) {
	mcAddr, stopMC, err := startMockMinecraftStatusServer()
	if err != nil {
		t.Fatal(err)
	}
	defer stopMC()
	ok, err := pingMinecraftStatus(context.Background(), mcAddr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected local mock minecraft status ping to succeed")
	}
}

func readPacketFromBuffer(data []byte) ([]byte, error) {
	reader := bytes.NewReader(data)
	length, err := readVarIntFromReader(reader)
	if err != nil {
		return nil, err
	}
	if length <= 0 || length > len(data) {
		return nil, fmt.Errorf("invalid packet length %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
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