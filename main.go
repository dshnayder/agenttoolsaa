package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
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

			userPhoneStr := v.Info.Chat.ToNonAD().String()
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

			// 3. Prepare config with Modular Tools
			config := &genai.GenerateContentConfig{
				Tools: getToolDeclarations(),
			}

			sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
			userFile := filepath.Join("memory", fmt.Sprintf("USER_%s.md", userPhoneStr))
			identityData, err := os.ReadFile(userFile)
			if err == nil && len(identityData) > 0 {
				sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile."
			}

			config.SystemInstruction = &genai.Content{
				Parts: []*genai.Part{
					{Text: sysText},
				},
			}

			// 4. Create stateful Chat session
			chatSession, err := genaiClient.Chats.Create(ctx, "gemini-3.1-flash-lite-preview", config, history)
			if err != nil {
				log.Printf("Error creating chat session via Gemini: %v", err)
				return
			}

			// 5. Query Gemini
			resp, err := chatSession.SendMessage(ctx, genai.Part{Text: userMessage})
			if err != nil {
				log.Printf("Error generating content via Gemini: %v", err)
				exhaustedMsg := "AI brain is exhausted, please try a bit later"
				msg := &waProto.Message{
					Conversation: proto.String(exhaustedMsg),
				}
				_, sendErr := client.SendMessage(ctx, v.Info.Chat, msg)
				if sendErr != nil {
					log.Printf("Error sending exhaustion message to WhatsApp: %v", sendErr)
				}
				return
			}

			// 6. Check for Function Call interception (allows multiple passes natively)
			for {
				if len(resp.Candidates) > 0 {
					hasToolCall := false
					var funcResponses []genai.Part

					for _, part := range resp.Candidates[0].Content.Parts {
						if part.FunctionCall != nil {
							hasToolCall = true
							fr := executeFunctionCall(part.FunctionCall, userPhoneStr)
							funcResponses = append(funcResponses, fr)
						}
					}

					if hasToolCall {
						resp, err = chatSession.SendMessage(ctx, funcResponses...)
						if err != nil {
							log.Printf("Error sending function responses chaining: %v", err)
							break
						}
						continue
					}
				}
				break
			}

			responseText := "Sorry, I couldn't generate a response."
			if resultText := resp.Text(); resultText != "" {
				responseText = resultText
			}

			// 7. Save bot's response to the database
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

func startBackgroundTimer(client *whatsmeow.Client) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			files, err := filepath.Glob(filepath.Join("memory", "CHECKIN_*.md"))
			if err != nil {
				continue
			}

			for _, file := range files {
				content, err := os.ReadFile(file)
				if err != nil || len(strings.TrimSpace(string(content))) == 0 {
					continue
				}

				base := filepath.Base(file)
				userPhoneStr := strings.TrimPrefix(base, "CHECKIN_")
				userPhoneStr = strings.TrimSuffix(userPhoneStr, ".md")

				ctx := context.Background()

				history, err := getChatHistory(ctx, userPhoneStr)
				if err != nil {
					continue
				}

				config := &genai.GenerateContentConfig{
					Tools: getToolDeclarations(),
				}

				sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
				userFile := filepath.Join("memory", fmt.Sprintf("USER_%s.md", userPhoneStr))
				identityData, err := os.ReadFile(userFile)
				if err == nil && len(identityData) > 0 {
					sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile.\n\n"
				}

				config.SystemInstruction = &genai.Content{
					Parts: []*genai.Part{{Text: sysText}},
				}

				chatSession, err := genaiClient.Chats.Create(ctx, "gemini-3.1-flash-lite-preview", config, history)
				if err != nil {
					log.Printf("Background: Error creating chat session: %v", err)
					continue
				}

				prompt := fmt.Sprintf("[BACKGROUND SCHEDULED WAKEUP] Here is the content of your checkin list. Execute whatever is due for the current time. If nothing is due, DO NOT send a message to the user. If you run a task, remember to use updateCheckin to remove it or update it. If you need to notify the user, include it in your final text response.\n\nCHECKIN LIST:\n%s", string(content))

				resp, err := chatSession.SendMessage(ctx, genai.Part{Text: prompt})
				if err != nil {
					log.Printf("Background: Error communicating with Gemini: %v", err)
					continue
				}

				for {
					if len(resp.Candidates) > 0 {
						hasToolCall := false
						var funcResponses []genai.Part

						for _, part := range resp.Candidates[0].Content.Parts {
							if part.FunctionCall != nil {
								hasToolCall = true
								fr := executeFunctionCall(part.FunctionCall, userPhoneStr)
								funcResponses = append(funcResponses, fr)
							}
						}

						if hasToolCall {
							resp2, err := chatSession.SendMessage(ctx, funcResponses...)
							if err != nil {
								log.Printf("Background: Error sending function responses: %v", err)
								break
							}
							resp = resp2
							continue
						}
					}
					break
				}

				resultText := resp.Text()
				if strings.TrimSpace(resultText) != "" {
					jid, err := types.ParseJID(userPhoneStr)
					if err == nil {
						msg := &waProto.Message{
							Conversation: proto.String(resultText),
						}
						_, err = client.SendMessage(ctx, jid, msg)
						if err == nil {
							// Only insert the model's response into the history 
							_ = saveChatMessage(ctx, userPhoneStr, "model", resultText)
						} else {
							log.Printf("Background: Error sending WhatsApp message: %v", err)
						}
					} else {
						log.Printf("Background: Invalid JID %s: %v", userPhoneStr, err)
					}
				}
			}
		}
	}()
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

	// Establish necessary system directories
	setupDirectories()

	// Internal local sqlite DB
	if err := initDB(filepath.Join("memory", "store.db")); err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	// Connect to WhatsApp
	client, err := setupWhatsApp(ctx, filepath.Join("memory", "store.db"), eventHandler)
	if err != nil {
		log.Fatalf("WhatsApp setup failed: %v", err)
	}

	// Start Background Monitoring
	startBackgroundTimer(client)
	log.Println("Background timer successfully armed.")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
