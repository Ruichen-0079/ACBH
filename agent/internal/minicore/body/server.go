package body

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	minibackup "github.com/Ruichen-0079/ACBH/agent/internal/minicore/backup"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
	minilistener "github.com/Ruichen-0079/ACBH/agent/internal/minicore/listener"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/operations"
	minirelay "github.com/Ruichen-0079/ACBH/agent/internal/minicore/relay"
)

const (
	Version     = "v0.5.1-public-relay-hotfix2"
	DefaultAddr = "127.0.0.1:6120"
	ServiceName = "acbh-body"
)

type Server struct {
	Addr        string
	ConfigStore coreconfig.Store
	Operations  *operations.Store
	HTTPClient  *http.Client
	Listener    minilistener.Service
	Relay       *minirelay.Service
	Backup      minibackup.Service
	httpServer  *http.Server
}

type HealthResponse struct {
	OK             bool              `json:"ok"`
	Service        string            `json:"service"`
	Version        string            `json:"version"`
	Mode           string            `json:"mode,omitempty"`
	ConfigPath     string            `json:"configPath"`
	CoordinatorURL string            `json:"coordinatorUrl,omitempty"`
	BodyAPI        string            `json:"bodyApi"`
	IdentityModel  string            `json:"identityModel"`
	InstanceID     string            `json:"instanceId,omitempty"`
	DeviceID       string            `json:"deviceId,omitempty"`
	ServerID       string            `json:"serverId,omitempty"`
	ConfigError    *coreerrors.Error `json:"configError,omitempty"`
}

type InitResult struct {
	State       string          `json:"state"`
	Coordinator InitCoordinator `json:"coordinator"`
	Identity    InitIdentity    `json:"identity"`
}

type InitCoordinator struct {
	URL              string                             `json:"url"`
	Version          string                             `json:"version,omitempty"`
	ProtocolVersion  int                                `json:"protocolVersion"`
	CapabilitiesOK   bool                               `json:"capabilitiesOk"`
	ActualRequestURL string                             `json:"actualRequestUrl,omitempty"`
	NetworkRequests  []coordinatorclient.NetworkRequest `json:"networkRequests,omitempty"`
}

type InitIdentity struct {
	IdentityModel string `json:"identityModel"`
	InstanceID    string `json:"instanceId"`
	DeviceID      string `json:"deviceId"`
	Valid         bool   `json:"valid"`
	Message       string `json:"message"`
}

type LocalInitResult struct {
	ConfigPath  string           `json:"configPath"`
	Coordinator string           `json:"coordinatorUrl,omitempty"`
	Identity    IdentityResponse `json:"identity"`
	Message     string           `json:"message"`
}

type IdentityResponse struct {
	OK            bool                `json:"ok"`
	IdentityModel string              `json:"identityModel"`
	Instance      IdentityInstance    `json:"instance"`
	Device        IdentityDevice      `json:"device"`
	Server        IdentityServer      `json:"server"`
	Coordinator   IdentityCoordinator `json:"coordinator"`
	Compat        IdentityCompat      `json:"compat"`
}

type IdentityInstance struct {
	InstanceID  string `json:"instanceId"`
	DisplayName string `json:"displayName"`
}

type IdentityDevice struct {
	DeviceID    string `json:"deviceId"`
	DisplayName string `json:"displayName"`
	Platform    string `json:"platform"`
}

type IdentityServer struct {
	ServerID    string `json:"serverId"`
	DisplayName string `json:"displayName"`
	Dir         string `json:"dir"`
}

type IdentityCoordinator struct {
	URL             string `json:"url"`
	ProtocolVersion int    `json:"protocolVersion"`
}

type IdentityCompat struct {
	UsesLegacyGroupAPI      bool `json:"usesLegacyGroupApi"`
	LegacyGroupIDPresent    bool `json:"legacyGroupIdPresent"`
	LegacyHostIDPresent     bool `json:"legacyHostIdPresent"`
	LegacyHostTokenPresent  bool `json:"legacyHostTokenPresent"`
	LegacyMemberIDPresent   bool `json:"legacyMemberIdPresent"`
	OwnerTokenPresent       bool `json:"ownerTokenPresent"`
	FullLegacyTokenRedacted bool `json:"fullLegacyTokenRedacted"`
	FullOwnerTokenRedacted  bool `json:"fullOwnerTokenRedacted"`
}

