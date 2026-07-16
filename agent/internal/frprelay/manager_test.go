package frprelay

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
)

type fakeLauncher struct {
	count   atomic.Int32
	start   func(int, LaunchRequest) (Process, error)
	request chan LaunchRequest
}

func (f *fakeLauncher) Start(_ context.Context, request LaunchRequest) (Process, error) {
	count := int(f.count.Add(1))
	if f.request != nil {
		f.request <- request
	}
	return f.start(count, request)
}

type fakeProcess struct {
	pid      int
	lines    chan OutputLine
	wait     chan error
	stopOnce sync.Once
}

func newBlockingProcess(pid int) *fakeProcess {
	return &fakeProcess{pid: pid, lines: make(chan OutputLine, 8), wait: make(chan error, 1)}
}

func exitedProcess(pid int, err error) *fakeProcess {
	process := newBlockingProcess(pid)
	process.wait <- err
	return process
}

func (p *fakeProcess) PID() int                 { return p.pid }
func (p *fakeProcess) Lines() <-chan OutputLine { return p.lines }
func (p *fakeProcess) Wait() <-chan error       { return p.wait }
func (p *fakeProcess) Stop(context.Context) error {
	p.stopOnce.Do(func() { p.wait <- nil })
	return nil
}
func (p *fakeProcess) Kill() error {
	p.stopOnce.Do(func() { p.wait <- errors.New("killed") })
	return nil
}

type fakeProber struct{ err error }

func (p fakeProber) Probe(context.Context, string) error { return p.err }

type recordingProber struct{ addresses chan string }

func (p recordingProber) Probe(_ context.Context, address string) error {
	p.addresses <- address
	return nil
}

type recordingSleeper struct {
	durations chan time.Duration
}

func (s recordingSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.durations <- duration:
		return nil
	}
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeInspector bool

func (value fakeInspector) Alive(int) bool { return bool(value) }

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		FRPCPath:        "frpc",
		RuntimeDir:      t.TempDir(),
		ServerHost:      "vps.example.test",
		ServerPort:      7000,
		AccessToken:     "super-secret-token",
		LocalHost:       "127.0.0.1",
		LocalPort:       25565,
		RemotePort:      25565,
		PublicHost:      "vps.example.test",
		ProbeInterval:   5 * time.Millisecond,
		ProbeTTL:        30 * time.Second,
		StopTimeout:     time.Second,
		StableResetTime: 5 * time.Minute,
	}
}

func TestManagerStartsOnlyOneFRPC(t *testing.T) {
	process := newBlockingProcess(101)
	launcher := &fakeLauncher{start: func(int, LaunchRequest) (Process, error) { return process, nil }}
	manager := NewManager(Dependencies{Launcher: launcher, Prober: fakeProber{}, Inspector: fakeInspector(false)})
	config := testConfig(t)

	if err := manager.Start(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return launcher.count.Load() == 1 })
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := launcher.count.Load(); got != 1 {
		t.Fatalf("expected one frpc launch, got %d", got)
	}
}

