package localcontrol

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcserver"
	"github.com/Ruichen-0079/ACBH/agent/internal/rcon"
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
)

type Server struct {
	ListenAddr  string
	Token       string
	Config      *agentconfig.Config
	AllowRemote bool
	logger      *log.Logger
}

func GenerateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func NewServer(listenAddr, token string, cfg *agentconfig.Config) *Server {
	return &Server{
		ListenAddr: listenAddr,
		Token:      token,
		Config:     cfg,
		logger:     log.New(os.Stderr, "[control] ", log.LstdFlags),
	}
}

func (s *Server) Run(ctx context.Context) error {
	if strings.TrimSpace(s.Token) == "" {
		return errors.New("control: bearer token is required")
	}
	remote, err := validateListenAddress(s.ListenAddr)
	if err != nil {
		return err
	}
	if remote && !s.AllowRemote {
		return errors.New("control: refusing non-loopback listen address; pass --allow-remote-control to acknowledge remote exposure")
	}
	if remote {
		s.logger.Printf("WARNING: local control API is exposed on non-loopback address %s", s.ListenAddr)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/doctor", s.withAuth(s.handleDoctor))
	mux.HandleFunc("/v1/scan", s.withAuth(s.handleScan))
	mux.HandleFunc("/v1/safe-sync", s.withAuth(s.handleSafeSync))
	mux.HandleFunc("/v1/push", s.withAuth(s.handlePush))
	mux.HandleFunc("/v1/pull", s.withAuth(s.handlePull))
	mux.HandleFunc("/v1/server/status", s.withAuth(s.handleServerStatus))
	mux.HandleFunc("/v1/server/start", s.withAuth(s.handleServerStart))
	mux.HandleFunc("/v1/server/stop", s.withAuth(s.handleServerStop))

	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return fmt.Errorf("control: listen on %s failed: %w", s.ListenAddr, err)
	}
	defer ln.Close()

	s.ListenAddr = ln.Addr().String()
	s.logger.Printf("Agent control API listening on http://%s", s.ListenAddr)
	s.logger.Printf("Dashboard: open Coordinator /dashboard and connect to http://%s", s.ListenAddr)

	h := corsMiddleware(mux)

	srv := &http.Server{Handler: h}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != http.ErrServerClosed {
		return fmt.Errorf("control: server error: %w", err)
	}
	return nil
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "missing or invalid Authorization header"})
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if !secureTokenEqual(token, s.Token) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid token"})
			return
		}
		next(w, r)
	}
}

func secureTokenEqual(provided, expected string) bool {
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func validateListenAddress(listenAddr string) (bool, error) {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false, fmt.Errorf("control: invalid listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return false, nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return false, nil
	}
	return true, nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"service":  "acbh-agent-control",
		"version":  agentconfig.AgentVersion,
		"platform": runtime.GOOS,
		"arch":     runtime.GOARCH,
		"pid":      os.Getpid(),
		"hostname": hostname(),
	})
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		ServerDir string `json:"serverDir"`
		JavaPath  string `json:"javaPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}
	if req.JavaPath == "" {
		req.JavaPath = "java"
	}

	configPath, _ := agentconfig.DefaultPath()
	cfgExists := false
	if configPath != "" {
		cfgExists = agentconfig.Exists(configPath)
	}

	javaPath, javaErr := exec.LookPath(req.JavaPath)
	javaAvailable := javaErr == nil
	if !javaAvailable {
		javaPath = "not found"
	}

	serverDirExists := false
	if req.ServerDir != "" {
		if info, err := os.Stat(req.ServerDir); err == nil && info.IsDir() {
			serverDirExists = true
		}
	}

	checks := []map[string]any{
		{"name": "os", "ok": true, "value": runtime.GOOS},
		{"name": "arch", "ok": true, "value": runtime.GOARCH},
		{"name": "cpuCores", "ok": true, "value": runtime.NumCPU()},
		{"name": "hostname", "ok": true, "value": hostname()},
		{"name": "configExists", "ok": cfgExists, "value": configPath},
		{"name": "javaAvailable", "ok": javaAvailable, "value": javaPath},
		{"name": "serverDir", "ok": serverDirExists, "value": req.ServerDir},
	}

	allOK := true
	for _, c := range checks {
		if !c["ok"].(bool) {
			allOK = false
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     allOK,
		"checks": checks,
	})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		ServerDir     string `json:"serverDir"`
		ArtifactKind  string `json:"artifactKind"`
		ArtifactID    string `json:"artifactId"`
		GroupID       string `json:"groupId"`
		CreatorHostID string `json:"creatorHostId"`
		Output        string `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}
	if req.ServerDir == "" || req.ArtifactID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "serverDir and artifactId are required"})
		return
	}

	gid := req.GroupID
	chid := req.CreatorHostID
	if s.Config != nil {
		if gid == "" {
			gid = s.Config.GroupID
		}
		if chid == "" {
			chid = s.Config.HostID
		}
	}

	kind := manifest.ArtifactKind(req.ArtifactKind)
	if kind == "" {
		kind = manifest.ServerPack
	}

	_, report, err := scanner.Scan(scanner.Options{
		ServerDir:     req.ServerDir,
		ArtifactKind:  kind,
		ArtifactID:    req.ArtifactID,
		GroupID:       gid,
		CreatorHostID: chid,
		OutputPath:    req.Output,
	})
	if err != nil {
		s.writeOperationError(w, "scan_failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"manifestPath": report.OutputPath,
		"summary": map[string]any{
			"included": report.IncludedFiles,
			"ignored":  report.IgnoredFiles,
			"unknown":  report.UnknownFiles,
			"deleted":  report.DeletedFiles,
			"bytes":    report.TotalBytes,
		},
	})
}

