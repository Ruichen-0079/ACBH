package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

func registerAlpha4Endpoints(register routeRegistrar, manager *OperationManager, opts Options) {
	register("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			desktopCfg, _ := LoadDesktopConfig(opts)
			setup, _ := LoadSetup(opts)
			var agentCfg *agentconfig.Config
			if cfg, err := agentconfig.Load(filepath.Join(withDefaults(opts).AppDataDir, agentconfig.FileName)); err == nil {
				agentCfg = &cfg
			}
			writeJSON(w, map[string]any{"desktop": desktopCfg, "setup": setup, "agent": agentCfg, "version": agentconfig.AgentVersion})
		default:
			methodNotAllowed(w)
		}
	})
	register("/api/config/network", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Host            string `json:"host"`
			CoordinatorPort string `json:"coordinatorPort"`
			PublicGamePort  string `json:"publicGamePort"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "SaveNetworkConfig", MutexClass: "config", Timeout: 30 * time.Second}, func(ctx OperationContext) (any, error) {
			ctx.Progress("network", "保存网络配置", 1, 1)
			return ConfigureNetwork(opts, body.Host, body.CoordinatorPort, body.PublicGamePort)
		})
	})
	register("/api/network/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Host            string `json:"host"`
			CoordinatorPort string `json:"coordinatorPort"`
			PublicGamePort  string `json:"publicGamePort"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "TestNetworkConfig", MutexClass: "read:network", Timeout: 30 * time.Second, Coalesce: true}, func(ctx OperationContext) (any, error) {
			ctx.Progress("network", "测试网络连通性", 1, 1)
			return TestNetworkConfig(ctx, opts, body.Host, body.CoordinatorPort, body.PublicGamePort)
		})
	})
	register("/api/group/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			GroupName      string `json:"groupName"`
			DisplayName    string `json:"displayName"`
			CoordinatorURL string `json:"coordinatorUrl"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "CreateGroup", MutexClass: "config", Timeout: 60 * time.Second}, func(ctx OperationContext) (any, error) {
			return SetupCreateGroup(ctx, opts, body.GroupName, body.DisplayName, body.CoordinatorURL)
		})
	})
	register("/api/group/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			InviteCode     string `json:"inviteCode"`
			DisplayName    string `json:"displayName"`
			CoordinatorURL string `json:"coordinatorUrl"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "JoinGroup", MutexClass: "config", Timeout: 60 * time.Second}, func(ctx OperationContext) (any, error) {
			return SetupJoinGroup(ctx, opts, body.InviteCode, body.DisplayName, body.CoordinatorURL)
		})
	})
	register("/api/group/whoami", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		out, err := GroupWhoAmI(r.Context(), opts)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"ok": false, "errorCode": "whoami_failed", "message": err.Error()})
			return
		}
		writeJSON(w, out)
	})
	register("/api/group/repair-identity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "RepairIdentity", MutexClass: "config", Timeout: 30 * time.Second}, func(ctx OperationContext) (any, error) {
			return RepairGroupIdentity(ctx, opts)
		})
	})
	register("/api/group/leave", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "LeaveGroup", MutexClass: "config", Timeout: 30 * time.Second}, func(ctx OperationContext) (any, error) {
			return LeaveGroup(opts)
		})
	})
	register("/api/server/select-directory", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "SelectServerDirectory", MutexClass: "config", Timeout: 60 * time.Second}, func(ctx OperationContext) (any, error) {
			return InspectServerForSetup(opts, body.Path)
		})
	})
	register("/api/server/inspect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "InspectServer", MutexClass: "read:server", Timeout: 60 * time.Second}, func(ctx OperationContext) (any, error) {
			return InspectServerForSetup(opts, body.Path)
		})
	})
	register("/api/server/select-launch-entry", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "SelectLaunchEntry", MutexClass: "config", Timeout: 60 * time.Second}, func(ctx OperationContext) (any, error) {
			return SelectServerLaunch(opts, body.Path)
		})
	})
	register("/api/config/server", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w)
			return
		}
		var body agentconfig.ServerConfig
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "SaveServerConfig", MutexClass: "config", Timeout: 30 * time.Second}, func(ctx OperationContext) (any, error) {
			return SaveServerConfig(opts, body)
		})
	})
	register("/api/server/preflight", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "ServerPreflight", MutexClass: "read:server", Timeout: 60 * time.Second}, func(ctx OperationContext) (any, error) {
			return ServerPreflight(opts)
		})
	})
	register("/api/picker/folder", pickerUnsupported("folder"))
	register("/api/picker/files", pickerUnsupported("files"))
	register("/api/picker/file", pickerUnsupported("file"))
	register("/api/backup/profiles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			profiles, err := LoadBackupProfiles(opts)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			writeJSON(w, profiles)
		case http.MethodPost:
			var profile BackupProfile
			if !decodeBody(w, r, &profile) {
				return
			}
			out, err := UpsertBackupProfile(opts, profile)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			writeJSON(w, out)
		default:
			methodNotAllowed(w)
		}
	})
	register("/api/backup/profiles/", func(w http.ResponseWriter, r *http.Request) {
		id, action, ok := parseProfileRoute(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch {
		case action == "" && r.Method == http.MethodPut:
			var profile BackupProfile
			if !decodeBody(w, r, &profile) {
				return
			}
			profile.ProfileID = id
			out, err := UpsertBackupProfile(opts, profile)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			writeJSON(w, out)
		case action == "" && r.Method == http.MethodDelete:
			out, err := DeleteBackupProfile(opts, id)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			writeJSON(w, out)
		case r.Method == http.MethodPost:
			handleProfileAction(w, r, manager, opts, id, action)
		default:
			methodNotAllowed(w)
		}
	})
}

