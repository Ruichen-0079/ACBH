package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

type BootstrapStepState string

const (
	BootstrapStepPending BootstrapStepState = "pending"
	BootstrapStepOK      BootstrapStepState = "ok"
	BootstrapStepWarning BootstrapStepState = "warning"
	BootstrapStepFailed  BootstrapStepState = "failed"
	BootstrapStepSkipped BootstrapStepState = "skipped"
)

type BootstrapStep struct {
	Name       string             `json:"name"`
	State      BootstrapStepState `json:"state"`
	Critical   bool               `json:"critical"`
	StartedAt  string             `json:"startedAt,omitempty"`
	FinishedAt string             `json:"finishedAt,omitempty"`
	Message    string             `json:"message,omitempty"`
	ErrorCode  string             `json:"errorCode,omitempty"`
}

type BootstrapWarning struct {
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	ErrorCode   string `json:"errorCode,omitempty"`
	Action      string `json:"action,omitempty"`
	ActionLabel string `json:"actionLabel,omitempty"`
}

type BootstrapResult struct {
	OK           bool                         `json:"ok"`
	Outcome      OperationOutcome             `json:"outcome"`
	State        string                       `json:"state"`
	Message      string                       `json:"message"`
	ErrorCode    string                       `json:"errorCode,omitempty"`
	Steps        []BootstrapStep              `json:"steps"`
	Warnings     []string                     `json:"warnings,omitempty"`
	Classified   []BootstrapWarning           `json:"classifiedWarnings,omitempty"`
	Capabilities *coordinator.Capabilities    `json:"capabilities,omitempty"`
	FeatureGate  *CoordinatorFeatureGate      `json:"featureGate,omitempty"`
	Identity     *coordinator.WhoAmIResponse  `json:"identity,omitempty"`
	Lease        *coordinator.HostLeaseStatus `json:"lease,omitempty"`
}

