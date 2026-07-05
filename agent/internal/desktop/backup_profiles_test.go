package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

func TestAnalyzeBackupServerDetectsNeoForgeMigratablePack(t *testing.T) {
	server := writePackFixture(t)
	analysis, err := AnalyzeBackupServer(server)
	if err != nil {
		t.Fatalf("AnalyzeBackupServer() error = %v", err)
	}
	if analysis.Loader != "NeoForge" {
		t.Fatalf("loader = %q, want NeoForge", analysis.Loader)
	}
	if analysis.MinecraftVersion != "1.21.1" {
		t.Fatalf("minecraftVersion = %q, want 1.21.1", analysis.MinecraftVersion)
	}
	if analysis.LevelName != "world" || !analysis.WorldPending {
		t.Fatalf("level/world pending = %q/%v, want world/true", analysis.LevelName, analysis.WorldPending)
	}
	if analysis.RecommendedPreset != BackupPresetMigratable {
		t.Fatalf("recommended preset = %q, want %s", analysis.RecommendedPreset, BackupPresetMigratable)
	}
	if !analysis.IncludeMods || analysis.IncludeLibraries || analysis.IncludeJRE {
		t.Fatalf("include flags mods/libraries/jre = %v/%v/%v", analysis.IncludeMods, analysis.IncludeLibraries, analysis.IncludeJRE)
	}
	if analysis.Mods.FileCount != 2 {
		t.Fatalf("mods file count = %d, want 2", analysis.Mods.FileCount)
	}
}

