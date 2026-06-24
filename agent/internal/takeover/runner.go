package takeover

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

var ErrNoAssignment = errors.New("takeover: no assignment available")

type Client interface {
	PollTakeover(context.Context, coordinator.TakeoverPollRequest) (coordinator.TakeoverPollResponse, error)
	AcceptTakeover(context.Context, coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error)
	CompleteTakeover(context.Context, coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error)
	FailTakeover(context.Context, coordinator.TakeoverFailRequest) (coordinator.TakeoverActionResponse, error)
}

type PullFunc func(context.Context, manifest.ArtifactKind, string, string, bool) error
type StartFunc func(context.Context) error
type HeartbeatFunc func(context.Context, string) error

type RunOptions struct {
	Auth         coordinator.ElectionAuthRequest
	ServerDir    string
	DryRun       bool
	ApplyDeletes bool
	Client       Client
	Pull         PullFunc
	Start        StartFunc
	Heartbeat    HeartbeatFunc
	StatePath    string
	Output       io.Writer
}

type RunSummary struct {
	NoAssignment bool
	DryRun       bool
	AssignmentID string
	Pulled       []manifest.ArtifactKind
	Skipped      []manifest.ArtifactKind
	Completed    bool
}

func Run(ctx context.Context, opts RunOptions) (RunSummary, error) {
	if opts.Client == nil || opts.Pull == nil || opts.Start == nil || opts.Heartbeat == nil {
		return RunSummary{}, errors.New("takeover runner dependencies are required")
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}

	polled, err := opts.Client.PollTakeover(ctx, coordinator.TakeoverPollRequest{
		ElectionAuthRequest: opts.Auth,
		DryRun:              opts.DryRun,
	})
	if err != nil {
		return RunSummary{}, fmt.Errorf("poll takeover assignment: %w", err)
	}
	if polled.Assignment == nil {
		fmt.Fprintln(opts.Output, "No takeover assignment is available.")
		return RunSummary{NoAssignment: true}, nil
	}

	assignment := polled.Assignment
	summary := RunSummary{AssignmentID: assignment.AssignmentID, DryRun: opts.DryRun}
	if opts.DryRun {
		fmt.Fprintf(opts.Output, "Takeover dry run for assignment %s\n", assignment.AssignmentID)
		printArtifactPlan(opts.Output, assignment.LatestArtifactsAtAssignment)
		fmt.Fprintln(opts.Output, "Server process would start after artifact restore.")
		return summary, nil
	}

	token := assignment.TakeoverToken
	if token == "" && opts.StatePath != "" {
		state, loadErr := LoadState(opts.StatePath)
		if loadErr == nil && state.AssignmentID == assignment.AssignmentID {
			token = state.TakeoverToken
		}
	}
	if token == "" {
		return RunSummary{}, errors.New("takeover assignment token is unavailable; poll the assignment again from the original Agent state")
	}
	if opts.StatePath != "" {
		if err := SaveState(opts.StatePath, State{
			AssignmentID:                assignment.AssignmentID,
			TakeoverToken:               token,
			CurrentHostGeneration:       assignment.CurrentHostGeneration,
			LatestArtifactsAtAssignment: assignment.LatestArtifactsAtAssignment,
			ExpiresAt:                   assignment.ExpiresAt,
		}); err != nil {
			return RunSummary{}, err
		}
	}

	action := coordinator.TakeoverActionRequest{
		GroupID:       opts.Auth.GroupID,
		HostID:        opts.Auth.HostID,
		HostToken:     opts.Auth.HostToken,
		AssignmentID:  assignment.AssignmentID,
		TakeoverToken: token,
	}
	if _, err := opts.Client.AcceptTakeover(ctx, action); err != nil {
		return RunSummary{}, fmt.Errorf("accept takeover assignment: %w", err)
	}

	fail := func(reason string, cause error) (RunSummary, error) {
		_, failErr := opts.Client.FailTakeover(ctx, coordinator.TakeoverFailRequest{
			TakeoverActionRequest: action,
			FailureReason:         reason,
		})
		if failErr != nil {
			return summary, fmt.Errorf("%w; report takeover failure: %v", cause, failErr)
		}
		return summary, cause
	}

	kind := manifest.WorldSnapshot
	artifactID := assignment.LatestArtifactsAtAssignment[string(kind)]
	if artifactID == "" {
		summary.Skipped = append(summary.Skipped, kind)
		fmt.Fprintf(opts.Output, "Skipping %s: no assigned artifact.\n", kind)
	} else {
		fmt.Fprintf(opts.Output, "Restoring %s %s\n", kind, artifactID)
		if err := opts.Pull(ctx, kind, artifactID, opts.ServerDir, opts.ApplyDeletes); err != nil {
			return fail("pull-"+string(kind)+"-failed", fmt.Errorf("pull %s: %w", kind, err))
		}
		summary.Pulled = append(summary.Pulled, kind)
	}

	if err := opts.Start(ctx); err != nil {
		return fail("server-start-failed", fmt.Errorf("start server: %w", err))
	}
	if err := opts.Heartbeat(ctx, "hosting"); err != nil {
		return fail("hosting-heartbeat-failed", fmt.Errorf("send hosting heartbeat: %w", err))
	}
	if _, err := opts.Client.CompleteTakeover(ctx, action); err != nil {
		return fail("completion-failed", fmt.Errorf("complete takeover assignment: %w", err))
	}

	if opts.StatePath != "" {
		if err := DeleteState(opts.StatePath); err != nil {
			return summary, err
		}
	}
	summary.Completed = true
	fmt.Fprintln(opts.Output, "Takeover assignment completed.")
	return summary, nil
}

func printArtifactPlan(output io.Writer, artifacts map[string]string) {
	kind := manifest.WorldSnapshot
	if artifactID := artifacts[string(kind)]; artifactID != "" {
		fmt.Fprintf(output, "Would restore %s %s\n", kind, artifactID)
	} else {
		fmt.Fprintf(output, "Would skip %s: no assigned artifact.\n", kind)
	}
}
