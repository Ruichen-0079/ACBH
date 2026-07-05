package relay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
	tunnelrelay "github.com/Ruichen-0079/ACBH/agent/internal/relay"
)

type Client interface {
	EnsureActiveLease(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error)
	SendHeartbeat(ctx context.Context, req coordinatorclient.HeartbeatRequest) (coordinatorclient.HeartbeatResponse, *coreerrors.Error)
	GetLeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.HostLeaseStatus, *coreerrors.Error)
	ListTunnelSessions(ctx context.Context, groupID string) ([]coordinatorclient.TunnelSession, *coreerrors.Error)
	StartPublicRelay(ctx context.Context, req coordinatorclient.PublicRelayControlRequest) (coordinatorclient.PublicRelayControlResponse, *coreerrors.Error)
	StopPublicRelay(ctx context.Context, req coordinatorclient.PublicRelayControlRequest) (coordinatorclient.PublicRelayControlResponse, *coreerrors.Error)
	PublicRelayStatus(ctx context.Context) (coordinatorclient.PublicRelayState, *coreerrors.Error)
}

type ConfigureRequest struct {
	LocalMinecraftHost  string `json:"localMinecraftHost,omitempty"`
	LocalMinecraftPort  int    `json:"localMinecraftPort,omitempty"`
	PublicMinecraftPort int    `json:"publicMinecraftPort,omitempty"`
}

type State struct {
	Configured           bool                `json:"configured"`
	Active               bool                `json:"active"`
	LocalServerListening bool                `json:"localServerListening"`
	TunnelConnected      bool                `json:"tunnelConnected"`
	PublicListenerActive bool                `json:"publicListenerActive"`
	PublicEndpoint       string              `json:"publicEndpoint"`
	LocalEndpoint        string              `json:"localEndpoint"`
	CurrentHost          bool                `json:"currentHost"`
	CurrentDevice        bool                `json:"currentDevice"`
	ActiveConnections    int                 `json:"activeConnections"`
	LastHeartbeatAt      string              `json:"lastHeartbeatAt,omitempty"`
	LastError            string              `json:"lastError,omitempty"`
	Errors               []*coreerrors.Error `json:"errors"`
}

type ConfigureResult struct {
	OK    bool  `json:"ok"`
	Relay State `json:"relay"`
}

type Service struct {
	Client       Client
	HTTPClient   *http.Client
	Tunnel       *TunnelManager
	PollInterval time.Duration
}

func (s Service) Configure(ctx context.Context, cfg coreconfig.Config, req ConfigureRequest) (ConfigureResult, *coreerrors.Error) {
	if err := validate(cfg); err != nil {
		return ConfigureResult{}, err
	}
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		return ConfigureResult{}, identityErr
	}
	req = applyRequestDefaults(cfg, req)
	client := s.Client
	if client == nil {
		created, err := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
		if err != nil {
			return ConfigureResult{}, err
		}
		client = created
	}
	lease, leaseErr := client.EnsureActiveLease(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken)
	if leaseErr != nil {
		return ConfigureResult{}, leaseErr
	}
	_, hbErr := client.SendHeartbeat(ctx, coordinatorclient.HeartbeatRequest{
		GroupID:   coordIdentity.GroupID,
		HostID:    coordIdentity.HostID,
		HostToken: coordIdentity.HostToken,
		Status:    "hosting",
		Connection: &coordinatorclient.HostConnection{
			Host:    req.LocalMinecraftHost,
			Port:    req.LocalMinecraftPort,
			Network: "tcp",
		},
	})
	if hbErr != nil {
		return ConfigureResult{}, hbErr
	}
	state := stateFromLease(cfg, req, lease.Lease)
	state.Configured = true
	state.Active = true
	return ConfigureResult{OK: true, Relay: state}, nil
}

func (s *Service) Start(ctx context.Context, cfg coreconfig.Config, req ConfigureRequest) (ConfigureResult, *coreerrors.Error) {
	result, err := s.Configure(ctx, cfg, req)
	if err != nil {
		return ConfigureResult{}, err
	}
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		return ConfigureResult{}, identityErr
	}
	req = applyRequestDefaults(cfg, req)
	client, clientErr := s.client(cfg)
	if clientErr != nil {
		return ConfigureResult{}, clientErr
	}
	public, publicErr := client.StartPublicRelay(ctx, coordinatorclient.PublicRelayControlRequest{
		GroupID:    coordIdentity.GroupID,
		HostID:     coordIdentity.HostID,
		HostToken:  coordIdentity.HostToken,
		PublicPort: req.PublicMinecraftPort,
	})
	if publicErr != nil {
		return ConfigureResult{}, publicErr
	}
	manager := s.manager()
	manager.Start(TunnelOptions{
		CoordinatorURL: cfg.CoordinatorURL,
		GroupID:        coordIdentity.GroupID,
		HostID:         coordIdentity.HostID,
		HostToken:      coordIdentity.HostToken,
		TargetAddress:  net.JoinHostPort(req.LocalMinecraftHost, strconv.Itoa(req.LocalMinecraftPort)),
		PollInterval:   s.PollInterval,
		Client:         client,
	})
	state := result.Relay
	state.TunnelConnected = manager.Running()
	state.PublicListenerActive = public.Relay.PublicListenerActive
	state.ActiveConnections = public.Relay.ActiveConnections
	if public.Relay.PublicEndpoint != "" {
		state.PublicEndpoint = public.Relay.PublicEndpoint
	}
	state.LocalServerListening = localServerListening(req.LocalMinecraftHost, req.LocalMinecraftPort)
	return ConfigureResult{OK: true, Relay: state}, nil
}