func TestBackupPresetScanCoversPackAwareDefaults(t *testing.T) {
	server := writePackFixture(t)
	core, err := BuildBackupProfileFromPreset(server, BackupPresetCore)
	if err != nil {
		t.Fatal(err)
	}
	if hasRoot(core, "mods") {
		t.Fatal("core preset should not include mods")
	}
	migratable, err := BuildBackupProfileFromPreset(server, BackupPresetMigratable)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoot(migratable, "mods") {
		t.Fatal("migratable preset should include mods")
	}
	if hasRoot(migratable, "libraries") || hasRoot(migratable, "jre") {
		t.Fatal("migratable preset should not include libraries or jre")
	}
	offline, err := BuildBackupProfileFromPreset(server, BackupPresetOffline)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoot(offline, "libraries") || !hasRoot(offline, "jre") {
		t.Fatal("offline preset should include libraries and jre")
	}

	files, roots, warnings, err := scanBackupProfileSources(migratable)
	if err != nil {
		t.Fatalf("scanBackupProfileSources() error = %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("world pending should produce a warning")
	}
	pending := 0
	for _, root := range roots {
		if root.RootID == "world" && root.Pending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending world roots = %d, want 1", pending)
	}
	paths := sourceManifestPaths(files)
	for _, want := range []string{
		"mods/mod-a.jar",
		"mods/mod-b.jar",
		"config/ftbquests/quests/data.snbt",
		"global_packs/required_data/pack.zip",
	} {
		if !slices.Contains(paths, want) {
			t.Fatalf("scan missing %s; paths=%v", want, paths)
		}
	}
	for _, excluded := range []string{
		"logs/latest.log",
		"config/.puzzle_cache/cache.png",
		"config/super_resolution/shader_caches/cache.bin",
		"libraries/neoforge/library.jar",
		"jre/bin/java.exe",
	} {
		if slices.Contains(paths, excluded) {
			t.Fatalf("scan included excluded path %s", excluded)
		}
	}
}

func TestBackupProfileManualFileExternalRootAndPersistence(t *testing.T) {
	server := writePackFixture(t)
	external := t.TempDir()
	writeFile(t, filepath.Join(external, "notes", "readme.txt"), "external")
	profile := BackupProfile{
		SchemaVersion: 2,
		ProfileID:     "manual",
		Name:          "Manual",
		Preset:        "custom",
		ServerDir:     server,
		ExcludeGlobs:  defaultBackupExcludeGlobs,
		Roots: []BackupProfileRoot{
			{RootID: "ops", DisplayName: "ops", Kind: "file", SourcePath: filepath.Join(server, "ops.json"), RestorePath: filepath.Join(server, "ops.json"), Enabled: true},
			{RootID: "external", DisplayName: "external", Kind: "directory", SourcePath: filepath.Join(external, "notes"), RestorePath: filepath.Join(external, "restore"), Enabled: true},
			{RootID: "disabled", DisplayName: "disabled", Kind: "directory", SourcePath: filepath.Join(server, "mods"), RestorePath: filepath.Join(server, "mods"), Enabled: false},
		},
	}
	files, roots, _, err := scanBackupProfileSources(profile)
	if err != nil {
		t.Fatalf("scanBackupProfileSources() error = %v", err)
	}
	paths := sourceManifestPaths(files)
	for _, want := range []string{"ops/ops.json", "external/readme.txt"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("missing manual path %s; paths=%v", want, paths)
		}
	}
	if slices.Contains(paths, "disabled/mod-a.jar") {
		t.Fatal("disabled root was scanned")
	}
	if len(roots) != 3 {
		t.Fatalf("roots = %d, want 3", len(roots))
	}

	appData := t.TempDir()
	if _, err := UpsertBackupProfile(Options{AppDataDir: appData}, profile); err != nil {
		t.Fatalf("UpsertBackupProfile() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(appData, "backup-profiles", "index.json")); err != nil {
		t.Fatalf("profile index was not saved in backup-profiles dir: %v", err)
	}
	loaded, err := findBackupProfile(Options{AppDataDir: appData}, "manual")
	if err != nil {
		t.Fatalf("findBackupProfile() error = %v", err)
	}
	if loaded.Roots[0].Kind != "file" || !loaded.Roots[0].Enabled {
		t.Fatalf("loaded manual profile lost root metadata: %#v", loaded.Roots[0])
	}
}

func TestBackupProfileSnapshotDeltaAndDeletedPaths(t *testing.T) {
	server := writePackFixture(t)
	writeFile(t, filepath.Join(server, "world", "region", "r.0.0.mca"), "region-a")
	profile, err := BuildBackupProfileFromPreset(server, BackupPresetMigratable)
	if err != nil {
		t.Fatal(err)
	}
	files, _, _, err := scanBackupProfileSources(profile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testBackupAgentConfig()
	first, _, _ := buildProfileSnapshotWithParent(profile, cfg, 1, "bp_first", files, nil)
	if first.ChangedFileCount != first.FileCount || first.DeletedFileCount != 0 {
		t.Fatalf("first changed/deleted = %d/%d, fileCount=%d", first.ChangedFileCount, first.DeletedFileCount, first.FileCount)
	}
	second, _, _ := buildProfileSnapshotWithParent(profile, cfg, 1, "bp_second", files, &first)
	if second.ChangedFileCount != 0 || second.UploadedSize != 0 {
		t.Fatalf("unchanged changed/uploaded = %d/%d, want 0/0", second.ChangedFileCount, second.UploadedSize)
	}
	writeFile(t, filepath.Join(server, "mods", "mod-a.jar"), "mod-a-v2")
	if err := os.Remove(filepath.Join(server, "mods", "mod-b.jar")); err != nil {
		t.Fatal(err)
	}
	files, _, _, err = scanBackupProfileSources(profile)
	if err != nil {
		t.Fatal(err)
	}
	third, plan, _ := buildProfileSnapshotWithParent(profile, cfg, 1, "bp_third", files, &second)
	if third.ChangedFileCount != 1 || len(plan.Objects) != 1 {
		t.Fatalf("third changed/objects = %d/%d, want 1/1", third.ChangedFileCount, len(plan.Objects))
	}
	if third.DeletedFileCount != 1 || third.DeletedPaths[0] != "mods/mod-b.jar" {
		t.Fatalf("deleted paths = %#v", third.DeletedPaths)
	}
}

func TestRestoreProfileManifestRollbackAndDownloadToDirectory(t *testing.T) {
	root := t.TempDir()
	restoreRoot := filepath.Join(root, "restore")
	oldTarget := filepath.Join(restoreRoot, "server.properties")
	writeFile(t, oldTarget, "old")
	profile := BackupProfile{
		SchemaVersion: 2,
		ProfileID:     "manual",
		Name:          "Manual",
		Roots: []BackupProfileRoot{{
			RootID: "server-properties", DisplayName: "server.properties", Kind: "file",
			SourcePath: filepath.Join(root, "server.properties"), RestorePath: restoreRoot, Enabled: true,
		}},
	}
	manifest := testManifest("bp_restore", []worldbackup.FileEntry{
		testFileEntry("server-properties/server.properties", "new"),
	})
	summary, err := restoreProfileManifest(context.Background(), profile, manifest, fakeDownloader(map[string]string{"new": "new"}))
	if err != nil {
		t.Fatalf("restoreProfileManifest() error = %v", err)
	}
	if summary.DownloadedFiles != 1 {
		t.Fatalf("downloaded files = %d, want 1", summary.DownloadedFiles)
	}
	if got := readFile(t, oldTarget); got != "new" {
		t.Fatalf("restored content = %q, want new", got)
	}

	writeFile(t, oldTarget, "old-again")
	failing := testManifest("bp_restore_fail", []worldbackup.FileEntry{
		testFileEntry("server-properties/server.properties", "newer"),
		testFileEntry("server-properties/second.txt", "missing"),
	})
	_, err = restoreProfileManifest(context.Background(), profile, failing, fakeDownloader(map[string]string{"newer": "newer"}))
	if err == nil {
		t.Fatal("restoreProfileManifest() should fail when an object is missing")
	}
	if got := readFile(t, oldTarget); got != "old-again" {
		t.Fatalf("rollback content = %q, want old-again", got)
	}

	downloadProfile := profileForDownloadTarget(profile, filepath.Join(root, "downloaded"))
	if downloadProfile.Roots[0].RestorePath != filepath.Join(root, "downloaded", "server-properties") {
		t.Fatalf("download target = %q", downloadProfile.Roots[0].RestorePath)
	}
}

func TestSafeProfileTargetRejectsTraversal(t *testing.T) {
	if _, err := safeProfileTarget(t.TempDir(), "../escape.txt"); err == nil {
		t.Fatal("safeProfileTarget should reject traversal")
	}
}

func TestSafeProfileTargetAllowsTopLevelFileInCleanDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific regression for non-existent restore roots")
	}
	base := t.TempDir()
	root := filepath.Join(base, "banned-ips")
	target, err := safeProfileTarget(root, "banned-ips.json")
	if err != nil {
		t.Fatalf("safeProfileTarget() error = %v", err)
	}
	want := filepath.Join(root, "banned-ips.json")
	if target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
}