func New(addr string, appDataDir string) *Server {
	if addr == "" {
		addr = DefaultAddr
	}
	store := coreconfig.NewStore(appDataDir)
	return &Server{Addr: addr, ConfigStore: store, Operations: operations.NewStore()}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := s.routes()
	s.httpServer = &http.Server{Addr: s.Addr, Handler: mux}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()
	err = s.httpServer.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/body/health", s.handleHealth)
	mux.HandleFunc("/v1/config", s.handleConfig)
	mux.HandleFunc("/v1/identity", s.handleIdentity)
	mux.HandleFunc("/v1/coordinator/probe", s.handleCoordinatorProbe)
	mux.HandleFunc("/v1/init", s.handleInit)
	mux.HandleFunc("/v1/local/init", s.handleLocalInit)
	mux.HandleFunc("/v1/listener/status", s.handleListenerStatus)
	mux.HandleFunc("/v1/listener/config", s.handleListenerConfig)
	mux.HandleFunc("/v1/listener/probe", s.handleListenerProbe)
	mux.HandleFunc("/v1/relay/configure", s.handleRelayConfigure)
	mux.HandleFunc("/v1/relay/status", s.handleRelayStatus)
	mux.HandleFunc("/v1/backup/analyze", s.handleBackupAnalyze)
	mux.HandleFunc("/v1/backup/upload", s.handleBackupUpload)
	mux.HandleFunc("/v1/snapshots", s.handleSnapshots)
	mux.HandleFunc("/v1/snapshots/latest/download", s.handleLatestSnapshotDownload)
	mux.HandleFunc("/v1/snapshots/", s.handleSnapshotDownload)
	mux.HandleFunc("/v1/operations", s.handleOperations)
	mux.HandleFunc("/v1/operations/", s.handleOperation)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, coreerrors.New(coreerrors.CoordinatorRouteMissing, "body route not found", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, "Use /v1/body/health or another v1 body API route."))
	})
	return withJSON(mux)
}

func (s *Server) handleListenerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	status, err := s.listenerStatus(r.Context())
	if err != nil {
		writeError(w, statusForCoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleListenerProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	status, err := s.listenerStatus(r.Context())
	if err != nil {
		writeError(w, statusForCoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleListenerConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.LoadOrCreate()
	if cfgErr != nil {
		writeError(w, statusForCoreError(cfgErr), cfgErr)
		return
	}
	var listenerCfg coreconfig.ListenerConfig
	if err := json.NewDecoder(r.Body).Decode(&listenerCfg); err != nil {
		writeError(w, http.StatusBadRequest, coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error()))
		return
	}
	cfg.Listener = listenerCfg
	if err := s.ConfigStore.Save(cfg); err != nil {
		writeError(w, statusForCoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cfg.Listener)
}

func (s *Server) handleRelayConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("relay.configure", "load_config", "loading config")
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeJSON(w, statusForCoreError(cfgErr), s.Operations.Fail(op, cfgErr))
		return
	}
	var req minirelay.ConfigureRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			parseErr := coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error())
			writeJSON(w, http.StatusBadRequest, s.Operations.Fail(op, parseErr))
			return
		}
	}
	service := s.relayService()
	op.Stage = "relay_configure"
	op.Progress = 40
	op.Message = "configuring relay through coordinator"
	s.Operations.Update(op)
	result, err := service.Configure(r.Context(), cfg, req)
	if err != nil {
		writeJSON(w, statusForCoreError(err), s.Operations.Fail(op, err))
		return
	}
	cfg.Relay.Enabled = true
	if req.LocalMinecraftHost != "" {
		cfg.Listener.LocalHost = req.LocalMinecraftHost
	}
	if req.LocalMinecraftPort > 0 {
		cfg.Listener.LocalPort = req.LocalMinecraftPort
	}
	if req.PublicMinecraftPort > 0 {
		cfg.Relay.MinecraftPort = req.PublicMinecraftPort
	}
	if saveErr := s.ConfigStore.Save(cfg); saveErr != nil {
		writeJSON(w, statusForCoreError(saveErr), s.Operations.Fail(op, saveErr))
		return
	}
	writeJSON(w, http.StatusOK, s.Operations.Complete(op, result, "relay configured"))
}

