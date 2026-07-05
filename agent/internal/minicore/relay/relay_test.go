package relay

import (
	"context"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

type fakeClient struct {
	ensureErr *coreerrors.Error
	hbErr     *coreerrors.Error
	statusErr *coreerrors.Error
	lease     coordinatorclient.HostLeaseStatus
	public    coordinatorclient.PublicRelayState
	sessions  []coordinatorclient.TunnelSession

	ensureGroupID string
	ensureHostID  string
	ensureToken   string
	heartbeat     coordinatorclient.HeartbeatRequest
	startPublic   coordinatorclient.PublicRelayControlRequest
	stopPublic    coordinatorclient.PublicRelayControlRequest
}

func (f *fakeClient) EnsureActiveLease(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error) {
	f.ensureGroupID = groupID
	f.ensureHostID = hostID
	f.ensureToken = hostToken
	if f.ensureErr != nil {
		return coordinatorclient.EnsureActiveLeaseResponse{}, f.ensureErr
	}
	return coordinatorclient.EnsureActiveLeaseResponse{OK: true, Lease: f.lease}, nil
}

func (f *fakeClient) SendHeartbeat(ctx context.Context, req coordinatorclient.HeartbeatRequest) (coordinatorclient.HeartbeatResponse, *coreerrors.Error) {
	f.heartbeat = req
	if f.hbErr != nil {
		return coordinatorclient.HeartbeatResponse{}, f.hbErr
	}
	return coordinatorclient.HeartbeatResponse{OK: true, HostID: req.HostID, Status: req.Status}, nil
}

func (f *fakeClient) GetLeaseStatus(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.HostLeaseStatus, *coreerrors.Error) {
	if f.statusErr != nil {
		return coordinatorclient.HostLeaseStatus{}, f.statusErr
	}
	return f.lease, nil
}

func (f *fakeClient) ListTunnelSessions(ctx context.Context, groupID string) ([]coordinatorclient.TunnelSession, *coreerrors.Error) {
	return f.sessions, nil
}

func (f *fakeClient) StartPublicRelay(ctx context.Context, req coordinatorclient.PublicRelayControlRequest) (coordinatorclient.PublicRelayControlResponse, *coreerrors.Error) {
	f.startPublic = req
	if f.public.PublicEndpoint == "" {
		f.public.PublicEndpoint = "121.40.101.224:25565"
	}
	f.public.PublicListenerActive = true
	return coordinatorclient.PublicRelayControlResponse{OK: true, Relay: f.public}, nil
}

func (f *fakeClient) StopPublicRelay(ctx context.Context, req coordinatorclient.PublicRelayControlRequest) (coordinatorclient.PublicRelayControlResponse, *coreerrors.Error) {
	f.stopPublic = req
	f.public.PublicListenerActive = false
	return coordinatorclient.PublicRelayControlResponse{OK: true, Relay: f.public}, nil
}

func (f *fakeClient) PublicRelayStatus(ctx context.Context) (coordinatorclient.PublicRelayState, *coreerrors.Error) {
	return f.public, nil
}

func testConfig() coreconfig.Config {
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance = coreconfig.InstanceConfig{InstanceID: "inst", DisplayName: "private", OwnerToken: "ht"}
	cfg.Device = coreconfig.DeviceConfig{DeviceID: "dev", DisplayName: "pc", Platform: "windows"}
	cfg.Server.ServerID = "srv"
	cfg.Compat = coreconfig.CompatConfig{CoordinatorProtocol: 2, LegacyGroupID: "grp", LegacyMemberID: "mem", LegacyHostID: "host", LegacyHostToken: "ht"}
	cfg.Relay.PublicHost = "121.40.101.224"
	return cfg
}

func TestConfigureUsesConfigIdentity(t *testing.T) {
	cfg := testConfig()
	client := &fakeClient{lease: coordinatorclient.HostLeaseStatus{CurrentHostID: "host", CurrentHostIDMatches: true, LeaseValid: true, ServerTime: "now"}}
	result, err := Service{Client: client}.Configure(context.Background(), cfg, ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if client.ensureGroupID != "grp" || client.ensureHostID != "host" || client.ensureToken != "ht" {
		t.Fatalf("identity not used: %#v", client)
	}
	if client.heartbeat.Connection == nil || client.heartbeat.Connection.Host != "127.0.0.1" || client.heartbeat.Connection.Port != 25565 {
		t.Fatalf("heartbeat connection = %#v", client.heartbeat.Connection)
	}
	if !result.OK || result.Relay.PublicEndpoint != "121.40.101.224:25565" {
		t.Fatalf("result = %#v", result)
	}
}

func TestStatusReturnsStructuredState(t *testing.T) {
	cfg := testConfig()
	client := &fakeClient{
		lease:  coordinatorclient.HostLeaseStatus{CurrentHostID: "host", CurrentHostIDMatches: true, LeaseValid: true, ServerTime: "2026-07-04T00:00:00Z"},
		public: coordinatorclient.PublicRelayState{PublicListenerActive: true, PublicEndpoint: "121.40.101.224:25565", ActiveConnections: 2},
	}
	state, err := (&Service{Client: client}).Status(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !state.Active || !state.CurrentHost || !state.PublicListenerActive || state.ActiveConnections != 2 || state.PublicEndpoint != "121.40.101.224:25565" {
		t.Fatalf("state = %#v", state)
	}
}

func TestStartStartsPublicRelayAndTunnelManager(t *testing.T) {
	cfg := testConfig()
	client := &fakeClient{lease: coordinatorclient.HostLeaseStatus{CurrentHostID: "host", CurrentHostIDMatches: true, LeaseValid: true, ServerTime: "now"}}
	service := &Service{Client: client, PollInterval: time.Hour}
	result, err := service.Start(context.Background(), cfg, ConfigureRequest{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { service.Tunnel.Stop() })
	if !result.Relay.TunnelConnected || !result.Relay.PublicListenerActive {
		t.Fatalf("relay start result = %#v", result.Relay)
	}
	if client.startPublic.GroupID != "grp" || client.startPublic.HostID != "host" || client.startPublic.PublicPort != 25565 {
		t.Fatalf("public relay start request = %#v", client.startPublic)
	}
}

func TestRelayErrorsPropagateCodes(t *testing.T) {
	cases := []coreerrors.ErrorCode{coreerrors.AuthMissing, coreerrors.LeaseExpired, coreerrors.CoordinatorRouteMissing}
	for _, code := range cases {
		t.Run(string(code), func(t *testing.T) {
			cfg := testConfig()
			client := &fakeClient{ensureErr: coreerrors.New(code, "failed", coreerrors.Details{URL: cfg.CoordinatorURL + "/x", Method: "POST", HTTPStatus: 403, ResponseBody: "{}"}, "")}
			_, err := Service{Client: client}.Configure(context.Background(), cfg, ConfigureRequest{})
			if err == nil || err.ErrorCode != code {
				t.Fatalf("Configure() error = %v, want %s", err, code)
			}
		})
	}
}

func TestRelayDoesNotFallbackLocalhost(t *testing.T) {
	cfg := testConfig()
	cfg.CoordinatorURL = "http://public.test:6121"
	cfg.Relay.PublicHost = ""
	client := &fakeClient{lease: coordinatorclient.HostLeaseStatus{CurrentHostID: "host", CurrentHostIDMatches: true, LeaseValid: true}}
	result, err := Service{Client: client}.Configure(context.Background(), cfg, ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if result.Relay.PublicEndpoint != "public.test:25565" {
		t.Fatalf("public endpoint = %q", result.Relay.PublicEndpoint)
	}
}
