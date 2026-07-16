package hobbyagent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
	"github.com/Ruichen-0079/ACBH/agent/internal/frprelay"
)

type fakeMinecraft struct {
	mu           sync.RWMutex
	state        componentstate.State
	startCount   int
	stopCount    int
	readyOnStart bool
}

func (m *fakeMinecraft) Start(context.Context, ImportedServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCount++
	if m.readyOnStart {
		m.state = componentstate.Ready
	}
	return nil
}

func (m *fakeMinecraft) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCount++
	m.state = componentstate.Stopped
	return nil
}

func (m *fakeMinecraft) Status(context.Context) MinecraftStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UTC()
	snapshot := componentstate.NewSnapshot(m.state, now, "fake", "fake")
	if m.state == componentstate.Ready {
		snapshot.LastOKAt = &now
	}
	if m.state == componentstate.Error {
		snapshot.TechnicalMessage = "minecraft exited"
	}
	return MinecraftStatus{Snapshot: snapshot, PID: 123}
}

func (m *fakeMinecraft) Diagnose(context.Context) any { return m.Status(context.Background()) }

func (m *fakeMinecraft) setState(state componentstate.State) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}

func (m *fakeMinecraft) counts() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startCount, m.stopCount
}

type fakeRelay struct {
	mu         sync.RWMutex
	status     frprelay.Status
	startCount int
	stopCount  int
	lastConfig frprelay.Config
}

func (r *fakeRelay) Start(_ context.Context, config frprelay.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCount++
	r.lastConfig = config
	now := time.Now().UTC()
	r.status = frprelay.Status{
		Snapshot:      componentstate.NewSnapshot(componentstate.Online, now, "public_probe_success", "online"),
		FRPSConnected: true, LocalReachable: true, PublicReachable: true,
	}
	r.status.LastOKAt = &now
	return nil
}

func (r *fakeRelay) Stop(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopCount++
	r.status = frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now().UTC(), "stopped", "stopped")}
	return nil
}

func (r *fakeRelay) Status() frprelay.Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *fakeRelay) Diagnose(context.Context) frprelay.Diagnosis {
	return frprelay.Diagnosis{Status: r.Status(), AccessToken: "[REDACTED]"}
}

func (r *fakeRelay) counts() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startCount, r.stopCount
}

type fakeCoordinator struct {
	mu           sync.Mutex
	info         CoordinatorInfo
	heartbeatErr error
	heartbeats   int
}

func (c *fakeCoordinator) Info(context.Context, Config) (CoordinatorInfo, error) { return c.info, nil }

func (c *fakeCoordinator) Heartbeat(context.Context, Config, Heartbeat) (CoordinatorStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heartbeats++
	return CoordinatorStatus{State: "ONLINE"}, c.heartbeatErr
}

func (c *fakeCoordinator) heartbeatCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.heartbeats
}

