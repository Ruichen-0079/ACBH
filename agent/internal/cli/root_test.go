package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestScanCommandWritesManifest(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "world-data")
	writeCLITestFile(t, serverDir, "logs/latest.log", "log")
	output := filepath.Join(t.TempDir(), "manifest.json")

	out, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "world-snapshot",
		"--artifact-id", "snap_000001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", output,
	)
	if err != nil {
		t.Fatalf("scan command error = %v", err)
	}
	if !strings.Contains(out, "Scan complete.") {
		t.Fatalf("scan output = %q", out)
	}

	loaded, err := manifest.LoadFile(output)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if loaded.ArtifactID != "snap_000001" || loaded.Summary.IncludedFiles != 1 || loaded.Summary.IgnoredFiles != 1 {
		t.Fatalf("manifest = %#v", loaded)
	}
}

func TestScanCommandPrintsManifestJSONWithoutOutput(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "server.properties", "motd=ACBH")

	out, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "admin-state",
		"--artifact-id", "admin_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--json",
	)
	if err != nil {
		t.Fatalf("scan command error = %v", err)
	}

	var loaded manifest.Manifest
	if err := json.Unmarshal([]byte(out), &loaded); err != nil {
		t.Fatalf("Unmarshal() error = %v output=%q", err, out)
	}
	if loaded.ArtifactKind != manifest.AdminState {
		t.Fatalf("artifactKind = %q", loaded.ArtifactKind)
	}
}

func TestManifestCommands(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "world-data")
	oldPath := filepath.Join(t.TempDir(), "old.json")
	if _, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "world-snapshot",
		"--artifact-id", "snap_000001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", oldPath,
	); err != nil {
		t.Fatalf("old scan error = %v", err)
	}

	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "changed-world-data")
	newPath := filepath.Join(t.TempDir(), "new.json")
	if _, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "world-snapshot",
		"--artifact-id", "snap_000002",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", newPath,
	); err != nil {
		t.Fatalf("new scan error = %v", err)
	}

	validateOut, err := executeCommand("manifest", "validate", "--file", newPath)
	if err != nil {
		t.Fatalf("validate command error = %v", err)
	}
	if !strings.Contains(validateOut, "Manifest is valid") {
		t.Fatalf("validate output = %q", validateOut)
	}

	diffOut, err := executeCommand("manifest", "diff", "--old", oldPath, "--new", newPath)
	if err != nil {
		t.Fatalf("diff command error = %v", err)
	}
	if !strings.Contains(diffOut, "Modified: 1") {
		t.Fatalf("diff output = %q", diffOut)
	}

	inspectOut, err := executeCommand("manifest", "inspect", "--file", newPath)
	if err != nil {
		t.Fatalf("inspect command error = %v", err)
	}
	if !strings.Contains(inspectOut, "Artifact ID: snap_000002") {
		t.Fatalf("inspect output = %q", inspectOut)
	}
}

func TestRootIncludesArtifactAndServerCommands(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"push", "pull", "safe-sync", "server"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatalf("Find(%q) error = %v", name, err)
		}
		if found == nil || found.Name() != name {
			t.Fatalf("Find(%q) = %#v", name, found)
		}
	}
	for _, name := range []string{"start", "stop", "status"} {
		found, _, err := cmd.Find([]string{"server", name})
		if err != nil {
			t.Fatalf("Find(server %q) error = %v", name, err)
		}
		if found == nil || found.Name() != name {
			t.Fatalf("Find(server %q) = %#v", name, found)
		}
	}
}

