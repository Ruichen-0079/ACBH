package coreconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

func validConfig() Config {
	cfg := DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Identity = Identity{
		GroupID: "grp_123", MemberID: "mem_123", HostID: "host_123", HostToken: "ht_123",
		DisplayName: "私人本地主机", DeviceName: "MSI", Platform: "windows",
	}
	cfg.Server.Dir = `C:\server`
	cfg.Relay.PublicHost = "121.40.101.224"
	return cfg
}

func TestValidConfigLoads(t *testing.T) {
	store := NewStore(t.TempDir())
	want := validConfig()
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.CoordinatorURL != want.CoordinatorURL || got.Identity.HostToken != want.Identity.HostToken {
		t.Fatalf("Load() = %#v, want coordinator/token preserved", got)
	}
}

func TestInvalidJSONReportsParseErrorAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := os.WriteFile(store.Path, []byte(`{"coordinatorUrl":`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, loadErr := store.Load()
	if loadErr == nil || loadErr.ErrorCode != coreerrors.ConfigParseError {
		t.Fatalf("Load() error = %v, want config_parse_error", loadErr)
	}
	matches, err := filepath.Glob(store.Path + ".broken-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("broken backup count = %d, want 1", len(matches))
	}
}

func TestYamlStyleCoordinatorKeyRejected(t *testing.T) {
	store := NewStore(t.TempDir())
	raw := map[string]any{
		"schemaVersion": 1,
		"mode":          "remote-public",
		"coordinatorUrl: http://121.40.101.224:6121": "",
		"identity": map[string]string{"groupId": "grp", "hostId": "host", "hostToken": "ht"},
	}
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(store.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, loadErr := store.Load()
	if loadErr == nil || loadErr.ErrorCode != coreerrors.ConfigInvalid {
		t.Fatalf("Load() error = %v, want config_invalid", loadErr)
	}
}

func TestRemotePublicRejectsLocalhost(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.CoordinatorURL = "http://127.0.0.1:6121"
	err := store.Save(cfg)
	if err == nil || err.ErrorCode != coreerrors.ConfigInvalid {
		t.Fatalf("Save() error = %v, want config_invalid", err)
	}
}

func TestConfigWriteIsAtomicAndNoSilentTokenReset(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(store.AppDataDir, FileName+".*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left after Save: %v", matches)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Identity.HostToken != cfg.Identity.HostToken || got.Identity.GroupID != cfg.Identity.GroupID {
		t.Fatalf("identity changed on save/load: %#v", got.Identity)
	}
}

func TestOldConfigDefaultsListenerRelay(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.Listener = ListenerConfig{}
	cfg.Relay = RelayConfig{}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Listener.LocalHost != "127.0.0.1" || got.Listener.LocalPort != 25565 {
		t.Fatalf("listener defaults = %#v", got.Listener)
	}
	if len(got.Listener.ExpectedProcessNames) == 0 {
		t.Fatalf("expected process defaults missing: %#v", got.Listener)
	}
	if got.Relay.PublicHost != "121.40.101.224" || got.Relay.MinecraftPort != 25565 {
		t.Fatalf("relay defaults = %#v", got.Relay)
	}
}

func TestInvalidListenerPortReportsConfigInvalid(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.Listener.LocalPort = 70000
	err := store.Save(cfg)
	if err == nil || err.ErrorCode != coreerrors.ConfigInvalid {
		t.Fatalf("Save() error = %v, want config_invalid", err)
	}
}

func TestRelayPublicHostCanBeOverridden(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.Relay.PublicHost = "relay.example.test"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Relay.PublicHost != "relay.example.test" {
		t.Fatalf("relay publicHost = %q", got.Relay.PublicHost)
	}
}
