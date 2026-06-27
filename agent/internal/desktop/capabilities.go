package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

const (
	CapabilityLeaseRenew       = "lease_renew_v1"
	CapabilityInviteManagement = "invite_management_v1"
	CapabilityWorldBackup      = "world_backup_v1"
	CapabilityCapabilities     = "capabilities_v1"
)

type CoordinatorFeatureGate struct {
	CoordinatorURL              string                    `json:"coordinatorUrl,omitempty"`
	CapabilitiesAvailable       bool                      `json:"capabilitiesAvailable"`
	Capabilities                *coordinator.Capabilities `json:"capabilities,omitempty"`
	LeaseRenewSupported         bool                      `json:"leaseRenewSupported"`
	InviteManagementSupported   bool                      `json:"inviteManagementSupported"`
	WorldBackupSupported        bool                      `json:"worldBackupSupported"`
	BackupUploadEnabled         bool                      `json:"backupUploadEnabled"`
	LeaseOperationsEnabled      bool                      `json:"leaseOperationsEnabled"`
	TakeoverEnabled             bool                      `json:"takeoverEnabled"`
	InviteManagementEnabled     bool                      `json:"inviteManagementEnabled"`
	CoordinatorVersionMismatch  bool                      `json:"coordinatorVersionMismatch"`
	CapabilityRouteMissing      bool                      `json:"capabilityRouteMissing"`
	Message                     string                    `json:"message,omitempty"`
	DisableReasons              map[string]string         `json:"disableReasons,omitempty"`
}

func ProbeCoordinatorCapabilities(ctx context.Context, coordURL string) (CoordinatorFeatureGate, error) {
	gate := CoordinatorFeatureGate{
		CoordinatorURL: strings.TrimSpace(coordURL),
		DisableReasons: map[string]string{},
	}
	if gate.CoordinatorURL == "" {
		return gate, errors.New("coordinator URL is not configured")
	}
	client, err := coordinator.NewClient(gate.CoordinatorURL)
	if err != nil {
		return gate, err
	}
	if err := client.Health(ctx); err != nil {
		return gate, fmt.Errorf("coordinator health check failed: %w", err)
	}
	caps, err := client.GetCapabilities(ctx)
	if err != nil {
		if coordinator.IsRouteNotFound(err) {
			gate.CoordinatorVersionMismatch = true
			gate.Message = "当前 Coordinator 版本过旧，不支持 alpha3 capability 协议，请升级 VPS。"
			gate.DisableReasons["backup_upload"] = leaseUpgradeMessage()
			gate.DisableReasons["backup_restore"] = leaseUpgradeMessage()
			gate.DisableReasons["takeover"] = leaseUpgradeMessage()
			gate.DisableReasons["invite_management"] = "当前 Coordinator 版本过旧，不支持邀请管理接口，请升级 VPS。"
			return gate, coordinatorVersionMismatchError()
		}
		return gate, err
	}
	gate.CapabilitiesAvailable = true
	gate.Capabilities = &caps
	gate.LeaseRenewSupported = caps.Supports(CapabilityLeaseRenew)
	gate.InviteManagementSupported = caps.Supports(CapabilityInviteManagement)
	gate.WorldBackupSupported = caps.Supports(CapabilityWorldBackup)
	gate.LeaseOperationsEnabled = gate.LeaseRenewSupported
	gate.BackupUploadEnabled = gate.LeaseRenewSupported && gate.WorldBackupSupported
	gate.TakeoverEnabled = gate.LeaseRenewSupported
	gate.InviteManagementEnabled = gate.InviteManagementSupported
	if !gate.LeaseRenewSupported {
		msg := "Coordinator 不支持 lease_renew_v1，已禁用需要活跃 Host Lease 的操作。"
		gate.Message = msg
		gate.DisableReasons["backup_upload"] = msg
		gate.DisableReasons["backup_restore"] = msg
		gate.DisableReasons["takeover"] = msg
	}
	if !gate.InviteManagementSupported {
		gate.DisableReasons["invite_management"] = "当前 Coordinator 不支持 invite_management_v1，邀请管理已禁用。"
	}
	return gate, nil
}

func leaseUpgradeMessage() string {
	return "当前公网 Coordinator 不支持 alpha3 Lease 接口，请先升级 VPS Coordinator 后再执行备份/接管操作。"
}

func coordinatorVersionMismatchError() error {
	return &coordinator.APIError{
		StatusCode: 404,
		Code:       "coordinator_version_mismatch",
		Message:    "当前 Coordinator 版本过旧，不支持 alpha3 capability 协议，请升级 VPS。",
	}
}

func coordinatorCapabilityRouteMissingError(route string) error {
	return &coordinator.APIError{
		StatusCode: 404,
		Code:       "coordinator_capability_route_missing",
		Message:    fmt.Sprintf("Coordinator 声称支持相关能力，但路由缺失：%s。请重新升级 VPS Coordinator。", route),
	}
}

func ensureLeaseRouteAvailable(ctx context.Context, client *coordinator.Client, auth coordinator.ArtifactAuth, gate *CoordinatorFeatureGate) error {
	if gate != nil && !gate.LeaseRenewSupported {
		return unsupportedCapabilityError("lease_renew_v1")
	}
	if _, err := client.GetLeaseStatus(ctx, auth); err != nil {
		if coordinator.IsRouteNotFound(err) {
			if gate != nil {
				gate.CapabilityRouteMissing = true
				gate.LeaseOperationsEnabled = false
				gate.BackupUploadEnabled = false
				gate.TakeoverEnabled = false
				gate.DisableReasons["backup_upload"] = leaseUpgradeMessage()
				gate.DisableReasons["backup_restore"] = leaseUpgradeMessage()
				gate.DisableReasons["takeover"] = leaseUpgradeMessage()
			}
			return coordinatorCapabilityRouteMissingError("GET /v1/groups/:groupId/lease/status")
		}
		return err
	}
	return nil
}

func ensureActiveLeaseIfSupported(ctx context.Context, client *coordinator.Client, auth coordinator.ArtifactAuth, generation *int, gate *CoordinatorFeatureGate) (coordinator.EnsureActiveLeaseResponse, error) {
	if gate != nil && !gate.LeaseRenewSupported {
		return coordinator.EnsureActiveLeaseResponse{}, unsupportedCapabilityError("lease_renew_v1")
	}
	out, err := client.EnsureActiveLease(ctx, auth, generation)
	if err != nil && coordinator.IsRouteNotFound(err) {
		if gate != nil {
			gate.CapabilityRouteMissing = true
			gate.LeaseOperationsEnabled = false
			gate.BackupUploadEnabled = false
			gate.TakeoverEnabled = false
		}
		return coordinator.EnsureActiveLeaseResponse{}, coordinatorCapabilityRouteMissingError("POST /v1/groups/:groupId/lease/ensure-active")
	}
	return out, err
}

func unsupportedCapabilityError(name string) error {
	return &coordinator.APIError{
		StatusCode: 400,
		Code:       "unsupported_capability",
		Message:    fmt.Sprintf("Coordinator 不支持 %s。", name),
	}
}

func loadCoordinatorFeatureGate(ctx context.Context, opts Options) (CoordinatorFeatureGate, *coordinator.Client, coordinator.ArtifactAuth, error) {
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return CoordinatorFeatureGate{}, nil, coordinator.ArtifactAuth{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return CoordinatorFeatureGate{}, nil, coordinator.ArtifactAuth{}, err
	}
	auth := coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}
	gate, err := ProbeCoordinatorCapabilities(ctx, cfg.CoordinatorURL)
	return gate, client, auth, err
}