func (s *Server) handleRelayStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeError(w, statusForCoreError(cfgErr), cfgErr)
		return
	}
	state, err := s.relayService().Status(r.Context(), cfg)
	if err != nil {
		writeError(w, statusForCoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relay": state})
}

func (s *Server) handleBackupAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeError(w, statusForCoreError(cfgErr), cfgErr)
		return
	}
	var req minibackup.AnalyzeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error()))
			return
		}
	}
	result, err := s.backupService().Analyze(r.Context(), cfg, req)
	if err != nil {
		writeError(w, statusForCoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleBackupUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("backup.upload", "load_config", "loading config")
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeJSON(w, statusForCoreError(cfgErr), s.Operations.Fail(op, cfgErr))
		return
	}
	var req minibackup.AnalyzeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			parseErr := coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error())
			writeJSON(w, http.StatusBadRequest, s.Operations.Fail(op, parseErr))
			return
		}
	}
	op.Stage = "backup_upload"
	op.Progress = 30
	op.Message = "uploading backup through coordinator"
	s.Operations.Update(op)
	service := s.backupService()
	service.Progress = func(progress minibackup.UploadProgress) {
		current, ok := s.Operations.Get(op.OperationID)
		if !ok || current.State != operations.Running {
			return
		}
		applyUploadProgress(&current, progress)
		s.Operations.Update(current)
	}
	go func(op operations.Operation) {
		result, err := service.Upload(context.Background(), cfg, req)
		if err != nil {
			current, ok := s.Operations.Get(op.OperationID)
			if !ok {
				current = op
			}
			s.Operations.Fail(current, err)
			return
		}
		final, ok := s.Operations.Get(op.OperationID)
		if !ok {
			final = op
		}
		final.UploadedSize = result.UploadedSize
		final.DeduplicatedSize = result.DeduplicatedSize
		final.LogicalSize = result.LogicalSize
		final.FileCount = result.FileCount
		final.RootCount = result.RootCount
		final.SnapshotID = result.SnapshotID
		s.Operations.Complete(final, result, "backup uploaded")
	}(op)
	writeJSON(w, http.StatusOK, op)
}

func applyUploadProgress(op *operations.Operation, progress minibackup.UploadProgress) {
	if progress.Stage != "" {
		op.Stage = progress.Stage
	}
	if progress.Progress > 0 {
		op.Progress = progress.Progress
	}
	op.Current = progress.Current
	op.Total = progress.Total
	if progress.UploadedSize > 0 {
		op.UploadedSize = progress.UploadedSize
	}
	if progress.DeduplicatedSize > 0 {
		op.DeduplicatedSize = progress.DeduplicatedSize
	}
	if progress.LogicalSize > 0 {
		op.LogicalSize = progress.LogicalSize
	}
	if progress.FileCount > 0 {
		op.FileCount = progress.FileCount
	}
	if progress.RootCount > 0 {
		op.RootCount = progress.RootCount
	}
	if progress.SnapshotID != "" {
		op.SnapshotID = progress.SnapshotID
	}
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeError(w, statusForCoreError(cfgErr), cfgErr)
		return
	}
	result, err := s.backupService().List(r.Context(), cfg)
	if err != nil {
		writeError(w, statusForCoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLatestSnapshotDownload(w http.ResponseWriter, r *http.Request) {
	s.handleSnapshotDownloadWithID(w, r, "latest")
}

func (s *Server) handleSnapshotDownload(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/v1/snapshots/")
	snapshotID, rest, ok := strings.Cut(suffix, "/")
	if !ok || rest != "download" || snapshotID == "" {
		writeError(w, http.StatusNotFound, coreerrors.New(coreerrors.CoordinatorRouteMissing, "body route not found", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, "Use /v1/snapshots/{snapshotId}/download."))
		return
	}
	s.handleSnapshotDownloadWithID(w, r, snapshotID)
}

func (s *Server) handleSnapshotDownloadWithID(w http.ResponseWriter, r *http.Request, snapshotID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("snapshot.download", "load_config", "loading config")
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeJSON(w, statusForCoreError(cfgErr), s.Operations.Fail(op, cfgErr))
		return
	}
	var req minibackup.DownloadRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			parseErr := coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error())
			writeJSON(w, http.StatusBadRequest, s.Operations.Fail(op, parseErr))
			return
		}
	}
	if err := preflightDownloadRequest(req); err != nil {
		writeJSON(w, statusForCoreError(err), s.Operations.Fail(op, err))
		return
	}
	op.Stage = "snapshot_download"
	op.Progress = 30
	op.Message = "downloading snapshot through coordinator"
	s.Operations.Update(op)
	service := s.backupService()
	go func(op operations.Operation) {
		result, err := service.Download(context.Background(), cfg, snapshotID, req)
		if err != nil {
			s.Operations.Fail(op, err)
			return
		}
		s.Operations.Complete(op, result, "snapshot downloaded")
	}(op)
	writeJSON(w, http.StatusOK, op)
}

