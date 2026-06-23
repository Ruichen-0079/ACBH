package desktop

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestChineseErrorMessages(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "missing ws",
			err:  errors.New("Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'ws'"),
			want: "控制端缺少运行依赖 ws",
		},
		{
			name: "program files eperm",
			err:  errors.New("EPERM C:\\Program Files\\nodejs\\pnpm"),
			want: "当前账户无权修改 Node.js 全局目录",
		},
		{
			name: "invalid access key",
			err:  errors.New("coordinator rejected request (401): Invalid access key"),
			want: "加入密钥与服务器组不匹配",
		},
		{
			name: "group missing",
			err:  errors.New("coordinator rejected request (404): Group not found"),
			want: "本地主机配置存在，但控制端没有对应服务器组",
		},
		{
			name: "rcon password",
			err:  errors.New("RCON password is required; pass --rcon-password"),
			want: "需要 RCON 密码才能安全保存世界",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ChineseError(tc.err).Error()
			if !strings.Contains(got, tc.want) {
				t.Fatalf("ChineseError() = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestPortableAppDataDir(t *testing.T) {
	tempDir := t.TempDir()
	exePath := filepath.Join(tempDir, "acbh-desktop-windows-amd64.exe")
	if err := os.WriteFile(filepath.Join(tempDir, "portable.flag"), []byte(""), 0o600); err != nil {
		t.Fatalf("write portable.flag: %v", err)
	}

	got, err := agentconfig.ResolveAppDataDir(exePath)
	if err != nil {
		t.Fatalf("ResolveAppDataDir() error = %v", err)
	}
	want := filepath.Join(tempDir, "data")
	if got != want {
		t.Fatalf("ResolveAppDataDir() = %q, want %q", got, want)
	}
}

func TestResolveNodeDoesNotUseCorepack(t *testing.T) {
	tempDir := t.TempDir()
	nodePath := filepath.Join(tempDir, "node.exe")
	if err := os.WriteFile(nodePath, []byte(""), 0o700); err != nil {
		t.Fatalf("write fake node: %v", err)
	}

	got, err := resolveNode(Options{
		NodePath:       nodePath,
		ExecutablePath: filepath.Join(tempDir, "acbh-desktop.exe"),
		WorkingDir:     tempDir,
	})
	if err != nil {
		t.Fatalf("resolveNode() error = %v", err)
	}
	if got != nodePath {
		t.Fatalf("resolveNode() = %q, want %q", got, nodePath)
	}
}

func TestMissingPrivateGroupErrorMatchesCoordinatorMessages(t *testing.T) {
	cases := []error{
		errors.New("coordinator rejected request (404): Group not found"),
		errors.New("coordinator rejected request (404): Group does not exist"),
	}
	for _, err := range cases {
		if !isMissingPrivateGroupError(err) {
			t.Fatalf("isMissingPrivateGroupError(%q) = false, want true", err)
		}
	}
}

func TestCheckEnvironmentWritesReport(t *testing.T) {
	tempDir := t.TempDir()
	exePath := filepath.Join(tempDir, "acbh-desktop-windows-amd64.exe")
	if err := os.WriteFile(exePath, []byte("desktop"), 0o700); err != nil {
		t.Fatalf("write desktop exe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "acbh-agent-windows-amd64.exe"), []byte("agent"), 0o700); err != nil {
		t.Fatalf("write agent exe: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tempDir, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "scripts", "acbh-desktop-gui.ps1"), []byte(""), 0o600); err != nil {
		t.Fatalf("write gui: %v", err)
	}

	report, err := CheckEnvironment(Options{AppDataDir: filepath.Join(tempDir, "data"), ExecutablePath: exePath})
	if err != nil {
		t.Fatalf("CheckEnvironment() error = %v", err)
	}
	if report.EnvironmentReportPath == "" {
		t.Fatal("EnvironmentReportPath is empty")
	}
	if _, err := os.Stat(report.EnvironmentReportPath); err != nil {
		t.Fatalf("environment report was not written: %v", err)
	}
}

func TestVerifyEnvironmentPackageRejectsUnsafeOrUnsignedPackages(t *testing.T) {
	tempDir := t.TempDir()
	unsafeZip := filepath.Join(tempDir, "unsafe.zip")
	writeTestPackage(t, unsafeZip, EnvironmentPackageManifest{
		Version: 1, PackageID: "acbh-runtime-base", OS: runtime.GOOS, Architecture: runtime.GOARCH, Signature: "sig",
		Files: []EnvironmentPackageFile{{Path: "../evil.txt", SHA256: strings.Repeat("0", 64), Size: 1}},
	}, map[string]string{"../evil.txt": "x"})
	result, err := VerifyEnvironmentPackage(unsafeZip)
	if err != nil {
		t.Fatalf("VerifyEnvironmentPackage() error = %v", err)
	}
	if result.OK {
		t.Fatal("unsafe zip unexpectedly verified")
	}

	unsignedZip := filepath.Join(tempDir, "unsigned.zip")
	writeTestPackage(t, unsignedZip, EnvironmentPackageManifest{
		Version: 1, PackageID: "acbh-runtime-base", OS: runtime.GOOS, Architecture: runtime.GOARCH,
		Files: []EnvironmentPackageFile{{Path: "base/readme.txt", SHA256: sha256Hex("ok"), Size: 2}},
	}, map[string]string{"base/readme.txt": "ok"})
	result, err = VerifyEnvironmentPackage(unsignedZip)
	if err != nil {
		t.Fatalf("VerifyEnvironmentPackage() unsigned error = %v", err)
	}
	if result.OK || !strings.Contains(strings.Join(result.Errors, "\n"), "签名缺失") {
		t.Fatalf("unsigned result = %+v, want signature error", result)
	}

	windowsPathZip := filepath.Join(tempDir, "windows-path.zip")
	writeTestPackage(t, windowsPathZip, EnvironmentPackageManifest{
		Version: 1, PackageID: "acbh-runtime-base", OS: runtime.GOOS, Architecture: runtime.GOARCH, Signature: "sig",
		Files: []EnvironmentPackageFile{{Path: "base/readme.txt", SHA256: sha256Hex("ok"), Size: 2}},
	}, map[string]string{"base\\readme.txt": "ok"})
	result, err = VerifyEnvironmentPackage(windowsPathZip)
	if err != nil {
		t.Fatalf("VerifyEnvironmentPackage() windows path error = %v", err)
	}
	if !result.OK {
		t.Fatalf("windows-style zip paths should verify, got %+v", result)
	}
}

func writeTestPackage(t *testing.T, target string, manifest EnvironmentPackageManifest, files map[string]string) {
	t.Helper()
	out, err := os.Create(target)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(out)
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	mw, err := zw.Create("acbh-package.json")
	if err != nil {
		t.Fatalf("create manifest: %v", err)
	}
	if _, err := mw.Write(manifestData); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
