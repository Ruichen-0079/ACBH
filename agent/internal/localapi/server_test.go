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
}

func (f *fakeRuntime) Config() (hobbyagent.PublicConfig, error) { return f.config.Public(), nil }
func (f *fakeRuntime) UpdateConfig(config hobbyagent.Config) (hobbyagent.PublicConfig, error) {
	f.updateCount++
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
		"lease", "tunnel_state", "current_step",
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
	body := []byte(`{"coordinator_host":"vps.example.test","coordinator_port":6121,"access_token":"top-secret"}`)
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
	if !public.HasAccessToken {
		t.Fatal("response did not indicate that a token is configured")
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
