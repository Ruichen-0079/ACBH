package backup

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

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

type fakeBackupClient struct {
	planObjects []worldbackup.PlannedObject
	uploads     []worldbackup.PlannedObject
	manifest    worldbackup.Manifest
	objects     map[string][]byte
	requestURLs []string
}

func (f *fakeBackupClient) Probe(ctx context.Context) (coordinatorclient.ProbeResult, *coreerrors.Error) {
	f.requestURLs = append(f.requestURLs, "http://public.test:6121/health")
	return coordinatorclient.ProbeResult{CoordinatorURL: "http://public.test:6121", ActualRequestURL: "http://public.test:6121/health"}, nil
}

func (f *fakeBackupClient) EnsureActiveLeaseWithGeneration(ctx context.Context, groupID string, hostID string, hostToken string, generation *int) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error) {
	return coordinatorclient.EnsureActiveLeaseResponse{OK: true, Lease: coordinatorclient.HostLeaseStatus{Generation: 7}}, nil
}

func (f *fakeBackupClient) GetLatestWorldBackup(ctx context.Context, groupID string, hostID string, hostToken string, consistentOnly bool) (coordinatorclient.WorldBackupManifestResponse, *coreerrors.Error) {
	if f.manifest.SnapshotID == "" {
		return coordinatorclient.WorldBackupManifestResponse{}, coreerrors.New(coreerrors.SnapshotNotFound, "no snapshot", coreerrors.Details{}, "")
	}
	return coordinatorclient.WorldBackupManifestResponse{Manifest: f.manifest}, nil
}

func (f *fakeBackupClient) PlanWorldBackup(ctx context.Context, groupID string, req coordinatorclient.WorldBackupPlanRequest) (coordinatorclient.WorldBackupPlanResponse, *coreerrors.Error) {
	f.planObjects = append([]worldbackup.PlannedObject{}, req.Objects...)
	missing := append([]worldbackup.PlannedObject{}, req.Objects...)
	if len(missing) > 1 {
		missing = missing[:1]
	}
	return coordinatorclient.WorldBackupPlanResponse{OK: true, MissingObjects: missing, ExistingCount: len(req.Objects) - len(missing)}, nil
}

func (f *fakeBackupClient) UploadWorldObjectStream(ctx context.Context, groupID string, hostID string, hostToken string, sha256 string, content io.Reader, size int64) (coordinatorclient.UploadObjectResponse, *coreerrors.Error) {
	data, _ := io.ReadAll(content)
	f.uploads = append(f.uploads, worldbackup.PlannedObject{SHA256: sha256, Size: int64(len(data))})
	return coordinatorclient.UploadObjectResponse{OK: true, SHA256: sha256, Size: int64(len(data))}, nil
}

func (f *fakeBackupClient) CommitWorldBackup(ctx context.Context, groupID string, req coordinatorclient.WorldBackupCommitRequest) (coordinatorclient.WorldBackupCommitResponse, *coreerrors.Error) {
	f.manifest = req.Manifest
	return coordinatorclient.WorldBackupCommitResponse{OK: true, SnapshotID: req.Manifest.SnapshotID, Status: "completed"}, nil
}

func (f *fakeBackupClient) ListWorldBackups(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.WorldBackupListResponse, *coreerrors.Error) {
	return coordinatorclient.WorldBackupListResponse{Snapshots: []coordinatorclient.WorldBackupMetadata{{SnapshotID: "ws_20260704_120000", Status: "completed", CreatedAt: "2026-07-04T12:00:00Z", FileCount: 1, RootCount: 1, CanRestore: true}}}, nil
}

func (f *fakeBackupClient) GetWorldBackup(ctx context.Context, groupID string, hostID string, hostToken string, snapshotID string) (coordinatorclient.WorldBackupManifestResponse, *coreerrors.Error) {
	return coordinatorclient.WorldBackupManifestResponse{Manifest: f.manifest}, nil
}

