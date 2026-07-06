package relay

import (
	"net"
	"strconv"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

func netJoinHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

const (
	defaultHeartbeatIntervalSeconds = 10
	defaultLeaseRenewInterval       = 15 * time.Second
	defaultSessionPollInterval      = 2 * time.Second
	defaultHealthCheckInterval      = 5 * time.Second
	defaultActiveTTLSeconds         = 30
)

// DisconnectReason values written to lastDisconnectReason.
const (
	DisconnectHeartbeatStopped    = "relay_heartbeat_stopped"
	DisconnectLeaseExpired        = "relay_lease_expired"
	DisconnectTTLExpired          = "relay_ttl_expired"
	DisconnectTunnelNotConnected  = "relay_tunnel_not_connected"
	DisconnectTunnelExited        = "relay_tunnel_exited"
	DisconnectSessionPumpExited   = "relay_session_pump_exited"
	DisconnectLocalMCUnreachable  = "local_minecraft_unreachable"
	DisconnectCoordinatorLost     = "coordinator_connection_lost"
	DisconnectAuthFailed          = "auth_failed"
	DisconnectPublicListenerDown  = "public_listener_not_ready"
)

type State struct {
	Configured                bool                `json:"configured"`
	CurrentHost               bool                `json:"currentHost"`
	CurrentDevice             bool                `json:"currentDevice"`
	Active                    bool                `json:"active"`
	PublicEndpoint            string              `json:"publicEndpoint"`
	LocalEndpoint             string              `json:"localEndpoint"`
	HeartbeatRunning          bool                `json:"heartbeatRunning"`
	HeartbeatOK               bool                `json:"heartbeatOk"`
	HeartbeatIntervalSeconds  int                 `json:"heartbeatIntervalSeconds"`
	LastHeartbeatAt           string              `json:"lastHeartbeatAt,omitempty"`
	LeaseRenewRunning         bool                `json:"leaseRenewRunning"`
	LeaseActive               bool                `json:"leaseActive"`
	LeaseExpiresAt            string              `json:"leaseExpiresAt,omitempty"`
	ActiveTTLSeconds          int                 `json:"activeTtlSeconds"`
	ActiveUntil               string              `json:"activeUntil,omitempty"`
	TunnelConnected           bool                `json:"tunnelConnected"`
	TunnelConnectedAt         string              `json:"tunnelConnectedAt,omitempty"`
	TunnelLastSeenAt          string              `json:"tunnelLastSeenAt,omitempty"`
	SessionPumpRunning        bool                `json:"sessionPumpRunning"`
	PublicListenerReady       bool                `json:"publicListenerReady"`
	LocalMinecraftReachable   bool                `json:"localMinecraftReachable"`
	LastTunnelError           string              `json:"lastTunnelError,omitempty"`
	LastDisconnectReason      string              `json:"lastDisconnectReason,omitempty"`
	Errors                    []*coreerrors.Error `json:"errors"`
}

func computeActive(s State) bool {
	return s.Configured &&
		s.CurrentHost &&
		s.CurrentDevice &&
		s.HeartbeatRunning &&
		s.HeartbeatOK &&
		s.LeaseRenewRunning &&
		s.LeaseActive &&
		s.TunnelConnected &&
		s.SessionPumpRunning &&
		s.PublicListenerReady &&
		s.LocalMinecraftReachable
}

func finalizeState(s State) State {
	s.Active = computeActive(s)
	if !s.Active && s.LastDisconnectReason == "" {
		s.LastDisconnectReason = inferDisconnectReason(s)
	}
	return s
}

func inferDisconnectReason(s State) string {
	switch {
	case !s.Configured:
		return ""
	case !s.TunnelConnected:
		return DisconnectTunnelNotConnected
	case !s.SessionPumpRunning:
		return DisconnectSessionPumpExited
	case !s.HeartbeatRunning:
		return DisconnectHeartbeatStopped
	case !s.HeartbeatOK:
		return DisconnectCoordinatorLost
	case !s.LeaseRenewRunning:
		return DisconnectHeartbeatStopped
	case !s.LeaseActive:
		return DisconnectLeaseExpired
	case !s.LocalMinecraftReachable:
		return DisconnectLocalMCUnreachable
	case !s.PublicListenerReady:
		return DisconnectPublicListenerDown
	default:
		return ""
	}
}

func applyLeaseTiming(s *State, leaseExpiresAt string, serverTime string) {
	if leaseExpiresAt != "" {
		s.LeaseExpiresAt = leaseExpiresAt
		if expires, err := time.Parse(time.RFC3339, leaseExpiresAt); err == nil {
			s.ActiveUntil = expires.Format(time.RFC3339)
			s.ActiveTTLSeconds = int(time.Until(expires).Seconds())
			if s.ActiveTTLSeconds < 0 {
				s.ActiveTTLSeconds = 0
			}
		}
	} else if serverTime != "" {
		if parsed, err := time.Parse(time.RFC3339, serverTime); err == nil {
			until := parsed.Add(time.Duration(defaultActiveTTLSeconds) * time.Second)
			s.ActiveUntil = until.Format(time.RFC3339)
			s.ActiveTTLSeconds = defaultActiveTTLSeconds
			s.LeaseExpiresAt = until.Format(time.RFC3339)
		}
	}
}

func baseStateFromConfig(cfg coreconfig.Config, req ConfigureRequest) State {
	req = applyRequestDefaults(cfg, req)
	publicHost := publicHost(cfg)
	return State{
		Configured:               cfg.Relay.Enabled,
		PublicEndpoint:           netJoinHostPort(publicHost, req.PublicMinecraftPort),
		LocalEndpoint:            netJoinHostPort(req.LocalMinecraftHost, req.LocalMinecraftPort),
		HeartbeatIntervalSeconds: defaultHeartbeatIntervalSeconds,
		ActiveTTLSeconds:         defaultActiveTTLSeconds,
	}
}