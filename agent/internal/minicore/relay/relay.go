package relay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
)

type Client interface {
	EnsureActiveLease(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error)
	SendHeartbeat(ctx context.Context, req coordinatorclient.HeartbeatRequest) (coordinatorclient.HeartbeatResponse, *coreerrors.Error)
	GetLeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.HostLeaseStatus, *coreerrors.Error)
}

type ConfigureRequest struct {
	LocalMinecraftHost  string `json:"localMinecraftHost,omitempty"`
	LocalMinecraftPort  int    `json:"localMinecraftPort,omitempty"`
	PublicMinecraftPort int    `json:"publicMinecraftPort,omitempty"`
}

type State struct {
	Configured      bool                `json:"configured"`
	Active          bool                `json:"active"`
	PublicEndpoint  string              `json:"publicEndpoint"`
	LocalEndpoint   string              `json:"localEndpoint"`
	CurrentHost     bool                `json:"currentHost"`
	CurrentDevice   bool                `json:"currentDevice"`
	LastHeartbeatAt string              `json:"lastHeartbeatAt,omitempty"`
	Errors          []*coreerrors.Error `json:"errors"`
}

type ConfigureResult struct {
	OK    bool  `json:"ok"`
	Relay State `json:"relay"`
}

type Service struct {
	Client     Client
	HTTPClient *http.Client
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

func (s Service) Status(ctx context.Context, cfg coreconfig.Config) (State, *coreerrors.Error) {
	if err := validate(cfg); err != nil {
		return State{}, err
	}
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		return State{}, identityErr
	}
	req := applyRequestDefaults(cfg, ConfigureRequest{})
	client := s.Client
	if client == nil {
		created, err := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
		if err != nil {
			return State{}, err
		}
		client = created
	}
	lease, leaseErr := client.GetLeaseStatus(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken)
	if leaseErr != nil {
		state := stateFromLease(cfg, req, coordinatorclient.HostLeaseStatus{})
		state.Errors = append(state.Errors, leaseErr)
		return state, nil
	}
	return stateFromLease(cfg, req, lease), nil
}

func stateFromLease(cfg coreconfig.Config, req ConfigureRequest, lease coordinatorclient.HostLeaseStatus) State {
	current := lease.CurrentHostIDMatches || (lease.CurrentHostID != "" && lease.CurrentHostID == cfg.Compat.LegacyHostID)
	return State{
		Configured:      cfg.Relay.Enabled,
		Active:          cfg.Relay.Enabled && current && lease.LeaseValid,
		PublicEndpoint:  net.JoinHostPort(publicHost(cfg), strconv.Itoa(req.PublicMinecraftPort)),
		LocalEndpoint:   net.JoinHostPort(req.LocalMinecraftHost, strconv.Itoa(req.LocalMinecraftPort)),
		CurrentHost:     current,
		CurrentDevice:   current,
		LastHeartbeatAt: lease.ServerTime,
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
