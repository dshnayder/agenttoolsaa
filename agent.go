package main

import (
	"context"
	"fmt"
	"os"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

func createAgent(ctx context.Context) (agent.Agent, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	model, err := gemini.NewModel(ctx, "gemini-2.5-flash", &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	tools, err := GetAllTools()
	if err != nil {
		return nil, fmt.Errorf("failed to get tools: %w", err)
	}

	myAgent, err := llmagent.New(llmagent.Config{
		Name:        "system_admin_agent",
		Model:       model,
		Instruction: "You are a fully autonomous, self-scheduling conversational AI agent capable of monitoring, managing, and fixing systems directly from Google Chat. You have raw unconstrained shell access and local filesystem tools.",
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return myAgent, nil
}
