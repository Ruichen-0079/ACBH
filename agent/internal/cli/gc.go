package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/spf13/cobra"
)

type gcOptions struct {
	execute          bool
	explicitDryRun   bool
	retentionPerKind int
	minAgeMs         int
}

func newGcCmd() *cobra.Command {
	var opts gcOptions
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Run artifact garbage collection on the Coordinator",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			opts.explicitDryRun = cmd.Flags().Changed("dry-run")
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGc(cmd, opts)
		},
	}
	cmd.Flags().Bool("dry-run", true, "Show what would be deleted without making changes")
	cmd.Flags().BoolVar(&opts.execute, "execute", false, "Actually delete artifacts and objects")
	cmd.Flags().IntVar(&opts.retentionPerKind, "retention-per-kind", 0, "Number of recent available artifacts to keep per kind")
	cmd.Flags().IntVar(&opts.minAgeMs, "min-age", 0, "Minimum artifact age in milliseconds before deletion")
	return cmd
}

func runGc(cmd *cobra.Command, opts gcOptions) error {
	if opts.explicitDryRun && opts.execute {
		return errors.New("--dry-run and --execute are mutually exclusive")
	}

	dryRun := !opts.execute

	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return err
	}

	status, statusErr := client.GetElectionStatus(cmd.Context(), coordinator.ArtifactAuth{
		GroupID:   cfg.GroupID,
		HostID:    cfg.HostID,
		HostToken: cfg.HostToken,
	})
	if statusErr != nil {
		return fmt.Errorf("cannot verify current host state before running GC: %w", statusErr)
	}
	var generation *int
	if status.CurrentHostID != nil {
		if *status.CurrentHostID != cfg.HostID {
			return fmt.Errorf("only the current host may run garbage collection")
		}
		gen := status.CurrentHostGeneration
		generation = &gen
	}

	req := coordinator.GcRequest{
		DryRun:           dryRun,
		RetentionPerKind: opts.retentionPerKind,
		MinAgeMs:         opts.minAgeMs,
	}
	result, err := client.RunGC(cmd.Context(), req, coordinator.ArtifactAuth{
		GroupID:   cfg.GroupID,
		HostID:    cfg.HostID,
		HostToken: cfg.HostToken,
	}, generation)
	if err != nil {
		if strings.Contains(err.Error(), "GC blocked") || strings.Contains(err.Error(), "retained manifest") {
			return fmt.Errorf("garbage collection blocked because a retained manifest could not be read: %w", err)
		}
		if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "Forbidden") {
			return fmt.Errorf("only the current host may run garbage collection: %w", err)
		}
		if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "stale") {
			return fmt.Errorf("host generation is stale; current host may have changed: %w", err)
		}
		if strings.Contains(err.Error(), "400") {
			return fmt.Errorf("host generation header is required: %w", err)
		}
		return fmt.Errorf("garbage collection failed: %w", err)
	}

	if err := printJSON(cmd, result); err != nil {
		return err
	}
	if result.DryRun {
		if result.Blocked {
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"Dry run blocked: %d retained manifest(s) could not be read; object deletion is not safe. %d artifact(s) matched deletion rules.\n",
				len(result.Blockers),
				len(result.DeletedArtifacts),
			)
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Dry run: %d artifact(s) would be deleted.\n", len(result.DeletedArtifacts))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d artifact(s) and %d object(s).\n", len(result.DeletedArtifacts), result.DeletedObjectCount)
	}
	return nil
}
