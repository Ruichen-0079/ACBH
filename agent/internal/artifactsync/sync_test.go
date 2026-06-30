package artifactsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestPushAbortsIfLocalHashDiffers(t *testing.T) {
	serverDir := t.TempDir()
	writeFile(t, serverDir, "world/region/r.0.0.mca", "changed")
	manifestPath := writeManifest(t, t.TempDir(), testManifest(sha256Hex([]byte("original"))))

	client := &fakeClient{}
	_, err := Push(context.Background(), PushOptions{
		ManifestPath: manifestPath,
		ServerDir:    serverDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("Push() error = %v, want sha mismatch", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("client calls = %#v, want none", client.calls)
	}
}

func TestPushUploadsObjectsBeforeManifestAndHandlesDeletes(t *testing.T) {
	serverDir := t.TempDir()
	writeFile(t, serverDir, "world/region/r.0.0.mca", "region data")
	m := testManifest(sha256Hex([]byte("region data")))
	m.Files = append(m.Files, manifest.FileEntry{
		Path:    "world/region/r.1.0.mca",
		Class:   fileclass.WorldRuntime,
		Size:    0,
		SHA256:  "",
		Deleted: true,
	})
	m.Summary.DeletedFiles = 1
	manifestPath := writeManifest(t, t.TempDir(), m)

	client := &fakeClient{}
	got, err := Push(context.Background(), PushOptions{
		ManifestPath: manifestPath,
		ServerDir:    serverDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if strings.Join(client.calls, ",") != "uploadObjectStream,uploadManifest" {
		t.Fatalf("calls = %#v", client.calls)
	}
	if got.UploadedObjects != 1 || got.DeletedEntries != 1 || got.CoordinatorStatus != "available" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestPushCanUseLegacyJSONUpload(t *testing.T) {
	serverDir := t.TempDir()
	writeFile(t, serverDir, "world/region/r.0.0.mca", "region data")
	manifestPath := writeManifest(t, t.TempDir(), testManifest(sha256Hex([]byte("region data"))))

	client := &fakeClient{}
	_, err := Push(context.Background(), PushOptions{
		ManifestPath:     manifestPath,
		ServerDir:        serverDir,
		Config:           testConfig(),
		Client:           client,
		LegacyJSONUpload: true,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if strings.Join(client.calls, ",") != "uploadObject,uploadManifest" {
		t.Fatalf("calls = %#v", client.calls)
	}
}

func TestPushPropagatesHostGeneration(t *testing.T) {
	serverDir := t.TempDir()
	writeFile(t, serverDir, "world/region/r.0.0.mca", "region data")
	manifestPath := writeManifest(t, t.TempDir(), testManifest(sha256Hex([]byte("region data"))))

	client := &fakeClient{}
	gen := 3
	_, err := Push(context.Background(), PushOptions{
		ManifestPath:   manifestPath,
		ServerDir:      serverDir,
		Config:         testConfig(),
		Client:         client,
		HostGeneration: &gen,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if client.lastManifestGeneration == nil || *client.lastManifestGeneration != 3 {
		t.Fatalf("lastManifestGeneration = %#v, want 3", client.lastManifestGeneration)
	}
}

func TestPushAndPullZeroByteNonDeletedFile(t *testing.T) {
	serverDir := t.TempDir()
	writeFile(t, serverDir, "world/data/empty.dat", "")
	m := testManifest(sha256Hex(nil))
	m.Files[0].Path = "world/data/empty.dat"
	m.Files[0].Size = 0
	m.Summary.TotalBytes = 0
	manifestPath := writeManifest(t, t.TempDir(), m)

	client := &fakeClient{}
	pushed, err := Push(context.Background(), PushOptions{
		ManifestPath: manifestPath,
		ServerDir:    serverDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if pushed.UploadedObjects != 1 || pushed.TotalBytesUploaded != 0 {
		t.Fatalf("push summary = %#v", pushed)
	}

	outputDir := t.TempDir()
	pulled, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if pulled.WrittenFiles != 1 || pulled.TotalBytes != 0 {
		t.Fatalf("pull summary = %#v", pulled)
	}
	if data := readFile(t, outputDir, "world/data/empty.dat"); len(data) != 0 {
		t.Fatalf("restored file length = %d, want 0", len(data))
	}
}

func TestPullWritesFilesUnderOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	content := []byte("region data")
	m := testManifest(sha256Hex(content))
	client := &fakeClient{
		manifest: m,
		objects: map[string][]byte{
			sha256Hex(content): content,
		},
	}

	got, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "latest",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if got.WrittenFiles != 1 {
		t.Fatalf("WrittenFiles = %d, want 1", got.WrittenFiles)
	}
	if strings.Join(client.calls, ",") != "downloadObjectStream" {
		t.Fatalf("calls = %#v, want streaming download", client.calls)
	}
	if data := readFile(t, outputDir, "world/region/r.0.0.mca"); string(data) != "region data" {
		t.Fatalf("restored content = %q", string(data))
	}
}

func TestPullWritesTopLevelFileUnderOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	content := []byte("server-port=25565\n")
	m := testManifest(sha256Hex(content))
	m.Files[0].Path = "server.properties"
	m.Files[0].Size = int64(len(content))
	m.Summary.TotalBytes = int64(len(content))
	client := &fakeClient{
		manifest: m,
		objects: map[string][]byte{
			sha256Hex(content): content,
		},
	}

	got, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if got.WrittenFiles != 1 {
		t.Fatalf("WrittenFiles = %d, want 1", got.WrittenFiles)
	}
	if data := readFile(t, outputDir, "server.properties"); string(data) != string(content) {
		t.Fatalf("restored content = %q", string(data))
	}
}

func TestPullRejectsTraversalAndAbsoluteManifestPaths(t *testing.T) {
	for _, unsafePath := range []string{
		"../evil",
		"/outside/evil",
		`C:\outside\evil`,
		`\\server\share\evil`,
	} {
		t.Run(unsafePath, func(t *testing.T) {
			outputDir := t.TempDir()
			m := testManifest(sha256Hex([]byte("region data")))
			m.Files[0].Path = unsafePath
			client := &fakeClient{manifest: m}

			_, err := Pull(context.Background(), PullOptions{
				ArtifactKind: manifest.WorldSnapshot,
				ArtifactID:   "snap_000001",
				OutputDir:    outputDir,
				Config:       testConfig(),
				Client:       client,
			})
			if err == nil {
				t.Fatalf("Pull() accepted unsafe path %q", unsafePath)
			}
		})
	}
}

func TestPullRejectsExistingSymlinkParent(t *testing.T) {
	outputDir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(outputDir, "world")); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	content := []byte("region data")
	client := &fakeClient{
		manifest: testManifest(sha256Hex(content)),
		objects:  map[string][]byte{sha256Hex(content): content},
	}

	_, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink or reparse point") {
		t.Fatalf("Pull() error = %v, want symlink rejection", err)
	}
	if fileExists(filepath.Join(outsideDir, "region", "r.0.0.mca")) {
		t.Fatal("restore wrote through a symlink parent")
	}
}

func TestPullRejectsSymlinkInOutputDirectoryPath(t *testing.T) {
	baseDir := t.TempDir()
	outsideDir := t.TempDir()
	linkPath := filepath.Join(baseDir, "linked-output")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	outputDir := filepath.Join(linkPath, "server")
	content := []byte("region data")
	client := &fakeClient{
		manifest: testManifest(sha256Hex(content)),
		objects:  map[string][]byte{sha256Hex(content): content},
	}

	_, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink or reparse point") {
		t.Fatalf("Pull() error = %v, want output directory symlink rejection", err)
	}
	if fileExists(filepath.Join(outsideDir, "server")) {
		t.Fatal("restore created output directories through a symlink")
	}
}

func TestPullRejectsExistingSymlinkFinalTarget(t *testing.T) {
	outputDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.dat")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outputDir, "world", "region", "r.0.0.mca")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, target); err != nil {
		t.Skipf("file symlink is unavailable: %v", err)
	}
	content := []byte("region data")
	client := &fakeClient{
		manifest: testManifest(sha256Hex(content)),
		objects:  map[string][]byte{sha256Hex(content): content},
	}

	_, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink or reparse point") {
		t.Fatalf("Pull() error = %v, want symlink rejection", err)
	}
	if data, readErr := os.ReadFile(outsidePath); readErr != nil || string(data) != "outside" {
		t.Fatalf("outside file changed: data=%q err=%v", data, readErr)
	}
}

func TestPullDeleteRejectsSymlinkTarget(t *testing.T) {
	outputDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.dat")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outputDir, "world", "region", "r.1.0.mca")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, target); err != nil {
		t.Skipf("file symlink is unavailable: %v", err)
	}
	m := testManifest(sha256Hex([]byte("region data")))
	m.Files = []manifest.FileEntry{{
		Path:    "world/region/r.1.0.mca",
		Class:   fileclass.WorldRuntime,
		Deleted: true,
	}}
	m.Summary = manifest.Summary{DeletedFiles: 1}

	_, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		ApplyDeletes: true,
		Config:       testConfig(),
		Client:       &fakeClient{manifest: m},
	})
	if err == nil || !strings.Contains(err.Error(), "symlink or reparse point") {
		t.Fatalf("Pull() error = %v, want symlink rejection", err)
	}
	if data, readErr := os.ReadFile(outsidePath); readErr != nil || string(data) != "outside" {
		t.Fatalf("outside file changed: data=%q err=%v", data, readErr)
	}
}

func TestRestorePathRejectsWindowsReservedNamesAndADS(t *testing.T) {
	for _, unsafePath := range []string{
		"world/CON",
		"world/con.txt",
		"world/AuX.dat",
		"world/COM9.log",
		"world/LPT1",
		"world/file.txt:stream",
		`C:\outside\file.txt`,
	} {
		t.Run(unsafePath, func(t *testing.T) {
			if _, err := resolveRestoreTarget(t.TempDir(), unsafePath); err == nil {
				t.Fatalf("resolveRestoreTarget() accepted %q", unsafePath)
			}
		})
	}
}

func TestRestorePathDetectsWindowsReparsePointAttribute(t *testing.T) {
	if !isReparsePointAttributes(fileAttributeReparsePoint) {
		t.Fatal("reparse point attribute was not detected")
	}
	if isReparsePointAttributes(0) {
		t.Fatal("ordinary attributes were treated as a reparse point")
	}
	err := validateRestoreDirectory("junction", restorePathEntry{
		mode:         os.ModeDir,
		reparsePoint: true,
	})
	if err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("validateRestoreDirectory() error = %v, want reparse point rejection", err)
	}
}