func TestNetworkConfig(ctx context.Context, opts Options, host, coordinatorPort, publicGamePort string) (ConfigureNetworkResult, error) {
	coordURL, publicHost, err := NormalizePublicCoordinatorInput(host, coordinatorPort)
	if err != nil {
		return ConfigureNetworkResult{OK: false, State: StateError, Message: err.Error()}, nil
	}
	if publicHost == "" {
		return ConfigureNetworkResult{OK: false, State: StateError, Message: "请输入公网服务器 IP 或域名。"}, nil
	}
	if publicGamePort == "" {
		publicGamePort = "25565"
	}
	client, err := coordinator.NewClient(coordURL)
	if err != nil {
		return ConfigureNetworkResult{OK: false, State: StateError, Message: "Coordinator URL 无效：" + err.Error()}, nil
	}
	warnings := []string{}
	checks := []NetworkCheck{}
	addCheck := func(id string, ok bool, message string) {
		status := "passed"
		if !ok {
			status = "warning"
			warnings = append(warnings, message)
		}
		checks = append(checks, NetworkCheck{ID: id, Status: status, Message: message})
	}
	coordHost, coordPort := hostPortFromURL(coordURL)
	addCheck("tcp_6121", tcpCheck(coordHost, coordPort) == nil, "公网控制端 TCP "+coordPort+" 可连接")
	addCheck("health", client.Health(ctx) == nil, "公网控制端 /health 正常")
	manifestOK, runtimeOK, javaOK := checkBootstrapManifest(ctx, coordURL)
	addCheck("bootstrap_manifest", manifestOK, "Bootstrap manifest 可读取")
	addCheck("runtime_package", runtimeOK, "环境包可用")
	addCheck("java_package", javaOK, "Java package 可用")
	addCheck("tcp_25565", tcpCheck(publicHost, publicGamePort) == nil, "玩家入口 TCP "+publicGamePort+" 可连接")
	return ConfigureNetworkResult{
		OK: true, Outcome: outcomeForWarnings(warnings), State: StateBootstrapReady,
		CoordinatorURL: coordURL, PlayerAddress: publicHost + ":" + publicGamePort,
		BootstrapURL: coordURL + "/v1/bootstrap/manifest", Checks: checks,
		Message: "网络测试完成；未改变已保存配置。", Warnings: warnings,
	}, nil
}

