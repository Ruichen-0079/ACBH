package mcserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultStartTimeout = 10 * time.Second
	controlTimeout      = 2 * time.Second
)

var processInspector = inspectProcess
var newSupervisorCommand = func(executable string, args ...string) *exec.Cmd {
	return exec.Command(executable, args...)
}

type StartOptions struct {
	ServerDir   string
	WorkingDir  string
	Command     string
	CommandArgv []string
	LogDir      string
	RuntimeDir  string
	StopTimeout time.Duration
}

type SupervisorOptions struct {
	StartOptions
	ReadyWriter io.Writer
	LockNonce   string
}

type Status struct {
	Running bool
	Stale   bool
	Unknown bool
	Reason  string
	State   State
	Lock    ProcessLock
}

type RepairResult struct {
	Repaired     bool
	RemovedState bool
	RemovedLock  bool
}

type processState uint8

const (
	processUnknown processState = iota
	processAlive
	processDead
)

type controlRequest struct {
	Token  string `json:"token"`
	Action string `json:"action"`
}

type controlResponse struct {
	OK      bool   `json:"ok"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

func Start(ctx context.Context, executable string, opts StartOptions) (State, error) {
	opts, err := normalizeStartOptions(opts)
	if err != nil {
		return State{}, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return State{}, fmt.Errorf("get hostname for server lock: %w", err)
	}
	nonce, err := randomToken()
	if err != nil {
		return State{}, err
	}
	lockPath := LockPath(opts.RuntimeDir)
	if err := CreateLock(lockPath, ProcessLock{
		PID:       os.Getpid(),
		Hostname:  hostname,
		CreatedAt: time.Now().UTC(),
		ServerDir: opts.ServerDir,
		Nonce:     nonce,
		Owner:     "starter",
	}); err != nil {
		return State{}, fmt.Errorf("%w; run `acbh-agent server repair-state` only after confirming the old server is stopped", err)
	}
	releaseReservation := true
	defer func() {
		if releaseReservation {
			deleteLockWithRetry(lockPath, nonce)
		}
	}()

	existingState, stateErr := LoadState(StatePath(opts.RuntimeDir))
	if stateErr == nil {
		resp, controlErr := sendControl(existingState, "status")
		if controlErr == nil && resp.OK && resp.Running {
			return State{}, fmt.Errorf("server is already running with PID %d", existingState.PID)
		}
		return State{}, errors.New("server state is stale or cannot be verified; refusing to start until `acbh-agent server repair-state` succeeds")
	}
	if !errors.Is(stateErr, ErrStateNotFound) {
		return State{}, fmt.Errorf("server state cannot be read; refusing to start until it is repaired: %w", stateErr)
	}

	args := []string{
		"server", "supervise",
		"--server-dir", opts.ServerDir,
		"--working-dir", opts.WorkingDir,
		"--command", opts.Command,
		"--log-dir", opts.LogDir,
		"--runtime-dir", opts.RuntimeDir,
		"--stop-timeout", opts.StopTimeout.String(),
		"--lock-nonce", nonce,
	}
	if encoded := EncodeCommandArgv(opts.CommandArgv); encoded != "" {
		args = append(args, "--command-argv", encoded)
	}
	cmd := newSupervisorCommand(executable, args...)
	configureDetachedProcess(cmd)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return State{}, fmt.Errorf("open null device: %w", err)
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return State{}, fmt.Errorf("start server supervisor: %w", err)
	}
	releaseReservation = false
	_ = cmd.Process.Release()

	deadline := time.Now().Add(defaultStartTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return State{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		status, statusErr := GetStatus(opts.RuntimeDir)
		if statusErr == nil && status.Running {
			return status.State, nil
		}
	}
	return State{}, errors.New("server supervisor did not become ready")
}

func RunSupervisor(ctx context.Context, opts SupervisorOptions) error {
	normalized, err := normalizeStartOptions(opts.StartOptions)
	if err != nil {
		return err
	}
	lockPath := LockPath(normalized.RuntimeDir)
	lock, err := LoadLock(lockPath)
	if err != nil {
		return fmt.Errorf("load server process lock: %w", err)
	}
	if opts.LockNonce == "" || lock.Nonce != opts.LockNonce || lock.ServerDir != normalized.ServerDir {
		return errors.New("server process lock ownership could not be verified")
	}
	lock.PID = os.Getpid()
	lock.Owner = "supervisor"
	if err := SaveLock(lockPath, lock); err != nil {
		return err
	}
	defer deleteLockWithRetry(lockPath, opts.LockNonce)

	var args []string
	if len(normalized.CommandArgv) > 0 {
		args = normalized.CommandArgv
	} else {
		args, err = ParseCommand(normalized.Command)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(normalized.LogDir, 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	stdoutPath := filepath.Join(normalized.LogDir, "server-stdout.log")
	stderrPath := filepath.Join(normalized.LogDir, "server-stderr.log")
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open stdout log: %w", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}
	defer stderrFile.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for server control: %w", err)
	}
	defer listener.Close()
	token, err := randomToken()
	if err != nil {
		return err
	}

	serverCmd := exec.Command(args[0], args[1:]...)
	serverCmd.Dir = normalized.WorkingDir
	serverCmd.Stdout = stdoutFile
	serverCmd.Stderr = stderrFile
	stdin, err := serverCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open server stdin: %w", err)
	}
	if err := serverCmd.Start(); err != nil {
		return fmt.Errorf("start server command: %w", err)
	}

	state := State{
		PID:           serverCmd.Process.Pid,
		SupervisorPID: os.Getpid(),
		LauncherPID:   serverCmd.Process.Pid,
		ServerDir:     normalized.ServerDir,
		WorkingDir:    normalized.WorkingDir,
		Command:       normalized.Command,
		CommandArgv:   normalized.CommandArgv,
		StartedAt:     time.Now().UTC(),
		StdoutLog:     stdoutPath,
		StderrLog:     stderrPath,
		Status:        "running",
		StopTimeout:   normalized.StopTimeout.String(),
		ControlAddr:   listener.Addr().String(),
		ControlToken:  token,
	}
	statePath := StatePath(normalized.RuntimeDir)
	if err := SaveState(statePath, state); err != nil {
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
		return err
	}
	defer deleteStateWithRetry(statePath)
	if opts.ReadyWriter != nil {
		_, _ = fmt.Fprintln(opts.ReadyWriter, "ready")
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- serverCmd.Wait() }()

	for {
		if tcp, ok := listener.(*net.TCPListener); ok {
			_ = tcp.SetDeadline(time.Now().Add(200 * time.Millisecond))
		}
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			stopped, handleErr := handleControl(conn, token, stdin, serverCmd, waitCh, normalized.StopTimeout)
			_ = conn.Close()
			if stopped {
				return handleErr
			}
		} else if !isTimeout(acceptErr) {
			return fmt.Errorf("accept server control: %w", acceptErr)
		}

		select {
		case <-ctx.Done():
			return stopProcess(stdin, serverCmd, waitCh, normalized.StopTimeout)
		case err := <-waitCh:
			return normalizeExitError(err)
		default:
		}
	}
}

func Stop(runtimeDir string) (State, bool, error) {
	state, err := LoadState(StatePath(runtimeDir))
	if errors.Is(err, ErrStateNotFound) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	resp, err := sendControl(state, "stop")
	if err != nil {
		return state, false, fmt.Errorf("server state is stale; refusing to signal PID %d: %w", state.PID, err)
	}
	if !resp.OK {
		return state, false, errors.New(resp.Error)
	}
	return state, true, nil
}

func GetStatus(runtimeDir string) (Status, error) {
	state, err := LoadState(StatePath(runtimeDir))
	if errors.Is(err, ErrStateNotFound) {
		lock, lockErr := LoadLock(LockPath(runtimeDir))
		if errors.Is(lockErr, ErrStateNotFound) {
			return Status{}, nil
		}
		if lockErr != nil {
			return Status{Stale: true, Unknown: true, Reason: lockErr.Error()}, nil
		}
		return Status{
			Stale:   true,
			Unknown: processInspector(lock.PID) == processUnknown,
			Reason:  "server process lock exists without a verifiable running state",
			Lock:    lock,
		}, nil
	}
	if err != nil {
		return Status{Stale: true, Unknown: true, Reason: err.Error()}, nil
	}
	resp, err := sendControl(state, "status")
	if err != nil || !resp.OK || !resp.Running {
		processCheck := processInspector(state.SupervisorPID)
		return Status{
			Stale:   true,
			Unknown: processCheck != processDead,
			Reason:  "server supervisor cannot be verified",
			State:   state,
		}, nil
	}
	return Status{Running: true, State: state}, nil
}

func RepairState(runtimeDir string, expectedServerDir ...string) (RepairResult, error) {
	statePath := StatePath(runtimeDir)
	lockPath := LockPath(runtimeDir)
	state, stateErr := LoadState(statePath)
	lock, lockErr := LoadLock(lockPath)
	if errors.Is(stateErr, ErrStateNotFound) && errors.Is(lockErr, ErrStateNotFound) {
		return RepairResult{}, nil
	}
	if stateErr != nil && !errors.Is(stateErr, ErrStateNotFound) {
		return RepairResult{}, fmt.Errorf("cannot repair unreadable server state automatically: %w", stateErr)
	}
	if lockErr != nil && !errors.Is(lockErr, ErrStateNotFound) {
		return RepairResult{}, fmt.Errorf("cannot repair unreadable server lock automatically: %w", lockErr)
	}
	if stateErr == nil && (state.PID <= 0 || state.SupervisorPID <= 0 || strings.TrimSpace(state.ServerDir) == "") {
		return RepairResult{}, errors.New("cannot repair incomplete server state because recorded processes cannot be verified")
	}
	if len(expectedServerDir) > 0 && strings.TrimSpace(expectedServerDir[0]) != "" {
		expected, err := filepath.Abs(expectedServerDir[0])
		if err != nil {
			return RepairResult{}, fmt.Errorf("resolve expected server directory: %w", err)
		}
		if stateErr == nil && !samePath(state.ServerDir, expected) {
			return RepairResult{}, errors.New("refusing to repair state for a different server directory")
		}
		if lockErr == nil && !samePath(lock.ServerDir, expected) {
			return RepairResult{}, errors.New("refusing to repair lock for a different server directory")
		}
	}

	if stateErr == nil {
		for _, pid := range []int{state.SupervisorPID, state.PID} {
			switch processInspector(pid) {
			case processAlive:
				return RepairResult{}, fmt.Errorf("refusing to repair state because PID %d is still running", pid)
			case processUnknown:
				return RepairResult{}, fmt.Errorf("refusing to repair state because PID %d cannot be verified", pid)
			}
		}
	}
	if lockErr == nil {
		hostname, err := os.Hostname()
		if err != nil || !strings.EqualFold(lock.Hostname, hostname) {
			return RepairResult{}, errors.New("refusing to repair lock because its owner host cannot be verified")
		}
		switch processInspector(lock.PID) {
		case processAlive:
			return RepairResult{}, fmt.Errorf("refusing to repair lock because PID %d is still running", lock.PID)
		case processUnknown:
			return RepairResult{}, fmt.Errorf("refusing to repair lock because PID %d cannot be verified", lock.PID)
		}
	}

	result := RepairResult{Repaired: true}
	if stateErr == nil {
		if err := DeleteState(statePath); err != nil {
			return RepairResult{}, err
		}
		result.RemovedState = true
	}
	if lockErr == nil {
		if err := DeleteLock(lockPath); err != nil {
			return RepairResult{}, err
		}
		result.RemovedLock = true
	}
	return result, nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func deleteLockIfNonce(path, nonce string) error {
	lock, err := LoadLock(path)
	if errors.Is(err, ErrStateNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if lock.Nonce != nonce {
		return errors.New("server process lock ownership changed")
	}
	return DeleteLock(path)
}

func deleteStateWithRetry(path string) {
	retryRuntimeCleanup(func() error { return DeleteState(path) })
}

func deleteLockWithRetry(path, nonce string) {
	retryRuntimeCleanup(func() error { return deleteLockIfNonce(path, nonce) })
}

func retryRuntimeCleanup(remove func() error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := remove(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func normalizeStartOptions(opts StartOptions) (StartOptions, error) {
	var err error
	opts.ServerDir, err = filepath.Abs(strings.TrimSpace(opts.ServerDir))
	if err != nil {
		return opts, fmt.Errorf("resolve server directory: %w", err)
	}
	info, err := os.Stat(opts.ServerDir)
	if err != nil {
		return opts, fmt.Errorf("inspect server directory: %w", err)
	}
	if !info.IsDir() {
		return opts, errors.New("server directory is not a directory")
	}
	if strings.TrimSpace(opts.WorkingDir) == "" {
		opts.WorkingDir = opts.ServerDir
	}
	if !filepath.IsAbs(opts.WorkingDir) {
		opts.WorkingDir = filepath.Join(opts.ServerDir, opts.WorkingDir)
	}
	opts.WorkingDir, err = filepath.Abs(opts.WorkingDir)
	if err != nil {
		return opts, fmt.Errorf("resolve working directory: %w", err)
	}
	workingInfo, err := os.Stat(opts.WorkingDir)
	if err != nil {
		return opts, fmt.Errorf("inspect working directory: %w", err)
	}
	if !workingInfo.IsDir() {
		return opts, errors.New("working directory is not a directory")
	}
	if len(opts.CommandArgv) > 0 {
		if opts.CommandArgv[0] == "" {
			return opts, errors.New("server command argv must contain a non-empty executable")
		}
		if opts.Command == "" {
			opts.Command = DisplayCommand(opts.CommandArgv)
		}
	} else {
		if _, err := ParseCommand(opts.Command); err != nil {
			return opts, err
		}
	}
	if opts.RuntimeDir == "" {
		return opts, errors.New("runtime directory is required")
	}
	opts.RuntimeDir, err = filepath.Abs(opts.RuntimeDir)
	if err != nil {
		return opts, fmt.Errorf("resolve runtime directory: %w", err)
	}
	if opts.LogDir == "" {
		return opts, errors.New("log directory is required")
	}
	if !filepath.IsAbs(opts.LogDir) {
		opts.LogDir = filepath.Join(opts.ServerDir, opts.LogDir)
	}
	opts.LogDir, err = filepath.Abs(opts.LogDir)
	if err != nil {
		return opts, fmt.Errorf("resolve log directory: %w", err)
	}
	if opts.StopTimeout <= 0 {
		return opts, errors.New("stop timeout must be positive")
	}
	return opts, nil
}

func handleControl(conn net.Conn, token string, stdin io.Writer, cmd *exec.Cmd, waitCh <-chan error, timeout time.Duration) (bool, error) {
	_ = conn.SetDeadline(time.Now().Add(controlTimeout))
	var req controlRequest
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		writeControlResponse(conn, controlResponse{Error: "invalid request"})
		return false, nil
	}
	if req.Token != token {
		writeControlResponse(conn, controlResponse{Error: "unauthorized"})
		return false, nil
	}
	switch req.Action {
	case "status":
		writeControlResponse(conn, controlResponse{OK: true, Running: true})
		return false, nil
	case "stop":
		_ = conn.SetDeadline(time.Now().Add(timeout + controlTimeout))
		err := stopProcess(stdin, cmd, waitCh, timeout)
		if err != nil {
			writeControlResponse(conn, controlResponse{Error: err.Error()})
			return true, err
		}
		writeControlResponse(conn, controlResponse{OK: true})
		return true, nil
	default:
		writeControlResponse(conn, controlResponse{Error: "unknown action"})
		return false, nil
	}
}

func stopProcess(stdin io.Writer, cmd *exec.Cmd, waitCh <-chan error, timeout time.Duration) error {
	if _, err := io.WriteString(stdin, "stop\n"); err != nil {
		_ = cmd.Process.Kill()
		<-waitCh
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-waitCh:
		return normalizeExitError(err)
	case <-timer.C:
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill server after graceful timeout: %w", err)
		}
		<-waitCh
		return nil
	}
}

func sendControl(state State, action string) (controlResponse, error) {
	conn, err := net.DialTimeout("tcp", state.ControlAddr, controlTimeout)
	if err != nil {
		return controlResponse{}, err
	}
	defer conn.Close()
	deadline := controlTimeout
	if action == "stop" {
		if stopTimeout, err := time.ParseDuration(state.StopTimeout); err == nil && stopTimeout > 0 {
			deadline += stopTimeout
		}
	}
	_ = conn.SetDeadline(time.Now().Add(deadline))
	if err := json.NewEncoder(conn).Encode(controlRequest{Token: state.ControlToken, Action: action}); err != nil {
		return controlResponse{}, err
	}
	var resp controlResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return controlResponse{}, err
	}
	return resp, nil
}

func writeControlResponse(writer io.Writer, resp controlResponse) {
	_ = json.NewEncoder(writer).Encode(resp)
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate control token: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func normalizeExitError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code == 0 || code == -1 {
			return nil
		}
	}
	return err
}

func DisplayCommand(argv []string) string {
	return strings.Join(argv, " ")
}

func EncodeCommandArgv(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	data, _ := json.Marshal(argv)
	return string(data)
}

func DecodeCommandArgv(raw string) []string {
	if raw == "" {
		return nil
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return nil
	}
	return argv
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
