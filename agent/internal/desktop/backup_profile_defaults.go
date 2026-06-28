package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

type BackupProfileEnsureResult struct {
	Profile       BackupProfile `json:"profile"`
	Created       bool          `json:"created"`
	Migrated      bool          `json:"migrated"`
	PreviousDir   string        `json:"previousServerDir,omitempty"`
	Message       string        `json:"message,omitempty"`
	WorldPath     string        `json:"worldPath,omitempty"`
	LevelName     string        `json:"levelName,omitempty"`
	WorldPending  bool          `json:"worldPending,omitempty"`
}

func EnsureBackupProfileForServerDir(opts Options, serverDir string) (BackupProfileEnsureResult, error) {
	opts = withDefaults(opts)
	serverDir = strings.TrimSpace(serverDir)
	if serverDir == "" {
		return BackupProfileEnsureResult{}, fmt.Errorf("serverDir is required")
	}
	abs, err := filepath.Abs(serverDir)
	if err != nil {
		return BackupProfileEnsureResult{}, err
	}
	desired, err := BuildBackupProfileFromPreset(abs, BackupPresetMigratable)
	if err != nil {
		return BackupProfileEnsureResult{}, err
	}
	levelName, _ := readServerLevelName(abs)
	worldPath := filepath.Join(abs, levelName)
	out := BackupProfileEnsureResult{
		Profile:      desired,
		WorldPath:    worldPath,
		LevelName:    levelName,
		WorldPending: !pathExists(worldPath),
	}
	doc, err := LoadBackupProfiles(opts)
	if err != nil {
		return BackupProfileEnsureResult{}, err
	}
	var existing *BackupProfile
	var existingIdx int
	for i := range doc.Profiles {
		if doc.Profiles[i].ProfileID == BackupPresetMigratable {
			existing = &doc.Profiles[i]
			existingIdx = i
			break
		}
	}
	if existing == nil && len(doc.Profiles) > 0 {
		existing = &doc.Profiles[0]
		existingIdx = 0
	}
	if existing == nil {
		if _, err := UpsertBackupProfile(opts, desired); err != nil {
			return BackupProfileEnsureResult{}, err
		}
		out.Created = true
		out.Message = "已根据 Minecraft 服务端目录创建默认备份配置。"
		out.Profile = desired
		return out, nil
	}
	prevDir := strings.TrimSpace(existing.ServerDir)
	if prevDir != "" && !pathsEqual(prevDir, abs) {
		desired.ProfileID = existing.ProfileID
		desired.Name = existing.Name
		desired.CreatedAt = existing.CreatedAt
		doc.Profiles[existingIdx] = desired
		if err := SaveBackupProfiles(opts, doc); err != nil {
			return BackupProfileEnsureResult{}, err
		}
		out.Migrated = true
		out.PreviousDir = prevDir
		out.Message = "备份配置已迁移到新的 Minecraft 服务端目录。"
		out.Profile = desired
		return out, nil
	}
	desired.ProfileID = existing.ProfileID
	desired.Name = existing.Name
	desired.CreatedAt = existing.CreatedAt
	doc.Profiles[existingIdx] = desired
	if err := SaveBackupProfiles(opts, doc); err != nil {
		return BackupProfileEnsureResult{}, err
	}
	out.Message = "备份配置已与 Minecraft 服务端目录同步。"
	out.Profile = desired
	return out, nil
}

func BackupProfileSummaryForServer(opts Options, serverDir string) (map[string]any, error) {
	opts = withDefaults(opts)
	serverDir = strings.TrimSpace(serverDir)
	if serverDir == "" {
		cfg, err := loadConfiguredServerDir(opts)
		if err != nil {
			return nil, err
		}
		if cfg == "" {
			return map[string]any{"ok": false, "message": "尚未配置 Minecraft 服务端目录。"}, nil
		}
		serverDir = cfg
	}
	abs, err := filepath.Abs(serverDir)
	if err != nil {
		return nil, err
	}
	ensure, err := EnsureBackupProfileForServerDir(opts, abs)
	if err != nil {
		return nil, err
	}
	roots := make([]BackupRootScanInfo, 0, len(ensure.Profile.Roots))
	for _, root := range ensure.Profile.Roots {
		info := BackupRootScanInfo{
			RootID: root.RootID, DisplayName: root.DisplayName, Kind: root.Kind,
			SourcePath: root.SourcePath, RestorePath: root.RestorePath,
			Enabled: root.Enabled, Required: root.Required,
		}
		switch root.Kind {
		case "file":
			info.Exists = fileExists(root.SourcePath)
		default:
			info.Exists = pathExists(root.SourcePath)
		}
		info.Pending = root.PendingIfMissing && !info.Exists
		if info.Pending {
			info.Warning = "待生成"
		} else if !info.Exists && root.Required {
			info.Warning = "缺失"
		} else if !info.Exists {
			info.Warning = "未发现"
		}
		roots = append(roots, info)
	}
	return map[string]any{
		"ok": true, "serverDir": abs, "worldPath": ensure.WorldPath, "levelName": ensure.LevelName,
		"worldPending": ensure.WorldPending, "profileId": ensure.Profile.ProfileID,
		"profileName": ensure.Profile.Name, "preset": ensure.Profile.Preset,
		"message": ensure.Message, "created": ensure.Created, "migrated": ensure.Migrated,
		"previousServerDir": ensure.PreviousDir, "roots": roots,
	}, nil
}

func loadConfiguredServerDir(opts Options) (string, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err == nil {
		if dir := strings.TrimSpace(cfg.Server.Dir); dir != "" {
			return dir, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	desktopCfg, err := LoadDesktopConfig(opts)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(desktopCfg.LastServerDir), nil
}

func pathsEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return true
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}