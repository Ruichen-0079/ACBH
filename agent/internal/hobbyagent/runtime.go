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

const ProtocolVersion = 1

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

type MinecraftStatus struct {
	componentstate.Snapshot
	PID       int      `json:"pid,omitempty"`
	ExitCode  *int     `json:"exit_code,omitempty"`
	RecentLog []string `json:"recent_log,omitempty"`
}

type Minecraft interface {
	Start(context.Context, ImportedServer, int) error
	Stop(context.Context) error
	Status(context.Context, int) MinecraftStatus
	Diagnose(context.Context, int) any
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
	Overall        componentstate.Snapshot `json:"overall_detail"`
	OverallState   componentstate.State    `json:"overall"`
	PublicEndpoint string                  `json:"public_endpoint,omitempty"`
	LocalEndpoint  string                  `json:"local_endpoint"`
	UptimeSeconds  int64                   `json:"uptime_seconds"`
	Minecraft      MinecraftStatus         `json:"minecraft"`
	Relay          frprelay.Status         `json:"relay"`
	Coordinator    componentstate.Snapshot `json:"coordinator"`
	CurrentStep    string                  `json:"current_step,omitempty"`
	UserMessage    string                  `json:"user_message"`
}

type RuntimeOptions struct {
	Store                 FileStore
	Minecraft             Minecraft
	Relay                 Relay
	Coordinator           Coordinator
	FRPCPath              string
	RuntimeDir            string
	AgentVersion          string
	RelayTTL              time.Duration
	ComponentTimeout      time.Duration
	PollInterval          time.Duration
	Now                   func() time.Time
	Preflight             func(string) (PreflightResult, error)
	MonitorInterval       time.Duration
	NodeID                string
	NodeName              string
	Logger                agentlog.Writer
	AutoRestartMinecraft  bool
	MaxMinecraftRestarts  int
	MinecraftRestartDelay time.Duration
}

type Runtime struct {
	store                 FileStore
	minecraft             Minecraft
	relay                 Relay
	coordinator           Coordinator
	states                *componentstate.Store
	frpcPath              string
	runtimeDir            string
	version               string
	relayTTL              time.Duration
	timeout               time.Duration
	poll                  time.Duration
	now                   func() time.Time
	preflight             func(string) (PreflightResult, error)
	monitor               time.Duration
	nodeID                string
	nodeName              string
	logger                agentlog.Writer
	autoRestartMinecraft  bool
	maxMinecraftRestarts  int
	minecraftRestartDelay time.Duration
	startedAt             time.Time

	mu              sync.RWMutex
	operations      map[string]Operation
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
}

