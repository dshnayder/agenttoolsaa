package main

import (
	"context"
)

type ChatMessage struct {
	Role string
	Text string
}

type LLMProvider interface {
	Chat(ctx context.Context, userPhone string, userMessage string, history []ChatMessage, systemPrompt string) (string, error)
}
