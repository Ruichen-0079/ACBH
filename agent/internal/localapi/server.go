package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/hobbyagent"
)

const maxJSONBody = 1024 * 1024

type Runtime interface {
	Config() (hobbyagent.PublicConfig, error)
	UpdateConfig(hobbyagent.Config) (hobbyagent.PublicConfig, error)
	Import(string) (hobbyagent.PreflightResult, error)
	Start() hobbyagent.Operation
	Stop() hobbyagent.Operation
	Operation(string) (hobbyagent.Operation, bool)
	Status() hobbyagent.RuntimeStatus
	Events(int) []componentEvent
	Logs(int) []string
	Diagnostics(context.Context) map[string]any
}

// componentEvent is an alias-shaped interface boundary that lets the HTTP
// server serialize the concrete state event slice without changing its schema.
type componentEvent = struct {
	Event       string    `json:"event"`
	Component   string    `json:"component"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Reason      string    `json:"reason"`
	OperationID string    `json:"operation_id,omitempty"`
	Time        time.Time `json:"time"`
}

type Server struct {
	runtime *hobbyagent.Runtime
}

func New(runtime *hobbyagent.Runtime) *Server { return &Server{runtime: runtime} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /local/v1/status", s.status)
	mux.HandleFunc("GET /local/v1/config", s.getConfig)
	mux.HandleFunc("PUT /local/v1/config", s.putConfig)
	mux.HandleFunc("POST /local/v1/import", s.importServer)
	mux.HandleFunc("POST /local/v1/start", s.start)
	mux.HandleFunc("POST /local/v1/stop", s.stop)
	mux.HandleFunc("GET /local/v1/operations/{id}", s.operation)
	mux.HandleFunc("GET /local/v1/events", s.events)
	mux.HandleFunc("GET /local/v1/diagnostics", s.diagnostics)
	mux.HandleFunc("GET /local/v1/logs", s.logs)
	return securityHeaders(mux)
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
		writeError(w, http.StatusBadRequest, "invalid_config", err)
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) importServer(w http.ResponseWriter, request *http.Request) {
	var input struct {
		ServerDir string `json:"server_dir"`
	}
	if err := decodeStrictJSON(request.Body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_import", err)
		return
	}
	result, err := s.runtime.Import(input.ServerDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "preflight_failed", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
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

func (s *Server) logs(w http.ResponseWriter, request *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.runtime.Logs(queryLimit(request, 200))})
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
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, request)
	})
}
