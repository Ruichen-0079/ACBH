package worldbackup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockRCON struct {
	mu       sync.Mutex
	commands []string
	fail     map[string]error
}

func (m *mockRCON) Execute(_ context.Context, command string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, command)
	if err := m.fail[command]; err != nil {
		return "", err
	}
	return "ok", nil
}

func (m *mockRCON) cmds() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.commands))
	copy(out, m.commands)
	return out
}

func TestOnlineBackupCommandOrder(t *testing.T) {
	server := t.TempDir()
	appData := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=world\n")
	writeWorldFile(t, server, "world/region/r.0.0.mca", "region-data")

	mock := &mockRCON{}
	result, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:     server,
		AppDataDir:    appData,
		TransactionID: "txn_order",
		RCON:          mock,
	})
	if err != nil {
		t.Fatalf("PrepareOnlineConsistentBackup() error = %v", err)
	}
	want := []string{"save-off", "save-all flush", "save-on"}
	got := mock.cmds()
	if len(got) != len(want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commands[%d] = %q, want %q (%#v)", i, got[i], want[i], got)
		}
	}
	if len(result.StagedFiles) != 1 || result.StagedFiles[0].Path != "world/region/r.0.0.mca" {
		t.Fatalf("staged = %#v", result.StagedFiles)
	}
}

func TestOnlineBackupSaveOffFailure(t *testing.T) {
	mock := &mockRCON{fail: map[string]error{"save-off": errors.New("denied")}}
	_, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:  t.TempDir(),
		AppDataDir: t.TempDir(),
		RCON:       mock,
	})
	if err == nil || !strings.Contains(err.Error(), "save-off") {
		t.Fatalf("error = %v", err)
	}
	if got := mock.cmds(); len(got) != 1 || got[0] != "save-off" {
		t.Fatalf("commands = %#v", got)
	}
}

func TestOnlineBackupFlushFailureRunsSaveOn(t *testing.T) {
	mock := &mockRCON{fail: map[string]error{"save-all flush": errors.New("flush failed")}}
	_, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:  t.TempDir(),
		AppDataDir: t.TempDir(),
		RCON:       mock,
	})
	if err == nil || !strings.Contains(err.Error(), "save-all flush") {
		t.Fatalf("error = %v", err)
	}
	if got := mock.cmds(); len(got) != 3 || got[2] != "save-on" {
		t.Fatalf("commands = %#v, want save-on in defer", got)
	}
}

