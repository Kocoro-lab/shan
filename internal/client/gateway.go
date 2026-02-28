package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// --- Public types (used by agent loop) ---

type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type CompletionRequest struct {
	Messages      []Message `json:"messages"`
	ModelTier     string    `json:"model_tier,omitempty"`
	SpecificModel string    `json:"specific_model,omitempty"`
	Temperature   float64   `json:"temperature,omitempty"`
	MaxTokens     int       `json:"max_tokens,omitempty"`
	Tools         []Tool    `json:"tools,omitempty"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ArgumentsString returns arguments as a JSON string regardless of
// whether the server sent a string or an object.
func (fc *FunctionCall) ArgumentsString() string {
	if len(fc.Arguments) == 0 {
		return "{}"
	}
	var s string
	if err := json.Unmarshal(fc.Arguments, &s); err == nil {
		return s
	}
	return string(fc.Arguments)
}

type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type CompletionResponse struct {
	Provider     string        `json:"provider"`
	Model        string        `json:"model"`
	OutputText   string        `json:"output_text"`
	FinishReason string        `json:"finish_reason"`
	FunctionCall *FunctionCall `json:"function_call,omitempty"`
	Usage        Usage         `json:"usage"`
	RequestID    string        `json:"request_id,omitempty"`
	LatencyMs    int           `json:"latency_ms,omitempty"`
	Cached       bool          `json:"cached"`
}


// --- Task/workflow types (used by /research, /swarm) ---

type TaskRequest struct {
	Query            string         `json:"query"`
	SessionID        string         `json:"session_id,omitempty"`
	Context          map[string]any `json:"context,omitempty"`
	ResearchStrategy string         `json:"research_strategy,omitempty"`
}

type TaskStreamResponse struct {
	WorkflowID string `json:"workflow_id"`
	TaskID     string `json:"task_id"`
	StreamURL  string `json:"stream_url"`
}

type TaskStatusResponse struct {
	TaskID     string         `json:"task_id"`
	WorkflowID string         `json:"workflow_id"`
	Status     string         `json:"status"`
	Result     string         `json:"result"`
	Query      string         `json:"query"`
	Usage      map[string]any `json:"usage,omitempty"`
}

// --- Client ---

type GatewayClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewGatewayClient(baseURL, apiKey string) *GatewayClient {
	return &GatewayClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Complete sends a completion request to the gateway's /v1/completions endpoint.
// This endpoint is a thin proxy to the LLM service that returns raw function_call
// responses for client-side tool execution.
func (c *GatewayClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody := readResponseBody(resp)
		if errBody != "" {
			return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, errBody)
		}
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var result CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

func (c *GatewayClient) SubmitTaskStream(ctx context.Context, req TaskRequest) (*TaskStreamResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tasks/stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body := readResponseBody(resp)
		if body != "" {
			return nil, fmt.Errorf("gateway returned %d (expected 201): %s", resp.StatusCode, body)
		}
		return nil, fmt.Errorf("gateway returned %d (expected 201)", resp.StatusCode)
	}

	var result TaskStreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

func (c *GatewayClient) StreamURL(workflowID string) string {
	return fmt.Sprintf("%s/api/v1/stream/sse?workflow_id=%s", c.baseURL, workflowID)
}

// ResolveURL prepends the base URL if the given URL is relative (starts with /).
func (c *GatewayClient) ResolveURL(u string) string {
	if len(u) > 0 && u[0] == '/' {
		return c.baseURL + u
	}
	return u
}

func (c *GatewayClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := readResponseBody(resp)
		if body != "" {
			return fmt.Errorf("health check returned %d: %s", resp.StatusCode, body)
		}
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

// --- Server tool types (used by tools.RegisterAll) ---

type ServerToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ToolExecuteRequest struct {
	Arguments map[string]any `json:"arguments"`
	SessionID string         `json:"session_id,omitempty"`
}

type ToolExecuteResponse struct {
	Success         bool            `json:"success"`
	Output          json.RawMessage `json:"output"`
	Text            *string         `json:"text"`
	Error           *string         `json:"error"`
	ExecutionTimeMs int             `json:"execution_time_ms,omitempty"`
}

// ListTools fetches available server-side tool schemas from the gateway.
func (c *GatewayClient) ListTools(ctx context.Context) ([]ServerToolSchema, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/tools", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody := readResponseBody(resp)
		if errBody != "" {
			return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, errBody)
		}
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var tools []ServerToolSchema
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return tools, nil
}

// ExecuteTool calls a server-side tool by name with the given arguments.
func (c *GatewayClient) ExecuteTool(ctx context.Context, name string, arguments map[string]any, sessionID string) (*ToolExecuteResponse, error) {
	reqBody := ToolExecuteRequest{
		Arguments: arguments,
		SessionID: sessionID,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := c.baseURL + "/api/v1/tools/" + url.PathEscape(name) + "/execute"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody := readResponseBody(resp)
		if errBody != "" {
			return nil, fmt.Errorf("tool %s returned %d: %s", name, resp.StatusCode, errBody)
		}
		return nil, fmt.Errorf("tool %s returned %d", name, resp.StatusCode)
	}

	var result ToolExecuteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

func readResponseBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}
