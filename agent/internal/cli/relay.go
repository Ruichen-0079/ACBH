package cli

import (
	"fmt"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/relay"
	"github.com/spf13/cobra"
)

func newRelayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Connect as a relay tunnel endpoint",
	}
	cmd.AddCommand(newRelayHostCmd())
	return cmd
}

func newRelayHostCmd() *cobra.Command {
	var opts relayHostOptions
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Connect as host-side relay tunnel and forward to a local TCP target",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelayHost(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.coordinatorURL, "coordinator-url", "", "Coordinator base URL (e.g. http://public-node:8080)")
	cmd.Flags().StringVar(&opts.groupID, "group-id", "", "ACBH group ID")
	cmd.Flags().StringVar(&opts.hostID, "host-id", "", "ACBH host ID")
	cmd.Flags().StringVar(&opts.hostToken, "host-token", "", "ACBH host token")
	cmd.Flags().IntVar(&opts.hostGeneration, "host-generation", 0, "Current host generation number")
	cmd.Flags().StringVar(&opts.sessionID, "session-id", "", "Tunnel session ID to connect to")
	cmd.Flags().StringVar(&opts.targetAddress, "target-address", "", "TCP target address (e.g. 127.0.0.1:25565)")

	return cmd
}

type relayHostOptions struct {
	coordinatorURL string
	groupID        string
	hostID         string
	hostToken      string
	hostGeneration int
	sessionID      string
	targetAddress  string
}

func runRelayHost(cmd *cobra.Command, opts relayHostOptions) error {
	if opts.coordinatorURL == "" {
		cfg, _, err := loadConfig()
		if err != nil {
			return fmt.Errorf("--coordinator-url is required (could not load config: %w)", err)
		}
		opts.coordinatorURL = cfg.CoordinatorURL
	}
	if opts.groupID == "" {
		cfg, _, err := loadConfig()
		if err != nil {
			return fmt.Errorf("--group-id is required (could not load config: %w)", err)
		}
		opts.groupID = cfg.GroupID
	}
	if opts.hostID == "" {
		cfg, _, err := loadConfig()
		if err != nil {
			return fmt.Errorf("--host-id is required (could not load config: %w)", err)
		}
		opts.hostID = cfg.HostID
	}
	if opts.hostToken == "" {
		cfg, _, err := loadConfig()
		if err != nil {
			return fmt.Errorf("--host-token is required (could not load config: %w)", err)
		}
		opts.hostToken = cfg.HostToken
	}

	if opts.groupID == "" {
		return fmt.Errorf("--group-id is required")
	}
	if opts.hostID == "" {
		return fmt.Errorf("--host-id is required")
	}
	if opts.hostToken == "" {
		return fmt.Errorf("--host-token is required")
	}
	if opts.hostGeneration <= 0 {
		return fmt.Errorf("--host-generation must be positive")
	}
	if opts.sessionID == "" {
		return fmt.Errorf("--session-id is required")
	}
	if opts.targetAddress == "" {
		return fmt.Errorf("--target-address is required")
	}
	if opts.coordinatorURL == "" {
		return fmt.Errorf("--coordinator-url is required")
	}

	if _, err := coordinator.NewClient(opts.coordinatorURL); err != nil {
		return fmt.Errorf("invalid coordinator URL: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Connecting to relay tunnel session %s...\n", opts.sessionID)

	client := relay.NewHostRelayClient(relay.HostRelayOptions{
		CoordinatorURL: opts.coordinatorURL,
		GroupID:        opts.groupID,
		SessionID:      opts.sessionID,
		HostID:         opts.hostID,
		HostToken:      opts.hostToken,
		HostGeneration: opts.hostGeneration,
		TargetAddress:  opts.targetAddress,
	})

	if err := client.Run(cmd.Context()); err != nil {
		return err
	}

	return nil
}
