package desktop

import (
	"context"
	"fmt"
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

type BootstrapResult struct {
	OK           bool                         `json:"ok"`
	State        string                       `json:"state"`
	Message      string                       `json:"message"`
	Steps        []BootstrapStep              `json:"steps"`
	Warnings     []string                     `json:"warnings,omitempty"`
	Capabilities *coordinator.Capabilities    `json:"capabilities,omitempty"`
	Identity     *coordinator.WhoAmIResponse  `json:"identity,omitempty"`
	Lease        *coordinator.HostLeaseStatus `json:"lease,omitempty"`
}

func RunBootstrap(ctx OperationContext, opts Options) (BootstrapResult, error) {
	opts = withDefaults(opts)
	result := BootstrapResult{OK: true, State: "ready"}
	add := func(step BootstrapStep) {
		result.Steps = append(result.Steps, step)
		if step.State == BootstrapStepWarning {
			result.Warnings = append(result.Warnings, step.Message)
		}
		if step.State == BootstrapStepFailed {
			if step.Critical {
				result.OK = false
				result.State = "failed"
			} else if result.State == "ready" {
				result.State = "degraded"
			}
			result.Warnings = append(result.Warnings, step.Message)
		}
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
			Name:       name,
			Critical:   critical,
			State:      BootstrapStepOK,
			StartedAt:  started.Format(time.RFC3339Nano),
			FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message:    msg,
			ErrorCode:  code,
		}
		if err != nil {
			step.State = BootstrapStepFailed
			step.Message = err.Error()
			if code == "" {
				step.ErrorCode = inferErrorCode(err)
			}
		}
		add(step)
	}

	var cfgLoaded bool
	var auth coordinator.ArtifactAuth
	var coordURL string
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
		client, err := coordinator.NewClient(coordURL)
		if err != nil {
			return "", "coordinator_url_invalid", err
		}
		caps, err := client.GetCapabilities(stepCtx)
		if err != nil {
			return "", "capability_handshake_failed", err
		}
		result.Capabilities = &caps
		return fmt.Sprintf("Coordinator %s protocol=%d。", caps.CoordinatorVersion, caps.ProtocolVersion), "", nil
	})
	run("ResolveIdentity", false, 10*time.Second, func(stepCtx context.Context) (string, string, error) {
		if !cfgLoaded || coordURL == "" {
			return "尚未配置身份。", "not_configured", nil
		}
		client, err := coordinator.NewClient(coordURL)
		if err != nil {
			return "", "coordinator_url_invalid", err
		}
		who, err := client.WhoAmI(stepCtx, auth)
		if err != nil {
			return "", "identity_resolve_failed", err
		}
		result.Identity = &who
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
		client, err := coordinator.NewClient(coordURL)
		if err != nil {
			return "", "coordinator_url_invalid", err
		}
		lease, err := client.GetLeaseStatus(stepCtx, auth)
		if err != nil {
			return "", "lease_status_failed", err
		}
		result.Lease = &lease
		if !lease.LeaseValid {
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
		if result.Capabilities == nil {
			return "无法确认邀请功能能力。", "capability_unknown", nil
		}
		if !result.Capabilities.Supports("invite_management_v1") {
			return "当前 Coordinator 版本不支持该功能，请先升级 VPS。", "unsupported_capability", nil
		}
		return "邀请能力可用。", "", nil
	})
	if result.State == "ready" {
		result.Message = "桌面端已就绪。"
	} else if result.State == "degraded" {
		result.Message = "桌面端已进入降级可操作状态。"
	} else {
		result.Message = "桌面端初始化失败。"
	}
	return result, nil
}
