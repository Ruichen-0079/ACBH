package desktop

import (
	"errors"
	"os"
	"path/filepath"
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
