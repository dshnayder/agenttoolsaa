package main

import (
	"context"
	"log"

	"google.golang.org/genai"
)

type GeminiProvider struct {
	client *genai.Client
}

func NewGeminiProvider() (*GeminiProvider, error) {
	client, err := genai.NewClient(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	return &GeminiProvider{client: client}, nil
}

func (g *GeminiProvider) generateToolConfig() *genai.GenerateContentConfig {
	var decls []*genai.FunctionDeclaration
	for _, td := range GetAvailableTools() {
		props := make(map[string]*genai.Schema)
		for k, v := range td.Properties {
			props[k] = &genai.Schema{Type: genai.TypeString, Description: v.Description}
		}

		decl := &genai.FunctionDeclaration{
			Name:        td.Name,
			Description: td.Description,
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: props,
				Required:   td.Required,
			},
		}
		decls = append(decls, decl)
	}

	return &genai.GenerateContentConfig{
		Tools: []*genai.Tool{
			{FunctionDeclarations: decls},
		},
	}
}

func (g *GeminiProvider) Chat(ctx context.Context, userMessage string, history []ChatMessage, systemPrompt string) (string, error) {
	config := g.generateToolConfig()
	config.SystemInstruction = &genai.Content{
		Parts: []*genai.Part{{Text: systemPrompt}},
	}

	var genaiHistory []*genai.Content
	for _, m := range history {
		roleStr := genai.RoleUser
		if m.Role == "model" {
			roleStr = genai.RoleModel
		}
		genaiHistory = append(genaiHistory, &genai.Content{
			Role:  roleStr,
			Parts: []*genai.Part{{Text: m.Text}},
		})
	}

	chatSession, err := g.client.Chats.Create(ctx, "gemini-3.1-flash-lite-preview", config, genaiHistory)
	if err != nil {
		return "", err
	}

	resp, err := chatSession.SendMessage(ctx, genai.Part{Text: userMessage})
	if err != nil {
		return "", err
	}

	for {
		if len(resp.Candidates) > 0 {
			hasToolCall := false
			var funcResponses []genai.Part

			for _, part := range resp.Candidates[0].Content.Parts {
				if part.FunctionCall != nil {
					hasToolCall = true
					result := ExecuteTool(part.FunctionCall.Name, part.FunctionCall.Args)

					fr := genai.Part{
						FunctionResponse: &genai.FunctionResponse{
							Name:     part.FunctionCall.Name,
							Response: result,
						},
					}
					funcResponses = append(funcResponses, fr)
				}
			}

			if hasToolCall {
				resp2, err := chatSession.SendMessage(ctx, funcResponses...)
				if err != nil {
					log.Printf("Gemini Error sending function responses: %v", err)
					break
				}
				resp = resp2
				continue
			}
		}
		break
	}

	return resp.Text(), nil
}
