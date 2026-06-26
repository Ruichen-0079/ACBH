package desktop

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
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
	registerBackupTopLevelEndpoints(register, manager, opts)
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
		case action == "" && r.Method == http.MethodGet:
			profile, err := findBackupProfile(opts, id)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			writeJSON(w, profile)
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
		var body backupRequestIDBody
		if !decodeOptionalBody(w, r, &body) {
			return
		}
		op := OperationOptions{Name: "BackupProfileCreate", MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute, IdempotencyKey: body.RequestID}
		startAndWrite(w, r, manager, op, func(ctx OperationContext) (any, error) {
			ctx.Progress("backup", "创建备份快照", 0, 0)
			return BackupProfileCreate(ctx, opts, id)
		})
	case "snapshots":
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileSnapshots", MutexClass: "read:backup", Timeout: 30 * time.Second, Coalesce: true}, func(ctx OperationContext) (any, error) {
			return WorldBackupList(ctx, opts)
		})
	case "latest", "pull", "restore", "sync":
		var body BackupProfileRestoreRequest
		if !decodeOptionalBody(w, r, &body) {
			return
		}
		if action == "latest" {
			body.SnapshotID = "latest"
		}
		body.ProfileID = firstNonEmpty(body.ProfileID, id)
		opName := map[string]string{"pull": "BackupProfilePull", "sync": "BackupProfileSync"}[action]
		if opName == "" {
			opName = "BackupProfileRestore"
		}
		op := OperationOptions{Name: opName, MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute, IdempotencyKey: body.RequestID}
		startAndWrite(w, r, manager, op, func(ctx OperationContext) (any, error) {
			return BackupProfileRestoreWithOptions(ctx, opts, body)
		})
	case "pin":
		var body struct {
			SnapshotID string `json:"snapshotId"`
			RequestID  string `json:"requestId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfilePin", MutexClass: "backup-admin", Timeout: 30 * time.Second, IdempotencyKey: body.RequestID}, func(ctx OperationContext) (any, error) {
			return WorldBackupPin(ctx, opts, body.SnapshotID)
		})
	case "delete":
		var body struct {
			SnapshotID string `json:"snapshotId"`
			RequestID  string `json:"requestId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{Name: "BackupProfileDelete", MutexClass: "backup-admin", Timeout: 30 * time.Second, IdempotencyKey: body.RequestID}, func(ctx OperationContext) (any, error) {
			return WorldBackupDelete(ctx, opts, body.SnapshotID)
		})
	default:
		http.NotFound(w, r)
	}
}
