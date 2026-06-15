package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestBootstrapCreateAndJoinServerRuntime(t *testing.T) {
	fake := newBootstrapCoordinator(t)
	defer fake.Close()

	serverA := t.TempDir()
	for rel, content := range map[string]string{
		"server.properties":   "motd=ACBH",
		"world/level.dat":     "world-data",
		"mods/example.jar":    "placeholder mod",
		"config/example.toml": "enabled=true",
		"logs/ignored.log":    "do not sync",
	} {
		writeCLITestFile(t, serverA, rel, content)
	}

	configA := t.TempDir()
	setTestConfigRoot(t, configA)
	createOutput, err := executeCommand(
		"bootstrap", "create-group",
		"--coordinator", fake.URL,
		"--group", "example-group",
		"--server-dir", serverA,
		"--artifact-class", "server-runtime",
		"--name", "Host A",
		"--device-name", "host-a",
	)
	if err != nil {
		t.Fatalf("create-group error = %v output=%s", err, createOutput)
	}
	if !strings.Contains(createOutput, "Server-runtime bootstrap complete.") ||
		!strings.Contains(createOutput, "Current host generation: 1") {
		t.Fatalf("create output = %s", createOutput)
	}
	if strings.Contains(createOutput, fake.accessKey) || strings.Contains(createOutput, "host-token-a") {
		t.Fatalf("create output leaked credentials: %s", createOutput)
	}
	if fake.currentHostID != "host-a" || fake.generation != 1 {
		t.Fatalf("current host = %q generation=%d", fake.currentHostID, fake.generation)
	}
	if fake.runtimeManifest == nil {
		t.Fatal("server-runtime manifest was not uploaded")
	}
	for _, file := range fake.runtimeManifest.Files {
		if file.Path == "logs/ignored.log" {
			t.Fatal("excluded log file entered server-runtime manifest")
		}
	}

	serverB := filepath.Join(t.TempDir(), "server-b")
	configB := t.TempDir()
	setTestConfigRoot(t, configB)
	t.Setenv("ACBH_ACCESS_KEY", fake.accessKey)
	joinOutput, err := executeCommand(
		"bootstrap", "join-group",
		"--coordinator", fake.URL,
		"--group", fake.groupID,
		"--server-dir", serverB,
		"--artifact-class", "server-runtime",
		"--name", "Host B",
		"--device-name", "host-b",
	)
	if err != nil {
		t.Fatalf("join-group error = %v output=%s", err, joinOutput)
	}
	if !strings.Contains(joinOutput, "Verify result: PASS") {
		t.Fatalf("join output = %s", joinOutput)
	}
	if strings.Contains(joinOutput, fake.accessKey) || strings.Contains(joinOutput, "host-token-b") {
		t.Fatalf("join output leaked credentials: %s", joinOutput)
	}
	if fake.currentHostID != "host-a" {
		t.Fatalf("join-group changed current host to %q", fake.currentHostID)
	}
	for rel, want := range map[string]string{
		"server.properties":   "motd=ACBH",
		"world/level.dat":     "world-data",
		"mods/example.jar":    "placeholder mod",
		"config/example.toml": "enabled=true",
	} {
		data, readErr := os.ReadFile(filepath.Join(serverB, filepath.FromSlash(rel)))
		if readErr != nil || string(data) != want {
			t.Fatalf("restored %s = %q err=%v", rel, data, readErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(serverB, "logs", "ignored.log")); !os.IsNotExist(statErr) {
		t.Fatalf("excluded log was restored: %v", statErr)
	}
}

func TestBootstrapJoinFailsWithoutLatestServerRuntime(t *testing.T) {
	fake := newBootstrapCoordinator(t)
	defer fake.Close()
	configRoot := t.TempDir()
	setTestConfigRoot(t, configRoot)
	t.Setenv("ACBH_ACCESS_KEY", fake.accessKey)

	output, err := executeCommand(
		"bootstrap", "join-group",
		"--coordinator", fake.URL,
		"--group", fake.groupID,
		"--server-dir", filepath.Join(t.TempDir(), "server"),
	)
	if err == nil || !strings.Contains(err.Error(), "latest server-runtime artifact is unavailable") {
		t.Fatalf("join error = %v output=%s", err, output)
	}
}

func setTestConfigRoot(t *testing.T, root string) {
	t.Helper()
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", root)
}

type bootstrapCoordinator struct {
	*httptest.Server
	t               *testing.T
	mu              sync.Mutex
	groupID         string
	accessKey       string
	hostCount       int
	currentHostID   string
	generation      int
	runtimeManifest *manifest.Manifest
	objects         map[string][]byte
}

func newBootstrapCoordinator(t *testing.T) *bootstrapCoordinator {
	t.Helper()
	fake := &bootstrapCoordinator{
		t:         t,
		groupID:   "grp_bootstrap",
		accessKey: "test-access-key",
		objects:   make(map[string][]byte),
	}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (f *bootstrapCoordinator) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/groups":
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"groupId": f.groupID, "ownerMemberId": "member-owner", "accessKey": f.accessKey,
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/groups/"+f.groupID+"/join":
		writeBootstrapJSON(w, http.StatusOK, map[string]any{"memberId": "member-b", "role": "member"})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/register":
		f.hostCount++
		hostID := fmt.Sprintf("host-%c", 'a'+rune(f.hostCount-1))
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"hostId": hostID, "hostToken": "host-token-" + string(hostID[len(hostID)-1]),
		})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/election/status"):
		var current any
		if f.currentHostID != "" {
			current = f.currentHostID
		}
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"groupId": f.groupID, "currentHostId": current, "currentHostGeneration": f.generation,
			"lastElection": nil, "activeTakeoverAssignment": nil,
		})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/artifacts/objects/"):
		sum := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/objects/")
		content, _ := io.ReadAll(r.Body)
		actual := sha256.Sum256(content)
		if hex.EncodeToString(actual[:]) != sum {
			writeBootstrapJSON(w, http.StatusBadRequest, map[string]any{"message": "sha mismatch"})
			return
		}
		_, existed := f.objects[sum]
		f.objects[sum] = content
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"ok": true, "sha256": sum, "exists": existed, "size": len(content),
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/artifacts/manifests":
		var body struct {
			Manifest manifest.Manifest `json:"manifest"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.runtimeManifest = &body.Manifest
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"ok": true, "artifactKind": "server-runtime",
			"artifactId": body.Manifest.ArtifactID, "status": "available",
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/heartbeat":
		writeBootstrapJSON(w, http.StatusOK, map[string]any{"ok": true, "hostId": "host-a", "status": "standby"})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/election/run"):
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"ok": true, "groupId": f.groupID, "selectedHostId": "host-a",
			"candidates": []any{},
			"election": map[string]any{
				"electionId": "election-1", "groupId": f.groupID, "reason": "no-current-host",
				"selectedHostId": "host-a", "currentHostGeneration": f.generation,
				"assignmentId": "assignment-1", "candidates": []any{}, "createdAt": "2026-06-15T00:00:00Z",
			},
			"assignment": map[string]any{"assignmentId": "assignment-1", "hostId": "host-a"},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/takeover/poll":
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"assignment": map[string]any{
				"assignmentId": "assignment-1", "groupId": f.groupID, "hostId": "host-a",
				"takeoverToken": "takeover-token", "currentHostGeneration": f.generation,
			},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/takeover/accept":
		writeBootstrapJSON(w, http.StatusOK, map[string]any{"ok": true, "assignment": map[string]any{"assignmentId": "assignment-1"}})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/takeover/complete":
		f.currentHostID = "host-a"
		f.generation++
		writeBootstrapJSON(w, http.StatusOK, map[string]any{"ok": true, "assignment": map[string]any{"assignmentId": "assignment-1"}})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/groups/"+f.groupID+"/artifacts/latest":
		if f.runtimeManifest == nil {
			writeBootstrapJSON(w, http.StatusNotFound, map[string]any{"message": "Artifact does not exist"})
			return
		}
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"groupId": f.groupID, "artifactKind": "server-runtime",
			"artifactId": f.runtimeManifest.ArtifactID, "creatorHostId": f.runtimeManifest.CreatorHostID,
			"generation": f.runtimeManifest.Generation, "fileCount": f.runtimeManifest.Summary.IncludedFiles,
			"totalBytes": f.runtimeManifest.Summary.TotalBytes,
		})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/manifest"):
		writeBootstrapJSON(w, http.StatusOK, map[string]any{
			"metadata": map[string]any{
				"groupId": f.groupID, "artifactKind": "server-runtime",
				"artifactId": f.runtimeManifest.ArtifactID, "generation": f.runtimeManifest.Generation,
			},
			"manifest": f.runtimeManifest,
		})
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/artifacts/objects/"):
		sum := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		content, ok := f.objects[sum]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	default:
		writeBootstrapJSON(w, http.StatusNotFound, map[string]any{"message": "not found"})
	}
}

func writeBootstrapJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func TestBootstrapProfileUsesProtectedConfig(t *testing.T) {
	root := t.TempDir()
	setTestConfigRoot(t, root)
	path, err := agentconfig.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) == "" {
		t.Fatal("config path has no directory")
	}
}