func preflightDownloadRequest(req minibackup.DownloadRequest) *coreerrors.Error {
	if strings.TrimSpace(req.TargetDir) == "" {
		return coreerrors.New(coreerrors.TargetDirRequired, "targetDir is required", coreerrors.Details{}, "Choose a new empty restore directory.")
	}
	targetDir, absErr := filepath.Abs(req.TargetDir)
	if absErr != nil {
		return coreerrors.New(coreerrors.InvalidRequest, "targetDir is invalid", coreerrors.Details{Path: req.TargetDir}, absErr.Error())
	}
	info, statErr := os.Lstat(targetDir)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		return coreerrors.New(coreerrors.InvalidRequest, "inspect targetDir failed", coreerrors.Details{Path: targetDir}, statErr.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return coreerrors.New(coreerrors.RestorePathEscapeBlocked, "targetDir is a symlink or reparse point", coreerrors.Details{Path: targetDir}, "Choose a normal directory.")
	}
	if !info.IsDir() {
		return coreerrors.New(coreerrors.InvalidRequest, "targetDir is not a directory", coreerrors.Details{Path: targetDir}, "Choose a directory.")
	}
	entries, readErr := os.ReadDir(targetDir)
	if readErr != nil {
		return coreerrors.New(coreerrors.InvalidRequest, "read targetDir failed", coreerrors.Details{Path: targetDir}, readErr.Error())
	}
	if len(entries) > 0 && !req.AllowNonEmpty {
		return coreerrors.New(coreerrors.TargetDirNotEmpty, "targetDir is not empty", coreerrors.Details{Path: targetDir}, "Choose an empty directory or set allowNonEmpty=true.")
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
	resp := HealthResponse{OK: true, Service: ServiceName, Version: Version, ConfigPath: s.ConfigStore.Path, BodyAPI: "http://" + s.Addr, IdentityModel: identity.Model}
	if cfgErr != nil {
		resp.ConfigError = cfgErr
	} else {
		resp.Mode = cfg.Mode
		resp.CoordinatorURL = cfg.CoordinatorURL
		resp.InstanceID = cfg.Instance.InstanceID
		resp.DeviceID = cfg.Device.DeviceID
		resp.ServerID = cfg.Server.ServerID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.ConfigStore.Load()
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, coreconfig.Sanitized(cfg))
	case http.MethodPut:
		var cfg coreconfig.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error()))
			return
		}
		if existing, loadErr := s.ConfigStore.Load(); loadErr == nil {
			preserveRedactedTokens(&cfg, existing)
		} else {
			clearRedactedTokens(&cfg)
		}
		if err := s.ConfigStore.Save(cfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, coreconfig.Sanitized(cfg))
	default:
		methodNotAllowed(w, r)
	}
}

func clearRedactedTokens(next *coreconfig.Config) {
	if next.Instance.OwnerToken == "[redacted]" {
		next.Instance.OwnerToken = ""
	}
	if next.Compat.LegacyHostToken == "[redacted]" {
		next.Compat.LegacyHostToken = ""
	}
	if next.Identity.HostToken == "[redacted]" {
		next.Identity.HostToken = ""
	}
}

func preserveRedactedTokens(next *coreconfig.Config, existing coreconfig.Config) {
	if next.Instance.InstanceID == "" {
		next.Instance.InstanceID = existing.Instance.InstanceID
	}
	if next.Device.DeviceID == "" {
		next.Device.DeviceID = existing.Device.DeviceID
	}
	if next.Server.ServerID == "" {
		next.Server.ServerID = existing.Server.ServerID
	}
	if next.Compat.LegacyGroupID == "" {
		next.Compat.LegacyGroupID = existing.Compat.LegacyGroupID
	}
	if next.Compat.LegacyMemberID == "" {
		next.Compat.LegacyMemberID = existing.Compat.LegacyMemberID
	}
	if next.Compat.LegacyHostID == "" {
		next.Compat.LegacyHostID = existing.Compat.LegacyHostID
	}
	if next.Instance.OwnerToken == "[redacted]" {
		next.Instance.OwnerToken = existing.Instance.OwnerToken
	}
	if next.Compat.LegacyHostToken == "[redacted]" {
		next.Compat.LegacyHostToken = existing.Compat.LegacyHostToken
	}
	if next.Identity.HostToken == "[redacted]" {
		next.Identity.HostToken = existing.Compat.LegacyHostToken
	}
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		writeError(w, statusForCoreError(cfgErr), cfgErr)
		return
	}
	writeJSON(w, http.StatusOK, identityResponse(cfg))
}

