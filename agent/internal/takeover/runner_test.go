package takeover

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestStateRoundTrip(t *testing.T) {
	path := StatePath(t.TempDir())
	want := State{
		AssignmentID:                "takeover_1",
		TakeoverToken:               "secret-token",
		CurrentHostGeneration:       2,
		LatestArtifactsAtAssignment: map[string]string{"world-snapshot": "snap_1"},
		ExpiresAt:                   "2026-06-06T00:01:00Z",
	}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadState() = %#v, want %#v", got, want)
	}
	if err := DeleteState(path); err != nil {
		t.Fatalf("DeleteState() error = %v", err)
	}
}

func TestRunNoAssignmentAndDryRunDoNotMutate(t *testing.T) {
	client := &fakeClient{}
	summary, err := Run(context.Background(), baseOptions(client))
	if err != nil || !summary.NoAssignment {
		t.Fatalf("Run() = %#v, %v", summary, err)
	}

	client.assignment = assignment()
	opts := baseOptions(client)
	opts.DryRun = true
	summary, err = Run(context.Background(), opts)
	if err != nil || !summary.DryRun {
		t.Fatalf("dry Run() = %#v, %v", summary, err)
	}
	if len(client.calls) != 2 || client.calls[1] != "poll" {
		t.Fatalf("calls = %#v", client.calls)
	}
}

func TestRunAcceptsPullsInOrderStartsHeartbeatsAndCompletes(t *testing.T) {
	client := &fakeClient{assignment: assignment()}
	var calls []string
	opts := baseOptions(client)
	opts.StatePath = filepath.Join(t.TempDir(), "takeover.json")
	opts.Pull = func(_ context.Context, kind manifest.ArtifactKind, id, _ string, applyDeletes bool) error {
		if !applyDeletes {
			t.Fatal("ApplyDeletes = false")
		}
		calls = append(calls, "pull:"+string(kind)+":"+id)
		return nil
	}
	opts.Start = func(context.Context) error {
		calls = append(calls, "start")
		return nil
	}
	opts.Heartbeat = func(_ context.Context, status string) error {
		calls = append(calls, "heartbeat:"+status)
		return nil
	}

	summary, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantCalls := []string{
		"pull:server-pack:pack_1",
		"pull:admin-state:admin_1",
		"pull:world-snapshot:snap_1",
		"start",
		"heartbeat:hosting",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if !reflect.DeepEqual(client.calls, []string{"poll", "accept", "complete"}) {
		t.Fatalf("client calls = %#v", client.calls)
	}
	if !summary.Completed {
		t.Fatal("Completed = false")
	}
}

func TestRunFailsAssignmentOnPullOrStartFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pullErr    error
		startErr   error
		wantReason string
	}{
		{name: "pull", pullErr: errors.New("download failed"), wantReason: "pull-server-pack-failed"},
		{name: "start", startErr: errors.New("launch failed"), wantReason: "server-start-failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{assignment: assignment()}
			opts := baseOptions(client)
			opts.Pull = func(context.Context, manifest.ArtifactKind, string, string, bool) error {
				return tc.pullErr
			}
			opts.Start = func(context.Context) error { return tc.startErr }
			_, err := Run(context.Background(), opts)
			if err == nil {
				t.Fatal("Run() error = nil")
			}
			if client.failureReason != tc.wantReason {
				t.Fatalf("failure reason = %q, want %q", client.failureReason, tc.wantReason)
			}
		})
	}
}

func TestRunOutputDoesNotExposeSecrets(t *testing.T) {
	client := &fakeClient{assignment: assignment()}
	var out bytes.Buffer
	opts := baseOptions(client)
	opts.Output = &out
	_, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, secret := range []string{"host-secret", "takeover-secret"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("output exposed %q: %s", secret, out.String())
		}
	}
}

type fakeClient struct {
	assignment    *coordinator.TakeoverAssignment
	calls         []string
	failureReason string
}

func (f *fakeClient) PollTakeover(_ context.Context, req coordinator.TakeoverPollRequest) (coordinator.TakeoverPollResponse, error) {
	f.calls = append(f.calls, "poll")
	if req.DryRun && f.assignment != nil {
		copy := *f.assignment
		copy.TakeoverToken = ""
		return coordinator.TakeoverPollResponse{Assignment: &copy}, nil
	}
	return coordinator.TakeoverPollResponse{Assignment: f.assignment}, nil
}

func (f *fakeClient) AcceptTakeover(context.Context, coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error) {
	f.calls = append(f.calls, "accept")
	return coordinator.TakeoverActionResponse{OK: true}, nil
}

func (f *fakeClient) CompleteTakeover(context.Context, coordinator.TakeoverActionRequest) (coordinator.TakeoverActionResponse, error) {
	f.calls = append(f.calls, "complete")
	return coordinator.TakeoverActionResponse{OK: true}, nil
}

func (f *fakeClient) FailTakeover(_ context.Context, req coordinator.TakeoverFailRequest) (coordinator.TakeoverActionResponse, error) {
	f.calls = append(f.calls, "fail")
	f.failureReason = req.FailureReason
	return coordinator.TakeoverActionResponse{OK: true}, nil
}

func baseOptions(client *fakeClient) RunOptions {
	return RunOptions{
		Auth: coordinator.ElectionAuthRequest{
			GroupID:   "group_1",
			HostID:    "host_1",
			HostToken: "host-secret",
		},
		ServerDir:    "server",
		ApplyDeletes: true,
		Client:       client,
		Pull: func(context.Context, manifest.ArtifactKind, string, string, bool) error {
			return nil
		},
		Start:     func(context.Context) error { return nil },
		Heartbeat: func(context.Context, string) error { return nil },
	}
}

func assignment() *coordinator.TakeoverAssignment {
	return &coordinator.TakeoverAssignment{
		AssignmentID:          "takeover_1",
		GroupID:               "group_1",
		HostID:                "host_1",
		TakeoverToken:         "takeover-secret",
		CurrentHostGeneration: 0,
		LatestArtifactsAtAssignment: map[string]string{
			"server-pack":    "pack_1",
			"admin-state":    "admin_1",
			"world-snapshot": "snap_1",
		},
		ExpiresAt: "2026-06-06T00:01:00Z",
	}
}
