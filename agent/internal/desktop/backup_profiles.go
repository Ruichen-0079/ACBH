package desktop

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

const (
	backupProfilesDirName   = "backup-profiles"
	backupProfilesIndexName = "index.json"
	legacyBackupProfiles    = "backup-profiles.json"

	BackupPresetCore       = "minecraft-core"
	BackupPresetMigratable = "minecraft-migratable"
	BackupPresetOffline    = "minecraft-offline-mirror"
)

var (
	defaultBackupExcludeGlobs = []string{
		"logs/**",
		"crash-reports/**",
		"debug/**",
		"*.log",
		"*.log.gz",
		"hs_err_pid*.log",
		".previousrun",
		".mixin.out/**",
		".mixedin.out/**",
		"cache/**",
		".cache/**",
		"config/.puzzle_cache/**",
		"config/super_resolution/shader_caches/**",
		"config/spark/tmp/**",
		"config/spark/tmp-client/**",
		"**/*.tmp",
		"**/*.temp",
	}
	regenerableExcludeGlobs = []string{
		"jre/**",
		"libraries/**",
		"server.jar",
		"neoforge-*-installer.jar",
		"neoforge-*-installer.jar.log",
		"forge-*-installer.jar",
		"fabric-installer*.jar",
	}
	minecraftVersionPattern = regexp.MustCompile(`\b1\.\d+(?:\.\d+)?\b`)
	loaderVersionPattern    = regexp.MustCompile(`(?i)(?:neoforge|forge|fabric|quilt)[-/_ ]?([0-9][A-Za-z0-9_.-]*)`)
)

type BackupProfilesDocument struct {
	SchemaVersion int             `json:"schemaVersion"`
	Profiles      []BackupProfile `json:"profiles"`
}

type BackupProfile struct {
	SchemaVersion  int                 `json:"schemaVersion"`
	ProfileID      string              `json:"profileId"`
	Name           string              `json:"name"`
	Preset         string              `json:"preset"`
	ServerDir      string              `json:"serverDir"`
	Roots          []BackupProfileRoot `json:"roots"`
	ExcludeGlobs   []string            `json:"excludeGlobs"`
	FollowSymlinks bool                `json:"followSymlinks"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}

type BackupProfileRoot struct {
	RootID           string   `json:"rootId"`
	DisplayName      string   `json:"displayName"`
	Kind             string   `json:"kind"`
	SourcePath       string   `json:"sourcePath"`
	RestorePath      string   `json:"restorePath"`
	Required         bool     `json:"required"`
	Enabled          bool     `json:"enabled"`
	PendingIfMissing bool     `json:"pendingIfMissing"`
	ConsistencyGroup string   `json:"consistencyGroup,omitempty"`
	ExcludePatterns  []string `json:"excludePatterns,omitempty"`
	FollowSymlinks   bool     `json:"followSymlinks"`
}

type BackupProfileScanResult struct {
	OK               bool                 `json:"ok"`
	ProfileID        string               `json:"profileId"`
	LogicalSize      int64                `json:"logicalSize"`
	FileCount        int                  `json:"fileCount"`
	RootCount        int                  `json:"rootCount"`
	PendingRootCount int                  `json:"pendingRootCount"`
	Roots            []BackupRootScanInfo `json:"roots"`
	Warnings         []string             `json:"warnings,omitempty"`
}

type BackupRootScanInfo struct {
	RootID      string `json:"rootId"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind"`
	SourcePath  string `json:"sourcePath"`
	RestorePath string `json:"restorePath,omitempty"`
	Enabled     bool   `json:"enabled"`
	Required    bool   `json:"required"`
	Exists      bool   `json:"exists"`
	Pending     bool   `json:"pending"`
	FileCount   int    `json:"fileCount"`
	Bytes       int64  `json:"bytes"`
	Warning     string `json:"warning,omitempty"`
}

type BackupPathStats struct {
	Path      string `json:"path,omitempty"`
	Exists    bool   `json:"exists"`
	FileCount int    `json:"fileCount"`
	Bytes     int64  `json:"bytes"`
}

type BackupPresetSummary struct {
	Preset      string              `json:"preset"`
	Name        string              `json:"name"`
	Recommended bool                `json:"recommended,omitempty"`
	RootCount   int                 `json:"rootCount"`
	Roots       []BackupProfileRoot `json:"roots,omitempty"`
}

type BackupServerAnalysis struct {
	OK                 bool                  `json:"ok"`
	ServerDir          string                `json:"serverDir"`
	MinecraftVersion   string                `json:"minecraftVersion,omitempty"`
	Loader             string                `json:"loader,omitempty"`
	LoaderVersion      string                `json:"loaderVersion,omitempty"`
	LevelName          string                `json:"levelName"`
	WorldPath          string                `json:"worldPath"`
	WorldPending       bool                  `json:"worldPending"`
	Mods               BackupPathStats       `json:"mods"`
	Config             BackupPathStats       `json:"config"`
	GlobalPacks        BackupPathStats       `json:"globalPacks"`
	Libraries          BackupPathStats       `json:"libraries"`
	JRE                BackupPathStats       `json:"jre"`
	Logs               BackupPathStats       `json:"logs"`
	StartScripts       []string              `json:"startScripts,omitempty"`
	Regenerable        []string              `json:"regenerable,omitempty"`
	Caches             []string              `json:"caches,omitempty"`
	RecommendedPreset  string                `json:"recommendedPreset"`
	IncludeMods        bool                  `json:"includeMods"`
	IncludeLibraries   bool                  `json:"includeLibraries"`
	IncludeJRE         bool                  `json:"includeJRE"`
	Warnings           []string              `json:"warnings,omitempty"`
	Profiles           []BackupPresetSummary `json:"profiles"`
	RecommendedProfile BackupProfile         `json:"recommendedProfile"`
}

type BackupProfileRestoreRequest struct {
	ProfileID  string `json:"profileId"`
	SnapshotID string `json:"snapshotId"`
	Strategy   string `json:"strategy"`
	TargetDir  string `json:"targetDir"`
	RequestID  string `json:"requestId"`
}

type backupRequestIDBody struct {
	RequestID string `json:"requestId"`
}

type backupCreateRequest struct {
	ProfileID string `json:"profileId"`
	RequestID string `json:"requestId"`
}

type backupSnapshotActionRequest struct {
	SnapshotID string `json:"snapshotId"`
	RequestID  string `json:"requestId"`
}

type profileSourceFile struct {
	ManifestPath  string
	LocalPath     string
	Size          int64
	SHA256        string
	RootID        string
	MTimeUnixNano int64
}

