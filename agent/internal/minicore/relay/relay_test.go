package relay

import (
	"context"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

func TestConfigureUsesConfigIdentity(t *testing.T) {
	cfg := testConfig()
	client := &fakeClient{lease: testLease()}
	result, err := (&Service{Client: client}).Configure(context.Background(), cfg, ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if client.bootstrapReq.InstanceID != "inst" || client.bootstrapReq.DeviceID != "dev" {
		t.Fatalf("bootstrap not called: %#v", client.bootstrapReq)
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
	cfg.Relay.Enabled = true
	client := &fakeClient{lease: testLease()}
	svc := &Service{Client: client}
	_, err := svc.Configure(context.Background(), cfg, ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	rt := svc.runtimeOrCreate()
	rt.keepalive = &keepaliveClient{coordinatorURL: cfg.CoordinatorURL, groupID: "grp", hostID: "host", hostToken: "ht", generation: 1}
	rt.keepalive.mu.Lock()
	rt.keepalive.connected = true
	rt.keepalive.mu.Unlock()
	rt.prober = alwaysReachable
	state, err := svc.Status(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !state.Configured || !state.CurrentHost || state.PublicEndpoint != "121.40.101.224:25565" {
		t.Fatalf("state = %#v", state)
	}
}

func TestRelayErrorsPropagateCodes(t *testing.T) {
	cases := []coreerrors.ErrorCode{coreerrors.AuthMissing, coreerrors.LeaseExpired, coreerrors.CoordinatorRouteMissing}
	for _, code := range cases {
		t.Run(string(code), func(t *testing.T) {
			cfg := testConfig()
			client := &fakeClient{ensureErr: coreerrors.New(code, "failed", coreerrors.Details{URL: cfg.CoordinatorURL + "/x", Method: "POST", HTTPStatus: 403, ResponseBody: "{}"}, "")}
			_, err := (&Service{Client: client}).Configure(context.Background(), cfg, ConfigureRequest{})
			if err == nil || err.ErrorCode != code {
				t.Fatalf("Configure() error = %v, want %s", err, code)
			}
		})
	}
}

func TestRelayRouteMissingPreservesCoordinatorDetails(t *testing.T) {
	cfg := testConfig()
	details := coreerrors.Details{
		URL:          cfg.CoordinatorURL + "/v1/groups/grp/lease/ensure-active",
		Method:       "POST",
		HTTPStatus:   404,
		ResponseBody: `{"message":"Route POST:/v1/groups/grp/lease/ensure-active not found"}`,
	}
	client := &fakeClient{ensureErr: coreerrors.New(coreerrors.CoordinatorRouteMissing, "route missing", details, "")}
	_, err := (&Service{Client: client}).Configure(context.Background(), cfg, ConfigureRequest{})
	if err == nil {
		t.Fatal("Configure() error = nil")
	}
	if err.Details.URL != details.URL || err.Details.HTTPStatus != details.HTTPStatus || err.Details.ResponseBody != details.ResponseBody {
		t.Fatalf("details = %#v, want %#v", err.Details, details)
	}
}

func TestRelayDoesNotFallbackLocalhost(t *testing.T) {
	cfg := testConfig()
	cfg.CoordinatorURL = "http://public.test:6121"
	cfg.Relay.PublicHost = ""
	client := &fakeClient{lease: testLease()}
	result, err := (&Service{Client: client}).Configure(context.Background(), cfg, ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if result.Relay.PublicEndpoint != "public.test:25565" {
		t.Fatalf("public endpoint = %q", result.Relay.PublicEndpoint)
	}
}

func testConfig() coreconfig.Config {
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance = coreconfig.InstanceConfig{InstanceID: "inst", DisplayName: "private", OwnerToken: "ht"}
	cfg.Device = coreconfig.DeviceConfig{DeviceID: "dev", DisplayName: "pc", Platform: "windows"}
	cfg.Server.ServerID = "srv"
	cfg.Compat = coreconfig.CompatConfig{CoordinatorProtocol: 2, LegacyGroupID: "grp", LegacyMemberID: "mem", LegacyHostID: "host", LegacyHostToken: "ht"}
	cfg.Relay.PublicHost = "121.40.101.224"
	cfg.Relay.Enabled = true
	return cfg
}