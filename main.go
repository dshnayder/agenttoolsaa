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
	"google.golang.org/protobuf/proto"
)

var aiProvider LLMProvider

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

			// 2. Retrieve history for LLM
			history, err := getChatHistory(ctx, userPhoneStr)
			if err != nil {
				log.Printf("Failed to retrieve chat history: %v", err)
				return
			}

			// 3. Dynamically set up unified contextual identity
			sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
			userFile := filepath.Join("memory", fmt.Sprintf("USER_%s.md", userPhoneStr))
			identityData, err := os.ReadFile(userFile)
			if err == nil && len(identityData) > 0 {
				sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile."
			}

			// 4. Query Unified AI Provider (which safely handles tool loops)
			responseText, err := aiProvider.Chat(ctx, userPhoneStr, userMessage, history, sysText)
			if err != nil {
				log.Printf("Error generating content via AI Provider: %v", err)
				exhaustedMsg := "AI brain is experiencing difficulties or exhausted, please try a bit later"
				msg := &waProto.Message{
					Conversation: proto.String(exhaustedMsg),
				}
				_, sendErr := client.SendMessage(ctx, v.Info.Chat, msg)
				if sendErr != nil {
					log.Printf("Error sending exhaustion message to WhatsApp: %v", sendErr)
				}
				return
			}

			if responseText == "" {
				responseText = "Sorry, I couldn't generate a response."
			}

			// 5. Save bot's response to the database
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

				sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
				userFile := filepath.Join("memory", fmt.Sprintf("USER_%s.md", userPhoneStr))
				identityData, err := os.ReadFile(userFile)
				if err == nil && len(identityData) > 0 {
					sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile.\n\n"
				}

				prompt := fmt.Sprintf("[BACKGROUND SCHEDULED WAKEUP] Here is the content of your checkin list. Execute whatever is due for the current time. If nothing is due, DO NOT send a message to the user. If you run a task, remember to use updateCheckin to remove it or update it. If you need to notify the user, include it in your final text response.\n\nCHECKIN LIST:\n%s", string(content))

				resultText, err := aiProvider.Chat(ctx, userPhoneStr, prompt, history, sysText)
				if err != nil {
					log.Printf("Background: Error communicating with AI provider: %v", err)
					continue
				}

				if strings.TrimSpace(resultText) != "" {
					jid, err := types.ParseJID(userPhoneStr)
					if err == nil {
						msg := &waProto.Message{
							Conversation: proto.String(resultText),
						}
						_, err = client.SendMessage(ctx, jid, msg)
						if err == nil {
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
	var err error

	// Determine LLM provider 
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey != "" {
		log.Println("Initializing Google Gemini API connection...")
		aiProvider, err = NewGeminiProvider()
		if err != nil {
			log.Fatalf("Failed to initialize Gemini provider: %v", err)
		}
	} else {
		targetModel := os.Getenv("OLLAMA_MODEL")
		if targetModel == "" {
			targetModel = "gemma:2b" // Safe lightweight default
			log.Println("OLLAMA_MODEL environment variable missing, defaulting to " + targetModel)
		}
		
		log.Printf("Initializing local Ollama connection targeting %s...", targetModel)
		aiProvider, err = NewOllamaProvider(targetModel)
		if err != nil {
			log.Fatalf("Failed to initialize Ollama API connection: %v", err)
		}
	}

	// Establish necessary system directories
	setupDirectories()

	// Internal local sqlite DB
	if err := initDB(filepath.Join("memory", "store.db")); err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	// Connect to WhatsApp
	ctx := context.Background()
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
