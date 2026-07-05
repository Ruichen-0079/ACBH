package body

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/listener"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/operations"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

type bodyFakeInspector struct {
	listeners []listener.Listener
}

func (f bodyFakeInspector) TCPListeners(ctx context.Context, host string, port int) ([]listener.Listener, error) {
	return f.listeners, nil
}

func (f bodyFakeInspector) ProcessInfo(ctx context.Context, pid int) (listener.ProcessInfo, error) {
	return listener.ProcessInfo{PID: pid, ProcessName: "java.exe", CommandLine: `java -jar C:\server\server.jar`}, nil
}

type rewriteTransport struct {
	fromHost string
	toURL    *url.URL
	base     http.RoundTripper
	requests *[]string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.requests != nil {
		*t.requests = append(*t.requests, clone.URL.String())
	}
	if clone.URL.Host == t.fromHost {
		nextURL := *clone.URL
		nextURL.Scheme = t.toURL.Scheme
		nextURL.Host = t.toURL.Host
		clone.URL = &nextURL
	}
	return t.base.RoundTrip(clone)
}

func testConfig(coordinatorURL string) coreconfig.Config {
	cfg := coreconfig.DefaultConfig()
	cfg.Mode = "local-private"
	cfg.CoordinatorURL = coordinatorURL
	cfg.Identity = coreconfig.Identity{
		GroupID: "grp_123", MemberID: "mem_123", HostID: "host_123", HostToken: "ht_123",
		DisplayName: "Host", DeviceName: "PC", Platform: "windows",
	}
	cfg.Relay.PublicHost = "127.0.0.1"
	return cfg
}

func mockCoordinator(t *testing.T) *httptest.Server {
	t.Helper()
	objectData := []byte("[]")
	sum := sha256.Sum256(objectData)
	objectSHA := hex.EncodeToString(sum[:])
	manifest := worldbackup.Manifest{
		SchemaVersion:  worldbackup.SchemaVersion,
		SnapshotID:     "ws_20260704_120000",
		GroupID:        "grp_123",
		SourceHostID:   "host_123",
		HostGeneration: 1,
		CreatedAt:      mustRFC3339(t, "2026-07-04T12:00:00Z"),
		Consistent:     true,
		LogicalSize:    int64(len(objectData)),
		FileCount:      1,
		Files:          []worldbackup.FileEntry{{Path: "banned-ips.json", Size: int64(len(objectData)), SHA256: objectSHA, ObjectID: worldbackup.ObjectID(objectSHA)}},
	}
	lease := map[string]any{
		"groupId":              "grp_123",
		"hostId":               "host_123",
		"currentHostId":        "host_123",
		"currentHostIdMatches": true,
		"leaseValid":           true,
		"leaseRemaining":       60,
		"generation":           1,
		"serverTime":           "2026-07-04T00:00:00Z",
		"heartbeatActive":      true,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/groups/grp_123/world-objects/") {
			sha := strings.TrimPrefix(r.URL.Path, "/v1/groups/grp_123/world-objects/")
			if r.Method == http.MethodGet && sha == objectSHA {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(objectData)
				return
			}
			if r.Method == http.MethodPut {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "sha256": sha, "size": r.ContentLength})
				return
			}
		}
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "protocolVersion": 2})
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"coordinatorVersion": "0.4.0-alpha6-hotfix2",
				"protocolVersion":    2,
				"capabilities":       []string{"lease_renew_v1", "world_backup_v1", "group_whoami_v1"},
			})
		case "/v1/groups/grp_123/whoami":
			_ = json.NewEncoder(w).Encode(map[string]any{"groupId": "grp_123", "hostId": "host_123"})
		case "/v1/groups/grp_123/lease/status":
			_ = json.NewEncoder(w).Encode(lease)
		case "/v1/groups/grp_123/lease/ensure-active":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"renewed": true,
				"lease":   lease,
				"message": "Host lease is active",
			})
		case "/v1/hosts/heartbeat":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "hostId": "host_123", "status": "hosting"})
		case "/v1/groups/grp_123/world-backups/plan":
			var req struct {
				Objects []worldbackup.PlannedObject `json:"objects"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "missingObjects": req.Objects, "existingCount": 0})
		case "/v1/groups/grp_123/world-backups/commit":
			var req struct {
				Manifest worldbackup.Manifest `json:"manifest"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "snapshotId": req.Manifest.SnapshotID, "status": "completed"})
		case "/v1/groups/grp_123/world-backups":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshots": []map[string]any{{
				"snapshotId": "ws_20260704_120000", "status": "completed", "createdAt": "2026-07-04T12:00:00Z", "logicalSize": len(objectData), "fileCount": 1, "rootCount": 1, "canRestore": true, "canDownload": true,
			}}})
		case "/v1/groups/grp_123/world-backups/latest", "/v1/groups/grp_123/world-backups/ws_20260704_120000":
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"snapshotId": "ws_20260704_120000"}, "manifest": manifest})
		case "/v1/groups/grp_test/lease/ensure-active":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_body"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"host_auth_required"}`))
		}
	}))
}

