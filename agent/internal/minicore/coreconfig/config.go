package coreconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

const (
	FileName      = "config.json"
	LegacyDirName = "legacy"
)

type Config struct {
	SchemaVersion  int            `json:"schemaVersion"`
	Mode           string         `json:"mode"`
	CoordinatorURL string         `json:"coordinatorUrl"`
	Instance       InstanceConfig `json:"instance"`
	Device         DeviceConfig   `json:"device"`
	Server         ServerConfig   `json:"server"`
	Compat         CompatConfig   `json:"compat"`
	Listener       ListenerConfig `json:"listener"`
	Relay          RelayConfig    `json:"relay"`
	Backup         BackupConfig   `json:"backup"`
	Identity       Identity       `json:"identity,omitempty"`
}

type Identity struct {
	GroupID     string `json:"groupId"`
	MemberID    string `json:"memberId"`
	HostID      string `json:"hostId"`
	HostToken   string `json:"hostToken"`
	DisplayName string `json:"displayName"`
	DeviceName  string `json:"deviceName"`
	Platform    string `json:"platform"`
}

type InstanceConfig struct {
	InstanceID  string `json:"instanceId"`
	DisplayName string `json:"displayName"`
	OwnerToken  string `json:"ownerToken,omitempty"`
}

type DeviceConfig struct {
	DeviceID    string `json:"deviceId"`
	DisplayName string `json:"displayName"`
	Platform    string `json:"platform"`
}

type ServerConfig struct {
	ServerID    string `json:"serverId"`
	DisplayName string `json:"displayName"`
	Dir         string `json:"dir,omitempty"`
}

type CompatConfig struct {
	CoordinatorProtocol int    `json:"coordinatorProtocol"`
	LegacyGroupID       string `json:"legacyGroupId,omitempty"`
	LegacyMemberID      string `json:"legacyMemberId,omitempty"`
	LegacyHostID        string `json:"legacyHostId,omitempty"`
	LegacyHostToken     string `json:"legacyHostToken,omitempty"`
}

type ListenerConfig struct {
	Enabled                bool     `json:"enabled"`
	LocalHost              string   `json:"localHost"`
	LocalPort              int      `json:"localPort"`
	ExpectedProcessNames   []string `json:"expectedProcessNames,omitempty"`
	ServerDirMatchRequired bool     `json:"serverDirMatchRequired"`
}

type RelayConfig struct {
	Enabled         bool   `json:"enabled"`
	PublicHost      string `json:"publicHost,omitempty"`
	CoordinatorPort int    `json:"coordinatorPort"`
	MinecraftPort   int    `json:"minecraftPort"`
}

type BackupConfig struct {
	ProfileID string   `json:"profileId"`
	Include   []string `json:"include"`
	Exclude   []string `json:"exclude"`
}

type Store struct {
	AppDataDir string
	Path       string
}

type MigrationReport struct {
	Migrated      bool     `json:"migrated"`
	SourceFiles   []string `json:"sourceFiles"`
	LegacyBackups []string `json:"legacyBackups"`
	Warnings      []string `json:"warnings"`
	CreatedAt     string   `json:"createdAt"`
}

func NewStore(appDataDir string) Store {
	if appDataDir == "" {
		if dir, err := agentconfig.DefaultDir(); err == nil {
			appDataDir = dir
		}
	}
	return Store{AppDataDir: appDataDir, Path: filepath.Join(appDataDir, FileName)}
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion: 2,
		Mode:          "remote-public",
		Instance:      InstanceConfig{DisplayName: "私人 ACBH 实例"},
		Device:        DeviceConfig{Platform: runtime.GOOS},
		Server:        ServerConfig{DisplayName: "Minecraft 服务端"},
		Compat:        CompatConfig{CoordinatorProtocol: 2},
		Listener:      ListenerConfig{Enabled: true, LocalHost: "127.0.0.1", LocalPort: 25565, ExpectedProcessNames: []string{"java.exe", "javaw.exe"}},
		Relay:         RelayConfig{Enabled: true, CoordinatorPort: 6121, MinecraftPort: 25565},
		Backup: BackupConfig{
			ProfileID: "minecraft-migratable",
			Include: []string{
				"dir:world", "dir:mods", "dir:config", "dir:defaultconfigs", "dir:datapacks", "dir:resourcepacks",
				"dir:global_packs", "dir:patchouli_books",
				"file:server.properties", "file:eula.txt", "file:ops.json", "file:whitelist.json",
				"file:banned-ips.json", "file:banned-players.json", "file:server-icon.png", "file:manifest.json",
				"file:variables.txt", "file:user_jvm_args.txt", "file:start.bat", "file:start.ps1", "file:start.sh",
				"file:run.sh", "file:双击直接开服！！！.bat", "file:HOW-TO-RUN.md",
			},
			Exclude: []string{"dir:libraries", "dir:jre", "dir:logs", "dir:crash-reports", "dir:versions", "dir:.cache", "dir:cache"},
		},
	}
}

