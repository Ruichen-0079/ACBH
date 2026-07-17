package agentlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxBytes = 20 * 1024 * 1024
	DefaultMaxFiles = 5
)

type Record struct {
	Time        time.Time `json:"time"`
	Level       string    `json:"level"`
	Event       string    `json:"event"`
	Component   string    `json:"component,omitempty"`
	OperationID string    `json:"operation_id,omitempty"`
	From        string    `json:"from,omitempty"`
	To          string    `json:"to,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	Message     string    `json:"message,omitempty"`
}

type Writer interface {
	Write(Record, ...string) error
}

type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
}

func New(path string, maxBytes int64, maxFiles int) (*RotatingWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("log path is required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &RotatingWriter{path: path, maxBytes: maxBytes, maxFiles: maxFiles}, nil
}

func (w *RotatingWriter) Write(record Record, secrets ...string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if record.Time.IsZero() {
		record.Time = time.Now().UTC()
	}
	record.Message = redact(record.Message, secrets)
	record.Reason = redact(record.Reason, secrets)
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := w.rotateIfNeeded(int64(len(data))); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (w *RotatingWriter) rotateIfNeeded(nextBytes int64) error {
	info, err := os.Stat(w.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size()+nextBytes <= w.maxBytes {
		return nil
	}
	oldest := fmt.Sprintf("%s.%d", w.path, w.maxFiles-1)
	if w.maxFiles > 1 {
		if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for index := w.maxFiles - 2; index >= 1; index-- {
			from := fmt.Sprintf("%s.%d", w.path, index)
			to := fmt.Sprintf("%s.%d", w.path, index+1)
			if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := os.Rename(w.path, w.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := os.Remove(w.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func redact(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

type DiscardWriter struct{}

func (DiscardWriter) Write(Record, ...string) error { return nil }
