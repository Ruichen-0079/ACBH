package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestControlServeDefaultsToLoopbackAndRequiresRemoteOptIn(t *testing.T) {
	cmd := newControlServeCmd()

	listen, err := cmd.Flags().GetString("listen")
	if err != nil {
		t.Fatal(err)
	}
	if listen != "127.0.0.1:6122" {
		t.Fatalf("listen = %q, want loopback default", listen)
	}

	allowRemote, err := cmd.Flags().GetBool("allow-remote-control")
	if err != nil {
		t.Fatal(err)
	}
	if allowRemote {
		t.Fatal("remote control must be disabled by default")
	}
}

func TestLoadOrCreateControlTokenStoresSecretOutsideOutput(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", configRoot)
	}

	token, tokenPath, err := loadOrCreateControlToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected generated token")
	}

	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tokenPath) != configDir {
		t.Fatalf("token path = %q, want directory %q", tokenPath, configDir)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != token {
		t.Fatal("stored token does not match generated token")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(tokenPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("token permissions = %o, want 600", info.Mode().Perm())
		}
	}

	masked := maskControlToken(token)
	if strings.Contains(masked, token) {
		t.Fatal("masked output contains the full token")
	}
}
