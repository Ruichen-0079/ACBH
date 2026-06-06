package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcserver"
	"github.com/Ruichen-0079/ACBH/agent/internal/takeover"
	"github.com/spf13/cobra"
)

func newElectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "election",
		Short: "Inspect or trigger Coordinator election timeout checks",
	}
	cmd.AddCommand(newElectionStatusCmd(), newElectionCheckTimeoutCmd())
	return cmd
}

func newElectionStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current host and the latest election result",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, auth, _, err := loadTakeoverContext()
			if err != nil {
				return err
			}
			status, err := client.GetElectionStatus(cmd.Context(), coordinator.ArtifactAuth{
				GroupID:   auth.GroupID,
				HostID:    auth.HostID,
				HostToken: auth.HostToken,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Group ID: %s\n", cfg.GroupID)
			fmt.Fprintf(cmd.OutOrStdout(), "Current host generation: %d\n", status.CurrentHostGeneration)
			if status.CurrentHostID == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Current host: none")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Current host: %s\n", *status.CurrentHostID)
			}
			if status.LastElection == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Last election: none")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Last election reason: %s\n", status.LastElection.Reason)
				if status.LastElection.SelectedHostID == nil {
					fmt.Fprintln(cmd.OutOrStdout(), "Last selected host: none")
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Last selected host: %s\n", *status.LastElection.SelectedHostID)
				}
			}
			if status.ActiveTakeoverAssignment == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Active takeover assignment: none")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Active takeover assignment: %s (%s)\n",
					status.ActiveTakeoverAssignment.AssignmentID,
					status.ActiveTakeoverAssignment.Status,
				)
			}
			return nil
		},
	}
}

func newElectionCheckTimeoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check-timeout",
		Short: "Ask the Coordinator to check whether current host election is needed",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, _, err := loadTakeoverContext()
			if err != nil {
				return err
			}
			result, err := client.CheckElectionTimeout(cmd.Context(), auth)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Election needed: %t\n", result.ElectionNeeded)
			if result.Election != nil && result.Election.SelectedHostID != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Selected host: %s\n", *result.Election.SelectedHostID)
				fmt.Fprintf(cmd.OutOrStdout(), "Reason: %s\n", result.Election.Election.Reason)
			}
			return nil
		},
	}
}

func newTakeoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "takeover",
		Short: "Poll, manage, and execute takeover assignments",
	}
	cmd.AddCommand(
		newTakeoverPollCmd(),
		newTakeoverAcceptCmd(),
		newTakeoverCompleteCmd(),
		newTakeoverFailCmd(),
		newTakeoverRunCmd(),
	)
	return cmd
}

func newTakeoverPollCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "poll",
		Short: "Poll for an assignment and store its one-time token locally",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, statePath, err := loadTakeoverContext()
			if err != nil {
				return err
			}
			result, err := client.PollTakeover(cmd.Context(), coordinator.TakeoverPollRequest{
				ElectionAuthRequest: auth,
			})
			if err != nil {
				return err
			}
			if result.Assignment == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No takeover assignment is available.")
				return nil
			}
			if result.Assignment.TakeoverToken != "" {
				if err := savePolledAssignment(statePath, result.Assignment); err != nil {
					return err
				}
			} else {
				state, loadErr := takeover.LoadState(statePath)
				if loadErr != nil || state.AssignmentID != result.Assignment.AssignmentID {
					return errors.New("takeover token was already returned and no matching local runtime state exists")
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Takeover assignment: %s\n", result.Assignment.AssignmentID)
			fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", result.Assignment.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Generation: %d\n", result.Assignment.CurrentHostGeneration)
			fmt.Fprintln(cmd.OutOrStdout(), "Takeover token stored locally.")
			return nil
		},
	}
}

func newTakeoverAcceptCmd() *cobra.Command {
	return takeoverStateCommand("accept", "Accept the locally stored takeover assignment", func(
		ctx context.Context,
		client *coordinator.Client,
		action coordinator.TakeoverActionRequest,
	) (coordinator.TakeoverActionResponse, error) {
		return client.AcceptTakeover(ctx, action)
	})
}

func newTakeoverCompleteCmd() *cobra.Command {
	cmd := takeoverStateCommand("complete", "Complete the locally stored takeover assignment", func(
		ctx context.Context,
		client *coordinator.Client,
		action coordinator.TakeoverActionRequest,
	) (coordinator.TakeoverActionResponse, error) {
		return client.CompleteTakeover(ctx, action)
	})
	cmd.PostRunE = func(cmd *cobra.Command, args []string) error {
		configDir, err := agentconfig.DefaultDir()
		if err != nil {
			return err
		}
		return takeover.DeleteState(takeover.StatePath(configDir))
	}
	return cmd
}

func newTakeoverFailCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "fail",
		Short: "Fail the locally stored takeover assignment",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, statePath, err := loadTakeoverContext()
			if err != nil {
				return err
			}
			state, err := takeover.LoadState(statePath)
			if err != nil {
				return err
			}
			_, err = client.FailTakeover(cmd.Context(), coordinator.TakeoverFailRequest{
				TakeoverActionRequest: actionFromState(auth, state),
				FailureReason:         reason,
			})
			if err != nil {
				return err
			}
			if err := takeover.DeleteState(statePath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Takeover assignment failed: %s\n", state.AssignmentID)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Failure reason")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newTakeoverRunCmd() *cobra.Command {
	var opts takeoverRunOptions
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Restore assigned artifacts, start the local server, and complete takeover",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTakeover(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Minecraft server working directory")
	cmd.Flags().StringVar(&opts.command, "command", "", "User-provided server launch command")
	cmd.Flags().StringVar(&opts.logDir, "log-dir", "", "Directory for server stdout and stderr logs")
	cmd.Flags().DurationVar(&opts.stopTimeout, "stop-timeout", 0, "Graceful stop timeout before forced kill")
	cmd.Flags().BoolVar(&opts.noApplyDeletes, "no-apply-deletes", false, "Do not apply deleted manifest entries")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show the takeover plan without accepting or executing it")
	return cmd
}

type takeoverRunOptions struct {
	serverStartOptions
	noApplyDeletes bool
	dryRun         bool
}

func runTakeover(ctx context.Context, cmd *cobra.Command, opts takeoverRunOptions) error {
	cfg, client, auth, statePath, err := loadTakeoverContext()
	if err != nil {
		return err
	}
	resolved, err := resolveServerStartOptions(opts.serverStartOptions)
	if err != nil {
		return err
	}
	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Planned server directory: %s\n", resolved.ServerDir)
		fmt.Fprintf(cmd.OutOrStdout(), "Planned server command: %s\n", resolved.Command)
	}

	_, err = takeover.Run(ctx, takeover.RunOptions{
		Auth:         auth,
		ServerDir:    resolved.ServerDir,
		DryRun:       opts.dryRun,
		ApplyDeletes: !opts.noApplyDeletes,
		Client:       client,
		StatePath:    statePath,
		Output:       cmd.OutOrStdout(),
		Pull: func(ctx context.Context, kind manifest.ArtifactKind, artifactID, outputDir string, applyDeletes bool) error {
			summary, err := artifactsync.Pull(ctx, artifactsync.PullOptions{
				ArtifactKind: kind,
				ArtifactID:   artifactID,
				OutputDir:    outputDir,
				ApplyDeletes: applyDeletes,
				Config:       cfg,
				Client:       client,
			})
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Restored %s: files=%d bytes=%d\n", kind, summary.WrittenFiles, summary.TotalBytes)
			}
			return err
		},
		Start: func(ctx context.Context) error {
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("find Agent executable: %w", err)
			}
			state, err := mcserver.Start(ctx, executable, resolved)
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Server started with PID %d\n", state.PID)
			}
			return err
		},
		Heartbeat: func(ctx context.Context, status string) error {
			req, err := buildHeartbeatRequest(cfg, heartbeatOptions{status: status})
			if err != nil {
				return err
			}
			_, err = sendHeartbeat(ctx, cfg, req)
			return err
		},
	})
	return err
}

func takeoverStateCommand(
	use string,
	short string,
	call func(context.Context, *coordinator.Client, coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, statePath, err := loadTakeoverContext()
			if err != nil {
				return err
			}
			state, err := takeover.LoadState(statePath)
			if err != nil {
				return err
			}
			response, err := call(cmd.Context(), client, actionFromState(auth, state))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Takeover assignment %s: %s\n",
				response.Assignment.AssignmentID,
				response.Assignment.Status,
			)
			return nil
		},
	}
}

func loadTakeoverContext() (
	agentconfig.Config,
	*coordinator.Client,
	coordinator.ElectionAuthRequest,
	string,
	error,
) {
	cfg, _, err := loadConfig()
	if err != nil {
		return agentconfig.Config{}, nil, coordinator.ElectionAuthRequest{}, "", err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return agentconfig.Config{}, nil, coordinator.ElectionAuthRequest{}, "", err
	}
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		return agentconfig.Config{}, nil, coordinator.ElectionAuthRequest{}, "", err
	}
	return cfg, client, coordinator.ElectionAuthRequest{
		GroupID:   cfg.GroupID,
		HostID:    cfg.HostID,
		HostToken: cfg.HostToken,
	}, takeover.StatePath(configDir), nil
}

func savePolledAssignment(path string, assignment *coordinator.TakeoverAssignment) error {
	if assignment == nil || assignment.TakeoverToken == "" {
		return errors.New("polled assignment did not include a one-time takeover token")
	}
	return takeover.SaveState(path, takeover.State{
		AssignmentID:                assignment.AssignmentID,
		TakeoverToken:               assignment.TakeoverToken,
		CurrentHostGeneration:       assignment.CurrentHostGeneration,
		LatestArtifactsAtAssignment: assignment.LatestArtifactsAtAssignment,
		ExpiresAt:                   assignment.ExpiresAt,
	})
}

func actionFromState(auth coordinator.ElectionAuthRequest, state takeover.State) coordinator.TakeoverActionRequest {
	return coordinator.TakeoverActionRequest{
		GroupID:       auth.GroupID,
		HostID:        auth.HostID,
		HostToken:     auth.HostToken,
		AssignmentID:  state.AssignmentID,
		TakeoverToken: state.TakeoverToken,
	}
}