func TestPullRejectsUnsafeManifestPaths(t *testing.T) {
	outputDir := t.TempDir()
	m := testManifest(sha256Hex([]byte("region data")))
	m.Files[0].Path = "../escape.txt"
	client := &fakeClient{manifest: m}

	_, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err == nil {
		t.Fatal("Pull() accepted unsafe path")
	}
}

func TestPullVerifiesSHA256(t *testing.T) {
	outputDir := t.TempDir()
	content := []byte("region data")
	m := testManifest(sha256Hex(content))
	client := &fakeClient{
		manifest: m,
		objects: map[string][]byte{
			sha256Hex(content): []byte("wrong"),
		},
	}

	_, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("Pull() error = %v, want sha mismatch", err)
	}
	target := filepath.Join(outputDir, "world", "region", "r.0.0.mca")
	if fileExists(target) {
		t.Fatal("hash-mismatched object was moved into final path")
	}
	temporaryFiles, globErr := filepath.Glob(filepath.Join(filepath.Dir(target), ".acbh-object-*.tmp"))
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("temporary files remain after failed pull: %#v", temporaryFiles)
	}
}

func TestPullDeletesOnlyWithApplyDeletes(t *testing.T) {
	outputDir := t.TempDir()
	writeFile(t, outputDir, "world/region/r.1.0.mca", "old")
	m := testManifest(sha256Hex([]byte("region data")))
	m.Files = []manifest.FileEntry{
		{
			Path:    "world/region/r.1.0.mca",
			Class:   fileclass.WorldRuntime,
			Size:    0,
			SHA256:  "",
			Deleted: true,
		},
	}
	m.Summary = manifest.Summary{IncludedFiles: 0, DeletedFiles: 1}

	client := &fakeClient{manifest: m}
	got, err := Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if got.PendingDeletes != 1 || !fileExists(filepath.Join(outputDir, "world", "region", "r.1.0.mca")) {
		t.Fatalf("summary = %#v, file should remain", got)
	}

	got, err = Pull(context.Background(), PullOptions{
		ArtifactKind: manifest.WorldSnapshot,
		ArtifactID:   "snap_000001",
		OutputDir:    outputDir,
		ApplyDeletes: true,
		Config:       testConfig(),
		Client:       client,
	})
	if err != nil {
		t.Fatalf("Pull(apply deletes) error = %v", err)
	}
	if got.AppliedDeletes != 1 || fileExists(filepath.Join(outputDir, "world", "region", "r.1.0.mca")) {
		t.Fatalf("summary = %#v, file should be deleted", got)
	}
}

