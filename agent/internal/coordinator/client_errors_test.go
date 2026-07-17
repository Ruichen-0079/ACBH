package coordinator

import (
	"errors"
	"net/http"
	"testing"
)

func TestResponseErrorMaps404ToRouteNotFound(t *testing.T) {
	err := responseError(http.StatusNotFound, []byte(`{"error":"Not Found","message":"Route POST:/v1/groups/x/lease/ensure-active not found"}`))
	if !IsRouteNotFound(err) {
		t.Fatalf("IsRouteNotFound() = false, want true for %#v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "route_not_found" {
		t.Fatalf("apiErr = %#v, want route_not_found", err)
	}
}

func TestIsUnsupportedCapabilityHelpers(t *testing.T) {
	err := &APIError{Code: "unsupported_capability", Message: "missing"}
	if !IsUnsupportedCapability(err) {
		t.Fatal("expected unsupported capability helper to match")
	}
	err = &APIError{Code: "coordinator_version_mismatch", Message: "old"}
	if !IsCoordinatorVersionMismatch(err) {
		t.Fatal("expected coordinator version mismatch helper to match")
	}
	err = &APIError{Code: "coordinator_capability_route_missing", Message: "missing route"}
	if !IsCoordinatorCapabilityRouteMissing(err) {
		t.Fatal("expected capability route missing helper to match")
	}
}