func (f *fakeBackupClient) DownloadWorldObjectStream(ctx context.Context, groupID string, hostID string, hostToken string, sha256 string) (io.ReadCloser, int64, *coreerrors.Error) {
	data, ok := f.objects[sha256]
	if !ok {
		return nil, 0, coreerrors.New(coreerrors.SnapshotDownloadFailed, "missing object", coreerrors.Details{}, "")
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func testBackupConfig(serverDir string) coreconfig.Config {
	cfg := coreconfig.DefaultConfig()
	cfg.Mode = "remote-public"
	cfg.CoordinatorURL = "http://public.test:6121"
	cfg.Server.Dir = serverDir
	cfg.Instance.InstanceID = "inst_123"
	cfg.Device.DeviceID = "dev_123"
	cfg.Server.ServerID = "srv_123"
	cfg.Compat.LegacyGroupID = "grp_123"
	cfg.Compat.LegacyHostID = "host_123"
	cfg.Compat.LegacyHostToken = "ht_123"
	return cfg
}

func mustTime(t *testing.T, raw string) time.Time {
	t.Helper()
	out, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func makeServerDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"world/level.dat":       "world",
		"mods/a.jar":            "mod",
		"config/server.toml":    "cfg",
		"banned-ips.json":       "[]",
		"server.properties":     "motd=test",
		"logs/latest.log":       "skip",
		"libraries/lib.jar":     "skip",
		"crash-reports/x.txt":   "skip",
		"versions/1/server.jar": "skip",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAnalyzeIncludesExpectedRootsAndTopLevelFiles(t *testing.T) {
	cfg := testBackupConfig(makeServerDir(t))
	result, err := Service{}.Analyze(context.Background(), cfg, AnalyzeRequest{})
	if err != nil {
		t.Fatalf("%#v suggestion=%s", err, err.Suggestion)
	}
	if !result.OK || result.FileCount != 5 || result.RootCount == 0 {
		t.Fatalf("analyze = %#v", result)
	}
	paths := map[string]bool{}
	for _, root := range result.Roots {
		paths[root.Path] = true
	}
	for _, want := range []string{"world", "mods", "config", "banned-ips.json", "server.properties"} {
		if !paths[want] {
			t.Fatalf("missing backup root %q in %#v", want, result.Roots)
		}
	}
	for _, forbidden := range []string{"logs", "libraries", "crash-reports", "versions"} {
		if paths[forbidden] {
			t.Fatalf("excluded root %q was included", forbidden)
		}
	}
}

func TestAnalyzeRecursivelyCountsAllFilesInDirectoryRoots(t *testing.T) {
	serverDir := t.TempDir()
	files := map[string]string{
		"world/level.dat":          "12345",
		"world/region/r.0.0.mca":   "1234567",
		"world/DIM-1/data/map.dat": "123",
		"mods/a.jar":               "1234",
		"mods/nested/b.jar":        "123456",
		"config/a.toml":            "12",
		"config/deeper/b.toml":     "123",
		"libraries/ignored.jar":    "this must be excluded",
		"logs/latest.log":          "this must be excluded",
		"crash-reports/crash.txt":  "this must be excluded",
		"server.properties":        "motd=test",
		"banned-ips.json":          "[]",
	}
	var wantSize int64
	for name, content := range files {
		path := filepath.Join(serverDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, "libraries/") && !strings.HasPrefix(name, "logs/") && !strings.HasPrefix(name, "crash-reports/") {
			wantSize += int64(len(content))
		}
	}
	for _, emptyDir := range []string{"defaultconfigs", "datapacks", "resourcepacks"} {
		if err := os.MkdirAll(filepath.Join(serverDir, emptyDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Service{}.Analyze(context.Background(), testBackupConfig(serverDir), AnalyzeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileCount != 9 || result.LogicalSize != wantSize {
		t.Fatalf("analyze counted fileCount=%d size=%d, want fileCount=9 size=%d roots=%#v", result.FileCount, result.LogicalSize, wantSize, result.Roots)
	}
	counts := map[string]int{}
	for _, root := range result.Roots {
		counts[root.Path] = root.FileCount
	}
	for path, want := range map[string]int{"world": 3, "mods": 2, "config": 2} {
		if counts[path] != want {
			t.Fatalf("root %s fileCount=%d, want %d; roots=%#v", path, counts[path], want, result.Roots)
		}
	}
	for _, path := range []string{"defaultconfigs", "datapacks", "resourcepacks"} {
		if _, ok := counts[path]; !ok {
			t.Fatalf("empty existing dir root %s was not reported; roots=%#v", path, result.Roots)
		}
	}
}

func TestUploadUsesIdentityAdapterAndDeduplicatesObjects(t *testing.T) {
	serverDir := t.TempDir()
	for _, name := range []string{"world/a.dat", "mods/copy.dat"} {
		path := filepath.Join(serverDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := testBackupConfig(serverDir)
	client := &fakeBackupClient{}
	result, err := Service{Client: client}.Upload(context.Background(), cfg, AnalyzeRequest{})
	if err != nil {
		t.Fatalf("%#v suggestion=%s", err, err.Suggestion)
	}
	if !result.OK || result.ActualRequestURL != "http://public.test:6121/health" {
		t.Fatalf("upload result = %#v", result)
	}
	if len(client.planObjects) != 1 || len(client.uploads) != 1 {
		t.Fatalf("objects were not deduplicated: plan=%#v uploads=%#v", client.planObjects, client.uploads)
	}
	if strings.Contains(strings.Join(client.requestURLs, "\n"), "127.0.0.1:6121") {
		t.Fatalf("used localhost coordinator URL: %#v", client.requestURLs)
	}
}

func TestDownloadBlocksUnsafeTargetsAndRestoresTopLevelFile(t *testing.T) {
	files := map[string][]byte{
		"banned-ips.json":   []byte("[]"),
		"whitelist.json":    []byte("[]"),
		"server.properties": []byte("motd=test"),
		"eula.txt":          []byte("eula=true"),
		"ops.json":          []byte("[]"),
		"world/level.dat":   []byte("world"),
		"mods/a.jar":        []byte("mod"),
		"config/a.toml":     []byte("config"),
	}
	entries := make([]worldbackup.FileEntry, 0, len(files))
	objects := map[string][]byte{}
	var logical int64
	for name, data := range files {
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		entries = append(entries, worldbackup.FileEntry{Path: name, Size: int64(len(data)), SHA256: sha, ObjectID: worldbackup.ObjectID(sha)})
		objects[sha] = data
		logical += int64(len(data))
	}
	client := &fakeBackupClient{
		manifest: worldbackup.Manifest{
			SchemaVersion:  worldbackup.SchemaVersion,
			SnapshotID:     "ws_20260704_120000",
			GroupID:        "grp_123",
			SourceHostID:   "host_123",
			HostGeneration: 7,
			CreatedAt:      mustTime(t, "2026-07-04T12:00:00Z"),
			Consistent:     true,
			LogicalSize:    logical,
			FileCount:      len(entries),
			Files:          entries,
		},
		objects: objects,
	}
	cfg := testBackupConfig(t.TempDir())

	_, err := Service{Client: client}.Download(context.Background(), cfg, "latest", DownloadRequest{})
	if err == nil || err.ErrorCode != coreerrors.TargetDirRequired {
		t.Fatalf("missing targetDir err = %#v", err)
	}

	nonEmpty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonEmpty, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Service{Client: client}.Download(context.Background(), cfg, "latest", DownloadRequest{TargetDir: nonEmpty})
	if err == nil || err.ErrorCode != coreerrors.TargetDirNotEmpty {
		t.Fatalf("non-empty target err = %#v", err)
	}

	target := filepath.Join(t.TempDir(), "restore")
	result, err := Service{Client: client}.Download(context.Background(), cfg, "latest", DownloadRequest{TargetDir: target})
	if err != nil {
		t.Fatal(err)
	}
	if result.DownloadedFiles != len(files) {
		t.Fatalf("download result = %#v", result)
	}
	for name, want := range files {
		got, readErr := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if readErr != nil || string(got) != string(want) {
			t.Fatalf("restore %s got=%q err=%v", name, got, readErr)
		}
	}
}