func mustRFC3339(t *testing.T, raw string) time.Time {
	t.Helper()
	out, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func waitOperation(t *testing.T, srv *Server, operationID string) operations.Operation {
	t.Helper()
	var op operations.Operation
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		var ok bool
		op, ok = srv.Operations.Get(operationID)
		if ok && op.State != operations.Running {
			return op
		}
		time.Sleep(10 * time.Millisecond)
	}
	op, _ = srv.Operations.Get(operationID)
	return op
}

func TestBodyHealthAndConfig(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	if err := srv.ConfigStore.Save(testConfig(coord.URL)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/body/health", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	var health HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.CoordinatorURL != coord.URL || health.ConfigPath == "" || health.Service != ServiceName || health.IdentityModel != "single-owner" || health.InstanceID == "" || health.DeviceID == "" || health.ServerID == "" {
		t.Fatalf("health = %#v", health)
	}
}

func TestBodyConfigPutGet(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	cfg := testConfig(coord.URL)
	data, _ := json.Marshal(cfg)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/config", bytes.NewReader(data)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /v1/config status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/config status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got coreconfig.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 2 || got.Instance.OwnerToken != "[redacted]" || got.Compat.LegacyHostToken != "[redacted]" {
		t.Fatalf("GET /v1/config = %#v, want schema v2 with redacted tokens", got)
	}
}

func TestBodyProbeAndInit(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	if err := srv.ConfigStore.Save(testConfig(coord.URL)); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/coordinator/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", rec.Code, rec.Body.String())
	}
	var op operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.State != operations.Success {
		t.Fatalf("probe op = %#v", op)
	}

	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/init", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("init status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.State != operations.Success {
		t.Fatalf("init op = %#v", op)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"memberId", "hostToken", "lease"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("init response contains user-visible legacy wording %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"actualRequestUrl"`) || !strings.Contains(body, `/whoami`) {
		t.Fatalf("init response missing coordinator network diagnostics: %s", body)
	}
	if !strings.Contains(body, "私人实例已连接") {
		t.Fatalf("init response missing single-owner wording: %s", body)
	}
}

func TestBodyProbeUsesConfigJsonNotLegacyCoordinatorState(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	targetURL, err := url.Parse(coord.URL)
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	legacyState := []byte(`{"coordinatorUrl":"http://127.0.0.1:6121"}`)
	if err := os.WriteFile(filepath.Join(appData, "coordinator-state.json"), legacyState, 0o600); err != nil {
		t.Fatal(err)
	}
	if localListener, err := net.Listen("tcp", "127.0.0.1:6121"); err == nil {
		localServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"code":"wrong_localhost"}`))
		})}
		go func() { _ = localServer.Serve(localListener) }()
		defer localServer.Close()
	} else {
		t.Logf("127.0.0.1:6121 interference listener skipped: %v", err)
	}

	srv := New("127.0.0.1:6120", appData)
	srv.HTTPClient = &http.Client{Transport: rewriteTransport{
		fromHost: "public.test:6121",
		toURL:    targetURL,
		base:     http.DefaultTransport,
		requests: nil,
	}}
	cfg := testConfig("http://public.test:6121")
	cfg.Mode = "remote-public"
	if err := srv.ConfigStore.Save(cfg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/coordinator/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("probe status = %d body=%s", rec.Code, rec.Body.String())
	}
	var op operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(op.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		CoordinatorURL   string `json:"coordinatorUrl"`
		ActualRequestURL string `json:"actualRequestUrl"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.CoordinatorURL != "http://public.test:6121" {
		t.Fatalf("coordinatorUrl = %q, want public URL", result.CoordinatorURL)
	}
	if result.ActualRequestURL != "http://public.test:6121/health" {
		t.Fatalf("actualRequestUrl = %q, want public health URL", result.ActualRequestURL)
	}
}

