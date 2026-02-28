package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTP_Info(t *testing.T) {
	tool := &HTTPTool{}
	info := tool.Info()
	if info.Name != "http" {
		t.Errorf("expected name 'http', got %q", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "url" {
		t.Errorf("expected required [url], got %v", info.Required)
	}
}

func TestHTTP_InvalidArgs(t *testing.T) {
	tool := &HTTPTool{}
	result, err := tool.Run(context.Background(), `not valid json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}
}

func TestHTTP_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("X-Test", "hello")
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := &HTTPTool{}
	result, err := tool.Run(context.Background(), `{"url": "`+srv.URL+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !contains(result.Content, "200") {
		t.Errorf("expected status 200 in output, got: %s", result.Content)
	}
	if !contains(result.Content, "ok") {
		t.Errorf("expected body 'ok' in output, got: %s", result.Content)
	}
}

func TestHTTP_POST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(201)
		w.Write([]byte("created"))
	}))
	defer srv.Close()

	tool := &HTTPTool{}
	result, err := tool.Run(context.Background(), `{"url": "`+srv.URL+`", "method": "POST", "body": "test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !contains(result.Content, "201") {
		t.Errorf("expected status 201 in output, got: %s", result.Content)
	}
}

func TestHTTP_InvalidURL(t *testing.T) {
	tool := &HTTPTool{}
	result, err := tool.Run(context.Background(), `{"url": "http://invalid.localhost.test:99999/nope"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid URL")
	}
}

func TestHTTP_RequiresApproval(t *testing.T) {
	tool := &HTTPTool{}
	if !tool.RequiresApproval() {
		t.Error("expected RequiresApproval to return true")
	}
}

func TestHTTP_IsSafeArgs(t *testing.T) {
	tool := &HTTPTool{}
	tests := []struct {
		argsJSON string
		safe     bool
	}{
		{`{"url": "http://localhost:8080/api"}`, true},
		{`{"url": "http://127.0.0.1:3000/test"}`, true},
		{`{"url": "http://localhost/path", "method": "GET"}`, true},
		{`{"url": "http://localhost/path", "method": "POST"}`, false},
		{`{"url": "https://example.com/api"}`, false},
		{`{"url": "https://example.com/api", "method": "GET"}`, false},
		{`not valid json`, false},
	}
	for _, tt := range tests {
		if tool.IsSafeArgs(tt.argsJSON) != tt.safe {
			t.Errorf("IsSafeArgs(%q) = %v, want %v", tt.argsJSON, !tt.safe, tt.safe)
		}
	}
}
