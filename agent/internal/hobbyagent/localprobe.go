package hobbyagent

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
)

type LocalServerStatus struct {
	componentstate.Snapshot
}

type LocalProbe interface {
	Status(context.Context, int) LocalServerStatus
}

type TCPLocalProbe struct {
	Timeout time.Duration
}

func timePointer(value time.Time) *time.Time { return &value }

func (p TCPLocalProbe) Status(parent context.Context, port int) LocalServerStatus {
	now := time.Now().UTC()
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		snapshot := componentstate.NewSnapshot(componentstate.Offline, now, "local_port_unreachable", "未检测到本地服务器")
		snapshot.TechnicalMessage = err.Error()
		return LocalServerStatus{Snapshot: snapshot}
	}
	_ = connection.Close()
	snapshot := componentstate.NewSnapshot(componentstate.Ready, now, "local_port_ready", "已检测到本地服务器")
	snapshot.LastOKAt = timePointer(now)
	return LocalServerStatus{Snapshot: snapshot}
}
