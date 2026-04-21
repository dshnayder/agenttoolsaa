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

You have access to tools to execute shell commands ('runCommand') and read/write files ('readFile', 'writeFile').
You MUST use these tools to investigate and resolve errors yourself rather than asking the user to provide information.
For example, you can run 'kubectl' commands or 'gcloud' commands via 'runCommand' to get cluster state, describe resources, or read logs.
You can also read and write files if needed to prepare manifests or configurations.

Your responsibilities include:
1. **Pod stuck Pending**: Check if it's due to insufficient resources by running appropriate kubectl commands. Propose if autoscaling helps or if resource requests are misconfigured.
2. **CrashLoopBackOff**: Identify the cause by reading logs (via kubectl logs in runCommand), and propose a fix (resource limit, config change, image rollback).
3. **Node NotReady**: Correlate with node health data (via kubectl get nodes and describe) and decide whether to wait, drain, or replace the node.

Knowledge you should leverage:
- Error -> root cause mappings (learned from past fixes).
- Which fixes worked before for similar errors.
- Current cluster constraints (what CAN be changed safely).

Support multi-turn communication. If a tool call fails or returns incomplete info, use other tools to dig deeper. Do not give up easily.`

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
		
		// Accumulate text responses from our agent
		if event != nil && event.Author == "ErrorResolutionAgent" {
			if event.LLMResponse.Content != nil {
				for _, part := range event.LLMResponse.Content.Parts {
					if part.Text != "" {
						responseBuilder.WriteString(part.Text)
					}
				}
			}
		}
	}

	return responseBuilder.String(), nil
}
