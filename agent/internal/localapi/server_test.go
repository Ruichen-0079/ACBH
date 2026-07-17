package localapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
	"github.com/Ruichen-0079/ACBH/agent/internal/hobbyagent"
)

type fakeRuntime struct {
	config      hobbyagent.Config
	updateCount int
	start       hobbyagent.Operation
	stop        hobbyagent.Operation
	updateErr   error
}

func (f *fakeRuntime) Config() (hobbyagent.PublicConfig, error) { return f.config.Public(), nil }
func (f *fakeRuntime) UpdateConfig(config hobbyagent.Config) (hobbyagent.PublicConfig, error) {
	f.updateCount++
	if f.updateErr != nil {
		return hobbyagent.PublicConfig{}, f.updateErr
	}
	f.config = config
	if err := config.Validate(); err != nil {
		return hobbyagent.PublicConfig{}, err
	}
	return config.Public(), nil
}
func (f *fakeRuntime) Start() hobbyagent.Operation { return f.start }
func (f *fakeRuntime) Stop() hobbyagent.Operation  { return f.stop }
func (f *fakeRuntime) Operation(id string) (hobbyagent.Operation, bool) {
	if id == f.start.ID {
		return f.start, true
	}
	return hobbyagent.Operation{}, false
}
func (f *fakeRuntime) Status() hobbyagent.RuntimeStatus  { return hobbyagent.RuntimeStatus{} }
func (f *fakeRuntime) Events(int) []componentstate.Event { return nil }
func (f *fakeRuntime) Logs(int) []string                 { return nil }
func (f *fakeRuntime) Diagnostics(context.Context) map[string]any {
	return map[string]any{"ok": true}
}
func (f *fakeRuntime) LogDirectory() string { return "" }

func TestConfigEndpointRejectsRuntimeStateFields(t *testing.T) {
	for _, field := range []string{
		"session_id", "relay_state", "heartbeat", "public_endpoint", "current_host",
		"lease", "tunnel_state", "current_step", "PID", "process_state", "reconnect_count",
	} {
		t.Run(field, func(t *testing.T) {
			runtime := &fakeRuntime{}
			body := []byte(`{"coordinator_host":"vps.example.test","coordinator_port":6121,"access_token":"secret","` + field + `":"forbidden"}`)
			request := httptest.NewRequest(http.MethodPut, "/local/v1/config", bytes.NewReader(body))
			response := httptest.NewRecorder()
			New(runtime).Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d: %s", field, response.Code, response.Body.String())
			}
			if runtime.updateCount != 0 {
				t.Fatalf("runtime state field %s reached config storage", field)
			}
		})
	}
}

func TestConfigResponseNeverReturnsAccessToken(t *testing.T) {
	runtime := &fakeRuntime{}
	body := []byte(`{"coordinator_host":"vps.example.test","coordinator_port":6121,"access_token":"top-secret","minecraft_local_port":25566,"public_minecraft_port":25575}`)
	request := httptest.NewRequest(http.MethodPut, "/local/v1/config", bytes.NewReader(body))
	response := httptest.NewRecorder()
	New(runtime).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("top-secret")) || bytes.Contains(response.Body.Bytes(), []byte(`"access_token":`)) {
		t.Fatalf("access token leaked in response: %s", response.Body.String())
	}
	var public hobbyagent.PublicConfig
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if !public.AccessTokenConfigured {
		t.Fatal("response did not indicate that a token is configured")
	}
}

