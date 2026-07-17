package localapi

import (
	"archive/zip"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
	"github.com/Ruichen-0079/ACBH/agent/internal/hobbyagent"
)

const maxJSONBody = 1024 * 1024

//go:embed web/index.html
var indexHTML []byte

//go:embed web/app.css
var appCSS []byte

//go:embed web/app.js
var appJS []byte

type Runtime interface {
	Config() (hobbyagent.PublicConfig, error)
	UpdateConfig(hobbyagent.Config) (hobbyagent.PublicConfig, error)
	Start() hobbyagent.Operation
	Stop() hobbyagent.Operation
	Operation(string) (hobbyagent.Operation, bool)
	Status() hobbyagent.RuntimeStatus
	Events(int) []componentstate.Event
	Logs(int) []string
	Diagnostics(context.Context) map[string]any
	LogDirectory() string
}

type Server struct {
	runtime Runtime
}

func New(runtime Runtime) *Server { return &Server{runtime: runtime} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", static(indexHTML, "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.css", static(appCSS, "text/css; charset=utf-8"))
	mux.HandleFunc("GET /app.js", static(appJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /local/v1/status", s.status)
	mux.HandleFunc("GET /local/v1/config", s.getConfig)
	mux.HandleFunc("PUT /local/v1/config", s.putConfig)
	mux.HandleFunc("POST /local/v1/start", s.start)
	mux.HandleFunc("POST /local/v1/stop", s.stop)
	mux.HandleFunc("GET /local/v1/operations/{id}", s.operation)
	mux.HandleFunc("GET /local/v1/events", s.events)
	mux.HandleFunc("GET /local/v1/diagnostics", s.diagnostics)
	mux.HandleFunc("GET /local/v1/diagnostics/export", s.exportDiagnostics)
	mux.HandleFunc("GET /local/v1/logs", s.logs)
	mux.HandleFunc("POST /local/v1/logs/open", s.openLogs)
	return securityHeaders(mux)
}

func static(content []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(content)
	}
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Status())
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	config, err := s.runtime.Config()
	if err != nil {
		writeError(w, http.StatusNotFound, "config_not_found", err)
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) putConfig(w http.ResponseWriter, request *http.Request) {
	var input hobbyagent.Config
	if err := decodeStrictJSON(request.Body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err)
		return
	}
	config, err := s.runtime.UpdateConfig(input)
	if err != nil {
		if hobbyagent.ErrorCode(err) == hobbyagent.CodeConfigLockedWhileRunning {
			writeError(w, http.StatusConflict, hobbyagent.CodeConfigLockedWhileRunning, err)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_config", err)
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) start(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, s.runtime.Start())
}

func (s *Server) stop(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, s.runtime.Stop())
}

func (s *Server) operation(w http.ResponseWriter, request *http.Request) {
	operation, ok := s.runtime.Operation(request.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "operation_not_found", errors.New("operation was not found"))
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (s *Server) events(w http.ResponseWriter, request *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"events": s.runtime.Events(queryLimit(request, 500))})
}

func (s *Server) diagnostics(w http.ResponseWriter, request *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Diagnostics(request.Context()))
}

func (s *Server) exportDiagnostics(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="acbh-diagnostics.zip"`)
	w.WriteHeader(http.StatusOK)
	archive := zip.NewWriter(w)
	defer archive.Close()
	files := []struct {
		name  string
		value any
	}{
		{name: "diagnostics.json", value: s.runtime.Diagnostics(request.Context())},
		{name: "recent-logs.json", value: map[string]any{"logs": s.runtime.Logs(500)}},
		{name: "state-transitions.json", value: map[string]any{"events": s.runtime.Events(500)}},
	}
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			return
		}
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(file.value); err != nil {
			return
		}
	}
}

func (s *Server) logs(w http.ResponseWriter, request *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.runtime.Logs(queryLimit(request, 200))})
}

func (s *Server) openLogs(w http.ResponseWriter, _ *http.Request) {
	opened, err := openDirectory(s.runtime.LogDirectory())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "open_log_directory_failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"opened":   opened,
		"protocol": "acbh://open-logs",
	})
}

func ListenAndServe(ctx context.Context, address string, handler http.Handler) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid local API address: %w", err)
	}
	if host != "127.0.0.1" {
		return errors.New("local API must bind to 127.0.0.1")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: loopbackHostOnly(handler), ReadHeaderTimeout: 5 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}

func loopbackHostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		host := request.Host
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		if host != "127.0.0.1" {
			writeError(w, http.StatusForbidden, "loopback_host_required", errors.New("local API requires the 127.0.0.1 host"))
			return
		}
		next.ServeHTTP(w, request)
	})
}

func decodeStrictJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxJSONBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON object")
		}
		return err
	}
	return nil
}

func queryLimit(request *http.Request, fallback int) int {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	if parsed > 500 {
		return 500
	}
	return parsed
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string, err error) {
	message := strings.TrimSpace(err.Error())
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'")
		if request.Method == http.MethodPost || request.Method == http.MethodPut || request.Method == http.MethodPatch || request.Method == http.MethodDelete {
			origin := request.Header.Get("Origin")
			if origin != "" && !sameLoopbackOrigin(origin, request.Host) {
				writeError(w, http.StatusForbidden, "cross_origin_request_rejected", errors.New("cross-origin local API mutation was rejected"))
				return
			}
		}
		next.ServeHTTP(w, request)
	})
}

func sameLoopbackOrigin(origin, requestHost string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "http" && parsed.Host == requestHost && parsed.Hostname() == "127.0.0.1"
}
