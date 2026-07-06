package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

const (
	defaultBufferSize = 32 * 1024
)

type SessionStats struct {
	SessionID            string
	StartedAt            time.Time
	ClosedAt             time.Time
	RemotePlayerAddress  string
	LocalConnected       bool
	ForwardingStarted    bool
	BytesPlayerToLocal   int64
	BytesLocalToPlayer   int64
	UpstreamCopyStarted  bool
	DownstreamCopyStarted bool
	UpstreamClosed       bool
	DownstreamClosed     bool
	CloseReason          string
	Error                string
}

type HostRelayOptions struct {
	CoordinatorURL  string
	GroupID         string
	SessionID       string
	HostID          string
	HostToken       string
	HostGeneration  int
	TargetAddress   string
	BufferSize      int
	OnClose         func(SessionStats)
}

type HostRelayClient struct {
	opts HostRelayOptions
}

func NewHostRelayClient(opts HostRelayOptions) *HostRelayClient {
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	return &HostRelayClient{opts: opts}
}

func (c *HostRelayClient) Run(ctx context.Context) error {
	stats := SessionStats{
		SessionID:  c.opts.SessionID,
		StartedAt:  time.Now().UTC(),
	}
	defer func() {
		stats.ClosedAt = time.Now().UTC()
		if c.opts.OnClose != nil {
			c.opts.OnClose(stats)
		}
	}()

	wsURL := fmt.Sprintf("%s/v1/groups/%s/relay/tunnel-sessions/%s/host",
		wsScheme(c.opts.CoordinatorURL), c.opts.GroupID, c.opts.SessionID)
	if wsURL == "" {
		err := fmt.Errorf("relay: invalid coordinator URL: %s", c.opts.CoordinatorURL)
		stats.CloseReason = "invalid_coordinator_url"
		stats.Error = err.Error()
		return err
	}

	headers := http.Header{}
	headers.Set("X-ACBH-Host-ID", c.opts.HostID)
	headers.Set("X-ACBH-Host-Token", c.opts.HostToken)
	headers.Set("X-ACBH-Host-Generation", fmt.Sprintf("%d", c.opts.HostGeneration))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wsConn *websocket.Conn
	var tcpConn *net.TCPConn
	var bytesPlayerToLocal int64
	var bytesLocalToPlayer int64

	var closeOnce sync.Once
	closeConns := func() {
		closeOnce.Do(func() {
			if tcpConn != nil {
				tcpConn.Close()
			}
			if wsConn != nil {
				go wsConn.Close(websocket.StatusNormalClosure, "host closing")
			}
		})
	}
	defer closeConns()

	var err error
	wsConn, _, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		stats.CloseReason = "websocket_dial_failed"
		stats.Error = err.Error()
		return fmt.Errorf("relay: websocket dial failed: %w", err)
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()
	var dialer net.Dialer
	rawTCPConn, err := dialer.DialContext(dialCtx, "tcp", c.opts.TargetAddress)
	if err != nil {
		stats.CloseReason = "local_dial_failed"
		stats.Error = err.Error()
		return fmt.Errorf("relay: target dial to %s failed: %w", c.opts.TargetAddress, err)
	}
	tcpConn = rawTCPConn.(*net.TCPConn)
	stats.LocalConnected = true
	stats.ForwardingStarted = true
	stats.UpstreamCopyStarted = true
	stats.DownstreamCopyStarted = true

	go func() {
		<-ctx.Done()
		closeConns()
	}()

	bufSize := c.opts.BufferSize
	errCh := make(chan copyResult, 2)

	go func() {
		errCh <- copyResult{
			direction: "local_to_player",
			err:       forwardTCPToWS(ctx, tcpConn, wsConn, bufSize, &bytesLocalToPlayer),
		}
	}()

	go func() {
		errCh <- copyResult{
			direction: "player_to_local",
			err:       forwardWSToTCP(ctx, wsConn, tcpConn, bufSize, &bytesPlayerToLocal),
		}
	}()

	first := <-errCh
	cancel()
	closeConns()
	second := <-errCh

	stats.BytesPlayerToLocal = atomic.LoadInt64(&bytesPlayerToLocal)
	stats.BytesLocalToPlayer = atomic.LoadInt64(&bytesLocalToPlayer)
	if first.direction == "player_to_local" {
		stats.UpstreamClosed = true
	} else {
		stats.DownstreamClosed = true
	}
	if second.direction == "player_to_local" {
		stats.UpstreamClosed = true
	} else {
		stats.DownstreamClosed = true
	}

	for _, result := range []copyResult{first, second} {
		if !isNormalShutdown(result.err) {
			stats.CloseReason = result.direction + "_copy_error"
			stats.Error = result.err.Error()
			return result.err
		}
	}
	stats.CloseReason = "normal_shutdown"
	return nil
}

type copyResult struct {
	direction string
	err       error
}

func wsScheme(coordinatorURL string) string {
	if len(coordinatorURL) > 5 && coordinatorURL[:5] == "https" {
		return "wss" + coordinatorURL[5:]
	}
	if len(coordinatorURL) > 4 && coordinatorURL[:4] == "http" {
		return "ws" + coordinatorURL[4:]
	}
	return ""
}

func forwardTCPToWS(ctx context.Context, tcpConn *net.TCPConn, wsConn *websocket.Conn, bufSize int, bytesOut *int64) error {
	buf := make([]byte, bufSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := tcpConn.Read(buf)
		if n > 0 {
			atomic.AddInt64(bytesOut, int64(n))
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			writeErr := wsConn.Write(writeCtx, websocket.MessageBinary, buf[:n])
			cancel()
			if writeErr != nil {
				return fmt.Errorf("relay: websocket write error: %w", writeErr)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) {
				return fmt.Errorf("relay: tcp connection closed: %w", readErr)
			}
			if readErr == io.EOF {
				return fmt.Errorf("relay: tcp connection closed by target: %w", readErr)
			}
			return fmt.Errorf("relay: tcp read error: %w", readErr)
		}
	}
}

func forwardWSToTCP(ctx context.Context, wsConn *websocket.Conn, tcpConn *net.TCPConn, bufSize int, bytesOut *int64) error {
	_ = bufSize
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, data, err := wsConn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				return fmt.Errorf("relay: websocket closed: %w", err)
			}
			return fmt.Errorf("relay: websocket read error: %w", err)
		}

		atomic.AddInt64(bytesOut, int64(len(data)))
		for written := 0; written < len(data); {
			n, writeErr := tcpConn.Write(data[written:])
			if writeErr != nil {
				return fmt.Errorf("relay: tcp write error: %w", writeErr)
			}
			written += n
		}
	}
}

func isNormalShutdown(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway
}