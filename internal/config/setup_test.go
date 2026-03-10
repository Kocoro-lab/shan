package config

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsSetup(t *testing.T) {
	cases := []struct {
		name     string
		apiKey   string
		endpoint string
		want     bool
	}{
		{"empty key remote", "", "https://api-dev.shannon.run", true},
		{"key set remote", "sk-abc", "https://api-dev.shannon.run", false},
		{"empty key localhost", "", "http://localhost:8080", false},
		{"empty key 127", "", "http://127.0.0.1:8080", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{APIKey: tc.apiKey, Endpoint: tc.endpoint}
			got := NeedsSetup(cfg)
			if got != tc.want {
				t.Errorf("NeedsSetup() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsLocalEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{"http://localhost:8080", true},
		{"http://127.0.0.1:8080", true},
		{"http://[::1]:8080", true},
		{"http://0.0.0.0:8080", true},
		{"https://api-dev.shannon.run", false},
		{"https://shannon.run", false},
		{"not-a-url", false},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint, func(t *testing.T) {
			got := isLocalEndpoint(tc.endpoint)
			if got != tc.want {
				t.Errorf("isLocalEndpoint(%q) = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

func TestCheckEndpointHealth(t *testing.T) {
	t.Run("200 OK returns nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if err := checkEndpointHealth(srv.URL, ""); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("500 returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		if err := checkEndpointHealth(srv.URL, ""); err == nil {
			t.Error("expected error for 500, got nil")
		}
	})

	t.Run("unreachable returns error", func(t *testing.T) {
		if err := checkEndpointHealth("http://127.0.0.1:1", ""); err == nil {
			t.Error("expected error for unreachable endpoint, got nil")
		}
	})

	t.Run("API key sent in header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-API-Key"); got != "test-key" {
				t.Errorf("X-API-Key = %q, want %q", got, "test-key")
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if err := checkEndpointHealth(srv.URL, "test-key"); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})
}

// setupTempHome sets HOME to a temp dir and initializes viper so config.Save() works in tests.
func setupTempHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	shannonDir := filepath.Join(tmp, ".shannon")
	if err := os.MkdirAll(shannonDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Initialize viper with the temp config file so Save() knows where to write.
	if _, err := Load(); err != nil {
		t.Fatalf("setupTempHome: Load() failed: %v", err)
	}
}

func TestRunSetup_SuccessLocal_WithKey(t *testing.T) {
	setupTempHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use the test server as endpoint. httptest binds to 127.0.0.1 (local),
	// so this exercises the local-endpoint path with a non-empty key entered.
	// We verify health check succeeds (OK in output) and API key is saved.
	// Hint correctness is covered by TestRunSetup_HintMessages.
	input := strings.NewReader(srv.URL + "\nmy-api-key\n")
	var out bytes.Buffer

	cfg := &Config{}
	if err := RunSetup(cfg, input, &out); err != nil {
		t.Fatalf("RunSetup() error: %v", err)
	}

	if cfg.APIKey != "my-api-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "my-api-key")
	}
	outStr := out.String()
	if !strings.Contains(outStr, "OK") {
		t.Errorf("output missing OK confirmation, got: %s", outStr)
	}
}

func TestRunSetup_SuccessLocal(t *testing.T) {
	setupTempHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use localhost URL (httptest server is on 127.0.0.1 which is local)
	input := strings.NewReader(srv.URL + "\n\n")
	var out bytes.Buffer

	cfg := &Config{}
	if err := RunSetup(cfg, input, &out); err != nil {
		t.Fatalf("RunSetup() error: %v", err)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "https://github.com/Kocoro-lab/Shannon") {
		t.Errorf("output missing local hint, got: %s", outStr)
	}
}

func TestRunSetup_HintMessages(t *testing.T) {
	t.Run("cloud hint", func(t *testing.T) {
		setupTempHome(t)
		// Use a cloud (non-local) endpoint: press Enter to accept default https://api-dev.shannon.run.
		// Health check will fail (no real server), but hint is printed before the check.
		// RunSetup ignores the health error and saves anyway.
		input := strings.NewReader("\ntest-key\nN\n")
		var out bytes.Buffer
		cfg := &Config{}
		_ = RunSetup(cfg, input, &out)
		if !strings.Contains(out.String(), "https://shannon.run") {
			t.Errorf("cloud hint missing from output: %s", out.String())
		}
	})

	t.Run("local hint", func(t *testing.T) {
		setupTempHome(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		// 127.0.0.1 is a local endpoint
		input := strings.NewReader(srv.URL + "\n\n")
		var out bytes.Buffer
		cfg := &Config{}
		_ = RunSetup(cfg, input, &out)
		if !strings.Contains(out.String(), "https://github.com/Kocoro-lab/Shannon") {
			t.Errorf("local hint missing from output: %s", out.String())
		}
	})
}

func TestRunSetup_RetryOnce(t *testing.T) {
	setupTempHome(t)
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	// endpoint, first key (fails), Y to retry, second key (succeeds)
	input := strings.NewReader(srv.URL + "\nfirst-key\nY\nsecond-key\n")
	var out bytes.Buffer
	cfg := &Config{}
	if err := RunSetup(cfg, input, &out); err != nil {
		t.Fatalf("RunSetup() error: %v", err)
	}
	if cfg.APIKey != "second-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "second-key")
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Re-enter credentials") {
		t.Errorf("expected retry prompt in output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "OK") {
		t.Errorf("expected OK after retry, got: %s", outStr)
	}
}

func TestRunSetup_RetryGiveUp(t *testing.T) {
	setupTempHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// endpoint, bad key, N to give up
	input := strings.NewReader(srv.URL + "\nbad-key\nN\n")
	var out bytes.Buffer
	cfg := &Config{}
	if err := RunSetup(cfg, input, &out); err != nil {
		t.Fatalf("RunSetup() unexpected error: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Re-run") {
		t.Errorf("expected warning about re-running, got: %s", outStr)
	}
}

func TestRunSetup_MaxRetries(t *testing.T) {
	setupTempHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// endpoint, 3 keys with Y between each — hits max
	input := strings.NewReader(srv.URL + "\nkey1\nY\nkey2\nY\nkey3\n")
	var out bytes.Buffer
	cfg := &Config{}
	if err := RunSetup(cfg, input, &out); err != nil {
		t.Fatalf("RunSetup() unexpected error: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Re-run") {
		t.Errorf("expected final warning after max retries, got: %s", outStr)
	}
}