func (s Store) Load() (Config, *coreerrors.Error) {
	cfg, migrated, err := s.loadConfigJSON()
	if err == nil {
		if migrated {
			return cfg, nil
		}
		return cfg, nil
	}
	var osPathErr *os.PathError
	if !errors.As(err, &osPathErr) || !errors.Is(osPathErr.Err, os.ErrNotExist) {
		if parseErr, ok := err.(*coreerrors.Error); ok && parseErr.ErrorCode == coreerrors.ConfigParseError {
			backupBrokenConfig(s.Path)
			return Config{}, parseErr
		}
		return Config{}, normalizeConfigLoadError(err, s.Path)
	}

	cfg, report, migErr := s.MigrateLegacy()
	if migErr != nil {
		return Config{}, migErr
	}
	if report.Migrated {
		return cfg, nil
	}
	return Config{}, coreerrors.New(coreerrors.ConfigMissing, "config.json does not exist", coreerrors.Details{ConfigPath: s.Path}, "Run init or save a config.json first.")
}

func (s Store) LoadOrCreate() (Config, *coreerrors.Error) {
	cfg, err := s.Load()
	if err == nil {
		return cfg, nil
	}
	if err.ErrorCode != coreerrors.ConfigMissing {
		return Config{}, err
	}
	cfg = DefaultConfig()
	if saveErr := s.Save(cfg); saveErr != nil {
		return Config{}, saveErr
	}
	log.Printf("Created default config at %s", s.Path)
	return cfg, nil
}

func (s Store) Save(cfg Config) *coreerrors.Error {
	cfg = applyDefaults(cfg)
	if err := validate(cfg, s.Path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return coreerrors.New(coreerrors.ConfigWriteFailed, "create config directory failed", coreerrors.Details{ConfigPath: s.Path}, err.Error())
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return coreerrors.New(coreerrors.ConfigInvalid, "encode config failed", coreerrors.Details{ConfigPath: s.Path}, err.Error())
	}
	data = append(data, '\n')
	if err := atomicWriteFile(s.Path, data, 0o600); err != nil {
		return coreerrors.New(coreerrors.ConfigWriteFailed, "write config failed", coreerrors.Details{ConfigPath: s.Path}, err.Error())
	}
	return nil
}

