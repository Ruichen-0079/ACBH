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
