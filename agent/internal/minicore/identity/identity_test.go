package identity

import (
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

func TestAdapterMapsSingleOwnerToLegacyCoordinatorIdentity(t *testing.T) {
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance = coreconfig.InstanceConfig{InstanceID: "inst_1", OwnerToken: "ht_owner"}
	cfg.Device = coreconfig.DeviceConfig{DeviceID: "dev_1", DisplayName: "PC", Platform: "windows"}
	cfg.Server.ServerID = "srv_1"
	cfg.Compat = coreconfig.CompatConfig{CoordinatorProtocol: 2, LegacyGroupID: "grp_1", LegacyMemberID: "mem_1", LegacyHostID: "host_1", LegacyHostToken: "ht_legacy"}

	got, err := Adapter(cfg)
	if err != nil {
		t.Fatalf("Adapter() error = %v", err)
	}
	if got.InstanceID != "inst_1" || got.DeviceID != "dev_1" || got.GroupID != "grp_1" || got.HostID != "host_1" || got.HostToken != "ht_legacy" {
		t.Fatalf("identity = %#v", got)
	}
}

func TestAdapterMissingAccessTokenReturnsIdentityIncomplete(t *testing.T) {
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance.InstanceID = "inst_1"
	cfg.Device.DeviceID = "dev_1"
	cfg.Server.ServerID = "srv_1"

	_, err := Adapter(cfg)
	if err == nil || err.ErrorCode != coreerrors.IdentityIncomplete {
		t.Fatalf("Adapter() error = %v, want identity_incomplete", err)
	}
}

func TestAdapterTokenOnlyMapsInstanceAndDeviceIDs(t *testing.T) {
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance = coreconfig.InstanceConfig{InstanceID: "inst_1", OwnerToken: "access-token"}
	cfg.Device = coreconfig.DeviceConfig{DeviceID: "dev_1", DisplayName: "PC", Platform: "windows"}
	cfg.Server.ServerID = "srv_1"

	got, err := Adapter(cfg)
	if err != nil {
		t.Fatalf("Adapter() error = %v", err)
	}
	if got.GroupID != "inst_1" || got.HostID != "dev_1" || got.HostToken != "access-token" {
		t.Fatalf("identity = %#v", got)
	}
}

func TestTokenRedaction(t *testing.T) {
	if RedactToken("ht_secret") != "[redacted]" || RedactToken("") != "" {
		t.Fatal("token redaction mismatch")
	}
}
