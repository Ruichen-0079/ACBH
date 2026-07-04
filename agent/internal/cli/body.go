package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	minibody "github.com/Ruichen-0079/ACBH/agent/internal/minicore/body"
	"github.com/spf13/cobra"
)

func newBodyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "body",
		Short: "Run the ACBH v0.5 minimal local body API",
	}
	cmd.AddCommand(
		newBodyServeCmd(),
		newBodyHealthCmd(),
		newBodyProbeCmd(),
		newBodyInitCmd(),
		newBodyWriteExampleCmd(),
	)
	return cmd
}

func newBodyServeCmd() *cobra.Command {
	var listen string
	var appDataDir string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start localhost body/runtime API on 127.0.0.1:6120",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			srv := minibody.New(listen, appDataDir)
			fmt.Fprintf(cmd.OutOrStdout(), "ACBH body listening on http://%s\n", srv.Addr)
			return srv.ListenAndServe(ctx)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", minibody.DefaultAddr, "Body API listen address")
	cmd.Flags().StringVar(&appDataDir, "app-data-dir", "", "ACBH app data directory")
	return cmd
}

func newBodyHealthCmd() *cobra.Command {
	var bodyURL string
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Query local body health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return getBodyJSON(cmd.Context(), cmd, bodyURL, "/v1/body/health")
		},
	}
	cmd.Flags().StringVar(&bodyURL, "body-url", minibody.Endpoint(minibody.DefaultAddr), "Body API URL")
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newBodyProbeCmd() *cobra.Command {
	var bodyURL string
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Probe configured coordinator through local body",
		RunE: func(cmd *cobra.Command, args []string) error {
			return getBodyJSON(cmd.Context(), cmd, bodyURL, "/v1/coordinator/probe")
		},
	}
	cmd.Flags().StringVar(&bodyURL, "body-url", minibody.Endpoint(minibody.DefaultAddr), "Body API URL")
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newBodyInitCmd() *cobra.Command {
	var bodyURL string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Run minimal-core init through local body",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, bodyURL+"/v1/init", nil)
			if err != nil {
				return err
			}
			return doBodyJSON(cmd, req)
		},
	}
	cmd.Flags().StringVar(&bodyURL, "body-url", minibody.Endpoint(minibody.DefaultAddr), "Body API URL")
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newBodyWriteExampleCmd() *cobra.Command {
	var appDataDir string
	var coordinatorURL string
	var serverDir string
	cmd := &cobra.Command{
		Use:   "write-example-config",
		Short: "Write an example v0.5 config.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := minibody.WriteExampleConfig(appDataDir, coordinatorURL, serverDir)
			if err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"ok": true, "configPath": path})
		},
	}
	cmd.Flags().StringVar(&appDataDir, "app-data-dir", "", "ACBH app data directory")
	cmd.Flags().StringVar(&coordinatorURL, "coordinator-url", "http://121.40.101.224:6121", "Coordinator URL")
	cmd.Flags().StringVar(&serverDir, "server-dir", "", "Minecraft server directory")
	addIgnoredJSONFlag(cmd)
	return cmd
}

func getBodyJSON(ctx context.Context, cmd *cobra.Command, bodyURL string, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bodyURL+path, nil)
	if err != nil {
		return err
	}
	return doBodyJSON(cmd, req)
}

func doBodyJSON(cmd *cobra.Command, req *http.Request) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var value any
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return err
	}
	if err := printJSON(cmd, value); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("body API returned HTTP %d", resp.StatusCode)
	}
	return nil
}
