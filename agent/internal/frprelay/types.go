package frprelay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
)

var (
	ErrAlreadyManaged = errors.New("a managed frpc process is already running")
	ErrTerminal       = errors.New("relay failure requires configuration change")
)

type Config struct {
	FRPCPath        string
	RuntimeDir      string
	ServerHost      string
	ServerPort      int
	AccessToken     string
	LocalHost       string
	LocalPort       int
	RemotePort      int
	PublicHost      string
	ProbeTTL        time.Duration
	ProbeInterval   time.Duration
	StopTimeout     time.Duration
	StableResetTime time.Duration
}

func (c Config) Validate() error {
	switch {
	case strings.TrimSpace(c.FRPCPath) == "":
		return errors.New("frpc path is required")
	case strings.TrimSpace(c.RuntimeDir) == "":
		return errors.New("relay runtime directory is required")
	case strings.TrimSpace(c.ServerHost) == "":
		return errors.New("frps host is required")
	case c.ServerPort < 1 || c.ServerPort > 65535:
		return errors.New("frps port must be between 1 and 65535")
	case strings.TrimSpace(c.AccessToken) == "":
		return errors.New("access token is required")
	case strings.TrimSpace(c.LocalHost) == "":
		return errors.New("local host is required")
	case c.LocalPort < 1 || c.LocalPort > 65535:
		return errors.New("local port must be between 1 and 65535")
	case c.RemotePort < 1 || c.RemotePort > 65535:
		return errors.New("remote port must be between 1 and 65535")
	case strings.TrimSpace(c.PublicHost) == "":
		return errors.New("public host is required")
	}
	return nil
}

func (c Config) withDefaults() Config {
	if c.ProbeTTL <= 0 {
		c.ProbeTTL = 30 * time.Second
	}
	if c.ProbeInterval <= 0 {
		c.ProbeInterval = 5 * time.Second
	}
	if c.StopTimeout <= 0 {
		c.StopTimeout = 10 * time.Second
	}
	if c.StableResetTime <= 0 {
		c.StableResetTime = 5 * time.Minute
	}
	return c
}

func (c Config) LocalAddress() string {
	return fmt.Sprintf("%s:%d", c.LocalHost, c.LocalPort)
}

func (c Config) PublicAddress() string {
	return fmt.Sprintf("%s:%d", c.PublicHost, c.RemotePort)
}

type OutputLine struct {
	Stream string
	Line   string
	Time   time.Time
}

type Process interface {
	PID() int
	Lines() <-chan OutputLine
	Wait() <-chan error
	Stop(context.Context) error
	Kill() error
}

type LaunchRequest struct {
	Executable string
	ConfigPath string
}

type Launcher interface {
	Start(context.Context, LaunchRequest) (Process, error)
}

type Prober interface {
	Probe(context.Context, string) error
}

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

type Clock interface {
	Now() time.Time
}

type ProcessInspector interface {
	Alive(pid int) bool
	Fingerprint(pid int) (string, error)
	TerminateOwned(pid int, fingerprint string) error
}

type Dependencies struct {
	Launcher  Launcher
	Prober    Prober
	Sleeper   Sleeper
	Clock     Clock
	Inspector ProcessInspector
}

type Status struct {
	componentstate.Snapshot
	PID             int        `json:"pid,omitempty"`
	ConnectedSince  *time.Time `json:"connected_since,omitempty"`
	LastProbeAt     *time.Time `json:"last_probe_at,omitempty"`
	ReconnectCount  int        `json:"reconnect_count"`
	FRPSConnected   bool       `json:"frps_connected"`
	LocalReachable  bool       `json:"local_reachable"`
	PublicReachable bool       `json:"public_reachable"`
	Terminal        bool       `json:"terminal"`
}

type Event struct {
	Event      string    `json:"event"`
	State      string    `json:"state,omitempty"`
	ReasonCode string    `json:"reason_code,omitempty"`
	Stream     string    `json:"stream,omitempty"`
	Message    string    `json:"message,omitempty"`
	Time       time.Time `json:"time"`
}

type Diagnosis struct {
	Status        Status  `json:"status"`
	Desired       bool    `json:"desired"`
	ConfigHash    string  `json:"config_hash,omitempty"`
	MetadataPath  string  `json:"metadata_path"`
	RecentEvents  []Event `json:"recent_events"`
	AccessToken   string  `json:"access_token"`
	Configuration string  `json:"configuration"`
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type realSleeper struct{}

func (realSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type tcpProber struct {
	timeout time.Duration
}

func (p tcpProber) Probe(ctx context.Context, address string) error {
	return probeTCP(ctx, address, p.timeout)
}

func drainAndClose(reader io.ReadCloser) {
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
}
