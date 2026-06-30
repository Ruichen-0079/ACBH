package worldbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildSnapshotFirstAndIncremental(t *testing.T) {
	server := t.TempDir()
	appData := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=survival\n")
	writeWorldFile(t, server, "survival/region/r.0.0.mca", "region-0")
	writeWorldFile(t, server, "survival/playerdata/alice.dat", "alice")
	writeWorldFile(t, server, "mods/mod.jar", "ignored mod")

	first := buildTestSnapshot(t, server, appData, "ws_001", nil)
	if first.Manifest.FileCount != 2 {
		t.Fatalf("fileCount = %d, want 2: %#v", first.Manifest.FileCount, first.Manifest.Files)
	}
	if first.Manifest.ChangedFileCount != 2 || len(first.Plan.Objects) != 2 {
		t.Fatalf("first changed=%d objects=%d", first.Manifest.ChangedFileCount, len(first.Plan.Objects))
	}
	if err := SaveIndexAtomic(appData, first.Index); err != nil {
		t.Fatalf("SaveIndexAtomic() error = %v", err)
	}

	second := buildTestSnapshot(t, server, appData, "ws_002", &first.Manifest)
	if second.Manifest.ChangedFileCount != 0 || len(second.Plan.Objects) != 0 {
		t.Fatalf("unchanged snapshot changed=%d objects=%d", second.Manifest.ChangedFileCount, len(second.Plan.Objects))
	}

	writeWorldFile(t, server, "survival/region/r.0.0.mca", "region-0-modified")
	removeFile(t, server, "survival/playerdata/alice.dat")
	writeWorldFile(t, server, "survival/data/new.dat", "new")
	third := buildTestSnapshot(t, server, appData, "ws_003", &second.Manifest)
	if third.Manifest.ChangedFileCount != 2 {
		t.Fatalf("changedFileCount = %d, want 2", third.Manifest.ChangedFileCount)
	}
	if third.Manifest.DeletedFileCount != 1 || third.Manifest.DeletedPaths[0] != "survival/playerdata/alice.dat" {
		t.Fatalf("deletedPaths = %#v", third.Manifest.DeletedPaths)
	}
}

func TestBuildSnapshotDedupesSameContentObjects(t *testing.T) {
	server := t.TempDir()
	appData := t.TempDir()
	writeWorldFile(t, server, "world/region/a.mca", "same")
	writeWorldFile(t, server, "world/region/b.mca", "same")

	got := buildTestSnapshot(t, server, appData, "ws_001", nil)
	if got.Manifest.FileCount != 2 {
		t.Fatalf("fileCount = %d, want 2", got.Manifest.FileCount)
	}
	if len(got.Plan.Objects) != 1 {
		t.Fatalf("objects = %#v, want 1 deduped object", got.Plan.Objects)
	}
	if got.Manifest.Files[0].ObjectID != got.Manifest.Files[1].ObjectID {
		t.Fatalf("object IDs differ: %#v", got.Manifest.Files)
	}
}

