package mcserver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseCommand(t *testing.T) {
	args, err := ParseCommand(`java -Xmx4G -jar "server file.jar" nogui`)
	if err != nil {
		t.Fatalf("ParseCommand() error = %v", err)
	}
	want := []string{"java", "-Xmx4G", "-jar", "server file.jar", "nogui"}
	if fmt.Sprint(args) != fmt.Sprint(want) {
		t.Fatalf("ParseCommand() = %#v, want %#v", args, want)
	}

	windows, err := ParseCommand(`"C:\Program Files\Java\bin\java.exe" -jar server.jar`)
	if err != nil {
		t.Fatalf("ParseCommand(windows) error = %v", err)
	}
	if windows[0] != `C:\Program Files\Java\bin\java.exe` {
		t.Fatalf("Windows executable = %q", windows[0])
	}
}

func TestParseCommandRejectsUnterminatedQuote(t *testing.T) {
	if _, err := ParseCommand(`java -jar "server.jar`); err == nil {
		t.Fatal("ParseCommand() error = nil")
	}
}

func TestStateSaveLoadDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", StateFileName)
	want := State{
		PID:           123,
		SupervisorPID: 456,
		ServerDir:     t.TempDir(),
		Command:       "java -jar server.jar",
		StartedAt:     time.Now().UTC().Truncate(time.Second),
		StdoutLog:     "stdout.log",
		StderrLog:     "stderr.log",
		Status:        "running",
		StopTimeout:   "30s",
		ControlAddr:   "127.0.0.1:12345",
		ControlToken:  "secret",
	}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("LoadState() = %#v, want %#v", got, want)
	}
	if err := DeleteState(path); err != nil {
		t.Fatalf("DeleteState() error = %v", err)
	}
	if _, err := LoadState(path); err != ErrStateNotFound {
		t.Fatalf("LoadState() error = %v, want ErrStateNotFound", err)
	}
}

func TestSupervisorGracefulStopCreatesLogsAndClearsState(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	logDir := filepath.Join(t.TempDir(), "logs")
	serverDir := t.TempDir()
	command := helperCommand(t, "graceful")

	done := make(chan error, 1)
	go func() {
		done <- RunSupervisor(context.Background(), SupervisorOptions{
			StartOptions: StartOptions{
				ServerDir:   serverDir,
				Command:     command,
				LogDir:      logDir,
				RuntimeDir:  runtimeDir,
				StopTimeout: time.Second,
			},
		})
	}()

	status := waitForRunning(t, runtimeDir)
	if status.State.PID <= 0 {
		t.Fatalf("PID = %d", status.State.PID)
	}
	_, stopped, err := Stop(runtimeDir)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped {
		t.Fatal("Stop() stopped = false")
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}
	waitForStateRemoval(t, runtimeDir)

	stdout := readTestFile(t, filepath.Join(logDir, "server-stdout.log"))
	stderr := readTestFile(t, filepath.Join(logDir, "server-stderr.log"))
	if !strings.Contains(stdout, "helper stdout") || !strings.Contains(stdout, "received stop") {
		t.Fatalf("stdout log = %q", stdout)
	}
	if !strings.Contains(stderr, "helper stderr") {
		t.Fatalf("stderr log = %q", stderr)
	}
}

func TestSupervisorForcedKillFallback(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	serverDir := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	command := helperCommand(t, "ignore")
	done := make(chan error, 1)
	go func() {
		done <- RunSupervisor(context.Background(), SupervisorOptions{
			StartOptions: StartOptions{
				ServerDir:   serverDir,
				Command:     command,
				LogDir:      logDir,
				RuntimeDir:  runtimeDir,
				StopTimeout: 100 * time.Millisecond,
			},
		})
	}()
	waitForRunning(t, runtimeDir)
	started := time.Now()
	if _, stopped, err := Stop(runtimeDir); err != nil || !stopped {
		t.Fatalf("Stop() stopped=%t error=%v", stopped, err)
	}
	if elapsed := time.Since(started); elapsed < 75*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("forced stop elapsed = %s", elapsed)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}
}

func TestStopWaitsForConfiguredGracefulTimeout(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	serverDir := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	command := helperCommand(t, "slow")
	done := make(chan error, 1)
	go func() {
		done <- RunSupervisor(context.Background(), SupervisorOptions{
			StartOptions: StartOptions{
				ServerDir:   serverDir,
				Command:     command,
				LogDir:      logDir,
				RuntimeDir:  runtimeDir,
				StopTimeout: 3 * time.Second,
			},
		})
	}()
	waitForRunning(t, runtimeDir)
	started := time.Now()
	if _, stopped, err := Stop(runtimeDir); err != nil || !stopped {
		t.Fatalf("Stop() stopped=%t error=%v", stopped, err)
	}
	if elapsed := time.Since(started); elapsed < 2*time.Second || elapsed > 3*time.Second {
		t.Fatalf("graceful stop elapsed = %s", elapsed)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}
}

func TestStatusMissingAndStaleState(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	status, err := GetStatus(runtimeDir)
	if err != nil || status.Running || status.Stale {
		t.Fatalf("missing GetStatus() = %#v, %v", status, err)
	}

	if err := SaveState(StatePath(runtimeDir), State{
		PID:          999999,
		ControlAddr:  "127.0.0.1:1",
		ControlToken: "stale",
	}); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	status, err = GetStatus(runtimeDir)
	if err != nil {
		t.Fatalf("stale GetStatus() error = %v", err)
	}
	if !status.Stale || status.Running {
		t.Fatalf("stale GetStatus() = %#v", status)
	}
	if _, _, err := Stop(runtimeDir); err == nil || !strings.Contains(err.Error(), "refusing to signal PID") {
		t.Fatalf("Stop(stale) error = %v", err)
	}
}

func TestMCServerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCSERVER_HELPER") != "1" {
		return
	}
	mode := "graceful"
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}
	fmt.Fprintln(os.Stdout, "helper stdout")
	fmt.Fprintln(os.Stderr, "helper stderr")
	if mode == "ignore" {
		for {
			time.Sleep(time.Second)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if scanner.Text() == "stop" {
			if mode == "slow" {
				time.Sleep(2200 * time.Millisecond)
			}
			fmt.Fprintln(os.Stdout, "received stop")
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func helperCommand(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv("GO_WANT_MCSERVER_HELPER", "1")
	return strconv.Quote(os.Args[0]) + " -test.run=TestMCServerHelperProcess -- " + mode
}

func waitForRunning(t *testing.T, runtimeDir string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := GetStatus(runtimeDir)
		if err == nil && status.Running {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not reach running state")
	return Status{}
}

func waitForStateRemoval(t *testing.T, runtimeDir string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := LoadState(StatePath(runtimeDir)); err == ErrStateNotFound {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("state file was not removed")
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}
