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
	cfg.Instance = InstanceConfig{InstanceID: "inst_123", DisplayName: "私人 ACBH 实例", OwnerToken: "ht_123"}
	cfg.Device = DeviceConfig{DeviceID: "dev_123", DisplayName: "MSI", Platform: "windows"}
	cfg.Server = ServerConfig{ServerID: "srv_123", DisplayName: "弥散往生1.2.4", Dir: `C:\server`}
	cfg.Compat = CompatConfig{CoordinatorProtocol: 2, LegacyGroupID: "grp_123", LegacyMemberID: "mem_123", LegacyHostID: "host_123", LegacyHostToken: "ht_123"}
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
	if got.CoordinatorURL != want.CoordinatorURL || got.Instance.OwnerToken != want.Instance.OwnerToken || got.Compat.LegacyHostToken != want.Compat.LegacyHostToken {
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
	if got.Compat.LegacyHostToken != cfg.Compat.LegacyHostToken || got.Compat.LegacyGroupID != cfg.Compat.LegacyGroupID {
		t.Fatalf("identity changed on save/load: %#v", got.Compat)
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

func TestBackupDefaultsIncludeMigratableMinecraftFiles(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.Backup = BackupConfig{}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Backup.ProfileID != "minecraft-migratable" {
		t.Fatalf("backup profileId = %q", got.Backup.ProfileID)
	}
	for _, want := range []string{"dir:world", "dir:mods", "dir:config", "file:banned-ips.json", "file:server.properties"} {
		if !containsString(got.Backup.Include, want) {
			t.Fatalf("backup include missing %q in %#v", want, got.Backup.Include)
		}
	}
	for _, want := range []string{"dir:libraries", "dir:jre", "dir:logs", "dir:crash-reports", "dir:versions"} {
		if !containsString(got.Backup.Exclude, want) {
			t.Fatalf("backup exclude missing %q in %#v", want, got.Backup.Exclude)
		}
	}
}

func TestBackupDefaultsDoNotOverwriteCustomRules(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.Backup = BackupConfig{
		ProfileID: "custom",
		Include:   []string{"dir:world"},
		Exclude:   []string{"dir:logs"},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Backup.ProfileID != "custom" || len(got.Backup.Include) != 1 || got.Backup.Include[0] != "dir:world" || len(got.Backup.Exclude) != 1 || got.Backup.Exclude[0] != "dir:logs" {
		t.Fatalf("custom backup rules overwritten: %#v", got.Backup)
	}
}

func TestOldConfigJSONMigratesToSchemaVersion2(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	old := Config{
		SchemaVersion:  1,
		Mode:           "remote-public",
		CoordinatorURL: "http://121.40.101.224:6121",
		Identity: Identity{
			GroupID: "grp_old", MemberID: "mem_old", HostID: "host_old", HostToken: "ht_old",
			DisplayName: "私人本地主机", DeviceName: "MSI", Platform: "windows",
		},
		Server: ServerConfig{Dir: `C:\server`},
	}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(store.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.SchemaVersion != 2 || got.Compat.LegacyGroupID != "grp_old" || got.Compat.LegacyHostToken != "ht_old" || got.Instance.OwnerToken != "ht_old" {
		t.Fatalf("migrated config = %#v", got)
	}
	if got.Instance.InstanceID == "" || got.Device.DeviceID == "" || got.Server.ServerID == "" {
		t.Fatalf("generated ids missing: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "migration-report.json")); err != nil {
		t.Fatalf("migration report missing: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, LegacyDirName, "config.*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("legacy config backups = %v", matches)
	}
}

func TestLegacyConfigYAMLMigratesToSchemaVersion2(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	legacy := map[string]any{
		"coordinatorUrl": "http://121.40.101.224:6121",
		"groupId":        "grp_yaml",
		"memberId":       "mem_yaml",
		"hostId":         "host_yaml",
		"hostToken":      "ht_yaml",
		"displayName":    "私人本地主机",
		"deviceName":     "MSI",
		"platform":       "windows",
		"agentVersion":   "0.1.0",
		"server":         map[string]any{"dir": `C:\server`},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.SchemaVersion != 2 || got.Compat.LegacyGroupID != "grp_yaml" || got.Compat.LegacyHostToken != "ht_yaml" {
		t.Fatalf("migrated config = %#v", got)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
