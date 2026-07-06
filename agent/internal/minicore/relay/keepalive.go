package relay

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type keepaliveClient struct {
	coordinatorURL string
	groupID        string
	hostID         string
	hostToken      string
	generation     int

	mu        sync.Mutex
	connected bool
	connectedAt time.Time
	lastSeenAt  time.Time
	lastError   string
	cancel    context.CancelFunc
}

func (k *keepaliveClient) start(ctx context.Context) {
	k.mu.Lock()
	if k.cancel != nil {
		k.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	k.cancel = cancel
	k.mu.Unlock()

	go k.run(loopCtx)
}

func (k *keepaliveClient) stop() {
	k.mu.Lock()
	cancel := k.cancel
	k.cancel = nil
	k.connected = false
	k.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (k *keepaliveClient) snapshot() (connected bool, connectedAt, lastSeenAt, lastError string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	connected = k.connected
	if !k.connectedAt.IsZero() {
		connectedAt = k.connectedAt.UTC().Format(time.RFC3339)
	}
	if !k.lastSeenAt.IsZero() {
		lastSeenAt = k.lastSeenAt.UTC().Format(time.RFC3339)
	}
	lastError = k.lastError
	return
}

func (k *keepaliveClient) run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := k.connectOnce(ctx)
		if err != nil {
			k.mu.Lock()
			k.connected = false
			k.lastError = err.Error()
			k.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff += time.Second
		}
	}
}

func (k *keepaliveClient) connectOnce(ctx context.Context) error {
	wsURL := fmt.Sprintf("%s/v1/groups/%s/relay/clients/host",
		wsScheme(k.coordinatorURL), k.groupID)
	headers := http.Header{}
	headers.Set("X-ACBH-Host-ID", k.hostID)
	headers.Set("X-ACBH-Host-Token", k.hostToken)
	headers.Set("X-ACBH-Host-Generation", fmt.Sprintf("%d", k.generation))

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return fmt.Errorf("relay keepalive websocket dial failed: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "relay keepalive stopping")

	now := time.Now().UTC()
	k.mu.Lock()
	k.connected = true
	k.connectedAt = now
	k.lastSeenAt = now
	k.lastError = ""
	k.mu.Unlock()

	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()

	errCh := make(chan error, 1)
	go func() {
		for {
			_, _, readErr := conn.Read(ctx)
			if readErr != nil {
				errCh <- readErr
				return
			}
			k.mu.Lock()
			k.lastSeenAt = time.Now().UTC()
			k.mu.Unlock()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			k.mu.Lock()
			k.connected = false
			k.mu.Unlock()
			return ctx.Err()
		case readErr := <-errCh:
			k.mu.Lock()
			k.connected = false
			k.lastError = readErr.Error()
			k.mu.Unlock()
			return readErr
		case <-pingTicker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			pingErr := conn.Ping(pingCtx)
			cancel()
			if pingErr != nil {
				k.mu.Lock()
				k.connected = false
				k.lastError = pingErr.Error()
				k.mu.Unlock()
				return pingErr
			}
			k.mu.Lock()
			k.lastSeenAt = time.Now().UTC()
			k.mu.Unlock()
		}
	}
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