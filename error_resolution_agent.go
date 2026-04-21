package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ErrorResolutionAgent wraps the ADK agent for error resolution.
type ErrorResolutionAgent struct {
	adkAgent agent.Agent
	runner   *runner.Runner
}

// NewErrorResolutionAgent creates a new instance of the Error Resolution Agent.
func NewErrorResolutionAgent(ctx context.Context) (*ErrorResolutionAgent, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	// Initialize the Gemini model via ADK
	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite-preview", &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini model: %w", err)
	}

	instruction := `You are an expert Kubernetes and GKE Error Resolution Agent.
Your role is to Detect, Diagnose, and Fix cluster-level errors.
You are reactive and event-driven. You will be invoked with reports of cluster events or specific errors.

Your responsibilities include:
1. **Pod stuck Pending**: Check if it's due to insufficient resources. Propose if autoscaling helps or if resource requests are misconfigured.
2. **CrashLoopBackOff**: Identify the cause (e.g., by reading logs if provided or suggesting to read them), and propose a fix (resource limit, config change, image rollback).
3. **Node NotReady**: Correlate with node health data and decide whether to wait, drain, or replace the node.

Knowledge you should leverage:
- Error -> root cause mappings (learned from past fixes).
- Which fixes worked before for similar errors.
- Current cluster constraints (what CAN be changed safely).

You have access to global tools like 'runCommand', 'readFile', and 'writeFile'. You can use them to gather more information (e.g., running kubectl commands or reading logs) if needed to diagnose the issue. Support multi-turn communication if the user provides follow-up information or logs.`

	tools, err := GetADKTools()
	if err != nil {
		return nil, fmt.Errorf("failed to get ADK tools: %w", err)
	}

	config := llmagent.Config{
		Name:        "ErrorResolutionAgent",
		Description: "Expert agent for detecting, diagnosing, and fixing cluster-level errors.",
		Instruction: instruction,
		Model:       model,
		Tools:       tools,
	}

	adkAgent, err := llmagent.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ADK agent: %w", err)
	}

	sessionService := session.InMemoryService()

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:           "ErrorResolutionApp",
		Agent:             adkAgent,
		SessionService:    sessionService,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	return &ErrorResolutionAgent{
		adkAgent: adkAgent,
		runner:   r,
	}, nil
}

// Run invokes the agent with a user message and returns the response.
func (a *ErrorResolutionAgent) Run(ctx context.Context, message string) (string, error) {
	msg := &genai.Content{
		Parts: []*genai.Part{{Text: message}},
	}

	var responseBuilder strings.Builder
	
	// Run the agent via runner
	for event, err := range a.runner.Run(ctx, "user-test", "session-test", msg, agent.RunConfig{}) {
		if err != nil {
			return "", fmt.Errorf("error during agent run: %w", err)
		}
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				responseBuilder.WriteString(part.Text)
			}
		}
	}

	return responseBuilder.String(), nil
}