func TestNetworkFailureUsesBackoffSchedule(t *testing.T) {
	durations := make(chan time.Duration, 8)
	third := newBlockingProcess(203)
	launcher := &fakeLauncher{start: func(count int, _ LaunchRequest) (Process, error) {
		if count < 3 {
			return exitedProcess(200+count, errors.New("dial tcp: network unreachable")), nil
		}
		return third, nil
	}}
	manager := NewManager(Dependencies{
		Launcher: launcher, Prober: fakeProber{}, Sleeper: recordingSleeper{durations: durations}, Inspector: fakeInspector(false),
	})
	if err := manager.Start(context.Background(), testConfig(t)); err != nil {
		t.Fatal(err)
	}

	first := receiveDuration(t, durations)
	second := receiveDuration(t, durations)
	if first != time.Second || second != 2*time.Second {
		t.Fatalf("unexpected retry delays: %s, %s", first, second)
	}
	waitFor(t, time.Second, func() bool { return launcher.count.Load() == 3 })
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationFailureDoesNotRetry(t *testing.T) {
	durations := make(chan time.Duration, 1)
	launcher := &fakeLauncher{start: func(int, LaunchRequest) (Process, error) {
		return nil, errors.New("authentication failed: invalid token")
	}}
	manager := NewManager(Dependencies{
		Launcher: launcher, Prober: fakeProber{}, Sleeper: recordingSleeper{durations: durations}, Inspector: fakeInspector(false),
	})
	if err := manager.Start(context.Background(), testConfig(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return manager.Status().Terminal })
	if launcher.count.Load() != 1 {
		t.Fatalf("authentication failure launched %d processes", launcher.count.Load())
	}
	select {
	case delay := <-durations:
		t.Fatalf("authentication failure unexpectedly retried after %s", delay)
	default:
	}
}

func TestPortConflictDoesNotCreateRestartStorm(t *testing.T) {
	launcher := &fakeLauncher{start: func(int, LaunchRequest) (Process, error) {
		return nil, errors.New("remote port 25565 already in use")
	}}
	manager := NewManager(Dependencies{Launcher: launcher, Prober: fakeProber{}, Inspector: fakeInspector(false)})
	if err := manager.Start(context.Background(), testConfig(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return manager.Status().Terminal })
	time.Sleep(20 * time.Millisecond)
	if launcher.count.Load() != 1 {
		t.Fatalf("port conflict launched %d processes", launcher.count.Load())
	}
	if manager.Status().ReasonCode != "PUBLIC_PORT_IN_USE" {
		t.Fatalf("unexpected reason %q", manager.Status().ReasonCode)
	}
}

func TestGeneratedConfigAndProbesUseConfiguredPorts(t *testing.T) {
	config := testConfig(t)
	config.LocalPort = 25566
	config.RemotePort = 25575
	path, err := writeTemporaryConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "localPort = 25566") || !strings.Contains(string(contents), "remotePort = 25575") {
		t.Fatalf("generated config has wrong ports: %s", contents)
	}

	process := newBlockingProcess(302)
	launcher := &fakeLauncher{start: func(int, LaunchRequest) (Process, error) { return process, nil }}
	addresses := make(chan string, 4)
	manager := NewManager(Dependencies{Launcher: launcher, Prober: recordingProber{addresses: addresses}, Inspector: fakeInspector(false)})
	if err := manager.Start(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	process.lines <- OutputLine{Stream: "stdout", Line: "login to server success", Time: time.Now().UTC()}
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case address := <-addresses:
			seen[address] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for configured probes: %+v", seen)
		}
	}
	if !seen["127.0.0.1:25566"] || !seen["vps.example.test:25575"] {
		t.Fatalf("unexpected probe addresses: %+v", seen)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineRequiresConnectionAndBothProbes(t *testing.T) {
	process := newBlockingProcess(301)
	launcher := &fakeLauncher{start: func(int, LaunchRequest) (Process, error) { return process, nil }}
	manager := NewManager(Dependencies{Launcher: launcher, Prober: fakeProber{}, Inspector: fakeInspector(false)})
	if err := manager.Start(context.Background(), testConfig(t)); err != nil {
		t.Fatal(err)
	}
	process.lines <- OutputLine{Stream: "stdout", Line: "login to server success", Time: time.Now().UTC()}
	waitFor(t, time.Second, func() bool { return manager.Status().State == componentstate.Online })
	status := manager.Status()
	if !status.FRPSConnected || !status.LocalReachable || !status.PublicReachable || status.LastOKAt == nil {
		t.Fatalf("incomplete online evidence: %+v", status)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartSafelyRebuildsDeadManagedRelay(t *testing.T) {
	config := testConfig(t)
	old := metadata{
		Version: 1, Desired: true, PID: 98765, Executable: config.FRPCPath,
		ConfigHash: configHash(config), UpdatedAt: time.Now().Add(-time.Minute),
	}
	if err := saveMetadata(metadataPath(config.RuntimeDir), old); err != nil {
		t.Fatal(err)
	}
	process := newBlockingProcess(401)
	launcher := &fakeLauncher{start: func(int, LaunchRequest) (Process, error) { return process, nil }}
	manager := NewManager(Dependencies{Launcher: launcher, Prober: fakeProber{}, Inspector: fakeInspector(false)})
	if err := manager.Reconcile(context.Background(), true, config); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return launcher.count.Load() == 1 })
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTokenIsAbsentFromMetadataEventsAndDiagnostics(t *testing.T) {
	requestChannel := make(chan LaunchRequest, 1)
	process := newBlockingProcess(501)
	launcher := &fakeLauncher{
		request: requestChannel,
		start:   func(int, LaunchRequest) (Process, error) { return process, nil },
	}
	manager := NewManager(Dependencies{Launcher: launcher, Prober: fakeProber{}, Inspector: fakeInspector(false)})
	config := testConfig(t)
	if err := manager.Start(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	request := <-requestChannel
	contents, err := os.ReadFile(request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(contents), config.AccessToken) {
		t.Fatal("generated frpc config did not contain the required token")
	}
	process.lines <- OutputLine{Stream: "stderr", Line: "request rejected token=" + config.AccessToken, Time: time.Now().UTC()}
	waitFor(t, time.Second, func() bool { return len(manager.Diagnose(context.Background()).RecentEvents) >= 2 })

	diagnostics, err := json.Marshal(manager.Diagnose(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(config.RuntimeDir, metadataFileName))
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(diagnostics), config.AccessToken) || contains(string(metadataBytes), config.AccessToken) {
		t.Fatal("token leaked into diagnostics or managed metadata")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		_, statErr := os.Stat(request.ConfigPath)
		return errors.Is(statErr, os.ErrNotExist)
	})
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func receiveDuration(t *testing.T, values <-chan time.Duration) time.Duration {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for retry delay")
		return 0
	}
}

func contains(value, part string) bool {
	return len(part) > 0 && len(value) >= len(part) && stringContains(value, part)
}

func stringContains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
