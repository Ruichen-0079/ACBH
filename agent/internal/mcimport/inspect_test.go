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

func TestInspectMCSL2LikeFabricLauncher(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fabric-server-mc.1.20.1-loader.0.16.7-launcher.1.0.1.jar"), "")
	mkdir(t, filepath.Join(dir, "mods"))
	mkdir(t, filepath.Join(dir, "config"))
	mkdir(t, filepath.Join(dir, ".fabric"))
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !report.InspectionOK || !report.LaunchReady {
		t.Fatalf("inspectionOk=%v launchReady=%v, want both true", report.InspectionOK, report.LaunchReady)
	}
	if report.ServerType != Fabric {
		t.Fatalf("ServerType = %s, want Fabric", report.ServerType)
	}
	if report.LaunchProfile.Kind != "jar" || report.LaunchProfile.JarPath == "" {
		t.Fatalf("LaunchProfile = %+v, want jar profile", report.LaunchProfile)
	}
}

func TestInspectStartBatAsCustomScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "start.bat"), "java -jar server.jar nogui")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.ServerType != CustomScript {
		t.Fatalf("ServerType = %s, want CustomScript", report.ServerType)
	}
	if !report.LaunchReady || report.LaunchProfile.ScriptPath != "start.bat" {
		t.Fatalf("LaunchProfile = %+v launchReady=%v", report.LaunchProfile, report.LaunchReady)
	}
}

func TestInspectRunPs1AsPowerShellCustomScript(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "测试 服务端")
	mkdir(t, dir)
	writeFile(t, filepath.Join(dir, "run.ps1"), "java -Xmx2G -jar server.jar nogui\n")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")
	mkdir(t, filepath.Join(dir, "mods"))
	mkdir(t, filepath.Join(dir, "world"))

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.ServerType != CustomScript || !report.LaunchReady {
		t.Fatalf("ServerType=%s launchReady=%v, want CustomScript true", report.ServerType, report.LaunchReady)
	}
	if report.LaunchEntry != "run.ps1" {
		t.Fatalf("LaunchEntry = %q, want run.ps1", report.LaunchEntry)
	}
	if report.LaunchProfile.Kind != "script" || report.LaunchProfile.ScriptType != "powershell" || report.LaunchProfile.ScriptPath != "run.ps1" {
		t.Fatalf("LaunchProfile = %+v", report.LaunchProfile)
	}
	if report.LaunchProfile.Shell != "powershell.exe" {
		t.Fatalf("Shell = %q, want powershell.exe", report.LaunchProfile.Shell)
	}
	if len(report.LaunchProfile.ShellArguments) == 0 || report.LaunchProfile.ShellArguments[len(report.LaunchProfile.ShellArguments)-1] != "run.ps1" {
		t.Fatalf("ShellArguments = %#v", report.LaunchProfile.ShellArguments)
	}
	if report.RequiredJavaVersion != "" {
		t.Fatalf("RequiredJavaVersion = %q, want empty for CustomScript", report.RequiredJavaVersion)
	}
}

func TestInspectRunPs1CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Run.PS1"), "Write-Host ok\n")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.LaunchEntry != "Run.PS1" || report.LaunchProfile.ScriptType != "powershell" {
		t.Fatalf("LaunchEntry=%q LaunchProfile=%+v", report.LaunchEntry, report.LaunchProfile)
	}
}

func TestInspectScriptCandidatePriorityAndManualSelectPowerShell(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "run.bat"), "java -jar server.jar nogui\n")
	writeFile(t, filepath.Join(dir, "run.ps1"), "java -jar server.jar nogui\n")
	writeFile(t, filepath.Join(dir, "start.ps1"), "java -jar server.jar nogui\n")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.LaunchEntry != "run.bat" {
		t.Fatalf("LaunchEntry = %q, want run.bat", report.LaunchEntry)
	}
	if len(report.Candidates.Scripts) != 3 {
		t.Fatalf("script candidates = %#v", report.Candidates.Scripts)
	}
	selected, err := SelectLaunchProfile(dir, "run.ps1")
	if err != nil {
		t.Fatalf("SelectLaunchProfile() error = %v", err)
	}
	if selected.LaunchProfile.ScriptPath != "run.ps1" || selected.LaunchProfile.ScriptType != "powershell" {
		t.Fatalf("selected profile = %+v", selected.LaunchProfile)
	}
}

func TestSelectLaunchProfileRejectsOutsideScript(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "server")
	mkdir(t, dir)
	writeFile(t, filepath.Join(root, "outside.ps1"), "Write-Host outside\n")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	if _, err := SelectLaunchProfile(dir, `..\outside.ps1`); err == nil {
		t.Fatal("SelectLaunchProfile() error = nil, want outside path rejection")
	}
}

func TestInspectForgeAndNeoForgeMarkers(t *testing.T) {
	cases := []struct {
		name string
		path string
		jar  string
		want ServerType
	}{
		{"forge", filepath.Join("libraries", "net", "minecraftforge"), "forge-1.20.1.jar", Forge},
		{"neoforge", filepath.Join("libraries", "net", "neoforged"), "neoforge-21.1.1.jar", NeoForge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mkdir(t, filepath.Join(dir, tc.path))
			writeFile(t, filepath.Join(dir, "user_jvm_args.txt"), "-Xmx4G\n")
			writeFile(t, filepath.Join(dir, tc.jar), "")
			writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
			writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

			report, err := Inspect(dir)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}
			if report.ServerType != tc.want {
				t.Fatalf("ServerType = %s, want %s", report.ServerType, tc.want)
			}
			if !report.LaunchReady {
				t.Fatal("LaunchReady = false, want true")
			}
		})
	}
}

func TestInspectPaperPurpurAndGenericJar(t *testing.T) {
	cases := []struct {
		name string
		jar  string
		want ServerType
	}{
		{"paper", "paper-1.20.1.jar", Paper},
		{"purpur", "purpur-1.20.1.jar", Purpur},
		{"generic", "custom-server.jar", GenericJar},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, tc.jar), "")
			writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
			writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")
			report, err := Inspect(dir)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}
			if report.ServerType != tc.want {
				t.Fatalf("ServerType = %s, want %s", report.ServerType, tc.want)
			}
			if !report.LaunchReady {
				t.Fatal("LaunchReady = false, want true")
			}
		})
	}
}

func TestInspectMultipleJarsRequireSelection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "server-a.jar"), "")
	writeFile(t, filepath.Join(dir, "server-b.jar"), "")
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.LaunchReady {
		t.Fatal("LaunchReady = true, want false")
	}
	if report.LaunchProfile.Kind != "unresolved" {
		t.Fatalf("LaunchProfile.Kind = %q, want unresolved", report.LaunchProfile.Kind)
	}
	if len(report.Candidates.Jars) != 2 {
		t.Fatalf("jar candidates = %d, want 2", len(report.Candidates.Jars))
	}
	if len(report.BlockingReasons) == 0 {
		t.Fatal("BlockingReasons is empty")
	}
}

func TestInspectNoLaunchEntryExplainsBlocker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\n")
	writeFile(t, filepath.Join(dir, "eula.txt"), "eula=true\n")

	report, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !report.InspectionOK || report.LaunchReady {
		t.Fatalf("inspectionOk=%v launchReady=%v, want inspection ok and launch not ready", report.InspectionOK, report.LaunchReady)
	}
	if len(report.BlockingReasons) == 0 {
		t.Fatal("BlockingReasons is empty")
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

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
