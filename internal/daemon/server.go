package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kocoro-lab/shan/internal/agent"
	"github.com/Kocoro-lab/shan/internal/agents"
	"github.com/Kocoro-lab/shan/internal/config"
	"github.com/Kocoro-lab/shan/internal/session"
	"github.com/Kocoro-lab/shan/internal/skills"
	"github.com/Kocoro-lab/shan/internal/tools"
	"gopkg.in/yaml.v3"
)

type Server struct {
	port     int
	client   *Client
	deps     *ServerDeps
	server   *http.Server
	listener net.Listener
	version  string
	cancel   context.CancelFunc
}

func NewServer(port int, client *Client, deps *ServerDeps, version string) *Server {
	return &Server{port: port, client: client, deps: deps, version: version}
}

func (s *Server) Port() int {
	if s.listener != nil {
		return s.listener.Addr().(*net.TCPAddr).Port
	}
	return s.port
}

// SetCancelFunc sets a cancel function that handleShutdown will call to stop the daemon.
func (s *Server) SetCancelFunc(cancel context.CancelFunc) {
	s.cancel = cancel
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /agents", s.handleAgents)
	mux.HandleFunc("GET /agents/{name}", s.handleGetAgent)
	mux.HandleFunc("POST /agents", s.handleCreateAgent)
	mux.HandleFunc("PUT /agents/{name}", s.handleUpdateAgent)
	mux.HandleFunc("DELETE /agents/{name}", s.handleDeleteAgent)
	mux.HandleFunc("PUT /agents/{name}/config", s.handlePutAgentConfig)
	mux.HandleFunc("DELETE /agents/{name}/config", s.handleDeleteAgentConfig)
	mux.HandleFunc("PUT /agents/{name}/commands/{cmd}", s.handlePutCommand)
	mux.HandleFunc("DELETE /agents/{name}/commands/{cmd}", s.handleDeleteCommand)
	mux.HandleFunc("PUT /agents/{name}/skills/{skill}", s.handlePutSkill)
	mux.HandleFunc("DELETE /agents/{name}/skills/{skill}", s.handleDeleteSkill)
	mux.HandleFunc("GET /config", s.handleGetConfig)
	mux.HandleFunc("PATCH /config", s.handlePatchConfig)
	mux.HandleFunc("POST /config/reload", s.handleConfigReload)
	mux.HandleFunc("GET /instructions", s.handleGetInstructions)
	mux.HandleFunc("PUT /instructions", s.handlePutInstructions)
	mux.HandleFunc("GET /sessions", s.handleSessions)
	mux.HandleFunc("GET /sessions/search", s.handleSessionSearch)
	mux.HandleFunc("POST /message", s.handleMessage)
	mux.HandleFunc("POST /shutdown", s.handleShutdown)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("daemon server listen: %w", err)
	}
	s.listener = ln
	s.server = &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.server.Shutdown(shutCtx)
	}()

	if err := s.server.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "shutting_down"})
	if s.cancel != nil {
		log.Println("daemon: shutdown requested via /shutdown")
		go s.cancel()
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"is_connected": s.client.IsConnected(),
		"active_agent": s.client.ActiveAgent(),
		"uptime":       int(s.client.Uptime().Seconds()),
		"version":      s.version,
	})
}

// handleAgents lists available agents with optional memory status.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.deps == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"agents": []interface{}{}})
		return
	}

	names, err := agents.ListAgents(s.deps.AgentsDir)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	type agentInfo struct {
		Name      string `json:"name"`
		HasMemory bool   `json:"has_memory"`
	}
	result := make([]agentInfo, 0, len(names))
	for _, name := range names {
		memPath := filepath.Join(s.deps.AgentsDir, name, "MEMORY.md")
		_, statErr := os.Stat(memPath)
		result = append(result, agentInfo{Name: name, HasMemory: statErr == nil})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"agents": result})
}

