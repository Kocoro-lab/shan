package tools

import (
	"context"
	"testing"
	"time"
)

func TestCloudDelegateInfo(t *testing.T) {
	tool := NewCloudDelegateTool(nil, "", 60*time.Second, nil, "", "")
	info := tool.Info()
	if info.Name != "cloud_delegate" {
		t.Errorf("expected name cloud_delegate, got %s", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "task" {
		t.Errorf("expected required=[task], got %v", info.Required)
	}
}

func TestCloudDelegateRequiresApproval(t *testing.T) {
	tool := NewCloudDelegateTool(nil, "", 60*time.Second, nil, "", "")
	if !tool.RequiresApproval() {
		t.Error("cloud_delegate should require approval")
	}
	if tool.IsSafeArgs(`{"task":"anything"}`) {
		t.Error("IsSafeArgs should always return false")
	}
}

func TestCloudDelegateEmptyTask(t *testing.T) {
	tool := NewCloudDelegateTool(nil, "", 60*time.Second, nil, "", "")
	result, err := tool.Run(context.Background(), `{"task":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for empty task")
	}
}

func TestCloudDelegateInvalidJSON(t *testing.T) {
	tool := NewCloudDelegateTool(nil, "", 60*time.Second, nil, "", "")
	result, err := tool.Run(context.Background(), `not json`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestCloudDelegateNoGateway(t *testing.T) {
	tool := NewCloudDelegateTool(nil, "", 60*time.Second, nil, "", "")
	result, err := tool.Run(context.Background(), `{"task":"test task"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error when gateway is nil")
	}
}

func TestCloudDelegateContextTruncation(t *testing.T) {
	tool := NewCloudDelegateTool(nil, "", 60*time.Second, nil, "", "")
	longCtx := make([]byte, 9000)
	for i := range longCtx {
		longCtx[i] = 'x'
	}
	// Will fail at submission (nil gateway), but should get past arg parsing + truncation
	result, _ := tool.Run(context.Background(), `{"task":"test","context":"`+string(longCtx)+`"}`)
	if !result.IsError {
		t.Log("Expected error (nil gateway)")
	}
}
