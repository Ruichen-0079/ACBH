package frprelay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
)

var retrySchedule = []time.Duration{
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

type Manager struct {
	mu          sync.RWMutex
	deps        Dependencies
	config      Config
	desired     bool
	process     Process
	cancel      context.CancelFunc
	done        chan struct{}
	status      Status
	events      []Event
	eventCh     chan Event
	metadata    metadata
	metadataErr error
}

func NewManager(deps Dependencies) *Manager {
	if deps.Launcher == nil {
		deps.Launcher = execLauncher{}
	}
	if deps.Prober == nil {
		deps.Prober = tcpProber{timeout: 2 * time.Second}
	}
	if deps.Sleeper == nil {
		deps.Sleeper = realSleeper{}
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	if deps.Inspector == nil {
		deps.Inspector = osProcessInspector{}
	}
	now := deps.Clock.Now()
	return &Manager{
		deps:    deps,
		eventCh: make(chan Event, 128),
		events:  make([]Event, 0, 500),
		status: Status{Snapshot: componentstate.NewSnapshot(
			componentstate.Offline, now, "not_started", "公网中转未启动",
		)},
	}
}

func (m *Manager) Start(_ context.Context, config Config) error {
	config = config.withDefaults()
	if err := config.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desired {
		if configHash(m.config) == configHash(config) {
			return nil
		}
		return errors.New("relay is already running with different configuration")
	}

	loaded, err := loadMetadata(metadataPath(config.RuntimeDir))
	if err != nil {
		return err
	}
	if loaded.PID > 0 && loaded.Desired && m.deps.Inspector.Alive(loaded.PID) {
		fingerprint, fingerprintErr := m.deps.Inspector.Fingerprint(loaded.PID)
		if fingerprintErr != nil || loaded.ProcessFingerprint == "" || fingerprint != loaded.ProcessFingerprint {
			return fmt.Errorf("%w with PID %d; process identity could not be verified", ErrAlreadyManaged, loaded.PID)
		}
		if err := m.deps.Inspector.TerminateOwned(loaded.PID, loaded.ProcessFingerprint); err != nil {
			return fmt.Errorf("stop verified recovered frpc PID %d: %w", loaded.PID, err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for m.deps.Inspector.Alive(loaded.PID) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if m.deps.Inspector.Alive(loaded.PID) {
			return fmt.Errorf("verified recovered frpc PID %d did not stop", loaded.PID)
		}
	}

	m.config = config
	m.desired = true
	m.metadata = metadata{
		Desired:    true,
		Executable: config.FRPCPath,
		ConfigHash: configHash(config),
		UpdatedAt:  m.deps.Clock.Now(),
	}
	if err := saveMetadata(metadataPath(config.RuntimeDir), m.metadata); err != nil {
		m.desired = false
		return err
	}
	workerContext, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.transitionLocked(componentstate.Connecting, "start_requested", "正在连接公网中转", "", false)
	go m.run(workerContext, m.done)
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.desired && m.process == nil {
		m.mu.Unlock()
		return nil
	}
	m.desired = false
	m.transitionLocked(componentstate.Stopping, "stop_requested", "正在停止公网中转", "", false)
	cancel := m.cancel
	process := m.process
	done := m.done
	config := m.config
	if cancel != nil {
		cancel()
	}
	m.mu.Unlock()

	if process != nil {
		stopContext, stopCancel := context.WithTimeout(ctx, config.StopTimeout)
		err := process.Stop(stopContext)
		stopCancel()
		if err != nil {
			_ = process.Kill()
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.mu.Lock()
	m.process = nil
	m.metadata = metadata{Desired: false, UpdatedAt: m.deps.Clock.Now()}
	persistErr := saveMetadata(metadataPath(config.RuntimeDir), m.metadata)
	m.transitionLocked(componentstate.Offline, "stopped", "公网中转已停止", "", false)
	m.mu.Unlock()
	return persistErr
}

func (m *Manager) Reconcile(ctx context.Context, desired bool, config Config) error {
	if desired {
		m.mu.RLock()
		same := m.desired && configHash(m.config) == configHash(config)
		m.mu.RUnlock()
		if same {
			return nil
		}
		if err := m.Stop(ctx); err != nil {
			return err
		}
		return m.Start(ctx, config)
	}
	return m.Stop(ctx)
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneStatus(m.status)
}

func (m *Manager) Events() <-chan Event { return m.eventCh }

func (m *Manager) Diagnose(_ context.Context) Diagnosis {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := make([]Event, len(m.events))
	copy(events, m.events)
	return Diagnosis{
		Status:        cloneStatus(m.status),
		Desired:       m.desired,
		ConfigHash:    m.metadata.ConfigHash,
		MetadataPath:  metadataPath(m.config.RuntimeDir),
		RecentEvents:  events,
		AccessToken:   "[REDACTED]",
		Configuration: "generated temporary frpc config (token redacted)",
	}
}

func (m *Manager) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	failures := 0
	for {
		m.mu.RLock()
		desired := m.desired
		config := m.config
		m.mu.RUnlock()
		if !desired || ctx.Err() != nil {
			return
		}

		configPath, err := writeTemporaryConfig(config)
		if err != nil {
			m.finishTerminal("config_write_failed", err)
			return
		}
		process, startErr := m.deps.Launcher.Start(ctx, LaunchRequest{
			Executable: config.FRPCPath,
			ConfigPath: configPath,
		})
		if startErr != nil {
			_ = os.Remove(configPath)
			if m.handleFailure(ctx, config, failures, startErr, "") {
				failures++
				continue
			}
			return
		}

		m.mu.Lock()
		m.process = process
		m.status.PID = process.PID()
		m.metadata.PID = process.PID()
		if fingerprint, fingerprintErr := m.deps.Inspector.Fingerprint(process.PID()); fingerprintErr == nil {
			m.metadata.ProcessFingerprint = fingerprint
		}
		m.metadata.UpdatedAt = m.deps.Clock.Now()
		_ = saveMetadata(metadataPath(config.RuntimeDir), m.metadata)
		m.mu.Unlock()

		failure, terminalReason, stable := m.monitorProcess(ctx, config, process)
		_ = os.Remove(configPath)
		m.mu.Lock()
		if m.process == process {
			m.process = nil
			m.status.PID = 0
		}
		m.mu.Unlock()
		if ctx.Err() != nil || !m.isDesired() {
			return
		}
		if stable {
			failures = 0
		}
		if !m.handleFailure(ctx, config, failures, failure, terminalReason) {
			return
		}
		failures++
	}
}

func (m *Manager) monitorProcess(ctx context.Context, config Config, process Process) (error, string, bool) {
	probeTicker := time.NewTicker(config.ProbeInterval)
	defer probeTicker.Stop()
	var connectedAt *time.Time
	lastTerminalReason := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err(), "", false
		case line, ok := <-process.Lines():
			if !ok {
				continue
			}
			clean := redact(line.Line, config.AccessToken)
			m.emit(Event{Event: "frpc_output", Stream: line.Stream, Message: clean, Time: line.Time.UTC()})
			if isConnectedLine(clean) {
				now := m.deps.Clock.Now()
				connectedAt = &now
				m.mu.Lock()
				m.status.FRPSConnected = true
				m.mu.Unlock()
			}
			if reason, terminal := classifyLine(clean); terminal {
				lastTerminalReason = reason
			}
		case err := <-process.Wait():
			stable := connectedAt != nil && m.deps.Clock.Now().Sub(*connectedAt) >= config.StableResetTime
			return err, lastTerminalReason, stable
		case <-probeTicker.C:
			m.probe(ctx, config, connectedAt)
		}
	}
}

func (m *Manager) probe(ctx context.Context, config Config, connectedAt *time.Time) {
	localErr := m.deps.Prober.Probe(ctx, config.LocalAddress())
	publicErr := m.deps.Prober.Probe(ctx, config.PublicAddress())
	now := m.deps.Clock.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LocalReachable = localErr == nil
	m.status.PublicReachable = publicErr == nil
	m.status.LastProbeAt = timePointer(now)
	if m.status.FRPSConnected && localErr == nil && publicErr == nil {
		m.status.ConnectedSince = cloneTimePointer(connectedAt)
		m.status.LastOKAt = timePointer(now)
		m.status.ObservedAt = timePointer(now)
		m.transitionLocked(componentstate.Online, "public_probe_success", "公网中转正常", "", false)
		return
	}
	reason := "frps_not_connected"
	technical := "frpc has not reported a successful frps connection"
	if localErr != nil {
		reason, technical = "local_port_unreachable", localErr.Error()
	} else if publicErr != nil {
		reason, technical = "public_probe_failed", publicErr.Error()
	}
	m.transitionLocked(componentstate.Connecting, reason, "正在验证公网中转", technical, false)
}

func (m *Manager) handleFailure(ctx context.Context, config Config, failures int, err error, terminalReason string) bool {
	if terminalReason == "" {
		terminalReason, _ = classifyError(err)
	}
	if terminalReason != "" {
		m.finishTerminal(terminalReason, err)
		return false
	}

	delay := retrySchedule[min(failures, len(retrySchedule)-1)]
	m.mu.Lock()
	m.status.ReconnectCount++
	m.status.FRPSConnected = false
	m.status.LocalReachable = false
	m.status.PublicReachable = false
	m.transitionLocked(componentstate.Reconnecting, "retryable_network_error", "公网中转断开，正在重连", errorString(err), false)
	m.mu.Unlock()
	return m.deps.Sleeper.Sleep(ctx, delay) == nil
}

func (m *Manager) finishTerminal(reason string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.desired = false
	m.status.Terminal = true
	userMessage := "公网中转配置需要检查"
	if reason == "PUBLIC_PORT_IN_USE" {
		userMessage = "VPS公网端口已被占用"
	} else if reason == "AUTH_FAILED" {
		userMessage = "Access Token 无效"
	}
	m.transitionLocked(componentstate.Error, reason, userMessage, errorString(err), true)
	m.metadata.Desired = false
	m.metadata.PID = 0
	m.metadata.UpdatedAt = m.deps.Clock.Now()
	_ = saveMetadata(metadataPath(m.config.RuntimeDir), m.metadata)
}

func (m *Manager) isDesired() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.desired
}

func (m *Manager) transitionLocked(state componentstate.State, reason, userMessage, technical string, terminal bool) {
	now := m.deps.Clock.Now()
	m.status.State = state
	m.status.UpdatedAt = now
	m.status.ObservedAt = timePointer(now)
	m.status.ReasonCode = reason
	m.status.UserMessage = userMessage
	m.status.TechnicalMessage = redact(technical, m.config.AccessToken)
	m.status.Terminal = terminal
	m.emitLocked(Event{Event: "relay_transition", State: string(state), ReasonCode: reason, Message: userMessage, Time: now})
}

func (m *Manager) emit(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitLocked(event)
}

func (m *Manager) emitLocked(event Event) {
	if event.Time.IsZero() {
		event.Time = m.deps.Clock.Now()
	}
	if len(m.events) == 500 {
		copy(m.events, m.events[1:])
		m.events[len(m.events)-1] = event
	} else {
		m.events = append(m.events, event)
	}
	select {
	case m.eventCh <- event:
	default:
	}
}

func classifyLine(line string) (string, bool) {
	normalized := strings.ToLower(line)
	for _, marker := range []string{"token in login doesn't match", "authentication failed", "authorization failed", "invalid token"} {
		if strings.Contains(normalized, marker) {
			return "AUTH_FAILED", true
		}
	}
	for _, marker := range []string{"port already used", "remote port", "already in use", "port conflict"} {
		if strings.Contains(normalized, marker) && (strings.Contains(normalized, "used") || strings.Contains(normalized, "in use") || strings.Contains(normalized, "conflict")) {
			return "PUBLIC_PORT_IN_USE", true
		}
	}
	for _, marker := range []string{"parse config error", "invalid configuration", "failed to parse"} {
		if strings.Contains(normalized, marker) {
			return "invalid_configuration", true
		}
	}
	return "", false
}

func classifyError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	return classifyLine(err.Error())
}

func isConnectedLine(line string) bool {
	normalized := strings.ToLower(line)
	return strings.Contains(normalized, "login to server success") ||
		strings.Contains(normalized, "successfully login") ||
		strings.Contains(normalized, "start proxy success")
}

func redact(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

func errorString(err error) string {
	if err == nil {
		return "frpc exited"
	}
	return err.Error()
}

func cloneStatus(value Status) Status {
	value.ConnectedSince = cloneTimePointer(value.ConnectedSince)
	value.LastProbeAt = cloneTimePointer(value.LastProbeAt)
	value.ObservedAt = cloneTimePointer(value.ObservedAt)
	value.LastOKAt = cloneTimePointer(value.LastOKAt)
	return value
}

func timePointer(value time.Time) *time.Time { return &value }

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