func registerBackupTopLevelEndpoints(register routeRegistrar, manager *OperationManager, opts Options) {
	register("/api/backup/analyze-server", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			ServerDir string `json:"serverDir"`
			RequestID string `json:"requestId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupAnalyzeServer", MutexClass: "read:backup", Timeout: 5 * time.Minute, Coalesce: true, IdempotencyKey: body.RequestID}, func(ctx OperationContext) (any, error) {
			return AnalyzeBackupServer(body.ServerDir)
		})
	})
	register("/api/backup/scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body backupCreateRequest
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileScan", MutexClass: "read:backup", Timeout: 5 * time.Minute, Coalesce: true, IdempotencyKey: body.RequestID}, func(ctx OperationContext) (any, error) {
			return BackupProfileScan(opts, body.ProfileID)
		})
	})
	register("/api/backup/create", func(w http.ResponseWriter, r *http.Request) {
		handleBackupCreateOperation(w, r, manager, opts, "BackupProfileCreate")
	})
	register("/api/backup/resume", func(w http.ResponseWriter, r *http.Request) {
		handleBackupCreateOperation(w, r, manager, opts, "BackupProfileResume")
	})
	register("/api/backup/snapshots", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		out, err := WorldBackupList(r.Context(), opts)
		writeBackupResponse(w, out, err)
	})
	register("/api/backup/snapshots/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		out, err := WorldBackupLatest(r.Context(), opts)
		writeBackupResponse(w, out, err)
	})
	register("/api/backup/snapshots/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		snapshotID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/backup/snapshots/"), "/")
		if snapshotID == "" {
			http.NotFound(w, r)
			return
		}
		out, err := WorldBackupShow(r.Context(), opts, snapshotID)
		writeBackupResponse(w, out, err)
	})
	register("/api/backup/pull", func(w http.ResponseWriter, r *http.Request) {
		handleBackupRestoreOperation(w, r, manager, opts, "BackupProfilePull", "remote_overwrite")
	})
	register("/api/backup/restore", func(w http.ResponseWriter, r *http.Request) {
		handleBackupRestoreOperation(w, r, manager, opts, "BackupProfileRestore", "ask_on_conflict")
	})
	register("/api/backup/sync", func(w http.ResponseWriter, r *http.Request) {
		handleBackupRestoreOperation(w, r, manager, opts, "BackupProfileSync", "ask_on_conflict")
	})
	register("/api/backup/pin", func(w http.ResponseWriter, r *http.Request) {
		handleBackupSnapshotAdmin(w, r, manager, opts, "BackupProfilePin")
	})
	register("/api/backup/delete", func(w http.ResponseWriter, r *http.Request) {
		handleBackupSnapshotAdmin(w, r, manager, opts, "BackupProfileDelete")
	})
}

func handleBackupCreateOperation(w http.ResponseWriter, r *http.Request, manager *OperationManager, opts Options, name string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body backupCreateRequest
	if !decodeBody(w, r, &body) {
		return
	}
	startAndWrite(w, r, manager, OperationOptions{Name: name, MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute, IdempotencyKey: body.RequestID}, func(ctx OperationContext) (any, error) {
		ctx.Progress("validating", "验证备份策略", 0, 0)
		return BackupProfileCreate(ctx, opts, body.ProfileID)
	})
}

func handleBackupRestoreOperation(w http.ResponseWriter, r *http.Request, manager *OperationManager, opts Options, name, defaultStrategy string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body BackupProfileRestoreRequest
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Strategy == "" {
		body.Strategy = defaultStrategy
	}
	startAndWrite(w, r, manager, OperationOptions{Name: name, MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute, IdempotencyKey: body.RequestID}, func(ctx OperationContext) (any, error) {
		ctx.Progress("downloading", "准备恢复快照", 0, 0)
		return BackupProfileRestoreWithOptions(ctx, opts, body)
	})
}

func handleBackupSnapshotAdmin(w http.ResponseWriter, r *http.Request, manager *OperationManager, opts Options, name string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body backupSnapshotActionRequest
	if !decodeBody(w, r, &body) {
		return
	}
	op := OperationOptions{Name: name, MutexClass: "backup-admin", Timeout: 30 * time.Second, IdempotencyKey: body.RequestID}
	startAndWrite(w, r, manager, op, func(ctx OperationContext) (any, error) {
		if name == "BackupProfileDelete" {
			return WorldBackupDelete(ctx, opts, body.SnapshotID)
		}
		return WorldBackupPin(ctx, opts, body.SnapshotID)
	})
}

func writeBackupResponse(w http.ResponseWriter, value any, err error) {
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, value)
}

func decodeOptionalBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	return decodeBody(w, r, out)
}

func backupProfilesDir(opts Options) string {
	return filepath.Join(withDefaults(opts).AppDataDir, backupProfilesDirName)
}

func backupProfilesPath(opts Options) string {
	return filepath.Join(backupProfilesDir(opts), backupProfilesIndexName)
}

func legacyBackupProfilesPath(opts Options) string {
	return filepath.Join(withDefaults(opts).AppDataDir, legacyBackupProfiles)
}

func LoadBackupProfiles(opts Options) (BackupProfilesDocument, error) {
	pathName := backupProfilesPath(opts)
	var doc BackupProfilesDocument
	if err := loadJSON(pathName, &doc); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return BackupProfilesDocument{}, err
		}
		if legacyErr := loadJSON(legacyBackupProfilesPath(opts), &doc); legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
			return BackupProfilesDocument{}, legacyErr
		} else if legacyErr != nil {
			doc = BackupProfilesDocument{SchemaVersion: 2}
			if p, ok := defaultBackupProfile(opts); ok {
				doc.Profiles = append(doc.Profiles, p)
			}
			return doc, nil
		}
	}
	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = 2
	}
	for i := range doc.Profiles {
		doc.Profiles[i] = normalizeBackupProfile(doc.Profiles[i])
	}
	return doc, nil
}

func SaveBackupProfiles(opts Options, doc BackupProfilesDocument) error {
	now := time.Now().UTC()
	doc.SchemaVersion = 2
	for i := range doc.Profiles {
		created := doc.Profiles[i].CreatedAt
		doc.Profiles[i] = normalizeBackupProfile(doc.Profiles[i])
		if created.IsZero() {
			doc.Profiles[i].CreatedAt = now
		} else {
			doc.Profiles[i].CreatedAt = created
		}
		doc.Profiles[i].UpdatedAt = now
	}
	sort.Slice(doc.Profiles, func(i, j int) bool { return doc.Profiles[i].ProfileID < doc.Profiles[j].ProfileID })
	return saveJSON(backupProfilesPath(opts), doc)
}

func UpsertBackupProfile(opts Options, profile BackupProfile) (BackupProfilesDocument, error) {
	profile = normalizeBackupProfile(profile)
	if err := validateBackupProfile(profile); err != nil {
		return BackupProfilesDocument{}, err
	}
	doc, err := LoadBackupProfiles(opts)
	if err != nil {
		return BackupProfilesDocument{}, err
	}
	replaced := false
	for i := range doc.Profiles {
		if doc.Profiles[i].ProfileID == profile.ProfileID {
			if profile.CreatedAt.IsZero() {
				profile.CreatedAt = doc.Profiles[i].CreatedAt
			}
			doc.Profiles[i] = profile
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Profiles = append(doc.Profiles, profile)
	}
	return doc, SaveBackupProfiles(opts, doc)
}

func DeleteBackupProfile(opts Options, profileID string) (map[string]any, error) {
	doc, err := LoadBackupProfiles(opts)
	if err != nil {
		return nil, err
	}
	next := doc.Profiles[:0]
	for _, profile := range doc.Profiles {
		if profile.ProfileID != profileID {
			next = append(next, profile)
		}
	}
	doc.Profiles = next
	return map[string]any{"ok": true, "profileId": profileID}, SaveBackupProfiles(opts, doc)
}

func defaultBackupProfile(opts Options) (BackupProfile, bool) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil || strings.TrimSpace(cfg.Server.Dir) == "" {
		return BackupProfile{}, false
	}
	profile, err := BuildBackupProfileFromPreset(cfg.Server.Dir, BackupPresetMigratable)
	if err != nil {
		return BackupProfile{}, false
	}
	return profile, len(profile.Roots) > 0
}

func normalizeBackupProfile(profile BackupProfile) BackupProfile {
	legacy := profile.SchemaVersion == 0 || profile.SchemaVersion == 1
	profile.SchemaVersion = 2
	profile.ProfileID = rootIDFromPath(firstNonEmpty(profile.ProfileID, profile.Name, profile.Preset, "profile"))
	if profile.Name == "" {
		profile.Name = profile.ProfileID
	}
	if profile.Preset == "" {
		profile.Preset = "custom"
	}
	if len(profile.ExcludeGlobs) == 0 {
		profile.ExcludeGlobs = append([]string{}, defaultBackupExcludeGlobs...)
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now().UTC()
	}
	for i := range profile.Roots {
		root := &profile.Roots[i]
		root.RootID = rootIDFromPath(firstNonEmpty(root.RootID, root.DisplayName, filepath.Base(root.SourcePath)))
		if root.DisplayName == "" {
			root.DisplayName = root.RootID
		}
		root.Kind = normalizeBackupRootKind(root.Kind)
		if legacy && !root.Enabled {
			root.Enabled = true
		}
		if root.RestorePath == "" {
			root.RestorePath = root.SourcePath
		}
	}
	return profile
}

func normalizeBackupRootKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "file":
		return "file"
	case "folder", "manual-folder", "minecraft-world", "directory", "dir":
		return "directory"
	default:
		if kind == "" {
			return "directory"
		}
		return kind
	}
}

func validateBackupProfile(profile BackupProfile) error {
	if profile.ProfileID == "" || len(profile.Roots) == 0 {
		return errors.New("profileId and at least one root are required")
	}
	seen := map[string]struct{}{}
	for _, root := range profile.Roots {
		if _, err := worldbackup.NormalizeManifestPath(root.RootID); err != nil || strings.Contains(root.RootID, "/") {
			return fmt.Errorf("rootId %q is not safe", root.RootID)
		}
		if strings.TrimSpace(root.SourcePath) == "" {
			return fmt.Errorf("root %s sourcePath is required", root.RootID)
		}
		if root.Kind != "file" && root.Kind != "directory" {
			return fmt.Errorf("root %s kind must be file or directory", root.RootID)
		}
		if _, ok := seen[root.RootID]; ok {
			return fmt.Errorf("duplicate rootId %s", root.RootID)
		}
		seen[root.RootID] = struct{}{}
	}
	return nil
}

func AnalyzeBackupServer(serverDir string) (BackupServerAnalysis, error) {
	serverDir = strings.TrimSpace(serverDir)
	if serverDir == "" {
		return BackupServerAnalysis{}, errors.New("serverDir is required")
	}
	abs, err := filepath.Abs(serverDir)
	if err != nil {
		return BackupServerAnalysis{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return BackupServerAnalysis{}, err
	}
	if !info.IsDir() {
		return BackupServerAnalysis{}, fmt.Errorf("serverDir %s is not a directory", abs)
	}
	levelName, err := readServerLevelName(abs)
	if err != nil {
		return BackupServerAnalysis{}, err
	}
	worldPath := filepath.Join(abs, filepath.FromSlash(levelName))
	analysis := BackupServerAnalysis{
		OK:                true,
		ServerDir:         abs,
		LevelName:         levelName,
		WorldPath:         worldPath,
		WorldPending:      !pathExists(worldPath),
		RecommendedPreset: BackupPresetCore,
		Loader:            "Vanilla",
	}
	analysis.Mods = pathStats(filepath.Join(abs, "mods"))
	analysis.Config = pathStats(filepath.Join(abs, "config"))
	analysis.GlobalPacks = pathStats(filepath.Join(abs, "global_packs"))
	analysis.Libraries = pathStats(filepath.Join(abs, "libraries"))
	analysis.JRE = pathStats(filepath.Join(abs, "jre"))
	analysis.Logs = pathStats(filepath.Join(abs, "logs"))
	analysis.StartScripts = existingRelativeFiles(abs, []string{
		"start.bat", "start.cmd", "start.ps1", "start.sh",
		"run.bat", "run.cmd", "run.ps1", "run.sh",
		"双击直接开服！！！.bat",
	})
	analysis.Regenerable = existingRelativePaths(abs, []string{"libraries", "jre", "server.jar"})
	analysis.Caches = existingRelativePaths(abs, []string{
		"logs", "crash-reports", "debug", "cache", ".cache",
		"config/.puzzle_cache", "config/super_resolution/shader_caches",
	})
	detectPackHints(abs, &analysis)
	if analysis.Mods.Exists && analysis.Mods.FileCount > 0 {
		analysis.RecommendedPreset = BackupPresetMigratable
		analysis.IncludeMods = true
	}
	analysis.IncludeLibraries = false
	analysis.IncludeJRE = false
	if analysis.WorldPending {
		analysis.Warnings = append(analysis.Warnings, "世界目录尚未生成，已按 pending root 保存，首次开服后会自动纳入备份。")
	}
	for _, preset := range []string{BackupPresetCore, BackupPresetMigratable, BackupPresetOffline} {
		profile, err := BuildBackupProfileFromPreset(abs, preset)
		if err != nil {
			analysis.Warnings = append(analysis.Warnings, err.Error())
			continue
		}
		summary := BackupPresetSummary{
			Preset:      profile.Preset,
			Name:        profile.Name,
			Recommended: profile.Preset == analysis.RecommendedPreset,
			RootCount:   len(profile.Roots),
			Roots:       profile.Roots,
		}
		analysis.Profiles = append(analysis.Profiles, summary)
		if summary.Recommended {
			analysis.RecommendedProfile = profile
		}
	}
	return analysis, nil
}

func BuildBackupProfileFromPreset(serverDir, preset string) (BackupProfile, error) {
	serverDir = strings.TrimSpace(serverDir)
	if serverDir == "" {
		return BackupProfile{}, errors.New("serverDir is required")
	}
	abs, err := filepath.Abs(serverDir)
	if err != nil {
		return BackupProfile{}, err
	}
	preset = canonicalBackupPreset(preset)
	levelName, err := readServerLevelName(abs)
	if err != nil {
		return BackupProfile{}, err
	}
	now := time.Now().UTC()
	profile := BackupProfile{
		SchemaVersion: 2,
		ProfileID:     preset,
		Name:          backupPresetName(preset),
		Preset:        preset,
		ServerDir:     abs,
		ExcludeGlobs:  append([]string{}, defaultBackupExcludeGlobs...),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if preset == BackupPresetCore || preset == BackupPresetMigratable {
		profile.ExcludeGlobs = append(profile.ExcludeGlobs, regenerableExcludeGlobs...)
	}
	rootIDs := map[string]int{}
	addDir := func(rootID, display, rel string, required, pending bool) {
		source := filepath.Join(abs, filepath.FromSlash(rel))
		if !pending && !pathExists(source) {
			return
		}
		rootID = uniqueRootID(rootID, rootIDs)
		profile.Roots = append(profile.Roots, BackupProfileRoot{
			RootID: rootID, DisplayName: display, Kind: "directory",
			SourcePath: source, RestorePath: source, Required: required, Enabled: true,
			PendingIfMissing: pending, ConsistencyGroup: consistencyForRoot(rootID),
		})
	}
	addFile := func(rel string) {
		source := filepath.Join(abs, filepath.FromSlash(rel))
		if !fileExists(source) {
			return
		}
		base := filepath.Base(rel)
		rootID := uniqueRootID(rootIDFromPath(strings.TrimSuffix(base, filepath.Ext(base))), rootIDs)
		profile.Roots = append(profile.Roots, BackupProfileRoot{
			RootID: rootID, DisplayName: base, Kind: "file",
			SourcePath: source, RestorePath: source, Required: false, Enabled: true,
		})
	}
	addDir("world", "世界 "+levelName, levelName, true, true)
	for _, rel := range []string{"config", "defaultconfigs", "global_packs", "datapacks", "resourcepacks", "patchouli_books", "kubejs", "scripts"} {
		addDir(rootIDFromPath(rel), rel, rel, false, false)
	}
	for _, rel := range []string{
		"server.properties", "eula.txt", "ops.json", "whitelist.json", "banned-players.json", "banned-ips.json",
		"server-icon.png", "manifest.json", "variables.txt", "user_jvm_args.txt",
		"start.bat", "start.cmd", "start.ps1", "start.sh", "run.bat", "run.cmd", "run.ps1", "run.sh",
		"双击直接开服！！！.bat", "HOW-TO-RUN.md",
	} {
		addFile(rel)
	}
	if preset == BackupPresetMigratable || preset == BackupPresetOffline {
		addDir("mods", "mods", "mods", false, false)
	}
	if preset == BackupPresetOffline {
		addDir("libraries", "libraries", "libraries", false, false)
		addDir("jre", "jre", "jre", false, false)
		addFile("server.jar")
		addMatchingRootFiles(abs, &profile, rootIDs, []string{"neoforge-*-installer.jar", "forge-*-installer.jar", "fabric-installer*.jar"})
	}
	return normalizeBackupProfile(profile), nil
}

func BackupProfileScan(opts Options, profileID string) (BackupProfileScanResult, error) {
	profile, err := findBackupProfile(opts, profileID)
	if err != nil {
		return BackupProfileScanResult{}, err
	}
	files, roots, warnings, err := scanBackupProfileSources(profile)
	if err != nil {
		return BackupProfileScanResult{}, err
	}
	var logical int64
	pending := 0
	for _, file := range files {
		logical += file.Size
	}
	for _, root := range roots {
		if root.Pending {
			pending++
		}
	}
	return BackupProfileScanResult{
		OK: true, ProfileID: profile.ProfileID, LogicalSize: logical, FileCount: len(files),
		RootCount: len(roots), PendingRootCount: pending, Roots: roots, Warnings: warnings,
	}, nil
}

func BackupProfileCreate(ctx context.Context, opts Options, profileID string) (WorldBackupCreateResult, error) {
	opts = withDefaults(opts)
	progressContext(ctx, "validating", "验证备份策略", 0, 0)
	profile, err := findBackupProfile(opts, profileID)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	cfg, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	ensured, err := client.EnsureActiveLease(ctx, auth, nil)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	if !ensured.Lease.LeaseValid {
		return WorldBackupCreateResult{}, errors.New("lease_expired: active host lease is required before publishing backup profiles")
	}
	generation := ensured.Lease.Generation
	progressContext(ctx, "scanning", "扫描备份文件", 0, 0)
	files, roots, warnings, err := scanBackupProfileSources(profile)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	var parent *worldbackup.Manifest
	if latest, latestErr := client.GetLatestWorldBackup(ctx, auth, false); latestErr == nil {
		parent = &latest.Manifest
	} else if !coordinator.IsAPIErrorCode(latestErr, "artifact_empty") && !strings.Contains(latestErr.Error(), "No available artifact exists for this kind") {
		return WorldBackupCreateResult{}, latestErr
	}
	snapshotID := "bp_" + profile.ProfileID + "_" + time.Now().UTC().Format("20060102_150405")
	progressContext(ctx, "hashing", "计算文件哈希", int64(len(files)), int64(len(files)))
	manifest, plan, index := buildProfileSnapshotWithParent(profile, cfg, generation, snapshotID, files, parent)
	if err := worldbackup.ValidateManifest(manifest); err != nil {
		return WorldBackupCreateResult{}, err
	}
	progressContext(ctx, "planning", "规划缺失对象", int64(len(plan.Objects)), int64(len(plan.Objects)))
	planned, err := client.PlanWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupPlanRequest{
		HostID: cfg.HostID, HostToken: cfg.HostToken, HostGeneration: generation, ParentSnapshotID: manifest.ParentSnapshotID, Objects: plan.Objects,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	bySHA := worldbackup.IndexChangedFilesBySHA(plan.ChangedFiles)
	uploadFn := func(ctx context.Context, sha256 string, content io.Reader, size int64) error {
		_, err := client.UploadWorldObjectStream(ctx, auth, sha256, content, size)
		return err
	}
	progressContext(ctx, "uploading", "上传缺失对象", 0, int64(len(planned.MissingObjects)))
	if err := worldbackup.UploadMissingObjects(ctx, uploadFn, planned.MissingObjects, bySHA); err != nil {
		return WorldBackupCreateResult{}, err
	}
	progressContext(ctx, "committing", "提交备份清单", 1, 1)
	commit, err := client.CommitWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupCommitRequest{
		HostID: cfg.HostID, HostToken: cfg.HostToken, HostGeneration: generation, Manifest: manifest,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	progressContext(ctx, "saving_index", "保存本地索引", 1, 1)
	if err := worldbackup.SaveIndexAtomic(opts.AppDataDir, index); err != nil {
		return WorldBackupCreateResult{}, err
	}
	pending := 0
	for _, root := range roots {
		if root.Pending {
			pending++
		}
	}
	outcomeOK := commit.OK
	return WorldBackupCreateResult{
		OK: outcomeOK, SnapshotID: commit.SnapshotID, ProfileID: profile.ProfileID, RootCount: len(roots),
		FileCount: manifest.FileCount, MissingObjects: len(planned.MissingObjects), PendingRootCount: pending,
		LogicalSize: manifest.LogicalSize, UploadedSize: manifest.UploadedSize,
		DeduplicatedSize: manifest.LogicalSize - manifest.UploadedSize,
		ChangedFileCount: manifest.ChangedFileCount, DeletedFileCount: manifest.DeletedFileCount,
		Consistent: manifest.Consistent, Warnings: warnings, IndexPath: worldbackup.IndexPath(opts.AppDataDir),
	}, nil
}

func BackupProfileRestore(ctx context.Context, opts Options, profileID, snapshotID string) (worldbackup.RestoreSummary, error) {
	return BackupProfileRestoreWithOptions(ctx, opts, BackupProfileRestoreRequest{ProfileID: profileID, SnapshotID: snapshotID})
}

func BackupProfileRestoreWithOptions(ctx context.Context, opts Options, req BackupProfileRestoreRequest) (worldbackup.RestoreSummary, error) {
	profile, err := findBackupProfile(opts, req.ProfileID)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	var remote coordinator.WorldBackupManifestResponse
	if strings.TrimSpace(req.SnapshotID) == "" || req.SnapshotID == "latest" {
		remote, err = client.GetLatestWorldBackup(ctx, auth, true)
	} else {
		remote, err = client.GetWorldBackup(ctx, auth, req.SnapshotID)
	}
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	if req.Strategy == "download_to_new_directory" {
		if strings.TrimSpace(req.TargetDir) == "" {
			return worldbackup.RestoreSummary{}, errors.New("targetDir is required for download_to_new_directory")
		}
		profile = profileForDownloadTarget(profile, req.TargetDir)
	}
	return restoreProfileManifest(ctx, profile, remote.Manifest, worldObjectDownloader(client, auth))
}

func WorldBackupLatest(ctx context.Context, opts Options) (coordinator.WorldBackupManifestResponse, error) {
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return coordinator.WorldBackupManifestResponse{}, err
	}
	return client.GetLatestWorldBackup(ctx, auth, false)
}

func findBackupProfile(opts Options, profileID string) (BackupProfile, error) {
	doc, err := LoadBackupProfiles(opts)
	if err != nil {
		return BackupProfile{}, err
	}
	profileID = rootIDFromPath(profileID)
	for _, profile := range doc.Profiles {
		if profile.ProfileID == profileID {
			return normalizeBackupProfile(profile), nil
		}
	}
	return BackupProfile{}, fmt.Errorf("backup profile %s not found", profileID)
}

func scanBackupProfileSources(profile BackupProfile) ([]profileSourceFile, []BackupRootScanInfo, []string, error) {
	profile = normalizeBackupProfile(profile)
	if err := validateBackupProfile(profile); err != nil {
		return nil, nil, nil, err
	}
	var files []profileSourceFile
	var roots []BackupRootScanInfo
	var warnings []string
	for _, root := range profile.Roots {
		info := BackupRootScanInfo{
			RootID: root.RootID, DisplayName: root.DisplayName, Kind: root.Kind,
			SourcePath: root.SourcePath, RestorePath: root.RestorePath, Enabled: root.Enabled, Required: root.Required,
		}
		if !root.Enabled {
			roots = append(roots, info)
			continue
		}
		sourceRoot, err := filepath.Abs(root.SourcePath)
		if err != nil {
			return nil, nil, nil, err
		}
		sourceRoot = filepath.Clean(sourceRoot)
		stat, err := os.Lstat(sourceRoot)
		if err != nil {
			if root.PendingIfMissing {
				info.Pending = true
				info.Warning = "备份 root 尚未生成：" + root.SourcePath
				warnings = append(warnings, info.Warning)
				roots = append(roots, info)
				continue
			}
			if root.Required {
				return nil, nil, nil, err
			}
			info.Warning = "可选备份 root 不存在：" + root.SourcePath
			warnings = append(warnings, info.Warning)
			roots = append(roots, info)
			continue
		}
		info.Exists = true
		if stat.Mode()&os.ModeSymlink != 0 && !root.FollowSymlinks {
			return nil, nil, nil, fmt.Errorf("backup root %s is a symlink and followSymlinks is disabled", root.RootID)
		}
		if root.Kind == "file" || !stat.IsDir() {
			file, ok, err := scanSingleProfileFile(profile, root, sourceRoot)
			if err != nil {
				return nil, nil, nil, err
			}
			if ok {
				files = append(files, file)
				info.FileCount = 1
				info.Bytes = file.Size
			}
			roots = append(roots, info)
			continue
		}
		patterns := backupExcludePatterns(profile, root)
		err = filepath.WalkDir(sourceRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filePath == sourceRoot {
				return nil
			}
			rel, err := filepath.Rel(sourceRoot, filePath)
			if err != nil {
				return err
			}
			relSlash := filepath.ToSlash(rel)
			if entry.Type()&os.ModeSymlink != 0 && !root.FollowSymlinks {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if shouldExcludeBackup(relSlash, entry.IsDir(), patterns) || shouldExcludeBackup(path.Join(root.RootID, relSlash), entry.IsDir(), patterns) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			real := filePath
			if root.FollowSymlinks {
				if resolved, err := filepath.EvalSymlinks(filePath); err == nil {
					if !underDesktopRoot(sourceRoot, resolved) {
						return fmt.Errorf("symlink %s escapes backup root", relSlash)
					}
					real = resolved
				}
			}
			stat, err := os.Stat(real)
			if err != nil {
				return err
			}
			sum, err := sha256FileLocal(real)
			if err != nil {
				return err
			}
			manifestPath, err := worldbackup.NormalizeManifestPath(path.Join(root.RootID, relSlash))
			if err != nil {
				return err
			}
			files = append(files, profileSourceFile{
				ManifestPath: manifestPath, LocalPath: real, Size: stat.Size(), SHA256: sum,
				RootID: root.RootID, MTimeUnixNano: stat.ModTime().UnixNano(),
			})
			info.FileCount++
			info.Bytes += stat.Size()
			return nil
		})
		if err != nil {
			return nil, nil, nil, err
		}
		roots = append(roots, info)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ManifestPath < files[j].ManifestPath })
	return files, roots, warnings, nil
}

func scanSingleProfileFile(profile BackupProfile, root BackupProfileRoot, source string) (profileSourceFile, bool, error) {
	patterns := backupExcludePatterns(profile, root)
	real := source
	if root.FollowSymlinks {
		if resolved, err := filepath.EvalSymlinks(source); err == nil {
			if !underDesktopRoot(filepath.Dir(source), resolved) {
				return profileSourceFile{}, false, fmt.Errorf("symlink root %s escapes parent directory", root.RootID)
			}
			real = resolved
		}
	}
	stat, err := os.Stat(real)
	if err != nil {
		return profileSourceFile{}, false, err
	}
	if stat.IsDir() {
		return profileSourceFile{}, false, fmt.Errorf("backup root %s is a directory but kind=file", root.RootID)
	}
	base := filepath.Base(source)
	if shouldExcludeBackup(base, false, patterns) {
		return profileSourceFile{}, false, nil
	}
	sum, err := sha256FileLocal(real)
	if err != nil {
		return profileSourceFile{}, false, err
	}
	manifestPath, err := worldbackup.NormalizeManifestPath(path.Join(root.RootID, filepath.ToSlash(base)))
	if err != nil {
		return profileSourceFile{}, false, err
	}
	return profileSourceFile{
		ManifestPath: manifestPath, LocalPath: real, Size: stat.Size(), SHA256: sum,
		RootID: root.RootID, MTimeUnixNano: stat.ModTime().UnixNano(),
	}, true, nil
}

func buildProfileSnapshot(profile BackupProfile, cfg agentconfig.Config, generation int, snapshotID string, files []profileSourceFile) (worldbackup.Manifest, worldbackup.Plan) {
	manifest, plan, _ := buildProfileSnapshotWithParent(profile, cfg, generation, snapshotID, files, nil)
	return manifest, plan
}

func buildProfileSnapshotWithParent(profile BackupProfile, cfg agentconfig.Config, generation int, snapshotID string, files []profileSourceFile, parent *worldbackup.Manifest) (worldbackup.Manifest, worldbackup.Plan, worldbackup.Index) {
	var manifestFiles []worldbackup.FileEntry
	current := map[string]worldbackup.IndexedFile{}
	localPaths := map[string]string{}
	var logical int64
	for _, file := range files {
		objectID := worldbackup.ObjectID(file.SHA256)
		manifestFiles = append(manifestFiles, worldbackup.FileEntry{Path: file.ManifestPath, Size: file.Size, SHA256: file.SHA256, ObjectID: objectID})
		current[file.ManifestPath] = worldbackup.IndexedFile{Size: file.Size, MTimeUnixNano: file.MTimeUnixNano, SHA256: file.SHA256, ObjectID: objectID}
		localPaths[file.ManifestPath] = file.LocalPath
		logical += file.Size
	}
	sort.Slice(manifestFiles, func(i, j int) bool { return manifestFiles[i].Path < manifestFiles[j].Path })
	changed, objects := changedProfileFiles(parent, manifestFiles, localPaths)
	deleted := deletedProfilePaths(parent, manifestFiles)
	uploadedSize := uniqueProfileObjectSize(changed)
	parentID := ""
	if parent != nil {
		parentID = parent.SnapshotID
	}
	manifest := worldbackup.Manifest{
		SchemaVersion: worldbackup.SchemaVersion, SnapshotID: snapshotID, GroupID: cfg.GroupID,
		SourceHostID: cfg.HostID, HostGeneration: generation, ParentSnapshotID: parentID,
		CreatedAt: time.Now().UTC(), Consistent: true, LogicalSize: logical, UploadedSize: uploadedSize,
		FileCount: len(manifestFiles), ChangedFileCount: len(changed), DeletedFileCount: len(deleted),
		Files: manifestFiles, DeletedPaths: deleted,
	}
	plan := worldbackup.Plan{
		SnapshotID: snapshotID, ParentSnapshotID: parentID, LogicalSize: logical, FileCount: len(manifestFiles),
		ChangedFileCount: len(changed), DeletedFileCount: len(deleted), ChangedFiles: changed, DeletedPaths: deleted, Objects: objects,
	}
	index := worldbackup.IndexFromManifest(manifest, current)
	_ = profile
	return manifest, plan, index
}

func restoreProfileManifest(ctx context.Context, profile BackupProfile, manifest worldbackup.Manifest, downloader worldbackup.ObjectDownloader) (worldbackup.RestoreSummary, error) {
	if err := worldbackup.ValidateManifest(manifest); err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	rootByID := map[string]BackupProfileRoot{}
	for _, root := range normalizeBackupProfile(profile).Roots {
		if root.Enabled {
			rootByID[root.RootID] = root
		}
	}
	txn := "profile-restore-" + time.Now().UTC().Format("20060102-150405.000000000")
	summary := worldbackup.RestoreSummary{SnapshotID: manifest.SnapshotID, TransactionID: txn}
	var applied []profileFileRollback
	for _, file := range manifest.Files {
		select {
		case <-ctx.Done():
			rollbackProfileFiles(applied)
			return summary, ctx.Err()
		default:
		}
		rootID, rest, ok := strings.Cut(file.Path, "/")
		if !ok {
			rollbackProfileFiles(applied)
			return summary, fmt.Errorf("manifest path %s is missing root id", file.Path)
		}
		root, ok := rootByID[rootID]
		if !ok {
			continue
		}
		targetRoot := firstNonEmpty(root.RestorePath, root.SourcePath)
		target, err := safeProfileTarget(targetRoot, rest)
		if err != nil {
			rollbackProfileFiles(applied)
			return summary, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			rollbackProfileFiles(applied)
			return summary, err
		}
		tmp := target + ".acbh-tmp-" + txn
		if err := downloadProfileObject(ctx, downloader, file, tmp); err != nil {
			_ = os.Remove(tmp)
			rollbackProfileFiles(applied)
			return summary, err
		}
		rollback := target + ".acbh-rollback-" + txn
		hadExisting := false
		if _, err := os.Stat(target); err == nil {
			hadExisting = true
			_ = os.Remove(rollback)
			if err := os.Rename(target, rollback); err != nil {
				_ = os.Remove(tmp)
				rollbackProfileFiles(applied)
				return summary, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(tmp)
			rollbackProfileFiles(applied)
			return summary, err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			if hadExisting {
				_ = os.Rename(rollback, target)
			}
			rollbackProfileFiles(applied)
			return summary, err
		}
		applied = append(applied, profileFileRollback{target: target, rollback: rollback, hadExisting: hadExisting})
		if hadExisting {
			summary.RollbackRoots = appendUnique(summary.RollbackRoots, rollback)
		}
		summary.DownloadedFiles++
		summary.AppliedRoots = appendUnique(summary.AppliedRoots, targetRoot)
	}
	sort.Strings(summary.AppliedRoots)
	sort.Strings(summary.RollbackRoots)
	return summary, nil
}

type profileFileRollback struct {
	target      string
	rollback    string
	hadExisting bool
}

func rollbackProfileFiles(applied []profileFileRollback) {
	for i := len(applied) - 1; i >= 0; i-- {
		item := applied[i]
		_ = os.Remove(item.target)
		if item.hadExisting {
			_ = os.Rename(item.rollback, item.target)
		}
	}
}

func downloadProfileObject(ctx context.Context, downloader worldbackup.ObjectDownloader, file worldbackup.FileEntry, target string) error {
	body, _, err := downloader(ctx, file.ObjectID)
	if err != nil {
		return err
	}
	defer body.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := newSHA256Writer(out)
	size, copyErr := io.Copy(hash, body)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size != file.Size {
		return fmt.Errorf("downloaded object size mismatch: manifest=%d actual=%d", file.Size, size)
	}
	if hash.Sum() != file.SHA256 {
		return fmt.Errorf("downloaded object sha256 mismatch")
	}
	return nil
}

func backupExcludePatterns(profile BackupProfile, root BackupProfileRoot) []string {
	patterns := append([]string{}, profile.ExcludeGlobs...)
	patterns = append(patterns, root.ExcludePatterns...)
	return uniqueStrings(patterns)
}

func shouldExcludeBackup(rel string, isDir bool, patterns []string) bool {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	base := path.Base(rel)
	for _, pattern := range patterns {
		pattern = strings.Trim(strings.ReplaceAll(pattern, "\\", "/"), "/")
		if pattern == "" {
			continue
		}
		if pattern == base || pattern == rel {
			return true
		}
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
		}
		if strings.HasPrefix(pattern, "**/") {
			suffix := strings.TrimPrefix(pattern, "**/")
			if rel == suffix || strings.HasSuffix(rel, "/"+suffix) {
				return true
			}
		}
		if strings.HasSuffix(pattern, "/") {
			prefix := strings.TrimSuffix(pattern, "/")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
		}
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
		if isDir && strings.HasPrefix(rel, pattern+"/") {
			return true
		}
	}
	return false
}

func safeProfileTarget(root, rel string) (string, error) {
	normalized, err := worldbackup.NormalizeManifestPath(rel)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootAbs, filepath.FromSlash(normalized))
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rootReal := rootAbs
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = resolved
	}
	parentReal, err := resolveExistingParent(filepath.Dir(targetAbs))
	if err != nil {
		return "", err
	}
	if !underDesktopRoot(rootReal, parentReal) {
		return "", fmt.Errorf("restore path %s escapes root through symlink", rel)
	}
	if resolved, err := filepath.EvalSymlinks(targetAbs); err == nil && !underDesktopRoot(rootReal, resolved) {
		return "", fmt.Errorf("restore path %s escapes root through symlink", rel)
	}
	if !underDesktopRoot(rootAbs, targetAbs) {
		return "", fmt.Errorf("restore path %s escapes root", rel)
	}
	return targetAbs, nil
}

func underDesktopRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func profileForDownloadTarget(profile BackupProfile, targetDir string) BackupProfile {
	targetDir = filepath.Clean(targetDir)
	for i := range profile.Roots {
		profile.Roots[i].RestorePath = filepath.Join(targetDir, profile.Roots[i].RootID)
	}
	return profile
}

func changedProfileFiles(parent *worldbackup.Manifest, files []worldbackup.FileEntry, localPaths map[string]string) ([]worldbackup.ChangedFile, []worldbackup.PlannedObject) {
	parentFiles := map[string]worldbackup.FileEntry{}
	if parent != nil {
		for _, file := range parent.Files {
			parentFiles[file.Path] = file
		}
	}
	seenObjects := map[string]worldbackup.PlannedObject{}
	var changed []worldbackup.ChangedFile
	for _, file := range files {
		old, ok := parentFiles[file.Path]
		if ok && old.Size == file.Size && old.SHA256 == file.SHA256 && old.ObjectID == file.ObjectID {
			continue
		}
		change := worldbackup.ChangedFile{
			Path: file.Path, Size: file.Size, SHA256: file.SHA256, ObjectID: file.ObjectID, LocalPath: localPaths[file.Path],
		}
		changed = append(changed, change)
		if _, ok := seenObjects[file.SHA256]; !ok {
			seenObjects[file.SHA256] = worldbackup.PlannedObject{SHA256: file.SHA256, Size: file.Size, Path: file.Path}
		}
	}
	objects := make([]worldbackup.PlannedObject, 0, len(seenObjects))
	for _, object := range seenObjects {
		objects = append(objects, object)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].SHA256 < objects[j].SHA256 })
	return changed, objects
}

func deletedProfilePaths(parent *worldbackup.Manifest, files []worldbackup.FileEntry) []string {
	if parent == nil {
		return nil
	}
	current := map[string]struct{}{}
	for _, file := range files {
		current[file.Path] = struct{}{}
	}
	var deleted []string
	for _, file := range parent.Files {
		if _, ok := current[file.Path]; !ok {
			deleted = append(deleted, file.Path)
		}
	}
	sort.Strings(deleted)
	return deleted
}

func uniqueProfileObjectSize(changed []worldbackup.ChangedFile) int64 {
	seen := map[string]int64{}
	for _, file := range changed {
		if _, ok := seen[file.SHA256]; !ok {
			seen[file.SHA256] = file.Size
		}
	}
	var total int64
	for _, size := range seen {
		total += size
	}
	return total
}

func detectPackHints(serverDir string, analysis *BackupServerAnalysis) {
	_ = filepath.WalkDir(serverDir, func(filePath string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := strings.ToLower(entry.Name())
		rel, _ := filepath.Rel(serverDir, filePath)
		rel = filepath.ToSlash(rel)
		detectLoaderFromText(name+" "+strings.ToLower(rel), analysis)
		if analysis.MinecraftVersion == "" {
			analysis.MinecraftVersion = firstMinecraftVersion(name + " " + rel)
		}
		if !entry.IsDir() && (name == "manifest.json" || strings.HasSuffix(name, ".json")) {
			detectVersionFromJSON(filePath, analysis)
		}
		return nil
	})
	if analysis.MinecraftVersion == "" && analysis.Loader == "NeoForge" && analysis.LoaderVersion != "" {
		analysis.MinecraftVersion = minecraftVersionFromNeoForge(analysis.LoaderVersion)
	}
}

func detectLoaderFromText(text string, analysis *BackupServerAnalysis) {
	switch {
	case strings.Contains(text, "neoforge"):
		analysis.Loader = "NeoForge"
	case strings.Contains(text, "fabric"):
		analysis.Loader = "Fabric"
	case strings.Contains(text, "quilt"):
		analysis.Loader = "Quilt"
	case strings.Contains(text, "purpur"):
		analysis.Loader = "Purpur"
	case strings.Contains(text, "paper"):
		analysis.Loader = "Paper"
	case strings.Contains(text, "spigot"):
		analysis.Loader = "Spigot"
	case strings.Contains(text, "forge"):
		analysis.Loader = "Forge"
	}
	if analysis.LoaderVersion == "" {
		if match := loaderVersionPattern.FindStringSubmatch(text); len(match) == 2 {
			analysis.LoaderVersion = strings.Trim(match[1], ".-_")
		}
	}
}

func detectVersionFromJSON(filePath string, analysis *BackupServerAnalysis) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	for _, key := range []string{"minecraftVersion", "minecraft", "version", "mcVersion"} {
		if value, ok := raw[key].(string); ok && analysis.MinecraftVersion == "" {
			analysis.MinecraftVersion = firstMinecraftVersion(value)
		}
	}
}

func readServerLevelName(serverDir string) (string, error) {
	file, err := os.Open(filepath.Join(serverDir, "server.properties"))
	if errors.Is(err, os.ErrNotExist) {
		return "world", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "level-name" {
			value = strings.TrimSpace(value)
			if value == "" {
				return "world", nil
			}
			return filepath.ToSlash(value), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "world", nil
}

func pathStats(target string) BackupPathStats {
	stats := BackupPathStats{Path: target, Exists: pathExists(target)}
	if !stats.Exists {
		return stats
	}
	info, err := os.Stat(target)
	if err != nil {
		return stats
	}
	if !info.IsDir() {
		stats.FileCount = 1
		stats.Bytes = info.Size()
		return stats
	}
	_ = filepath.WalkDir(target, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			stats.FileCount++
			stats.Bytes += info.Size()
		}
		return nil
	})
	return stats
}

func existingRelativeFiles(root string, rels []string) []string {
	var out []string
	for _, rel := range rels {
		if fileExists(filepath.Join(root, filepath.FromSlash(rel))) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func existingRelativePaths(root string, rels []string) []string {
	var out []string
	for _, rel := range rels {
		if pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func addMatchingRootFiles(serverDir string, profile *BackupProfile, rootIDs map[string]int, patterns []string) {
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		for _, pattern := range patterns {
			if ok, _ := path.Match(pattern, entry.Name()); ok {
				base := entry.Name()
				rootID := uniqueRootID(rootIDFromPath(strings.TrimSuffix(base, filepath.Ext(base))), rootIDs)
				source := filepath.Join(serverDir, base)
				profile.Roots = append(profile.Roots, BackupProfileRoot{
					RootID: rootID, DisplayName: base, Kind: "file", SourcePath: source, RestorePath: source, Enabled: true,
				})
				break
			}
		}
	}
}

func canonicalBackupPreset(preset string) string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case BackupPresetCore:
		return BackupPresetCore
	case BackupPresetOffline:
		return BackupPresetOffline
	default:
		return BackupPresetMigratable
	}
}

func backupPresetName(preset string) string {
	switch preset {
	case BackupPresetCore:
		return "Minecraft 核心存档"
	case BackupPresetOffline:
		return "Minecraft 完整离线镜像"
	default:
		return "Minecraft 可迁移服务端"
	}
}

func consistencyForRoot(rootID string) string {
	if rootID == "world" {
		return "minecraft"
	}
	return "files"
}

func uniqueRootID(raw string, seen map[string]int) string {
	base := rootIDFromPath(raw)
	seen[base]++
	if seen[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, seen[base])
}

func firstMinecraftVersion(text string) string {
	return minecraftVersionPattern.FindString(text)
}

func minecraftVersionFromNeoForge(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 && len(parts[0]) == 2 {
		return "1." + parts[0] + "." + parts[1]
	}
	return ""
}

func pathExists(target string) bool {
	_, err := os.Stat(target)
	return err == nil
}

func resolveExistingParent(dir string) (string, error) {
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return "", err
			}
			return resolved, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			return filepath.Clean(dir), nil
		}
		dir = next
	}
}

func progressContext(ctx context.Context, stage, message string, current, total int64) {
	if op, ok := ctx.(interface {
		Progress(string, string, int64, int64)
	}); ok {
		op.Progress(stage, message, current, total)
	}
}

func rootIDFromPath(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "root"
	}
	if len(out) > 64 {
		return out[:64]
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
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

type sha256Writer struct {
	w io.Writer
	h interface {
		io.Writer
		Sum([]byte) []byte
	}
}

func newSHA256Writer(w io.Writer) *sha256Writer {
	return &sha256Writer{w: w, h: sha256.New()}
}

func (w *sha256Writer) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 {
		_, _ = w.h.Write(p[:n])
	}
	return n, err
}

func (w *sha256Writer) Sum() string {
	return fmt.Sprintf("%x", w.h.Sum(nil))
}

func sha256FileLocal(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