func (s *Service) Stop(ctx context.Context, cfg coreconfig.Config) (ConfigureResult, *coreerrors.Error) {
	if err := validate(cfg); err != nil {
		return ConfigureResult{}, err
	}
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		return ConfigureResult{}, identityErr
	}
	client, clientErr := s.client(cfg)
	if clientErr != nil {
		return ConfigureResult{}, clientErr
	}
	if s.Tunnel != nil {
		s.Tunnel.Stop()
	}
	public, publicErr := client.StopPublicRelay(ctx, coordinatorclient.PublicRelayControlRequest{
		GroupID:   coordIdentity.GroupID,
		HostID:    coordIdentity.HostID,
		HostToken: coordIdentity.HostToken,
	})
	if publicErr != nil {
		return ConfigureResult{}, publicErr
	}
	req := applyRequestDefaults(cfg, ConfigureRequest{})
	state := stateFromLease(cfg, req, coordinatorclient.HostLeaseStatus{})
	state.TunnelConnected = false
	state.PublicListenerActive = public.Relay.PublicListenerActive
	state.ActiveConnections = public.Relay.ActiveConnections
	state.LocalServerListening = localServerListening(req.LocalMinecraftHost, req.LocalMinecraftPort)
	return ConfigureResult{OK: true, Relay: state}, nil
}

func (s *Service) Status(ctx context.Context, cfg coreconfig.Config) (State, *coreerrors.Error) {
	if err := validate(cfg); err != nil {
		return State{}, err
	}
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		return State{}, identityErr
	}
	req := applyRequestDefaults(cfg, ConfigureRequest{})
	client, clientErr := s.client(cfg)
	if clientErr != nil {
		return State{}, clientErr
	}
	lease, leaseErr := client.GetLeaseStatus(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken)
	public, publicErr := client.PublicRelayStatus(ctx)
	if leaseErr != nil {
		state := stateFromLease(cfg, req, coordinatorclient.HostLeaseStatus{})
		state.Errors = append(state.Errors, leaseErr)
		if publicErr != nil {
			state.Errors = append(state.Errors, publicErr)
			state.LastError = publicErr.Message
		}
		applyRuntimeState(&state, public, s.Tunnel)
		return state, nil
	}
	state := stateFromLease(cfg, req, lease)
	if publicErr != nil {
		state.Errors = append(state.Errors, publicErr)
		state.LastError = publicErr.Message
	}
	applyRuntimeState(&state, public, s.Tunnel)
	return state, nil
}

func stateFromLease(cfg coreconfig.Config, req ConfigureRequest, lease coordinatorclient.HostLeaseStatus) State {
	current := lease.CurrentHostIDMatches || (lease.CurrentHostID != "" && lease.CurrentHostID == cfg.Compat.LegacyHostID)
	return State{
		Configured:           cfg.Relay.Enabled,
		Active:               cfg.Relay.Enabled && current && lease.LeaseValid,
		LocalServerListening: localServerListening(req.LocalMinecraftHost, req.LocalMinecraftPort),
		PublicEndpoint:       net.JoinHostPort(publicHost(cfg), strconv.Itoa(req.PublicMinecraftPort)),
		LocalEndpoint:        net.JoinHostPort(req.LocalMinecraftHost, strconv.Itoa(req.LocalMinecraftPort)),
		CurrentHost:          current,
		CurrentDevice:        current,
		LastHeartbeatAt:      lease.ServerTime,
	}
}

func (s *Service) client(cfg coreconfig.Config) (Client, *coreerrors.Error) {
	if s.Client != nil {
		return s.Client, nil
	}
	return coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
}

func (s *Service) manager() *TunnelManager {
	if s.Tunnel == nil {
		s.Tunnel = NewTunnelManager()
	}
	return s.Tunnel
}

func applyRuntimeState(state *State, public coordinatorclient.PublicRelayState, manager *TunnelManager) {
	if public.PublicEndpoint != "" {
		state.PublicEndpoint = public.PublicEndpoint
	}
	state.PublicListenerActive = public.PublicListenerActive
	state.ActiveConnections = public.ActiveConnections
	if public.LastError != "" && state.LastError == "" {
		state.LastError = public.LastError
	}
	if manager != nil {
		state.TunnelConnected = manager.Running()
		state.ActiveConnections = max(state.ActiveConnections, manager.ActiveConnections())
		if err := manager.LastError(); err != "" && state.LastError == "" {
			state.LastError = err
		}
	}
}

