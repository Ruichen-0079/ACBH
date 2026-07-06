package coreconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadOrCreateWritesDefaultConfigWhenMissing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "含 空格", "ACBH"))

	got, loadErr := store.LoadOrCreate()
	if loadErr != nil {
		t.Fatalf("LoadOrCreate() error = %v", loadErr)
	}
	if got.SchemaVersion != 2 || got.Mode == "" || got.Listener.LocalHost != "127.0.0.1" || got.Listener.LocalPort != 25565 {
		t.Fatalf("default config = %#v", got)
	}
	if _, err := os.Stat(store.Path); err != nil {
		t.Fatalf("config.json was not created: %v", err)
	}

	got.Listener.LocalPort = 25566
	if err := store.Save(got); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	again, loadErr := store.LoadOrCreate()
	if loadErr != nil {
		t.Fatalf("second LoadOrCreate() error = %v", loadErr)
	}
	if again.Listener.LocalPort != 25566 {
		t.Fatalf("LoadOrCreate() overwrote existing listener: %#v", again.Listener)
	}
}

func TestConfigAliasesMigrateToCanonicalSchema(t *testing.T) {
	store := NewStore(t.TempDir())
	raw := []byte(`{
  "vpsUrl": "http://121.40.101.224:6121",
  "privateInstanceId": "abc",
  "privateInstanceName": "私人实例",
  "deviceName": "MSI",
  "authToken": "secret"
}`)
	if err := os.WriteFile(store.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.CoordinatorURL != "http://121.40.101.224:6121" {
		t.Fatalf("coordinatorUrl = %q", got.CoordinatorURL)
	}
	if !strings.HasPrefix(got.Instance.InstanceID, "acbh_instance_") || got.Instance.DisplayName != "私人实例" {
		t.Fatalf("instance = %#v", got.Instance)
	}
	if got.Device.DisplayName != "MSI" || got.Device.DeviceID == "" {
		t.Fatalf("device = %#v", got.Device)
	}
	if got.Instance.OwnerToken != "secret" || got.Compat.LegacyHostToken != "secret" {
		t.Fatalf("tokens were not migrated: instance=%#v compat=%#v", got.Instance, got.Compat)
	}
	normalized, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(normalized) || !containsStringInBytes(normalized, `"coordinatorUrl": "http://121.40.101.224:6121"`) {
		t.Fatalf("normalized config not written: %s", string(normalized))
	}
	if containsStringInBytes(normalized, `"vpsUrl"`) || containsStringInBytes(normalized, `"authToken"`) {
		t.Fatalf("legacy aliases remained in normalized config: %s", string(normalized))
	}
}

func TestGeneratedDeviceIDIsSavedAndStable(t *testing.T) {
	store := NewStore(t.TempDir())
	raw := []byte(`{
  "schemaVersion": 2,
  "mode": "remote-public",
  "coordinatorUrl": "http://121.40.101.224:6121",
  "instance": {"displayName": "私人实例"},
  "device": {"displayName": "MSI", "platform": "windows"},
  "server": {"displayName": "Minecraft 服务端"},
  "compat": {"coordinatorProtocol": 2},
  "listener": {"enabled": true, "localHost": "127.0.0.1", "localPort": 25565},
  "relay": {"enabled": true, "coordinatorPort": 6121, "minecraftPort": 25565},
  "backup": {"profileId": "minecraft-migratable"}
}`)
	if err := os.WriteFile(store.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	first, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("first Load() error = %v", loadErr)
	}
	if first.Device.DeviceID == "" {
		t.Fatal("DeviceID was not generated")
	}
	if first.Instance.InstanceID == first.Device.DeviceID {
		t.Fatalf("instanceId and deviceId should be distinct, got %q", first.Instance.InstanceID)
	}
	if !strings.HasPrefix(first.Instance.InstanceID, "acbh_instance_") || !strings.HasPrefix(first.Device.DeviceID, "acbh_device_") {
		t.Fatalf("generated IDs should use acbh prefixes: instance=%q device=%q", first.Instance.InstanceID, first.Device.DeviceID)
	}
	if first.Compat.LegacyGroupID != first.Instance.InstanceID || first.Compat.LegacyHostID != first.Device.DeviceID {
		t.Fatalf("legacy compatibility IDs not bridged: instance=%q group=%q device=%q host=%q", first.Instance.InstanceID, first.Compat.LegacyGroupID, first.Device.DeviceID, first.Compat.LegacyHostID)
	}
	second, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("second Load() error = %v", loadErr)
	}
	if second.Device.DeviceID != first.Device.DeviceID || second.Instance.InstanceID != first.Instance.InstanceID {
		t.Fatalf("IDs changed across loads: instance %q -> %q device %q -> %q", first.Instance.InstanceID, second.Instance.InstanceID, first.Device.DeviceID, second.Device.DeviceID)
	}
}

