package coordinator

import (
	"encoding/json"
	"testing"
)

func TestHeartbeatRequestPayload(t *testing.T) {
	req := HeartbeatRequest{
		GroupID:               "grp_123",
		HostID:                "host_123",
		HostToken:             "secret",
		Status:                "standby",
		LatestLocalSnapshotID: nil,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"groupId":"grp_123","hostId":"host_123","hostToken":"secret","status":"standby","latestLocalSnapshotId":null}`
	if string(data) != want {
		t.Fatalf("payload = %s, want %s", string(data), want)
	}
}

func TestHeartbeatRequestIncludesElectionHints(t *testing.T) {
	javaAvailable := false
	req := HeartbeatRequest{
		GroupID:   "grp_123",
		HostID:    "host_123",
		HostToken: "secret",
		Status:    "standby",
		LatestLocalArtifacts: map[string]string{
			"world-snapshot": "snap_001",
			"server-pack":    "pack_001",
			"admin-state":    "admin_001",
		},
		HostScoreHints: &HostScoreHints{
			CPUCores:      8,
			JavaAvailable: &javaAvailable,
		},
		Connection: &HostConnection{
			Host:    "100.64.0.10",
			Port:    25565,
			Network: "tailscale",
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	artifacts := payload["latestLocalArtifacts"].(map[string]any)
	if artifacts["world-snapshot"] != "snap_001" || artifacts["server-pack"] != "pack_001" {
		t.Fatalf("latestLocalArtifacts = %#v", artifacts)
	}
	hints := payload["hostScoreHints"].(map[string]any)
	if hints["javaAvailable"] != false || hints["cpuCores"] != float64(8) {
		t.Fatalf("hostScoreHints = %#v", hints)
	}
	connection := payload["connection"].(map[string]any)
	if connection["host"] != "100.64.0.10" || connection["port"] != float64(25565) {
		t.Fatalf("connection = %#v", connection)
	}
}

func TestValidStatus(t *testing.T) {
	for _, status := range []string{"online", "standby", "hosting", "unhealthy", "offline"} {
		if !ValidStatus(status) {
			t.Fatalf("ValidStatus(%q) = false", status)
		}
	}
	if ValidStatus("starting") {
		t.Fatal("ValidStatus(\"starting\") = true")
	}
}
