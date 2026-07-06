package relay

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	hostrelay "github.com/Ruichen-0079/ACBH/agent/internal/relay"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
)

func (f *fakeClient) ListTunnelSessions(ctx context.Context, groupID string) ([]coordinatorclient.TunnelSession, *coreerrors.Error) {
	return f.sessions, f.sessionsErr
}

type fakeClient struct {
	bootstrapErr *coreerrors.Error
	ensureErr    *coreerrors.Error
	hbErr        *coreerrors.Error
	statusErr    *coreerrors.Error
	sessionsErr  *coreerrors.Error
	lease        coordinatorclient.HostLeaseStatus
	sessions     []coordinatorclient.TunnelSession

	bootstrapReq  coordinatorclient.BootstrapRequest
	ensureGroupID string
	ensureHostID  string
	ensureToken   string
	heartbeat     coordinatorclient.HeartbeatRequest
	heartbeatN    int
	mu            sync.Mutex
}

func (f *fakeClient) Bootstrap(ctx context.Context, accessToken string, req coordinatorclient.BootstrapRequest) (coordinatorclient.BootstrapResponse, *coreerrors.Error) {
	f.bootstrapReq = req
	if f.bootstrapErr != nil {
		return coordinatorclient.BootstrapResponse{}, f.bootstrapErr
	}
	return coordinatorclient.BootstrapResponse{OK: true, InstanceID: req.InstanceID, DeviceID: req.DeviceID, GroupID: "grp", HostID: "host"}, nil
}

func (f *fakeClient) EnsureActiveLease(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error) {
	f.ensureGroupID = groupID
	f.ensureHostID = hostID
	f.ensureToken = hostToken
	if f.ensureErr != nil {
		return coordinatorclient.EnsureActiveLeaseResponse{}, f.ensureErr
	}
	lease := f.lease
	lease.LeaseExpiresAt = time.Now().UTC().Add(45 * time.Second).Format(time.RFC3339)
	lease.LeaseValid = true
	lease.CurrentHostIDMatches = true
	return coordinatorclient.EnsureActiveLeaseResponse{OK: true, Renewed: true, Lease: lease}, nil
}

func (f *fakeClient) SendHeartbeat(ctx context.Context, req coordinatorclient.HeartbeatRequest) (coordinatorclient.HeartbeatResponse, *coreerrors.Error) {
	f.mu.Lock()
	f.heartbeat = req
	f.heartbeatN++
	f.mu.Unlock()
	if f.hbErr != nil {
		return coordinatorclient.HeartbeatResponse{}, f.hbErr
	}
	return coordinatorclient.HeartbeatResponse{OK: true, HostID: req.HostID, Status: req.Status}, nil
}

func (f *fakeClient) GetLeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.HostLeaseStatus, *coreerrors.Error) {
	if f.statusErr != nil {
		return coordinatorclient.HostLeaseStatus{}, f.statusErr
	}
	lease := f.lease
	lease.LeaseExpiresAt = time.Now().UTC().Add(45 * time.Second).Format(time.RFC3339)
	lease.LeaseValid = true
	lease.ServerTime = time.Now().UTC().Format(time.RFC3339)
	return lease, nil
}

func testLease() coordinatorclient.HostLeaseStatus {
	return coordinatorclient.HostLeaseStatus{
		CurrentHostID:        "host",
		CurrentHostIDMatches: true,
		LeaseValid:           true,
		LeaseExpiresAt:       time.Now().UTC().Add(45 * time.Second).Format(time.RFC3339),
		Generation:           1,
		ServerTime:           time.Now().UTC().Format(time.RFC3339),
		HeartbeatActive:      true,
	}
}

func testCoordIdentity() identity.CoordinatorIdentity {
	return identity.CoordinatorIdentity{GroupID: "grp", HostID: "host", HostToken: "ht", OwnerToken: "ht"}
}

func startTestRuntime(t *testing.T, client *fakeClient, prober TCPProber) *Runtime {
	t.Helper()
	cfg := testConfig()
	cfg.Relay.Enabled = true
	rt := NewRuntime(client, nil)
	rt.prober = prober
	rt.runPump = func(ctx context.Context, opts hostrelay.HostRelayOptions) error {
		<-ctx.Done()
		return ctx.Err()
	}
	rt.keepalive = &keepaliveClient{
		coordinatorURL: cfg.CoordinatorURL,
		groupID:        "grp",
		hostID:         "host",
		hostToken:      "ht",
		generation:     1,
	}
	rt.keepalive.mu.Lock()
	rt.keepalive.connected = true
	rt.keepalive.connectedAt = time.Now().UTC()
	rt.keepalive.lastSeenAt = time.Now().UTC()
	rt.keepalive.mu.Unlock()
	rt.Start(context.Background(), cfg, ConfigureRequest{}, testCoordIdentity(), 1)
	t.Cleanup(rt.Stop)
	return rt
}

func alwaysReachable(context.Context, string, time.Duration) bool { return true }

