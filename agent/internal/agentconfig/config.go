package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DirName      = "ACBH"
	FileName     = "config.yaml"
	AgentVersion = "v0.4.0-alpha5"

	DefaultServerStartTimeout = 120 * time.Second
	MinServerStartTimeout     = 10 * time.Second
)

type Config struct {
	CoordinatorURL string       `json:"coordinatorUrl"`
	GroupID        string       `json:"groupId"`
	MemberID       string       `json:"memberId"`
	HostID         string       `json:"hostId"`
	HostToken      string       `json:"hostToken"`
	DisplayName    string       `json:"displayName"`
	DeviceName     string       `json:"deviceName"`
	Platform       string       `json:"platform"`
	AgentVersion   string       `json:"agentVersion"`
	Server         ServerConfig `json:"server,omitempty"`
}

type ServerConfig struct {
	Dir          string `json:"dir,omitempty"`
	Command      string `json:"command,omitempty"`
	LogDir       string `json:"logDir,omitempty"`
	StopTimeout  string `json:"stopTimeout,omitempty"`
	StartTimeout string `json:"startTimeout,omitempty"`
}

func (s ServerConfig) ResolvedStartTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(s.StartTimeout)
	if raw == "" {
		return DefaultServerStartTimeout, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid server.startTimeout %q: %w", s.StartTimeout, err)
	}
	if parsed < MinServerStartTimeout {
		return 0, fmt.Errorf("server.startTimeout must be at least %s", MinServerStartTimeout)
	}
	return parsed, nil
}

func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, FileName), nil
}

func DefaultDir() (string, error) {
	if override := os.Getenv("ACBH_APP_DATA_DIR"); override != "" {
		return override, nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}

	return filepath.Join(dir, DirName), nil
}

func ResolveAppDataDir(executablePath string) (string, error) {
	if override := os.Getenv("ACBH_APP_DATA_DIR"); override != "" {
		return override, nil
	}

	exeDir := filepath.Dir(executablePath)
	if Exists(filepath.Join(exeDir, "portable.flag")) {
		return filepath.Join(exeDir, "data"), nil
	}

	return DefaultDir()
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	if cfg.AgentVersion == "" {
		cfg.AgentVersion = AgentVersion
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func (cfg Config) Validate() error {
	switch {
	case cfg.CoordinatorURL == "":
		return errors.New("config is missing coordinator URL")
	case cfg.GroupID == "":
		return errors.New("config is missing group ID")
	case cfg.MemberID == "":
		return errors.New("config is missing member ID")
	case cfg.HostID == "":
		return errors.New("config is missing host ID")
	case cfg.HostToken == "":
		return errors.New("config is missing host token")
	case cfg.DisplayName == "":
		return errors.New("config is missing display name")
	case cfg.DeviceName == "":
		return errors.New("config is missing device name")
	case cfg.Platform == "":
		return errors.New("config is missing platform")
	case cfg.AgentVersion == "":
		return errors.New("config is missing agent version")
	default:
		return nil
	}
}
