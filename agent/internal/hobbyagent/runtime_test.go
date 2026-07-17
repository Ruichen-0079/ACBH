package hobbyagent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
	"github.com/Ruichen-0079/ACBH/agent/internal/frprelay"
)

type fakeProbe struct {
	mu    sync.RWMutex
	state componentstate.State
	port  int
	count int
}

func (p *fakeProbe) Status(_ context.Context, port int) LocalServerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.port, p.count = port, p.count+1
	now := time.Now().UTC()
	message := "未检测到本地服务器"
	if p.state == componentstate.Ready {
		message = "已检测到本地服务器"
	}
	snapshot := componentstate.NewSnapshot(p.state, now, "fake_probe", message)
	if p.state == componentstate.Ready {
		snapshot.LastOKAt = &now
	}
	return LocalServerStatus{Snapshot: snapshot}
}

func (p *fakeProbe) setState(state componentstate.State) {
	p.mu.Lock()
	p.state = state
	p.mu.Unlock()
}

func (p *fakeProbe) observedPort() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.port
}

type fakeRelay struct {
	mu         sync.RWMutex
	status     frprelay.Status
	startCount int
	stopCount  int
	lastConfig frprelay.Config
}

func newFakeRelay() *fakeRelay {
	return &fakeRelay{status: frprelay.Status{Snapshot: componentstate.NewSnapshot(componentstate.Offline, time.Now(), "not_started", "offline")}}
}

