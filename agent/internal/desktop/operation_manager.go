package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type OperationState string

const (
	OperationQueued         OperationState = "queued"
	OperationRunning        OperationState = "running"
	OperationSucceeded      OperationState = "succeeded"
	OperationWarning        OperationState = "success_with_warnings"
	OperationPartialFailure OperationState = "partial_failure"
	OperationFailed         OperationState = "failed"
	OperationCancelled      OperationState = "cancelled"
	OperationTimedOut       OperationState = "timed_out"
)

type ProgressEvent struct {
	Type        string `json:"type"`
	OperationID string `json:"operationId"`
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Current     int64  `json:"current,omitempty"`
	Total       int64  `json:"total,omitempty"`
	At          string `json:"at"`
}

type OperationSnapshot struct {
	OperationID  string         `json:"operationId"`
	Name         string         `json:"name"`
	MutexClass   string         `json:"mutexClass"`
	State        OperationState `json:"state"`
	StartedAt    time.Time      `json:"startedAt"`
	CompletedAt  *time.Time     `json:"completedAt,omitempty"`
	CurrentStage string         `json:"currentStage"`
	Progress     *ProgressEvent `json:"progress,omitempty"`
	Cancellable  bool           `json:"cancellable"`
	Timeout      string         `json:"timeout"`
	TraceID      string         `json:"traceId"`
	Result       *Envelope      `json:"terminalResult,omitempty"`
}

type OperationSummary struct {
	Busy       bool                `json:"busy"`
	Operations []OperationSnapshot `json:"operations"`
}

type OperationOptions struct {
	Name        string
	MutexClass  string
	Cancellable bool
	Timeout     time.Duration
	Coalesce    bool
}

type OperationContext struct {
	context.Context
	OperationID string
	TraceID     string
	emit        func(stage string, message string, current int64, total int64)
}

func (ctx OperationContext) Progress(stage string, message string, current int64, total int64) {
	if ctx.emit != nil {
		ctx.emit(stage, message, current, total)
	}
}

type OperationFunc func(OperationContext) (any, error)

var (
	keyValueSecretPattern = regexp.MustCompile(`(?i)\b(hostToken|memberToken|accessKey|inviteCode|takeoverToken|rcon\.password)=("[^"]*"|'[^']*'|[^\s,}]+)`)
	inviteCodePattern     = regexp.MustCompile(`(?i)\bACBH-[0-9A-Z]{6}-[0-9A-Z]{6}\b`)
)

type OperationManager struct {
	mu          sync.Mutex
	opts        Options
	operations  map[string]*managedOperation
	mutexOwners map[string]string
	logPath     string
}

type managedOperation struct {
	snapshot OperationSnapshot
	cancel   context.CancelFunc
	once     sync.Once
}

func NewOperationManager(opts Options) *OperationManager {
	opts = withDefaults(opts)
	return &OperationManager{
		opts:        opts,
		operations:  map[string]*managedOperation{},
		mutexOwners: map[string]string{},
		logPath:     filepath.Join(opts.AppDataDir, "logs", "desktop-debug.log"),
	}
}

func (m *OperationManager) Start(parent context.Context, opOpts OperationOptions, fn OperationFunc) (OperationSnapshot, error) {
	if opOpts.Name == "" {
		return OperationSnapshot{}, errors.New("operation name is required")
	}
	if opOpts.Timeout <= 0 {
		opOpts.Timeout = 60 * time.Second
	}
	if opOpts.MutexClass == "" {
		opOpts.MutexClass = "default"
	}
	if parent == nil {
		parent = context.Background()
	}
	now := time.Now().UTC()
	op := &managedOperation{
		snapshot: OperationSnapshot{
			OperationID:  "op_" + randomHex(10),
			Name:         opOpts.Name,
			MutexClass:   opOpts.MutexClass,
			State:        OperationQueued,
			StartedAt:    now,
			CurrentStage: "queued",
			Cancellable:  opOpts.Cancellable,
			Timeout:      opOpts.Timeout.String(),
			TraceID:      "tr_" + randomHex(12),
		},
	}
	ctx, cancel := context.WithTimeout(parent, opOpts.Timeout)
	op.cancel = cancel

	m.mu.Lock()
	if ownerID := m.mutexOwners[opOpts.MutexClass]; ownerID != "" {
		if opOpts.Coalesce {
			if owner := m.operations[ownerID]; owner != nil {
				snap := owner.snapshot
				m.mu.Unlock()
				cancel()
				return snap, nil
			}
		}
		m.mu.Unlock()
		cancel()
		return OperationSnapshot{}, fmt.Errorf("operation mutex %s is already held by %s", opOpts.MutexClass, ownerID)
	}
	m.operations[op.snapshot.OperationID] = op
	m.mutexOwners[opOpts.MutexClass] = op.snapshot.OperationID
	m.mu.Unlock()

	m.appendSummary(op.snapshot.TraceID, "queued", op.snapshot.Name, "")
	go m.run(ctx, cancel, op, fn)
	return op.snapshot, nil
}