func applyRequestDefaults(cfg coreconfig.Config, req ConfigureRequest) ConfigureRequest {
	if req.LocalMinecraftHost == "" {
		req.LocalMinecraftHost = cfg.Listener.LocalHost
	}
	if req.LocalMinecraftPort == 0 {
		req.LocalMinecraftPort = cfg.Listener.LocalPort
	}
	if req.PublicMinecraftPort == 0 {
		req.PublicMinecraftPort = cfg.Relay.MinecraftPort
	}
	return req
}

func validate(cfg coreconfig.Config) *coreerrors.Error {
	if strings.TrimSpace(cfg.CoordinatorURL) == "" {
		return coreerrors.New(coreerrors.ConfigInvalid, "coordinatorUrl is required", coreerrors.Details{}, "Set coordinatorUrl in config.json.")
	}
	req := applyRequestDefaults(cfg, ConfigureRequest{})
	for name, port := range map[string]int{"listener.localPort": req.LocalMinecraftPort, "relay.minecraftPort": req.PublicMinecraftPort} {
		if port < 1 || port > 65535 {
			return coreerrors.New(coreerrors.ConfigInvalid, fmt.Sprintf("%s must be between 1 and 65535", name), coreerrors.Details{CoordinatorURL: cfg.CoordinatorURL}, "Use a valid TCP port.")
		}
	}
	return nil
}

func publicHost(cfg coreconfig.Config) string {
	if strings.TrimSpace(cfg.Relay.PublicHost) != "" {
		return cfg.Relay.PublicHost
	}
	if parsed, err := url.Parse(cfg.CoordinatorURL); err == nil {
		return parsed.Hostname()
	}
	return ""
}

func localServerListening(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type TunnelOptions struct {
	CoordinatorURL string
	GroupID        string
	HostID         string
	HostToken      string
	TargetAddress  string
	PollInterval   time.Duration
	Client         Client
}

type TunnelManager struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	running    bool
	active     map[string]context.CancelFunc
	lastErr    string
	generation int
}

func NewTunnelManager() *TunnelManager {
	return &TunnelManager{active: map[string]context.CancelFunc{}}
}

func (m *TunnelManager) Start(opts TunnelOptions) {
	m.Stop()
	m.mu.Lock()
	defer m.mu.Unlock()
	if opts.PollInterval <= 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.running = true
	m.lastErr = ""
	m.generation++
	generation := m.generation
	go m.loop(ctx, opts, generation)
}

func (m *TunnelManager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	active := make([]context.CancelFunc, 0, len(m.active))
	for _, c := range m.active {
		active = append(active, c)
	}
	m.running = false
	m.cancel = nil
	m.generation++
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, c := range active {
		c()
	}
}

func (m *TunnelManager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *TunnelManager) ActiveConnections() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

func (m *TunnelManager) LastError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

func (m *TunnelManager) loop(ctx context.Context, opts TunnelOptions, generation int) {
	defer func() {
		m.mu.Lock()
		if m.generation == generation {
			m.running = false
		}
		m.mu.Unlock()
	}()
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()
	for {
		m.poll(ctx, opts)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *TunnelManager) poll(ctx context.Context, opts TunnelOptions) {
	if opts.Client == nil {
		m.setError("relay tunnel client is not configured")
		return
	}
	_, _ = opts.Client.SendHeartbeat(ctx, coordinatorclient.HeartbeatRequest{
		GroupID:   opts.GroupID,
		HostID:    opts.HostID,
		HostToken: opts.HostToken,
		Status:    "hosting",
	})
	sessions, err := opts.Client.ListTunnelSessions(ctx, opts.GroupID)
	if err != nil {
		m.setError(err.Message)
		return
	}
	for _, session := range sessions {
		if session.HostID != opts.HostID || (session.Status != "pending" && session.Status != "active") {
			continue
		}
		m.startSession(ctx, opts, session)
	}
}

func (m *TunnelManager) startSession(parent context.Context, opts TunnelOptions, session coordinatorclient.TunnelSession) {
	m.mu.Lock()
	if _, ok := m.active[session.SessionID]; ok {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.active[session.SessionID] = cancel
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.active, session.SessionID)
			m.mu.Unlock()
		}()
		client := tunnelrelay.NewHostRelayClient(tunnelrelay.HostRelayOptions{
			CoordinatorURL: opts.CoordinatorURL,
			GroupID:        opts.GroupID,
			SessionID:      session.SessionID,
			HostID:         opts.HostID,
			HostToken:      opts.HostToken,
			HostGeneration: session.CurrentHostGeneration,
			TargetAddress:  opts.TargetAddress,
		})
		if err := client.Run(ctx); err != nil {
			m.setError(err.Error())
		}
	}()
}

func (m *TunnelManager) setError(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastErr = message
}