// handleSessions lists sessions, optionally filtered by agent.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.deps == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"sessions": []interface{}{}})
		return
	}

	agentName := r.URL.Query().Get("agent")
	if agentName != "" {
		if err := agents.ValidateAgentName(agentName); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
	}
	mgr := s.deps.SessionCache.GetOrCreate(agentName)
	summaries, err := mgr.List()
	if err != nil {
		// If the directory doesn't exist, return empty list.
		if os.IsNotExist(err) {
			json.NewEncoder(w).Encode(map[string]interface{}{"sessions": []interface{}{}})
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []session.SessionSummary{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": summaries})
}

func (s *Server) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.deps == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
		return
	}

	agentName := r.URL.Query().Get("agent")
	if agentName != "" {
		if err := agents.ValidateAgentName(agentName); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error":"q parameter required"}`, http.StatusBadRequest)
		return
	}

	mgr := s.deps.SessionCache.GetOrCreate(agentName)
	results, err := mgr.Search(query, 20)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []session.SearchResult{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

// handleMessage runs an agent turn via POST. Supports synchronous JSON and SSE streaming.
func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if s.deps == nil {
		http.Error(w, `{"error":"daemon deps not configured"}`, http.StatusInternalServerError)
		return
	}

	var req RunAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.handleMessageSSE(w, r, req)
		return
	}

	handler := &httpEventHandler{}
	result, err := RunAgent(r.Context(), s.deps, req, handler)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleMessageSSE streams agent events as SSE.
func (s *Server) handleMessageSSE(w http.ResponseWriter, r *http.Request, req RunAgentRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	handler := &sseEventHandler{w: w, flusher: flusher}
	result, err := RunAgent(r.Context(), s.deps, req, handler)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", mustJSON(map[string]string{"error": err.Error()}))
		flusher.Flush()
		return
	}

	fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSON(result))
	flusher.Flush()
}

// httpEventHandler is an EventHandler for synchronous HTTP responses.
type httpEventHandler struct{}

func (h *httpEventHandler) OnToolCall(name string, args string) {}
func (h *httpEventHandler) OnToolResult(name string, args string, result agent.ToolResult, elapsed time.Duration) {
	log.Printf("http: tool %s completed (%.1fs)", name, elapsed.Seconds())
}
func (h *httpEventHandler) OnText(text string)            {}
func (h *httpEventHandler) OnStreamDelta(delta string)    {}
func (h *httpEventHandler) OnUsage(usage agent.TurnUsage) {}
func (h *httpEventHandler) OnApprovalNeeded(tool string, args string) bool {
	if DaemonDeniedTools[tool] {
		log.Printf("http: denied %s (not auto-approved in daemon mode)", tool)
		return false
	}
	return true
}

// sseEventHandler streams agent events as SSE to an HTTP response.
type sseEventHandler struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (h *sseEventHandler) OnToolCall(name string, args string) {
	data := mustJSON(map[string]interface{}{"tool": name, "status": "running"})
	fmt.Fprintf(h.w, "event: tool\ndata: %s\n\n", data)
	h.flusher.Flush()
}

func (h *sseEventHandler) OnToolResult(name string, args string, result agent.ToolResult, elapsed time.Duration) {
	data := mustJSON(map[string]interface{}{
		"tool":    name,
		"status":  "completed",
		"elapsed": elapsed.Seconds(),
	})
	fmt.Fprintf(h.w, "event: tool\ndata: %s\n\n", data)
	h.flusher.Flush()
}

func (h *sseEventHandler) OnText(text string) {}

func (h *sseEventHandler) OnStreamDelta(delta string) {
	data := mustJSON(map[string]string{"text": delta})
	fmt.Fprintf(h.w, "event: delta\ndata: %s\n\n", data)
	h.flusher.Flush()
}

func (h *sseEventHandler) OnUsage(usage agent.TurnUsage) {}

func (h *sseEventHandler) OnApprovalNeeded(tool string, args string) bool {
	if DaemonDeniedTools[tool] {
		return false
	}
	return true
}

