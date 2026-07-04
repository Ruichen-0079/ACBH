package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirName        = "ACBH"
	FileName       = "config.json"
	LegacyFileName = "config.yaml"
	AgentVersion   = "0.1.0"
)

type Config struct {
	CoordinatorURL string              `json:"coordinatorUrl"`
	GroupID        string              `json:"groupId"`
	MemberID       string              `json:"memberId"`
	HostID         string              `json:"hostId"`
	HostToken      string              `json:"hostToken"`
	DisplayName    string              `json:"displayName"`
	DeviceName     string              `json:"deviceName"`
	Platform       string              `json:"platform"`
	AgentVersion   string              `json:"agentVersion"`
	Server         ServerConfig        `json:"server,omitempty"`
	BackupProfile  BackupProfileConfig `json:"backupProfile,omitempty"`
}

type ServerConfig struct {
	Dir         string `json:"dir,omitempty"`
	Command     string `json:"command,omitempty"`
	LogDir      string `json:"logDir,omitempty"`
	StopTimeout string `json:"stopTimeout,omitempty"`
	LaunchPath  string `json:"launchPath,omitempty"`
	JavaPath    string `json:"javaPath,omitempty"`
	WorkingDir  string `json:"workingDir,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type BackupProfileConfig struct {
	ProfileID       string   `json:"profileId,omitempty"`
	ProfileName     string   `json:"profileName,omitempty"`
	PresetID        string   `json:"presetId,omitempty"`
	PresetName      string   `json:"presetName,omitempty"`
	ServerDir       string   `json:"serverDir,omitempty"`
	IncludeFiles    []string `json:"includeFiles,omitempty"`
	IncludeDirs     []string `json:"includeDirs,omitempty"`
	WorldRoots      []string `json:"worldRoots,omitempty"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
	MigrationNotice string   `json:"migrationNotice,omitempty"`
}

func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, FileName), nil
}

func LegacyPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, LegacyFileName), nil
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
		if errors.Is(err, os.ErrNotExist) && filepath.Base(path) == FileName {
			legacyPath := filepath.Join(filepath.Dir(path), LegacyFileName)
			data, err = os.ReadFile(legacyPath)
			if err != nil {
				return Config{}, fmt.Errorf("read config: %w", err)
			}
		} else {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
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

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp config: %w", err)
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