func TestResolveServerStartOptionsUsesConfigAndFlags(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", configRoot)
	}
	configPath, err := agentconfig.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	serverDir := t.TempDir()
	if err := agentconfig.Save(configPath, agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "group_abc",
		MemberID:       "member_abc",
		HostID:         "host_abc",
		HostToken:      "secret",
		DisplayName:    "PlayerA",
		DeviceName:     "PlayerA-PC",
		Platform:       runtime.GOOS,
		AgentVersion:   agentconfig.AgentVersion,
		Server: agentconfig.ServerConfig{
			Dir:         serverDir,
			Command:     "java -jar configured.jar nogui",
			LogDir:      "configured-logs",
			StopTimeout: "45s",
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	resolved, err := resolveServerStartOptions(serverStartOptions{
		command:     "java -jar override.jar nogui",
		stopTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("resolveServerStartOptions() error = %v", err)
	}
	if resolved.ServerDir != serverDir {
		t.Fatalf("ServerDir = %q, want %q", resolved.ServerDir, serverDir)
	}
	if resolved.Command != "java -jar override.jar nogui" {
		t.Fatalf("Command = %q", resolved.Command)
	}
	if resolved.LogDir != "configured-logs" {
		t.Fatalf("LogDir = %q", resolved.LogDir)
	}
	if resolved.StopTimeout != 5*time.Second {
		t.Fatalf("StopTimeout = %s", resolved.StopTimeout)
	}
}

func TestSafeSyncUsesEnvironmentPasswordAndGeneratesWorldSnapshot(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "world-data")
	output := filepath.Join(t.TempDir(), "manifest.json")
	address, commands, closeServer := startCLIFakeRCONServer(t, "env-secret", "Saved the game")
	defer closeServer()
	host, port := splitCLIAddress(t, address)
	t.Setenv("ACBH_RCON_PASSWORD", "env-secret")

	out, err := executeCommand(
		"safe-sync",
		"--server-dir", serverDir,
		"--artifact-id", "snap_safe_001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", output,
		"--rcon-host", host,
		"--rcon-port", strconv.Itoa(port),
		"--rcon-timeout", "1s",
	)
	if err != nil {
		t.Fatalf("safe-sync command error = %v", err)
	}
	if command := <-commands; command != "save-all flush" {
		t.Fatalf("RCON command = %q", command)
	}
	if !strings.Contains(out, "RCON save-all flush succeeded.") || !strings.Contains(out, "Safe sync complete.") {
		t.Fatalf("safe-sync output = %q", out)
	}
	if strings.Contains(out, "env-secret") {
		t.Fatalf("safe-sync output exposed password: %q", out)
	}

	loaded, err := manifest.LoadFile(output)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if loaded.ArtifactKind != manifest.WorldSnapshot || loaded.ArtifactID != "snap_safe_001" {
		t.Fatalf("manifest = %#v", loaded)
	}
}

func TestSafeSyncRCONFailurePreventsScan(t *testing.T) {
	output := filepath.Join(t.TempDir(), "manifest.json")
	address, _, closeServer := startCLIFakeRCONServer(t, "correct", "Saved the game")
	defer closeServer()
	host, port := splitCLIAddress(t, address)

	_, err := executeCommand(
		"safe-sync",
		"--server-dir", filepath.Join(t.TempDir(), "missing-server"),
		"--artifact-id", "snap_safe_001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", output,
		"--rcon-host", host,
		"--rcon-port", strconv.Itoa(port),
		"--rcon-password", "wrong",
		"--rcon-timeout", "1s",
	)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("safe-sync error = %v, want authentication failure", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("manifest exists after RCON failure, stat error = %v", statErr)
	}
}

func TestSafeSyncRejectsFailureResponseBeforeScan(t *testing.T) {
	output := filepath.Join(t.TempDir(), "manifest.json")
	address, _, closeServer := startCLIFakeRCONServer(t, "secret", "Error: rejected secret")
	defer closeServer()
	host, port := splitCLIAddress(t, address)

	_, err := executeCommand(
		"safe-sync",
		"--server-dir", filepath.Join(t.TempDir(), "missing-server"),
		"--artifact-id", "snap_safe_001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", output,
		"--rcon-host", host,
		"--rcon-port", strconv.Itoa(port),
		"--rcon-password", "secret",
		"--rcon-timeout", "1s",
	)
	if err == nil || !strings.Contains(err.Error(), "returned failure") {
		t.Fatalf("safe-sync error = %v, want command failure", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("safe-sync error exposed password: %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("manifest exists after RCON command failure, stat error = %v", statErr)
	}
}

func TestSafeSyncRequiresPassword(t *testing.T) {
	t.Setenv("ACBH_RCON_PASSWORD", "")
	_, err := executeCommand(
		"safe-sync",
		"--server-dir", t.TempDir(),
		"--artifact-id", "snap_safe_001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", filepath.Join(t.TempDir(), "manifest.json"),
	)
	if err == nil || !strings.Contains(err.Error(), "ACBH_RCON_PASSWORD") {
		t.Fatalf("safe-sync error = %v, want password guidance", err)
	}
}

func TestSafeSyncRejectsNonWorldSnapshot(t *testing.T) {
	_, err := executeCommand(
		"safe-sync",
		"--server-dir", t.TempDir(),
		"--artifact-kind", "server-pack",
		"--artifact-id", "pack_001",
		"--server-pack-version", "pack_001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", filepath.Join(t.TempDir(), "manifest.json"),
		"--rcon-password", "secret",
	)
	if err == nil || !strings.Contains(err.Error(), "only supports") {
		t.Fatalf("safe-sync error = %v, want artifact kind rejection", err)
	}
}

func executeCommand(args ...string) (string, error) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeCLITestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func startCLIFakeRCONServer(t *testing.T, password string, response string) (string, <-chan string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	commands := make(chan string, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		authID, authType, authPayload, readErr := readCLIRCONPacket(conn)
		if readErr != nil {
			return
		}
		if authType != 3 || authPayload != password {
			authID = -1
		}
		if writeCLIRCONPacket(conn, authID, 2, "") != nil || authID == -1 {
			return
		}

		commandID, commandType, command, readErr := readCLIRCONPacket(conn)
		if readErr != nil || commandType != 2 {
			return
		}
		commands <- command
		_ = writeCLIRCONPacket(conn, commandID, 0, response)
	}()

	return listener.Addr().String(), commands, func() {
		_ = listener.Close()
		<-done
	}
}

func readCLIRCONPacket(reader io.Reader) (int32, int32, string, error) {
	var sizeBytes [4]byte
	if _, err := io.ReadFull(reader, sizeBytes[:]); err != nil {
		return 0, 0, "", err
	}
	size := int(binary.LittleEndian.Uint32(sizeBytes[:]))
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return 0, 0, "", err
	}
	return int32(binary.LittleEndian.Uint32(data[0:4])),
		int32(binary.LittleEndian.Uint32(data[4:8])),
		string(data[8 : size-2]),
		nil
}

func writeCLIRCONPacket(writer io.Writer, requestID int32, packetType int32, payload string) error {
	content := []byte(payload)
	size := 4 + 4 + len(content) + 2
	data := make([]byte, 4+size)
	binary.LittleEndian.PutUint32(data[0:4], uint32(size))
	binary.LittleEndian.PutUint32(data[4:8], uint32(requestID))
	binary.LittleEndian.PutUint32(data[8:12], uint32(packetType))
	copy(data[12:], content)
	_, err := writer.Write(data)
	return err
}

func splitCLIAddress(t *testing.T, address string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}
	return host, port
}