type fakeClient struct {
	calls                  []string
	manifest               manifest.Manifest
	objects                map[string][]byte
	lastManifestGeneration *int
}

func (c *fakeClient) UploadObject(ctx context.Context, req coordinator.UploadObjectRequest) (coordinator.UploadObjectResponse, error) {
	c.calls = append(c.calls, "uploadObject")
	content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		return coordinator.UploadObjectResponse{}, err
	}
	if c.objects == nil {
		c.objects = make(map[string][]byte)
	}
	exists := c.objects[req.SHA256] != nil
	c.objects[req.SHA256] = content
	return coordinator.UploadObjectResponse{OK: true, SHA256: req.SHA256, Exists: exists}, nil
}

func (c *fakeClient) UploadObjectStream(
	ctx context.Context,
	auth coordinator.ArtifactAuth,
	sha256 string,
	content io.Reader,
	size int64,
) (coordinator.UploadObjectResponse, error) {
	c.calls = append(c.calls, "uploadObjectStream")
	data, err := io.ReadAll(content)
	if err != nil {
		return coordinator.UploadObjectResponse{}, err
	}
	if int64(len(data)) != size {
		return coordinator.UploadObjectResponse{}, io.ErrUnexpectedEOF
	}
	if c.objects == nil {
		c.objects = make(map[string][]byte)
	}
	_, exists := c.objects[sha256]
	c.objects[sha256] = data
	return coordinator.UploadObjectResponse{
		OK:     true,
		SHA256: sha256,
		Exists: exists,
		Size:   size,
	}, nil
}

