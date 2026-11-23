//go:build !android && !ios

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	mcpProtocolVersionLatest = "2025-03-26"
	mcpProtocolVersionCompat = "2024-11-05"
	maxJSONRPCBodyBytes      = 2 * 1024 * 1024
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type desktopService struct {
	deps Dependencies

	mu       sync.RWMutex
	server   *http.Server
	listener net.Listener
	running  bool
	port     int
	lastErr  string

	sessMu   sync.RWMutex
	sessions map[string]time.Time
}

// NewService creates a desktop local-only MCP server service.
func NewService(deps Dependencies) Service {
	return &desktopService{
		deps:     deps,
		port:     DefaultPort,
		sessions: make(map[string]time.Time),
	}
}

func (s *desktopService) Configure(ctx context.Context, enabled bool, port int) error {
	if port < 1024 || port > 65535 {
		port = DefaultPort
	}

	s.mu.Lock()
	alreadyRunning := s.running && s.port == port && s.server != nil
	if !enabled {
		srv := s.server
		ln := s.listener
		s.server = nil
		s.listener = nil
		s.running = false
		s.port = port
		s.lastErr = ""
		s.mu.Unlock()
		return shutdownHTTPServer(ctx, srv, ln)
	}
	if alreadyRunning {
		s.mu.Unlock()
		return nil
	}
	oldSrv := s.server
	oldLn := s.listener
	s.server = nil
	s.listener = nil
	s.running = false
	s.port = port
	s.lastErr = ""
	s.mu.Unlock()

	if oldSrv != nil || oldLn != nil {
		_ = shutdownHTTPServer(ctx, oldSrv, oldLn)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           s.newHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	s.mu.Lock()
	s.server = srv
	s.listener = ln
	s.running = true
	s.port = port
	s.lastErr = ""
	s.mu.Unlock()

	log.Printf("MCP server listening on http://%s/mcp (loopback only)", addr)

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("MCP server error: %v", err)
			s.mu.Lock()
			if s.server == srv {
				s.running = false
				s.lastErr = err.Error()
			}
			s.mu.Unlock()
		}
	}()

	return nil
}

func (s *desktopService) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.server
	ln := s.listener
	s.server = nil
	s.listener = nil
	s.running = false
	s.mu.Unlock()
	return shutdownHTTPServer(ctx, srv, ln)
}

func (s *desktopService) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	endpoint := ""
	if s.running && s.port > 0 {
		endpoint = fmt.Sprintf("http://127.0.0.1:%d/mcp", s.port)
	}
	return Status{
		Supported: true,
		Running:   s.running,
		Port:      s.port,
		Endpoint:  endpoint,
		Error:     s.lastErr,
	}
}

