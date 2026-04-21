package main

import (
	"os"
	"testing"
)

func TestRunErrorResolutionAgentTool(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" || apiKey == "dummy_key" {
		t.Skip("Skipping test: GEMINI_API_KEY is not set to a valid key")
	}

	// Setup mock AI
	mockAI := &MockLLMProvider{Reply: "Diagnosis: Insufficient resources. Proposed Fix: Enable autoscaling."}
	aiProvider = mockAI

	// Arguments for the tool
	args := map[string]any{
		"prompt": "Pod stuck Pending due to CPU limit",
	}

	// Execute tool
	result := ExecuteTool("runErrorResolutionAgent", args)

	// Verify results
	if errObj, ok := result["error"]; ok {
		t.Fatalf("Tool failed with error: %v", errObj)
	}

	respObj, ok := result["response"]
	if !ok {
		t.Fatalf("Result missing response field")
	}

	respStr, ok := respObj.(string)
	if !ok {
		t.Fatalf("Response is not a string")
	}

	expected := "Diagnosis: Insufficient resources. Proposed Fix: Enable autoscaling."
	if respStr != expected {
		t.Errorf("Expected response '%s', got '%s'", expected, respStr)
	}
}