func RunBootstrap(ctx OperationContext, opts Options) (BootstrapResult, error) {
	opts = withDefaults(opts)
	result := BootstrapResult{
		OK:      true,
		Outcome: OutcomeSuccess,
		State:   "ready",
	}
	var featureGate CoordinatorFeatureGate
	var cfgLoaded bool
	var auth coordinator.ArtifactAuth
	var coordURL string

	addWarning := func(severity, message, errorCode, action, actionLabel string) {
		result.Warnings = append(result.Warnings, message)
		result.Classified = append(result.Classified, BootstrapWarning{
			Severity: severity, Message: message, ErrorCode: errorCode, Action: action, ActionLabel: actionLabel,
		})
	}
	add := func(step BootstrapStep) {
		result.Steps = append(result.Steps, step)
		switch step.State {
		case BootstrapStepWarning:
			addWarning(warningSeverityForCode(step.ErrorCode), step.Message, step.ErrorCode, warningActionForCode(step.ErrorCode), warningActionLabelForCode(step.ErrorCode))
		case BootstrapStepFailed:
			if step.Critical {
				result.OK = false
				result.Outcome = OutcomeFailure
				result.State = "failed"
				if step.ErrorCode != "" {
					result.ErrorCode = step.ErrorCode
				}
			} else if result.State == "ready" {
				result.State = "degraded"
			}
			addWarning("needs_attention", step.Message, step.ErrorCode, warningActionForCode(step.ErrorCode), warningActionLabelForCode(step.ErrorCode))
		}
	}
	finalizeOutcome := func() {
		if !result.OK {
			result.Outcome = OutcomeFailure
			return
		}
		if len(result.Warnings) > 0 || result.State == "degraded" {
			result.Outcome = OutcomeSuccessWithWarnings
			return
		}
		result.Outcome = OutcomeSuccess
	}

	run := func(name string, critical bool, timeout time.Duration, fn func(context.Context) (string, string, error)) {
		if !result.OK && critical {
			add(BootstrapStep{Name: name, Critical: critical, State: BootstrapStepSkipped, Message: "前置关键步骤失败，已跳过。"})
			return
		}
		ctx.Progress(name, "运行 "+name, 0, 0)
		started := time.Now().UTC()
		child, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		msg, code, err := fn(child)
		step := BootstrapStep{
			Name: name, Critical: critical, State: BootstrapStepOK,
			StartedAt: started.Format(time.RFC3339Nano), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: msg, ErrorCode: code,
		}
		if err != nil {
			step.State = BootstrapStepFailed
			step.Message = humanBootstrapError(err)
			if code == "" {
				step.ErrorCode = bootstrapErrorCode(err)
			}
		} else if code != "" {
			step.State = BootstrapStepWarning
			if step.Message == "" {
				step.Message = msg
			}
		}
		add(step)
	}

	run("RuntimeCheck", true, 15*time.Second, func(context.Context) (string, string, error) {
		report, err := CheckEnvironment(opts)
		if err != nil {
			return "", "runtime_check_failed", err
		}
		if !report.OK {
			return "运行环境可用但存在需要处理的检查项。", "runtime_degraded", nil
		}
		return "运行环境已就绪。", "", nil
	})
	run("LoadLocalConfig", false, 10*time.Second, func(context.Context) (string, string, error) {
		local, err := loadDesktopConfig(opts)
		if err != nil {
			return "", "not_configured", err
		}
		cfgLoaded = true
		auth = coordinator.ArtifactAuth{GroupID: local.GroupID, HostID: local.HostID, HostToken: local.HostToken}
		coordURL = local.CoordinatorURL
		return "本地配置已加载。", "", nil
	})
	run("EnvironmentCheck", false, 15*time.Second, func(context.Context) (string, string, error) {
		report, err := CheckEnvironment(opts)
		if err != nil {
			return "", "environment_check_failed", err
		}
		if len(report.Warnings) > 0 {
			return "环境检查完成，有 warning。", "environment_warnings", nil
		}
		return "环境检查完成。", "", nil
	})
	run("CoordinatorHandshake", false, 10*time.Second, func(stepCtx context.Context) (string, string, error) {
		if !cfgLoaded || coordURL == "" {
			return "尚未配置 Coordinator。", "not_configured", nil
		}
		gate, err := ProbeCoordinatorCapabilities(stepCtx, coordURL)
		featureGate = gate
		result.FeatureGate = &featureGate
		if err != nil {
			if coordinator.IsCoordinatorVersionMismatch(err) {
				return gate.Message, "coordinator_version_mismatch", nil
			}
			return "", "capability_handshake_failed", err
		}
		result.Capabilities = gate.Capabilities
		if gate.CoordinatorVersionMismatch {
			return gate.Message, "coordinator_version_mismatch", nil
		}
		if gate.Message != "" {
			return gate.Message, "unsupported_capability", nil
		}
		return fmt.Sprintf("Coordinator %s protocol=%d。", gate.Capabilities.CoordinatorVersion, gate.Capabilities.ProtocolVersion), "", nil
	})
	run("ResolveIdentity", false, 10*time.Second, func(stepCtx context.Context) (string, string, error) {
		if !cfgLoaded || coordURL == "" {
			return "尚未配置身份。", "not_configured", nil
		}
		if featureGate.CoordinatorVersionMismatch {
			return "Coordinator 版本过旧，跳过身份确认。", "coordinator_version_mismatch", nil
		}
		client, err := coordinator.NewClient(coordURL)
		if err != nil {
			return "", "coordinator_url_invalid", err
		}
		who, err := client.WhoAmI(stepCtx, auth)
		if err != nil {
			if coordinator.IsRouteNotFound(err) {
				return "Coordinator 缺少 whoami 路由。", "coordinator_capability_route_missing", nil
			}
			return "", "identity_resolve_failed", err
		}
		result.Identity = &who
		desktopCfg, _ := LoadDesktopConfig(opts)
		if desktopCfg.Group.MemberID != "" && desktopCfg.Group.MemberID != who.MemberID {
			return "本地身份与服务端不一致。", "identity_mismatch", nil
		}
		return "身份已由 Coordinator 确认。", "", nil
	})
	run("RefreshServerStatus", false, 15*time.Second, func(stepCtx context.Context) (string, string, error) {
		if _, err := CurrentStatus(stepCtx, opts); err != nil {
			return "", "status_refresh_failed", err
		}
		return "服务器状态已刷新。", "", nil
	})
	run("RefreshLeaseStatus", false, 10*time.Second, func(stepCtx context.Context) (string, string, error) {
		if !cfgLoaded || coordURL == "" {
			return "尚未配置 Host。", "not_configured", nil
		}
		if featureGate.CoordinatorVersionMismatch {
			return "Coordinator 版本过旧，跳过 Lease 检查。", "coordinator_version_mismatch", nil
		}
		if !featureGate.LeaseRenewSupported {
			return "Coordinator 不支持 lease_renew_v1，已跳过 Lease 检查。", "unsupported_capability", nil
		}
		client, err := coordinator.NewClient(coordURL)
		if err != nil {
			return "", "coordinator_url_invalid", err
		}
		if err := ensureLeaseRouteAvailable(stepCtx, client, auth, &featureGate); err != nil {
			if coordinator.IsCoordinatorCapabilityRouteMissing(err) {
				result.FeatureGate = &featureGate
				return err.Error(), "coordinator_capability_route_missing", nil
			}
			return "", "lease_status_failed", err
		}
		lease, err := client.GetLeaseStatus(stepCtx, auth)
		if err != nil {
			if coordinator.IsRouteNotFound(err) {
				result.FeatureGate = &featureGate
				return coordinatorCapabilityRouteMissingError("GET /v1/groups/:groupId/lease/status").Error(), "coordinator_capability_route_missing", nil
			}
			return "", "lease_status_failed", err
		}
		result.Lease = &lease
		result.FeatureGate = &featureGate
		if !lease.LeaseValid {
			ensured, ensureErr := ensureActiveLeaseIfSupported(stepCtx, client, auth, nil, &featureGate)
			if ensureErr != nil {
				if coordinator.IsCoordinatorCapabilityRouteMissing(ensureErr) {
					result.FeatureGate = &featureGate
					return ensureErr.Error(), "coordinator_capability_route_missing", nil
				}
				return "", "lease_status_failed", ensureErr
			}
			result.Lease = &ensured.Lease
			if ensured.Lease.LeaseValid {
				if ensured.Renewed {
					return "Host lease 已续期。", "", nil
				}
				return "Host lease 已恢复有效。", "", nil
			}
			return "Host lease 当前无效。", "lease_invalid", nil
		}
		return "Host lease 有效。", "", nil
	})
	run("RefreshWorldBackupStatus", false, 15*time.Second, func(stepCtx context.Context) (string, string, error) {
		status, err := WorldBackupStatus(stepCtx, opts)
		if err != nil {
			return "", "world_status_failed", err
		}
		if status.State == "empty" {
			return "尚无世界快照，可以创建首次备份。", "", nil
		}
		return "世界备份状态已刷新。", "", nil
	})
	run("RefreshInviteCapability", false, 10*time.Second, func(context.Context) (string, string, error) {
		if featureGate.CoordinatorVersionMismatch {
			return "Coordinator 版本过旧，邀请能力未知。", "coordinator_version_mismatch", nil
		}
		if result.Capabilities == nil {
			return "无法确认邀请功能能力。", "capability_unknown", nil
		}
		if !featureGate.InviteManagementSupported {
			return "当前 Coordinator 版本不支持该功能，请先升级 VPS。", "unsupported_capability", nil
		}
		return "邀请能力可用。", "", nil
	})

	finalizeOutcome()
	switch {
	case !result.OK:
		result.Message = "桌面端初始化失败。"
	case result.State == "degraded" || len(result.Warnings) > 0:
		result.Message = "初始化完成，但有需要处理的问题。"
	default:
		result.Message = "桌面端已就绪。"
	}
	return result, nil
}

