package body

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/listener"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/operations"
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
		case "/v1/groups/grp_test/lease/ensure-active":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_body"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"host_auth_required"}`))
		}
	}))
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
	if health.CoordinatorURL != coord.URL || health.ConfigPath == "" || health.Service != ServiceName {
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
}
