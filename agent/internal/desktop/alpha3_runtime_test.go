package desktop

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestOperationManagerTimeoutProducesSingleTerminalResult(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	manager := NewOperationManager(context.Background(), opts)
	snap, err := manager.Start(OperationOptions{
		Name: "slow", MutexClass: "test", Timeout: 15 * time.Millisecond, Cancellable: true,
	}, func(ctx OperationContext) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range manager.Summary().Operations {
			if op.OperationID == snap.OperationID && op.Result != nil {
				if op.State != OperationTimedOut {
					t.Fatalf("state = %s, want timed_out", op.State)
				}
				if op.Result.Outcome != OutcomeTimedOut || op.Result.ErrorCode != "timed_out" {
					t.Fatalf("result = %#v, want timed_out", op.Result)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation did not complete")
}

func TestOperationManagerImmediateCancelProducesSingleTerminalResult(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	manager := NewOperationManager(context.Background(), opts)
	snap, err := manager.Start(OperationOptions{
		Name: "cancel", MutexClass: "test", Timeout: 5 * time.Second, Cancellable: true,
	}, func(ctx OperationContext) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if result := manager.Cancel(snap.OperationID); !result.OK {
		t.Fatalf("Cancel() = %#v, want ok", result)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range manager.Summary().Operations {
			if op.OperationID == snap.OperationID && op.Result != nil {
				if op.State != OperationCancelled {
					t.Fatalf("state = %s, want cancelled", op.State)
				}
				if op.Result.Outcome != OutcomeCancelled || op.Result.ErrorCode != "cancelled" {
					t.Fatalf("result = %#v, want cancelled", op.Result)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation did not cancel")
}

func TestOperationRedactionRemovesSensitiveValues(t *testing.T) {
	raw := `hostToken=ht_secret accessKey=ak_secret inviteCode=ACBH-ABCDEF-123456 rcon.password=hunter2`
	redacted := redact(raw)
	for _, forbidden := range []string{"ht_secret", "ak_secret", "ACBH-ABCDEF-123456", "hunter2"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("redact(%q) = %q, still contains %q", raw, redacted, forbidden)
		}
	}

	nested := map[string]any{
		"items": []any{
			map[string]any{"hostToken": "ht_nested_secret"},
			"takeoverToken=tt_nested_secret",
		},
	}
	encoded, err := json.Marshal(redactAny(nested))
	if err != nil {
		t.Fatalf("marshal redacted data: %v", err)
	}
	for _, forbidden := range []string{"ht_nested_secret", "tt_nested_secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("redactAny() = %s, still contains %q", encoded, forbidden)
		}
	}
}

func TestConfigureNetworkWarningsAreSuccessWithWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/bootstrap/manifest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"packages": []map[string]any{
					{"packageId": "acbh-runtime-base-windows-amd64", "available": true},
					{"packageId": "java-21-windows-amd64", "available": false},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := ConfigureNetwork(Options{AppDataDir: t.TempDir()}, server.URL, "", freeClosedPort(t))
	if err != nil {
		t.Fatalf("ConfigureNetwork() error = %v", err)
	}
	if !result.OK || result.Outcome != string(OutcomeSuccessWithWarnings) {
		t.Fatalf("result OK/outcome = %v/%s, want true/success_with_warnings", result.OK, result.Outcome)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning for missing Java package or closed game port")
	}
}

func TestWorldBackupStatusMapsRemoteEmptySnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/groups/grp/world-backups/latest":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "Not Found", "code": "artifact_empty", "message": "No available artifact exists for this kind",
			})
		case "/v1/groups/grp/world-backups":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshots": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	opts := Options{AppDataDir: t.TempDir()}
	err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: server.URL,
		GroupID:        "grp",
		MemberID:       "mem",
		HostID:         "host",
		HostToken:      "token",
		DisplayName:    "Owner",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	status, err := WorldBackupStatus(context.Background(), opts)
	if err != nil {
		t.Fatalf("WorldBackupStatus() error = %v", err)
	}
	if status.State != "empty" || status.HistoryCount != 0 || status.RemoteLatestError != "" {
		t.Fatalf("status = %#v, want empty without remote error", status)
	}
}

func freeClosedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return strconv.Itoa(addr.Port)
}
