package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/localcontrol"
	"github.com/spf13/cobra"
)

func newControlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "control",
		Short: "Agent local control API for dashboard actions",
	}
	cmd.AddCommand(newControlServeCmd())
	return cmd
}

func newControlServeCmd() *cobra.Command {
	var listenAddr string
	var token string
	var allowRemote bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start local control HTTP server for dashboard integration",
		Long:  "Starts a localhost-only HTTP API that the Coordinator dashboard can call to run Agent actions directly.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			var configPtr *agentconfig.Config
			if err == nil {
				configPtr = &cfg
			}

			if token == "" {
				var tokenPath string
				token, tokenPath, err = loadOrCreateControlToken()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Local control token: %s\n", maskControlToken(token))
				fmt.Fprintf(cmd.OutOrStdout(), "Token file: %s\n", tokenPath)
				fmt.Fprintln(cmd.OutOrStdout(), "Read the token file when connecting the dashboard; the full token is not printed.")
			}

			server := localcontrol.NewServer(listenAddr, token, configPtr)
			server.AllowRemote = allowRemote
			return server.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:6122", "Listen address for control API")
	cmd.Flags().StringVar(&token, "token", "", "Local control token (auto-generated if not provided)")
	cmd.Flags().BoolVar(&allowRemote, "allow-remote-control", false, "Allow the control API to bind a non-loopback address (unsafe on untrusted networks)")

	return cmd
}

func loadOrCreateControlToken() (string, string, error) {
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		return "", "", fmt.Errorf("find control token directory: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create control token directory: %w", err)
	}

	tokenPath := filepath.Join(configDir, "control-token")
	if data, readErr := os.ReadFile(tokenPath); readErr == nil {
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", "", fmt.Errorf("control token file is empty: %s", tokenPath)
		}
		if err := os.Chmod(tokenPath, 0o600); err != nil {
			return "", "", fmt.Errorf("secure control token file: %w", err)
		}
		return token, tokenPath, nil
	} else if !os.IsNotExist(readErr) {
		return "", "", fmt.Errorf("read control token file: %w", readErr)
	}

	token := localcontrol.GenerateToken()
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write control token file: %w", err)
	}
	return token, tokenPath, nil
}

func maskControlToken(token string) string {
	if len(token) <= 8 {
		return "[hidden]"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
