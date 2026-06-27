package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

func TestBootstrapSkipsLeaseWhenCapabilitiesMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	opts := Options{AppDataDir: t.TempDir()}
	err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: server.URL,
		GroupID:        "grp_test",
		MemberID:       "mem_test",
		HostID:         "host_test",
		HostToken:      "token_test",
		DisplayName:    "Tester",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}

	result, err := RunBootstrap(OperationContext{Context: context.Background()}, opts)
	if err != nil {
		t.Fatalf("RunBootstrap() error = %v", err)
	}
	if result.Outcome != OutcomeSuccessWithWarnings {
		t.Fatalf("outcome = %s, want success_with_warnings", result.Outcome)
	}
	if result.OK {
		for _, step := range result.Steps {
			if step.Name == "RefreshLeaseStatus" && step.State == BootstrapStepFailed && step.ErrorCode == "lease_status_failed" {
				t.Fatalf("unexpected lease failure step: %#v", step)
			}
		}
	}
	for _, warning := range result.Warnings {
		if containsNestedFailureEnvelope(warning) {
			t.Fatalf("warning contains nested failure envelope: %q", warning)
		}
	}
}

func TestBootstrapLeaseRouteMissingUsesCapabilityRouteMissing(t *testing.T) {
	leaseStatusCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health", "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "coordinatorVersion": "0.4.0-alpha3", "protocolVersion": 2,
				"minimumClientProtocol": 2,
				"capabilities": []string{"lease_renew_v1", "world_backup_v1"},
				"serverTime": time.Now().UTC().Format(time.RFC3339Nano),
			})
		case "/v1/groups/grp_test/lease/status":
			leaseStatusCalled = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	opts := Options{AppDataDir: t.TempDir()}
	err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: server.URL,
		GroupID:        "grp_test",
		MemberID:       "mem_test",
		HostID:         "host_test",
		HostToken:      "token_test",
		DisplayName:    "Tester",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}

	result, err := RunBootstrap(OperationContext{Context: context.Background()}, opts)
	if err != nil {
		t.Fatalf("RunBootstrap() error = %v", err)
	}
	if !leaseStatusCalled {
		t.Fatal("expected lease status probe when capability claims lease_renew_v1")
	}
	found := false
	for _, step := range result.Steps {
		if step.Name == "RefreshLeaseStatus" && step.ErrorCode == "coordinator_capability_route_missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steps = %#v, want coordinator_capability_route_missing on RefreshLeaseStatus", result.Steps)
	}
	if result.Outcome == OutcomeFailure && result.ErrorCode == "operation_failed" {
		t.Fatalf("bootstrap should not surface operation_failed for route missing: %#v", result)
	}
}

func TestEnvelopeStateMatchesTerminalOutcome(t *testing.T) {
	manager := NewOperationManager(context.Background(), Options{AppDataDir: t.TempDir()})
	snap, err := manager.Start(OperationOptions{Name: "bootstrap-test", MutexClass: "test", Timeout: 2 * time.Second}, func(ctx OperationContext) (any, error) {
		return BootstrapResult{OK: true, Outcome: OutcomeSuccessWithWarnings, Message: "warn", Warnings: []string{"Coordinator 不支持 lease_renew_v1"}}, nil
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		op, ok := manager.Get(snap.OperationID)
		if ok && op.Result != nil {
			if op.State != OperationWarning {
				t.Fatalf("state = %s, want success_with_warnings", op.State)
			}
			if op.Result.Outcome != OutcomeSuccessWithWarnings || op.Result.OK != true {
				t.Fatalf("terminalResult = %#v", op.Result)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("operation did not complete")
}

func TestRouteMissingMapsToRouteNotFoundCode(t *testing.T) {
	err := &coordinator.APIError{StatusCode: http.StatusNotFound, Code: "route_not_found", Message: "missing"}
	code := inferErrorCode(err)
	if code != "route_not_found" {
		t.Fatalf("inferErrorCode() = %q, want route_not_found", code)
	}
}

func containsNestedFailureEnvelope(warning string) bool {
	return len(warning) > 0 && (json.Valid([]byte(warning)) || containsSubstring(warning, `"outcome":"failure"`))
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexSubstring(s, sub) >= 0)
}

func indexSubstring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}