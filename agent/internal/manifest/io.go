package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func MarshalPretty(m Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func LoadFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	return UnmarshalAndValidate(data)
}

func UnmarshalAndValidate(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest JSON: %w", err)
	}
	if err := Validate(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func SaveFile(path string, m Manifest) error {
	if err := Validate(m); err != nil {
		return err
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create manifest directory: %w", err)
		}
	}

	data, err := MarshalPretty(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}
