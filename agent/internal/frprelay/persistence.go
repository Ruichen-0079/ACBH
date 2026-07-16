package frprelay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const metadataFileName = "managed-frpc.json"

type metadata struct {
	Version            int       `json:"version"`
	Desired            bool      `json:"desired"`
	PID                int       `json:"pid,omitempty"`
	Executable         string    `json:"executable,omitempty"`
	ConfigHash         string    `json:"config_hash,omitempty"`
	ProcessFingerprint string    `json:"process_fingerprint,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func metadataPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, metadataFileName)
}

func loadMetadata(path string) (metadata, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return metadata{}, nil
	}
	if err != nil {
		return metadata{}, fmt.Errorf("read relay metadata: %w", err)
	}
	var value metadata
	if err := json.Unmarshal(data, &value); err != nil {
		return metadata{}, fmt.Errorf("parse relay metadata: %w", err)
	}
	if value.Version != 1 {
		return metadata{}, fmt.Errorf("unsupported relay metadata version %d", value.Version)
	}
	return value, nil
}

func saveMetadata(path string, value metadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create relay runtime directory: %w", err)
	}
	value.Version = 1
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode relay metadata: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".managed-frpc-*.tmp")
	if err != nil {
		return fmt.Errorf("create relay metadata temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace relay metadata: %w", err)
	}
	return nil
}

func configHash(config Config) string {
	parts := []string{
		filepath.Clean(config.FRPCPath), strings.ToLower(config.ServerHost),
		strconv.Itoa(config.ServerPort), strings.ToLower(config.LocalHost),
		strconv.Itoa(config.LocalPort), strconv.Itoa(config.RemotePort),
		strings.ToLower(config.PublicHost),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func writeTemporaryConfig(config Config) (string, error) {
	if err := os.MkdirAll(config.RuntimeDir, 0o700); err != nil {
		return "", fmt.Errorf("create relay runtime directory: %w", err)
	}
	file, err := os.CreateTemp(config.RuntimeDir, "frpc-*.toml")
	if err != nil {
		return "", fmt.Errorf("create temporary frpc config: %w", err)
	}
	path := file.Name()
	cleanup := true
	defer func() {
		file.Close()
		if cleanup {
			os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", err
	}
	contents := fmt.Sprintf(
		"serverAddr = %q\nserverPort = %d\n\n[auth]\nmethod = \"token\"\ntoken = %q\n\n[[proxies]]\nname = \"acbh-minecraft\"\ntype = \"tcp\"\nlocalIP = %q\nlocalPort = %d\nremotePort = %d\n",
		config.ServerHost, config.ServerPort, config.AccessToken,
		config.LocalHost, config.LocalPort, config.RemotePort,
	)
	if _, err := file.WriteString(contents); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	cleanup = false
	return path, nil
}