func (s *Server) handleCoordinatorProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("coordinator.probe", "load_config", "loading config")
	result, err := s.probe(r.Context(), &op, false)
	if err != nil {
		writeJSON(w, statusForCoreError(err), s.Operations.Fail(op, err))
		return
	}
	writeJSON(w, http.StatusOK, s.Operations.Complete(op, result, "coordinator probe completed"))
}

func (s *Server) handleLocalInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("local.init", "load_or_create_config", "creating or normalizing local config")
	cfg, cfgErr := s.ConfigStore.LoadOrCreate()
	if cfgErr != nil {
		writeJSON(w, statusForCoreError(cfgErr), s.Operations.Fail(op, cfgErr))
		return
	}
	if saveErr := s.ConfigStore.Save(cfg); saveErr != nil {
		writeJSON(w, statusForCoreError(saveErr), s.Operations.Fail(op, saveErr))
		return
	}
	cfg, cfgErr = s.ConfigStore.Load()
	if cfgErr != nil {
		writeJSON(w, statusForCoreError(cfgErr), s.Operations.Fail(op, cfgErr))
		return
	}
	result := LocalInitResult{
		ConfigPath:  s.ConfigStore.Path,
		Coordinator: cfg.CoordinatorURL,
		Identity:    identityResponse(cfg),
		Message:     "本机初始化完成。下一步请点击“测试连接”检查 VPS Coordinator。",
	}
	writeJSON(w, http.StatusOK, s.Operations.Complete(op, result, "local init completed"))
}

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("init", "load_config", "loading config")
	probe, err := s.probe(r.Context(), &op, true)
	if err != nil {
		writeJSON(w, statusForCoreError(err), s.Operations.Fail(op, err))
		return
	}
	cfg, cfgErr := s.ConfigStore.LoadOrCreate()
	if cfgErr != nil {
		writeJSON(w, statusForCoreError(cfgErr), s.Operations.Fail(op, cfgErr))
		return
	}
	client, clientErr := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
	if clientErr != nil {
		writeJSON(w, http.StatusBadRequest, s.Operations.Fail(op, clientErr))
		return
	}
	op.Stage = "identity"
	op.Progress = 80
	op.Message = "validating access token and bootstrapping remote instance"
	s.Operations.Update(op)
	coordIdentity, identityErr := identity.Adapter(cfg)
	if identityErr != nil {
		identityErr.Details.ConfigPath = s.ConfigStore.Path
		identityErr.Details.CoordinatorURL = cfg.CoordinatorURL
		writeJSON(w, statusForCoreError(identityErr), s.Operations.Fail(op, identityErr))
		return
	}
	bootstrapURL := strings.TrimRight(cfg.CoordinatorURL, "/") + "/v1/bootstrap"
	_, bootstrapErr := client.Bootstrap(r.Context(), coordIdentity.OwnerToken, coordinatorclient.BootstrapRequest{
		InstanceID:   cfg.Instance.InstanceID,
		InstanceName: cfg.Instance.DisplayName,
		DeviceID:     cfg.Device.DeviceID,
		DeviceName:   cfg.Device.DisplayName,
		ServerID:     cfg.Server.ServerID,
		ServerName:   cfg.Server.DisplayName,
	})
	if bootstrapErr != nil {
		bootstrapErr.Details.ConfigPath = s.ConfigStore.Path
		bootstrapErr.Details.CoordinatorURL = cfg.CoordinatorURL
		writeJSON(w, statusForCoreError(bootstrapErr), s.Operations.Fail(op, bootstrapErr))
		return
	}
	if _, verifyErr := client.VerifyAuth(r.Context(), coordIdentity.OwnerToken); verifyErr != nil {
		verifyErr.Details.ConfigPath = s.ConfigStore.Path
		verifyErr.Details.CoordinatorURL = cfg.CoordinatorURL
		writeJSON(w, statusForCoreError(verifyErr), s.Operations.Fail(op, verifyErr))
		return
	}
	version := ""
	protocol := 0
	capabilitiesOK := false
	if probe.Capabilities != nil {
		version = probe.Capabilities.CoordinatorVersion
		protocol = probe.Capabilities.ProtocolVersion
		capabilitiesOK = probe.CapabilitiesOK
	}
	result := InitResult{
		State: "private instance connected",
		Coordinator: InitCoordinator{
			URL:              cfg.CoordinatorURL,
			Version:          version,
			ProtocolVersion:  protocol,
			CapabilitiesOK:   capabilitiesOK,
			ActualRequestURL: probe.ActualRequestURL,
			NetworkRequests: []coordinatorclient.NetworkRequest{
				{Stage: "coordinator_probe", Method: http.MethodGet, ActualRequestURL: probe.ActualRequestURL, HTTPStatus: http.StatusOK},
				{Stage: "remote_bootstrap", Method: http.MethodPost, ActualRequestURL: bootstrapURL, HTTPStatus: http.StatusOK},
				{Stage: "token_verify", Method: http.MethodPost, ActualRequestURL: strings.TrimRight(cfg.CoordinatorURL, "/") + "/v1/auth/verify", HTTPStatus: http.StatusOK},
			},
		},
		Identity: InitIdentity{
			IdentityModel: identity.Model,
			InstanceID:    cfg.Instance.InstanceID,
			DeviceID:      cfg.Device.DeviceID,
			Valid:         true,
			Message:       "私人实例已连接；远端已初始化；访问令牌有效；备份/中转能力可用",
		},
	}
	writeJSON(w, http.StatusOK, s.Operations.Complete(op, result, "private instance connected"))
}

