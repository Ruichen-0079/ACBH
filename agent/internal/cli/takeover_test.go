package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/takeover"
)

func TestBuildHeartbeatRequestIncludesHintsAndConnection(t *testing.T) {
	cfg := testAgentConfig("http://127.0.0.1")
	req, err := buildHeartbeatRequest(cfg, heartbeatOptions{
		status:              "standby",
		latestWorldSnapshot: "snap_1",
		latestServerPack:    "pack_1",
		latestAdminState:    "admin_1",
		javaAvailable:       "false",
		connectionHost:      "100.64.0.10",
		connectionPort:      25565,
		connectionNetwork:   "tailscale",
	})
	if err != nil {
		t.Fatalf("buildHeartbeatRequest() error = %v", err)
	}
	if req.LatestLocalArtifacts["world-snapshot"] != "snap_1" ||
		req.LatestLocalArtifacts["server-pack"] != "pack_1" ||
		req.LatestLocalArtifacts["admin-state"] != "admin_1" {
		t.Fatalf("LatestLocalArtifacts = %#v", req.LatestLocalArtifacts)
	}
	if req.HostScoreHints == nil || req.HostScoreHints.JavaAvailable == nil ||
		*req.HostScoreHints.JavaAvailable || req.HostScoreHints.CPUCores < 1 {
		t.Fatalf("HostScoreHints = %#v", req.HostScoreHints)
	}
	if req.Connection == nil || req.Connection.Host != "100.64.0.10" ||
		req.Connection.Port != 25565 || req.Connection.Network != "tailscale" {
		t.Fatalf("Connection = %#v", req.Connection)
	}
}

