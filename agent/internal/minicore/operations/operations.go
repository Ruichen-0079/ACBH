package operations

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

type State string

const (
	Running State = "running"
	Success State = "success"
	Failed  State = "failed"
)

type Operation struct {
	OperationID string               `json:"operationId"`
	TraceID     string               `json:"traceId"`
	Name        string               `json:"name"`
	State       State                `json:"state"`
	Stage       string               `json:"stage"`
	Progress    int                  `json:"progress"`
	StartedAt   string               `json:"startedAt"`
	CompletedAt string               `json:"completedAt,omitempty"`
	ErrorCode   coreerrors.ErrorCode `json:"errorCode,omitempty"`
	Message     string               `json:"message"`
	Error       *coreerrors.Error    `json:"error,omitempty"`
	Result      any                  `json:"result,omitempty"`
}

type Store struct {
	mu         sync.Mutex
	operations map[string]Operation
	order      []string
}

func NewStore() *Store {
	return &Store{operations: map[string]Operation{}}
}

func (s *Store) Start(name string, stage string, message string) Operation {
	op := Operation{
		OperationID: "op_" + randomHex(8),
		TraceID:     "tr_" + randomHex(8),
		Name:        name,
		State:       Running,
		Stage:       stage,
		Progress:    0,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
		Message:     message,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operations[op.OperationID] = op
	s.order = append(s.order, op.OperationID)
	return op
}

func (s *Store) Update(op Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operations[op.OperationID] = op
}

func (s *Store) Complete(op Operation, result any, message string) Operation {
	op.State = Success
	op.Stage = "completed"
	op.Progress = 100
	op.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	op.Message = message
	op.Result = result
	s.Update(op)
	return op
}

func (s *Store) Fail(op Operation, err *coreerrors.Error) Operation {
	op.State = Failed
	op.Stage = "failed"
	op.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		op.ErrorCode = err.ErrorCode
		op.Message = err.Message
		op.Error = err
	}
	s.Update(op)
	return op
}

func (s *Store) List() []Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Operation, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.operations[id])
	}
	return out
}

func (s *Store) Get(id string) (Operation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.operations[id]
	return op, ok
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000")))
	}
	return hex.EncodeToString(buf)
}