func humanBootstrapError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *coordinator.APIError
	if errorsAsCoordinatorAPI(err, &apiErr) {
		switch apiErr.Code {
		case "route_not_found", "coordinator_capability_route_missing":
			return leaseUpgradeMessage()
		case "coordinator_version_mismatch":
			return apiErr.Message
		case "unsupported_capability":
			return apiErr.Message
		}
	}
	text := err.Error()
	if strings.Contains(strings.ToLower(text), "operation_failed") {
		return leaseUpgradeMessage()
	}
	return text
}

func bootstrapErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *coordinator.APIError
	if errorsAsCoordinatorAPI(err, &apiErr) && apiErr.Code != "" {
		return apiErr.Code
	}
	code := inferErrorCode(err)
	if code == "operation_failed" && strings.Contains(strings.ToLower(err.Error()), "404") {
		return "coordinator_capability_route_missing"
	}
	return code
}

func errorsAsCoordinatorAPI(err error, target **coordinator.APIError) bool {
	var apiErr *coordinator.APIError
	if errors.As(err, &apiErr) {
		*target = apiErr
		return true
	}
	return false
}

func warningSeverityForCode(code string) string {
	switch code {
	case "coordinator_version_mismatch", "coordinator_capability_route_missing", "identity_mismatch", "unsupported_capability", "lease_invalid", "identity_resolve_failed":
		return "needs_attention"
	default:
		return "informational"
	}
}

func warningActionForCode(code string) string {
	switch code {
	case "coordinator_version_mismatch", "coordinator_capability_route_missing":
		return "upgrade_vps"
	case "identity_mismatch", "identity_resolve_failed":
		return "repair_identity"
	case "unsupported_capability", "lease_invalid":
		return "recheck_capabilities"
	default:
		return "open_diagnostics"
	}
}

func warningActionLabelForCode(code string) string {
	switch code {
	case "coordinator_version_mismatch", "coordinator_capability_route_missing":
		return "升级 VPS 指引"
	case "identity_mismatch", "identity_resolve_failed":
		return "修复身份"
	case "unsupported_capability", "lease_invalid":
		return "重新检测"
	default:
		return "打开诊断"
	}
}