package playerproxy

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

const defaultBufferSize = 32 * 1024

type PlayerProxyOptions struct {
	CoordinatorURL string
	GroupID        string
	SessionID      string
	PlayerID       string
	PlayerToken    string
	ListenAddress  string
	BufferSize     int
}

type PlayerProxy struct {
	opts PlayerProxyOptions
}

func NewPlayerProxy(opts PlayerProxyOptions) *PlayerProxy {
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	return &PlayerProxy{opts: opts}
}

func (p *PlayerProxy) ListenAddress() string {
	return p.opts.ListenAddress
}

func (p *PlayerProxy) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.opts.ListenAddress)
	if err != nil {
		return fmt.Errorf("relay: listen on %s failed: %w", p.opts.ListenAddress, err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		localConn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("relay: accept error: %w", err)
			}
		}

		if err := p.serveOne(ctx, localConn); err != nil {
			if isNormalShutdown(err) {
				continue
			}
			localConn.Close()
			return err
		}
	}
}

func (p *PlayerProxy) serveOne(ctx context.Context, localConn net.Conn) error {
	wsURL := fmt.Sprintf("%s/v1/groups/%s/relay/tunnel-sessions/%s/player",
		wsScheme(p.opts.CoordinatorURL), p.opts.GroupID, p.opts.SessionID)
	if wsURL == "" {
		return fmt.Errorf("relay: invalid coordinator URL: %s", p.opts.CoordinatorURL)
	}

	headers := http.Header{}
	headers.Set("X-ACBH-Player-ID", p.opts.PlayerID)
	headers.Set("X-ACBH-Player-Token", p.opts.PlayerToken)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wsConn *websocket.Conn
	tcpConn := localConn.(*net.TCPConn)

	var closeOnce sync.Once
	closeConns := func() {
		closeOnce.Do(func() {
			tcpConn.Close()
			if wsConn != nil {
				go wsConn.Close(websocket.StatusNormalClosure, "player closing")
			}
		})
	}
	defer closeConns()

	var err error
	wsConn, _, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return fmt.Errorf("relay: player websocket dial failed: %w", err)
	}

	go func() {
		<-ctx.Done()
		closeConns()
	}()

	bufSize := p.opts.BufferSize

	errCh := make(chan error, 2)

	go func() {
		errCh <- forwardTCPToWS(ctx, tcpConn, wsConn, bufSize)
	}()

	go func() {
		errCh <- forwardWSToTCP(ctx, wsConn, tcpConn, bufSize)
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

func forwardTCPToWS(ctx context.Context, tcpConn *net.TCPConn, wsConn *websocket.Conn, bufSize int) error {
	buf := make([]byte, bufSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := tcpConn.Read(buf)
		if n > 0 {
			writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
			writeErr := wsConn.Write(writeCtx, websocket.MessageBinary, buf[:n])
			writeCancel()
			if writeErr != nil {
				return fmt.Errorf("relay: websocket write error: %w", writeErr)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) {
				return fmt.Errorf("relay: tcp connection closed: %w", readErr)
			}
			if readErr == io.EOF {
				return fmt.Errorf("relay: tcp connection closed by client: %w", readErr)
			}
			return fmt.Errorf("relay: tcp read error: %w", readErr)
		}
	}
}

func forwardWSToTCP(ctx context.Context, wsConn *websocket.Conn, tcpConn *net.TCPConn, bufSize int) error {
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
