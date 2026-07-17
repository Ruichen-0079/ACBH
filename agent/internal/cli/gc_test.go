package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGcDefaultsToDryRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{"groupId":"group_1","currentHostId":"host_1","currentHostGeneration":1,"lastElection":null,"activeTakeoverAssignment":null}`))
		case "/v1/groups/group_1/artifacts/gc":
			var req struct {
				DryRun bool `json:"dryRun"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if !req.DryRun {
				t.Error("expected dryRun=true by default")
			}
			_, _ = w.Write([]byte(`{"dryRun":true,"deletedArtifacts":[],"deletedObjectCount":0,"protectedArtifactIds":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)
	out, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("gc command error = %v", err)
	}
	if !strings.Contains(out, "Dry run") {
		t.Fatalf("gc output missing dry run info: %q", out)
	}
}

func TestGcExecuteFlag(t *testing.T) {
	dryRunReceived := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{"groupId":"group_1","currentHostId":"host_1","currentHostGeneration":1,"lastElection":null,"activeTakeoverAssignment":null}`))
		case "/v1/groups/group_1/artifacts/gc":
			var req struct {
				DryRun bool `json:"dryRun"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			dryRunReceived = req.DryRun
			_, _ = w.Write([]byte(`{"dryRun":false,"deletedArtifacts":[{"groupId":"g","artifactKind":"world-snapshot","artifactId":"old","status":"available"}],"deletedObjectCount":3,"protectedArtifactIds":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)
	out, err := executeCommand("gc", "--execute")
	if err != nil {
		t.Fatalf("gc --execute error = %v", err)
	}
	if dryRunReceived {
		t.Fatal("expected dryRun=false with --execute")
	}
	if !strings.Contains(out, "Deleted") {
		t.Fatalf("gc output missing delete count: %q", out)
	}
}

func TestGcDryRunReportsRetainedManifestBlocker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{"groupId":"group_1","currentHostId":"host_1","currentHostGeneration":1,"lastElection":null,"activeTakeoverAssignment":null}`))
		case "/v1/groups/group_1/artifacts/gc":
			_, _ = w.Write([]byte(`{"dryRun":true,"blocked":true,"blockers":[{"groupId":"group_1","artifactKind":"world-snapshot","artifactId":"snap_retained","reason":"manifest does not exist"}],"deletedArtifacts":[{"groupId":"group_1","artifactKind":"world-snapshot","artifactId":"snap_candidate","status":"rejected"}],"deletedObjectCount":0,"protectedArtifactIds":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)
	out, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("gc command error = %v", err)
	}
	if !strings.Contains(out, "Dry run blocked") || !strings.Contains(out, "object deletion is not safe") {
		t.Fatalf("gc output missing blocked warning: %q", out)
	}
	if strings.Contains(out, "would be deleted") {
		t.Fatalf("gc output misleadingly reports safe deletion: %q", out)
	}
}

func TestGcDryRunAndExecuteMutuallyExclusive(t *testing.T) {
	_, err := executeCommand("gc", "--dry-run", "--execute")
	if err == nil {
		t.Fatal("expected error when --dry-run and --execute are both set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want mutually exclusive message", err)
	}
}

func TestGc403ErrorClearMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{"groupId":"group_1","currentHostId":"host_2","currentHostGeneration":1,"lastElection":null,"activeTakeoverAssignment":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)
	_, err := executeCommand("gc")
	if err == nil {
		t.Fatal("expected error for non-current host GC")
	}
	if !strings.Contains(err.Error(), "only the current host may run garbage collection") {
		t.Fatalf("error = %v, want current host message", err)
	}
}

func TestGc409ErrorClearMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{"groupId":"group_1","currentHostId":"host_1","currentHostGeneration":2,"lastElection":null,"activeTakeoverAssignment":null}`))
		case "/v1/groups/group_1/artifacts/gc":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"Conflict","message":"Host generation is stale; current host may have changed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)
	_, err := executeCommand("gc", "--execute")
	if err == nil {
		t.Fatal("expected error for stale generation")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("error = %v, want stale generation message", err)
	}
}

func TestGcRetainedManifest409ErrorClearMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{"groupId":"group_1","currentHostId":"host_1","currentHostGeneration":1,"lastElection":null,"activeTakeoverAssignment":null}`))
		case "/v1/groups/group_1/artifacts/gc":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"Conflict","message":"Artifact GC blocked by retained manifest read failure"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)
	_, err := executeCommand("gc", "--execute")
	if err == nil {
		t.Fatal("expected error for retained manifest failure")
	}
	if !strings.Contains(err.Error(), "retained manifest could not be read") {
		t.Fatalf("error = %v, want retained manifest message", err)
	}
	if strings.Contains(err.Error(), "generation is stale") {
		t.Fatalf("error = %v, should not report a generation failure", err)
	}
}

func TestGcCommandExistsInRoot(t *testing.T) {
	cmd := newRootCmd()
	found, _, err := cmd.Find([]string{"gc"})
	if err != nil || found == nil || found.Name() != "gc" {
		t.Fatalf("Find(gc) = %#v, %v", found, err)
	}
	flag := found.Flags().Lookup("execute")
	if flag == nil {
		t.Fatal("gc command missing --execute flag")
	}
	flag = found.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("gc command missing --dry-run flag")
	}
}
