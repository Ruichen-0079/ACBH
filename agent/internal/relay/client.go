package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

const (
	defaultBufferSize = 32 * 1024
)

type HostRelayOptions struct {
	CoordinatorURL string
	GroupID        string
	SessionID      string
	HostID         string
	HostToken      string
	HostGeneration int
	TargetAddress  string
	BufferSize     int
	Diagnostics    func(HostRelayDiagnostics)
}

type HostRelayClient struct {
	opts HostRelayOptions
}

type HostRelayDiagnostics struct {
	ConnectionID       string
	SessionID          string
	HostConnected      bool
	LocalDialAttempted bool
	LocalDialSucceeded bool
	LocalEndpoint      string
	BytesHostToLocal   int64
	BytesLocalToHost   int64
	CloseReason        string
	LastError          string
	OpenedAt           time.Time
	ClosedAt           time.Time
}

func NewHostRelayClient(opts HostRelayOptions) *HostRelayClient {
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	return &HostRelayClient{opts: opts}
}

func (c *HostRelayClient) Run(ctx context.Context) (runErr error) {
	diag := HostRelayDiagnostics{
		ConnectionID:  c.opts.SessionID,
		SessionID:     c.opts.SessionID,
		LocalEndpoint: c.opts.TargetAddress,
		OpenedAt:      time.Now().UTC(),
	}
	var diagMu sync.Mutex
	report := func(update func(*HostRelayDiagnostics)) {
		diagMu.Lock()
		if update != nil {
			update(&diag)
		}
		snapshot := diag
		diagMu.Unlock()
		if c.opts.Diagnostics != nil {
			c.opts.Diagnostics(snapshot)
		}
	}
	report(nil)
	defer func() {
		report(func(d *HostRelayDiagnostics) {
			if !d.ClosedAt.IsZero() {
				return
			}
			d.ClosedAt = time.Now().UTC()
			if runErr != nil {
				d.LastError = runErr.Error()
				if d.CloseReason == "" {
					d.CloseReason = runErr.Error()
				}
			} else if d.CloseReason == "" {
				d.CloseReason = "normal"
			}
		})
	}()

	wsURL := fmt.Sprintf("%s/v1/groups/%s/relay/tunnel-sessions/%s/host",
		wsScheme(c.opts.CoordinatorURL), c.opts.GroupID, c.opts.SessionID)
	if wsURL == "" {
		return fmt.Errorf("relay: invalid coordinator URL: %s", c.opts.CoordinatorURL)
	}

	headers := http.Header{}
	headers.Set("X-ACBH-Host-ID", c.opts.HostID)
	headers.Set("X-ACBH-Host-Token", c.opts.HostToken)
	headers.Set("X-ACBH-Host-Generation", fmt.Sprintf("%d", c.opts.HostGeneration))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wsConn *websocket.Conn
	var tcpConn *net.TCPConn

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
		return fmt.Errorf("relay: websocket dial failed: %w", err)
	}
	report(func(d *HostRelayDiagnostics) {
		d.HostConnected = true
	})

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()
	var dialer net.Dialer
	report(func(d *HostRelayDiagnostics) {
		d.LocalDialAttempted = true
	})
	rawTCPConn, err := dialer.DialContext(dialCtx, "tcp", c.opts.TargetAddress)
	if err != nil {
		return fmt.Errorf("relay: target dial to %s failed: %w", c.opts.TargetAddress, err)
	}
	tcpConn = rawTCPConn.(*net.TCPConn)
	report(func(d *HostRelayDiagnostics) {
		d.LocalDialSucceeded = true
	})

	go func() {
		<-ctx.Done()
		closeConns()
	}()

	bufSize := c.opts.BufferSize

	errCh := make(chan error, 2)

	go func() {
		errCh <- forwardTCPToWS(ctx, tcpConn, wsConn, bufSize, func(n int) {
			report(func(d *HostRelayDiagnostics) {
				d.BytesLocalToHost += int64(n)
			})
		})
	}()

	go func() {
		errCh <- forwardWSToTCP(ctx, wsConn, tcpConn, bufSize, func(n int) {
			report(func(d *HostRelayDiagnostics) {
				d.BytesHostToLocal += int64(n)
			})
		})
	}()

	firstErr := <-errCh
	cancel()
	closeConns()
	secondErr := <-errCh

	for _, err := range []error{firstErr, secondErr} {
		if !isNormalShutdown(err) {
			return err
		}
	}
	return nil
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

func forwardTCPToWS(ctx context.Context, tcpConn *net.TCPConn, wsConn *websocket.Conn, bufSize int, onBytes func(int)) error {
	buf := make([]byte, bufSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := tcpConn.Read(buf)
		if n > 0 {
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			writeErr := wsConn.Write(writeCtx, websocket.MessageBinary, buf[:n])
			cancel()
			if writeErr != nil {
				return fmt.Errorf("relay: websocket write error: %w", writeErr)
			}
			if onBytes != nil {
				onBytes(n)
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

func forwardWSToTCP(ctx context.Context, wsConn *websocket.Conn, tcpConn *net.TCPConn, bufSize int, onBytes func(int)) error {
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

		for written := 0; written < len(data); {
			n, writeErr := tcpConn.Write(data[written:])
			if writeErr != nil {
				return fmt.Errorf("relay: tcp write error: %w", writeErr)
			}
			written += n
		}
		if onBytes != nil && len(data) > 0 {
			onBytes(len(data))
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
