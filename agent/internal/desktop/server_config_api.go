package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

type ServerConfigPayload struct {
	ServerDir            string   `json:"serverDir"`
	LaunchType           string   `json:"launchType"`
	LaunchPath           string   `json:"launchPath"`
	JavaPath             string   `json:"javaPath"`
	WorkingDir           string   `json:"workingDir"`
	StartArgs            []string `json:"startArgs"`
	StartTimeoutSeconds  int      `json:"startTimeoutSeconds"`
}

type SaveServerConfigResult struct {
	OK        bool                      `json:"ok"`
	Outcome   OperationOutcome          `json:"outcome"`
	Message   string                    `json:"message"`
	ErrorCode string                    `json:"errorCode,omitempty"`
	Field     string                    `json:"field,omitempty"`
	TraceID   string                    `json:"traceId,omitempty"`
	Server    ServerConfigPayload       `json:"server,omitempty"`
	Saved     ServerConfigPayload       `json:"saved,omitempty"`
	Backup    *BackupProfileEnsureResult `json:"backup,omitempty"`
}

func LoadServerConfigPayload(opts Options) (ServerConfigPayload, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			desktopCfg, desktopErr := LoadDesktopConfig(opts)
			return serverPayloadFromDesktop(desktopCfg, desktopErr), nil
		}
		return ServerConfigPayload{}, err
	}
	return serverPayloadFromAgent(cfg.Server, cfg.Server.Dir), nil
}

func serverPayloadFromDesktop(desktopCfg DesktopConfig, desktopErr error) ServerConfigPayload {
	if desktopErr != nil {
		return ServerConfigPayload{}
	}
	if strings.TrimSpace(desktopCfg.LastServerDir) == "" {
		return ServerConfigPayload{}
	}
	payload := ServerConfigPayload{
		ServerDir: desktopCfg.LastServerDir,
		LaunchType: firstNonEmpty(desktopCfg.LaunchProfile.Kind, "jar"),
		LaunchPath: desktopCfg.LaunchProfile.Path,
		JavaPath:   desktopCfg.JavaPath,
		WorkingDir: desktopCfg.LastServerDir,
		StartTimeoutSeconds: 120,
	}
	return payload
}

func SaveServerConfigValidated(opts Options, payload ServerConfigPayload, traceID string) (SaveServerConfigResult, error) {
	opts = withDefaults(opts)
	result := SaveServerConfigResult{OK: false, Outcome: OutcomeFailure, TraceID: traceID}
	payload.ServerDir = strings.TrimSpace(payload.ServerDir)
	if payload.ServerDir == "" {
		result.ErrorCode = "validation_failed"
		result.Field = "serverDir"
		result.Message = "serverDir 不能为空。"
		return result, nil
	}
	if err := validateExistingPath(payload.ServerDir, true); err != nil {
		result.ErrorCode = "validation_failed"
		result.Field = "serverDir"
		result.Message = err.Error()
		return result, nil
	}
	if payload.LaunchPath != "" {
		launchPath := payload.LaunchPath
		if !filepath.IsAbs(launchPath) {
			launchPath = filepath.Join(payload.ServerDir, launchPath)
		}
		if err := validateExistingPath(launchPath, false); err != nil {
			result.ErrorCode = "validation_failed"
			result.Field = "launchPath"
			result.Message = err.Error()
			return result, nil
		}
	}
	if payload.JavaPath != "" {
		if err := validateExistingPath(payload.JavaPath, false); err != nil {
			result.ErrorCode = "validation_failed"
			result.Field = "javaPath"
			result.Message = err.Error()
			return result, nil
		}
	}
	expected := payload
	if strings.TrimSpace(expected.WorkingDir) == "" {
		expected.WorkingDir = expected.ServerDir
	}
	if expected.StartTimeoutSeconds <= 0 {
		expected.StartTimeoutSeconds = 120
	}
	if err := syncDesktopConfig(opts, func(dcfg *DesktopConfig) {
		dcfg.LastServerDir = expected.ServerDir
		if expected.LaunchPath != "" {
			dcfg.LaunchProfile = DesktopLaunchProfile{Kind: expected.LaunchType, Path: expected.LaunchPath}
		}
		dcfg.JavaPath = expected.JavaPath
	}); err != nil {
		result.ErrorCode = "save_failed"
		result.Message = err.Error()
		return result, nil
	}
	desktopCfg, desktopErr := LoadDesktopConfig(opts)
	saved := serverPayloadFromDesktop(desktopCfg, desktopErr)
	if !serverPayloadEqual(saved, expected) {
		result.ErrorCode = "read_back_mismatch"
		result.Field = "serverDir"
		result.Message = fmt.Sprintf("保存后回读不一致：提交 %q，读取 %q", expected.ServerDir, saved.ServerDir)
		return result, nil
	}
	configPath := filepath.Join(opts.AppDataDir, agentconfig.FileName)
	cfg, err := agentconfig.Load(configPath)
	agentReady := err == nil && cfg.Validate() == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if agentReady {
		cfg.Server = agentServerFromPayload(expected)
		if err := agentconfig.Save(configPath, cfg); err != nil {
			result.ErrorCode = "save_failed"
			result.Message = err.Error()
			return result, nil
		}
		readBack, err := agentconfig.Load(configPath)
		if err != nil {
			result.ErrorCode = "read_back_failed"
			result.Message = err.Error()
			return result, nil
		}
		agentSaved := serverPayloadFromAgent(readBack.Server, readBack.Server.Dir)
		if !serverPayloadEqual(agentSaved, expected) {
			result.ErrorCode = "read_back_mismatch"
			result.Field = "serverDir"
			result.Message = fmt.Sprintf("保存后回读不一致：提交 %q，读取 %q", expected.ServerDir, agentSaved.ServerDir)
			return result, nil
		}
		saved = agentSaved
	}
	if backup, err := EnsureBackupProfileForServerDir(opts, saved.ServerDir); err == nil {
		result.Backup = &backup
		if backup.Message != "" {
			result.Message = "服务器配置已保存。" + backup.Message
		} else {
			result.Message = "服务器配置已保存。"
		}
	} else {
		result.Message = "服务器配置已保存，但备份配置同步失败：" + err.Error()
	}
	result.OK = true
	result.Outcome = OutcomeSuccess
	result.Server = saved
	result.Saved = saved
	return result, nil
}