func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operations": s.Operations.List()})
}

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/operations/")
	if id == "" {
		writeJSON(w, http.StatusOK, map[string]any{"operations": s.Operations.List()})
		return
	}
	op, ok := s.Operations.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, coreerrors.New(coreerrors.CoordinatorRouteMissing, "operation not found", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, "Check operationId."))
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) probe(ctx context.Context, op *operations.Operation, createConfig bool) (coordinatorclient.ProbeResult, *coreerrors.Error) {
	var cfg coreconfig.Config
	var cfgErr *coreerrors.Error
	if createConfig {
		cfg, cfgErr = s.ConfigStore.LoadOrCreate()
	} else {
		cfg, cfgErr = s.ConfigStore.Load()
	}
	if cfgErr != nil {
		return coordinatorclient.ProbeResult{}, cfgErr
	}
	if strings.TrimSpace(cfg.CoordinatorURL) == "" {
		return coordinatorclient.ProbeResult{}, coreerrors.New(
			coreerrors.ConfigInvalid,
			"缺少 VPS 协调器地址 coordinatorUrl。",
			coreerrors.Details{ConfigPath: s.ConfigStore.Path},
			"请在「VPS 地址」中填写，例如：\nhttp://<VPS公网IP>:6121\n然后点击「保存配置」或「初始化」。",
		)
	}
	client, clientErr := coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
	if clientErr != nil {
		clientErr.Details.ConfigPath = s.ConfigStore.Path
		return coordinatorclient.ProbeResult{}, clientErr
	}
	if op != nil {
		op.Stage = "coordinator_probe"
		op.Progress = 30
		op.Message = "probing " + cfg.CoordinatorURL
		s.Operations.Update(*op)
	}
	result, probeErr := client.Probe(ctx)
	if probeErr != nil {
		probeErr.Details.ConfigPath = s.ConfigStore.Path
		probeErr.Details.CoordinatorURL = cfg.CoordinatorURL
		return result, probeErr
	}
	if op != nil {
		op.Stage = "route_probe"
		op.Progress = 70
		op.Message = "coordinator routes verified"
		s.Operations.Update(*op)
	}
	return result, nil
}

func (s *Server) listenerStatus(ctx context.Context) (minilistener.Status, *coreerrors.Error) {
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		return minilistener.Status{}, cfgErr
	}
	service := s.Listener
	return service.Status(ctx, cfg)
}

func (s *Server) relayService() *minirelay.Service {
	if s.Relay == nil {
		s.Relay = &minirelay.Service{HTTPClient: s.HTTPClient}
	} else if s.Relay.HTTPClient == nil {
		s.Relay.HTTPClient = s.HTTPClient
	}
	return s.Relay
}

