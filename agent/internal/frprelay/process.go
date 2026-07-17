package frprelay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"
)

type execLauncher struct{}

type execProcess struct {
	cmd    *exec.Cmd
	lines  chan OutputLine
	wait   chan error
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (execLauncher) Start(_ context.Context, request LaunchRequest) (Process, error) {
	cmd := exec.Command(request.Executable, "-c", request.ConfigPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open frpc stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open frpc stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start frpc: %w", err)
	}

	process := &execProcess{
		cmd:    cmd,
		lines:  make(chan OutputLine, 128),
		wait:   make(chan error, 1),
		stdout: stdout,
		stderr: stderr,
	}
	var readers = make(chan struct{}, 2)
	go process.scan("stdout", stdout, readers)
	go process.scan("stderr", stderr, readers)
	go func() {
		err := cmd.Wait()
		<-readers
		<-readers
		close(process.lines)
		process.wait <- err
		close(process.wait)
	}()
	return process, nil
}

func (p *execProcess) PID() int { return p.cmd.Process.Pid }

func (p *execProcess) Lines() <-chan OutputLine { return p.lines }

func (p *execProcess) Wait() <-chan error { return p.wait }

func (p *execProcess) Stop(ctx context.Context) error {
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.wait:
		return nil
	}
}

func (p *execProcess) Kill() error { return p.cmd.Process.Kill() }

func (p *execProcess) scan(stream string, reader io.Reader, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 4096)
	scanner.Buffer(buffer, 64*1024)
	for scanner.Scan() {
		p.lines <- OutputLine{Stream: stream, Line: scanner.Text(), Time: time.Now().UTC()}
	}
}

func probeTCP(ctx context.Context, address string, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return connection.Close()
}

type osProcessInspector struct{}

func (osProcessInspector) Alive(pid int) bool {
	return processAlive(pid)
}

func (osProcessInspector) Fingerprint(pid int) (string, error) {
	return processFingerprint(pid)
}

func (osProcessInspector) TerminateOwned(pid int, fingerprint string) error {
	current, err := processFingerprint(pid)
	if err != nil {
		return err
	}
	if fingerprint == "" || current != fingerprint {
		return errors.New("managed frpc process identity no longer matches; refusing to terminate")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