func TestRestoreProfileAllowsTopLevelFileInCleanDownloadDir(t *testing.T) {
	base := t.TempDir()
	downloadDir := filepath.Join(base, "download")
	if err := os.MkdirAll(downloadDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := BackupProfile{
		SchemaVersion: 2,
		ProfileID:     "core",
		Name:          "Core",
		Roots: []BackupProfileRoot{{
			RootID: "banned-ips", DisplayName: "banned-ips.json", Kind: "file",
			SourcePath: filepath.Join(base, "banned-ips.json"), RestorePath: filepath.Join(base, "banned-ips.json"),
			Enabled: true,
		}},
	}
	profile = profileForDownloadTarget(profile, downloadDir)
	manifest := testManifest("bp_download", []worldbackup.FileEntry{
		testFileEntry("banned-ips/banned-ips.json", "[]"),
	})
	summary, err := restoreProfileManifest(context.Background(), profile, manifest, fakeDownloader(map[string]string{"[]": "[]"}))
	if err != nil {
		t.Fatalf("restoreProfileManifest() error = %v", err)
	}
	if summary.DownloadedFiles != 1 {
		t.Fatalf("downloaded files = %d, want 1", summary.DownloadedFiles)
	}
	want := filepath.Join(downloadDir, "banned-ips", "banned-ips.json")
	if got := readFile(t, want); got != "[]" {
		t.Fatalf("restored content = %q, want []", got)
	}
}

func TestScanProfileRejectsSymlinkEscapeWhenSupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation commonly requires elevated privileges")
	}
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "secret")
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	profile := BackupProfile{
		SchemaVersion: 2,
		ProfileID:     "manual",
		Name:          "Manual",
		Roots: []BackupProfileRoot{{
			RootID: "link", DisplayName: "link", Kind: "file", SourcePath: link, RestorePath: link, Enabled: true, FollowSymlinks: true,
		}},
	}
	if _, _, _, err := scanBackupProfileSources(profile); err == nil {
		t.Fatal("scanBackupProfileSources should reject a symlink file root that escapes its parent")
	}
}

