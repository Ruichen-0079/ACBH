package desktop

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

const (
	minecraftMigratablePresetID   = "minecraft-migratable"
	minecraftMigratablePresetName = "Minecraft 可迁移服务端"
)

type BackupProfileResult struct {
	OK      bool                            `json:"ok"`
	Profile agentconfig.BackupProfileConfig `json:"profile"`
	Message string                          `json:"message"`
}

func CurrentBackupProfile(opts Options) (BackupProfileResult, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return BackupProfileResult{}, err
	}
	profile := cfg.BackupProfile
	if profile.ProfileID == "" && strings.TrimSpace(cfg.Server.Dir) != "" {
		profile = defaultAgentBackupProfile(cfg.Server.Dir)
	}
	return BackupProfileResult{OK: true, Profile: profile, Message: "备份策略已读取。"}, nil
}

func UseBackupPreset(opts Options, presetID string, includeFiles []string, includeDirs []string) (BackupProfileResult, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return BackupProfileResult{}, err
	}
	if strings.TrimSpace(presetID) == "" {
		presetID = minecraftMigratablePresetID
	}
	if presetID != minecraftMigratablePresetID {
		return BackupProfileResult{OK: false, Profile: cfg.BackupProfile, Message: "未知备份预设：" + presetID}, nil
	}
	serverDir := firstNonEmpty(cfg.Server.Dir, cfg.BackupProfile.ServerDir)
	if strings.TrimSpace(serverDir) == "" {
		if setup, setupErr := LoadSetup(opts); setupErr == nil {
			serverDir = setup.ServerDir
		}
	}
	profile := cfg.BackupProfile
	if profile.ProfileID == "" {
		profile = defaultAgentBackupProfile(serverDir)
	}
	profile.ProfileID = minecraftMigratablePresetID
	profile.ProfileName = minecraftMigratablePresetName
	profile.PresetID = minecraftMigratablePresetID
	profile.PresetName = minecraftMigratablePresetName + "（推荐）"
	profile.ServerDir = serverDir
	profile.IncludeFiles = mergeUnique(profile.IncludeFiles, includeFiles)
	profile.IncludeDirs = mergeUnique(profile.IncludeDirs, includeDirs)
	if len(profile.WorldRoots) == 0 {
		profile.WorldRoots = []string{"world"}
	}
	profile.UpdatedAt = time.Now().Format(time.RFC3339)
	cfg.BackupProfile = profile
	if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
		return BackupProfileResult{}, err
	}
	return BackupProfileResult{OK: true, Profile: profile, Message: "备份预设已保存到 profile。"}, nil
}

func migrateBackupProfileServerDir(profile *agentconfig.BackupProfileConfig, serverDir string) {
	if profile == nil || profile.ProfileID == "" || strings.TrimSpace(serverDir) == "" {
		return
	}
	if strings.TrimSpace(profile.ServerDir) == "" {
		profile.ServerDir = serverDir
		return
	}
	if samePath(profile.ServerDir, serverDir) {
		profile.ServerDir = serverDir
		return
	}
	old := profile.ServerDir
	profile.ServerDir = serverDir
	profile.MigrationNotice = fmt.Sprintf("备份 profile 已从 %s 迁移到 %s；自定义文件/目录已保留。", old, serverDir)
	profile.UpdatedAt = time.Now().Format(time.RFC3339)
}

func defaultAgentBackupProfile(serverDir string) agentconfig.BackupProfileConfig {
	return agentconfig.BackupProfileConfig{
		ProfileID:   minecraftMigratablePresetID,
		ProfileName: minecraftMigratablePresetName,
		PresetID:    minecraftMigratablePresetID,
		PresetName:  minecraftMigratablePresetName + "（推荐）",
		ServerDir:   serverDir,
		WorldRoots:  []string{"world"},
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}
}

func mergeUnique(existing []string, added []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+len(added))
	for _, value := range append(existing, added...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func samePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(filepath.Clean(a))
	bAbs, bErr := filepath.Abs(filepath.Clean(b))
	if aErr == nil {
		a = aAbs
	}
	if bErr == nil {
		b = bAbs
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
