package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestDaemonAutoTakeoverRequiresServerDirAndCommand(t *testing.T) {
	_, err := executeCommand("daemon", "--auto-takeover=true")
	if err == nil || !strings.Contains(err.Error(), "--server-dir and --command are required") {
		t.Fatalf("error = %v, want --server-dir/--command message", err)
	}

	_, err = executeCommand("daemon", "--auto-takeover=true", "--server-dir", "/tmp")
	if err == nil || !strings.Contains(err.Error(), "--server-dir and --command are required") {
		t.Fatalf("error = %v, want --server-dir/--command message", err)
	}
}

func TestDaemonAutoTakeoverDefaultOff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hosts/takeover/poll" {
			http.Error(w, "unexpected poll", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/v1/hosts/heartbeat" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"hostId":"host_1","status":"standby"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{"daemon", "--interval", "500ms"})
	err := cmd.ExecuteContext(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("daemon error = %v", err)
	}
}

func TestDaemonAutoTakeoverDefaultLogDir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{
				"groupId":"group_1",
				"currentHostId":"host_1",
				"currentHostGeneration":1,
				"lastElection":null,
				"activeTakeoverAssignment":null
			}`))
		case "/v1/hosts/heartbeat":
			_, _ = w.Write([]byte(`{"ok":true,"hostId":"host_1","status":"hosting"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := newRootCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"daemon", "--auto-takeover=true",
		"--server-dir", t.TempDir(),
		"--command", "echo test",
		"--interval", "500ms",
	})
	err := cmd.ExecuteContext(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("daemon error = %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Log dir:") {
		t.Fatal("output missing Log dir line")
	}
	if strings.Contains(output, "Log dir: ") {
		idx := strings.Index(output, "Log dir: ")
		rest := output[idx+len("Log dir: "):]
		endIdx := strings.IndexByte(rest, '\n')
		logDir := rest
		if endIdx >= 0 {
			logDir = rest[:endIdx]
		}
		if logDir == "" {
			t.Fatal("Log dir default is empty")
		}
	}
}

