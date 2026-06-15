package localcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcserver"
)

func TestHealthEndpoint(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr
	if addr == "" {
		t.Fatal("listen address not set")
	}

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)

	if body["ok"] != true {
		t.Error("expected ok=true")
	}
	if body["service"] != "acbh-agent-control" {
		t.Errorf("expected service=acbh-agent-control, got %v", body["service"])
	}
	if _, ok := body["pid"]; !ok {
		t.Error("expected pid in response")
	}
}

func TestProtectedEndpointRequiresToken(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", 401},
		{"wrong prefix", "Token test-token", 401},
		{"wrong token", "Bearer wrong", 401},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(`{"serverDir":"/tmp"}`)
			req, _ := http.NewRequest("POST", "http://"+addr+"/v1/doctor", body)
			req.Header.Set("Content-Type", "application/json")
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("expected %d, got %d", tt.want, resp.StatusCode)
			}
		})
	}
}

func TestServerRefusesEmptyToken(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "", nil)
	err := srv.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("error = %v, want required token failure", err)
	}
}

func TestProtectedEndpointWithValidToken(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "my-secret", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{"serverDir":"/tmp"}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/doctor", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer my-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 with valid token, got %d", resp.StatusCode)
	}
}

func TestCORSHeaders(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	req, _ := http.NewRequest("OPTIONS", "http://"+addr+"/v1/doctor", nil)
	req.Header.Set("Origin", "http://localhost:6121")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		t.Errorf("expected 204 for OPTIONS, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:6121" {
		t.Errorf("expected CORS origin, got %s", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
}

func TestCORSRejectsNonLocalOrigin(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	req, _ := http.NewRequest("OPTIONS", "http://"+srv.ListenAddr+"/v1/doctor", nil)
	req.Header.Set("Origin", "https://attacker.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if origin := resp.Header.Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("unexpected CORS origin %q", origin)
	}
}

func TestRemoteListenRequiresExplicitOptIn(t *testing.T) {
	remote, err := validateListenAddress("0.0.0.0:6122")
	if err != nil {
		t.Fatal(err)
	}
	if !remote {
		t.Fatal("0.0.0.0 must be treated as remote")
	}

	srv := NewServer("0.0.0.0:0", "test-token", nil)
	err = srv.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "--allow-remote-control") {
		t.Fatalf("error = %v, want explicit remote opt-in failure", err)
	}
}

func TestRemoteListenAllowedWithExplicitOptIn(t *testing.T) {
	srv := NewServer("0.0.0.0:0", "test-token", nil)
	srv.AllowRemote = true
	var logs bytes.Buffer
	srv.logger = log.New(&logs, "", 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(logs.String(), "WARNING") {
		t.Fatalf("logs = %q, want remote exposure warning", logs.String())
	}
}

func TestMalformedJSON(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString("not json")
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/scan", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}
}

func TestServerRuntimePullRequiresExplicitNonEmptyPermission(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer("127.0.0.1:0", "test-token", &agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:1",
		GroupID:        "group-test",
		HostID:         "host-test",
		HostToken:      "host-token",
	})

	request := func(allow bool) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{
			"artifactKind":  "server-runtime",
			"artifactId":    "latest",
			"outputDir":     outputDir,
			"allowNonEmpty": allow,
		})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		srv.handlePull(recorder, httptest.NewRequest(http.MethodPost, "/v1/pull", bytes.NewReader(body)))
		return recorder
	}

	blocked := request(false)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("default pull status = %d body=%s", blocked.Code, blocked.Body.String())
	}
	if !strings.Contains(blocked.Body.String(), "restore_target_not_empty") {
		t.Fatalf("default pull body = %s", blocked.Body.String())
	}

	allowed := request(true)
	if allowed.Code == http.StatusConflict {
		t.Fatalf("explicitly allowed pull was still blocked: %s", allowed.Body.String())
	}
}

func TestManifestValidateEndpoint(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	manifestPath := filepath.Join("..", "manifest", "testdata", "valid", "world-snapshot.json")
	body, err := json.Marshal(map[string]string{"path": manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", "http://"+srv.ListenAddr+"/v1/manifest/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true || result["artifactId"] != "snap_valid_001" {
		t.Fatalf("result = %#v", result)
	}
}

func TestManifestValidateErrorDoesNotExposePath(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	secretPath := filepath.Join(t.TempDir(), "private-manifest.json")
	if err := os.WriteFile(secretPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"path": secretPath})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", "http://"+srv.ListenAddr+"/v1/manifest/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(responseBody), secretPath) {
		t.Fatalf("response exposed manifest path: %s", responseBody)
	}
	if !strings.Contains(string(responseBody), "manifest_invalid") {
		t.Fatalf("response = %s, want manifest_invalid", responseBody)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "test-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/doctor", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 405 {
		t.Errorf("expected 405 for GET on POST endpoint, got %d", resp.StatusCode)
	}
}

