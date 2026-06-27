package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestGroupMutationGuardBlocksDuplicateCreate(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp_existing",
		MemberID:       "mem_existing",
		HostID:         "host_existing",
		HostToken:      "token",
		DisplayName:    "Tester",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	_ = SaveDesktopConfig(opts, DesktopConfig{GroupName: "Existing", Group: DesktopGroupConfig{GroupID: "grp_existing", Role: "owner"}})

	if err := guardGroupMutation(opts, "create"); err == nil {
		t.Fatal("expected duplicate create guard error")
	}
	current, err := ResolveCurrentGroup(opts)
	if err != nil {
		t.Fatalf("ResolveCurrentGroup() error = %v", err)
	}
	if current.CanCreate || current.CanJoin {
		t.Fatalf("current = %#v, want configured group without create/join", current)
	}
}

func TestResetLocalAllowsReconfigure(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp_existing",
		MemberID:       "mem_existing",
		HostID:         "host_existing",
		HostToken:      "token",
		DisplayName:    "Tester",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := ResetLocalGroup(opts); err != nil {
		t.Fatalf("ResetLocalGroup() error = %v", err)
	}
	current, err := ResolveCurrentGroup(opts)
	if err != nil {
		t.Fatalf("ResolveCurrentGroup() error = %v", err)
	}
	if !current.CanCreate || !current.CanJoin || current.Phase != GroupPhaseUnconfigured {
		t.Fatalf("current = %#v, want unconfigured after reset", current)
	}
}

func TestMembersEndpointReturnsRoles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"coordinatorVersion": "0.4.0-alpha3", "protocolVersion": 2, "minimumClientProtocol": 2,
				"capabilities": []string{"group_whoami_v1", "lease_renew_v1"},
			})
		case "/v1/groups/grp_test/members":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"groupId": "grp_test", "groupName": "Alpha", "currentHostId": "host_owner",
				"members": []map[string]any{
					{"memberId": "mem_owner", "displayName": "Owner", "role": "owner", "hostId": "host_owner", "isLocal": true, "isCurrentHost": true},
					{"memberId": "mem_member", "displayName": "Member", "role": "member", "hostId": "host_member"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	opts := Options{AppDataDir: t.TempDir()}
	err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: server.URL,
		GroupID:        "grp_test",
		MemberID:       "mem_owner",
		HostID:         "host_owner",
		HostToken:      "token",
		DisplayName:    "Owner",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	view, err := ListCurrentGroupMembers(context.Background(), opts)
	if err != nil {
		t.Fatalf("ListCurrentGroupMembers() error = %v", err)
	}
	if !view.OK || len(view.Members) != 2 {
		t.Fatalf("view = %#v, want two members", view)
	}
}