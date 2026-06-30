package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/mcimport"
)

const desktopConfigFileName = "desktop-config.json"

type DesktopConfig struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Mode           string               `json:"mode"`
	CoordinatorURL string               `json:"coordinatorUrl,omitempty"`
	PublicEntry    string               `json:"publicEntry,omitempty"`
	LastServerDir  string               `json:"lastServerDir,omitempty"`
	LaunchProfile  DesktopLaunchProfile `json:"launchProfile,omitempty"`
	JavaPath       string               `json:"javaPath,omitempty"`
	Group          DesktopGroupConfig   `json:"group,omitempty"`
	RelayTarget    string               `json:"relayTarget,omitempty"`
	UI             DesktopUIConfig      `json:"ui"`
}

type DesktopLaunchProfile struct {
	Kind       string `json:"kind,omitempty"`
	Path       string `json:"path,omitempty"`
	ScriptType string `json:"scriptType,omitempty"`
}

type DesktopGroupConfig struct {
	GroupID  string `json:"groupId,omitempty"`
	MemberID string `json:"memberId,omitempty"`
	HostID   string `json:"hostId,omitempty"`
	Role     string `json:"role,omitempty"`
}

type DesktopUIConfig struct {
	LastCompletedStep     int  `json:"lastCompletedStep"`
	AdvancedPanelExpanded bool `json:"advancedPanelExpanded"`
}

func desktopConfigPath(opts Options) string {
	opts = withDefaults(opts)
	if isPortableAppData(opts.AppDataDir) {
		return filepath.Join(opts.AppDataDir, "config", desktopConfigFileName)
	}
	return filepath.Join(opts.AppDataDir, desktopConfigFileName)
}

func isPortableAppData(appDataDir string) bool {
	return filepath.Base(filepath.Clean(appDataDir)) == "data" && fileExists(filepath.Join(filepath.Dir(filepath.Clean(appDataDir)), "portable.flag"))
}

func defaultDesktopConfig() DesktopConfig {
	return DesktopConfig{SchemaVersion: 1, Mode: "remote-public", RelayTarget: "127.0.0.1:25565"}
}

func LoadDesktopConfig(opts Options) (DesktopConfig, error) {
	path := desktopConfigPath(opts)
	var cfg DesktopConfig
	if err := loadJSON(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return migrateDesktopConfigFromLegacy(opts), nil
		}
		return recoverDesktopConfig(path, opts), nil
	}
	cfg = migrateDesktopConfig(cfg)
	return cfg, nil
}

func SaveDesktopConfig(opts Options, cfg DesktopConfig) error {
	cfg = migrateDesktopConfig(cfg)
	cfg.CoordinatorURL = strings.TrimSpace(cfg.CoordinatorURL)
	cfg.PublicEntry = strings.TrimSpace(cfg.PublicEntry)
	return saveJSON(desktopConfigPath(opts), cfg)
}

func ForgetDesktopConfig(opts Options, secrets SecretStore) error {
	opts = withDefaults(opts)
	_ = os.Remove(desktopConfigPath(opts))
	if secrets != nil {
		for _, key := range []string{"accessKey", "hostToken", "memberToken"} {
			_ = secrets.Delete(key)
		}
	}
	return nil
}

func ResetDesktopWizard(opts Options) (DesktopConfig, error) {
	cfg, _ := LoadDesktopConfig(opts)
	cfg.UI.LastCompletedStep = 0
	cfg.LastServerDir = ""
	cfg.LaunchProfile = DesktopLaunchProfile{}
	cfg.JavaPath = ""
	if err := SaveDesktopConfig(opts, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func migrateDesktopConfig(cfg DesktopConfig) DesktopConfig {
	if cfg.SchemaVersion <= 0 {
		cfg.SchemaVersion = 1
	}
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "remote-public"
	}
	if strings.TrimSpace(cfg.RelayTarget) == "" {
		cfg.RelayTarget = "127.0.0.1:25565"
	}
	return cfg
}

func recoverDesktopConfig(path string, opts Options) DesktopConfig {
	if raw, err := os.ReadFile(path); err == nil && !json.Valid(raw) {
		_ = os.Rename(path, path+".corrupt")
	}
	return migrateDesktopConfigFromLegacy(opts)
}

func migrateDesktopConfigFromLegacy(opts Options) DesktopConfig {
	opts = withDefaults(opts)
	cfg := defaultDesktopConfig()
	if setup, err := LoadSetup(opts); err == nil {
		cfg.Mode = firstNonEmpty(setup.Mode, cfg.Mode)
		cfg.CoordinatorURL = setup.CoordinatorURL
		cfg.PublicEntry = setup.PlayerAddress
		cfg.LastServerDir = setup.ServerDir
		cfg.JavaPath = setup.JavaPath
		cfg.RelayTarget = firstNonEmpty(setup.PlayerAddress, cfg.RelayTarget)
		cfg.Group = DesktopGroupConfig{GroupID: "", MemberID: "", HostID: ""}
		cfg.LaunchProfile = desktopLaunchProfileFromMC(setup.LaunchProfile)
	}
	return cfg
}

func desktopLaunchProfileFromMC(profile mcimport.LaunchProfile) DesktopLaunchProfile {
	path := profile.ScriptPath
	if path == "" {
		path = profile.JarPath
	}
	return DesktopLaunchProfile{Kind: profile.Kind, Path: path, ScriptType: profile.ScriptType}
}

func syncDesktopConfig(opts Options, mutate func(*DesktopConfig)) error {
	cfg, _ := LoadDesktopConfig(opts)
	mutate(&cfg)
	return SaveDesktopConfig(opts, cfg)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
