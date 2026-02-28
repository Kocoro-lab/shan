package client

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
)

type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

func StreamSSE(ctx context.Context, url string, apiKey string, handler func(SSEEvent)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE returned %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // up to 4MB per line
	var current SSEEvent

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, ":") {
			continue // comment (heartbeat)
		}

		if line == "" {
			// Empty line = event boundary
			if current.Event != "" || current.Data != "" {
				if current.Event == "done" {
					return nil // stream complete
				}
				handler(current)
				current = SSEEvent{}
			}
			continue
		}

		// Split on the first ":" to get field name and value.
		// Per SSE spec, an optional single leading space after the colon is stripped.
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "id":
			current.ID = value
		case "event":
			current.Event = value
		case "data":
			if current.Data != "" {
				current.Data += "\n" + value
			} else {
				current.Data = value
			}
		}
	}

	return scanner.Err()
}
