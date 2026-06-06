package takeover

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const StateFileName = "takeover-assignment.json"

var ErrStateNotFound = errors.New("takeover assignment state not found")

type State struct {
	AssignmentID                string            `json:"assignmentId"`
	TakeoverToken               string            `json:"takeoverToken"`
	CurrentHostGeneration       int               `json:"currentHostGeneration"`
	LatestArtifactsAtAssignment map[string]string `json:"latestArtifactsAtAssignment"`
	ExpiresAt                   string            `json:"expiresAt"`
}

func StatePath(configDir string) string {
	return filepath.Join(configDir, "runtime", StateFileName)
}

func SaveState(path string, state State) error {
	if state.AssignmentID == "" || state.TakeoverToken == "" {
		return errors.New("takeover assignment ID and token are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create takeover runtime directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode takeover state: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write takeover state: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace takeover state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace takeover state: %w", err)
	}
	return nil
}

func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrStateNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("read takeover state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse takeover state: %w", err)
	}
	if state.AssignmentID == "" || state.TakeoverToken == "" {
		return State{}, errors.New("takeover state is missing assignment ID or token")
	}
	return state, nil
}

func DeleteState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete takeover state: %w", err)
	}
	return nil
}
