package mcserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const StateFileName = "server-state.json"

var ErrStateNotFound = errors.New("server state not found")

type State struct {
	PID           int       `json:"pid"`
	SupervisorPID int       `json:"supervisorPid"`
	ServerDir     string    `json:"serverDir"`
	Command       string    `json:"command"`
	StartedAt     time.Time `json:"startedAt"`
	StdoutLog     string    `json:"stdoutLog"`
	StderrLog     string    `json:"stderrLog"`
	Status        string    `json:"status"`
	StopTimeout   string    `json:"stopTimeout"`
	ControlAddr   string    `json:"controlAddr"`
	ControlToken  string    `json:"controlToken"`
}

func StatePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, StateFileName)
}

func SaveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server state: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write server state: %w", err)
	}
	if err := replaceFile(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace server state: %w", err)
	}
	return nil
}

func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrStateNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("read server state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse server state: %w", err)
	}
	return state, nil
}

func DeleteState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete server state: %w", err)
	}
	return nil
}

func replaceFile(source, target string) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}