func shutdownHTTPServer(ctx context.Context, srv *http.Server, ln net.Listener) error {
	if srv != nil {
		shutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		err := srv.Shutdown(shutCtx)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

func (s *desktopService) newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	return loopbackOnlyMiddleware(mux)
}

// loopbackOnlyMiddleware enforces three layers of local-only protection:
// 1. RemoteAddr must be a loopback IP (127.0.0.1 or ::1).
// 2. Host header must be localhost, 127.0.0.1, or ::1 (DNS Rebinding defense).
// 3. Origin header, if present, must resolve to localhost, 127.0.0.1, or ::1 (CSRF defense).
func loopbackOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemoteAddr(r.RemoteAddr) {
			http.Error(w, "forbidden: loopback connections only", http.StatusForbidden)
			return
		}
		if !isLoopbackHostHeader(r.Host) {
			http.Error(w, "forbidden: invalid Host header", http.StatusForbidden)
			return
		}
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !isLoopbackOrigin(origin) {
			http.Error(w, "forbidden: cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if zoneIdx := strings.Index(host, "%"); zoneIdx >= 0 {
		host = host[:zoneIdx]
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackHostHeader(hostHeader string) bool {
	host := strings.TrimSpace(hostHeader)
	if host == "" {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return isLoopbackHostname(host)
}

func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return isLoopbackHostname(u.Hostname())
}

func isLoopbackHostname(host string) bool {
	h := strings.ToLower(strings.TrimSpace(strings.Trim(host, "[]")))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (s *desktopService) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleMCPPost(w, r)
	case http.MethodGet:
		s.handleMCPGet(w, r)
	case http.MethodDelete:
		s.handleMCPDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *desktopService) handleMCPGet(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *desktopService) handleMCPDelete(w http.ResponseWriter, r *http.Request) {
	if sid := strings.TrimSpace(r.Header.Get("Mcp-Session-Id")); sid != "" {
		s.sessMu.Lock()
		delete(s.sessions, sid)
		s.sessMu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *desktopService) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRPCBodyBytes+1))
	if err != nil {
		writeJSONRPC(w, http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &jsonRPCError{Code: -32700, Message: "failed to read request body"},
		})
		return
	}
	if len(body) > maxJSONRPCBodyBytes {
		writeJSONRPC(w, http.StatusRequestEntityTooLarge, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &jsonRPCError{Code: -32600, Message: "request body too large"},
		})
		return
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		writeJSONRPC(w, http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &jsonRPCError{Code: -32600, Message: "empty request body"},
		})
		return
	}

	if strings.HasPrefix(trimmed, "[") {
		var reqs []jsonRPCRequest
		if err := json.Unmarshal(body, &reqs); err != nil || len(reqs) == 0 {
			writeJSONRPC(w, http.StatusBadRequest, jsonRPCResponse{
				JSONRPC: "2.0",
				Error:   &jsonRPCError{Code: -32700, Message: "invalid JSON-RPC batch"},
			})
			return
		}
		resps := make([]jsonRPCResponse, 0, len(reqs))
		var newSessionID string
		for _, req := range reqs {
			resp, sid, isNotif := s.dispatchRPC(r.Context(), req)
			if sid != "" {
				newSessionID = sid
			}
			if !isNotif {
				resps = append(resps, resp)
			}
		}
		if newSessionID != "" {
			w.Header().Set("Mcp-Session-Id", newSessionID)
		}
		if len(resps) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, resps)
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPC(w, http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &jsonRPCError{Code: -32700, Message: "invalid JSON"},
		})
		return
	}

	resp, newSessionID, isNotif := s.dispatchRPC(r.Context(), req)
	if newSessionID != "" {
		w.Header().Set("Mcp-Session-Id", newSessionID)
	}
	if isNotif {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSONRPC(w, http.StatusOK, resp)
}

func (s *desktopService) dispatchRPC(ctx context.Context, req jsonRPCRequest) (resp jsonRPCResponse, newSessionID string, isNotification bool) {
	isNotification = len(req.ID) == 0 || string(req.ID) == "null"

	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32600, Message: "unsupported jsonrpc version"},
		}, "", false
	}

	if isNotification {
		// Handle notifications silently (e.g. notifications/initialized, notifications/cancelled).
		return jsonRPCResponse{}, "", true
	}

	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		proto := mcpProtocolVersionLatest
		if p.ProtocolVersion == mcpProtocolVersionCompat {
			proto = mcpProtocolVersionCompat
		}
		sid := newSessionToken()
		s.sessMu.Lock()
		s.sessions[sid] = time.Now().UTC()
		s.sessMu.Unlock()

		version := s.deps.Version
		if version == "" {
			version = "0.1.0"
		}
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": proto,
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
				},
				"serverInfo": map[string]any{
					"name":    "twearth",
					"version": version,
				},
			},
		}, sid, false

	case "ping":
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}, "", false

	case "tools/list":
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": s.listToolDefinitions(),
			},
		}, "", false

	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32602, Message: "invalid tools/call params"},
			}, "", false
		}
		result, rpcErr := s.executeTool(ctx, p.Name, p.Arguments)
		if rpcErr != nil {
			return jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   rpcErr,
			}, "", false
		}
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}, "", false

	default:
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}, "", false
	}
}

func newSessionToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func writeJSONRPC(w http.ResponseWriter, status int, resp jsonRPCResponse) {
	writeJSON(w, status, resp)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
