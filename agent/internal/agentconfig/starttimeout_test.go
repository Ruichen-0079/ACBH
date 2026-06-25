package agentconfig

import (
	"strings"
	"testing"
	"time"
)

func TestResolvedStartTimeoutDefaults(t *testing.T) {
	got, err := (ServerConfig{}).ResolvedStartTimeout()
	if err != nil {
		t.Fatalf("ResolvedStartTimeout() error = %v", err)
	}
	if got != DefaultServerStartTimeout {
		t.Fatalf("ResolvedStartTimeout() = %v, want %v", got, DefaultServerStartTimeout)
	}
}

func TestResolvedStartTimeoutCustom(t *testing.T) {
	got, err := (ServerConfig{StartTimeout: "45s"}).ResolvedStartTimeout()
	if err != nil {
		t.Fatalf("ResolvedStartTimeout() error = %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("ResolvedStartTimeout() = %v, want 45s", got)
	}
}

func TestResolvedStartTimeoutRejectsTooSmall(t *testing.T) {
	_, err := (ServerConfig{StartTimeout: "5s"}).ResolvedStartTimeout()
	if err == nil || !strings.Contains(err.Error(), "at least") {
		t.Fatalf("ResolvedStartTimeout() error = %v, want minimum validation error", err)
	}
}

func TestResolvedStartTimeoutRejectsInvalid(t *testing.T) {
	_, err := (ServerConfig{StartTimeout: "not-a-duration"}).ResolvedStartTimeout()
	if err == nil || !strings.Contains(err.Error(), "invalid server.startTimeout") {
		t.Fatalf("ResolvedStartTimeout() error = %v, want invalid duration error", err)
	}
}