func (c *fakeClient) UploadManifest(ctx context.Context, req coordinator.UploadManifestRequest) (coordinator.UploadManifestResponse, error) {
	c.calls = append(c.calls, "uploadManifest")
	c.manifest = req.Manifest
	if req.HostGeneration != nil {
		gen := *req.HostGeneration
		c.lastManifestGeneration = &gen
	}
	return coordinator.UploadManifestResponse{OK: true, ArtifactKind: req.ArtifactKind, ArtifactID: req.ArtifactID, Status: "available"}, nil
}

func (c *fakeClient) GetLatestArtifact(ctx context.Context, auth coordinator.ArtifactAuth, artifactKind manifest.ArtifactKind) (coordinator.ArtifactMetadata, error) {
	return coordinator.ArtifactMetadata{GroupID: auth.GroupID, ArtifactKind: artifactKind, ArtifactID: "snap_000001", Status: "available"}, nil
}

func (c *fakeClient) DownloadManifest(ctx context.Context, auth coordinator.ArtifactAuth, artifactKind manifest.ArtifactKind, artifactID string) (coordinator.DownloadManifestResponse, error) {
	return coordinator.DownloadManifestResponse{
		Metadata: coordinator.ArtifactMetadata{GroupID: auth.GroupID, ArtifactKind: artifactKind, ArtifactID: artifactID, Status: "available"},
		Manifest: c.manifest,
	}, nil
}

func (c *fakeClient) DownloadObjectStream(ctx context.Context, auth coordinator.ArtifactAuth, sha256 string) (io.ReadCloser, int64, error) {
	c.calls = append(c.calls, "downloadObjectStream")
	content := c.objects[sha256]
	return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
}

func testConfig() agentconfig.Config {
	return agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "group_abc",
		MemberID:       "mem_abc",
		HostID:         "host_abc",
		HostToken:      "secret",
		DisplayName:    "PlayerA",
		DeviceName:     "PlayerA-PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	}
}

func testManifest(fileSHA string) manifest.Manifest {
	modifiedAt := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	pack := "pack_000001"
	return manifest.Manifest{
		ManifestVersion:   manifest.ManifestVersion,
		ArtifactKind:      manifest.WorldSnapshot,
		ArtifactID:        "snap_000001",
		GroupID:           "group_abc",
		CreatedAt:         modifiedAt,
		CreatorHostID:     "host_abc",
		ParentArtifactID:  nil,
		ServerPackVersion: &pack,
		Files: []manifest.FileEntry{
			{
				Path:       "world/region/r.0.0.mca",
				Class:      fileclass.WorldRuntime,
				Size:       int64(len("region data")),
				SHA256:     fileSHA,
				ModifiedAt: &modifiedAt,
				Deleted:    false,
			},
		},
		Summary: manifest.Summary{IncludedFiles: 1, TotalBytes: int64(len("region data"))},
	}
}

func writeManifest(t *testing.T, dir string, m manifest.Manifest) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	if err := manifest.SaveFile(path, m); err != nil {
		t.Fatalf("SaveFile() error = %v", err)
	}
	return path
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readFile(t *testing.T, root, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return data
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
