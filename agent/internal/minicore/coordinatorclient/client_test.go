package coordinatorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

func TestProbeUsesConfiguredCoordinatorURLAndClassifiesRoutes(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "protocolVersion": 2})
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"protocolVersion":    2,
				"capabilities":       []string{"lease_renew_v1", "world_backup_v1", "group_whoami_v1"},
				"authenticationMode": "host_token_or_owner_access_key",
			})
		case "/v1/groups/grp_test/lease/ensure-active":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_body","message":"invalid body"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"host_auth_required","message":"host auth required"}`))
		}
	}))
	defer srv.Close()

	client, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, probeErr := client.Probe(context.Background())
	if probeErr != nil {
		t.Fatalf("Probe() error = %v", probeErr)
	}
	if result.CoordinatorURL != srv.URL {
		t.Fatalf("Probe() coordinatorUrl = %q, want %q", result.CoordinatorURL, srv.URL)
	}
	if result.ActualRequestURL != srv.URL+"/health" {
		t.Fatalf("Probe() actualRequestUrl = %q, want %q", result.ActualRequestURL, srv.URL+"/health")
	}
	if result.CapabilityRouteMissing {
		t.Fatal("CapabilityRouteMissing = true, want false for 401/400 probes")
	}
	for _, path := range requested {
		if strings.Contains(path, "127.0.0.1") || strings.Contains(path, "localhost") {
			t.Fatalf("request path unexpectedly used localhost: %s", path)
		}
	}
}

func TestResponseClassifiesAuthInvalidAndRouteMissing(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   coreerrors.ErrorCode
	}{
		{"401 auth missing", http.StatusUnauthorized, `{"code":"host_auth_required"}`, coreerrors.AuthMissing},
		{"400 invalid request", http.StatusBadRequest, `{"code":"invalid_body"}`, coreerrors.InvalidRequest},
		{"403 lease expired", http.StatusForbidden, `{"code":"host_lease_expired"}`, coreerrors.LeaseExpired},
		{"403 not current host", http.StatusForbidden, `{"code":"not_current_host"}`, coreerrors.NotCurrentHost},
		{"404 route missing", http.StatusNotFound, `{"code":"route_not_found"}`, coreerrors.CoordinatorRouteMissing},
		{"502 proxy", http.StatusBadGateway, `Proxy-Connection: keep-alive`, coreerrors.ProxyInterferenceSuspected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := responseError(http.MethodGet, "http://example.test/x", tc.status, tc.body)
			if err.ErrorCode != tc.want {
				t.Fatalf("errorCode = %s, want %s", err.ErrorCode, tc.want)
			}
			if err.Details.HTTPStatus != tc.status || err.Details.ResponseBody != tc.body {
				t.Fatalf("details not preserved: %#v", err.Details)
			}
		})
	}
}

func TestProbeMarksOnly404RouteMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"protocolVersion": 2,
				"capabilities":    []string{"lease_renew_v1", "world_backup_v1", "group_whoami_v1"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"route_not_found"}`))
		}
	}))
	defer srv.Close()
	client, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, probeErr := client.Probe(context.Background())
	if probeErr != nil {
		t.Fatalf("Probe() error = %v", probeErr)
	}
	if !result.CapabilityRouteMissing {
		t.Fatal("CapabilityRouteMissing = false, want true for 404 route probes")
	}
}
