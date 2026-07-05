package listener

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

type fakeInspector struct {
	listeners        []Listener
	listenerErr      error
	processes        map[int]ProcessInfo
	processErr       error
	tcpCalls         int
	processInfoCalls int
}

func (f *fakeInspector) TCPListeners(ctx context.Context, host string, port int) ([]Listener, error) {
	f.tcpCalls++
	return f.listeners, f.listenerErr
}

func (f *fakeInspector) ProcessInfo(ctx context.Context, pid int) (ProcessInfo, error) {
	f.processInfoCalls++
	if f.processErr != nil {
		return ProcessInfo{PID: pid}, f.processErr
	}
	return f.processes[pid], nil
}

func testConfig() coreconfig.Config {
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.Identity = coreconfig.Identity{GroupID: "grp", HostID: "host", HostToken: "ht", MemberID: "mem", DisplayName: "host", DeviceName: "pc", Platform: "windows"}
	cfg.Server.Dir = `C:\server`
	return cfg
}

func TestNoListenerIsNotFatal(t *testing.T) {
	cfg := testConfig()
	inspector := &fakeInspector{}
	status, err := Service{Inspector: inspector}.Status(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.OK || status.Listening {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Warnings) == 0 || status.Warnings[0].Code != coreerrors.LocalPortNotListening {
		t.Fatalf("warnings = %#v", status.Warnings)
	}
}

func TestInvalidPortReturnsConfigInvalid(t *testing.T) {
	cfg := testConfig()
	cfg.Listener.LocalPort = 70000
	_, err := Service{Inspector: &fakeInspector{}}.Status(context.Background(), cfg)
	if err == nil || err.ErrorCode != coreerrors.ConfigInvalid {
		t.Fatalf("Status() error = %v, want config_invalid", err)
	}
}

func TestMockListenerReturnsProcessInfo(t *testing.T) {
	cfg := testConfig()
	inspector := &fakeInspector{
		listeners: []Listener{{LocalAddress: "127.0.0.1", LocalPort: 25565, PID: 42}},
		processes: map[int]ProcessInfo{42: {
			PID: 42, ProcessName: "java.exe", ExecutablePath: `C:\Java\bin\java.exe`, CommandLine: `java -jar server.jar --nogui C:\server`,
		}},
	}
	status, err := Service{Inspector: inspector}.Status(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Listening || len(status.Listeners) != 1 {
		t.Fatalf("status = %#v", status)
	}
	got := status.Listeners[0]
	if got.ProcessName != "java.exe" || !got.ServerDirMatched || got.Confidence != "high" {
		t.Fatalf("listener = %#v", got)
	}
}

func TestProcessInspectionUnavailableIsWarning(t *testing.T) {
	cfg := testConfig()
	inspector := &fakeInspector{
		listeners:  []Listener{{LocalAddress: "127.0.0.1", LocalPort: 25565, PID: 42}},
		processErr: errors.New("access denied"),
	}
	status, err := Service{Inspector: inspector}.Status(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Listening {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Warnings) == 0 || status.Warnings[0].Code != coreerrors.ProcessInspectionLimited {
		t.Fatalf("warnings = %#v", status.Warnings)
	}
}

func TestListenerDoesNotAttemptLifecycleActions(t *testing.T) {
	cfg := testConfig()
	inspector := &fakeInspector{}
	_, err := Service{Inspector: inspector}.Status(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if inspector.tcpCalls != 1 || inspector.processInfoCalls != 0 {
		t.Fatalf("unexpected inspector calls: %#v", inspector)
	}
}

func TestDefaultInspectorDetectsMockListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := testConfig()
	cfg.Listener.LocalPort = port
	status, statusErr := Service{}.Status(context.Background(), cfg)
	if statusErr != nil {
		t.Fatalf("Status() error = %v", statusErr)
	}
	if !status.Listening {
		t.Fatalf("status = %#v, want listening true", status)
	}
}