func (m *OperationManager) run(ctx context.Context, cancel context.CancelFunc, op *managedOperation, fn OperationFunc) {
	defer cancel()

	m.update(op.snapshot.OperationID, func(s *OperationSnapshot) {
		s.State = OperationRunning
		s.CurrentStage = "running"
	})
	m.appendSummary(op.snapshot.TraceID, "start", op.snapshot.Name, "")

	opCtx := OperationContext{
		Context:     ctx,
		OperationID: op.snapshot.OperationID,
		TraceID:     op.snapshot.TraceID,
		emit: func(stage string, message string, current int64, total int64) {
			event := ProgressEvent{
				Type:        "progress",
				OperationID: op.snapshot.OperationID,
				Stage:       stage,
				Message:     message,
				Current:     current,
				Total:       total,
				At:          time.Now().UTC().Format(time.RFC3339Nano),
			}
			m.update(op.snapshot.OperationID, func(s *OperationSnapshot) {
				s.CurrentStage = stage
				s.Progress = &event
			})
			m.appendSummary(op.snapshot.TraceID, "stage", op.snapshot.Name, stage+": "+message)
		},
	}

	data, err := fn(opCtx)
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = ctx.Err()
		} else if err == nil {
			err = ctx.Err()
		}
	}
	env := envelopeFromResult(op.snapshot.TraceID, op.snapshot.StartedAt, data, err)
	if errors.Is(err, context.Canceled) {
		env.Outcome = OutcomeCancelled
		env.ErrorCode = "cancelled"
		env.OK = false
	} else if errors.Is(err, context.DeadlineExceeded) {
		env.Outcome = OutcomeTimedOut
		env.ErrorCode = "timed_out"
		env.OK = false
	}
	m.complete(op.snapshot.OperationID, env)
}

func (m *OperationManager) Cancel(operationID string) bool {
	m.mu.Lock()
	op := m.operations[operationID]
	m.mu.Unlock()
	if op == nil || op.cancel == nil || !op.snapshot.Cancellable {
		return false
	}
	op.cancel()
	return true
}

func (m *OperationManager) Summary() OperationSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	ops := make([]OperationSnapshot, 0, len(m.operations))
	busy := false
	for _, op := range m.operations {
		snap := op.snapshot
		ops = append(ops, snap)
		if snap.State == OperationQueued || snap.State == OperationRunning {
			busy = true
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].StartedAt.After(ops[j].StartedAt)
	})
	return OperationSummary{Busy: busy, Operations: ops}
}

func (m *OperationManager) complete(operationID string, env Envelope) {
	m.mu.Lock()
	op := m.operations[operationID]
	if op == nil {
		m.mu.Unlock()
		return
	}
	op.once.Do(func() {
		now := time.Now().UTC()
		op.snapshot.CompletedAt = &now
		op.snapshot.Result = &env
		op.snapshot.CurrentStage = "completed"
		switch env.Outcome {
		case OutcomeSuccess:
			op.snapshot.State = OperationSucceeded
		case OutcomeSuccessWithWarnings:
			op.snapshot.State = OperationWarning
		case OutcomePartialFailure:
			op.snapshot.State = OperationPartialFailure
		case OutcomeCancelled:
			op.snapshot.State = OperationCancelled
		case OutcomeTimedOut:
			op.snapshot.State = OperationTimedOut
		default:
			op.snapshot.State = OperationFailed
		}
		delete(m.mutexOwners, op.snapshot.MutexClass)
	})
	snap := op.snapshot
	m.mu.Unlock()

	m.appendSummary(snap.TraceID, "finish", snap.Name, string(env.Outcome)+" "+env.ErrorCode+" "+env.Message)
	m.appendDebug(env)
}

func (m *OperationManager) update(operationID string, update func(*OperationSnapshot)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op := m.operations[operationID]; op != nil {
		update(&op.snapshot)
	}
}

func (m *OperationManager) appendSummary(traceID, event, name, detail string) {
	line := fmt.Sprintf("%s traceId=%s event=%s operation=%q %s\n",
		time.Now().UTC().Format(time.RFC3339Nano), traceID, event, name, redact(detail))
	m.appendLog(line)
}

func (m *OperationManager) appendDebug(env Envelope) {
	m.appendLog(string(marshalEnvelopeDebug(redactEnvelope(env))) + "\n")
}

func (m *OperationManager) appendLog(line string) {
	if err := os.MkdirAll(filepath.Dir(m.logPath), 0o700); err != nil {
		return
	}
	rotateIfLarge(m.logPath, 4*1024*1024)
	f, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func rotateIfLarge(path string, limit int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < limit {
		return
	}
	_ = os.Rename(path, path+".1")
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func redactEnvelope(env Envelope) Envelope {
	env.Message = redact(env.Message)
	env.Warnings = redactSlice(env.Warnings)
	env.Data = redactAny(env.Data)
	return env
}

func redactSlice(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = redact(v)
	}
	return out
}

func redactAny(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return redactValue(out)
}

func redactValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		redactMap(typed)
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = redactValue(item)
		}
		return typed
	case string:
		return redact(typed)
	default:
		return typed
	}
}

func redactMap(m map[string]any) {
	for k, v := range m {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "token") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") ||
			strings.Contains(lower, "accesskey") ||
			strings.Contains(lower, "invitecode") ||
			strings.Contains(lower, "takeovertoken") {
			m[k] = "[REDACTED]"
			continue
		}
		switch typed := v.(type) {
		case map[string]any:
			redactMap(typed)
		case []any:
			for i, item := range typed {
				typed[i] = redactValue(item)
			}
		case string:
			m[k] = redact(typed)
		}
	}
}

func redact(text string) string {
	if text == "" {
		return ""
	}
	out := keyValueSecretPattern.ReplaceAllString(text, "$1=[REDACTED]")
	out = inviteCodePattern.ReplaceAllString(out, "[REDACTED_INVITE]")
	return out
}
