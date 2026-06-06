package rcon

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPacketEncodeDecode(t *testing.T) {
	want := packet{
		RequestID: 42,
		Type:      packetTypeCommand,
		Payload:   "save-all flush",
	}
	encoded, err := encodePacket(want)
	if err != nil {
		t.Fatalf("encodePacket() error = %v", err)
	}
	got, err := decodePacket(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decodePacket() error = %v", err)
	}
	if got != want {
		t.Fatalf("decoded packet = %#v, want %#v", got, want)
	}
}

func TestExecuteAuthenticatesAndSendsCommand(t *testing.T) {
	address, commands, closeServer := startFakeServer(t, fakeServerOptions{
		Password: "secret",
		Response: "Saved the game",
	})
	defer closeServer()

	host, port := splitAddress(t, address)
	response, err := Execute(context.Background(), Config{
		Host:     host,
		Port:     port,
		Password: "secret",
		Timeout:  time.Second,
	}, "save-all flush")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if response != "Saved the game" {
		t.Fatalf("response = %q", response)
	}
	if command := <-commands; command != "save-all flush" {
		t.Fatalf("command = %q", command)
	}
}

func TestExecuteRejectsAuthenticationFailure(t *testing.T) {
	address, _, closeServer := startFakeServer(t, fakeServerOptions{
		Password: "correct",
		Response: "Saved the game",
	})
	defer closeServer()

	host, port := splitAddress(t, address)
	_, err := Execute(context.Background(), Config{
		Host:     host,
		Port:     port,
		Password: "wrong",
		Timeout:  time.Second,
	}, "save-all flush")
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("Execute() error = %v, want authentication failure", err)
	}
}

func TestExecuteTimesOutWaitingForCommand(t *testing.T) {
	address, _, closeServer := startFakeServer(t, fakeServerOptions{
		Password:     "secret",
		Response:     "Saved the game",
		CommandDelay: 200 * time.Millisecond,
	})
	defer closeServer()

	host, port := splitAddress(t, address)
	_, err := Execute(context.Background(), Config{
		Host:     host,
		Port:     port,
		Password: "secret",
		Timeout:  25 * time.Millisecond,
	}, "save-all flush")
	if err == nil {
		t.Fatal("Execute() succeeded, want timeout")
	}
	var netErr net.Error
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "i/o timeout") && !(errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("Execute() error = %v, want timeout", err)
	}
}

type fakeServerOptions struct {
	Password     string
	Response     string
	CommandDelay time.Duration
}

func startFakeServer(t *testing.T, options fakeServerOptions) (string, <-chan string, func()) {
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

		auth, readErr := decodePacket(conn)
		if readErr != nil {
			return
		}
		authID := auth.RequestID
		if auth.Type != packetTypeAuth || auth.Payload != options.Password {
			authID = -1
		}
		authResponse, _ := encodePacket(packet{
			RequestID: authID,
			Type:      packetTypeCommand,
		})
		_, _ = conn.Write(authResponse)
		if authID == -1 {
			return
		}

		command, readErr := decodePacket(conn)
		if readErr != nil {
			return
		}
		commands <- command.Payload
		if options.CommandDelay > 0 {
			time.Sleep(options.CommandDelay)
		}
		response, _ := encodePacket(packet{
			RequestID: command.RequestID,
			Type:      packetTypeResponse,
			Payload:   options.Response,
		})
		_, _ = conn.Write(response)
	}()

	return listener.Addr().String(), commands, func() {
		_ = listener.Close()
		<-done
	}
}

func splitAddress(t *testing.T, address string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("LookupPort() error = %v", err)
	}
	return host, port
}
