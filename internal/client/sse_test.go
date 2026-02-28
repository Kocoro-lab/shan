package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSEClient_ReadEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "id: 1\nevent: AGENT_STARTED\ndata: {\"agent_id\":\"shibuya\",\"message\":\"planning\"}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "id: 2\nevent: done\ndata: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make([]SSEEvent, 0)
	err := StreamSSE(ctx, server.URL, "", func(ev SSEEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) < 1 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Event != "AGENT_STARTED" {
		t.Errorf("expected AGENT_STARTED, got %s", events[0].Event)
	}
}

func TestSSEClient_MultiLineData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		fmt.Fprintf(w, "event: test\ndata: line one\ndata: line two\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "event: done\ndata: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make([]SSEEvent, 0)
	err := StreamSSE(ctx, server.URL, "", func(ev SSEEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) < 1 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Data != "line one\nline two" {
		t.Errorf("expected multi-line data %q, got %q", "line one\nline two", events[0].Data)
	}
}
