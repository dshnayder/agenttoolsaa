package main

import (
	"context"
	"os"
	"testing"
)

type MockLLMProvider struct {
	Reply string
	Err   error
}

func (m *MockLLMProvider) Chat(ctx context.Context, userMessage string, history []ChatMessage, systemPrompt string) (string, error) {
	return m.Reply, m.Err
}

func TestHandleGoogleChatEvent(t *testing.T) {
	// Setup temp file for history
	tmpFile, err := os.CreateTemp("", "history_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	
	if _, err := tmpFile.Write([]byte("[]")); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	err = initDB(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to init history file: %v", err)
	}

	// Setup mock AI
	mockAI := &MockLLMProvider{Reply: "Hello from AI"}
	aiProvider = mockAI

	// Mock sendGoogleChatMessage
	var sentSpace, sentText, sentThread string
	originalSend := sendGoogleChatMessage
	defer func() { sendGoogleChatMessage = originalSend }() // Restore after test
	
	sendGoogleChatMessage = func(ctx context.Context, space string, text string, thread string) error {
		sentSpace = space
		sentText = text
		sentThread = thread
		return nil
	}

	// Create mock event
	var event GoogleChatEvent
	event.Type = "MESSAGE"
	event.Space.Name = "spaces/test_space"
	event.Message.Text = "Hello Bot"
	event.Message.Thread.Name = "spaces/test_space/threads/test_thread"

	// Call handler
	handleGoogleChatEvent(event)

	// Verify results
	if sentSpace != "spaces/test_space" {
		t.Errorf("Expected space spaces/test_space, got %s", sentSpace)
	}
	if sentText != "Hello from AI" {
		t.Errorf("Expected text 'Hello from AI', got '%s'", sentText)
	}
	if sentThread != "spaces/test_space/threads/test_thread" {
		t.Errorf("Expected thread spaces/test_space/threads/test_thread, got %s", sentThread)
	}
}