func GroupWhoAmI(ctx context.Context, opts Options) (coordinator.WhoAmIResponse, error) {
	cfg, err := agentconfig.Load(filepath.Join(withDefaults(opts).AppDataDir, agentconfig.FileName))
	if err != nil {
		return coordinator.WhoAmIResponse{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return coordinator.WhoAmIResponse{}, err
	}
	return client.WhoAmI(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
}

func RepairGroupIdentity(ctx context.Context, opts Options) (map[string]any, error) {
	who, err := GroupWhoAmI(ctx, opts)
	if err != nil {
		return nil, err
	}
	err = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.Group = DesktopGroupConfig{GroupID: who.GroupID, MemberID: who.MemberID, HostID: who.HostID, Role: who.Role}
	})
	return map[string]any{"ok": err == nil, "whoami": who}, err
}

func LeaveGroup(opts Options) (map[string]any, error) {
	opts = withDefaults(opts)
	if err := os.Remove(filepath.Join(opts.AppDataDir, agentconfig.FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.Group = DesktopGroupConfig{}
	})
	return map[string]any{"ok": true, "message": "本地已离开当前组；远端成员记录未删除。"}, nil
}

func SaveServerConfig(opts Options, server agentconfig.ServerConfig) (map[string]any, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(server.Dir) == "" {
		return nil, errors.New("server.dir is required")
	}
	cfg.Server = server
	if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
		return nil, err
	}
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.LastServerDir = server.Dir
	})
	return map[string]any{"ok": true, "server": cfg.Server}, nil
}

func ServerPreflight(opts Options) (InspectServerSetupResult, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		return InspectServerSetupResult{}, err
	}
	return InspectServerForSetup(opts, cfg.Server.Dir)
}

func pickerUnsupported(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Path  string   `json:"path"`
			Paths []string `json:"paths"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		if body.Path != "" || len(body.Paths) > 0 {
			writeJSON(w, map[string]any{"ok": true, "kind": kind, "path": body.Path, "paths": body.Paths})
			return
		}
		writeJSON(w, map[string]any{"ok": false, "errorCode": "native_picker_unavailable", "message": "当前 Go runtime 未启用原生文件选择器；请手动粘贴路径。"})
	}
}

func parseProfileRoute(raw string) (string, string, bool) {
	rest := strings.TrimPrefix(raw, "/api/backup/profiles/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	id := parts[0]
	if len(parts) == 1 {
		return id, "", true
	}
	return id, strings.Join(parts[1:], "/"), true
}

func handleProfileAction(w http.ResponseWriter, r *http.Request, manager *OperationManager, opts Options, id, action string) {
	switch action {
	case "scan":
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileScan", MutexClass: "read:backup", Timeout: 5 * time.Minute, Coalesce: true}, func(ctx OperationContext) (any, error) {
			return BackupProfileScan(opts, id)
		})
	case "create", "resume":
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileCreate", MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute}, func(ctx OperationContext) (any, error) {
			ctx.Progress("backup", "创建备份快照", 0, 0)
			return BackupProfileCreate(ctx, opts, id)
		})
	case "snapshots":
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileSnapshots", MutexClass: "read:backup", Timeout: 30 * time.Second, Coalesce: true}, func(ctx OperationContext) (any, error) {
			return WorldBackupList(ctx, opts)
		})
	case "latest", "pull", "restore", "sync":
		var body struct {
			SnapshotID string `json:"snapshotId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileRestore", MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute}, func(ctx OperationContext) (any, error) {
			return BackupProfileRestore(ctx, opts, id, body.SnapshotID)
		})
	case "pin":
		var body struct {
			SnapshotID string `json:"snapshotId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfilePin", MutexClass: "backup-admin", Timeout: 30 * time.Second}, func(ctx OperationContext) (any, error) {
			return WorldBackupPin(ctx, opts, body.SnapshotID)
		})
	case "delete":
		var body struct {
			SnapshotID string `json:"snapshotId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileDelete", MutexClass: "backup-admin", Timeout: 30 * time.Second}, func(ctx OperationContext) (any, error) {
			return WorldBackupDelete(ctx, opts, body.SnapshotID)
		})
	default:
		http.NotFound(w, r)
	}
}

