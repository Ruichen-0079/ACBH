package hobbyagent

import (
	"context"
	"net"
	"path/filepath"
	"testing"
)

func TestManagedMinecraftRefusesUnknownProcessOnConfiguredPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	directory := t.TempDir()
	managed := ManagedMinecraft{Executable: "must-not-run", RuntimeDir: filepath.Join(directory, "runtime"), LogDir: filepath.Join(directory, "logs")}
	err = managed.Start(context.Background(), ImportedServer{ServerDir: directory, JavaPath: "java", JarPath: "server.jar"}, port)
	if ErrorCode(err) != CodeLocalPortInUse {
		t.Fatalf("expected %s, got %v", CodeLocalPortInUse, err)
	}
}