func TestElectionCommandsAndTakeoverStateCommands(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/v1/groups/group_1/election/status":
			if r.Header.Get("X-ACBH-Host-Token") != "host-secret" {
				t.Error("status request missing host token")
			}
			_, _ = w.Write([]byte(`{
				"groupId":"group_1",
				"currentHostId":null,
				"currentHostGeneration":0,
				"lastElection":{"electionId":"election_1","groupId":"group_1","reason":"manual","selectedHostId":"host_1","currentHostGeneration":0,"assignmentId":"takeover_1","candidates":[],"createdAt":"2026-06-06T00:00:00Z"},
				"activeTakeoverAssignment":null
			}`))
		case "/v1/groups/group_1/election/check-timeout":
			var req coordinator.ElectionAuthRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.GroupID != "group_1" || req.HostID != "host_1" || req.HostToken != "host-secret" {
				t.Errorf("check timeout request = %#v", req)
			}
			_, _ = w.Write([]byte(`{"electionNeeded":false,"election":null}`))
		case "/v1/hosts/takeover/poll":
			var poll coordinator.TakeoverPollRequest
			_ = json.NewDecoder(r.Body).Decode(&poll)
			_, _ = w.Write([]byte(`{
				"assignment":{
					"assignmentId":"takeover_1",
					"groupId":"group_1",
					"hostId":"host_1",
					"reason":"manual",
					"status":"offered",
					"takeoverToken":"takeover-secret",
					"currentHostGeneration":0,
					"latestArtifactsAtAssignment":{"world-snapshot":"snap_1"},
					"createdAt":"2026-06-06T00:00:00Z",
					"expiresAt":"2026-06-06T00:01:00Z",
					"acceptedAt":null,
					"completedAt":null,
					"failedAt":null,
					"failureReason":null
				}
			}`))
		case "/v1/hosts/takeover/accept":
			assertTakeoverActionBody(t, r, "")
			_, _ = w.Write([]byte(takeoverActionResponse("accepted")))
		case "/v1/hosts/takeover/complete":
			assertTakeoverActionBody(t, r, "")
			_, _ = w.Write([]byte(takeoverActionResponse("completed")))
		case "/v1/hosts/takeover/fail":
			assertTakeoverActionBody(t, r, "pull-failed")
			_, _ = w.Write([]byte(takeoverActionResponse("failed")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configDir := configureTestAgent(t, server.URL)
	statusOut, err := executeCommand("election", "status")
	if err != nil || !strings.Contains(statusOut, "Last selected host: host_1") {
		t.Fatalf("election status = %q, %v", statusOut, err)
	}
	checkOut, err := executeCommand("election", "check-timeout")
	if err != nil || !strings.Contains(checkOut, "Election needed: false") {
		t.Fatalf("check timeout = %q, %v", checkOut, err)
	}
	pollOut, err := executeCommand("takeover", "poll")
	if err != nil {
		t.Fatalf("takeover poll error = %v", err)
	}
	if strings.Contains(pollOut, "takeover-secret") || strings.Contains(pollOut, "host-secret") {
		t.Fatalf("poll output exposed secret: %q", pollOut)
	}
	statePath := takeover.StatePath(configDir)
	state, err := takeover.LoadState(statePath)
	if err != nil || state.AssignmentID != "takeover_1" || state.TakeoverToken != "takeover-secret" {
		t.Fatalf("takeover state = %#v, %v", state, err)
	}

	if _, err := executeCommand("takeover", "accept"); err != nil {
		t.Fatalf("takeover accept error = %v", err)
	}
	if _, err := executeCommand("takeover", "complete"); err != nil {
		t.Fatalf("takeover complete error = %v", err)
	}
	if _, err := takeover.LoadState(statePath); err != takeover.ErrStateNotFound {
		t.Fatalf("state after complete error = %v", err)
	}

	if err := takeover.SaveState(statePath, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	failOut, err := executeCommand("takeover", "fail", "--reason", "pull-failed")
	if err != nil || !strings.Contains(failOut, "Takeover assignment failed") {
		t.Fatalf("takeover fail = %q, %v", failOut, err)
	}
	if strings.Contains(failOut, "takeover-secret") || strings.Contains(failOut, "host-secret") {
		t.Fatalf("fail output exposed secret: %q", failOut)
	}
	if len(requests) < 6 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRootIncludesElectionAndTakeoverCommands(t *testing.T) {
	cmd := newRootCmd()
	for _, path := range [][]string{
		{"election", "status"},
		{"election", "check-timeout"},
		{"takeover", "poll"},
		{"takeover", "accept"},
		{"takeover", "complete"},
		{"takeover", "fail"},
		{"takeover", "run"},
	} {
		found, _, err := cmd.Find(path)
		if err != nil || found == nil || found.Name() != path[len(path)-1] {
			t.Fatalf("Find(%v) = %#v, %v", path, found, err)
		}
	}
}

func configureTestAgent(t *testing.T, coordinatorURL string) string {
	t.Helper()
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", configRoot)
	}
	configPath, err := agentconfig.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	cfg := testAgentConfig(coordinatorURL)
	if err := agentconfig.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return filepath.Dir(configPath)
}

func testAgentConfig(coordinatorURL string) agentconfig.Config {
	return agentconfig.Config{
		CoordinatorURL: coordinatorURL,
		GroupID:        "group_1",
		MemberID:       "member_1",
		HostID:         "host_1",
		HostToken:      "host-secret",
		DisplayName:    "Host One",
		DeviceName:     "host-one",
		Platform:       runtime.GOOS,
		AgentVersion:   agentconfig.AgentVersion,
	}
}

func assertTakeoverActionBody(t *testing.T, r *http.Request, wantFailureReason string) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode action body: %v", err)
		return
	}
	if body["groupId"] != "group_1" || body["hostId"] != "host_1" ||
		body["hostToken"] != "host-secret" || body["assignmentId"] != "takeover_1" ||
		body["takeoverToken"] != "takeover-secret" {
		t.Errorf("action body = %#v", body)
	}
	if wantFailureReason != "" && body["failureReason"] != wantFailureReason {
		t.Errorf("failureReason = %#v", body["failureReason"])
	}
}

func takeoverActionResponse(status string) string {
	return `{"ok":true,"assignment":{"assignmentId":"takeover_1","groupId":"group_1","hostId":"host_1","reason":"manual","status":"` +
		status +
		`","currentHostGeneration":0,"latestArtifactsAtAssignment":{},"createdAt":"2026-06-06T00:00:00Z","expiresAt":"2026-06-06T00:01:00Z","acceptedAt":null,"completedAt":null,"failedAt":null,"failureReason":null}}`
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
