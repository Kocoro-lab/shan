package config

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

const DefaultEndpoint = "https://api-dev.shannon.run"

const (
	hintCloud = "Get your API key at https://shannon.run"
	hintLocal = "Running locally? See https://github.com/Kocoro-lab/Shannon for self-hosting docs."
)

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
func RunSetup(cfg *Config, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)

	fmt.Fprintln(out, "Shannon CLI Setup")
	fmt.Fprintln(out)

	// Endpoint
	defaultEP := cfg.Endpoint
	if defaultEP == "" {
		defaultEP = DefaultEndpoint
	}
	fmt.Fprintf(out, "API endpoint [%s]: ", defaultEP)
	epInput, _ := reader.ReadString('\n')
	epInput = strings.TrimSpace(epInput)
	if epInput != "" {
		cfg.Endpoint = epInput
	} else {
		cfg.Endpoint = defaultEP
	}

	// Contextual hint
	if isLocalEndpoint(cfg.Endpoint) {
		fmt.Fprintln(out, hintLocal)
	} else {
		fmt.Fprintln(out, hintCloud)
	}
	fmt.Fprintln(out)

	// API key + health check with retry (max 3 attempts)
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Prompt for key
		if isLocalEndpoint(cfg.Endpoint) {
			fmt.Fprint(out, "API key (optional for local, Enter to skip): ")
		} else {
			fmt.Fprint(out, "API key: ")
		}

		if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			keyBytes, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(out) // newline after masked input
			if err != nil {
				fmt.Fprintf(out, "Error reading key: %v\n", err)
				continue
			}
			cfg.APIKey = strings.TrimSpace(string(keyBytes))
		} else {
			keyInput, _ := reader.ReadString('\n')
			cfg.APIKey = strings.TrimSpace(keyInput)
		}

		// Health check
		fmt.Fprint(out, "Testing connection... ")
		if err := checkEndpointHealth(cfg.Endpoint, cfg.APIKey); err != nil {
			fmt.Fprintf(out, "FAILED (%v)\n", err)

			if attempt == maxAttempts-1 {
				// Exhausted all attempts
				fmt.Fprintln(out, "Config saved anyway. Re-run 'shan --setup' to fix.")
				break
			}
			fmt.Fprint(out, "Re-enter credentials? [Y/n]: ")
			ans, _ := reader.ReadString('\n')
			ans = strings.TrimSpace(strings.ToLower(ans))
			if ans == "n" || ans == "no" {
				fmt.Fprintln(out, "Config saved anyway. Re-run 'shan --setup' to fix.")
				break
			}
			continue
		}

		fmt.Fprintln(out, "OK")
		break
	}

	if err := Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Fprintf(out, "Config saved to %s/config.yaml\n", ShannonDir())
	fmt.Fprintln(out)
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
