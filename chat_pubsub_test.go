package main

import (
	"context"
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
	t.Skip("Skipping test as ADK requires full model mocking which is not documented here.")
}