func (r *fakeRelay) Start(_ context.Context, config frprelay.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCount++
	r.lastConfig = config
	now := time.Now().UTC()
	r.status = frprelay.Status{
		Snapshot:      componentstate.NewSnapshot(componentstate.Online, now, "public_probe_success", "online"),
		FRPSConnected: true, LocalReachable: false, PublicReachable: true,
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
	return frprelay.Diagnosis{Status: r.Status(), Desired: r.Status().State != componentstate.Offline, AccessToken: "[REDACTED]"}
}

func (r *fakeRelay) counts() (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startCount, r.stopCount
}

func (r *fakeRelay) setStatus(status frprelay.Status) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

type fakeCoordinator struct {
	mu            sync.Mutex
	info          CoordinatorInfo
	infoErr       error
	heartbeatErr  error
	heartbeats    int
	lastHeartbeat Heartbeat
}

func (c *fakeCoordinator) Info(context.Context, Config) (CoordinatorInfo, error) {
	return c.info, c.infoErr
}

func (c *fakeCoordinator) Heartbeat(_ context.Context, _ Config, heartbeat Heartbeat) (CoordinatorStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heartbeats++
	c.lastHeartbeat = heartbeat
	return CoordinatorStatus{State: "ONLINE"}, c.heartbeatErr
}

func (c *fakeCoordinator) latestHeartbeat() Heartbeat {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastHeartbeat
}

func defaultCoordinator() *fakeCoordinator {
	return &fakeCoordinator{info: CoordinatorInfo{
		ProtocolVersion: 1, FRPServerPort: 7000, PublicMinecraftPort: 25565,
		HeartbeatIntervalSeconds: 3600,
	}}
}

func newTestRuntime(t *testing.T, probe *fakeProbe, relay *fakeRelay, coordinator *fakeCoordinator) *Runtime {
	return newTestRuntimeWithOperationLimit(t, probe, relay, coordinator, 0)
}

func newTestRuntimeWithOperationLimit(t *testing.T, probe *fakeProbe, relay *fakeRelay, coordinator *fakeCoordinator, operationLimit int) *Runtime {
	t.Helper()
	directory := t.TempDir()
	store := FileStore{
		ConfigPath:  filepath.Join(directory, "config.json"),
		DesiredPath: filepath.Join(directory, "desired.json"),
	}
	if err := store.SaveConfig(Config{
		CoordinatorHost: "vps.example.test", CoordinatorPort: 6121,
		AccessToken: "test-secret", MinecraftLocalPort: 25566, PublicMinecraftPort: 25575,
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeOptions{
		Store: store, Probe: probe, Relay: relay, Coordinator: coordinator,
		FRPCPath: "frpc", RuntimeDir: filepath.Join(directory, "relay"),
		LogDir: filepath.Join(directory, "logs"), AgentVersion: "test",
		ComponentTimeout: 100 * time.Millisecond, PollInterval: time.Millisecond,
		MonitorInterval: 2 * time.Millisecond, RelayTTL: time.Minute,
		NodeID: "test-node", OperationHistoryLimit: operationLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestStartKeepsRelayOnlineWhenLocalServerOffline(t *testing.T) {
	probe := &fakeProbe{state: componentstate.Offline}
	relay := newFakeRelay()
	runtime := newTestRuntime(t, probe, relay, defaultCoordinator())
	operation := runtime.Start()
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")

	status := runtime.Status()
	if status.LocalServer.State != componentstate.Offline {
		t.Fatalf("local server = %s, want OFFLINE", status.LocalServer.State)
	}
	if status.Relay.State != componentstate.Online || status.OverallState != componentstate.Online {
		t.Fatalf("offline local server degraded relay: relay=%s overall=%s", status.Relay.State, status.OverallState)
	}
	starts, stops := relay.counts()
	if starts != 1 || stops != 0 {
		t.Fatalf("relay lifecycle = start %d stop %d", starts, stops)
	}
	if probe.observedPort() != 25566 || relay.lastConfig.LocalPort != 25566 || relay.lastConfig.RemotePort != 25575 {
		t.Fatalf("configured ports not propagated: probe=%d relay=%+v", probe.observedPort(), relay.lastConfig)
	}
}

func TestLocalServerTransitionsNeverRestartOrStopRelay(t *testing.T) {
	probe := &fakeProbe{state: componentstate.Offline}
	relay := newFakeRelay()
	runtime := newTestRuntime(t, probe, relay, defaultCoordinator())
	waitOperation(t, runtime, runtime.Start().ID, "SUCCEEDED")

	probe.setState(componentstate.Ready)
	waitFor(t, func() bool { return runtime.Status().LocalServer.State == componentstate.Ready })
	probe.setState(componentstate.Offline)
	waitFor(t, func() bool { return runtime.Status().LocalServer.State == componentstate.Offline })
	starts, stops := relay.counts()
	if starts != 1 || stops != 0 {
		t.Fatalf("local probe transition changed relay lifecycle: start=%d stop=%d", starts, stops)
	}
}

func TestStopOnlyStopsRelayAndIsIdempotent(t *testing.T) {
	probe := &fakeProbe{state: componentstate.Ready}
	relay := newFakeRelay()
	runtime := newTestRuntime(t, probe, relay, defaultCoordinator())
	waitOperation(t, runtime, runtime.Start().ID, "SUCCEEDED")
	first := runtime.Stop()
	second := runtime.Stop()
	if first.ID != second.ID {
		t.Fatalf("duplicate stop operation: %s != %s", first.ID, second.ID)
	}
	waitOperation(t, runtime, first.ID, "SUCCEEDED")
	starts, stops := relay.counts()
	if starts != 1 || stops != 1 {
		t.Fatalf("relay lifecycle = start %d stop %d", starts, stops)
	}
	if runtime.Status().LocalServer.State != componentstate.Ready {
		t.Fatal("stopping relay changed the user-owned local server state")
	}
}

func TestCoordinatorHeartbeatFailureDoesNotStopHealthyDataPlane(t *testing.T) {
	coordinator := defaultCoordinator()
	coordinator.heartbeatErr = errors.New("coordinator unavailable")
	relay := newFakeRelay()
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, relay, coordinator)
	waitOperation(t, runtime, runtime.Start().ID, "SUCCEEDED")
	waitFor(t, func() bool { return runtime.Status().Coordinator.State == componentstate.Degraded })
	status := runtime.Status()
	if status.Relay.State != componentstate.Online || status.OverallState != componentstate.Online {
		t.Fatalf("Coordinator failure closed data plane: %+v", status)
	}
	_, stops := relay.counts()
	if stops != 0 {
		t.Fatalf("Coordinator failure stopped relay %d time(s)", stops)
	}
}

func TestHeartbeatUsesProbeSnapshotWithoutProcessData(t *testing.T) {
	coordinator := defaultCoordinator()
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, newFakeRelay(), coordinator)
	waitOperation(t, runtime, runtime.Start().ID, "SUCCEEDED")
	waitFor(t, func() bool { return coordinator.latestHeartbeat().NodeID != "" })
	heartbeat := coordinator.latestHeartbeat()
	if heartbeat.Minecraft.State != componentstate.Offline || heartbeat.MinecraftLocalPort != 25566 || heartbeat.PublicMinecraftPort != 25575 {
		t.Fatalf("unexpected heartbeat: %+v", heartbeat)
	}
	encoded, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"pid", "java", "server_dir", "jar", "restart"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("heartbeat contains managed Minecraft field %q: %s", forbidden, encoded)
		}
	}
}

func TestConfigPreservesStoredTokenButNeverReturnsIt(t *testing.T) {
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, newFakeRelay(), defaultCoordinator())
	public, err := runtime.UpdateConfig(Config{
		CoordinatorHost: "vps.example.test", CoordinatorPort: 6121,
		MinecraftLocalPort: 25566, PublicMinecraftPort: 25575,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !public.AccessTokenConfigured {
		t.Fatal("stored token was not preserved")
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "test-secret") || strings.Contains(string(encoded), "access_token\"") {
		t.Fatalf("public config leaked token: %s", encoded)
	}
}

func TestTerminalRelayErrorAllowsCorrectiveConfiguration(t *testing.T) {
	relay := newFakeRelay()
	relay.setStatus(frprelay.Status{
		Snapshot: componentstate.NewSnapshot(componentstate.Error, time.Now().UTC(), CodePublicPortInUse, "端口被占用"),
		Terminal: true,
	})
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, relay, defaultCoordinator())
	public, err := runtime.UpdateConfig(Config{
		CoordinatorHost: "vps.example.test", CoordinatorPort: 6121,
		MinecraftLocalPort: 25577, PublicMinecraftPort: 25577,
	})
	if err != nil {
		t.Fatalf("terminal relay error blocked corrective configuration: %v", err)
	}
	if public.MinecraftLocalPort != 25577 || public.PublicMinecraftPort != 25577 || !public.AccessTokenConfigured {
		t.Fatalf("corrective configuration was not stored safely: %+v", public)
	}
}

func TestRuntimeDiagnosticsExcludeManagedMinecraftAndSecrets(t *testing.T) {
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, newFakeRelay(), defaultCoordinator())
	diagnostics := runtime.Diagnostics(context.Background())
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"test-secret", "server_dir", "server_jar", "eula", "java", "session", "lease", "remote-public", "current_host", "tunnel"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics contain forbidden value %q: %s", forbidden, encoded)
		}
	}
}