func writePackFixture(t *testing.T) string {
	t.Helper()
	server := t.TempDir()
	writeFile(t, filepath.Join(server, "server.properties"), "level-name=world\n")
	writeFile(t, filepath.Join(server, "eula.txt"), "eula=true\n")
	writeFile(t, filepath.Join(server, "ops.json"), "[]\n")
	writeFile(t, filepath.Join(server, "whitelist.json"), "[]\n")
	writeFile(t, filepath.Join(server, "manifest.json"), `{"minecraftVersion":"1.21.1"}`)
	writeFile(t, filepath.Join(server, "variables.txt"), "SERVERSTARTERJAR_FORCE_FETCH=true\n")
	writeFile(t, filepath.Join(server, "user_jvm_args.txt"), "-Xmx4G\n")
	writeFile(t, filepath.Join(server, "start.bat"), "java -jar server.jar\n")
	writeFile(t, filepath.Join(server, "start.ps1"), "java -jar server.jar\n")
	writeFile(t, filepath.Join(server, "mods", "mod-a.jar"), "mod-a")
	writeFile(t, filepath.Join(server, "mods", "mod-b.jar"), "mod-b")
	writeFile(t, filepath.Join(server, "config", "ftbquests", "quests", "data.snbt"), "quest")
	writeFile(t, filepath.Join(server, "config", "example-server.toml"), "config")
	writeFile(t, filepath.Join(server, "config", ".puzzle_cache", "cache.png"), "cache")
	writeFile(t, filepath.Join(server, "config", "super_resolution", "shader_caches", "cache.bin"), "shader")
	writeFile(t, filepath.Join(server, "global_packs", "required_data", "pack.zip"), "pack")
	writeFile(t, filepath.Join(server, "libraries", "net", "neoforged", "neoforge", "21.1.218", "neoforge-21.1.218.jar"), "loader")
	writeFile(t, filepath.Join(server, "libraries", "net", "minecraft", "server", "1.21.1", "server-1.21.1.jar"), "server")
	writeFile(t, filepath.Join(server, "jre", "bin", exeName("java")), "java")
	writeFile(t, filepath.Join(server, "logs", "latest.log"), "log")
	return server
}

func testBackupAgentConfig() agentconfig.Config {
	return agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp",
		MemberID:       "mem",
		HostID:         "host",
		HostToken:      "token",
		DisplayName:    "owner",
		DeviceName:     "pc",
		Platform:       runtime.GOOS,
		AgentVersion:   agentconfig.AgentVersion,
	}
}

func hasRoot(profile BackupProfile, rootID string) bool {
	for _, root := range profile.Roots {
		if root.RootID == rootID {
			return true
		}
	}
	return false
}

func sourceManifestPaths(files []profileSourceFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.ManifestPath)
	}
	slices.Sort(paths)
	return paths
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func testManifest(snapshotID string, files []worldbackup.FileEntry) worldbackup.Manifest {
	var logical int64
	for i := range files {
		logical += files[i].Size
	}
	return worldbackup.Manifest{
		SchemaVersion:    worldbackup.SchemaVersion,
		SnapshotID:       snapshotID,
		GroupID:          "grp",
		SourceHostID:     "host",
		HostGeneration:   1,
		CreatedAt:        time.Now().UTC(),
		Consistent:       true,
		LogicalSize:      logical,
		UploadedSize:     logical,
		FileCount:        len(files),
		ChangedFileCount: len(files),
		Files:            files,
	}
}

func testFileEntry(pathValue, content string) worldbackup.FileEntry {
	sum := backupProfileTestSHA256(content)
	return worldbackup.FileEntry{Path: pathValue, Size: int64(len(content)), SHA256: sum, ObjectID: worldbackup.ObjectID(sum)}
}

func fakeDownloader(objects map[string]string) worldbackup.ObjectDownloader {
	return func(ctx context.Context, objectID string) (io.ReadCloser, int64, error) {
		for _, content := range objects {
			sum := backupProfileTestSHA256(content)
			if objectID == worldbackup.ObjectID(sum) {
				return io.NopCloser(bytes.NewReader([]byte(content))), int64(len(content)), nil
			}
		}
		return nil, 0, fmt.Errorf("object not found: %s", objectID)
	}
}

func backupProfileTestSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
