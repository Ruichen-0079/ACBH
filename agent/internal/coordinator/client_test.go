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
