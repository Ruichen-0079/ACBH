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
const LockFileName = "server.lock"

var ErrStateNotFound = errors.New("server state not found")

type State struct {
	PID           int       `json:"pid"`
	SupervisorPID int       `json:"supervisorPid"`
	LauncherPID   int       `json:"launcherPid,omitempty"`
	MinecraftPID  int       `json:"minecraftPid,omitempty"`
	ServerDir     string    `json:"serverDir"`
	WorkingDir    string    `json:"workingDir,omitempty"`
	Command       string    `json:"command"`
	CommandArgv   []string  `json:"commandArgv,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	StdoutLog     string    `json:"stdoutLog"`
	StderrLog     string    `json:"stderrLog"`
	Status        string    `json:"status"`
	StopTimeout   string    `json:"stopTimeout"`
	ControlAddr   string    `json:"controlAddr"`
	ControlToken  string    `json:"controlToken"`
}

type ProcessLock struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	CreatedAt time.Time `json:"createdAt"`
	ServerDir string    `json:"serverDir"`
	Nonce     string    `json:"nonce"`
	Owner     string    `json:"owner"`
}

func StatePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, StateFileName)
}

func LockPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, LockFileName)
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

func CreateLock(path string, lock ProcessLock) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server lock: %w", err)
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("server process lock already exists at %s", path)
	}
	if err != nil {
		return fmt.Errorf("create server process lock: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write server process lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("sync server process lock: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close server process lock: %w", err)
	}
	return nil
}

func LoadLock(path string) (ProcessLock, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ProcessLock{}, ErrStateNotFound
	}
	if err != nil {
		return ProcessLock{}, fmt.Errorf("read server process lock: %w", err)
	}
	var lock ProcessLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return ProcessLock{}, fmt.Errorf("parse server process lock: %w", err)
	}
	if lock.PID <= 0 || lock.Hostname == "" || lock.ServerDir == "" || lock.Nonce == "" {
		return ProcessLock{}, errors.New("server process lock is incomplete")
	}
	return lock, nil
}

func SaveLock(path string, lock ProcessLock) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server process lock: %w", err)
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open server process lock: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write server process lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync server process lock: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close server process lock: %w", err)
	}
	return nil
}

func DeleteLock(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete server process lock: %w", err)
	}
	return nil
}

func replaceFile(source, target string) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}
