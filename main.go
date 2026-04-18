package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/genai"
	"google.golang.org/protobuf/proto"
)

var genaiClient *genai.Client

func eventHandler(client *whatsmeow.Client) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			if v.Info.IsGroup {
				return
			}

			userMessage := v.Message.GetConversation()
			if userMessage == "" {
				userMessage = v.Message.GetExtendedTextMessage().GetText()
			}
			if userMessage == "" {
				return
			}

			userPhoneStr := v.Info.Chat.User
			log.Printf("Received message from %s: %s", userPhoneStr, userMessage)

			ctx := context.Background()

			// 1. Save user message to database
			if err := saveChatMessage(ctx, userPhoneStr, "user", userMessage); err != nil {
				log.Printf("Failed to insert user message into history: %v", err)
			}

			// 2. Retrieve history for Gemini
			history, err := getChatHistory(ctx, userPhoneStr)
			if err != nil {
				log.Printf("Failed to retrieve chat history: %v", err)
				return
			}

			// 3. Prepare config with Tools and System Instruction
			saveUserIdentityFunc := &genai.FunctionDeclaration{
				Name:        "saveUserIdentity",
				Description: "Call this function to save or update the User's identity in the local system when they introduce themselves, state their name, occupation, or interests. Provide the identity data fully formatted as a Markdown document. Ensure you update and include all previously known data.",
				Parameters: &genai.Schema{
					Type: "object",
					Properties: map[string]*genai.Schema{
						"markdown_content": {
							Type:        "string",
							Description: "A complete Markdown formatted string containing the user's name, occupation, interests, etc.",
						},
					},
					Required: []string{"markdown_content"},
				},
			}

			config := &genai.GenerateContentConfig{
				Tools: []*genai.Tool{
					{
						FunctionDeclarations: []*genai.FunctionDeclaration{saveUserIdentityFunc},
					},
				},
			}

			userFile := fmt.Sprintf("USER_%s.md", userPhoneStr)
			identityData, err := os.ReadFile(userFile)
			if err == nil {
				config.SystemInstruction = &genai.Content{
					Parts: []*genai.Part{
						{Text: "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile."},
					},
				}
			}

			// 4. Create stateful Chat session
			chatSession, err := genaiClient.Chats.Create(ctx, "gemini-3-flash-preview", config, history)
			if err != nil {
				log.Printf("Error creating chat session via Gemini: %v", err)
				return
			}

			// 5. Send the *new* message to that chat session
			resp, err := chatSession.SendMessage(ctx, genai.Part{Text: userMessage})
			if err != nil {
				log.Printf("Error generating content via Gemini: %v", err)
				return
			}

			// Check for Function Calling
			if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
				for _, part := range resp.Candidates[0].Content.Parts {
					if part.FunctionCall != nil && part.FunctionCall.Name == "saveUserIdentity" {
						if contentObj, ok := part.FunctionCall.Args["markdown_content"]; ok {
							if mdStr, isStr := contentObj.(string); isStr {
								// Silently save identity data
								_ = os.WriteFile(userFile, []byte(mdStr), 0644)
								log.Printf("Silently saved identity data to %s", userFile)
							}
						}

						// Send the function response back to Gemini so it resumes
						fr := genai.Part{
							FunctionResponse: &genai.FunctionResponse{
								Name: "saveUserIdentity",
								Response: map[string]any{"status": "success"},
							},
						}
						resp2, err := chatSession.SendMessage(ctx, fr)
						if err != nil {
							log.Printf("Error sending function response: %v", err)
						} else {
							resp = resp2 // Replace resp with the final text output
						}
					}
				}
			}

			responseText := "Sorry, I couldn't generate a response."
			if resultText := resp.Text(); resultText != "" {
				responseText = resultText
			}

			// 6. Save the AI's response to the database
			if err := saveChatMessage(ctx, userPhoneStr, "model", responseText); err != nil {
				log.Printf("Failed to insert model message into history: %v", err)
			}

			log.Printf("Sending reply: %s", responseText)

			msg := &waProto.Message{
				Conversation: proto.String(responseText),
			}
			_, err = client.SendMessage(ctx, v.Info.Chat, msg)
			if err != nil {
				log.Printf("Error sending message to WhatsApp: %v", err)
			}
		}
	}
}

func main() {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is not set")
	}

	ctx := context.Background()
	var err error
	genaiClient, err = genai.NewClient(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to initialize GenAI client: %v", err)
	}

	// Initialize our local sqlite DB for chat history
	if err := initDB("store.db"); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close() // db is gracefully closed when main exits

	// Connect to WhatsApp
	client, err := setupWhatsApp(ctx, "store.db", eventHandler)
	if err != nil {
		log.Fatalf("WhatsApp setup failed: %v", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
