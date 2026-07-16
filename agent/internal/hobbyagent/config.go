package hobbyagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	CoordinatorHost string `json:"coordinator_host"`
	CoordinatorPort int    `json:"coordinator_port"`
	AccessToken     string `json:"access_token"`
}

type PublicConfig struct {
	CoordinatorHost string `json:"coordinator_host"`
	CoordinatorPort int    `json:"coordinator_port"`
	HasAccessToken  bool   `json:"has_access_token"`
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.CoordinatorHost) == "" {
		return errors.New("coordinator_host is required")
	}
	if c.CoordinatorPort == 0 {
		c.CoordinatorPort = 6121
	}
	if c.CoordinatorPort < 1 || c.CoordinatorPort > 65535 {
		return errors.New("coordinator_port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return errors.New("access_token is required")
	}
	return nil
}

func (c Config) normalized() Config {
	c.CoordinatorHost = strings.TrimSpace(c.CoordinatorHost)
	if c.CoordinatorPort == 0 {
		c.CoordinatorPort = 6121
	}
	return c
}

func (c Config) Public() PublicConfig {
	c = c.normalized()
	return PublicConfig{
		CoordinatorHost: c.CoordinatorHost,
		CoordinatorPort: c.CoordinatorPort,
		HasAccessToken:  c.AccessToken != "",
	}
}

type ImportedServer struct {
	ServerDir string `json:"server_dir"`
	JavaPath  string `json:"java_path"`
	JarPath   string `json:"jar_path"`
}

type FileStore struct {
	ConfigPath string
	ImportPath string
}

func (s FileStore) LoadConfig() (Config, error) {
	var value Config
	if err := readJSON(s.ConfigPath, &value); err != nil {
		return Config{}, err
	}
	value = value.normalized()
	if err := value.Validate(); err != nil {
		return Config{}, err
	}
	return value, nil
}

func (s FileStore) SaveConfig(value Config) error {
	value = value.normalized()
	if err := value.Validate(); err != nil {
		return err
	}
	return atomicWriteJSON(s.ConfigPath, value)
}

func (s FileStore) LoadImportedServer() (ImportedServer, error) {
	var value ImportedServer
	if err := readJSON(s.ImportPath, &value); err != nil {
		return ImportedServer{}, err
	}
	return value, nil
}

func (s FileStore) SaveImportedServer(value ImportedServer) error {
	if strings.TrimSpace(value.ServerDir) == "" {
		return errors.New("server_dir is required")
	}
	return atomicWriteJSON(s.ImportPath, value)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".acbh-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