func TestShutdownStopsRelayButPreservesDesiredState(t *testing.T) {
	relay := newFakeRelay()
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, relay, defaultCoordinator())
	waitOperation(t, runtime, runtime.Start().ID, "SUCCEEDED")
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	desired, err := runtime.store.LoadDesired()
	if err != nil || !desired {
		t.Fatalf("service shutdown lost desired relay state: desired=%v err=%v", desired, err)
	}
	_, stops := relay.counts()
	if stops != 1 {
		t.Fatalf("service shutdown stopped relay %d times", stops)
	}
}

func TestResumeRestoresPersistedRelayIntent(t *testing.T) {
	relay := newFakeRelay()
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, relay, defaultCoordinator())
	if err := runtime.store.SaveDesired(true); err != nil {
		t.Fatal(err)
	}
	operation, resumed := runtime.Resume()
	if !resumed {
		t.Fatal("desired relay state was not resumed")
	}
	waitOperation(t, runtime, operation.ID, "SUCCEEDED")
	starts, _ := relay.counts()
	if starts != 1 {
		t.Fatalf("resume started relay %d times", starts)
	}
}

func TestOperationHistoryDefaultsToBoundedCapacityAndPreservesNewestOrder(t *testing.T) {
	runtime := newTestRuntime(t, &fakeProbe{state: componentstate.Offline}, newFakeRelay(), defaultCoordinator())
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
		t.Fatalf("operation history is not bounded: map=%d order=%d", len(runtime.operations), len(runtime.operationOrder))
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
			t.Fatalf("newest operation %s missing", id)
		}
	}
}

func TestOperationHistoryLimitOneDoesNotResurrectEvictedCallback(t *testing.T) {
	runtime := newTestRuntimeWithOperationLimit(t, &fakeProbe{state: componentstate.Offline}, newFakeRelay(), defaultCoordinator(), 1)
	runtime.mu.Lock()
	first := runtime.newOperationLocked("first")
	second := runtime.newOperationLocked("second")
	runtime.mu.Unlock()
	runtime.setCurrentStep(first.ID, "late")
	runtime.succeed(first.ID, "late")
	runtime.fail(first.ID, errors.New("late"))
	if _, ok := runtime.Operation(first.ID); ok {
		t.Fatal("late callback resurrected an evicted operation")
	}
	if operation, ok := runtime.Operation(second.ID); !ok || operation.Kind != "second" {
		t.Fatalf("newest operation missing: %+v %v", operation, ok)
	}
}

func TestOperationHistoryConcurrentWritersAndReadersRemainBounded(t *testing.T) {
	const limit, writerCount, writesPerWriter = 32, 8, 1_000
	runtime := newTestRuntimeWithOperationLimit(t, &fakeProbe{state: componentstate.Offline}, newFakeRelay(), defaultCoordinator(), limit)
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
				_, _ = runtime.Operation("missing")
			}
		}()
	}
	waitGroup.Wait()
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if len(runtime.operations) != limit || len(runtime.operationOrder) != limit {
		t.Fatalf("concurrent history is not bounded: map=%d order=%d", len(runtime.operations), len(runtime.operationOrder))
	}
}

func TestOperationHistoryRejectsNegativeLimit(t *testing.T) {
	_, err := NewRuntime(RuntimeOptions{
		Probe: &fakeProbe{}, Relay: newFakeRelay(), Coordinator: defaultCoordinator(),
		OperationHistoryLimit: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "operation history limit") {
		t.Fatalf("negative operation history error = %v", err)
	}
}

func TestTCPLocalProbeConnectsWithoutOccupyingPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan struct{})
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
			close(accepted)
		}
	}()
	probe := TCPLocalProbe{Timeout: time.Second}
	if status := probe.Status(context.Background(), port); status.State != componentstate.Ready {
		t.Fatalf("listening server probe = %s", status.State)
	}
	<-accepted
	_ = listener.Close()
	if replacement, err := net.Listen("tcp", listener.Addr().String()); err != nil {
		t.Fatalf("probe occupied the Minecraft port: %v", err)
	} else {
		_ = replacement.Close()
	}
}

func waitOperation(t *testing.T, runtime *Runtime, id, expected string) Operation {
	t.Helper()
	var result Operation
	waitFor(t, func() bool {
		var ok bool
		result, ok = runtime.Operation(id)
		return ok && result.Status == expected
	})
	return result
}

func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
