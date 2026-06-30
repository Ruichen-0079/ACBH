package mcserver

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
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
		LauncherPID:   123,
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
		done <- runTestSupervisor(context.Background(), SupervisorOptions{
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

func TestSupervisorCommandArgvHandlesSpacesAndRecordsLauncherPID(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	logDir := filepath.Join(t.TempDir(), "logs")
	serverDir := filepath.Join(t.TempDir(), "测试 服务端")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatal(err)
	}
	workingDir := filepath.Join(serverDir, "工作目录")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WANT_MCSERVER_HELPER", "1")
	argv := []string{os.Args[0], "-test.run=TestMCServerHelperProcess", "--", "graceful"}

	done := make(chan error, 1)
	go func() {
		done <- runTestSupervisor(context.Background(), SupervisorOptions{
			StartOptions: StartOptions{
				ServerDir:   serverDir,
				WorkingDir:  workingDir,
				CommandArgv: argv,
				LogDir:      logDir,
				RuntimeDir:  runtimeDir,
				StopTimeout: time.Second,
			},
		})
	}()

	status := waitForRunning(t, runtimeDir)
	if status.State.LauncherPID != status.State.PID || status.State.LauncherPID <= 0 {
		t.Fatalf("state PID=%d launcherPID=%d", status.State.PID, status.State.LauncherPID)
	}
	if len(status.State.CommandArgv) != len(argv) {
		t.Fatalf("CommandArgv = %#v, want %#v", status.State.CommandArgv, argv)
	}
	if status.State.WorkingDir != workingDir {
		t.Fatalf("WorkingDir = %q, want %q", status.State.WorkingDir, workingDir)
	}
	if _, stopped, err := Stop(runtimeDir); err != nil || !stopped {
		t.Fatalf("Stop() stopped=%t error=%v", stopped, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}
}

func TestSupervisorForcedKillFallback(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	serverDir := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	command := helperCommand(t, "ignore")
	done := make(chan error, 1)
	go func() {
		done <- runTestSupervisor(context.Background(), SupervisorOptions{
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
		done <- runTestSupervisor(context.Background(), SupervisorOptions{
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

func TestProcessLockAllowsOnlyOneConcurrentOwner(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	lockPath := LockPath(runtimeDir)
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	serverDirs := []string{t.TempDir(), t.TempDir()}
	for i := 0; i < 2; i++ {
		go func(index int) {
			<-start
			results <- CreateLock(lockPath, ProcessLock{
				PID:       os.Getpid(),
				Hostname:  hostname,
				CreatedAt: time.Now().UTC(),
				ServerDir: serverDirs[index],
				Nonce:     fmt.Sprintf("nonce-%d", index),
				Owner:     "starter",
			})
		}(i)
	}
	close(start)
	var successes int
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful lock acquisitions = %d, want 1", successes)
	}
	if _, err := LoadLock(lockPath); err != nil {
		t.Fatalf("LoadLock() error = %v", err)
	}
}

func TestStartFailsClosedForExistingStateAndLock(t *testing.T) {
	serverDir := t.TempDir()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	opts := StartOptions{
		ServerDir:   serverDir,
		Command:     helperCommand(t, "graceful"),
		LogDir:      filepath.Join(t.TempDir(), "logs"),
		RuntimeDir:  runtimeDir,
		StopTimeout: time.Second,
	}
	if err := SaveState(StatePath(runtimeDir), State{
		PID:           999998,
		SupervisorPID: 999999,
		ServerDir:     serverDir,
		ControlAddr:   "127.0.0.1:1",
		ControlToken:  "stale",
		StopTimeout:   "1s",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), os.Args[0], opts); err == nil || !strings.Contains(err.Error(), "repair-state") {
		t.Fatalf("Start(stale state) error = %v", err)
	}
	if _, err := LoadState(StatePath(runtimeDir)); err != nil {
		t.Fatalf("stale state was removed: %v", err)
	}

	if err := DeleteState(StatePath(runtimeDir)); err != nil {
		t.Fatal(err)
	}
	hostname, _ := os.Hostname()
	if err := CreateLock(LockPath(runtimeDir), ProcessLock{
		PID: os.Getpid(), Hostname: hostname, CreatedAt: time.Now().UTC(),
		ServerDir: serverDir, Nonce: "existing-lock", Owner: "supervisor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), os.Args[0], opts); err == nil || !strings.Contains(err.Error(), "process lock already exists") {
		t.Fatalf("Start(existing lock) error = %v", err)
	}
}

func TestConcurrentStartAllowsOneInstanceAndStopAllowsRestart(t *testing.T) {
	installTestSupervisorLauncher(t)
	serverDir := t.TempDir()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	opts := StartOptions{
		ServerDir:   serverDir,
		Command:     helperCommand(t, "graceful"),
		LogDir:      filepath.Join(t.TempDir(), "logs"),
		RuntimeDir:  runtimeDir,
		StopTimeout: time.Second,
	}

	start := make(chan struct{})
	type result struct {
		state State
		err   error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			state, err := Start(context.Background(), os.Args[0], opts)
			results <- result{state: state, err: err}
		}()
	}
	close(start)

	var succeeded State
	var successes, failures int
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err == nil {
			successes++
			succeeded = got.state
		} else {
			failures++
			if !strings.Contains(got.err.Error(), "process lock already exists") {
				t.Fatalf("losing Start() error = %v", got.err)
			}
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent Start() successes=%d failures=%d", successes, failures)
	}
	stored, err := LoadState(StatePath(runtimeDir))
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if stored.PID != succeeded.PID {
		t.Fatalf("stored PID=%d, successful PID=%d", stored.PID, succeeded.PID)
	}

	if _, stopped, err := Stop(runtimeDir); err != nil || !stopped {
		t.Fatalf("Stop() stopped=%t error=%v", stopped, err)
	}
	waitForRuntimeRemoval(t, runtimeDir)

	restarted, err := Start(context.Background(), os.Args[0], opts)
	if err != nil {
		t.Fatalf("Start(after stop) error = %v", err)
	}
	if restarted.PID <= 0 {
		t.Fatalf("restarted PID = %d", restarted.PID)
	}
	if _, stopped, err := Stop(runtimeDir); err != nil || !stopped {
		t.Fatalf("Stop(restarted) stopped=%t error=%v", stopped, err)
	}
	waitForRuntimeRemoval(t, runtimeDir)
}

func TestRepairStateOnlyRemovesConfirmedDeadProcesses(t *testing.T) {
	oldInspector := processInspector
	t.Cleanup(func() { processInspector = oldInspector })
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	serverDir := t.TempDir()
	hostname, _ := os.Hostname()
	writeStateAndLock := func() {
		t.Helper()
		if err := SaveState(StatePath(runtimeDir), State{
			PID: 101, SupervisorPID: 102, ServerDir: serverDir,
		}); err != nil {
			t.Fatal(err)
		}
		if err := CreateLock(LockPath(runtimeDir), ProcessLock{
			PID: 102, Hostname: hostname, CreatedAt: time.Now().UTC(),
			ServerDir: serverDir, Nonce: "repair", Owner: "supervisor",
		}); err != nil {
			t.Fatal(err)
		}
	}

	writeStateAndLock()
	processInspector = func(int) processState { return processAlive }
	if _, err := RepairState(runtimeDir); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("RepairState(alive) error = %v", err)
	}
	if _, err := LoadState(StatePath(runtimeDir)); err != nil {
		t.Fatalf("live state was removed: %v", err)
	}

	processInspector = func(int) processState { return processUnknown }
	if _, err := RepairState(runtimeDir); err == nil || !strings.Contains(err.Error(), "cannot be verified") {
		t.Fatalf("RepairState(unknown) error = %v", err)
	}
	if _, err := LoadState(StatePath(runtimeDir)); err != nil {
		t.Fatalf("unknown state was removed: %v", err)
	}

	processInspector = func(int) processState { return processDead }
	result, err := RepairState(runtimeDir)
	if err != nil {
		t.Fatalf("RepairState(dead) error = %v", err)
	}
	if !result.Repaired || !result.RemovedState || !result.RemovedLock {
		t.Fatalf("RepairState(dead) = %#v", result)
	}
	if _, err := LoadState(StatePath(runtimeDir)); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("state still exists: %v", err)
	}
}

func TestStatusReportsUnknownForUnverifiableLock(t *testing.T) {
	oldInspector := processInspector
	t.Cleanup(func() { processInspector = oldInspector })
	processInspector = func(int) processState { return processUnknown }
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	hostname, _ := os.Hostname()
	if err := CreateLock(LockPath(runtimeDir), ProcessLock{
		PID: 777, Hostname: hostname, CreatedAt: time.Now().UTC(),
		ServerDir: t.TempDir(), Nonce: "unknown", Owner: "supervisor",
	}); err != nil {
		t.Fatal(err)
	}
	status, err := GetStatus(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Stale || !status.Unknown || status.Running || status.Lock.PID != 777 {
		t.Fatalf("GetStatus(lock-only) = %#v", status)
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

func TestStartSupervisorProcess(t *testing.T) {
	if os.Getenv("GO_WANT_START_SUPERVISOR") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	if len(args) < 2 || args[0] != "server" || args[1] != "supervise" {
		os.Exit(2)
	}
	flags := flag.NewFlagSet("supervise", flag.ContinueOnError)
	serverDir := flags.String("server-dir", "", "")
	workingDir := flags.String("working-dir", "", "")
	command := flags.String("command", "", "")
	commandArgv := flags.String("command-argv", "", "")
	logDir := flags.String("log-dir", "", "")
	runtimeDir := flags.String("runtime-dir", "", "")
	stopTimeout := flags.Duration("stop-timeout", time.Second, "")
	lockNonce := flags.String("lock-nonce", "", "")
	if err := flags.Parse(args[2:]); err != nil {
		os.Exit(2)
	}
	err := RunSupervisor(context.Background(), SupervisorOptions{
		StartOptions: StartOptions{
			ServerDir: *serverDir, WorkingDir: *workingDir, Command: *command, LogDir: *logDir,
			CommandArgv: DecodeCommandArgv(*commandArgv),
			RuntimeDir:  *runtimeDir, StopTimeout: *stopTimeout,
		},
		LockNonce: *lockNonce,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
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

func waitForRuntimeRemoval(t *testing.T, runtimeDir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastStateErr, lastLockErr error
	for time.Now().Before(deadline) {
		_, stateErr := LoadState(StatePath(runtimeDir))
		_, lockErr := LoadLock(LockPath(runtimeDir))
		lastStateErr, lastLockErr = stateErr, lockErr
		if errors.Is(stateErr, ErrStateNotFound) && errors.Is(lockErr, ErrStateNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state or process lock was not removed: state=%v lock=%v", lastStateErr, lastLockErr)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func runTestSupervisor(ctx context.Context, opts SupervisorOptions) error {
	normalized, err := normalizeStartOptions(opts.StartOptions)
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	nonce := fmt.Sprintf("test-%d", time.Now().UnixNano())
	if err := CreateLock(LockPath(normalized.RuntimeDir), ProcessLock{
		PID:       os.Getpid(),
		Hostname:  hostname,
		CreatedAt: time.Now().UTC(),
		ServerDir: normalized.ServerDir,
		Nonce:     nonce,
		Owner:     "starter",
	}); err != nil {
		return err
	}
	opts.LockNonce = nonce
	return RunSupervisor(ctx, opts)
}

func installTestSupervisorLauncher(t *testing.T) {
	t.Helper()
	old := newSupervisorCommand
	t.Cleanup(func() { newSupervisorCommand = old })
	newSupervisorCommand = func(_ string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestStartSupervisorProcess", "--"}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = append(os.Environ(), "GO_WANT_START_SUPERVISOR=1")
		return cmd
	}
}