func TestBodySmokeAPISet(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	cfg := testConfig(coord.URL)
	data, _ := json.Marshal(cfg)

	cases := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/v1/body/health", nil},
		{http.MethodPut, "/v1/config", data},
		{http.MethodGet, "/v1/config", nil},
		{http.MethodGet, "/v1/identity", nil},
		{http.MethodGet, "/v1/coordinator/probe", nil},
		{http.MethodPost, "/v1/init", nil},
		{http.MethodGet, "/v1/operations", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.routes().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body)))
			if rec.Code < 200 || rec.Code >= 300 {
				t.Fatalf("%s %s status = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBodyIdentityAPI(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	if err := srv.ConfigStore.Save(testConfig(coord.URL)); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/identity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("identity status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got IdentityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.IdentityModel != "single-owner" || got.Instance.InstanceID == "" || !got.Compat.UsesLegacyGroupAPI || !got.Compat.LegacyGroupIDPresent || !got.Compat.LegacyHostTokenPresent {
		t.Fatalf("identity = %#v", got)
	}
	if strings.Contains(rec.Body.String(), "ht_123") {
		t.Fatalf("identity response leaked token: %s", rec.Body.String())
	}
}

func TestBodyListenerAPIs(t *testing.T) {
	srv := New("127.0.0.1:6120", t.TempDir())
	srv.Listener = listener.Service{Inspector: bodyFakeInspector{listeners: []listener.Listener{{LocalAddress: "127.0.0.1", LocalPort: 25565, PID: 123}}}}
	if err := srv.ConfigStore.Save(testConfig("http://127.0.0.1:6121")); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/listener/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("listener/status status = %d body=%s", rec.Code, rec.Body.String())
	}
	var status listener.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Listening || status.Listeners[0].ProcessName != "java.exe" {
		t.Fatalf("listener status = %#v", status)
	}

	cfg := coreconfig.ListenerConfig{Enabled: true, LocalHost: "127.0.0.1", LocalPort: 25566}
	data, _ := json.Marshal(cfg)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/listener/config", bytes.NewReader(data)))
	if rec.Code != http.StatusOK {
		t.Fatalf("listener/config status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/listener/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("listener/probe status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBodyRelayAPIs(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	if err := srv.ConfigStore.Save(testConfig(coord.URL)); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/relay/configure", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("relay/configure status = %d body=%s", rec.Code, rec.Body.String())
	}
	var op operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.State != operations.Success {
		t.Fatalf("relay configure op = %#v", op)
	}

	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/relay/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("relay/status status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBodyBackupSnapshotAPIs(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	serverDir := t.TempDir()
	for name, content := range map[string]string{
		"world/level.dat": "world",
		"banned-ips.json": "[]",
	} {
		path := filepath.Join(serverDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	srv := New("127.0.0.1:6120", t.TempDir())
	cfg := testConfig(coord.URL)
	cfg.Server.Dir = serverDir
	if err := srv.ConfigStore.Save(cfg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/backup/analyze", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("backup/analyze status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "banned-ips.json") {
		t.Fatalf("analyze did not include top-level file: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/backup/upload", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("backup/upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	var op operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.State != operations.Running {
		t.Fatalf("backup upload initial op = %#v", op)
	}
	op = waitOperation(t, srv, op.OperationID)
	if op.State != operations.Success {
		t.Fatalf("backup upload op = %#v", op)
	}
	uploadJSON, _ := json.Marshal(op)
	if op.Progress != 100 || op.FileCount == 0 || op.RootCount == 0 || op.LogicalSize == 0 || op.SnapshotID == "" {
		t.Fatalf("backup upload progress fields missing: %#v", op)
	}
	if !strings.Contains(string(uploadJSON), `"networkRequests"`) || !strings.Contains(string(uploadJSON), "/world-backups/plan") || !strings.Contains(string(uploadJSON), "/world-backups/commit") {
		t.Fatalf("backup upload missing coordinator network diagnostics: %s", string(uploadJSON))
	}

	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/snapshots", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshots status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hostToken") || strings.Contains(rec.Body.String(), "groupId") {
		t.Fatalf("snapshots leaked legacy fields: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"actualRequestUrl"`) || !strings.Contains(rec.Body.String(), "/world-backups") {
		t.Fatalf("snapshots missing actualRequestUrl diagnostics: %s", rec.Body.String())
	}

	target := filepath.Join(t.TempDir(), "restore")
	body, _ := json.Marshal(map[string]any{"targetDir": target})
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/snapshots/latest/download", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("latest download status = %d body=%s", rec.Code, rec.Body.String())
	}
	var downloadOp operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &downloadOp); err != nil {
		t.Fatal(err)
	}
	downloadOp = waitOperation(t, srv, downloadOp.OperationID)
	if downloadOp.State != operations.Success {
		t.Fatalf("snapshot download op = %#v", downloadOp)
	}
	if got, err := os.ReadFile(filepath.Join(target, "banned-ips.json")); err != nil || string(got) != "[]" {
		t.Fatalf("downloaded top-level file got=%q err=%v", got, err)
	}
	downloadJSON, _ := json.Marshal(downloadOp)
	if !strings.Contains(string(downloadJSON), `"actualRequestUrl"`) || !strings.Contains(string(downloadJSON), "/world-backups/latest") {
		t.Fatalf("download operation missing actualRequestUrl diagnostics: %s", string(downloadJSON))
	}
}

func TestBodyBackupUploadAsyncFailureRecordsError(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	srv := New("127.0.0.1:6120", t.TempDir())
	cfg := testConfig(coord.URL)
	cfg.Server.Dir = filepath.Join(t.TempDir(), "missing-server")
	if err := srv.ConfigStore.Save(cfg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/backup/upload", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("backup/upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	var op operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.State != operations.Running {
		t.Fatalf("backup upload initial op = %#v", op)
	}
	op = waitOperation(t, srv, op.OperationID)
	if op.State != operations.Failed || op.ErrorCode == "" || op.Error == nil {
		t.Fatalf("backup upload failed op diagnostics = %#v", op)
	}
}

func TestBodySnapshotDownloadRejectsNonEmptyTarget(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := New("127.0.0.1:6120", t.TempDir())
	if err := srv.ConfigStore.Save(testConfig(coord.URL)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"targetDir": target})
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/snapshots/ws_20260704_120000/download", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("snapshot download non-empty status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), string(coreerrors.TargetDirNotEmpty)) {
		t.Fatalf("expected target_dir_not_empty: %s", rec.Body.String())
	}
}

func TestBodyRelayDoesNotUseLocalhostFallback(t *testing.T) {
	coord := mockCoordinator(t)
	defer coord.Close()
	targetURL, err := url.Parse(coord.URL)
	if err != nil {
		t.Fatal(err)
	}
	if localListener, err := net.Listen("tcp", "127.0.0.1:6121"); err == nil {
		localServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"code":"wrong_localhost"}`))
		})}
		go func() { _ = localServer.Serve(localListener) }()
		defer localServer.Close()
	} else {
		t.Logf("127.0.0.1:6121 interference listener skipped: %v", err)
	}

	var requests []string
	srv := New("127.0.0.1:6120", t.TempDir())
	srv.HTTPClient = &http.Client{Transport: rewriteTransport{
		fromHost: "public.test:6121",
		toURL:    targetURL,
		base:     http.DefaultTransport,
		requests: &requests,
	}}
	cfg := testConfig("http://public.test:6121")
	cfg.Mode = "remote-public"
	cfg.Relay.PublicHost = "public.test"
	if err := srv.ConfigStore.Save(cfg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/relay/configure", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("relay/configure status = %d body=%s", rec.Code, rec.Body.String())
	}
	var op operations.Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.State != operations.Success {
		t.Fatalf("relay configure op = %#v", op)
	}
	for _, requestURL := range requests {
		if strings.Contains(requestURL, "127.0.0.1:6121") || strings.Contains(requestURL, "localhost:6121") {
			t.Fatalf("relay used localhost fallback: %s", requestURL)
		}
		if !strings.HasPrefix(requestURL, "http://public.test:6121/") {
			t.Fatalf("relay request URL = %q, want public.test coordinator URL", requestURL)
		}
	}

	serverDir := t.TempDir()
	for name, content := range map[string]string{"world/level.dat": "world", "banned-ips.json": "[]"} {
		path := filepath.Join(serverDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Server.Dir = serverDir
	if err := srv.ConfigStore.Save(cfg); err != nil {
		t.Fatal(err)
	}
	requests = nil
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/backup/upload", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("backup/upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	op = waitOperation(t, srv, op.OperationID)
	if op.State != operations.Success {
		t.Fatalf("backup upload op = %#v", op)
	}
	for _, requestURL := range requests {
		if strings.Contains(requestURL, "127.0.0.1:6121") || strings.Contains(requestURL, "localhost:6121") {
			t.Fatalf("backup upload used localhost fallback: %s", requestURL)
		}
		if !strings.HasPrefix(requestURL, "http://public.test:6121/") {
			t.Fatalf("backup upload request URL = %q, want public.test coordinator URL", requestURL)
		}
	}
}
