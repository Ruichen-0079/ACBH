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
	lastPort     int
}

func (m *fakeMinecraft) Start(_ context.Context, _ ImportedServer, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCount++
	m.lastPort = port
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

func (m *fakeMinecraft) Status(_ context.Context, port int) MinecraftStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	snapshot := componentstate.NewSnapshot(m.state, now, "fake", "fake")
	if m.state == componentstate.Ready {
		snapshot.LastOKAt = &now
	}
	if m.state == componentstate.Error {
		snapshot.TechnicalMessage = "minecraft exited"
	}
	m.lastPort = port
	return MinecraftStatus{Snapshot: snapshot, PID: 123}
}

func (m *fakeMinecraft) Diagnose(ctx context.Context, port int) any { return m.Status(ctx, port) }

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
	mu            sync.Mutex
	info          CoordinatorInfo
	heartbeatErr  error
	heartbeats    int
	lastHeartbeat Heartbeat
}

func (c *fakeCoordinator) Info(context.Context, Config) (CoordinatorInfo, error) { return c.info, nil }

func (c *fakeCoordinator) Heartbeat(_ context.Context, _ Config, heartbeat Heartbeat) (CoordinatorStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heartbeats++
	c.lastHeartbeat = heartbeat
	return CoordinatorStatus{State: "ONLINE"}, c.heartbeatErr
}

func (c *fakeCoordinator) heartbeatCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.heartbeats
}

func (c *fakeCoordinator) latestHeartbeat() Heartbeat {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastHeartbeat
}

func newTestRuntime(t *testing.T, minecraft *fakeMinecraft, relay *fakeRelay, coordinator *fakeCoordinator) *Runtime {
	return newTestRuntimeWithOperationLimit(t, minecraft, relay, coordinator, 0)
}

func newTestRuntimeWithOperationLimit(t *testing.T, minecraft *fakeMinecraft, relay *fakeRelay, coordinator *fakeCoordinator, operationLimit int) *Runtime {
	t.Helper()
	directory := t.TempDir()
	store := FileStore{
		ConfigPath: filepath.Join(directory, "config.json"),
		ImportPath: filepath.Join(directory, "import.json"),
	}
	if err := store.SaveConfig(Config{CoordinatorHost: "vps.example.test", CoordinatorPort: 6121, AccessToken: "test-secret", MinecraftLocalPort: 25566, PublicMinecraftPort: 25575}); err != nil {
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
		OperationHistoryLimit: operationLimit,
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

func TestConfiguredPortsDriveMinecraftRelayStatusAndHeartbeat(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	coordinator := defaultCoordinator()
	runtime := newTestRuntime(t, minecraft, relay, coordinator)
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	waitForCondition(t, time.Second, func() bool { return coordinator.heartbeatCount() > 0 })

	minecraft.mu.RLock()
	observedPort := minecraft.lastPort
	minecraft.mu.RUnlock()
	relay.mu.RLock()
	relayConfig := relay.lastConfig
	relay.mu.RUnlock()
	status := runtime.Status()
	heartbeat := coordinator.latestHeartbeat()
	if observedPort != 25566 || relayConfig.LocalPort != 25566 || relayConfig.RemotePort != 25575 {
		t.Fatalf("configured ports were not propagated: Minecraft=%d Relay=%+v", observedPort, relayConfig)
	}
	if status.PublicEndpoint != "vps.example.test:25575" || status.LocalEndpoint != "127.0.0.1:25566" {
		t.Fatalf("configured endpoints missing from status: %+v", status)
	}
	if heartbeat.MinecraftLocalPort != 25566 || heartbeat.PublicMinecraftPort != 25575 || heartbeat.PublicEndpoint != "vps.example.test:25575" {
		t.Fatalf("configured endpoints missing from heartbeat: %+v", heartbeat)
	}
	stop := runtime.Stop()
	waitOperation(t, runtime, stop.ID, "SUCCEEDED")
}

func TestPortConfigIsLockedWhileHosting(t *testing.T) {
	minecraft := &fakeMinecraft{state: componentstate.Ready}
	relay := &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}}
	runtime := newTestRuntime(t, minecraft, relay, defaultCoordinator())
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	_, err := runtime.UpdateConfig(Config{CoordinatorHost: "vps.example.test", CoordinatorPort: 6121, AccessToken: "secret", MinecraftLocalPort: 25567, PublicMinecraftPort: 25576})
	if ErrorCode(err) != CodeConfigLockedWhileRunning {
		t.Fatalf("expected %s, got %v", CodeConfigLockedWhileRunning, err)
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
	status := runtime.Status()
	if status.Minecraft.State != componentstate.Error || status.Minecraft.ReasonCode != "restart_limit_reached" {
		t.Fatalf("restart limit is not visible in runtime status: %+v", status.Minecraft)
	}
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
	status := runtime.Status()
	if status.Minecraft.State != componentstate.Error || status.Minecraft.ReasonCode != "restart_limit_reached" {
		t.Fatalf("restart limit is not visible in runtime status: %+v", status.Minecraft)
	}
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
		`"local_minecraft_probe"`, `"minecraft_local_port":25566`, `"public_minecraft_port":25575`, `"frpc"`, `"disk"`, `"recent_state_transitions"`,
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

func TestOperationHistoryDefaultsToBoundedCapacityAndPreservesNewestOrder(t *testing.T) {
	runtime := newTestRuntime(t,
		&fakeMinecraft{state: componentstate.Stopped},
		&fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}},
		defaultCoordinator(),
	)
	if runtime.operationLimit != DefaultOperationHistoryLimit {
		t.Fatalf("operation limit = %d, want %d", runtime.operationLimit, DefaultOperationHistoryLimit)
	}

	const writes = 20_000
	generated := make([]string, 0, writes)
	for index := 0; index < writes; index++ {
		runtime.mu.Lock()
		operation := runtime.newOperationLocked("stress")
		runtime.activeID, runtime.activeKind = "", ""
		runtime.mu.Unlock()
		generated = append(generated, operation.ID)
	}

	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if len(runtime.operations) != DefaultOperationHistoryLimit || len(runtime.operationOrder) != DefaultOperationHistoryLimit {
		t.Fatalf("operation history is not bounded: map=%d order=%d limit=%d", len(runtime.operations), len(runtime.operationOrder), runtime.operationLimit)
	}
	if cap(runtime.operationOrder) != DefaultOperationHistoryLimit {
		t.Fatalf("operation order capacity grew to %d", cap(runtime.operationOrder))
	}
	retained := generated[len(generated)-DefaultOperationHistoryLimit:]
	for index, id := range retained {
		if runtime.operationOrder[index] != id {
			t.Fatalf("retained order[%d] = %s, want %s", index, runtime.operationOrder[index], id)
		}
		if _, ok := runtime.operations[id]; !ok {
			t.Fatalf("newest operation %s missing from lookup map", id)
		}
	}
	if _, ok := runtime.operations[generated[0]]; ok {
		t.Fatal("oldest operation was not evicted")
	}
}