const backupProfilesFileName = "backup-profiles.json"

type BackupProfilesDocument struct {
	SchemaVersion int             `json:"schemaVersion"`
	Profiles      []BackupProfile `json:"profiles"`
}

type BackupProfile struct {
	ProfileID string              `json:"profileId"`
	Name      string              `json:"name"`
	Roots     []BackupProfileRoot `json:"roots"`
}

type BackupProfileRoot struct {
	RootID           string   `json:"rootId"`
	DisplayName      string   `json:"displayName"`
	Kind             string   `json:"kind"`
	SourcePath       string   `json:"sourcePath"`
	RestorePath      string   `json:"restorePath"`
	Required         bool     `json:"required"`
	ConsistencyGroup string   `json:"consistencyGroup,omitempty"`
	ExcludePatterns  []string `json:"excludePatterns,omitempty"`
	FollowSymlinks   bool     `json:"followSymlinks"`
}

type BackupProfileScanResult struct {
	OK          bool                 `json:"ok"`
	ProfileID   string               `json:"profileId"`
	LogicalSize int64                `json:"logicalSize"`
	FileCount   int                  `json:"fileCount"`
	Roots       []BackupRootScanInfo `json:"roots"`
	Warnings    []string             `json:"warnings,omitempty"`
}

type BackupRootScanInfo struct {
	RootID     string `json:"rootId"`
	SourcePath string `json:"sourcePath"`
	FileCount  int    `json:"fileCount"`
	Bytes      int64  `json:"bytes"`
}

type profileSourceFile struct {
	ManifestPath string
	LocalPath    string
	Size         int64
	SHA256       string
	RootID       string
}

func backupProfilesPath(opts Options) string {
	return filepath.Join(withDefaults(opts).AppDataDir, backupProfilesFileName)
}

func LoadBackupProfiles(opts Options) (BackupProfilesDocument, error) {
	pathName := backupProfilesPath(opts)
	var doc BackupProfilesDocument
	if err := loadJSON(pathName, &doc); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			doc = BackupProfilesDocument{SchemaVersion: 1}
			if p, ok := defaultBackupProfile(opts); ok {
				doc.Profiles = append(doc.Profiles, p)
			}
			return doc, nil
		}
		return BackupProfilesDocument{}, err
	}
	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = 1
	}
	return doc, nil
}

func SaveBackupProfiles(opts Options, doc BackupProfilesDocument) error {
	doc.SchemaVersion = 1
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
	roots := []BackupProfileRoot{}
	if resolved, err := worldbackup.ResolveWorldRoots(cfg.Server.Dir, nil); err == nil {
		for _, root := range resolved {
			abs := filepath.Join(cfg.Server.Dir, filepath.FromSlash(root))
			roots = append(roots, BackupProfileRoot{
				RootID: rootIDFromPath(root), DisplayName: root, Kind: "minecraft-world",
				SourcePath: abs, RestorePath: abs, Required: true, ConsistencyGroup: "minecraft", ExcludePatterns: []string{"session.lock", "*.tmp", "*.log"},
			})
		}
	}
	return BackupProfile{ProfileID: "minecraft-auto", Name: "Minecraft 自动世界备份", Roots: roots}, len(roots) > 0
}

func normalizeBackupProfile(profile BackupProfile) BackupProfile {
	profile.ProfileID = rootIDFromPath(firstNonEmpty(profile.ProfileID, profile.Name, "profile"))
	if profile.Name == "" {
		profile.Name = profile.ProfileID
	}
	for i := range profile.Roots {
		root := &profile.Roots[i]
		root.RootID = rootIDFromPath(firstNonEmpty(root.RootID, root.DisplayName, filepath.Base(root.SourcePath)))
		if root.DisplayName == "" {
			root.DisplayName = root.RootID
		}
		if root.Kind == "" {
			root.Kind = "folder"
		}
		if root.RestorePath == "" {
			root.RestorePath = root.SourcePath
		}
	}
	return profile
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
		if _, ok := seen[root.RootID]; ok {
			return fmt.Errorf("duplicate rootId %s", root.RootID)
		}
		seen[root.RootID] = struct{}{}
	}
	return nil
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
	for _, file := range files {
		logical += file.Size
	}
	return BackupProfileScanResult{OK: true, ProfileID: profile.ProfileID, LogicalSize: logical, FileCount: len(files), Roots: roots, Warnings: warnings}, nil
}