func (s *Server) handleSafeSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		ServerDir         string `json:"serverDir"`
		RCONHost          string `json:"rconHost"`
		RCONPort          int    `json:"rconPort"`
		RCONPassword      string `json:"rconPassword"`
		ArtifactID        string `json:"artifactId"`
		GroupID           string `json:"groupId"`
		CreatorHostID     string `json:"creatorHostId"`
		ServerPackVersion string `json:"serverPackVersion"`
		Output            string `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}
	if req.ServerDir == "" || req.RCONPassword == "" || req.ArtifactID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "serverDir, rconPassword, and artifactId are required"})
		return
	}
	if req.RCONHost == "" {
		req.RCONHost = "127.0.0.1"
	}
	if req.RCONPort == 0 {
		req.RCONPort = 25575
	}

	resp, err := rcon.Execute(r.Context(), rcon.Config{
		Host:     req.RCONHost,
		Port:     req.RCONPort,
		Password: req.RCONPassword,
		Timeout:  10 * time.Second,
	}, "save-all flush")
	if err != nil {
		s.writeOperationError(w, "rcon_failed", err)
		return
	}

	gid := req.GroupID
	chid := req.CreatorHostID
	if s.Config != nil {
		if gid == "" {
			gid = s.Config.GroupID
		}
		if chid == "" {
			chid = s.Config.HostID
		}
	}

	_, report, err := scanner.Scan(scanner.Options{
		ServerDir:         req.ServerDir,
		ArtifactKind:      manifest.WorldSnapshot,
		ArtifactID:        req.ArtifactID,
		GroupID:           gid,
		CreatorHostID:     chid,
		ServerPackVersion: req.ServerPackVersion,
		OutputPath:        req.Output,
	})
	if err != nil {
		s.writeOperationError(w, "scan_failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"rconResponse": resp,
		"manifestPath": report.OutputPath,
		"summary": map[string]any{
			"included": report.IncludedFiles,
			"ignored":  report.IgnoredFiles,
			"unknown":  report.UnknownFiles,
			"deleted":  report.DeletedFiles,
			"bytes":    report.TotalBytes,
		},
	})
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		CoordinatorURL string `json:"coordinatorUrl"`
		ServerDir      string `json:"serverDir"`
		Manifest       string `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}
	if req.ServerDir == "" || req.Manifest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "serverDir and manifest are required"})
		return
	}

	cfg := s.resolveConfig(req.CoordinatorURL)

	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid coordinator URL", "code": "invalid_coordinator_url"})
		return
	}

	summary, err := artifactsync.Push(r.Context(), artifactsync.PushOptions{
		ManifestPath: req.Manifest,
		ServerDir:    req.ServerDir,
		Config:       cfg,
		Client:       client,
	})
	if err != nil {
		s.writeOperationError(w, "push_failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": summary,
	})
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		CoordinatorURL string `json:"coordinatorUrl"`
		ArtifactKind   string `json:"artifactKind"`
		ArtifactID     string `json:"artifactId"`
		OutputDir      string `json:"outputDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}
	if req.ArtifactKind == "" || req.OutputDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "artifactKind and outputDir are required"})
		return
	}
	if req.ArtifactID == "" {
		req.ArtifactID = "latest"
	}

	cfg := s.resolveConfig(req.CoordinatorURL)

	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid coordinator URL", "code": "invalid_coordinator_url"})
		return
	}

	summary, err := artifactsync.Pull(r.Context(), artifactsync.PullOptions{
		ArtifactKind: manifest.ArtifactKind(req.ArtifactKind),
		ArtifactID:   req.ArtifactID,
		OutputDir:    req.OutputDir,
		Config:       cfg,
		Client:       client,
	})
	if err != nil {
		s.writeOperationError(w, "pull_failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": summary,
	})
}

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		ServerDir string `json:"serverDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}

	runtimeDir, err := s.serverRuntimeDir()
	if err != nil {
		s.writeOperationError(w, "server_status_failed", err)
		return
	}

	status, err := mcserver.GetStatus(runtimeDir)
	if err != nil {
		s.writeOperationError(w, "server_status_failed", err)
		return
	}

	resp := map[string]any{"ok": true}
	if status.Running {
		resp["running"] = true
		resp["state"] = map[string]any{
			"pid":       status.State.PID,
			"status":    status.State.Status,
			"serverDir": status.State.ServerDir,
			"command":   status.State.Command,
			"startedAt": status.State.StartedAt,
			"stdoutLog": status.State.StdoutLog,
			"stderrLog": status.State.StderrLog,
		}
	} else if status.Stale {
		resp["stale"] = true
		resp["unknown"] = status.Unknown
		resp["reason"] = status.Reason
		if status.State.PID > 0 {
			resp["state"] = map[string]any{
				"pid":       status.State.PID,
				"status":    status.State.Status,
				"serverDir": status.State.ServerDir,
			}
		} else {
			resp["lock"] = map[string]any{
				"pid":       status.Lock.PID,
				"owner":     status.Lock.Owner,
				"serverDir": status.Lock.ServerDir,
				"createdAt": status.Lock.CreatedAt,
			}
		}
	} else {
		resp["running"] = false
		resp["message"] = "server not running"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleServerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		ServerDir    string   `json:"serverDir"`
		JavaPath     string   `json:"javaPath"`
		JarPath      string   `json:"jarPath"`
		JVMArgs      []string `json:"jvmArgs"`
		ServerArgs   []string `json:"serverArgs"`
		RCONHost     string   `json:"rconHost"`
		RCONPort     int      `json:"rconPort"`
		RCONPassword string   `json:"rconPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}
	if req.ServerDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "serverDir is required"})
		return
	}
	if req.JavaPath == "" {
		req.JavaPath = "java"
	}
	if req.JarPath == "" {
		req.JarPath = "fabric-server-launch.jar"
	}
	if len(req.ServerArgs) == 0 {
		req.ServerArgs = []string{"nogui"}
	}

	cmd := buildServerCommand(req.JavaPath, req.JarPath, req.JVMArgs, req.ServerArgs)

	logDir := filepath.Join(req.ServerDir, "logs")
	runtimeDir, err := s.serverRuntimeDir()
	if err != nil {
		s.writeOperationError(w, "server_start_failed", err)
		return
	}

	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		s.writeOperationError(w, "server_start_failed", err)
		return
	}

	executable, err := os.Executable()
	if err != nil {
		s.writeOperationError(w, "server_start_failed", err)
		return
	}

	state, err := mcserver.Start(r.Context(), executable, mcserver.StartOptions{
		ServerDir:   req.ServerDir,
		Command:     cmd.display,
		CommandArgv: cmd.argv,
		LogDir:      logDir,
		RuntimeDir:  runtimeDir,
		StopTimeout: 30 * time.Second,
	})
	if err != nil {
		s.writeOperationError(w, "server_start_failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Server started",
		"state": map[string]any{
			"pid":       state.PID,
			"status":    state.Status,
			"serverDir": state.ServerDir,
			"command":   state.Command,
			"startedAt": state.StartedAt,
			"stdoutLog": state.StdoutLog,
			"stderrLog": state.StderrLog,
		},
	})
}

func (s *Server) handleServerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req struct {
		ServerDir      string `json:"serverDir"`
		RCONHost       string `json:"rconHost"`
		RCONPort       int    `json:"rconPort"`
		RCONPassword   string `json:"rconPassword"`
		TimeoutSeconds int    `json:"timeoutSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return
	}

	runtimeDir, err := s.serverRuntimeDir()
	if err != nil {
		s.writeOperationError(w, "server_stop_failed", err)
		return
	}

	state, stopped, err := mcserver.Stop(runtimeDir)
	if err != nil {
		s.writeOperationError(w, "server_stop_failed", err)
		return
	}
	if !stopped {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stopped": false, "message": "Server is not running"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"stopped": true,
		"message": "Server stopped",
		"pid":     state.PID,
	})
}

