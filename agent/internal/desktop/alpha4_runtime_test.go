package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestDesktopRuntimeRejectsMaliciousLocalhostMutations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	appData, cleanup := runtimeTestAppData(t)
	defer cleanup()
	rt, err := StartDesktopRuntime(ctx, Options{AppDataDir: appData}, RuntimeOptions{})
	if err != nil {
		cancel()
		t.Fatalf("StartDesktopRuntime() error = %v", err)
	}
	defer closeRuntimeForTest(rt, cancel)
	base := runtimeBaseURL(t, rt.URL)
	client := &http.Client{Timeout: 3 * time.Second}

	req, err := http.NewRequest(http.MethodPost, base+"/api/environment/check", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unauth request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want 401", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodPost, base+"/api/environment/check", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.localhost")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("bad origin request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad origin status = %d, want 403", resp.StatusCode)
	}
}

func TestDesktopRuntimeAcceptsSameOriginJSONMutationAndOperationLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	appData, cleanup := runtimeTestAppData(t)
	defer cleanup()
	rt, err := StartDesktopRuntime(ctx, Options{AppDataDir: appData}, RuntimeOptions{})
	if err != nil {
		cancel()
		t.Fatalf("StartDesktopRuntime() error = %v", err)
	}
	defer closeRuntimeForTest(rt, cancel)
	base := runtimeBaseURL(t, rt.URL)
	req, err := http.NewRequest(http.MethodPost, base+"/api/environment/check", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("same-origin request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("same-origin status = %d, want 200", resp.StatusCode)
	}
	var snap OperationSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode operation: %v", err)
	}
	if snap.OperationID == "" {
		t.Fatal("missing operation id")
	}
	req, err = http.NewRequest(http.MethodGet, base+"/api/operations/"+snap.OperationID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("operation lookup: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operation lookup status = %d, want 200", resp.StatusCode)
	}
}

func TestBackupProfileManualRootsProduceRelativeManifestPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "world", "region"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "world", "level.dat"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "world", "debug.log"), []byte("skip"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := BackupProfile{
		ProfileID: "manual",
		Name:      "Manual",
		Roots: []BackupProfileRoot{{
			RootID: "world-main", DisplayName: "World", Kind: "manual-folder",
			SourcePath: filepath.Join(root, "world"), RestorePath: filepath.Join(root, "restore"),
			Required: true, ExcludePatterns: []string{"*.log"},
		}},
	}
	files, _, _, err := scanBackupProfileSources(profile)
	if err != nil {
		t.Fatalf("scanBackupProfileSources() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	if strings.Contains(files[0].ManifestPath, root) || filepath.IsAbs(files[0].ManifestPath) {
		t.Fatalf("manifest path leaks absolute path: %q", files[0].ManifestPath)
	}
	if files[0].ManifestPath != "world-main/level.dat" {
		t.Fatalf("manifest path = %q, want world-main/level.dat", files[0].ManifestPath)
	}
	cfg := agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121", GroupID: "grp", MemberID: "mem",
		HostID: "host", HostToken: "token", DisplayName: "owner", DeviceName: "pc",
		Platform: runtime.GOOS, AgentVersion: agentconfig.AgentVersion,
	}
	manifest, _ := buildProfileSnapshot(profile, cfg, 1, "bp_manual_test", files)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), filepath.Join(root, "world")) {
		t.Fatalf("manifest leaked absolute source path: %s", encoded)
	}
}

func runtimeBaseURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.RawQuery = ""
	u.Path = ""
	return strings.TrimRight(u.String(), "/")
}

func runtimeTestAppData(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", strings.ReplaceAll(t.Name(), "/", "_"))
	if err != nil {
		t.Fatal(err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }
}

func closeRuntimeForTest(rt *DesktopRuntime, cancel context.CancelFunc) {
	cancel()
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	_ = rt.server.Shutdown(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !rt.Manager.Summary().Busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
