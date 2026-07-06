package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
)

type Client interface {
	Bootstrap(ctx context.Context, accessToken string, req coordinatorclient.BootstrapRequest) (coordinatorclient.BootstrapResponse, *coreerrors.Error)
	EnsureActiveLease(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error)
	SendHeartbeat(ctx context.Context, req coordinatorclient.HeartbeatRequest) (coordinatorclient.HeartbeatResponse, *coreerrors.Error)
	GetLeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.HostLeaseStatus, *coreerrors.Error)
	ListTunnelSessions(ctx context.Context, groupID string) ([]coordinatorclient.TunnelSession, *coreerrors.Error)
}

type ConfigureRequest struct {
	LocalMinecraftHost  string `json:"localMinecraftHost,omitempty"`
	LocalMinecraftPort  int    `json:"localMinecraftPort,omitempty"`
	PublicMinecraftPort int    `json:"publicMinecraftPort,omitempty"`
}

type ConfigureResult struct {
	OK    bool  `json:"ok"`
	Relay State `json:"relay"`
}

type Service struct {
	Client     Client
	HTTPClient *http.Client

	mu      sync.Mutex
	runtime *Runtime
}

func (s *Service) runtimeOrCreate() *Runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime == nil {
		s.runtime = NewRuntime(s.Client, s.HTTPClient)
	}
	return s.runtime
}

func (s *Service) Configure(ctx context.Context, cfg coreconfig.Config, req ConfigureRequest) (ConfigureResult, *coreerrors.Error) {
	if err := validate(cfg); err != nil {
		return ConfigureResult{}, err
	}
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		return ConfigureResult{}, identityErr
	}
	req = applyRequestDefaults(cfg, req)
	client := s.clientOrCreate(cfg)
	if client == nil {
		return ConfigureResult{}, coreerrors.New(coreerrors.CoordinatorUnreachable, "coordinator client unavailable", coreerrors.Details{CoordinatorURL: cfg.CoordinatorURL}, "")
	}
	if _, bootstrapErr := client.Bootstrap(ctx, coordIdentity.OwnerToken, coordinatorclient.BootstrapRequest{
		InstanceID:   cfg.Instance.InstanceID,
		InstanceName: cfg.Instance.DisplayName,
		DeviceID:     cfg.Device.DeviceID,
		DeviceName:   cfg.Device.DisplayName,
		ServerID:     cfg.Server.ServerID,
		ServerName:   cfg.Server.DisplayName,
	}); bootstrapErr != nil {
		return ConfigureResult{}, bootstrapErr
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

	cfg.Relay.Enabled = true
	cfg.Listener.LocalHost = req.LocalMinecraftHost
	cfg.Listener.LocalPort = req.LocalMinecraftPort
	cfg.Relay.MinecraftPort = req.PublicMinecraftPort

	rt := s.runtimeOrCreate()
	rt.Start(ctx, cfg, req, coordIdentity, lease.Lease.Generation)

	state := rt.Snapshot(cfg, lease.Lease)
	state.Configured = true
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
	client := s.clientOrCreate(cfg)
	if client == nil {
		state := baseStateFromConfig(cfg, ConfigureRequest{})
		state.Errors = append(state.Errors, coreerrors.New(coreerrors.CoordinatorUnreachable, "coordinator client unavailable", coreerrors.Details{CoordinatorURL: cfg.CoordinatorURL}, ""))
		return finalizeState(state), nil
	}
	lease, leaseErr := client.GetLeaseStatus(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken)
	if leaseErr != nil {
		state := baseStateFromConfig(cfg, ConfigureRequest{})
		state.Errors = append(state.Errors, leaseErr)
		rt := s.runtimeOrCreate()
		if rt.IsRunning() {
			return rt.Snapshot(cfg, coordinatorclient.HostLeaseStatus{}), nil
		}
		return finalizeState(state), nil
	}

	rt := s.runtimeOrCreate()
	if !rt.IsRunning() && cfg.Relay.Enabled {
		req := applyRequestDefaults(cfg, ConfigureRequest{})
		rt.Start(ctx, cfg, req, coordIdentity, lease.Generation)
	}
	return rt.Snapshot(cfg, lease), nil
}

func stateFromLease(cfg coreconfig.Config, req ConfigureRequest, lease coordinatorclient.HostLeaseStatus) State {
	current := lease.CurrentHostIDMatches || (lease.CurrentHostID != "" && lease.CurrentHostID == cfg.Compat.LegacyHostID)
	state := State{
		Configured:              cfg.Relay.Enabled,
		PublicEndpoint:          netJoinHostPort(publicHost(cfg), req.PublicMinecraftPort),
		LocalEndpoint:           netJoinHostPort(req.LocalMinecraftHost, req.LocalMinecraftPort),
		CurrentHost:             current,
		CurrentDevice:           current,
		LastHeartbeatAt:         lease.ServerTime,
		HeartbeatIntervalSeconds: defaultHeartbeatIntervalSeconds,
		LeaseActive:             lease.LeaseValid,
	}
	applyLeaseTiming(&state, lease.LeaseExpiresAt, lease.ServerTime)
	return finalizeState(state)
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

func (s *Service) clientOrCreate(cfg coreconfig.Config) Client {
	if s.Client != nil {
		return s.Client
	}
	created, err := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
	if err != nil {
		return nil
	}
	return created
}