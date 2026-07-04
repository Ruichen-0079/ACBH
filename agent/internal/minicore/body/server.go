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

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	minilistener "github.com/Ruichen-0079/ACBH/agent/internal/minicore/listener"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/operations"
	minirelay "github.com/Ruichen-0079/ACBH/agent/internal/minicore/relay"
)

const (
	Version     = "0.5.0-minimal"
	DefaultAddr = "127.0.0.1:6120"
	ServiceName = "acbh-body"
)

type Server struct {
	Addr        string
	ConfigStore coreconfig.Store
	Operations  *operations.Store
	HTTPClient  *http.Client
	Listener    minilistener.Service
	Relay       minirelay.Service
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
	ConfigError    *coreerrors.Error `json:"configError,omitempty"`
}

type InitResult struct {
	State       string          `json:"state"`
	Coordinator InitCoordinator `json:"coordinator"`
	Identity    InitIdentity    `json:"identity"`
}

type InitCoordinator struct {
	URL             string `json:"url"`
	Version         string `json:"version,omitempty"`
	ProtocolVersion int    `json:"protocolVersion"`
	CapabilitiesOK  bool   `json:"capabilitiesOk"`
}

type InitIdentity struct {
	GroupID string `json:"groupId"`
	HostID  string `json:"hostId"`
	Valid   bool   `json:"valid"`
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
	mux.HandleFunc("/v1/coordinator/probe", s.handleCoordinatorProbe)
	mux.HandleFunc("/v1/init", s.handleInit)
	mux.HandleFunc("/v1/listener/status", s.handleListenerStatus)
	mux.HandleFunc("/v1/listener/config", s.handleListenerConfig)
	mux.HandleFunc("/v1/listener/probe", s.handleListenerProbe)
	mux.HandleFunc("/v1/relay/configure", s.handleRelayConfigure)
	mux.HandleFunc("/v1/relay/status", s.handleRelayStatus)
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
	cfg, cfgErr := s.ConfigStore.Load()
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
	resp := HealthResponse{OK: true, Service: ServiceName, Version: Version, ConfigPath: s.ConfigStore.Path, BodyAPI: "http://" + s.Addr}
	if cfgErr != nil {
		resp.ConfigError = cfgErr
	} else {
		resp.Mode = cfg.Mode
		resp.CoordinatorURL = cfg.CoordinatorURL
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
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg coreconfig.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, coreerrors.New(coreerrors.ConfigParseError, "request body is not valid JSON", coreerrors.Details{URL: r.URL.Path, Method: r.Method}, err.Error()))
			return
		}
		if err := s.ConfigStore.Save(cfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		methodNotAllowed(w, r)
	}
}

func (s *Server) handleCoordinatorProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("coordinator.probe", "load_config", "loading config")
	result, err := s.probe(r.Context(), &op)
	if err != nil {
		writeJSON(w, statusForCoreError(err), s.Operations.Fail(op, err))
		return
	}
	writeJSON(w, http.StatusOK, s.Operations.Complete(op, result, "coordinator probe completed"))
}

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	op := s.Operations.Start("init", "load_config", "loading config")
	probe, err := s.probe(r.Context(), &op)
	if err != nil {
		writeJSON(w, statusForCoreError(err), s.Operations.Fail(op, err))
		return
	}
	cfg, cfgErr := s.ConfigStore.Load()
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
	op.Message = "validating host identity"
	s.Operations.Update(op)
	if _, whoErr := client.WhoAmI(r.Context(), cfg.Identity.GroupID, cfg.Identity.HostID, cfg.Identity.HostToken); whoErr != nil {
		whoErr.Details.ConfigPath = s.ConfigStore.Path
		whoErr.Details.CoordinatorURL = cfg.CoordinatorURL
		writeJSON(w, statusForCoreError(whoErr), s.Operations.Fail(op, whoErr))
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
		State:       "ready",
		Coordinator: InitCoordinator{URL: cfg.CoordinatorURL, Version: version, ProtocolVersion: protocol, CapabilitiesOK: capabilitiesOK},
		Identity:    InitIdentity{GroupID: cfg.Identity.GroupID, HostID: cfg.Identity.HostID, Valid: true},
	}
	writeJSON(w, http.StatusOK, s.Operations.Complete(op, result, "init completed"))
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

func (s *Server) probe(ctx context.Context, op *operations.Operation) (coordinatorclient.ProbeResult, *coreerrors.Error) {
	cfg, cfgErr := s.ConfigStore.Load()
	if cfgErr != nil {
		return coordinatorclient.ProbeResult{}, cfgErr
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

func (s *Server) relayService() minirelay.Service {
	service := s.Relay
	if service.HTTPClient == nil {
		service.HTTPClient = s.HTTPClient
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
	case coreerrors.ConfigMissing, coreerrors.ConfigInvalid, coreerrors.ConfigParseError, coreerrors.InvalidRequest:
		return http.StatusBadRequest
	case coreerrors.AuthMissing, coreerrors.AuthInvalid:
		return http.StatusUnauthorized
	case coreerrors.LeaseExpired, coreerrors.NotCurrentHost:
		return http.StatusForbidden
	case coreerrors.ProcessInspectionLimited:
		return http.StatusOK
	case coreerrors.CoordinatorRouteMissing:
		return http.StatusNotFound
	case coreerrors.CoordinatorUnreachable, coreerrors.ProxyInterferenceSuspected:
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
	cfg.Identity.GroupID = "grp_xxx"
	cfg.Identity.MemberID = "mem_xxx"
	cfg.Identity.HostID = "host_xxx"
	cfg.Identity.HostToken = "ht_xxx"
	cfg.Identity.DisplayName = "私人本地主机"
	hostname, _ := os.Hostname()
	cfg.Identity.DeviceName = hostname
	cfg.Identity.Platform = "windows"
	if err := store.Save(cfg); err != nil {
		return "", err
	}
	return filepath.Clean(store.Path), nil
}

func Endpoint(addr string) string {
	if addr == "" {
		addr = DefaultAddr
	}
	return fmt.Sprintf("http://%s", addr)
}
