package cli

import (
	"strings"
	"testing"
)

func TestRelayHostRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing session-id",
			args: []string{"relay", "host", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--host-id", "h", "--host-token", "t", "--host-generation", "3", "--target-address", "127.0.0.1:25565"},
			want: "session-id",
		},
		{
			name: "missing target-address",
			args: []string{"relay", "host", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--host-id", "h", "--host-token", "t", "--host-generation", "3", "--session-id", "s"},
			want: "target-address",
		},
		{
			name: "missing host-generation",
			args: []string{"relay", "host", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--host-id", "h", "--host-token", "t", "--session-id", "s", "--target-address", "127.0.0.1:25565"},
			want: "host-generation",
		},
		{
			name: "negative host-generation",
			args: []string{"relay", "host", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--host-id", "h", "--host-token", "t", "--host-generation", "-1", "--session-id", "s", "--target-address", "127.0.0.1:25565"},
			want: "host-generation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeCommand(tt.args...)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestRelayHostInvalidTargetAddress(t *testing.T) {
	_, err := executeCommand(
		"relay", "host",
		"--coordinator-url", "http://localhost:8080",
		"--group-id", "g",
		"--host-id", "h",
		"--host-token", "t",
		"--host-generation", "3",
		"--session-id", "s",
		"--target-address", "not-a-valid-address",
	)
	if err == nil {
		t.Fatal("expected error for invalid target address")
	}
}

func TestRelayHostNoSecretsInErrors(t *testing.T) {
	_, err := executeCommand(
		"relay", "host",
		"--coordinator-url", "http://localhost:8080",
		"--group-id", "g",
		"--host-id", "h",
		"--host-token", "secret_token_value",
		"--host-generation", "1",
		"--session-id", "s",
		"--target-address", "127.0.0.1:1",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret_token_value") {
		t.Error("error message must not contain the host token")
	}
}

func TestRelayCommandStructure(t *testing.T) {
	cmd := newRelayCmd()
	if cmd.Use != "relay" {
		t.Errorf("expected Use=relay, got %s", cmd.Use)
	}
	if len(cmd.Commands()) != 2 {
		t.Fatalf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
	hostCmd := cmd.Commands()[0]
	if hostCmd.Use != "host" {
		t.Errorf("expected host subcommand, got %s", hostCmd.Use)
	}
	playerCmd := cmd.Commands()[1]
	if playerCmd.Use != "player" {
		t.Errorf("expected player subcommand, got %s", playerCmd.Use)
	}
}

func TestRelayPlayerRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing session-id",
			args: []string{"relay", "player", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--player-id", "p", "--player-token", "t", "--listen-address", "127.0.0.1:0"},
			want: "session-id",
		},
		{
			name: "missing player-id",
			args: []string{"relay", "player", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--player-token", "t", "--session-id", "s", "--listen-address", "127.0.0.1:0"},
			want: "player-id",
		},
		{
			name: "missing player-token",
			args: []string{"relay", "player", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--player-id", "p", "--session-id", "s", "--listen-address", "127.0.0.1:0"},
			want: "player-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeCommand(tt.args...)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestRelayPlayerListenAddressIsConfigurable(t *testing.T) {
	// Verifies that --listen-address accepts non-default values.
	// Test uses port 0 which is valid but won't actually bind.
	_, err := executeCommand(
		"relay", "player",
		"--coordinator-url", "http://localhost:8080",
		"--group-id", "g",
		"--player-id", "p",
		"--player-token", "t",
		"--session-id", "s",
		"--listen-address", "127.0.0.1:25577",
	)
	// Expects error (can't connect) but NOT a flag parse error.
	if err == nil {
		t.Fatal("expected error from run")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--listen-address should be accepted, got: %v", err)
	}
}

func TestRelayPlayerInvalidListenAddress(t *testing.T) {
	_, err := executeCommand(
		"relay", "player",
		"--coordinator-url", "http://localhost:8080",
		"--group-id", "g",
		"--player-id", "p",
		"--player-token", "t",
		"--session-id", "s",
		"--listen-address", "not-a-valid-address",
	)
	if err == nil {
		t.Fatal("expected error for invalid listen address")
	}
}

func TestRelayPlayerNoSecretsInErrors(t *testing.T) {
	_, err := executeCommand(
		"relay", "player",
		"--coordinator-url", "http://localhost:8080",
		"--group-id", "g",
		"--player-id", "p",
		"--player-token", "secret_player_token_value",
		"--session-id", "s",
		"--listen-address", "127.0.0.1:25565",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret_player_token_value") {
		t.Error("error message must not contain the player token")
	}
}