// mustJSON marshals v to JSON, returning "{}" on error.
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// isJSONNull checks if a json.RawMessage represents a JSON null value.
func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// --- Agent CRUD handlers ---

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := agents.ValidateAgentName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentDir := filepath.Join(s.deps.AgentsDir, name)
	if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent not found: %s", name))
		return
	}
	a, err := agents.LoadAgent(s.deps.AgentsDir, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to load agent %s: %v", name, err))
		return
	}
	writeJSON(w, http.StatusOK, a.ToAPI())
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req agents.AgentCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentDir := filepath.Join(s.deps.AgentsDir, req.Name)
	if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); err == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("agent %q already exists", req.Name))
		return
	}
	if err := agents.WriteAgentPrompt(s.deps.AgentsDir, req.Name, req.Prompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Memory != nil {
		if err := agents.WriteAgentMemory(s.deps.AgentsDir, req.Name, *req.Memory); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write memory: %v", err))
			return
		}
	}
	if req.Config != nil {
		if err := agents.WriteAgentConfig(s.deps.AgentsDir, req.Name, req.Config); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write config: %v", err))
			return
		}
	}
	for name, content := range req.Commands {
		if err := agents.WriteAgentCommand(s.deps.AgentsDir, req.Name, name, content); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write command %s: %v", name, err))
			return
		}
	}
	for _, skill := range req.Skills {
		if skill == nil {
			writeError(w, http.StatusBadRequest, "skill entry cannot be null")
			return
		}
		if err := agents.WriteAgentSkill(s.deps.AgentsDir, req.Name, skill); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write skill %s: %v", skill.Name, err))
			return
		}
	}
	a, err := agents.LoadAgent(s.deps.AgentsDir, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, a.ToAPI())
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := agents.ValidateAgentName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentDir := filepath.Join(s.deps.AgentsDir, name)
	if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}
	var req agents.AgentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// --- Pre-validate all fields before any mutations ---
	if req.Prompt != nil && *req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt cannot be empty")
		return
	}
	var parsedMemory *string
	if req.Memory != nil && !isJSONNull(req.Memory) {
		var mem string
		if err := json.Unmarshal(req.Memory, &mem); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid memory value: %v", err))
			return
		}
		parsedMemory = &mem
	}
	var parsedConfig *agents.AgentConfigAPI
	if req.Config != nil && !isJSONNull(req.Config) {
		var cfg agents.AgentConfigAPI
		if err := json.Unmarshal(req.Config, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid config value: %v", err))
			return
		}
		if cfg.Tools != nil {
			if err := agents.ValidateToolsFilter(cfg.Tools); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		parsedConfig = &cfg
	}
	for cmdName := range req.Commands {
		if err := agents.ValidateCommandName(cmdName); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, skill := range req.Skills {
		if skill == nil {
			writeError(w, http.StatusBadRequest, "skill entry cannot be null")
			return
		}
		if err := agents.ValidateCommandName(skill.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if skill.Type != skills.SkillTypePrompt {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported skill type %q", skill.Type))
			return
		}
		if skill.Prompt == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("skill %q requires a prompt", skill.Name))
			return
		}
	}

	// --- Apply mutations (all inputs validated) ---
	if req.Prompt != nil {
		if err := agents.WriteAgentPrompt(s.deps.AgentsDir, name, *req.Prompt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Memory != nil {
		if isJSONNull(req.Memory) {
			if err := os.Remove(filepath.Join(agentDir, "MEMORY.md")); err != nil && !os.IsNotExist(err) {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete memory: %v", err))
				return
			}
		} else {
			if err := agents.WriteAgentMemory(s.deps.AgentsDir, name, *parsedMemory); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if req.Config != nil {
		if isJSONNull(req.Config) {
			if err := os.Remove(filepath.Join(agentDir, "config.yaml")); err != nil && !os.IsNotExist(err) {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete config: %v", err))
				return
			}
		} else {
			if err := agents.WriteAgentConfig(s.deps.AgentsDir, name, parsedConfig); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	for cmdName, content := range req.Commands {
		if err := agents.WriteAgentCommand(s.deps.AgentsDir, name, cmdName, content); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write command %s: %v", cmdName, err))
			return
		}
	}
	for _, skill := range req.Skills {
		if skill == nil {
			writeError(w, http.StatusBadRequest, "skill entry cannot be null")
			return
		}
		if err := agents.WriteAgentSkill(s.deps.AgentsDir, name, skill); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write skill %s: %v", skill.Name, err))
			return
		}
	}
	a, err := agents.LoadAgent(s.deps.AgentsDir, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.ToAPI())
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := agents.ValidateAgentName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentDir := filepath.Join(s.deps.AgentsDir, name)
	if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}
	s.deps.SessionCache.Lock(name)
	s.deps.SessionCache.Evict(name)
	s.deps.SessionCache.Unlock(name)
	if err := agents.DeleteAgentDir(s.deps.AgentsDir, name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Agent sub-resource handlers ---

// agentExists checks that the agent directory has AGENT.md. Returns false
// and writes a 404 error if the agent does not exist.
func (s *Server) agentExists(w http.ResponseWriter, name string) bool {
	agentDir := filepath.Join(s.deps.AgentsDir, name)
	if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return false
	}
	return true
}

func (s *Server) handlePutAgentConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := agents.ValidateAgentName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.agentExists(w, name) {
		return
	}
	var cfg agents.AgentConfigAPI
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if cfg.Tools != nil {
		if err := agents.ValidateToolsFilter(cfg.Tools); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := agents.WriteAgentConfig(s.deps.AgentsDir, name, &cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteAgentConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := agents.ValidateAgentName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.agentExists(w, name) {
		return
	}
	path := filepath.Join(s.deps.AgentsDir, name, "config.yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handlePutCommand(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("name")
	cmdName := r.PathValue("cmd")
	if err := agents.ValidateAgentName(agentName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.agentExists(w, agentName) {
		return
	}
	if err := agents.ValidateCommandName(cmdName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if err := agents.WriteAgentCommand(s.deps.AgentsDir, agentName, cmdName, body.Content); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteCommand(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("name")
	cmdName := r.PathValue("cmd")
	if err := agents.ValidateAgentName(agentName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.agentExists(w, agentName) {
		return
	}
	if err := agents.ValidateCommandName(cmdName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := agents.DeleteAgentCommand(s.deps.AgentsDir, agentName, cmdName); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handlePutSkill(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("name")
	skillName := r.PathValue("skill")
	if err := agents.ValidateAgentName(agentName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.agentExists(w, agentName) {
		return
	}
	if err := agents.ValidateCommandName(skillName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var skill skills.Skill
	if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	skill.Name = skillName // URL takes precedence
	if skill.Type != skills.SkillTypePrompt {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported skill type %q", skill.Type))
		return
	}
	if skill.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required for prompt-type skills")
		return
	}
	if err := agents.WriteAgentSkill(s.deps.AgentsDir, agentName, &skill); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("name")
	skillName := r.PathValue("skill")
	if err := agents.ValidateAgentName(agentName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.agentExists(w, agentName) {
		return
	}
	if err := agents.ValidateCommandName(skillName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := agents.DeleteAgentSkill(s.deps.AgentsDir, agentName, skillName); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Global config + instructions handlers ---

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	globalPath := filepath.Join(s.deps.ShannonDir, "config.yaml")
	globalData, _ := os.ReadFile(globalPath)
	var globalMap map[string]interface{}
	yaml.Unmarshal(globalData, &globalMap)

	cfg, _ := s.deps.Snapshot()
	effectiveJSON, _ := json.Marshal(cfg)
	var effectiveMap map[string]interface{}
	json.Unmarshal(effectiveJSON, &effectiveMap)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"global":    globalMap,
		"effective": effectiveMap,
	})
}

func (s *Server) handlePatchConfig(w http.ResponseWriter, r *http.Request) {
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	globalPath := filepath.Join(s.deps.ShannonDir, "config.yaml")
	globalData, _ := os.ReadFile(globalPath)
	var current map[string]interface{}
	yaml.Unmarshal(globalData, &current)
	if current == nil {
		current = make(map[string]interface{})
	}

	for key, val := range patch {
		if val == nil {
			delete(current, key)
		} else {
			current[key] = val
		}
	}

	data, err := yaml.Marshal(current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := agents.AtomicWrite(globalPath, data); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	oldCfg, _ := s.deps.Snapshot()

	newCfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("config load failed: %v", err))
		return
	}

	newReg, newCleanup, regErr := tools.RegisterAll(s.deps.GW, newCfg)
	if regErr != nil {
		log.Printf("daemon: reload warning: %v", regErr)
	}
	tools.RegisterCloudDelegate(newReg, s.deps.GW, newCfg, nil, "", "")

	s.deps.mu.Lock()
	oldCleanup := s.deps.Cleanup
	s.deps.Config = newCfg
	s.deps.Registry = newReg
	s.deps.Cleanup = newCleanup
	s.deps.mu.Unlock()

	if oldCleanup != nil {
		oldCleanup()
	}

	resp := map[string]interface{}{"status": "reloaded"}
	if oldCfg != nil && (oldCfg.Endpoint != newCfg.Endpoint || oldCfg.APIKey != newCfg.APIKey) {
		resp["restart_required"] = true
		resp["restart_reason"] = "endpoint or api_key changed — restart daemon to apply"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetInstructions(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.deps.ShannonDir, "instructions.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"content": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(data)})
}

func (s *Server) handlePutInstructions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	path := filepath.Join(s.deps.ShannonDir, "instructions.md")
	if body.Content == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := agents.AtomicWrite(path, []byte(*body.Content)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
