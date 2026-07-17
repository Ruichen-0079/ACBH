package hobbyagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentlog"
	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
	"github.com/Ruichen-0079/ACBH/agent/internal/frprelay"
)

const (
	ProtocolVersion              = 1
	DefaultOperationHistoryLimit = 256
	DefaultRecentLogLimit        = 500
)

type CoordinatorInfo struct {
	ProtocolVersion          int    `json:"protocol_version"`
	ServerVersion            string `json:"server_version"`
	FRPServerPort            int    `json:"frp_server_port"`
	PublicMinecraftPort      int    `json:"public_minecraft_port"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
}

type CoordinatorStatus struct {
	State string `json:"state"`
}

type Coordinator interface {
	Info(context.Context, Config) (CoordinatorInfo, error)
	Heartbeat(context.Context, Config, Heartbeat) (CoordinatorStatus, error)
}

// Heartbeat retains the Coordinator v0.4 wire field named minecraft for
// compatibility. The snapshot is now only a TCP reachability probe; the Hobby
// Agent never starts, stops, or owns the Minecraft process.
type Heartbeat struct {
	ProtocolVersion     int                     `json:"protocol_version"`
	NodeID              string                  `json:"node_id"`
	NodeName            string                  `json:"node_name,omitempty"`
	AgentVersion        string                  `json:"agent_version"`
	Minecraft           componentstate.Snapshot `json:"minecraft"`
	Relay               componentstate.Snapshot `json:"relay"`
	Overall             componentstate.Snapshot `json:"overall"`
	MinecraftLocalPort  int                     `json:"minecraft_local_port"`
	PublicMinecraftPort int                     `json:"public_minecraft_port"`
	PublicEndpoint      string                  `json:"public_endpoint"`
}

type Relay interface {
	Start(context.Context, frprelay.Config) error
	Stop(context.Context) error
	Status() frprelay.Status
	Diagnose(context.Context) frprelay.Diagnosis
}

type Operation struct {
	ID          string     `json:"operation_id"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	CurrentStep string     `json:"current_step,omitempty"`
	Error       string     `json:"error,omitempty"`
	ErrorCode   string     `json:"error_code,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type RuntimeStatus struct {
	Agent          componentstate.Snapshot `json:"agent"`
	Overall        componentstate.Snapshot `json:"overall_detail"`
	OverallState   componentstate.State    `json:"overall"`
	PublicEndpoint string                  `json:"public_endpoint,omitempty"`
	LocalEndpoint  string                  `json:"local_endpoint"`
	UptimeSeconds  int64                   `json:"uptime_seconds"`
	LocalServer    LocalServerStatus       `json:"local_server"`
	Relay          frprelay.Status         `json:"relay"`
	Coordinator    componentstate.Snapshot `json:"coordinator"`
	CurrentStep    string                  `json:"current_step,omitempty"`
	UserMessage    string                  `json:"user_message"`
	AgentVersion   string                  `json:"agent_version"`
}

type RuntimeOptions struct {
	Store                 FileStore
	Probe                 LocalProbe
	Relay                 Relay
	Coordinator           Coordinator
	FRPCPath              string
	RuntimeDir            string
	LogDir                string
	AgentVersion          string
	RelayTTL              time.Duration
	ComponentTimeout      time.Duration
	PollInterval          time.Duration
	Now                   func() time.Time
	MonitorInterval       time.Duration
	NodeID                string
	NodeName              string
	Logger                agentlog.Writer
	OperationHistoryLimit int
}

type Runtime struct {
	store       FileStore
	probe       LocalProbe
	relay       Relay
	coordinator Coordinator
	states      *componentstate.Store
	frpcPath    string
	runtimeDir  string
	logDir      string
	version     string
	relayTTL    time.Duration
	timeout     time.Duration
	poll        time.Duration
	now         func() time.Time
	monitor     time.Duration
	nodeID      string
	nodeName    string
	logger      agentlog.Writer
	startedAt   time.Time

	mu              sync.RWMutex
	operations      map[string]Operation
	operationOrder  []string
	operationLimit  int
	activeKind      string
	activeID        string
	activeCancel    context.CancelFunc
	lastStartID     string
	lastStopID      string
	currentStep     string
	publicEndpoint  string
	coordinatorInfo *CoordinatorInfo
	logs            []string
	hostingCancel   context.CancelFunc
	frpcVersionOnce sync.Once
	frpcVersion     map[string]string
}

func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	if options.Probe == nil || options.Relay == nil || options.Coordinator == nil {
		return nil, errors.New("local probe, relay, and coordinator dependencies are required")
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.RelayTTL <= 0 {
		options.RelayTTL = 30 * time.Second
	}
	if options.ComponentTimeout <= 0 {
		options.ComponentTimeout = 60 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 200 * time.Millisecond
	}
	if options.MonitorInterval <= 0 {
		options.MonitorInterval = 2 * time.Second
	}
	if options.NodeID == "" {
		hostname, _ := os.Hostname()
		options.NodeID = hostname
		options.NodeName = hostname
	}
	if options.Logger == nil {
		options.Logger = agentlog.DiscardWriter{}
	}
	if options.OperationHistoryLimit < 0 {
		return nil, errors.New("operation history limit cannot be negative")
	}
	if options.OperationHistoryLimit == 0 {
		options.OperationHistoryLimit = DefaultOperationHistoryLimit
	}
	if options.NodeID == "" {
		options.NodeID = "local-agent"
	}
	now := options.Now()
	return &Runtime{
		store: options.Store, probe: options.Probe, relay: options.Relay,
		coordinator: options.Coordinator, states: componentstate.NewStore(now, DefaultRecentLogLimit),
		frpcPath: options.FRPCPath, runtimeDir: options.RuntimeDir, logDir: options.LogDir,
		version: options.AgentVersion, relayTTL: options.RelayTTL,
		timeout: options.ComponentTimeout, poll: options.PollInterval,
		now: options.Now, monitor: options.MonitorInterval,
		nodeID: options.NodeID, nodeName: options.NodeName, logger: options.Logger,
		startedAt:      now,
		operations:     make(map[string]Operation, options.OperationHistoryLimit),
		operationOrder: make([]string, 0, options.OperationHistoryLimit),
		operationLimit: options.OperationHistoryLimit,
		logs:           make([]string, 0, DefaultRecentLogLimit),
	}, nil
}

func (r *Runtime) Config() (PublicConfig, error) {
	config, err := r.store.LoadConfig()
	if err != nil {
		return PublicConfig{}, err
	}
	return config.Public(), nil
}

func (r *Runtime) UpdateConfig(config Config) (PublicConfig, error) {
	config = config.normalized()
	current, loadErr := r.store.LoadConfig()
	if config.AccessToken == "" && loadErr == nil {
		config.AccessToken = current.AccessToken
	}
	if err := config.Validate(); err != nil {
		return PublicConfig{}, err
	}
	if loadErr == nil && !sameConfig(current, config) && r.relayActive() {
		return PublicConfig{}, &CodedError{Code: CodeConfigLockedWhileRunning, Message: "请先停止中转，再修改配置。"}
	}
	if err := r.store.SaveConfig(config); err != nil {
		return PublicConfig{}, err
	}
	return config.Public(), nil
}

func sameConfig(left, right Config) bool {
	left, right = left.normalized(), right.normalized()
	return left.CoordinatorHost == right.CoordinatorHost &&
		left.CoordinatorPort == right.CoordinatorPort &&
		left.AccessToken == right.AccessToken &&
		left.MinecraftLocalPort == right.MinecraftLocalPort &&
		left.PublicMinecraftPort == right.PublicMinecraftPort
}

func (r *Runtime) relayActive() bool {
	r.mu.RLock()
	operationActive := r.activeID != ""
	r.mu.RUnlock()
	if operationActive {
		return true
	}
	switch r.relay.Status().State {
	case componentstate.Online, componentstate.Connecting, componentstate.Reconnecting, componentstate.Stopping:
		return true
	default:
		return false
	}
}

func (r *Runtime) Start() Operation {
	r.mu.Lock()
	if r.activeID != "" {
		operation := r.operations[r.activeID]
		r.mu.Unlock()
		return operation
	}
	relay := r.relay.Status()
	if (relay.State == componentstate.Online || relay.State == componentstate.Connecting || relay.State == componentstate.Reconnecting) && r.lastStartID != "" {
		if operation, ok := r.operations[r.lastStartID]; ok {
			r.mu.Unlock()
			return operation
		}
		r.lastStartID = ""
	}
	operation := r.newOperationLocked("start")
	ctx, cancel := context.WithCancel(context.Background())
	r.activeCancel = cancel
	r.mu.Unlock()
	go r.runStart(ctx, operation.ID)
	return operation
}

func (r *Runtime) Resume() (Operation, bool) {
	desired, err := r.store.LoadDesired()
	if err != nil || !desired {
		return Operation{}, false
	}
	return r.Start(), true
}

func (r *Runtime) Stop() Operation {
	r.mu.Lock()
	if r.activeKind == "stop" && r.activeID != "" {
		operation := r.operations[r.activeID]
		r.mu.Unlock()
		return operation
	}
	if r.relay.Status().State == componentstate.Offline && r.lastStopID != "" {
		if operation, ok := r.operations[r.lastStopID]; ok {
			r.mu.Unlock()
			return operation
		}
		r.lastStopID = ""
	}
	if r.activeCancel != nil {
		r.activeCancel()
	}
	operation := r.newOperationLocked("stop")
	ctx, cancel := context.WithCancel(context.Background())
	r.activeCancel = cancel
	r.mu.Unlock()
	go r.runStop(ctx, operation.ID)
	return operation
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	if r.activeCancel != nil {
		r.activeCancel()
	}
	if r.hostingCancel != nil {
		r.hostingCancel()
		r.hostingCancel = nil
	}
	r.mu.Unlock()
	return r.relay.Stop(ctx)
}

func (r *Runtime) Operation(id string) (Operation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	operation, ok := r.operations[id]
	return operation, ok
}

func (r *Runtime) Status() RuntimeStatus {
	config, _ := r.store.LoadConfig()
	port := config.normalized().MinecraftLocalPort
	local := r.probe.Status(context.Background(), port)
	r.states.Set("local_probe", local.Snapshot, "")
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.statusLocked(local, port)
}

func (r *Runtime) Events(limit int) []componentstate.Event { return r.states.Events(limit) }

func (r *Runtime) Logs(limit int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > len(r.logs) {
		limit = len(r.logs)
	}
	result := make([]string, limit)
	copy(result, r.logs[len(r.logs)-limit:])
	return result
}

func (r *Runtime) Diagnostics(ctx context.Context) map[string]any { return r.buildDiagnostics(ctx) }

func (r *Runtime) LogDirectory() string { return r.logDir }

func (r *Runtime) runStart(ctx context.Context, operationID string) {
	if err := r.step(operationID, "正在检查配置", func() error {
		if _, err := r.store.LoadConfig(); err != nil {
			return fmt.Errorf("load Hobby Edition config: %w", err)
		}
		return r.store.SaveDesired(true)
	}); err != nil {
		r.fail(operationID, err)
		return
	}
	config, _ := r.store.LoadConfig()
	r.refreshLocalProbe(operationID, config.MinecraftLocalPort)
	if err := ctx.Err(); err != nil {
		r.fail(operationID, err)
		return
	}

	r.setState("coordinator", componentstate.Connecting, "info_requested", "正在连接 Coordinator", "", operationID)
	var info CoordinatorInfo
	if err := r.step(operationID, "正在连接 VPS", func() error {
		var err error
		info, err = r.coordinator.Info(ctx, config)
		return err
	}); err != nil {
		r.setState("coordinator", componentstate.Degraded, "info_failed", "Coordinator 暂时不可用", err.Error(), operationID)
		r.fail(operationID, err)
		return
	}
	if info.ProtocolVersion != ProtocolVersion {
		err := fmt.Errorf("coordinator protocol %d is incompatible with agent protocol %d", info.ProtocolVersion, ProtocolVersion)
		r.setState("coordinator", componentstate.Incompatible, "protocol_incompatible", "Coordinator 版本不兼容", err.Error(), operationID)
		r.fail(operationID, err)
		return
	}
	r.setState("coordinator", componentstate.Online, "info_ok", "Coordinator 在线", "", operationID)

	relayConfig := r.relayConfig(config, info)
	if err := ctx.Err(); err != nil {
		r.fail(operationID, err)
		return
	}
	r.setState("relay", componentstate.Connecting, "start_requested", "正在连接公网中转", "", operationID)
	if err := r.step(operationID, "正在启动公网中转", func() error { return r.relay.Start(ctx, relayConfig) }); err != nil {
		r.fail(operationID, err)
		return
	}
	if err := r.step(operationID, "正在验证公网入口", func() error { return r.waitRelayOnline(ctx, operationID) }); err != nil {
		_ = r.relay.Stop(context.Background())
		r.fail(operationID, err)
		return
	}

	r.mu.Lock()
	r.publicEndpoint = fmt.Sprintf("%s:%d", config.CoordinatorHost, config.PublicMinecraftPort)
	r.coordinatorInfo = &info
	hostingContext, cancelHosting := context.WithCancel(context.Background())
	if r.hostingCancel != nil {
		r.hostingCancel()
	}
	r.hostingCancel = cancelHosting
	r.mu.Unlock()
	r.succeed(operationID, "公网中转已上线")
	go r.runHeartbeat(hostingContext, config, info)
	go r.monitorLocalProbe(hostingContext, config.MinecraftLocalPort)
}

func (r *Runtime) runStop(ctx context.Context, operationID string) {
	if err := r.store.SaveDesired(false); err != nil {
		r.fail(operationID, err)
		return
	}
	r.mu.Lock()
	if r.hostingCancel != nil {
		r.hostingCancel()
		r.hostingCancel = nil
	}
	r.mu.Unlock()
	r.setCurrentStep(operationID, "正在停止公网中转")
	if err := r.relay.Stop(ctx); err != nil {
		r.fail(operationID, err)
		return
	}
	r.setState("relay", componentstate.Offline, "stopped", "公网中转已停止", "", operationID)
	r.mu.Lock()
	r.publicEndpoint = ""
	r.mu.Unlock()
	r.succeed(operationID, "公网中转已停止")
}

func (r *Runtime) waitRelayOnline(ctx context.Context, operationID string) error {
	deadline := time.NewTimer(r.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		status := r.relay.Status()
		r.states.Set("relay", status.Snapshot, operationID)
		if status.State == componentstate.Online && status.Fresh(r.now(), r.relayTTL) {
			return nil
		}
		if status.Terminal || status.State == componentstate.Error {
			if status.ReasonCode == CodePublicPortInUse {
				return &CodedError{Code: CodePublicPortInUse, Message: "VPS公网端口已被占用", Cause: errors.New(status.TechnicalMessage)}
			}
			return &CodedError{Code: status.ReasonCode, Message: status.UserMessage, Cause: errors.New(status.TechnicalMessage)}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Relay did not become ONLINE before timeout")
		case <-ticker.C:
		}
	}
}

func (r *Runtime) monitorLocalProbe(ctx context.Context, port int) {
	ticker := time.NewTicker(r.monitor)
	defer ticker.Stop()
	for {
		r.refreshLocalProbe("", port)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) refreshLocalProbe(operationID string, port int) LocalServerStatus {
	status := r.probe.Status(context.Background(), port)
	r.states.Set("local_probe", status.Snapshot, operationID)
	return status
}

func (r *Runtime) relayConfig(config Config, info CoordinatorInfo) frprelay.Config {
	return frprelay.Config{
		FRPCPath: r.frpcPath, RuntimeDir: r.runtimeDir,
		ServerHost: config.CoordinatorHost, ServerPort: info.FRPServerPort,
		AccessToken: config.AccessToken, LocalHost: "127.0.0.1",
		LocalPort: config.MinecraftLocalPort, RemotePort: config.PublicMinecraftPort,
		PublicHost: config.CoordinatorHost, ProbeTTL: r.relayTTL,
	}
}

func (r *Runtime) runHeartbeat(ctx context.Context, config Config, info CoordinatorInfo) {
	interval := time.Duration(info.HeartbeatIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		status := r.Status()
		response, err := r.coordinator.Heartbeat(ctx, config, Heartbeat{
			ProtocolVersion: ProtocolVersion, NodeID: r.nodeID, NodeName: r.nodeName,
			AgentVersion: r.version, Minecraft: status.LocalServer.Snapshot,
			Relay: status.Relay.Snapshot, Overall: status.Overall,
			MinecraftLocalPort:  config.MinecraftLocalPort,
			PublicMinecraftPort: config.PublicMinecraftPort,
			PublicEndpoint:      fmt.Sprintf("%s:%d", config.CoordinatorHost, config.PublicMinecraftPort),
		})
		if err != nil {
			state, reason, message := componentstate.Degraded, "heartbeat_failed", "Coordinator 暂时不可用"
			var httpError *HTTPError
			if errors.As(err, &httpError) && (httpError.StatusCode == 401 || httpError.StatusCode == 403) {
				state, reason, message = componentstate.AuthFailed, "authentication_failed", "Access Token 无效"
			} else if errors.As(err, &httpError) && httpError.StatusCode == 409 {
				state, reason, message = componentstate.Incompatible, "protocol_incompatible", "Coordinator 版本不兼容"
			}
			r.setState("coordinator", state, reason, message, err.Error(), "")
		} else {
			r.setState("coordinator", componentstate.Online, "heartbeat_ok", "Coordinator 在线", response.State, "")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) step(id, name string, action func() error) error {
	r.setCurrentStep(id, name)
	return action()
}

func (r *Runtime) setCurrentStep(id, step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	operation, ok := r.operations[id]
	if !ok {
		return
	}
	operation.CurrentStep = step
	r.operations[id] = operation
	if r.activeID == id {
		r.currentStep = step
	}
	r.appendLogLocked(step)
}

func (r *Runtime) setState(component string, state componentstate.State, reason, user, technical, operationID string) {
	before := r.states.Components()
	from := componentstate.Unknown
	switch component {
	case "local_probe":
		from = before.Minecraft.State
	case "relay":
		from = before.Relay.State
	case "coordinator":
		from = before.Coordinator.State
	}
	snapshot := componentstate.NewSnapshot(state, r.now(), reason, user)
	snapshot.TechnicalMessage = redact(technical, r.accessToken())
	r.states.Set(component, snapshot, operationID)
	_ = r.logger.Write(agentlog.Record{
		Time: snapshot.UpdatedAt, Level: "info", Event: "state_transition",
		Component: component, OperationID: operationID, From: string(from), To: string(state),
		Reason: reason, Message: snapshot.TechnicalMessage,
	}, r.accessToken())
}

func (r *Runtime) succeed(id, finalStep string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	operation, ok := r.operations[id]
	if !ok {
		return
	}
	operation.Status = "SUCCEEDED"
	operation.CurrentStep = finalStep
	operation.FinishedAt = &now
	r.operations[id] = operation
	if operation.Kind == "start" {
		r.lastStartID = id
	} else if operation.Kind == "stop" {
		r.lastStopID = id
	}
	if r.activeID == id {
		r.activeID, r.activeKind, r.currentStep, r.activeCancel = "", "", "", nil
	}
	r.appendLogLocked(finalStep)
}

func (r *Runtime) fail(id string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	operation, ok := r.operations[id]
	if !ok {
		return
	}
	operation.Status = "FAILED"
	operation.Error = redact(err.Error(), r.accessTokenLocked())
	operation.ErrorCode = errorCode(err)
	operation.FinishedAt = &now
	r.operations[id] = operation
	if r.activeID == id {
		r.activeID, r.activeKind, r.currentStep, r.activeCancel = "", "", "", nil
	}
	r.appendLogLocked(operation.Error)
}

func (r *Runtime) newOperationLocked(kind string) Operation {
	operation := Operation{ID: randomID(), Kind: kind, Status: "RUNNING", StartedAt: r.now()}
	if len(r.operationOrder) == r.operationLimit {
		evictedID := r.operationOrder[0]
		delete(r.operations, evictedID)
		copy(r.operationOrder, r.operationOrder[1:])
		r.operationOrder[len(r.operationOrder)-1] = operation.ID
		if r.lastStartID == evictedID {
			r.lastStartID = ""
		}
		if r.lastStopID == evictedID {
			r.lastStopID = ""
		}
	} else {
		r.operationOrder = append(r.operationOrder, operation.ID)
	}
	r.operations[operation.ID] = operation
	r.activeKind, r.activeID = kind, operation.ID
	return operation
}

func (r *Runtime) statusLocked(local LocalServerStatus, port int) RuntimeStatus {
	relay := r.relay.Status()
	components := r.states.Components()
	components.Minecraft = local.Snapshot
	if components.Relay.State == componentstate.Error && relay.State != componentstate.Online {
		relay.Snapshot = components.Relay
	} else {
		components.Relay = relay.Snapshot
	}
	overall := deriveAgentOverall(components, r.now(), r.relayTTL)
	agent := componentstate.NewSnapshot(componentstate.Online, r.now(), "agent_running", "Agent 正常")
	agent.LastOKAt = timePointer(r.now())
	return RuntimeStatus{
		Agent: agent, Overall: overall, OverallState: overall.State,
		PublicEndpoint: r.publicEndpoint, LocalEndpoint: fmt.Sprintf("127.0.0.1:%d", port),
		UptimeSeconds: int64(r.now().Sub(r.startedAt).Seconds()), LocalServer: local,
		Relay: relay, Coordinator: components.Coordinator, CurrentStep: r.currentStep,
		UserMessage: overall.UserMessage, AgentVersion: r.version,
	}
}

func deriveAgentOverall(components componentstate.Components, now time.Time, relayTTL time.Duration) componentstate.Snapshot {
	state, reason, message := componentstate.Offline, "relay_offline", "公网中转未启动"
	switch {
	case components.Relay.State == componentstate.Error:
		state, reason, message = componentstate.Error, "relay_error", components.Relay.UserMessage
	case components.Relay.State == componentstate.Stopping:
		state, reason, message = componentstate.Stopping, "stop_in_progress", "正在停止公网中转"
	case components.Relay.State == componentstate.Online && components.Relay.Fresh(now, relayTTL):
		state, reason, message = componentstate.Online, "relay_healthy", "公网中转运行正常"
	case components.Relay.State == componentstate.Online:
		state, reason, message = componentstate.Degraded, "relay_observation_stale", "公网中转状态已过期"
	case components.Relay.State == componentstate.Reconnecting:
		state, reason, message = componentstate.Reconnecting, "relay_reconnecting", "公网中转断开，正在重连"
	case components.Relay.State == componentstate.Connecting:
		state, reason, message = componentstate.Connecting, "start_in_progress", "正在连接公网中转"
	}
	return componentstate.NewSnapshot(state, now, reason, message)
}

func (r *Runtime) appendLogLocked(message string) {
	message = redact(message, r.accessTokenLocked())
	_ = r.logger.Write(agentlog.Record{
		Time: r.now(), Level: "info", Event: "agent_operation", OperationID: r.activeID, Message: message,
	}, r.accessTokenLocked())
	if len(r.logs) == DefaultRecentLogLimit {
		copy(r.logs, r.logs[1:])
		r.logs[len(r.logs)-1] = message
	} else {
		r.logs = append(r.logs, message)
	}
}

func (r *Runtime) accessToken() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.accessTokenLocked()
}

func (r *Runtime) accessTokenLocked() string {
	config, err := r.store.LoadConfig()
	if err != nil {
		return ""
	}
	return config.AccessToken
}

func randomID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("op-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func redact(value, secret string) string {
	if secret == "" {
		return value
	}
	return replaceAll(value, secret, "[REDACTED]")
}

func replaceAll(value, old, replacement string) string {
	for {
		index := indexOf(value, old)
		if index < 0 {
			return value
		}
		value = value[:index] + replacement + value[index+len(old):]
	}
}

func indexOf(value, part string) int {
	if part == "" {
		return -1
	}
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return index
		}
	}
	return -1
}
