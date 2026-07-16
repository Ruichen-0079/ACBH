package localapi

import (
	"bytes"
	"context"
	"encoding/json"
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
func (f *fakeRuntime) Import(string) (hobbyagent.PreflightResult, error) {
	return hobbyagent.PreflightResult{}, nil
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
	for _, required := range []string{"开始托管", "公网服务器在线", "停止托管", "设置", "诊断", "本地 MC 端口", "VPS 公网端口", "local-endpoint"} {
		if !strings.Contains(page, required) {
			t.Fatalf("UI is missing %q", required)
		}
	}
	for _, forbidden := range []string{"session_id", "heartbeat", "epoch", "capability", "PID"} {
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
}