func TestGenerateToken(t *testing.T) {
	t1 := GenerateToken()
	t2 := GenerateToken()
	if t1 == t2 {
		t.Error("generated tokens should be unique")
	}
	if len(t1) != 32 {
		t.Errorf("expected 32-char hex token, got %d", len(t1))
	}
}

func TestNewServer(t *testing.T) {
	cfg := &agentconfig.Config{HostID: "h1", GroupID: "g1"}
	srv := NewServer("127.0.0.1:6122", "tok", cfg)
	if srv.ListenAddr != "127.0.0.1:6122" {
		t.Errorf("expected 127.0.0.1:6122, got %s", srv.ListenAddr)
	}
	if srv.Token != "tok" {
		t.Errorf("expected token=tok, got %s", srv.Token)
	}
	if srv.Config.HostID != "h1" {
		t.Errorf("expected host ID h1, got %s", srv.Config.HostID)
	}
}

func TestTokenNotInErrors(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "secret-token-value", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/doctor", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	errStr, _ := result["error"].(string)
	if strings.Contains(errStr, "secret-token-value") {
		t.Error("error message must not contain the token value")
	}
}

func TestServerStatusRequiresToken(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{"serverDir":"/tmp"}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/server/status", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}
}

func TestServerStatusValidTokenReturnsSchema(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/server/status", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer srv-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["ok"] != true {
		t.Error("expected ok=true in server status response")
	}
	if _, ok := result["running"]; !ok {
		t.Error("expected running field in server status response")
	}
}

func TestServerStartMalformedJSON(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString("not json")
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/server/start", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer srv-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}
}

func TestServerStartMissingServerDir(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/server/start", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer srv-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for missing serverDir, got %d", resp.StatusCode)
	}
}

func TestServerStartUsesSharedProcessLock(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", configRoot)
	}
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(configDir, "runtime")
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	serverDir := t.TempDir()
	if err := mcserver.CreateLock(mcserver.LockPath(runtimeDir), mcserver.ProcessLock{
		PID: os.Getpid(), Hostname: hostname, CreatedAt: time.Now().UTC(),
		ServerDir: serverDir, Nonce: "local-control-lock", Owner: "supervisor",
	}); err != nil {
		t.Fatal(err)
	}

	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	body, _ := json.Marshal(map[string]any{
		"serverDir": serverDir,
		"javaPath":  "java",
		"jarPath":   "server.jar",
	})
	req, _ := http.NewRequest("POST", "http://"+srv.ListenAddr+"/v1/server/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer srv-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["code"] != "server_state_blocked" {
		t.Fatalf("code = %q, want server_state_blocked", result["code"])
	}
	if result["requestId"] == "" {
		t.Fatal("expected requestId")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), serverDir) {
		t.Fatalf("error response exposed absolute server path: %s", encoded)
	}
}

func TestServerEndpointTokenNotInErrors(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "my-server-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	endpoints := []string{"/v1/server/status", "/v1/server/start", "/v1/server/stop"}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			body := bytes.NewBufferString(`{}`)
			req, _ := http.NewRequest("POST", "http://"+addr+ep, body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer wrong-token")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			var result map[string]any
			json.NewDecoder(resp.Body).Decode(&result)

			errStr, _ := result["error"].(string)
			if strings.Contains(errStr, "my-server-token") {
				t.Error("error message must not contain the token value")
			}
		})
	}
}

func TestServerEndpointMethodNotAllowed(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	endpoints := []string{"/v1/server/status", "/v1/server/start", "/v1/server/stop"}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://"+addr+ep, nil)
			req.Header.Set("Authorization", "Bearer srv-token")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 405 {
				t.Errorf("expected 405 for GET on %s, got %d", ep, resp.StatusCode)
			}
		})
	}
}

func TestServerStartDefaultValues(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{"serverDir":"/tmp/test-dir"}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/server/start", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer srv-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	errStr, _ := result["error"].(string)
	if strings.Contains(errStr, "srv-token") {
		t.Error("error response must not contain the token value")
	}
}

func TestServerStopNotRunning(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/server/stop", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer srv-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for stop when not running, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["stopped"] != false {
		t.Error("expected stopped=false when server is not running")
	}
}

func TestServerCORSHeaders(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "srv-token", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := srv.ListenAddr

	endpoints := []string{"/v1/server/status", "/v1/server/start", "/v1/server/stop"}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req, _ := http.NewRequest("OPTIONS", "http://"+addr+ep, nil)
			req.Header.Set("Origin", "http://localhost:6121")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("OPTIONS failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 204 {
				t.Errorf("expected 204 for OPTIONS on %s, got %d", ep, resp.StatusCode)
			}
			if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:6121" {
				t.Errorf("expected CORS origin on %s", ep)
			}
		})
	}
}