func TestActiveStability60s(t *testing.T) {
	if testing.Short() {
		t.Skip("stability test requires 60s")
	}
	coord := newE2ECoordinator(t)
	client := &fakeClient{lease: testLease()}
	rt := NewRuntime(client, coord.server.Client())
	rt.prober = alwaysReachable
	rt.runPump = func(ctx context.Context, opts hostrelay.HostRelayOptions) error {
		<-ctx.Done()
		return ctx.Err()
	}
	cfg := testConfig()
	cfg.CoordinatorURL = coord.server.URL
	cfg.Relay.Enabled = true
	rt.Start(context.Background(), cfg, ConfigureRequest{}, testCoordIdentity(), 1)
	t.Cleanup(rt.Stop)
	waitUntil(t, 10*time.Second, func() bool {
		state := rt.Snapshot(cfg, testLease())
		return state.TunnelConnected
	})

	var lastHeartbeatCount int
	for i := 0; i < 6; i++ {
		time.Sleep(10 * time.Second)
		state := rt.Snapshot(cfg, testLease())
		elapsed := (i + 1) * 10
		if !state.Active {
			t.Fatalf("t=%ds active=false reason=%s errors=%v", elapsed, state.LastDisconnectReason, state.Errors)
		}
		if state.LastHeartbeatAt == "" {
			t.Fatalf("t=%ds lastHeartbeatAt empty", elapsed)
		}
		client.mu.Lock()
		heartbeatCount := client.heartbeatN
		client.mu.Unlock()
		if i > 0 && heartbeatCount <= lastHeartbeatCount {
			t.Fatalf("t=%ds heartbeat count not refreshed: %d", elapsed, heartbeatCount)
		}
		lastHeartbeatCount = heartbeatCount
		if state.LeaseExpiresAt == "" || state.ActiveUntil == "" {
			t.Fatalf("t=%ds lease timing missing", elapsed)
		}
	}
	final := rt.Snapshot(cfg, testLease())
	t.Logf("active remained true for 60s; heartbeats=%d lastHeartbeatAt=%s leaseExpiresAt=%s activeUntil=%s",
		lastHeartbeatCount, final.LastHeartbeatAt, final.LeaseExpiresAt, final.ActiveUntil)
}

func TestNoTunnelClientReportsInactive(t *testing.T) {
	client := &fakeClient{lease: testLease()}
	rt := NewRuntime(client, nil)
	rt.prober = alwaysReachable
	cfg := testConfig()
	cfg.Relay.Enabled = true
	state := rt.Snapshot(cfg, testLease())
	if state.Active {
		t.Fatalf("expected inactive without runtime, got %#v", state)
	}
	if state.TunnelConnected {
		t.Fatal("expected tunnelConnected=false")
	}
	found := false
	for _, err := range state.Errors {
		if err.ErrorCode == coreerrors.RelayTunnelNotConnected {
			found = true
		}
	}
	if state.LastDisconnectReason != DisconnectTunnelNotConnected && !found {
		t.Fatalf("expected relay_tunnel_not_connected, got reason=%s errors=%v", state.LastDisconnectReason, state.Errors)
	}
}

func TestTunnelLoopExitMarksInactive(t *testing.T) {
	client := &fakeClient{lease: testLease()}
	rt := NewRuntime(client, nil)
	rt.prober = alwaysReachable
	rt.runPump = func(ctx context.Context, opts hostrelay.HostRelayOptions) error {
		return errors.New("forced tunnel exit")
	}
	rt.keepalive = &keepaliveClient{coordinatorURL: testConfig().CoordinatorURL, groupID: "grp", hostID: "host", hostToken: "ht", generation: 1}
	rt.keepalive.mu.Lock()
	rt.keepalive.connected = true
	rt.keepalive.connectedAt = time.Now().UTC()
	rt.keepalive.lastSeenAt = time.Now().UTC()
	rt.keepalive.mu.Unlock()
	cfg := testConfig()
	cfg.Relay.Enabled = true
	rt.Start(context.Background(), cfg, ConfigureRequest{}, testCoordIdentity(), 1)
	defer rt.Stop()

	client.sessions = []coordinatorclient.TunnelSession{{
		SessionID: "tun_test", GroupID: "grp", HostID: "host", Status: "pending", CurrentHostGeneration: 1,
	}}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := rt.Snapshot(cfg, testLease())
		if state.LastDisconnectReason == DisconnectTunnelExited || state.LastTunnelError != "" {
			if state.Active {
				t.Fatalf("expected inactive after tunnel exit, got %#v", state)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("tunnel exit reason not recorded")
}

func TestLocalMinecraftUnreachable(t *testing.T) {
	client := &fakeClient{lease: testLease()}
	rt := startTestRuntime(t, client, func(ctx context.Context, address string, timeout time.Duration) bool {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return false
		}
		if host == "127.0.0.1" && port == "25565" {
			return false
		}
		return defaultTCPProber(ctx, address, timeout)
	})
	rt.pingStatus = func(ctx context.Context, address string, timeout time.Duration) (bool, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return false, err
		}
		if host == "127.0.0.1" && port == "25565" {
			return false, nil
		}
		return true, nil
	}
	cfg := testConfig()
	cfg.Relay.Enabled = true
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state := rt.Snapshot(cfg, testLease())
		if !state.LocalMinecraftReachable {
			if state.Active {
				t.Fatalf("expected inactive when local mc unreachable: %#v", state)
			}
			found := false
			for _, err := range state.Errors {
				if err.ErrorCode == coreerrors.LocalMinecraftUnreachable {
					found = true
				}
			}
			if !found && state.LastDisconnectReason != DisconnectLocalMCUnreachable {
				t.Fatalf("expected local_minecraft_unreachable, got %#v", state)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("local minecraft reachability not updated")
}

func TestComputeActiveRequiresAllFields(t *testing.T) {
	base := State{
		Configured: true, CurrentHost: true, CurrentDevice: true,
		HeartbeatRunning: true, HeartbeatOK: true,
		LeaseRenewRunning: true, LeaseActive: true,
		TunnelConnected: true, SessionPumpRunning: true,
		PublicListenerReady: true, LocalMinecraftReachable: true,
	}
	if !computeActive(base) {
		t.Fatal("expected active")
	}
	base.TunnelConnected = false
	if computeActive(base) {
		t.Fatal("expected inactive without tunnel")
	}
}

func startLocalEcho(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, readErr := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if readErr != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}