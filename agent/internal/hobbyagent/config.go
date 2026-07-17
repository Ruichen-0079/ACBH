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
	CoordinatorHost     string `json:"coordinator_host"`
	CoordinatorPort     int    `json:"coordinator_port"`
	AccessToken         string `json:"access_token"`
	MinecraftLocalPort  int    `json:"minecraft_local_port"`
	PublicMinecraftPort int    `json:"public_minecraft_port"`
}

type PublicConfig struct {
	CoordinatorHost       string `json:"coordinator_host"`
	CoordinatorPort       int    `json:"coordinator_port"`
	MinecraftLocalPort    int    `json:"minecraft_local_port"`
	PublicMinecraftPort   int    `json:"public_minecraft_port"`
	AccessTokenConfigured bool   `json:"access_token_configured"`
}

func (c Config) Validate() error {
	c = c.normalized()
	if strings.TrimSpace(c.CoordinatorHost) == "" {
		return errors.New("coordinator_host is required")
	}
	if c.CoordinatorPort == 0 {
		c.CoordinatorPort = 6121
	}
	if c.CoordinatorPort < 1 || c.CoordinatorPort > 65535 {
		return errors.New("coordinator_port must be between 1 and 65535")
	}
	if c.MinecraftLocalPort < 1024 || c.MinecraftLocalPort > 65535 {
		return errors.New("minecraft_local_port must be between 1024 and 65535")
	}
	if c.PublicMinecraftPort < 1024 || c.PublicMinecraftPort > 65535 {
		return errors.New("public_minecraft_port must be between 1024 and 65535")
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
	if c.MinecraftLocalPort == 0 {
		c.MinecraftLocalPort = 25565
	}
	if c.PublicMinecraftPort == 0 {
		c.PublicMinecraftPort = 25565
	}
	return c
}

func (c Config) Public() PublicConfig {
	c = c.normalized()
	return PublicConfig{
		CoordinatorHost:       c.CoordinatorHost,
		CoordinatorPort:       c.CoordinatorPort,
		MinecraftLocalPort:    c.MinecraftLocalPort,
		PublicMinecraftPort:   c.PublicMinecraftPort,
		AccessTokenConfigured: c.AccessToken != "",
	}
}

type FileStore struct {
	ConfigPath  string
	DesiredPath string
}

func (s FileStore) LoadDesired() (bool, error) {
	var value struct {
		Desired bool `json:"desired"`
	}
	if err := readJSON(s.desiredPath(), &value); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return value.Desired, nil
}

func (s FileStore) SaveDesired(desired bool) error {
	return atomicWriteJSON(s.desiredPath(), struct {
		Desired bool `json:"desired"`
	}{Desired: desired})
}

func (s FileStore) desiredPath() string {
	if strings.TrimSpace(s.DesiredPath) != "" {
		return s.DesiredPath
	}
	return s.ConfigPath + ".desired"
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