func (s Store) MigrateLegacy() (Config, MigrationReport, *coreerrors.Error) {
	report := MigrationReport{CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	cfg := DefaultConfig()
	legacyPath := filepath.Join(s.AppDataDir, agentconfig.LegacyFileName)
	var legacy agentconfig.Config
	if loaded, err := agentconfig.Load(legacyPath); err == nil {
		legacy = loaded
		report.SourceFiles = append(report.SourceFiles, legacyPath)
	} else {
		return Config{}, report, nil
	}

	cfg.CoordinatorURL = strings.TrimSpace(legacy.CoordinatorURL)
	cfg.Identity = Identity{
		GroupID: legacy.GroupID, MemberID: legacy.MemberID, HostID: legacy.HostID, HostToken: legacy.HostToken,
		DisplayName: legacy.DisplayName, DeviceName: legacy.DeviceName, Platform: firstNonEmpty(legacy.Platform, runtime.GOOS),
	}
	cfg.Server.Dir = legacy.Server.Dir
	cfg.Backup.ProfileID = firstNonEmpty(legacy.BackupProfile.ProfileID, cfg.Backup.ProfileID)
	cfg = applyDefaults(migrateLegacyIdentity(cfg))
	if err := validate(cfg, s.Path); err != nil {
		return Config{}, report, err
	}
	if err := s.Save(cfg); err != nil {
		return Config{}, report, err
	}
	if backup, err := backupLegacyFile(s.AppDataDir, legacyPath); err == nil && backup != "" {
		report.LegacyBackups = append(report.LegacyBackups, backup)
	}
	report.Migrated = true
	_ = writeMigrationReport(s.AppDataDir, report)
	return cfg, report, nil
}

func (s Store) loadConfigJSON() (Config, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return Config{}, false, err
	}
	cfg, err := decodeConfig(data, s.Path)
	if err != nil {
		return Config{}, false, err
	}
	migrated := needsV2Migration(cfg)
	cfg = applyDefaults(cfg)
	if err := validate(cfg, s.Path); err != nil {
		return Config{}, false, err
	}
	if migrated {
		report := MigrationReport{
			Migrated:      true,
			SourceFiles:   []string{s.Path},
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			LegacyBackups: nil,
		}
		if backup, backupErr := backupConfigJSON(s.AppDataDir, s.Path); backupErr == nil && backup != "" {
			report.LegacyBackups = append(report.LegacyBackups, backup)
		} else if backupErr != nil {
			report.Warnings = append(report.Warnings, backupErr.Error())
		}
		if err := s.Save(cfg); err != nil {
			return Config{}, false, err
		}
		_ = writeMigrationReport(s.AppDataDir, report)
	}
	return cfg, migrated, nil
}

func decodeConfig(data []byte, path string) (Config, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		line, col := jsonLineColumn(data, err)
		return Config{}, coreerrors.New(coreerrors.ConfigParseError, "config.json is not valid JSON", coreerrors.Details{ConfigPath: path, Path: path, Line: line, Column: col}, "Fix JSON syntax. Use \"coordinatorUrl\": \"http://host:6121\", not YAML-style text.")
	}
	for key := range raw {
		if strings.Contains(key, ":") {
			return Config{}, coreerrors.New(coreerrors.ConfigInvalid, "config contains YAML-style key "+key, coreerrors.Details{ConfigPath: path}, "Use JSON keys such as \"coordinatorUrl\": \"http://host:6121\".")
		}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyDefaults(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 2
	}
	cfg = migrateLegacyIdentity(cfg)
	if cfg.Mode == "" {
		cfg.Mode = defaults.Mode
	}
	if cfg.Instance.DisplayName == "" {
		cfg.Instance.DisplayName = defaults.Instance.DisplayName
	}
	if cfg.Device.Platform == "" {
		cfg.Device.Platform = firstNonEmpty(cfg.Identity.Platform, defaults.Device.Platform)
	}
	if cfg.Device.DisplayName == "" {
		cfg.Device.DisplayName = firstNonEmpty(cfg.Identity.DeviceName, cfg.Identity.DisplayName)
	}
	if cfg.Server.DisplayName == "" {
		cfg.Server.DisplayName = defaults.Server.DisplayName
	}
	if cfg.Compat.CoordinatorProtocol == 0 {
		cfg.Compat.CoordinatorProtocol = defaults.Compat.CoordinatorProtocol
	}
	if cfg.Instance.InstanceID == "" {
		cfg.Instance.InstanceID = derivedID("inst", cfg.Compat.LegacyGroupID)
	}
	if cfg.Device.DeviceID == "" {
		cfg.Device.DeviceID = derivedID("dev", cfg.Compat.LegacyHostID)
	}
	if cfg.Server.ServerID == "" {
		cfg.Server.ServerID = derivedID("srv", firstNonEmpty(cfg.Server.Dir, cfg.Compat.LegacyGroupID))
	}
	if cfg.Listener.LocalHost == "" {
		cfg.Listener.LocalHost = defaults.Listener.LocalHost
	}
	if cfg.Listener.LocalPort == 0 {
		cfg.Listener.LocalPort = defaults.Listener.LocalPort
	}
	if len(cfg.Listener.ExpectedProcessNames) == 0 {
		cfg.Listener.ExpectedProcessNames = append([]string{}, defaults.Listener.ExpectedProcessNames...)
	}
	if cfg.Relay.PublicHost == "" && cfg.CoordinatorURL != "" {
		if parsed, err := url.Parse(cfg.CoordinatorURL); err == nil {
			cfg.Relay.PublicHost = parsed.Hostname()
		}
	}
	if cfg.Relay.CoordinatorPort == 0 {
		cfg.Relay.CoordinatorPort = defaults.Relay.CoordinatorPort
	}
	if cfg.Relay.MinecraftPort == 0 {
		cfg.Relay.MinecraftPort = defaults.Relay.MinecraftPort
	}
	if cfg.Backup.ProfileID == "" {
		cfg.Backup.ProfileID = defaults.Backup.ProfileID
	}
	if len(cfg.Backup.Include) == 0 || (cfg.Backup.ProfileID == defaults.Backup.ProfileID && sameStrings(cfg.Backup.Include, legacyMinimalBackupInclude())) {
		cfg.Backup.Include = append([]string{}, defaults.Backup.Include...)
	}
	if len(cfg.Backup.Exclude) == 0 || (cfg.Backup.ProfileID == defaults.Backup.ProfileID && sameStrings(cfg.Backup.Exclude, legacyMinimalBackupExclude())) {
		cfg.Backup.Exclude = append([]string{}, defaults.Backup.Exclude...)
	}
	return cfg
}