func TestConfigEndpointRejectsNonIntegerPorts(t *testing.T) {
	for name, value := range map[string]string{"string": `"25566"`, "fraction": `25566.5`, "negative": `-25566`} {
		t.Run(name, func(t *testing.T) {
			body := `{"coordinator_host":"vps","coordinator_port":6121,"access_token":"secret","minecraft_local_port":` + value + `,"public_minecraft_port":25575}`
			response := httptest.NewRecorder()
			New(&fakeRuntime{}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/local/v1/config", strings.NewReader(body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestConfigLockedWhileRunningUsesStableError(t *testing.T) {
	runtime := &fakeRuntime{updateErr: &hobbyagent.CodedError{Code: hobbyagent.CodeConfigLockedWhileRunning, Message: "请先停止托管，再修改端口。"}}
	body := `{"coordinator_host":"vps","coordinator_port":6121,"access_token":"secret","minecraft_local_port":25566,"public_minecraft_port":25575}`
	response := httptest.NewRecorder()
	New(runtime).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/local/v1/config", strings.NewReader(body)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), hobbyagent.CodeConfigLockedWhileRunning) {
		t.Fatalf("unexpected locked response: %d %s", response.Code, response.Body.String())
	}
}

func TestStartIsAsynchronous(t *testing.T) {
	runtime := &fakeRuntime{start: hobbyagent.Operation{ID: "op-1", Kind: "start", Status: "RUNNING", StartedAt: time.Now()}}
	request := httptest.NewRequest(http.MethodPost, "/local/v1/start", nil)
	response := httptest.NewRecorder()
	New(runtime).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !bytes.Contains(response.Body.Bytes(), []byte("op-1")) {
		t.Fatalf("unexpected async start response: %d %s", response.Code, response.Body.String())
	}
}

func TestListenRejectsNonLoopbackAddress(t *testing.T) {
	err := ListenAndServe(context.Background(), "0.0.0.0:6130", http.NewServeMux())
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("expected loopback binding error, got %v", err)
	}
}

func TestLocalAPIMutationsRejectCrossOriginRequests(t *testing.T) {
	runtime := &fakeRuntime{}
	body := `{"coordinator_host":"vps","coordinator_port":6121,"access_token":"secret","minecraft_local_port":25566,"public_minecraft_port":25566}`
	request := httptest.NewRequest(http.MethodPut, "/local/v1/config", strings.NewReader(body))
	request.Host = "127.0.0.1:6130"
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	New(runtime).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || runtime.updateCount != 0 {
		t.Fatalf("cross-origin mutation reached runtime: %d %s", response.Code, response.Body.String())
	}
}

func TestLocalAPIRejectsDNSRebindingHost(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://attacker.example:6130/local/v1/status", nil)
	response := httptest.NewRecorder()
	loopbackHostOnly(New(&fakeRuntime{}).Handler()).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-loopback Host was accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestEmbeddedUIIsMinimalAndAutomaticallyRefreshes(t *testing.T) {
	runtime := &fakeRuntime{}
	handler := New(runtime).Handler()
	pageRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	pageResponse := httptest.NewRecorder()
	handler.ServeHTTP(pageResponse, pageRequest)
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("expected UI 200, got %d", pageResponse.Code)
	}
	page := pageResponse.Body.String()
	for _, required := range []string{"保存并启动", "停止中转", "复制公网地址", "公网 IP / 域名", "Access Token", "自定义端口", "查看详情", "打开日志目录", "导出脱敏诊断包"} {
		if !strings.Contains(page, required) {
			t.Fatalf("UI is missing %q", required)
		}
	}
	for _, forbidden := range []string{"session_id", "heartbeat", "epoch", "capability", "PID", "服务端目录", "server.jar", "Java/JVM", "侧边栏"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("main UI exposes internal field %q", forbidden)
		}
	}
	if pageResponse.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("UI response is missing Content-Security-Policy")
	}

	scriptRequest := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	scriptResponse := httptest.NewRecorder()
	handler.ServeHTTP(scriptResponse, scriptRequest)
	if !strings.Contains(scriptResponse.Body.String(), "setInterval(refresh, 2000)") {
		t.Fatal("UI does not automatically refresh status")
	}
	script := scriptResponse.Body.String()
	if !strings.Contains(script, "minecraft_local_port: port") || !strings.Contains(script, "public_minecraft_port: port") {
		t.Fatal("single custom port is not mapped to both local and public ports")
	}
}

func TestLegacyHobbyImportRouteIsRemoved(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/local/v1/import", strings.NewReader(`{"server_dir":"C:\\server"}`))
	New(&fakeRuntime{}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy import route still reachable: %d %s", response.Code, response.Body.String())
	}
}

func TestDiagnosticExportIsBoundedAndContainsOnlySafeFiles(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/local/v1/diagnostics/export", nil)
	New(&fakeRuntime{}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("unexpected diagnostic export: %d %s", response.Code, response.Header().Get("Content-Type"))
	}
	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"diagnostics.json": false, "recent-logs.json": false, "state-transitions.json": false}
	for _, file := range reader.File {
		if _, ok := want[file.Name]; !ok {
			t.Fatalf("unexpected diagnostic file %q", file.Name)
		}
		want[file.Name] = true
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(io.LimitReader(stream, 1024*1024))
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"access_token", "Bearer ", "private key", "server_dir", "current_host", "tunnel"} {
			if bytes.Contains(bytes.ToLower(content), bytes.ToLower([]byte(forbidden))) {
				t.Fatalf("diagnostic file %s contains %q: %s", file.Name, forbidden, content)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("diagnostic export missing %s", name)
		}
	}
}
