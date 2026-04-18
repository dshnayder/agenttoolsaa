package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/genai"
	"google.golang.org/protobuf/proto"
)

var genaiClient *genai.Client
var db *sql.DB
var ai = true

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
			responseText := "Sorry, I couldn't generate a response."

			if ai {
				// 1. Save user message to database
				_, err := db.ExecContext(ctx, "INSERT INTO chat_history (phone_number, role, message) VALUES (?, ?, ?)", userPhoneStr, "user", userMessage)
				if err != nil {
					log.Printf("Failed to insert user message into history: %v", err)
				}

				// 2. Retrieve last 50 messages for this user (history)
				rows, err := db.QueryContext(ctx, "SELECT role, message FROM chat_history WHERE phone_number = ? ORDER BY timestamp DESC LIMIT 50", userPhoneStr)
				if err != nil {
					log.Printf("Failed to retrieve chat history: %v", err)
					return
				}

				type chatMsg struct {
					role    string
					message string
				}
				var messages []chatMsg
				for rows.Next() {
					var m chatMsg
					if err := rows.Scan(&m.role, &m.message); err == nil {
						messages = append(messages, m)
					}
				}
				rows.Close()

				// Reverse order back to chronological
				var history []*genai.Content
				for i := len(messages) - 1; i >= 0; i-- {
					m := messages[i]
					if i == 0 && m.role == "user" {
						continue
					}
					
					roleStr := genai.RoleUser
					if m.role == "model" {
						roleStr = genai.RoleModel
					}
					
					history = append(history, &genai.Content{
						Role: roleStr,
						Parts: []*genai.Part{
							{Text: m.message},
						},
					})
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

				// Load USER_<phone>.md if it exists
				userFile := fmt.Sprintf("USER_%s.md", userPhoneStr)
				identityData, err := os.ReadFile(userFile)
				if err == nil {
					config.SystemInstruction = &genai.Content{
						Parts: []*genai.Part{
							{Text: "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile."},
						},
					}
				}

				// 4. Create a stateful Chat session
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

				if resultText := resp.Text(); resultText != "" {
					responseText = resultText
				}

				// 6. Save the AI's response to the database
				_, err = db.ExecContext(ctx, "INSERT INTO chat_history (phone_number, role, message) VALUES (?, ?, ?)", userPhoneStr, "model", responseText)
				if err != nil {
					log.Printf("Failed to insert model message into history: %v", err)
				}

			} else {
				responseText = "Response: " + userMessage
			}

			log.Printf("Sending reply: %s", responseText)

			msg := &waProto.Message{
				Conversation: proto.String(responseText),
			}
			_, err := client.SendMessage(ctx, v.Info.Chat, msg)
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
	db, err = sql.Open("sqlite3", "store.db")
	if err != nil {
		log.Fatalf("Failed to open main database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS chat_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		phone_number TEXT,
		role TEXT,
		message TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_phone ON chat_history(phone_number, timestamp DESC);
	`)
	if err != nil {
		log.Fatalf("Failed to create chat_history table: %v", err)
	}

	dbLog := waLog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New(ctx, "sqlite3", "file:store.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler(client))

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("Please scan the QR code above from the 'Linked Devices' screen in WhatsApp")
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		fmt.Println("Successfully connected to WhatsApp!")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
