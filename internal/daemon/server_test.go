package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestServer_Health(t *testing.T) {
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("body = %v", body)
	}
}

func TestServer_Status(t *testing.T) {
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/status", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		IsConnected bool   `json:"is_connected"`
		ActiveAgent string `json:"active_agent"`
		Uptime      int    `json:"uptime"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.IsConnected {
		t.Error("should not be connected")
	}
	if body.Uptime < 0 {
		t.Error("uptime should be non-negative")
	}
}

func TestServer_Shutdown(t *testing.T) {
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c)
	ctx, cancel := context.WithCancel(context.Background())

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	cancel()
	time.Sleep(200 * time.Millisecond)

	_, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", srv.Port()))
	if err == nil {
		t.Error("expected connection refused after shutdown")
	}
}
