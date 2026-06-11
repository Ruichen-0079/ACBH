package cli

import (
	"fmt"

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
				token = localcontrol.GenerateToken()
				fmt.Fprintf(cmd.OutOrStdout(), "Local control token: %s\n", token)
				fmt.Fprintln(cmd.OutOrStdout(), "Use this token in the dashboard Agent panel to connect.")
				fmt.Fprintln(cmd.OutOrStdout(), "")
			}

			server := localcontrol.NewServer(listenAddr, token, configPtr)
			return server.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:6122", "Listen address for control API")
	cmd.Flags().StringVar(&token, "token", "", "Local control token (auto-generated if not provided)")

	return cmd
}
