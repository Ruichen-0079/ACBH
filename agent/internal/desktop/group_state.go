package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

type LocalGroupPhase string

const (
	GroupPhaseUnconfigured      LocalGroupPhase = "unconfigured"
	GroupPhaseCreating          LocalGroupPhase = "creating"
	GroupPhaseJoined            LocalGroupPhase = "joined"
	GroupPhaseOwner             LocalGroupPhase = "owner"
	GroupPhaseMember            LocalGroupPhase = "member"
	GroupPhaseIdentityMismatch  LocalGroupPhase = "identity_mismatch"
)

type CurrentGroupView struct {
	Phase           LocalGroupPhase `json:"phase"`
	GroupName       string          `json:"groupName,omitempty"`
	GroupID         string          `json:"groupId,omitempty"`
	DisplayName     string          `json:"displayName,omitempty"`
	MemberID        string          `json:"memberId,omitempty"`
	HostID          string          `json:"hostId,omitempty"`
	Role            string          `json:"role,omitempty"`
	CoordinatorURL  string          `json:"coordinatorUrl,omitempty"`
	CanCreate       bool            `json:"canCreate"`
	CanJoin         bool            `json:"canJoin"`
	CanReset        bool            `json:"canReset"`
	Message         string          `json:"message,omitempty"`
	IdentityMismatch bool           `json:"identityMismatch,omitempty"`
}

type GroupMembersView struct {
	OK            bool                          `json:"ok"`
	GroupID       string                        `json:"groupId,omitempty"`
	GroupName     string                        `json:"groupName,omitempty"`
	CurrentHostID string                        `json:"currentHostId,omitempty"`
	LocalHostID   string                        `json:"localHostId,omitempty"`
	Members       []coordinator.GroupMemberInfo `json:"members,omitempty"`
	Message       string                        `json:"message,omitempty"`
	ErrorCode     string                        `json:"errorCode,omitempty"`
}

func ResolveCurrentGroup(opts Options) (CurrentGroupView, error) {
	opts = withDefaults(opts)
	desktopCfg, _ := LoadDesktopConfig(opts)
	view := CurrentGroupView{
		GroupName:      desktopCfg.GroupName,
		CoordinatorURL: firstNonEmpty(desktopCfg.CoordinatorURL, ""),
		CanCreate:      true,
		CanJoin:        true,
		Phase:          GroupPhaseUnconfigured,
		Message:        "尚未配置服务器组，可创建新组或加入已有组。",
	}
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return view, nil
		}
		return view, err
	}
	if strings.TrimSpace(cfg.GroupID) == "" {
		return view, nil
	}
	view.GroupID = cfg.GroupID
	view.MemberID = cfg.MemberID
	view.HostID = cfg.HostID
	view.DisplayName = cfg.DisplayName
	view.CoordinatorURL = cfg.CoordinatorURL
	view.CanCreate = false
	view.CanJoin = false
	view.CanReset = true
	role := strings.ToLower(firstNonEmpty(desktopCfg.Group.Role, "member"))
	view.Role = role
	switch role {
	case "owner", "admin":
		view.Phase = GroupPhaseOwner
	default:
		if desktopCfg.Group.Role == "" {
			view.Phase = GroupPhaseJoined
		} else {
			view.Phase = GroupPhaseMember
		}
	}
	view.Message = "当前已配置服务器组。"
	return view, nil
}

func CurrentGroupStatus(ctx context.Context, opts Options) (CurrentGroupView, error) {
	view, err := ResolveCurrentGroup(opts)
	if err != nil || view.Phase == GroupPhaseUnconfigured {
		return view, err
	}
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return view, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return view, err
	}
	who, err := client.WhoAmI(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
	if err != nil {
		if coordinator.IsRouteNotFound(err) {
			view.Message = "Coordinator 缺少 whoami 接口，无法确认身份。"
			return view, nil
		}
		view.IdentityMismatch = true
		view.Phase = GroupPhaseIdentityMismatch
		view.Message = "无法确认本机身份：" + err.Error()
		return view, nil
	}
	view.Role = who.Role
	view.MemberID = who.MemberID
	view.HostID = who.HostID
	desktopCfg, _ := LoadDesktopConfig(opts)
	if desktopCfg.Group.MemberID != "" && desktopCfg.Group.MemberID != who.MemberID {
		view.IdentityMismatch = true
		view.Phase = GroupPhaseIdentityMismatch
		view.Message = "本地身份与服务端不一致，请修复身份或重置当前组。"
		return view, nil
	}
	switch strings.ToLower(who.Role) {
	case "owner", "admin":
		view.Phase = GroupPhaseOwner
	default:
		view.Phase = GroupPhaseMember
	}
	return view, nil
}

func ListCurrentGroupMembers(ctx context.Context, opts Options) (GroupMembersView, error) {
	view := GroupMembersView{OK: false}
	current, err := ResolveCurrentGroup(opts)
	if err != nil {
		return view, err
	}
	if current.Phase == GroupPhaseUnconfigured {
		return GroupMembersView{OK: false, ErrorCode: "not_configured", Message: "尚未配置服务器组。"}, nil
	}
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return view, err
	}
	auth := coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return view, err
	}
	gate, gateErr := ProbeCoordinatorCapabilities(ctx, cfg.CoordinatorURL)
	if gateErr != nil && coordinator.IsCoordinatorVersionMismatch(gateErr) {
		return GroupMembersView{
			OK: false, ErrorCode: "coordinator_version_mismatch",
			Message: "当前 Coordinator 版本过旧，无法列出成员。",
		}, nil
	}
	list, err := client.ListGroupMembers(ctx, auth)
	if err != nil {
		if coordinator.IsRouteNotFound(err) {
			return GroupMembersView{OK: false, ErrorCode: "unsupported_capability", Message: "Coordinator 不支持成员列表接口。"}, nil
		}
		return GroupMembersView{OK: false, ErrorCode: "members_list_failed", Message: err.Error()}, nil
	}
	_ = gate
	for i := range list.Members {
		list.Members[i].IsLocal = list.Members[i].HostID == cfg.HostID || list.Members[i].MemberID == cfg.MemberID
	}
	view = GroupMembersView{
		OK: true, GroupID: list.GroupID, GroupName: list.GroupName,
		CurrentHostID: stringPtrValue(list.CurrentHostID), LocalHostID: cfg.HostID, Members: list.Members,
		Message: "成员列表已刷新。",
	}
	return view, nil
}

func ResetLocalGroup(opts Options) (map[string]any, error) {
	if _, err := LeaveGroup(opts); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "已离开并重置当前组，可重新创建或加入。"}, nil
}

func guardGroupMutation(opts Options, action string) error {
	current, err := ResolveCurrentGroup(opts)
	if err != nil {
		return err
	}
	switch action {
	case "create":
		if !current.CanCreate {
			return fmt.Errorf("group_already_configured: 已配置 Group %s，请先离开/重置当前 Group", current.GroupID)
		}
	case "join":
		if !current.CanJoin {
			return fmt.Errorf("group_already_configured: 已加入 Group %s，请先离开/重置当前 Group", current.GroupID)
		}
	}
	return nil
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}