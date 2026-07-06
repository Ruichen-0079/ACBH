package relay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	hostrelay "github.com/Ruichen-0079/ACBH/agent/internal/relay"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
)

type TCPProber func(ctx context.Context, address string, timeout time.Duration) bool

type SessionPumpRunner func(ctx context.Context, opts hostrelay.HostRelayOptions) error

type Runtime struct {
	client     Client
	httpClient *http.Client
	prober     TCPProber
	runPump    SessionPumpRunner
	pingStatus MCPingFunc

	mu              sync.RWMutex
	running         bool
	cancel          context.CancelFunc
	cfg             coreconfig.Config
	req             ConfigureRequest
	coordIdentity   identity.CoordinatorIdentity
	generation      int
	keepalive       *keepaliveClient
	activeSessions  map[string]context.CancelFunc
	sessionPumpLive bool

	heartbeatRunning        bool
	heartbeatOK             bool
	lastHeartbeatAt         string
	leaseRenewRunning       bool
	leaseActive             bool
	leaseExpiresAt          string
	currentHost             bool
	currentDevice           bool
	publicListenerReady     bool
	localMinecraftReachable bool
	publicMinecraftPingOk   bool
	lastPublicPingAt        time.Time
	recentSessions          []SessionDiagnostic
	lastTunnelError         string
	lastDisconnectReason    string
	errors                  []*coreerrors.Error
}

type MCPingFunc func(ctx context.Context, address string, timeout time.Duration) (bool, error)

func NewRuntime(client Client, httpClient *http.Client) *Runtime {
	return &Runtime{
		client:         client,
		httpClient:     httpClient,
		prober:         defaultTCPProber,
		runPump:        defaultSessionPump,
		pingStatus:     pingMinecraftStatus,
		activeSessions: map[string]context.CancelFunc{},
	}
}

