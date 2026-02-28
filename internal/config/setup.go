package config

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

const DefaultEndpoint = "https://api-dev.shannon.run"

// NeedsSetup returns true if the config has no API key and the endpoint
// is not a local address (localhost/127.0.0.1 bypass auth).
func NeedsSetup(cfg *Config) bool {
	if cfg.APIKey != "" {
		return false
	}
	return !isLocalEndpoint(cfg.Endpoint)
}

// RunSetup runs the interactive setup flow, prompting the user for
// endpoint and API key. Returns the updated config.
func RunSetup(cfg *Config) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Shannon CLI Setup")
	fmt.Println()

	// Endpoint
	defaultEP := cfg.Endpoint
	if defaultEP == "" {
		defaultEP = DefaultEndpoint
	}
	fmt.Printf("API endpoint [%s]: ", defaultEP)
	epInput, _ := reader.ReadString('\n')
	epInput = strings.TrimSpace(epInput)
	if epInput != "" {
		cfg.Endpoint = epInput
	} else {
		cfg.Endpoint = defaultEP
	}

	// API key (optional for local endpoints)
	if isLocalEndpoint(cfg.Endpoint) {
		fmt.Print("API key (optional for local, Enter to skip): ")
	} else {
		fmt.Print("API key: ")
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println() // newline after masked input
		if err == nil {
			cfg.APIKey = strings.TrimSpace(string(keyBytes))
		}
	} else {
		keyInput, _ := reader.ReadString('\n')
		cfg.APIKey = strings.TrimSpace(keyInput)
	}

	// Health check
	fmt.Print("Testing connection... ")
	if err := checkEndpointHealth(cfg.Endpoint, cfg.APIKey); err != nil {
		fmt.Printf("FAILED (%v)\n", err)
		fmt.Println("Config saved anyway. You can re-run /setup to fix.")
	} else {
		fmt.Println("OK")
	}

	if err := Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Config saved to %s/config.yaml\n", ShannonDir())
	fmt.Println()
	return nil
}

func isLocalEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0"
}

func checkEndpointHealth(endpoint, apiKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	base := strings.TrimSuffix(endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
