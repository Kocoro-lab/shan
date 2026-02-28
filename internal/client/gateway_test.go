package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompleteUsesCompletionsEndpoint(t *testing.T) {
	got := struct {
		Messages []Message `json:"messages"`
		Tools    []Tool    `json:"tools"`
	}{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CompletionResponse{
			OutputText:   "hello",
			FinishReason: "end_turn",
			Usage: Usage{
				InputTokens:  3,
				OutputTokens: 4,
				TotalTokens:  7,
			},
		})
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := gw.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
		Tools:    []Tool{{Type: "function"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText != "hello" {
		t.Fatalf("expected output hello, got %s", resp.OutputText)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "ping" {
		t.Errorf("request body messages not preserved")
	}
	if len(got.Tools) != 1 || got.Tools[0].Type != "function" {
		t.Errorf("expected tool payload to include tools")
	}
}

func TestListTools(t *testing.T) {
	tools := []ServerToolSchema{
		{Name: "web_search", Description: "Search the web", Parameters: map[string]any{"type": "object"}},
		{Name: "getStockBars", Description: "Get stock bars"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key=test-key, got %s", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tools)
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "test-key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := gw.ListTools(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	if got[0].Name != "web_search" {
		t.Errorf("expected web_search, got %s", got[0].Name)
	}
	if got[1].Name != "getStockBars" {
		t.Errorf("expected getStockBars, got %s", got[1].Name)
	}
}

func TestListTools_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gw.ListTools(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/web_search/execute" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ToolExecuteRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Arguments["query"] != "golang testing" {
			t.Errorf("expected query=golang testing, got %v", req.Arguments["query"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolExecuteResponse{
			Success: true,
			Output:  json.RawMessage(`{"results":["found 10 results"]}`),
		})
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := gw.ExecuteTool(ctx, "web_search", map[string]any{"query": "golang testing"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if string(resp.Output) != `{"results":["found 10 results"]}` {
		t.Errorf("unexpected output: %s", string(resp.Output))
	}
}

func TestExecuteTool_UrlEscapesName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.RawPath preserves the percent-encoding; r.URL.Path is decoded
		want := "/api/v1/tools/my%2Ftool/execute"
		if r.URL.RawPath != want {
			t.Errorf("expected raw path %s, got %s", want, r.URL.RawPath)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolExecuteResponse{Success: true, Output: json.RawMessage(`"ok"`)})
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gw.ExecuteTool(ctx, "my/tool", map[string]any{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteTool_403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("tool not allowed"))
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gw.ExecuteTool(ctx, "dangerous_tool", map[string]any{}, "")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}