func TestWorldRootsAndIgnoreRules(t *testing.T) {
	server := t.TempDir()
	appData := t.TempDir()
	writeWorldFile(t, server, "server.properties", "level-name=main\n")
	writeWorldFile(t, server, ".acbh-worldignore", "main/data/ignored.dat\n*.bak\n")
	writeWorldFile(t, server, "main/session.lock", "lock")
	writeWorldFile(t, server, "main/data/ignored.dat", "ignored")
	writeWorldFile(t, server, "main/data/ignored.bak", "ignored")
	writeWorldFile(t, server, "main/DIM-1/region/r.0.0.mca", "nether")
	writeWorldFile(t, server, "world_the_end/region/r.0.0.mca", "end")

	got, err := BuildSnapshot(testOptions(server, appData, "ws_001", nil, "world_the_end"))
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	paths := make([]string, 0, len(got.Manifest.Files))
	for _, file := range got.Manifest.Files {
		paths = append(paths, file.Path)
	}
	want := []string{"main/DIM-1/region/r.0.0.mca", "world_the_end/region/r.0.0.mca"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	server := t.TempDir()
	appData := t.TempDir()
	outside := t.TempDir()
	writeWorldFile(t, outside, "secret.dat", "secret")
	if err := os.MkdirAll(filepath.Join(server, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.dat"), filepath.Join(server, "world", "linked.dat")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := BuildSnapshot(testOptions(server, appData, "ws_001", nil))
	if err == nil {
		t.Fatalf("BuildSnapshot() should reject symlink")
	}
}

func TestRestoreStagesAndAppliesWorld(t *testing.T) {
	server := t.TempDir()
	writeWorldFile(t, server, "world/level.dat", "old")
	content := []byte("new")
	sum := sha(content)
	manifest := Manifest{
		SchemaVersion:    1,
		SnapshotID:       "ws_restore",
		GroupID:          "grp_123",
		SourceHostID:     "host_123",
		HostGeneration:   1,
		CreatedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		Consistent:       true,
		LogicalSize:      int64(len(content)),
		UploadedSize:     int64(len(content)),
		FileCount:        1,
		ChangedFileCount: 1,
		Files: []FileEntry{{
			Path:     "world/level.dat",
			Size:     int64(len(content)),
			SHA256:   sum,
			ObjectID: ObjectID(sum),
		}},
	}
	downloader := func(context.Context, string) (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
	}
	summary, err := Restore(context.Background(), RestoreOptions{
		ServerDir:      server,
		Manifest:       manifest,
		Downloader:     downloader,
		ConsistentOnly: true,
		TransactionID:  "txn",
	})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if summary.DownloadedFiles != 1 || len(summary.RollbackRoots) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	got, err := os.ReadFile(filepath.Join(server, "world", "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("restored content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(server, ".acbh-world-staging-txn")); !os.IsNotExist(err) {
		t.Fatalf("staging directory should be removed, stat err=%v", err)
	}
}

func TestRestoreHashFailureDoesNotModifyCurrentWorld(t *testing.T) {
	server := t.TempDir()
	writeWorldFile(t, server, "world/level.dat", "old")
	content := []byte("new")
	wrong := sha([]byte("different"))
	manifest := Manifest{
		SchemaVersion:    1,
		SnapshotID:       "ws_restore",
		GroupID:          "grp_123",
		SourceHostID:     "host_123",
		HostGeneration:   1,
		CreatedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		Consistent:       true,
		LogicalSize:      int64(len(content)),
		UploadedSize:     int64(len(content)),
		FileCount:        1,
		ChangedFileCount: 1,
		Files: []FileEntry{{
			Path:     "world/level.dat",
			Size:     int64(len(content)),
			SHA256:   wrong,
			ObjectID: ObjectID(wrong),
		}},
	}
	downloader := func(context.Context, string) (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
	}
	_, err := Restore(context.Background(), RestoreOptions{
		ServerDir:      server,
		Manifest:       manifest,
		Downloader:     downloader,
		ConsistentOnly: true,
		TransactionID:  "txn",
	})
	if err == nil {
		t.Fatalf("Restore() should fail hash verification")
	}
	got, err := os.ReadFile(filepath.Join(server, "world", "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("current world changed on failed restore: %q", got)
	}
}

func TestRestoreAllowsTopLevelFilesInCleanTarget(t *testing.T) {
	server := t.TempDir()
	content := []byte("allow-list=true")
	sum := sha(content)
	manifest := Manifest{
		SchemaVersion:    1,
		SnapshotID:       "ws_top_level",
		GroupID:          "grp_123",
		SourceHostID:     "host_123",
		HostGeneration:   1,
		CreatedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		Consistent:       true,
		LogicalSize:      int64(len(content)),
		UploadedSize:     int64(len(content)),
		FileCount:        1,
		ChangedFileCount: 1,
		Files: []FileEntry{{
			Path:     "banned-ips.json",
			Size:     int64(len(content)),
			SHA256:   sum,
			ObjectID: ObjectID(sum),
		}},
	}
	downloader := func(context.Context, string) (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
	}
	summary, err := Restore(context.Background(), RestoreOptions{
		ServerDir:      server,
		Manifest:       manifest,
		Downloader:     downloader,
		ConsistentOnly: true,
		TransactionID:  "txn",
	})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if summary.DownloadedFiles != 1 {
		t.Fatalf("DownloadedFiles = %d, want 1", summary.DownloadedFiles)
	}
	got, err := os.ReadFile(filepath.Join(server, "banned-ips.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("restored content = %q", got)
	}
}

func TestRestoreRejectsSymlinkTopLevelTarget(t *testing.T) {
	server := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "banned-ips.json")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(server, "banned-ips.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	content := []byte("new")
	sum := sha(content)
	manifest := Manifest{
		SchemaVersion:    1,
		SnapshotID:       "ws_symlink",
		GroupID:          "grp_123",
		SourceHostID:     "host_123",
		HostGeneration:   1,
		CreatedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		Consistent:       true,
		LogicalSize:      int64(len(content)),
		UploadedSize:     int64(len(content)),
		FileCount:        1,
		ChangedFileCount: 1,
		Files: []FileEntry{{
			Path:     "banned-ips.json",
			Size:     int64(len(content)),
			SHA256:   sum,
			ObjectID: ObjectID(sum),
		}},
	}
	downloader := func(context.Context, string) (io.ReadCloser, int64, error) {
		return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
	}
	_, err := Restore(context.Background(), RestoreOptions{
		ServerDir:      server,
		Manifest:       manifest,
		Downloader:     downloader,
		ConsistentOnly: true,
		TransactionID:  "txn",
	})
	if err == nil || !strings.Contains(err.Error(), "symlink or reparse point") {
		t.Fatalf("Restore() error = %v, want symlink rejection", err)
	}
	got, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "outside" {
		t.Fatalf("outside file changed: %q", got)
	}
}

func buildTestSnapshot(t *testing.T, server, appData, snapshotID string, parent *Manifest) Snapshot {
	t.Helper()
	got, err := BuildSnapshot(testOptions(server, appData, snapshotID, parent))
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	return got
}

func testOptions(server, appData, snapshotID string, parent *Manifest, roots ...string) ScanOptions {
	return ScanOptions{
		ServerDir:      server,
		AppDataDir:     appData,
		WorldRoots:     roots,
		SnapshotID:     snapshotID,
		GroupID:        "grp_123",
		SourceHostID:   "host_123",
		HostGeneration: 1,
		Parent:         parent,
		Consistent:     true,
		Clock: func() time.Time {
			return time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
		},
	}
}

func writeWorldFile(t *testing.T, root, rel, content string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func removeFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func sha(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