func TestOperationHistoryLimitOneEvictsAndDoesNotResurrect(t *testing.T) {
	runtime := newTestRuntimeWithOperationLimit(t,
		&fakeMinecraft{state: componentstate.Stopped},
		&fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}},
		defaultCoordinator(),
		1,
	)
	runtime.mu.Lock()
	first := runtime.newOperationLocked("first")
	second := runtime.newOperationLocked("second")
	runtime.mu.Unlock()

	if _, ok := runtime.Operation(first.ID); ok {
		t.Fatal("capacity-one history retained the oldest operation")
	}
	if operation, ok := runtime.Operation(second.ID); !ok || operation.Kind != "second" {
		t.Fatalf("newest operation missing: %+v, %v", operation, ok)
	}

	// Late callbacks from an evicted asynchronous operation must not insert it
	// back into the bounded history.
	runtime.setCurrentStep(first.ID, "late step")
	runtime.succeed(first.ID, "late success")
	runtime.fail(first.ID, errors.New("late failure"))
	if _, ok := runtime.Operation(first.ID); ok {
		t.Fatal("late callback resurrected an evicted operation")
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if len(runtime.operations) != 1 || len(runtime.operationOrder) != 1 || runtime.operationOrder[0] != second.ID {
		t.Fatalf("capacity-one history changed after late callback: map=%d order=%v", len(runtime.operations), runtime.operationOrder)
	}
}

func TestOperationHistoryRejectsNegativeLimit(t *testing.T) {
	_, err := NewRuntime(RuntimeOptions{
		Minecraft: &fakeMinecraft{}, Relay: &fakeRelay{}, Coordinator: defaultCoordinator(),
		OperationHistoryLimit: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "operation history limit") {
		t.Fatalf("negative operation history limit error = %v", err)
	}
}

func TestOperationHistoryConcurrentWritersAndReadersRemainBounded(t *testing.T) {
	const (
		limit           = 32
		writerCount     = 8
		writesPerWriter = 1_000
	)
	runtime := newTestRuntimeWithOperationLimit(t,
		&fakeMinecraft{state: componentstate.Stopped},
		&fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "idle", "idle")}},
		defaultCoordinator(),
		limit,
	)

	var waitGroup sync.WaitGroup
	for writer := 0; writer < writerCount; writer++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := 0; index < writesPerWriter; index++ {
				runtime.mu.Lock()
				operation := runtime.newOperationLocked("concurrent")
				runtime.activeID, runtime.activeKind = "", ""
				runtime.mu.Unlock()
				_, _ = runtime.Operation(operation.ID)
			}
		}()
	}
	for reader := 0; reader < writerCount; reader++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := 0; index < writesPerWriter; index++ {
				_, _ = runtime.Operation("missing-operation")
			}
		}()
	}
	waitGroup.Wait()

	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if len(runtime.operations) != limit || len(runtime.operationOrder) != limit {
		t.Fatalf("concurrent history is not bounded: map=%d order=%d", len(runtime.operations), len(runtime.operationOrder))
	}
	for _, id := range runtime.operationOrder {
		if _, ok := runtime.operations[id]; !ok {
			t.Fatalf("ordered operation %s missing after concurrent writes", id)
		}
	}
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
