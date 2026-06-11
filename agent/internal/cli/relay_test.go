package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
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
	// Uses a timeout context so proxy.Run does not hang on ln.Accept.
	cmd := newRelayPlayerCmd()
	for _, addr := range []string{"127.0.0.1:25565", "127.0.0.1:25577"} {
		flagAddr, err := cmd.Flags().GetString("listen-address")
		if err != nil {
			t.Fatal(err)
		}
		if flagAddr != "127.0.0.1:25565" {
			t.Errorf("expected default listen-address=127.0.0.1:25565, got %s", flagAddr)
		}

		// Verify flag parse for non-default
		c := newRelayPlayerCmd()
		c.SetArgs([]string{"--listen-address", addr})
		if err := c.ParseFlags([]string{"--listen-address", addr}); err != nil {
			t.Fatalf("failed to parse --listen-address %s: %v", addr, err)
		}
		got, err := c.Flags().GetString("listen-address")
		if err != nil {
			t.Fatal(err)
		}
		if got != addr {
			t.Errorf("expected --listen-address=%s, got %s", addr, got)
		}
	}
}

func TestRelayPlayerInvalidListenAddress(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"relay", "player", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--player-id", "p", "--player-token", "t", "--session-id", "s", "--listen-address", "not-a-valid-address"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid listen address")
	}
}

func TestRelayPlayerNoSecretsInErrors(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"relay", "player", "--coordinator-url", "http://localhost:8080", "--group-id", "g", "--player-id", "p", "--player-token", "secret_player_token_value", "--session-id", "s", "--listen-address", "127.0.0.1:0"})
	cmd.SetContext(ctx)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret_player_token_value") {
		t.Error("error message must not contain the player token")
	}
}

func TestRelayPlayerCommandRegistered(t *testing.T) {
	playerCmd := &cobra.Command{Use: "player"}
	found := false
	for _, sub := range newRelayCmd().Commands() {
		if sub.Use == "player" {
			playerCmd = sub
			found = true
			break
		}
	}
	if !found {
		t.Fatal("player subcommand not registered under relay")
	}
	if playerCmd.Use != "player" {
		t.Errorf("expected Use=player, got %s", playerCmd.Use)
	}

	// Verify required flags exist
	flags := []string{"coordinator-url", "group-id", "player-id", "player-token", "session-id", "listen-address"}
	for _, name := range flags {
		if playerCmd.Flags().Lookup(name) == nil {
			t.Errorf("missing required flag --%s", name)
		}
	}
}
