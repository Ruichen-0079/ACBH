package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	req, err := http.NewRequest(http.MethodPost, base+"/api/status/refresh", bytes.NewReader([]byte(`{}`)))
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
	req, err := http.NewRequest(http.MethodPost, base+"/api/status/refresh", bytes.NewReader([]byte(`{}`)))
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
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("same-origin status = %d, want 202", resp.StatusCode)
	}
	var accepted struct {
		OperationID string         `json:"operationId"`
		State       OperationState `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode operation: %v", err)
	}
	if accepted.OperationID == "" || accepted.State == "" {
		t.Fatal("missing operation id")
	}
	req, err = http.NewRequest(http.MethodGet, base+"/api/operations/"+accepted.OperationID, nil)
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

func TestDesktopRuntimeRequestReturnDoesNotCancelOperation(t *testing.T) {
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
	req, err := http.NewRequest(http.MethodPost, base+"/api/status/refresh", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var accepted struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode accepted: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	snap := waitRuntimeOperation(t, rt, accepted.OperationID)
	if snap.State == OperationCancelled || snap.CurrentStage == "cancelled" {
		t.Fatalf("operation was cancelled after handler returned: %#v", snap)
	}
	if snap.State != OperationWarning && snap.State != OperationSucceeded {
		t.Fatalf("state = %s, want success or warning", snap.State)
	}
}

func TestOperationManagerTerminalStagesAndIdempotency(t *testing.T) {
	manager := NewOperationManager(context.Background(), Options{AppDataDir: t.TempDir()})
	defer func() {
		_ = manager.Close(context.Background())
	}()
	cases := []struct {
		name  string
		data  any
		err   error
		state OperationState
		stage string
	}{
		{name: "success", data: map[string]string{"ok": "true"}, state: OperationSucceeded, stage: "completed"},
		{name: "warning", data: struct {
			Warnings []string `json:"warnings"`
		}{Warnings: []string{"warn"}}, state: OperationWarning, stage: "completed_with_warnings"},
		{name: "failure", data: map[string]string{"ok": "false"}, err: errors.New("boom"), state: OperationFailed, stage: "failed"},
	}
	for _, tc := range cases {
		snap, err := manager.Start(OperationOptions{Name: tc.name, MutexClass: tc.name, Timeout: time.Second}, func(ctx OperationContext) (any, error) {
			return tc.data, tc.err
		})
		if err != nil {
			t.Fatalf("Start(%s): %v", tc.name, err)
		}
		done := waitManagerOperation(t, manager, snap.OperationID)
		if done.State != tc.state || done.CurrentStage != tc.stage {
			t.Fatalf("%s state/stage = %s/%s, want %s/%s", tc.name, done.State, done.CurrentStage, tc.state, tc.stage)
		}
	}

	first, err := manager.Start(OperationOptions{Name: "idem", MutexClass: "idem", Timeout: time.Second, IdempotencyKey: "same"}, func(ctx OperationContext) (any, error) {
		return "once", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Start(OperationOptions{Name: "idem", MutexClass: "idem", Timeout: time.Second, IdempotencyKey: "same"}, func(ctx OperationContext) (any, error) {
		t.Fatal("idempotent operation ran twice")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.OperationID != first.OperationID {
		t.Fatalf("idempotent operation id = %s, want %s", second.OperationID, first.OperationID)
	}
	_ = waitManagerOperation(t, manager, first.OperationID)
}

func TestOperationManagerCancelCloseTimeoutAndHistory(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	manager := NewOperationManager(root, Options{AppDataDir: t.TempDir()})
	defer func() {
		_ = manager.Close(context.Background())
	}()
	cancelSnap, err := manager.Start(OperationOptions{Name: "cancel", MutexClass: "cancel", Timeout: time.Second, Cancellable: true}, func(ctx OperationContext) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := manager.Cancel(cancelSnap.OperationID); !result.OK || result.NewState != OperationCancelling {
		t.Fatalf("Cancel() = %#v, want cancelling", result)
	}
	cancelDone := waitManagerOperation(t, manager, cancelSnap.OperationID)
	if cancelDone.State != OperationCancelled || cancelDone.CurrentStage != "cancelled" {
		t.Fatalf("cancel state/stage = %s/%s", cancelDone.State, cancelDone.CurrentStage)
	}

	timeoutSnap, err := manager.Start(OperationOptions{Name: "timeout", MutexClass: "timeout", Timeout: 10 * time.Millisecond}, func(ctx OperationContext) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	timeoutDone := waitManagerOperation(t, manager, timeoutSnap.OperationID)
	if timeoutDone.State != OperationTimedOut || timeoutDone.CurrentStage != "timed_out" {
		t.Fatalf("timeout state/stage = %s/%s", timeoutDone.State, timeoutDone.CurrentStage)
	}

	closeRoot, closeCancel := context.WithCancel(context.Background())
	closeManager := NewOperationManager(closeRoot, Options{AppDataDir: t.TempDir()})
	closeSnap, err := closeManager.Start(OperationOptions{Name: "close", MutexClass: "close", Timeout: time.Minute, Cancellable: true}, func(ctx OperationContext) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	closeCancel()
	if err := closeManager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	closeDone := waitManagerOperation(t, closeManager, closeSnap.OperationID)
	if closeDone.State != OperationCancelled || closeDone.CurrentStage != "cancelled" {
		t.Fatalf("close state/stage = %s/%s", closeDone.State, closeDone.CurrentStage)
	}

	for i := 0; i < 110; i++ {
		snap, err := manager.Start(OperationOptions{Name: "history", MutexClass: fmt.Sprintf("history-%d", i), Timeout: time.Second}, func(ctx OperationContext) (any, error) {
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = waitManagerOperation(t, manager, snap.OperationID)
	}
	if got := len(manager.Summary().Operations); got > 100 {
		t.Fatalf("history length = %d, want <= 100", got)
	}
}

func TestDesktopRuntimeSaveServerConfigAcceptsUnicodeBodyWithASCIIIdempotencyKey(t *testing.T) {
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

	if err := agentconfig.Save(filepath.Join(appData, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp_test",
		MemberID:       "mem_test",
		HostID:         "host_test",
		HostToken:      "token",
		DisplayName:    "Tester",
		DeviceName:     "PC",
		Platform:       runtime.GOOS,
		AgentVersion:   agentconfig.AgentVersion,
	}); err != nil {
		t.Fatal(err)
	}
	serverDir := filepath.Join(appData, "中文 Server Dir")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatal(err)
	}
	launchPath := filepath.Join(serverDir, "双击直接开服！！！.bat")
	if err := os.WriteFile(launchPath, []byte("@echo off"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"serverDir":%q,"launchType":"script","launchPath":%q,"workingDir":%q,"startArgs":[],"startTimeoutSeconds":120}`,
		serverDir, launchPath, serverDir)
	req, err := http.NewRequest(http.MethodPut, base+"/api/config/server", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	req.Header.Set("Idempotency-Key", "req_7f3c2a1b-4d5e-6789-abcd-ef0123456789")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("save request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("save status = %d, want 202", resp.StatusCode)
	}
	var accepted struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	snap := waitRuntimeOperation(t, rt, accepted.OperationID)
	if snap.State != OperationSucceeded {
		t.Fatalf("operation state = %s, want succeeded; terminal=%#v", snap.State, snap.Result)
	}
	loaded, err := LoadServerConfigPayload(Options{AppDataDir: appData})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ServerDir != serverDir || loaded.LaunchPath != launchPath {
		t.Fatalf("loaded = %#v, want chinese paths preserved", loaded)
	}
	summary, err := BackupProfileSummaryForServer(Options{AppDataDir: appData}, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary["serverDir"] != serverDir {
		t.Fatalf("backup summary serverDir = %#v, want %q", summary["serverDir"], serverDir)
	}
}

func TestDesktopRuntimeBackupSummaryWithoutQueryFallsBackToDesktopConfig(t *testing.T) {
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

	serverDir := filepath.Join(appData, "Desktop Summary Server")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatal(err)
	}
	launchPath := filepath.Join(serverDir, "双击直接开服！！！.bat")
	if err := os.WriteFile(launchPath, []byte("@echo off"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "server.properties"), []byte("level-name=world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDesktopConfig(Options{AppDataDir: appData}, DesktopConfig{
		LastServerDir: serverDir,
		LaunchProfile: DesktopLaunchProfile{Kind: "script", Path: launchPath},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(appData, agentconfig.FileName)); !os.IsNotExist(err) {
		t.Fatalf("agent config should not exist: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, base+"/api/backup/summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("summary status = %d, body = %s", resp.StatusCode, string(body))
	}
	var out struct {
		OK        bool   `json:"ok"`
		ServerDir string `json:"serverDir"`
		Message   string `json:"message"`
		Roots     []struct {
			RootID      string `json:"rootId"`
			SourcePath  string `json:"sourcePath"`
			RestorePath string `json:"restorePath"`
			Pending     bool   `json:"pending"`
		} `json:"roots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("summary = %#v, want ok=true", out)
	}
	if out.ServerDir != serverDir {
		t.Fatalf("summary serverDir = %q, want %q", out.ServerDir, serverDir)
	}
	if strings.Contains(out.Message, "read config:") {
		t.Fatalf("summary message must not expose wrapped agent config read error: %q", out.Message)
	}
	if len(out.Roots) == 0 {
		t.Fatal("summary roots must not be empty")
	}
	for _, root := range out.Roots {
		if !strings.HasPrefix(root.SourcePath, serverDir) || !strings.HasPrefix(root.RestorePath, serverDir) {
			t.Fatalf("root %q paths must stay under serverDir", root.RootID)
		}
	}
}

func TestDesktopRuntimePickerAPIAcceptsStructuredFiltersInBody(t *testing.T) {
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

	serverDir := filepath.Join(appData, "picker-server")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatal(err)
	}
	launchPath := filepath.Join(serverDir, "start.bat")
	if err := os.WriteFile(launchPath, []byte("@echo off"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"title":"选择启动文件","initialDir":%q,"filters":[{"name":"启动脚本或服务端核心","patterns":["*.bat","*.jar"]}],"path":%q}`,
		serverDir, launchPath)
	req, err := http.NewRequest(http.MethodPost, base+"/api/picker/file", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACBH-Desktop-Session", rt.session)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("picker status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Path != launchPath {
		t.Fatalf("picker response = %#v, want path %q", out, launchPath)
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

func waitRuntimeOperation(t *testing.T, rt *DesktopRuntime, operationID string) OperationSnapshot {
	t.Helper()
	return waitManagerOperation(t, rt.Manager, operationID)
}

func waitManagerOperation(t *testing.T, manager *OperationManager, operationID string) OperationSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap, ok := manager.Get(operationID); ok && isTerminalState(snap.State) {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap, ok := manager.Get(operationID); ok {
		t.Fatalf("operation %s did not finish; last state=%s stage=%s", operationID, snap.State, snap.CurrentStage)
	}
	t.Fatalf("operation %s not found", operationID)
	return OperationSnapshot{}
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