func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	if options.Minecraft == nil || options.Relay == nil || options.Coordinator == nil {
		return nil, errors.New("minecraft, relay, and coordinator dependencies are required")
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
	if options.Preflight == nil {
		options.Preflight = PreflightServer
	}
	if options.NodeID == "" {
		hostname, _ := os.Hostname()
		options.NodeID = hostname
		options.NodeName = hostname
	}
	if options.Logger == nil {
		options.Logger = agentlog.DiscardWriter{}
	}
	if options.MaxMinecraftRestarts < 0 {
		return nil, errors.New("maximum Minecraft restarts cannot be negative")
	}
	if options.MaxMinecraftRestarts == 0 {
		options.MaxMinecraftRestarts = 3
	}
	if options.MinecraftRestartDelay <= 0 {
		options.MinecraftRestartDelay = time.Second
	}
	if options.NodeID == "" {
		options.NodeID = "local-agent"
	}
	now := options.Now()
	return &Runtime{
		store: options.Store, minecraft: options.Minecraft, relay: options.Relay,
		coordinator: options.Coordinator, states: componentstate.NewStore(now, 500),
		frpcPath: options.FRPCPath, runtimeDir: options.RuntimeDir, version: options.AgentVersion,
		relayTTL: options.RelayTTL, timeout: options.ComponentTimeout, poll: options.PollInterval,
		preflight: options.Preflight, monitor: options.MonitorInterval,
		nodeID: options.NodeID, nodeName: options.NodeName,
		logger:                options.Logger,
		autoRestartMinecraft:  options.AutoRestartMinecraft,
		maxMinecraftRestarts:  options.MaxMinecraftRestarts,
		minecraftRestartDelay: options.MinecraftRestartDelay,
		now:                   options.Now, startedAt: now, operations: make(map[string]Operation), logs: make([]string, 0, 500),
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
	portsChanged := loadErr == nil && (current.MinecraftLocalPort != config.MinecraftLocalPort || current.PublicMinecraftPort != config.PublicMinecraftPort)
	if portsChanged && r.hostingActive(current.MinecraftLocalPort) {
		return PublicConfig{}, &CodedError{Code: CodeConfigLockedWhileRunning, Message: "请先停止托管，再修改端口。"}
	}
	if err := r.store.SaveConfig(config); err != nil {
		return PublicConfig{}, err
	}
	return config.Public(), nil
}

func (r *Runtime) hostingActive(port int) bool {
	r.mu.RLock()
	operationActive := r.activeID != ""
	r.mu.RUnlock()
	if operationActive {
		return true
	}
	minecraft := r.minecraft.Status(context.Background(), port)
	relay := r.relay.Status()
	return minecraft.State != componentstate.Stopped || relay.State != componentstate.Offline
}

func (r *Runtime) Import(serverDir string) (PreflightResult, error) {
	result, err := r.preflight(serverDir)
	if err != nil {
		return PreflightResult{}, err
	}
	if err := r.store.SaveImportedServer(ImportedServer{
		ServerDir: result.ServerDir, JavaPath: result.JavaPath, JarPath: result.JarPath,
	}); err != nil {
		return PreflightResult{}, err
	}
	return result, nil
}

func (r *Runtime) Start() Operation {
	r.mu.Lock()
	if r.activeID != "" {
		operation := r.operations[r.activeID]
		r.mu.Unlock()
		return operation
	}
	status := r.statusLocked()
	if status.OverallState == componentstate.Online && r.lastStartID != "" {
		operation := r.operations[r.lastStartID]
		r.mu.Unlock()
		return operation
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
	status := r.statusLocked()
	if status.Minecraft.State == componentstate.Stopped && status.Relay.State == componentstate.Offline && r.lastStopID != "" {
		operation := r.operations[r.lastStopID]
		r.mu.Unlock()
		return operation
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

func (r *Runtime) Operation(id string) (Operation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	operation, ok := r.operations[id]
	return operation, ok
}

func (r *Runtime) Status() RuntimeStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.statusLocked()
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

func (r *Runtime) Diagnostics(ctx context.Context) map[string]any {
	return r.buildDiagnostics(ctx)
}

func (r *Runtime) runStart(ctx context.Context, operationID string) {
	if err := r.step(operationID, "正在检查服务端", func() error {
		if err := r.validateStartInputs(); err != nil {
			return err
		}
		return r.store.SaveDesired(true)
	}); err != nil {
		r.fail(operationID, err)
		return
	}
	imported, _ := r.store.LoadImportedServer()
	config, _ := r.store.LoadConfig()
	r.setState("minecraft", componentstate.Starting, "start_requested", "正在启动 Minecraft", "", operationID)
	if err := r.step(operationID, "正在启动 Minecraft", func() error { return r.minecraft.Start(ctx, imported, config.MinecraftLocalPort) }); err != nil {
		reason, message := "start_failed", "Minecraft 启动失败"
		if errorCode(err) == CodeLocalPortInUse {
			reason, message = CodeLocalPortInUse, err.Error()
		}
		r.setState("minecraft", componentstate.Error, reason, message, err.Error(), operationID)
		r.fail(operationID, err)
		return
	}
	if err := r.step(operationID, "正在等待本地端口", func() error { return r.waitMinecraftReady(ctx, operationID, config.MinecraftLocalPort) }); err != nil {
		r.setState("minecraft", componentstate.Error, CodeLocalMCNotListening, err.Error(), err.Error(), operationID)
		_ = r.minecraft.Stop(context.Background())
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
		_ = r.minecraft.Stop(context.Background())
		r.fail(operationID, err)
		return
	}
	if info.ProtocolVersion != ProtocolVersion {
		err := fmt.Errorf("coordinator protocol %d is incompatible with agent protocol %d", info.ProtocolVersion, ProtocolVersion)
		r.setState("coordinator", componentstate.Incompatible, "protocol_incompatible", "Coordinator 版本不兼容", err.Error(), operationID)
		_ = r.minecraft.Stop(context.Background())
		r.fail(operationID, err)
		return
	}
	r.setState("coordinator", componentstate.Online, "info_ok", "Coordinator 在线", "", operationID)

	relayConfig := r.relayConfig(config, info)
	r.setState("relay", componentstate.Connecting, "start_requested", "正在连接公网中转", "", operationID)
	if err := r.step(operationID, "正在连接 VPS", func() error { return r.relay.Start(ctx, relayConfig) }); err != nil {
		_ = r.minecraft.Stop(context.Background())
		r.fail(operationID, err)
		return
	}
	if err := r.step(operationID, "正在验证公网入口", func() error { return r.waitRelayOnline(ctx, operationID) }); err != nil {
		_ = r.relay.Stop(context.Background())
		_ = r.minecraft.Stop(context.Background())
		r.fail(operationID, err)
		return
	}

	r.mu.Lock()
	r.publicEndpoint = fmt.Sprintf("%s:%d", config.CoordinatorHost, config.PublicMinecraftPort)
	r.coordinatorInfo = &info
	heartbeatContext, cancelHeartbeat := context.WithCancel(context.Background())
	if r.hostingCancel != nil {
		r.hostingCancel()
	}
	r.hostingCancel = cancelHeartbeat
	r.mu.Unlock()
	r.succeed(operationID, "公网服务已上线")
	go r.runHeartbeat(heartbeatContext, config, info)
	go r.monitorMinecraft(heartbeatContext, operationID, imported, relayConfig)
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
	r.setCurrentStep(operationID, "正在停止 Minecraft")
	if err := r.minecraft.Stop(ctx); err != nil {
		r.fail(operationID, err)
		return
	}
	r.setState("minecraft", componentstate.Stopped, "stopped", "Minecraft 已停止", "", operationID)
	// Future safe-sync hook: archive upload belongs here, after Minecraft has
	// stopped and before the operation is marked complete.
	r.mu.Lock()
	r.publicEndpoint = ""
	r.mu.Unlock()
	r.succeed(operationID, "托管已停止")
}

func (r *Runtime) validateStartInputs() error {
	if _, err := r.store.LoadConfig(); err != nil {
		return fmt.Errorf("load Hobby Edition config: %w", err)
	}
	imported, err := r.store.LoadImportedServer()
	if err != nil {
		return fmt.Errorf("load imported server: %w", err)
	}
	_, err = r.preflight(imported.ServerDir)
	return err
}

func (r *Runtime) waitMinecraftReady(ctx context.Context, operationID string, port int) error {
	deadline := time.NewTimer(r.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		status := r.minecraft.Status(ctx, port)
		r.states.Set("minecraft", status.Snapshot, operationID)
		if status.State == componentstate.Ready {
			return nil
		}
		if status.State == componentstate.Error {
			return errors.New(status.TechnicalMessage)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return &CodedError{Code: CodeLocalMCNotListening, Message: "Minecraft没有监听填写的本地端口，请检查端口设置是否一致。"}
		case <-ticker.C:
		}
	}
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
			return fmt.Errorf("relay failed: %s", status.ReasonCode)
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

func (r *Runtime) monitorMinecraft(ctx context.Context, operationID string, imported ImportedServer, relayConfig frprelay.Config) {
	ticker := time.NewTicker(r.monitor)
	defer ticker.Stop()
	restarts := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		status := r.minecraft.Status(context.Background(), relayConfig.LocalPort)
		r.states.Set("minecraft", status.Snapshot, operationID)
		if status.State == componentstate.Ready {
			continue
		}
		_ = r.relay.Stop(context.Background())
		r.setState("relay", componentstate.Error, "minecraft_not_ready", "Minecraft 已停止，公网中转不可用", "", operationID)
		if !r.autoRestartMinecraft || restarts >= r.maxMinecraftRestarts {
			r.setState("minecraft", componentstate.Error, "restart_limit_reached", "Minecraft 自动恢复次数已用尽", status.TechnicalMessage, operationID)
			return
		}
		restarts++
		r.setState("minecraft", componentstate.Starting, "automatic_restart", "正在自动重启 Minecraft", fmt.Sprintf("attempt %d of %d", restarts, r.maxMinecraftRestarts), operationID)
		if !waitContext(ctx, r.minecraftRestartDelay) {
			return
		}
		if err := r.minecraft.Start(ctx, imported, relayConfig.LocalPort); err != nil {
			r.setState("minecraft", componentstate.Error, "automatic_restart_failed", "Minecraft 自动重启失败", err.Error(), operationID)
			continue
		}
		if err := r.waitMinecraftReady(ctx, operationID, relayConfig.LocalPort); err != nil {
			r.setState("minecraft", componentstate.Error, "automatic_restart_failed", "Minecraft 自动重启失败", err.Error(), operationID)
			continue
		}
		r.setState("relay", componentstate.Reconnecting, "minecraft_recovered", "Minecraft 已恢复，正在重连公网中转", "", operationID)
		if err := r.relay.Start(ctx, relayConfig); err != nil {
			r.setState("relay", componentstate.Error, "restart_failed", "公网中转恢复失败", err.Error(), operationID)
			continue
		}
		if err := r.waitRelayOnline(ctx, operationID); err != nil {
			_ = r.relay.Stop(context.Background())
			r.setState("relay", componentstate.Error, "restart_failed", "公网中转恢复失败", err.Error(), operationID)
			continue
		}
	}
}

func (r *Runtime) relayConfig(config Config, info CoordinatorInfo) frprelay.Config {
	return frprelay.Config{
		FRPCPath: r.frpcPath, RuntimeDir: r.runtimeDir, ServerHost: config.CoordinatorHost,
		ServerPort: info.FRPServerPort, AccessToken: config.AccessToken,
		LocalHost: "127.0.0.1", LocalPort: config.MinecraftLocalPort, RemotePort: config.PublicMinecraftPort,
		PublicHost: config.CoordinatorHost, ProbeTTL: r.relayTTL,
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
			ProtocolVersion:     ProtocolVersion,
			NodeID:              r.nodeID,
			NodeName:            r.nodeName,
			AgentVersion:        r.version,
			Minecraft:           status.Minecraft.Snapshot,
			Relay:               status.Relay.Snapshot,
			Overall:             status.Overall,
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
	operation := r.operations[id]
	operation.CurrentStep = step
	r.operations[id] = operation
	r.currentStep = step
	r.appendLogLocked(step)
}

func (r *Runtime) setState(component string, state componentstate.State, reason, user, technical, operationID string) {
	before := r.states.Components()
	from := componentstate.Unknown
	switch component {
	case "minecraft":
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
	operation := r.operations[id]
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
	operation := r.operations[id]
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
	r.operations[operation.ID] = operation
	r.activeKind, r.activeID = kind, operation.ID
	return operation
}

func (r *Runtime) statusLocked() RuntimeStatus {
	config, _ := r.store.LoadConfig()
	port := config.MinecraftLocalPort
	if port == 0 {
		port = 25565
	}
	minecraft := r.minecraft.Status(context.Background(), port)
	relay := r.relay.Status()
	components := r.states.Components()
	if components.Minecraft.State == componentstate.Error && minecraft.State != componentstate.Ready {
		minecraft.Snapshot = components.Minecraft
	} else {
		components.Minecraft = minecraft.Snapshot
	}
	if components.Relay.State == componentstate.Error && relay.State != componentstate.Online {
		relay.Snapshot = components.Relay
	} else {
		components.Relay = relay.Snapshot
	}
	overall := componentstate.DeriveOverall(components, r.now(), r.relayTTL)
	return RuntimeStatus{
		Overall: overall, OverallState: overall.State, PublicEndpoint: r.publicEndpoint,
		LocalEndpoint: fmt.Sprintf("127.0.0.1:%d", port),
		UptimeSeconds: int64(r.now().Sub(r.startedAt).Seconds()), Minecraft: minecraft,
		Relay: relay, Coordinator: components.Coordinator, CurrentStep: r.currentStep,
		UserMessage: overall.UserMessage,
	}
}

func (r *Runtime) appendLogLocked(message string) {
	message = redact(message, r.accessTokenLocked())
	_ = r.logger.Write(agentlog.Record{
		Time: r.now(), Level: "info", Event: "agent_operation", OperationID: r.activeID, Message: message,
	}, r.accessTokenLocked())
	if len(r.logs) == 500 {
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