func BackupProfileCreate(ctx context.Context, opts Options, profileID string) (WorldBackupCreateResult, error) {
	opts = withDefaults(opts)
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
	files, _, _, err := scanBackupProfileSources(profile)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	snapshotID := "bp_" + profile.ProfileID + "_" + time.Now().UTC().Format("20060102_150405")
	manifest, plan := buildProfileSnapshot(profile, cfg, generation, snapshotID, files)
	if err := worldbackup.ValidateManifest(manifest); err != nil {
		return WorldBackupCreateResult{}, err
	}
	planned, err := client.PlanWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupPlanRequest{
		HostID: cfg.HostID, HostToken: cfg.HostToken, HostGeneration: generation, Objects: plan.Objects,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	bySHA := worldbackup.IndexChangedFilesBySHA(plan.ChangedFiles)
	uploadFn := func(ctx context.Context, sha256 string, content io.Reader, size int64) error {
		_, err := client.UploadWorldObjectStream(ctx, auth, sha256, content, size)
		return err
	}
	if err := worldbackup.UploadMissingObjects(ctx, uploadFn, planned.MissingObjects, bySHA); err != nil {
		return WorldBackupCreateResult{}, err
	}
	commit, err := client.CommitWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupCommitRequest{
		HostID: cfg.HostID, HostToken: cfg.HostToken, HostGeneration: generation, Manifest: manifest,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	return WorldBackupCreateResult{
		OK: commit.OK, SnapshotID: commit.SnapshotID, MissingObjects: len(planned.MissingObjects),
		LogicalSize: manifest.LogicalSize, UploadedSize: manifest.UploadedSize,
		ChangedFileCount: manifest.ChangedFileCount, DeletedFileCount: manifest.DeletedFileCount,
	}, nil
}

func BackupProfileRestore(ctx context.Context, opts Options, profileID, snapshotID string) (worldbackup.RestoreSummary, error) {
	profile, err := findBackupProfile(opts, profileID)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	var remote coordinator.WorldBackupManifestResponse
	if strings.TrimSpace(snapshotID) == "" || snapshotID == "latest" {
		remote, err = client.GetLatestWorldBackup(ctx, auth, true)
	} else {
		remote, err = client.GetWorldBackup(ctx, auth, snapshotID)
	}
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	return restoreProfileManifest(ctx, profile, remote.Manifest, worldObjectDownloader(client, auth))
}

func findBackupProfile(opts Options, profileID string) (BackupProfile, error) {
	doc, err := LoadBackupProfiles(opts)
	if err != nil {
		return BackupProfile{}, err
	}
	for _, profile := range doc.Profiles {
		if profile.ProfileID == profileID {
			return profile, nil
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
		info := BackupRootScanInfo{RootID: root.RootID, SourcePath: root.SourcePath}
		sourceRoot, err := filepath.Abs(root.SourcePath)
		if err != nil {
			return nil, nil, nil, err
		}
		sourceRoot = filepath.Clean(sourceRoot)
		if stat, err := os.Stat(sourceRoot); err != nil {
			if root.Required {
				return nil, nil, nil, err
			}
			warnings = append(warnings, "可选备份目录不存在："+root.SourcePath)
			roots = append(roots, info)
			continue
		} else if !stat.IsDir() {
			return nil, nil, nil, fmt.Errorf("backup root %s is not a directory", root.RootID)
		}
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
			if shouldExcludeBackup(relSlash, entry.IsDir(), root.ExcludePatterns) {
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
			files = append(files, profileSourceFile{ManifestPath: manifestPath, LocalPath: real, Size: stat.Size(), SHA256: sum, RootID: root.RootID})
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

func buildProfileSnapshot(profile BackupProfile, cfg agentconfig.Config, generation int, snapshotID string, files []profileSourceFile) (worldbackup.Manifest, worldbackup.Plan) {
	var manifestFiles []worldbackup.FileEntry
	var changed []worldbackup.ChangedFile
	seenObjects := map[string]worldbackup.PlannedObject{}
	var logical int64
	for _, file := range files {
		objectID := worldbackup.ObjectID(file.SHA256)
		entry := worldbackup.FileEntry{Path: file.ManifestPath, Size: file.Size, SHA256: file.SHA256, ObjectID: objectID}
		manifestFiles = append(manifestFiles, entry)
		changed = append(changed, worldbackup.ChangedFile{Path: file.ManifestPath, Size: file.Size, SHA256: file.SHA256, ObjectID: objectID, LocalPath: file.LocalPath})
		seenObjects[file.SHA256] = worldbackup.PlannedObject{SHA256: file.SHA256, Size: file.Size, Path: file.ManifestPath}
		logical += file.Size
	}
	objects := make([]worldbackup.PlannedObject, 0, len(seenObjects))
	for _, obj := range seenObjects {
		objects = append(objects, obj)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].SHA256 < objects[j].SHA256 })
	manifest := worldbackup.Manifest{
		SchemaVersion: worldbackup.SchemaVersion, SnapshotID: snapshotID, GroupID: cfg.GroupID,
		SourceHostID: cfg.HostID, HostGeneration: generation, CreatedAt: time.Now().UTC(),
		Consistent: true, LogicalSize: logical, UploadedSize: logical, FileCount: len(manifestFiles),
		ChangedFileCount: len(changed), Files: manifestFiles,
	}
	plan := worldbackup.Plan{
		SnapshotID: snapshotID, LogicalSize: logical, FileCount: len(manifestFiles),
		ChangedFileCount: len(changed), ChangedFiles: changed, Objects: objects,
	}
	_ = profile
	return manifest, plan
}

func restoreProfileManifest(ctx context.Context, profile BackupProfile, manifest worldbackup.Manifest, downloader worldbackup.ObjectDownloader) (worldbackup.RestoreSummary, error) {
	if err := worldbackup.ValidateManifest(manifest); err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	rootByID := map[string]BackupProfileRoot{}
	for _, root := range profile.Roots {
		rootByID[root.RootID] = root
	}
	txn := "profile-restore-" + time.Now().UTC().Format("20060102-150405.000000000")
	summary := worldbackup.RestoreSummary{SnapshotID: manifest.SnapshotID, TransactionID: txn}
	for _, file := range manifest.Files {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}
		rootID, rest, ok := strings.Cut(file.Path, "/")
		if !ok {
			return summary, fmt.Errorf("manifest path %s is missing root id", file.Path)
		}
		root, ok := rootByID[rootID]
		if !ok {
			continue
		}
		targetRoot := firstNonEmpty(root.RestorePath, root.SourcePath)
		target, err := safeProfileTarget(targetRoot, rest)
		if err != nil {
			return summary, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return summary, err
		}
		tmp := target + ".acbh-tmp-" + txn
		if err := downloadProfileObject(ctx, downloader, file, tmp); err != nil {
			_ = os.Remove(tmp)
			return summary, err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return summary, err
		}
		summary.DownloadedFiles++
		summary.AppliedRoots = appendUnique(summary.AppliedRoots, targetRoot)
	}
	sort.Strings(summary.AppliedRoots)
	return summary, nil
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
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
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

func shouldExcludeBackup(rel string, isDir bool, patterns []string) bool {
	base := path.Base(rel)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
		if pattern == "" {
			continue
		}
		if pattern == base || pattern == rel {
			return true
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
	target := filepath.Join(root, filepath.FromSlash(normalized))
	if !underDesktopRoot(root, target) {
		return "", fmt.Errorf("restore path %s escapes root", rel)
	}
	return target, nil
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
