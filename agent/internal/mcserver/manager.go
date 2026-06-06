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

type StartOptions struct {
	ServerDir   string
	Command     string
	LogDir      string
	RuntimeDir  string
	StopTimeout time.Duration
}

type SupervisorOptions struct {
	StartOptions
	ReadyWriter io.Writer
}

type Status struct {
	Running bool
	Stale   bool
	State   State
}

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
	status, err := GetStatus(opts.RuntimeDir)
	if err != nil {
		return State{}, err
	}
	if status.Running {
		return State{}, fmt.Errorf("server is already running with PID %d", status.State.PID)
	}
	if status.Stale {
		if err := DeleteState(StatePath(opts.RuntimeDir)); err != nil {
			return State{}, err
		}
	}

	args := []string{
		"server", "supervise",
		"--server-dir", opts.ServerDir,
		"--command", opts.Command,
		"--log-dir", opts.LogDir,
		"--runtime-dir", opts.RuntimeDir,
		"--stop-timeout", opts.StopTimeout.String(),
	}
	cmd := exec.Command(executable, args...)
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
	args, err := ParseCommand(normalized.Command)
	if err != nil {
		return err
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
	serverCmd.Dir = normalized.ServerDir
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
		ServerDir:     normalized.ServerDir,
		Command:       normalized.Command,
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
	defer DeleteState(statePath)
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
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	resp, err := sendControl(state, "status")
	if err != nil || !resp.OK || !resp.Running {
		return Status{Stale: true, State: state}, nil
	}
	return Status{Running: true, State: state}, nil
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
	if _, err := ParseCommand(opts.Command); err != nil {
		return opts, err
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

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