func (s *Server) backupService() minibackup.Service {
	service := s.Backup
	if service.HTTPClient == nil {
		service.HTTPClient = s.HTTPClient
	}
	if service.HTTPClient == nil {
		service.HTTPClient = &http.Client{}
	}
	return service
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err *coreerrors.Error) {
	writeJSON(w, status, err)
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, coreerrors.New(coreerrors.InvalidRequest, "method not allowed", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, "Use the documented HTTP method."))
}

func statusForCoreError(err *coreerrors.Error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	switch err.ErrorCode {
	case coreerrors.ConfigMissing, coreerrors.ConfigInvalid, coreerrors.ConfigParseError, coreerrors.ConfigWriteFailed, coreerrors.InvalidRequest, coreerrors.TargetDirRequired, coreerrors.TargetDirNotEmpty, coreerrors.RestorePathEscapeBlocked, coreerrors.SnapshotDownloadFailed:
		return http.StatusBadRequest
	case coreerrors.IdentityIncomplete:
		return http.StatusBadRequest
	case coreerrors.BackupObjectTooLarge:
		return http.StatusRequestEntityTooLarge
	case coreerrors.AuthMissing, coreerrors.AuthInvalid:
		return http.StatusUnauthorized
	case coreerrors.LeaseExpired, coreerrors.NotCurrentHost:
		return http.StatusForbidden
	case coreerrors.SnapshotNotFound:
		return http.StatusNotFound
	case coreerrors.ProcessInspectionLimited:
		return http.StatusOK
	case coreerrors.CoordinatorRouteMissing:
		return http.StatusNotFound
	case coreerrors.CoordinatorUnreachable, coreerrors.ProxyInterferenceSuspected, coreerrors.CoordinatorServerError, coreerrors.NetworkError:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func WriteExampleConfig(appDataDir string, coordinatorURL string, serverDir string) (string, *coreerrors.Error) {
	store := coreconfig.NewStore(appDataDir)
	cfg := coreconfig.DefaultConfig()
	cfg.CoordinatorURL = coordinatorURL
	cfg.Server.Dir = serverDir
	cfg.Instance.InstanceID = "inst_xxx"
	cfg.Instance.OwnerToken = "ht_xxx"
	cfg.Compat.LegacyGroupID = "grp_xxx"
	cfg.Compat.LegacyMemberID = "mem_xxx"
	cfg.Compat.LegacyHostID = "host_xxx"
	cfg.Compat.LegacyHostToken = "ht_xxx"
	hostname, _ := os.Hostname()
	cfg.Device.DeviceID = "dev_xxx"
	cfg.Device.DisplayName = hostname
	cfg.Device.Platform = "windows"
	cfg.Server.ServerID = "srv_xxx"
	if err := store.Save(cfg); err != nil {
		return "", err
	}
	return filepath.Clean(store.Path), nil
}

func identityResponse(cfg coreconfig.Config) IdentityResponse {
	return IdentityResponse{
		OK:            true,
		IdentityModel: identity.Model,
		Instance:      IdentityInstance{InstanceID: cfg.Instance.InstanceID, DisplayName: cfg.Instance.DisplayName},
		Device:        IdentityDevice{DeviceID: cfg.Device.DeviceID, DisplayName: cfg.Device.DisplayName, Platform: cfg.Device.Platform},
		Server:        IdentityServer{ServerID: cfg.Server.ServerID, DisplayName: cfg.Server.DisplayName, Dir: cfg.Server.Dir},
		Coordinator:   IdentityCoordinator{URL: cfg.CoordinatorURL, ProtocolVersion: cfg.Compat.CoordinatorProtocol},
		Compat: IdentityCompat{
			UsesLegacyGroupAPI:      false,
			LegacyGroupIDPresent:    cfg.Compat.LegacyGroupID != "",
			LegacyHostIDPresent:     cfg.Compat.LegacyHostID != "",
			LegacyHostTokenPresent:  cfg.Compat.LegacyHostToken != "",
			LegacyMemberIDPresent:   cfg.Compat.LegacyMemberID != "",
			OwnerTokenPresent:       cfg.Instance.OwnerToken != "",
			FullLegacyTokenRedacted: true,
			FullOwnerTokenRedacted:  true,
		},
	}
}

func Endpoint(addr string) string {
	if addr == "" {
		addr = DefaultAddr
	}
	return fmt.Sprintf("http://%s", addr)
}