func legacyMinimalBackupInclude() []string {
	return []string{"dir:world", "dir:mods", "dir:config", "file:server.properties", "file:eula.txt", "file:ops.json", "file:whitelist.json", "file:banned-ips.json", "file:banned-players.json"}
}

func legacyMinimalBackupExclude() []string {
	return []string{"dir:libraries", "dir:jre", "dir:logs", "dir:crash-reports"}
}

func sameStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func validate(cfg Config, path string) *coreerrors.Error {
	if cfg.SchemaVersion != 2 {
		return coreerrors.New(coreerrors.ConfigInvalid, "unsupported config schemaVersion", coreerrors.Details{ConfigPath: path}, "Use schemaVersion 2.")
	}
	if strings.TrimSpace(cfg.CoordinatorURL) != "" {
		parsed, err := url.Parse(cfg.CoordinatorURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return coreerrors.New(coreerrors.ConfigInvalid, "coordinatorUrl is invalid", coreerrors.Details{ConfigPath: path, CoordinatorURL: cfg.CoordinatorURL}, "Use a full URL such as http://121.40.101.224:6121.")
		}
		host := parsed.Hostname()
		if cfg.Mode == "remote-public" && isLoopbackHost(host) {
			return coreerrors.New(coreerrors.ConfigInvalid, "remote-public mode cannot use a localhost coordinatorUrl", coreerrors.Details{ConfigPath: path, CoordinatorURL: cfg.CoordinatorURL}, "Use the VPS coordinator URL, or explicitly switch mode to local-private.")
		}
	}
	if cfg.Listener.LocalPort < 1 || cfg.Listener.LocalPort > 65535 {
		return coreerrors.New(coreerrors.ConfigInvalid, "listener.localPort must be between 1 and 65535", coreerrors.Details{ConfigPath: path}, "Set listener.localPort to the Minecraft server port.")
	}
	if strings.TrimSpace(cfg.Listener.LocalHost) == "" {
		return coreerrors.New(coreerrors.ConfigInvalid, "listener.localHost is required", coreerrors.Details{ConfigPath: path}, "Set listener.localHost to 127.0.0.1 unless you intentionally bind elsewhere.")
	}
	if cfg.Relay.MinecraftPort < 1 || cfg.Relay.MinecraftPort > 65535 {
		return coreerrors.New(coreerrors.ConfigInvalid, "relay.minecraftPort must be between 1 and 65535", coreerrors.Details{ConfigPath: path}, "Set relay.minecraftPort to the public Minecraft port.")
	}
	if cfg.Instance.InstanceID == "" || cfg.Device.DeviceID == "" || cfg.Server.ServerID == "" {
		return coreerrors.New(coreerrors.ConfigInvalid, "instanceId, deviceId and serverId are required", coreerrors.Details{ConfigPath: path}, "Save or migrate config.json before running body.")
	}
	return nil
}

func needsV2Migration(cfg Config) bool {
	return cfg.SchemaVersion != 2 || cfg.Identity.GroupID != "" || cfg.Identity.HostID != "" || cfg.Identity.HostToken != ""
}