func defaultTCPProber(ctx context.Context, address string, timeout time.Duration) bool {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func defaultSessionPump(ctx context.Context, opts hostrelay.HostRelayOptions) error {
	return hostrelay.NewHostRelayClient(opts).Run(ctx)
}

func (r *Runtime) Start(ctx context.Context, cfg coreconfig.Config, req ConfigureRequest, coordIdentity identity.CoordinatorIdentity, generation int) {
	r.mu.Lock()
	if r.running {
		cancel := r.cancel
		r.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		r.waitStopped()
	} else {
		r.mu.Unlock()
	}

	r.mu.Lock()
	loopCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.cfg = cfg
	r.req = applyRequestDefaults(cfg, req)
	r.coordIdentity = coordIdentity
	r.generation = generation
	r.heartbeatRunning = true
	r.leaseRenewRunning = true
	r.sessionPumpLive = true
	r.keepalive = &keepaliveClient{
		coordinatorURL: cfg.CoordinatorURL,
		groupID:        coordIdentity.GroupID,
		hostID:         coordIdentity.HostID,
		hostToken:      coordIdentity.HostToken,
		generation:     generation,
	}
	r.keepalive.start(loopCtx)
	r.mu.Unlock()

	r.sendHeartbeat(loopCtx)
	r.renewLease(loopCtx)
	r.checkHealth(loopCtx)

	go r.heartbeatLoop(loopCtx)
	go r.leaseRenewLoop(loopCtx)
	go r.sessionConsumerLoop(loopCtx)
	go r.healthCheckLoop(loopCtx)
}

func (r *Runtime) waitStopped() {
	for i := 0; i < 50; i++ {
		r.mu.RLock()
		running := r.running
		r.mu.RUnlock()
		if !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r *Runtime) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.running = false
	r.heartbeatRunning = false
	r.leaseRenewRunning = false
	r.sessionPumpLive = false
	if r.keepalive != nil {
		r.keepalive.stop()
	}
	for _, cancelSession := range r.activeSessions {
		cancelSession()
	}
	r.activeSessions = map[string]context.CancelFunc{}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Runtime) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

func (r *Runtime) Snapshot(cfg coreconfig.Config, lease coordinatorclient.HostLeaseStatus) State {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state := baseStateFromConfig(cfg, r.req)
	state.CurrentHost = lease.CurrentHostIDMatches || (lease.CurrentHostID != "" && lease.CurrentHostID == cfg.Compat.LegacyHostID)
	state.CurrentDevice = state.CurrentHost
	if r.running {
		state.CurrentHost = r.currentHost || state.CurrentHost
		state.CurrentDevice = r.currentDevice || state.CurrentDevice
	}
	state.HeartbeatRunning = r.running && r.heartbeatRunning
	state.HeartbeatOK = r.heartbeatOK
	state.LastHeartbeatAt = r.lastHeartbeatAt
	if state.LastHeartbeatAt == "" {
		state.LastHeartbeatAt = lease.ServerTime
	}
	state.LeaseRenewRunning = r.running && r.leaseRenewRunning
	state.LeaseActive = r.leaseActive || lease.LeaseValid
	applyLeaseTiming(&state, firstNonEmpty(r.leaseExpiresAt, lease.LeaseExpiresAt), lease.ServerTime)

	connected, connectedAt, lastSeenAt, keepaliveErr := false, "", "", ""
	if r.keepalive != nil {
		connected, connectedAt, lastSeenAt, keepaliveErr = r.keepalive.snapshot()
	}
	state.TunnelConnected = r.running && connected
	state.TunnelConnectedAt = connectedAt
	state.TunnelLastSeenAt = lastSeenAt
	state.SessionPumpRunning = r.running && r.sessionPumpLive
	state.PublicListenerReady = r.publicListenerReady
	state.LocalMinecraftReachable = r.localMinecraftReachable
	state.PublicMinecraftPingOk = r.publicMinecraftPingOk
	if !r.lastPublicPingAt.IsZero() {
		state.LastPublicPingAt = r.lastPublicPingAt.UTC().Format(time.RFC3339)
		state.RecentPublicPingOk = time.Since(r.lastPublicPingAt) <= 5*time.Minute
	}
	state.RecentSessions = append([]SessionDiagnostic{}, r.recentSessions...)
	state.LastTunnelError = firstNonEmpty(r.lastTunnelError, keepaliveErr)
	state.LastDisconnectReason = r.lastDisconnectReason
	state.Errors = append([]*coreerrors.Error{}, r.errors...)
	if !state.Active && len(state.Errors) == 0 && state.LastDisconnectReason != "" {
		state.Errors = append(state.Errors, coreerrors.New(
			coreerrors.ErrorCode(state.LastDisconnectReason),
			state.LastDisconnectReason,
			coreerrors.Details{CoordinatorURL: cfg.CoordinatorURL},
			"",
		))
	}
	return finalizeState(state)
}

func (r *Runtime) recordError(code coreerrors.ErrorCode, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, coreerrors.New(code, message, coreerrors.Details{CoordinatorURL: r.cfg.CoordinatorURL}, ""))
	if len(r.errors) > 8 {
		r.errors = r.errors[len(r.errors)-8:]
	}
}

func (r *Runtime) setDisconnectReason(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastDisconnectReason = reason
}

func (r *Runtime) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(defaultHeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	r.sendHeartbeat(ctx)
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.heartbeatRunning = false
			r.lastDisconnectReason = DisconnectHeartbeatStopped
			r.mu.Unlock()
			return
		case <-ticker.C:
			r.sendHeartbeat(ctx)
		}
	}
}

func (r *Runtime) sendHeartbeat(ctx context.Context) {
	r.mu.RLock()
	cfg := r.cfg
	req := r.req
	identitySnapshot := r.coordIdentity
	r.mu.RUnlock()

	client := r.clientOrCreate(cfg)
	if client == nil {
		r.mu.Lock()
		r.heartbeatOK = false
		r.mu.Unlock()
		r.recordError(coreerrors.CoordinatorConnectionLost, "coordinator client unavailable")
		r.setDisconnectReason(DisconnectCoordinatorLost)
		return
	}
	_, err := client.SendHeartbeat(ctx, coordinatorclient.HeartbeatRequest{
		GroupID:   identitySnapshot.GroupID,
		HostID:    identitySnapshot.HostID,
		HostToken: identitySnapshot.HostToken,
		Status:    "hosting",
		Connection: &coordinatorclient.HostConnection{
			Host:    req.LocalMinecraftHost,
			Port:    req.LocalMinecraftPort,
			Network: "tcp",
		},
	})
	r.mu.Lock()
	if err != nil {
		r.heartbeatOK = false
		r.lastDisconnectReason = DisconnectCoordinatorLost
		r.mu.Unlock()
		r.recordError(coreerrors.CoordinatorConnectionLost, err.Message)
		return
	}
	r.heartbeatOK = true
	r.lastHeartbeatAt = time.Now().UTC().Format(time.RFC3339)
	r.mu.Unlock()
}