func validateExistingPath(path string, mustBeDir bool) error {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("路径不存在：%s", path)
		}
		return err
	}
	if mustBeDir && !info.IsDir() {
		return fmt.Errorf("路径必须是目录：%s", path)
	}
	if !mustBeDir && info.IsDir() {
		return fmt.Errorf("路径必须是文件：%s", path)
	}
	return nil
}

func serverPayloadFromAgent(server agentconfig.ServerConfig, fallbackDir string) ServerConfigPayload {
	dir := firstNonEmpty(server.Dir, fallbackDir)
	timeout := 120
	if d, err := server.ResolvedStartTimeout(); err == nil {
		timeout = int(d.Seconds())
	}
	return ServerConfigPayload{
		ServerDir: dir, LaunchType: server.LaunchType, LaunchPath: firstNonEmpty(server.LaunchPath, server.Command),
		JavaPath: server.JavaPath, WorkingDir: firstNonEmpty(server.WorkingDir, dir),
		StartArgs: server.StartArgs, StartTimeoutSeconds: timeout,
	}
}

func agentServerFromPayload(payload ServerConfigPayload) agentconfig.ServerConfig {
	timeout := payload.StartTimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	command := payload.LaunchPath
	if payload.LaunchType == "custom" && len(payload.StartArgs) > 0 {
		command = strings.Join(payload.StartArgs, " ")
	}
	return agentconfig.ServerConfig{
		Dir: payload.ServerDir, LaunchType: payload.LaunchType, LaunchPath: payload.LaunchPath,
		Command: command, JavaPath: payload.JavaPath, WorkingDir: firstNonEmpty(payload.WorkingDir, payload.ServerDir),
		StartArgs: payload.StartArgs, StartTimeout: fmt.Sprintf("%ds", timeout),
	}
}

func serverPayloadEqual(a, b ServerConfigPayload) bool {
	return a.ServerDir == b.ServerDir &&
		a.LaunchType == b.LaunchType &&
		a.LaunchPath == b.LaunchPath &&
		a.JavaPath == b.JavaPath &&
		a.WorkingDir == b.WorkingDir &&
		strings.Join(a.StartArgs, "\x00") == strings.Join(b.StartArgs, "\x00") &&
		a.StartTimeoutSeconds == b.StartTimeoutSeconds
}