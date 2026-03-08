package daemon

import (
	"testing"
)

func TestRunAgentRequest_Validate_EmptyText(t *testing.T) {
	req := RunAgentRequest{Text: ""}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestRunAgentRequest_Validate_WhitespaceOnly(t *testing.T) {
	req := RunAgentRequest{Text: "   "}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for whitespace-only text")
	}
}

func TestRunAgentRequest_Validate_NonEmpty(t *testing.T) {
	req := RunAgentRequest{Text: "hello"}
	if err := req.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAgentRequest_Validate_WithAgent(t *testing.T) {
	req := RunAgentRequest{Text: "do something", Agent: "ops-bot"}
	if err := req.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAgentRequest_Validate_WithSessionID(t *testing.T) {
	req := RunAgentRequest{Text: "do something", SessionID: "sess-123"}
	if err := req.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