func (r *Runtime) leaseRenewLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultLeaseRenewInterval)
	defer ticker.Stop()
	r.renewLease(ctx)
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.leaseRenewRunning = false
			r.leaseActive = false
			if r.lastDisconnectReason == "" {
				r.lastDisconnectReason = DisconnectLeaseExpired
			}
			r.mu.Unlock()
			return
		case <-ticker.C:
			r.renewLease(ctx)
		}
	}
}

func (r *Runtime) renewLease(ctx context.Context) {
	r.mu.RLock()
	cfg := r.cfg
	identitySnapshot := r.coordIdentity
	r.mu.RUnlock()

	client := r.clientOrCreate(cfg)
	if client == nil {
		r.mu.Lock()
		r.leaseActive = false
		r.mu.Unlock()
		return
	}
	ensured, err := client.EnsureActiveLease(ctx, identitySnapshot.GroupID, identitySnapshot.HostID, identitySnapshot.HostToken)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.leaseActive = false
		r.currentHost = false
		r.currentDevice = false
		r.lastDisconnectReason = DisconnectLeaseExpired
		return
	}
	lease := ensured.Lease
	r.leaseActive = lease.LeaseValid
	r.leaseExpiresAt = lease.LeaseExpiresAt
	r.generation = lease.Generation
	r.currentHost = lease.CurrentHostIDMatches
	r.currentDevice = lease.CurrentHostIDMatches
	if r.keepalive != nil {
		r.keepalive.generation = lease.Generation
	}
}

func (r *Runtime) sessionConsumerLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultSessionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.sessionPumpLive = false
			if r.lastDisconnectReason == "" {
				r.lastDisconnectReason = DisconnectSessionPumpExited
			}
			r.mu.Unlock()
			return
		case <-ticker.C:
			r.pollSessions(ctx)
		}
	}
}

func (r *Runtime) pollSessions(ctx context.Context) {
	r.mu.RLock()
	cfg := r.cfg
	req := r.req
	identitySnapshot := r.coordIdentity
	generation := r.generation
	target := netJoinHostPort(req.LocalMinecraftHost, req.LocalMinecraftPort)
	r.mu.RUnlock()

	client := r.clientOrCreate(cfg)
	if client == nil {
		return
	}
	sessions, err := client.ListTunnelSessions(ctx, identitySnapshot.GroupID)
	if err != nil {
		r.mu.Lock()
		r.lastTunnelError = err.Message
		r.mu.Unlock()
		return
	}

	seen := map[string]bool{}
	for _, session := range sessions {
		if session.HostID != identitySnapshot.HostID {
			continue
		}
		if session.Status == "closed" || session.Status == "expired" || session.Status == "failed" {
			continue
		}
		seen[session.SessionID] = true
		r.mu.RLock()
		_, exists := r.activeSessions[session.SessionID]
		r.mu.RUnlock()
		if exists {
			continue
		}
		pumpCtx, cancel := context.WithCancel(ctx)
		r.mu.Lock()
		r.activeSessions[session.SessionID] = cancel
		r.mu.Unlock()
		go r.runSessionPump(pumpCtx, cfg, identitySnapshot, session, target, generation)
	}

	r.mu.Lock()
	for sessionID, cancel := range r.activeSessions {
		if !seen[sessionID] {
			cancel()
			delete(r.activeSessions, sessionID)
		}
	}
	r.mu.Unlock()
}