func migrateLegacyIdentity(cfg Config) Config {
	if cfg.SchemaVersion < 2 {
		cfg.SchemaVersion = 2
	}
	if cfg.Identity.GroupID != "" && cfg.Compat.LegacyGroupID == "" {
		cfg.Compat.LegacyGroupID = cfg.Identity.GroupID
	}
	if cfg.Identity.MemberID != "" && cfg.Compat.LegacyMemberID == "" {
		cfg.Compat.LegacyMemberID = cfg.Identity.MemberID
	}
	if cfg.Identity.HostID != "" && cfg.Compat.LegacyHostID == "" {
		cfg.Compat.LegacyHostID = cfg.Identity.HostID
	}
	if cfg.Identity.HostToken != "" && cfg.Compat.LegacyHostToken == "" {
		cfg.Compat.LegacyHostToken = cfg.Identity.HostToken
	}
	if cfg.Identity.HostToken != "" && cfg.Instance.OwnerToken == "" {
		cfg.Instance.OwnerToken = cfg.Identity.HostToken
	}
	if cfg.Identity.DisplayName != "" && cfg.Instance.DisplayName == "" {
		cfg.Instance.DisplayName = "私人 ACBH 实例"
	}
	if cfg.Identity.HostID != "" && cfg.Device.DeviceID == "" {
		cfg.Device.DeviceID = cfg.Identity.HostID
	}
	if cfg.Identity.DeviceName != "" && cfg.Device.DisplayName == "" {
		cfg.Device.DisplayName = cfg.Identity.DeviceName
	}
	if cfg.Identity.Platform != "" && cfg.Device.Platform == "" {
		cfg.Device.Platform = cfg.Identity.Platform
	}
	cfg.Identity = Identity{}
	return cfg
}

func Sanitized(cfg Config) Config {
	cfg.Instance.OwnerToken = redactToken(cfg.Instance.OwnerToken)
	cfg.Compat.LegacyHostToken = redactToken(cfg.Compat.LegacyHostToken)
	cfg.Identity.HostToken = redactToken(cfg.Identity.HostToken)
	return cfg
}

func normalizeConfigLoadError(err error, path string) *coreerrors.Error {
	if e, ok := err.(*coreerrors.Error); ok {
		return e
	}
	return coreerrors.New(coreerrors.ConfigInvalid, "load config failed", coreerrors.Details{ConfigPath: path}, err.Error())
}

func backupBrokenConfig(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	backup := fmt.Sprintf("%s.broken-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	_ = os.Rename(path, backup)
}

func backupLegacyFile(appDataDir string, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", nil
	}
	legacyDir := filepath.Join(appDataDir, LegacyDirName)
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(legacyDir, filepath.Base(path)+".bak-"+time.Now().UTC().Format("20060102T150405Z"))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := atomicWriteFile(target, data, 0o600); err != nil {
		return "", err
	}
	return target, nil
}

func backupConfigJSON(appDataDir string, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", nil
	}
	legacyDir := filepath.Join(appDataDir, LegacyDirName)
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(legacyDir, "config."+time.Now().UTC().Format("20060102T150405Z")+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := atomicWriteFile(target, data, 0o600); err != nil {
		return "", err
	}
	return target, nil
}

func writeMigrationReport(appDataDir string, report MigrationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(filepath.Join(appDataDir, "migration-report.json"), data, 0o600)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func jsonLineColumn(data []byte, err error) (int, int) {
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		return 0, 0
	}
	line, col := 1, 1
	for i := int64(0); i < syntaxErr.Offset-1 && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func isLoopbackHost(host string) bool {
	lower := strings.ToLower(strings.TrimSpace(host))
	return lower == "localhost" || lower == "::1" || lower == "127.0.0.1" || strings.HasPrefix(lower, "127.")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func derivedID(prefix string, seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return prefix + "_" + time.Now().UTC().Format("20060102T150405Z")
	}
	var b strings.Builder
	for _, r := range seed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		}
	}
	value := strings.Trim(b.String(), "_-")
	if value == "" {
		value = time.Now().UTC().Format("20060102T150405Z")
	}
	if strings.HasPrefix(value, prefix+"_") {
		return value
	}
	return prefix + "_" + value
}

func redactToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	return "[redacted]"
}
