package mcimport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectDetectsServerTypes(t *testing.T) {
	cases := []struct {
		name string
		jar  string
		want ServerType
	}{
		{"fabric", "fabric-server-launch.jar", Fabric},
		{"paper", "paper.jar", Paper},
		{"purpur", "purpur-1.20.1.jar", Purpur},
		{"forge", "forge-1.20.1.jar", Forge},
		{"neoforge", "neoforge-21.1.1.jar", NeoForge},
		{"cleanroom", "cleanroom-0.2.4.jar", Cleanroom},
		{"velocity", "velocity.jar", Velocity},
		{"vanilla", "server.jar", Vanilla},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, tc.jar), "")
			writeFile(t, filepath.Join(dir, "server.properties"), "enable-rcon=true\nrcon.port=25575\nrcon.password=secret\n")
			writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

			report, err := Inspect(dir)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}
			if report.ServerType != tc.want {
				t.Fatalf("ServerType = %s, want %s", report.ServerType, tc.want)
			}
			if report.SuggestedCommand == "" {
				t.Fatal("SuggestedCommand is empty")
			}
		})
	}
}

func TestInspectPrefersStartScriptAndIgnoresInstallerJar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "run.bat"), "java -jar forge.jar nogui")
	writeFile(t, filepath.Join(dir, "forge-installer.jar"), "")
	writeFile(t, filepath.Join(dir, "server.jar"), "")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25566\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.LaunchEntry != "run.bat" {
		t.Fatalf("LaunchEntry = %q, want run.bat", report.LaunchEntry)
	}
	if report.ServerPort != "25566" {
		t.Fatalf("ServerPort = %q, want 25566", report.ServerPort)
	}
	for _, candidate := range report.LaunchCandidates {
		if candidate == "forge-installer.jar" {
			t.Fatal("installer jar should not be a launch candidate")
		}
	}
}

func TestInspectReportsRCONActionableChineseMessage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "server.jar"), "")
	writeFile(t, filepath.Join(dir, "server.properties"), "enable-rcon=false\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.RCON.Enabled {
		t.Fatal("RCON.Enabled = true, want false")
	}
	if report.RCON.ChineseMessage == "" {
		t.Fatal("RCON.ChineseMessage is empty")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