func (r *Runtime) runSessionPump(ctx context.Context, cfg coreconfig.Config, coordIdentity identity.CoordinatorIdentity, session coordinatorclient.TunnelSession, target string, generation int) {
	defer func() {
		r.mu.Lock()
		delete(r.activeSessions, session.SessionID)
		r.mu.Unlock()
	}()
	gen := session.CurrentHostGeneration
	if gen == 0 {
		gen = generation
	}
	sessionID := session.SessionID
	err := r.runPump(ctx, hostrelay.HostRelayOptions{
		CoordinatorURL: cfg.CoordinatorURL,
		GroupID:        coordIdentity.GroupID,
		SessionID:      sessionID,
		HostID:         coordIdentity.HostID,
		HostToken:      coordIdentity.HostToken,
		HostGeneration: gen,
		TargetAddress:  target,
		OnClose: func(stats hostrelay.SessionStats) {
			r.recordSessionDiagnostic(sessionID, stats)
		},
	})
	if err != nil && ctx.Err() == nil {
		r.mu.Lock()
		r.lastTunnelError = err.Error()
		r.lastDisconnectReason = DisconnectTunnelExited
		r.mu.Unlock()
		r.recordError(coreerrors.RelayTunnelExited, err.Error())
	}
}

func (r *Runtime) recordSessionDiagnostic(sessionID string, stats hostrelay.SessionStats) {
	diag := SessionDiagnostic{
		SessionID:          sessionID,
		StartedAt:          stats.StartedAt.UTC().Format(time.RFC3339),
		ClosedAt:           stats.ClosedAt.UTC().Format(time.RFC3339),
		RemotePlayerAddress: stats.RemotePlayerAddress,
		LocalConnected:     stats.LocalConnected,
		ForwardingStarted:  stats.ForwardingStarted,
		BytesPlayerToLocal: stats.BytesPlayerToLocal,
		BytesLocalToPlayer: stats.BytesLocalToPlayer,
		UpstreamClosed:     stats.UpstreamClosed,
		DownstreamClosed:   stats.DownstreamClosed,
		CloseReason:        stats.CloseReason,
		Error:              stats.Error,
	}
	r.mu.Lock()
	r.recentSessions = append(r.recentSessions, diag)
	if len(r.recentSessions) > 16 {
		r.recentSessions = r.recentSessions[len(r.recentSessions)-16:]
	}
	r.mu.Unlock()
}

func (r *Runtime) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultHealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkHealth(ctx)
		}
	}
}

func (r *Runtime) checkHealth(ctx context.Context) {
	r.mu.RLock()
	cfg := r.cfg
	req := r.req
	r.mu.RUnlock()

	localAddr := netJoinHostPort(req.LocalMinecraftHost, req.LocalMinecraftPort)
	publicAddr := netJoinHostPort(publicHost(cfg), req.PublicMinecraftPort)
	publicTCP := r.prober(ctx, publicAddr, 1200*time.Millisecond)

	localOK := false
	if r.pingStatus != nil {
		localOK, _ = r.pingStatus(ctx, localAddr, 2*time.Second)
	}
	if !localOK {
		localOK = r.prober(ctx, localAddr, 800*time.Millisecond)
	}

	publicPingOK := false
	if publicTCP && r.pingStatus != nil {
		publicPingOK, _ = r.pingStatus(ctx, publicAddr, 4*time.Second)
	}

	r.mu.Lock()
	r.localMinecraftReachable = localOK
	r.publicListenerReady = publicTCP
	r.publicMinecraftPingOk = publicPingOK
	if publicPingOK {
		r.lastPublicPingAt = time.Now().UTC()
	}
	if !localOK {
		r.lastDisconnectReason = DisconnectLocalMCUnreachable
	}
	if !publicTCP && r.lastDisconnectReason == "" {
		r.lastDisconnectReason = DisconnectPublicListenerDown
	}
	r.mu.Unlock()

	if !localOK {
		r.recordError(coreerrors.LocalMinecraftUnreachable, fmt.Sprintf("local target %s unreachable", localAddr))
	}
	if !publicTCP {
		r.recordError(coreerrors.PublicListenerNotReady, fmt.Sprintf("public listener %s unreachable", publicAddr))
	}
}

func (r *Runtime) clientOrCreate(cfg coreconfig.Config) Client {
	if r.client != nil {
		return r.client
	}
	created, err := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, r.httpClient)
	if err != nil {
		return nil
	}
	return created
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}