func TestDaemonAutoTakeoverAlreadyCurrentHost(t *testing.T) {
	pollCalled := &atomic.Bool{}
	heartbeatStatuses := make(chan string, 20)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			_, _ = w.Write([]byte(`{
				"groupId":"group_1",
				"currentHostId":"host_1",
				"currentHostGeneration":1,
				"lastElection":null,
				"activeTakeoverAssignment":null
			}`))
		case "/v1/hosts/heartbeat":
			var req coordinator.HeartbeatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			heartbeatStatuses <- req.Status
			_, _ = w.Write([]byte(`{"ok":true,"hostId":"host_1","status":"` + req.Status + `"}`))
		case "/v1/hosts/takeover/poll":
			pollCalled.Store(true)
			_, _ = w.Write([]byte(`{"assignment":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configureTestAgent(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"daemon", "--auto-takeover=true",
		"--server-dir", t.TempDir(),
		"--command", "echo test",
		"--interval", "500ms",
		"--log-dir", t.TempDir(),
	})
	err := cmd.ExecuteContext(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("daemon error = %v", err)
	}

	if pollCalled.Load() {
		t.Fatal("PollTakeover was called even though host is already current")
	}

	close(heartbeatStatuses)
	foundHosting := false
	for s := range heartbeatStatuses {
		if s == "hosting" {
			foundHosting = true
		}
	}
	if !foundHosting {
		t.Fatal("expected at least one hosting heartbeat when already current host")
	}
}

type fakeTakeoverClient struct {
	assignment    *coordinator.TakeoverAssignment
	calls         []string
	failureReason string
	acceptErr     error
}

func (f *fakeTakeoverClient) PollTakeover(_ context.Context, _ coordinator.TakeoverPollRequest) (coordinator.TakeoverPollResponse, error) {
	f.calls = append(f.calls, "poll")
	return coordinator.TakeoverPollResponse{Assignment: f.assignment}, nil
}

func (f *fakeTakeoverClient) AcceptTakeover(_ context.Context, _ coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error) {
	f.calls = append(f.calls, "accept")
	return coordinator.TakeoverActionResponse{OK: true}, f.acceptErr
}

func (f *fakeTakeoverClient) CompleteTakeover(_ context.Context, _ coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error) {
	f.calls = append(f.calls, "complete")
	return coordinator.TakeoverActionResponse{OK: true}, nil
}

func (f *fakeTakeoverClient) FailTakeover(_ context.Context, req coordinator.TakeoverFailRequest) (coordinator.TakeoverActionResponse, error) {
	f.calls = append(f.calls, "fail")
	f.failureReason = req.FailureReason
	return coordinator.TakeoverActionResponse{OK: true}, nil
}

func noopPull(context.Context, manifest.ArtifactKind, string, string, bool) error { return nil }
func noopStart(context.Context) error                                              { return nil }
func noopHeartbeat(context.Context, string) error                                   { return nil }

func TestRunAutoTakeoverWithDepsNoAssignment(t *testing.T) {
	client := &fakeTakeoverClient{}
	cmd := newRootCmd()

	completed, err := runAutoTakeoverWithDeps(
		context.Background(),
		cmd,
		client,
		coordinator.ElectionAuthRequest{GroupID: "g", HostID: "h", HostToken: "t"},
		filepath.Join(t.TempDir(), "takeover.json"),
		"/tmp/server",
		noopPull, noopStart, noopHeartbeat,
	)
	if err != nil {
		t.Fatalf("runAutoTakeoverWithDeps() error = %v", err)
	}
	if completed {
		t.Fatal("expected completed=false with no assignment")
	}
	if len(client.calls) != 1 || client.calls[0] != "poll" {
		t.Fatalf("calls = %#v, want [poll]", client.calls)
	}
}

func TestRunAutoTakeoverWithDepsExecutesAssignment(t *testing.T) {
	client := &fakeTakeoverClient{
		assignment: makeTestAssignment(),
	}
	cmd := newRootCmd()

	completed, err := runAutoTakeoverWithDeps(
		context.Background(),
		cmd,
		client,
		coordinator.ElectionAuthRequest{GroupID: "g", HostID: "h", HostToken: "t"},
		filepath.Join(t.TempDir(), "takeover.json"),
		"/tmp/server",
		noopPull, noopStart, noopHeartbeat,
	)
	if err != nil {
		t.Fatalf("runAutoTakeoverWithDeps() error = %v", err)
	}
	if !completed {
		t.Fatal("expected completed=true")
	}
	wantCalls := []string{"poll", "accept", "complete"}
	if len(client.calls) != len(wantCalls) {
		t.Fatalf("calls = %#v, want %#v", client.calls, wantCalls)
	}
	for i, c := range wantCalls {
		if client.calls[i] != c {
			t.Fatalf("calls[%d] = %q, want %q", i, client.calls[i], c)
		}
	}
}

func TestRunAutoTakeoverWithDepsFailureReportsFail(t *testing.T) {
	client := &fakeTakeoverClient{
		assignment: makeTestAssignment(),
	}
	cmd := newRootCmd()

	pullFail := errors.New("download failed")
	completed, err := runAutoTakeoverWithDeps(
		context.Background(),
		cmd,
		client,
		coordinator.ElectionAuthRequest{GroupID: "g", HostID: "h", HostToken: "t"},
		filepath.Join(t.TempDir(), "takeover.json"),
		"/tmp/server",
		func(_ context.Context, _ manifest.ArtifactKind, _ string, _ string, _ bool) error {
			return pullFail
		},
		noopStart, noopHeartbeat,
	)
	if err == nil {
		t.Fatal("expected error from failed pull")
	}
	if completed {
		t.Fatal("expected completed=false on failure")
	}
	if client.failureReason != "pull-world-snapshot-failed" {
		t.Fatalf("failureReason = %q, want pull-world-snapshot-failed", client.failureReason)
	}
	wantCalls := []string{"poll", "accept", "fail"}
	if len(client.calls) != len(wantCalls) {
		t.Fatalf("calls = %#v, want %#v", client.calls, wantCalls)
	}
	for i, c := range wantCalls {
		if client.calls[i] != c {
			t.Fatalf("calls[%d] = %q, want %q", i, client.calls[i], c)
		}
	}
}

func TestRunAutoTakeoverWithDepsAcceptFailureDoesNotCallFail(t *testing.T) {
	client := &fakeTakeoverClient{
		assignment: makeTestAssignment(),
	}
	client.acceptErr = errors.New("accept rejected")
	cmd := newRootCmd()

	completed, err := runAutoTakeoverWithDeps(
		context.Background(),
		cmd,
		client,
		coordinator.ElectionAuthRequest{GroupID: "g", HostID: "h", HostToken: "t"},
		filepath.Join(t.TempDir(), "takeover.json"),
		"/tmp/server",
		noopPull, noopStart, noopHeartbeat,
	)
	if err == nil {
		t.Fatal("expected error from failed accept")
	}
	if completed {
		t.Fatal("expected completed=false on accept failure")
	}
	for _, c := range client.calls {
		if c == "fail" {
			t.Fatal("FailTakeover was called before accept succeeded")
		}
	}
}

func TestDaemonAutoTakeoverRootIncludesNewFlags(t *testing.T) {
	cmd := newRootCmd()
	found, _, err := cmd.Find([]string{"daemon"})
	if err != nil || found == nil {
		t.Fatalf("Find(daemon) = %#v, %v", found, err)
	}
	flag := found.Flags().Lookup("auto-takeover")
	if flag == nil {
		t.Fatal("daemon command missing --auto-takeover flag")
	}
	flag = found.Flags().Lookup("takeover-interval")
	if flag == nil {
		t.Fatal("daemon command missing --takeover-interval flag")
	}
}

func makeTestAssignment() *coordinator.TakeoverAssignment {
	return &coordinator.TakeoverAssignment{
		AssignmentID:          "takeover_auto",
		GroupID:               "group_1",
		HostID:                "host_1",
		TakeoverToken:         "token-auto",
		CurrentHostGeneration: 1,
		LatestArtifactsAtAssignment: map[string]string{
			"server-pack":    "pack_1",
			"admin-state":    "admin_1",
			"world-snapshot": "snap_1",
		},
		ExpiresAt: "2026-06-06T00:01:00Z",
	}
}