func newTestRuntime(t *testing.T, minecraft *fakeMinecraft, relay *fakeRelay, coordinator *fakeCoordinator) *Runtime {
	t.Helper()
	directory := t.TempDir()
	store := FileStore{
		ConfigPath: filepath.Join(directory, "config.json"),
		ImportPath: filepath.Join(directory, "import.json"),
	}
	if err := store.SaveConfig(Config{CoordinatorHost: "vps.example.test", CoordinatorPort: 6121, AccessToken: "test-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveImportedServer(ImportedServer{ServerDir: directory, JavaPath: "java", JarPath: filepath.Join(directory, "server.jar")}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeOptions{
		Store: store, Minecraft: minecraft, Relay: relay, Coordinator: coordinator,
		FRPCPath: "frpc", RuntimeDir: filepath.Join(directory, "relay"), AgentVersion: "test",
		ComponentTimeout: 40 * time.Millisecond, PollInterval: time.Millisecond,
		MonitorInterval: 2 * time.Millisecond, RelayTTL: time.Minute,
		Preflight: func(serverDir string) (PreflightResult, error) {
			return PreflightResult{ServerDir: serverDir, JavaPath: "java", JarPath: filepath.Join(serverDir, "server.jar"), EULAAccepted: true}, nil
		},
		NodeID:               "test-node",
		AutoRestartMinecraft: true, MaxMinecraftRestarts: 3, MinecraftRestartDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func defaultCoordinator() *fakeCoordinator {
	return &fakeCoordinator{info: CoordinatorInfo{
		ProtocolVersion: 1, FRPServerPort: 7000, PublicMinecraftPort: 25565,
		HeartbeatIntervalSeconds: 1,
	}}
}

func TestRelayDoesNotStartBeforeMinecraftReady(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Starting}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "FAILED")
	starts, _ := relay.counts()
	if starts != 0 {
		t.Fatalf("relay started %d time(s) before Minecraft READY", starts)
	}
}

func TestDuplicateStartDoesNotCreateTwoProcesses(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	first := runtime.Start()
	second := runtime.Start()
	if first.ID != second.ID {
		t.Fatalf("duplicate start returned different operations: %s and %s", first.ID, second.ID)
	}
	waitOperation(t, runtime, first.ID, "SUCCEEDED")
	minecraftStarts, _ := minecraft.counts()
	relayStarts, _ := relay.counts()
	if minecraftStarts != 1 || relayStarts != 1 {
		t.Fatalf("expected one Minecraft and relay start, got %d and %d", minecraftStarts, relayStarts)
	}
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func TestMinecraftCrashInvalidatesRelay(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	minecraft.setState(componentstate.Error)
	waitForCondition(t, time.Second, func() bool { return runtime.Status().Relay.State != componentstate.Online })
	if runtime.Status().OverallState == componentstate.Online {
		t.Fatal("overall status remained ONLINE after Minecraft crash")
	}
}

func TestMinecraftCrashRestartsWithinLimitAndRestoresRelay(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready, readyOnStart: true}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	minecraft.setState(componentstate.Error)
	waitForCondition(t, time.Second, func() bool {
		minecraftStarts, _ := minecraft.counts()
		relayStarts, _ := relay.counts()
		return minecraftStarts == 2 && relayStarts == 2 && runtime.Status().OverallState == componentstate.Online
	})
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func TestMinecraftRestartCanBeDisabled(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	runtime.autoRestartMinecraft = false
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	minecraft.setState(componentstate.Error)
	waitForCondition(t, time.Second, func() bool {
		return runtime.states.Components().Minecraft.ReasonCode == "restart_limit_reached"
	})
	minecraftStarts, _ := minecraft.counts()
	if minecraftStarts != 1 {
		t.Fatalf("disabled policy restarted Minecraft %d time(s)", minecraftStarts-1)
	}
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func TestMinecraftRestartStopsAtConfiguredLimit(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	runtime.maxMinecraftRestarts = 2
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	minecraft.setState(componentstate.Error)
	waitForCondition(t, time.Second, func() bool {
		return runtime.states.Components().Minecraft.ReasonCode == "restart_limit_reached"
	})
	minecraftStarts, _ := minecraft.counts()
	if minecraftStarts != 3 {
		t.Fatalf("expected initial start plus two retries, got %d starts", minecraftStarts)
	}
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func TestCoordinatorDegradedDoesNotOverrideHealthyDataPlane(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	coordinator := defaultCoordinator()
	coordinator.heartbeatErr = errors.New("temporary coordinator outage token=test-secret")
	runtime := newTestRuntime(t, minecraft, relay, coordinator)
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	waitForCondition(t, time.Second, func() bool { return runtime.Status().Coordinator.State == componentstate.Degraded })
	if runtime.Status().OverallState != componentstate.Online {
		t.Fatalf("healthy data plane was not ONLINE: %+v", runtime.Status())
	}
	diagnostics, err := json.Marshal(runtime.Diagnostics(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(diagnostics), "test-secret") {
		t.Fatal("access token leaked into diagnostics")
	}
	for _, required := range []string{
		`"operating_system"`, `"coordinator"`, `"java"`, `"eula_accepted"`,
		`"local_25565_probe"`, `"frpc"`, `"disk"`, `"recent_state_transitions"`,
	} {
		if !strings.Contains(string(diagnostics), required) {
			t.Fatalf("diagnostics are missing %s: %s", required, diagnostics)
		}
	}
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func TestStopIsIdempotent(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	start := runtime.Start()
	waitOperation(t, runtime, start.ID, "SUCCEEDED")
	first := runtime.Stop()
	waitOperation(t, runtime, first.ID, "SUCCEEDED")
	second := runtime.Stop()
	if first.ID != second.ID {
		t.Fatalf("idempotent stop returned a new operation: %s then %s", first.ID, second.ID)
	}
	_, minecraftStops := minecraft.counts()
	_, relayStops := relay.counts()
	if minecraftStops != 1 || relayStops != 1 {
		t.Fatalf("expected one stop per component, got Minecraft=%d Relay=%d", minecraftStops, relayStops)
	}
}

func TestResumeRestoresPersistedDesiredState(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	if err := runtime.store.SaveDesired(true); err != nil {
		t.Fatal(err)
	}
	operation, resumed := runtime.Resume()
	if !resumed {
		t.Fatal("persisted desired hosting state was not resumed")
	}
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	minecraftStarts, _ := minecraft.counts()
	relayStarts, _ := relay.counts()
	if minecraftStarts != 1 || relayStarts != 1 {
		t.Fatalf("resume did not safely rebuild components: Minecraft=%d Relay=%d", minecraftStarts, relayStarts)
	}
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func waitOperation(t *testing.T, runtime *Runtime, id, expected string) Operation {
	t.Helper()
	var result Operation
	waitForCondition(t, time.Second, func() bool {
		operation, ok := runtime.Operation(id)
		if !ok {
			return false
		}
		result = operation
		return operation.Status == expected
	})
	return result
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
