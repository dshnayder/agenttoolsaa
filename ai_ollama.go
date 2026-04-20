package main

import (
	"context"
	"encoding/json"

	"github.com/ollama/ollama/api"
)

type OllamaProvider struct {
	client *api.Client
	model  string
}

func NewOllamaProvider(model string) (*OllamaProvider, error) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, err
	}
	return &OllamaProvider{client: client, model: model}, nil
}

func (o *OllamaProvider) getOllamaTools() []api.Tool {
	var tools []api.Tool
	for _, td := range GetAvailableTools() {
		props := api.NewToolPropertiesMap()

		for k, v := range td.Properties {
			props.Set(k, api.ToolProperty{
				Type:        api.PropertyType{v.Type},
				Description: v.Description,
			})
		}

		tool := api.Tool{
			Type: "function",
			Function: api.ToolFunction{
				Name:        td.Name,
				Description: td.Description,
				Parameters: api.ToolFunctionParameters{
					Type:       "object",
					Required:   td.Required,
					Properties: props,
				},
			},
		}
		tools = append(tools, tool)
	}
	return tools
}

func (o *OllamaProvider) Chat(ctx context.Context, userMessage string, history []ChatMessage, systemPrompt string) (string, error) {
	var messages []api.Message

	messages = append(messages, api.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	for _, h := range history {
		role := h.Role
		if role == "model" {
			role = "assistant"
		}
		messages = append(messages, api.Message{
			Role:    role,
			Content: h.Text,
		})
	}

	messages = append(messages, api.Message{
		Role:    "user",
		Content: userMessage,
	})

	tools := o.getOllamaTools()
	stream := false

	for {
		req := &api.ChatRequest{
			Model:    o.model,
			Messages: messages,
			Tools:    tools,
			Stream:   &stream,
		}

		var respMessage *api.Message

		err := o.client.Chat(ctx, req, func(resp api.ChatResponse) error {
			respMessage = &resp.Message
			return nil
		})

		if err != nil {
			return "", err
		}

		messages = append(messages, *respMessage)

		if len(respMessage.ToolCalls) == 0 {
			return respMessage.Content, nil
		}

		for _, tc := range respMessage.ToolCalls {
			name := tc.Function.Name
			argsMap := tc.Function.Arguments.ToMap()

			resultMap := ExecuteTool(name, argsMap)

			resultBytes, _ := json.Marshal(resultMap)

			messages = append(messages, api.Message{
				Role:    "tool",
				Content: string(resultBytes),
			})
		}
	}
}
