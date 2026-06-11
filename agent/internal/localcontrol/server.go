package localcontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/rcon"
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
)

type Server struct {
	ListenAddr string
	Token      string
	Config     *agentconfig.Config
	logger     *log.Logger
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
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/doctor", s.withAuth(s.handleDoctor))
	mux.HandleFunc("/v1/scan", s.withAuth(s.handleScan))
	mux.HandleFunc("/v1/safe-sync", s.withAuth(s.handleSafeSync))
	mux.HandleFunc("/v1/push", s.withAuth(s.handlePush))
	mux.HandleFunc("/v1/pull", s.withAuth(s.handlePull))

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
		if token != s.Token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid token"})
			return
		}
		next(w, r)
	}
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
		ServerDir      string `json:"serverDir"`
		ArtifactKind   string `json:"artifactKind"`
		ArtifactID     string `json:"artifactId"`
		GroupID        string `json:"groupId"`
		CreatorHostID  string `json:"creatorHostId"`
		Output         string `json:"output"`
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
		ServerDir:    req.ServerDir,
		ArtifactKind: kind,
		ArtifactID:   req.ArtifactID,
		GroupID:      gid,
		CreatorHostID: chid,
		OutputPath:   req.Output,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "RCON failed: " + err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "scan failed: " + err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid coordinator URL: " + err.Error()})
		return
	}

	summary, err := artifactsync.Push(r.Context(), artifactsync.PushOptions{
		ManifestPath: req.Manifest,
		ServerDir:    req.ServerDir,
		Config:       cfg,
		Client:       client,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid coordinator URL: " + err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": summary,
	})
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
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
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