func TestTimestampLikeDuplicateIDsAreRegeneratedDistinctly(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance.InstanceID = "60705T134739Z"
	cfg.Device.DeviceID = "60705T134739Z"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Instance.InstanceID == got.Device.DeviceID {
		t.Fatalf("duplicate IDs survived normalization: %#v %#v", got.Instance, got.Device)
	}
	if !strings.HasPrefix(got.Instance.InstanceID, "acbh_instance_") || !strings.HasPrefix(got.Device.DeviceID, "acbh_device_") {
		t.Fatalf("bad regenerated IDs: instance=%q device=%q", got.Instance.InstanceID, got.Device.DeviceID)
	}
}

func TestNonPrefixedIDsAreRegeneratedDistinctly(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance.InstanceID = "inst_123"
	cfg.Device.DeviceID = "dev_123"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Instance.InstanceID == "inst_123" || got.Device.DeviceID == "dev_123" {
		t.Fatalf("non-prefixed IDs survived normalization: %#v %#v", got.Instance, got.Device)
	}
	if got.Instance.InstanceID == got.Device.DeviceID {
		t.Fatalf("duplicate IDs after normalization: %#v %#v", got.Instance, got.Device)
	}
	if !strings.HasPrefix(got.Instance.InstanceID, "acbh_instance_") || !strings.HasPrefix(got.Device.DeviceID, "acbh_device_") {
		t.Fatalf("bad regenerated IDs: instance=%q device=%q", got.Instance.InstanceID, got.Device.DeviceID)
	}
}

func TestConfigSaveLoadPreservesChineseUTF8(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Instance.DisplayName = "私人 ACBH 实例"
	cfg.Device.DisplayName = "星星"
	cfg.Server.DisplayName = "Minecraft 服务端"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got.Instance.DisplayName != "私人 ACBH 实例" || got.Device.DisplayName != "星星" || got.Server.DisplayName != "Minecraft 服务端" {
		t.Fatalf("Chinese text changed after save/load: instance=%q device=%q server=%q", got.Instance.DisplayName, got.Device.DisplayName, got.Server.DisplayName)
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "??") {
		t.Fatalf("config contains replacement question marks: %s", string(raw))
	}
}

func TestLoadOrCreateDoesNotOverwriteBrokenJSON(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.WriteFile(store.Path, []byte(`{ bad json`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, loadErr := store.LoadOrCreate()
	if loadErr == nil || loadErr.ErrorCode != coreerrors.ConfigParseError {
		t.Fatalf("LoadOrCreate() error = %v, want config_parse_error", loadErr)
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("broken config path exists or stat failed with non-not-exist err: %v", err)
	}
	matches, err := filepath.Glob(store.Path + ".broken-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("broken backup count = %d, want 1", len(matches))
	}
}

func TestDraftConfigWithoutCoordinatorOrIdentityLoads(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := DefaultConfig()

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save(default draft) error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load(default draft) error = %v", loadErr)
	}
	if got.CoordinatorURL != "" || got.Instance.OwnerToken != "" || got.Compat.LegacyHostToken != "" {
		t.Fatalf("draft config gained coordinator or token fields: %#v", got)
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

func TestLegacyMinimalBackupDefaultsUpgradeToPhase3Defaults(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := validConfig()
	cfg.Backup = BackupConfig{
		ProfileID: "minecraft-migratable",
		Include:   legacyMinimalBackupInclude(),
		Exclude:   legacyMinimalBackupExclude(),
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	for _, want := range []string{"dir:defaultconfigs", "dir:datapacks", "dir:resourcepacks", "dir:global_packs", "dir:patchouli_books", "file:server-icon.png", "file:manifest.json", "file:HOW-TO-RUN.md"} {
		if !containsString(got.Backup.Include, want) {
			t.Fatalf("legacy minimal include was not upgraded, missing %q in %#v", want, got.Backup.Include)
		}
	}
	for _, want := range []string{"dir:versions", "dir:.cache", "dir:cache"} {
		if !containsString(got.Backup.Exclude, want) {
			t.Fatalf("legacy minimal exclude was not upgraded, missing %q in %#v", want, got.Backup.Exclude)
		}
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

func containsStringInBytes(data []byte, want string) bool {
	return strings.Contains(string(data), want)
}