func (s *Server) serverRuntimeDir() (string, error) {
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(configDir, "runtime"), nil
}

type serverCommand struct {
	display string
	argv    []string
}

func buildServerCommand(javaPath, jarPath string, jvmArgs, serverArgs []string) serverCommand {
	argv := make([]string, 0, 1+len(jvmArgs)+2+len(serverArgs))
	argv = append(argv, javaPath)
	argv = append(argv, jvmArgs...)
	argv = append(argv, "-jar", jarPath)
	argv = append(argv, serverArgs...)
	return serverCommand{
		display: mcserver.DisplayCommand(argv),
		argv:    argv,
	}
}

func (s *Server) resolveConfig(coordinatorURL string) agentconfig.Config {
	cfg := agentconfig.Config{}
	if s.Config != nil {
		cfg = *s.Config
	}
	if coordinatorURL != "" {
		cfg.CoordinatorURL = coordinatorURL
	}
	return cfg
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !isAllowedDashboardOrigin(origin) {
				if r.Method == http.MethodOptions {
					writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "origin is not allowed"})
					return
				}
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeOperationError(w http.ResponseWriter, code string, err error) {
	requestID := GenerateToken()[:12]
	s.logger.Printf("request_id=%s code=%s error=%v", requestID, code, err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"ok":        false,
		"error":     "operation failed",
		"code":      code,
		"requestId": requestID,
	})
}

func isAllowedDashboardOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// Ensure io is used (imported for future streaming)
var _ io.Reader
