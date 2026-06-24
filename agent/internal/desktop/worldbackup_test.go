package desktop

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

func TestWorldBackupStatusResultJSONShape(t *testing.T) {
	appData := t.TempDir()
	index := worldbackup.Index{
		Version:          worldbackup.IndexVersion,
		LatestSnapshotID: "ws_test_001",
		Files: map[string]worldbackup.IndexedFile{
			"world/level.dat": {Size: 1024, SHA256: strings.Repeat("a", 64)},
		},
	}
	if err := worldbackup.SaveIndexAtomic(appData, index); err != nil {
		t.Fatalf("SaveIndexAtomic() error = %v", err)
	}
	if err := agentconfig.Save(filepath.Join(appData, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:65535",
		GroupID:        "grp_test",
		MemberID:       "mem_test",
		HostID:         "host_test",
		HostToken:      "test-host-token",
		DisplayName:    "owner",
		DeviceName:     "pc",
		Platform:       runtime.GOOS,
		AgentVersion:   agentconfig.AgentVersion,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	result, err := WorldBackupStatus(context.Background(), Options{AppDataDir: appData})
	if err != nil {
		t.Fatalf("WorldBackupStatus() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("WorldBackupStatus().OK = false, want true")
	}
	if !result.IndexExists {
		t.Fatalf("IndexExists = false, want true")
	}
	if result.LocalLatestSnapshotID != "ws_test_001" {
		t.Fatalf("LocalLatestSnapshotID = %q, want ws_test_001", result.LocalLatestSnapshotID)
	}
	if result.LocalFileCount != 1 {
		t.Fatalf("LocalFileCount = %d, want 1", result.LocalFileCount)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, key := range []string{"ok", "indexPath", "indexExists", "localLatestSnapshotId", "localFileCount", "historyCount"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("JSON missing key %q: %s", key, string(data))
		}
	}
}