func TestOnlineBackupStagingFailureRunsSaveOn(t *testing.T) {
	server := t.TempDir()
	outside := t.TempDir()
	writeWorldFile(t, outside, "secret.dat", "secret")
	writeWorldFile(t, server, "server.properties", "level-name=world\n")
	if err := os.MkdirAll(filepath.Join(server, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.dat"), filepath.Join(server, "world", "linked.dat")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	mock := &mockRCON{}
	_, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:  server,
		AppDataDir: t.TempDir(),
		RCON:       mock,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v", err)
	}
	if got := mock.cmds(); len(got) != 3 || got[2] != "save-on" {
		t.Fatalf("commands = %#v", got)
	}
}

func TestCopyFileStableRetriesAfterSourceChange(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "world.dat")
	dst := filepath.Join(dir, "staged.dat")
	if err := os.WriteFile(src, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hookCalls int
	beforeCopyHook = func(path string) error {
		if path != src {
			return nil
		}
		hookCalls++
		if hookCalls > 1 {
			return nil
		}
		return os.WriteFile(src, []byte("v2-stable"), 0o644)
	}
	t.Cleanup(func() { beforeCopyHook = nil })
	if err := copyFileStable(src, dst); err != nil {
		t.Fatalf("copyFileStable() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2-stable" {
		t.Fatalf("staged content = %q", got)
	}
}

func TestOnlineBackupCopyContinuousChangeFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "world.dat")
	dst := filepath.Join(dir, "staged.dat")
	if err := os.WriteFile(src, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hookCalls int
	beforeCopyHook = func(path string) error {
		if path != src {
			return nil
		}
		hookCalls++
		// Always write unique content so post-copy stat differs from pre-copy stat.
		return os.WriteFile(src, []byte(fmt.Sprintf("mutated-%d", hookCalls)), 0o644)
	}
	t.Cleanup(func() { beforeCopyHook = nil })
	err := copyFileStable(src, dst)
	if err == nil || !strings.Contains(err.Error(), "changed during staging copy") {
		t.Fatalf("error = %v", err)
	}
}

func TestOnlineBackupSaveOnFailure(t *testing.T) {
	server := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=world\n")
	writeWorldFile(t, server, "world/level.dat", "level")
	mock := &mockRCON{fail: map[string]error{"save-on": errors.New("save-on failed")}}
	_, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:  server,
		AppDataDir: t.TempDir(),
		RCON:       mock,
	})
	if err == nil || !strings.Contains(err.Error(), "save_on_failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestOnlineBackupContextCancelRunsSaveOn(t *testing.T) {
	server := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=world\n")
	for i := 0; i < 200; i++ {
		writeWorldFile(t, server, fmt.Sprintf("world/chunk/%d.dat", i), strings.Repeat("x", 4096))
	}
	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRCON{}
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	_, err := PrepareOnlineConsistentBackup(ctx, OnlineStagingOptions{
		ServerDir:  server,
		AppDataDir: t.TempDir(),
		RCON:       mock,
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if got := mock.cmds(); len(got) < 3 || got[len(got)-1] != "save-on" {
		t.Fatalf("commands = %#v", got)
	}
}

func TestConsistentSnapshotReadsStagingOnly(t *testing.T) {
	server := t.TempDir()
	appData := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=world\n")
	writeWorldFile(t, server, "world/region/r.0.0.mca", "live")

	mock := &mockRCON{}
	session, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:     server,
		AppDataDir:    appData,
		TransactionID: "txn_scan",
		RCON:          mock,
	})
	if err != nil {
		t.Fatalf("PrepareOnlineConsistentBackup() error = %v", err)
	}
	_ = os.WriteFile(filepath.Join(server, "world", "region", "r.0.0.mca"), []byte("mutated-after-save-on"), 0o644)

	snapshot, err := BuildSnapshot(ScanOptions{
		ServerDir:      session.StagingDir,
		AppDataDir:     appData,
		IgnoreRulesDir: server,
		SnapshotID:     "ws_online",
		GroupID:        "grp_123",
		SourceHostID:   "host_123",
		HostGeneration: 1,
		Consistent:     true,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Manifest.Files[0].SHA256 != session.StagedFiles[0].SHA256 {
		t.Fatalf("manifest hash = %s staged hash = %s", snapshot.Manifest.Files[0].SHA256, session.StagedFiles[0].SHA256)
	}
}

func TestOnlineBackupSaveOnCompletedBeforeUploadPhase(t *testing.T) {
	server := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=world\n")
	writeWorldFile(t, server, "world/level.dat", "level")
	mock := &mockRCON{}
	session, err := PrepareOnlineConsistentBackup(context.Background(), OnlineStagingOptions{
		ServerDir:     server,
		AppDataDir:    t.TempDir(),
		TransactionID: "txn_upload",
		RCON:          mock,
	})
	if err != nil {
		t.Fatalf("PrepareOnlineConsistentBackup() error = %v", err)
	}
	if got := mock.cmds(); len(got) != 3 || got[2] != "save-on" {
		t.Fatalf("save-on must complete before upload/hash phase: %#v", got)
	}
	_ = os.WriteFile(filepath.Join(server, "world", "level.dat"), []byte("mutated"), 0o644)
	snapshot, err := BuildSnapshot(ScanOptions{
		ServerDir:      session.StagingDir,
		AppDataDir:     t.TempDir(),
		IgnoreRulesDir: server,
		SnapshotID:     "ws_upload",
		GroupID:        "grp_123",
		SourceHostID:   "host_123",
		HostGeneration: 1,
		Consistent:     true,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if len(mock.cmds()) != 3 {
		t.Fatalf("upload/hash phase must not invoke additional RCON commands: %#v", mock.cmds())
	}
	if snapshot.Manifest.Files[0].SHA256 != session.StagedFiles[0].SHA256 {
		t.Fatalf("snapshot must come from staging, not live world")
	}
}

func TestInconsistentSnapshotRejectedForAutoRestore(t *testing.T) {
	sum := strings.Repeat("a", 64)
	manifest := Manifest{
		SchemaVersion:    1,
		SnapshotID:       "ws_bad",
		GroupID:          "grp_123",
		SourceHostID:     "host_123",
		HostGeneration:   1,
		CreatedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		Consistent:       false,
		LogicalSize:      1,
		UploadedSize:     1,
		FileCount:        1,
		ChangedFileCount: 1,
		Files: []FileEntry{{
			Path: "world/level.dat", Size: 1, SHA256: sum, ObjectID: ObjectID(sum),
		}},
	}
	_, err := Restore(context.Background(), RestoreOptions{
		ServerDir:      t.TempDir(),
		Manifest:       manifest,
		ConsistentOnly: true,
		Downloader: func(context.Context, string) (io.ReadCloser, int64, error) {
			return nil, 0, errors.New("should not download")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "inconsistent") {
		t.Fatalf("Restore() error = %v", err)
	}
}