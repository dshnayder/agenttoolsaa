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
			// Ignore group messages
			if v.Info.IsGroup {
				return
			}
			// Avoid answering our own messages
			// if v.Info.IsFromMe {
			// 	return
			// }

			userMessage := v.Message.GetConversation()
			if userMessage == "" {
				userMessage = v.Message.GetExtendedTextMessage().GetText()
			}
			if userMessage == "" {
				return // Not a text message
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

				// Reverse order back to chronological (ascending) because we queried DESC
				var history []*genai.Content
				for i := len(messages) - 1; i >= 0; i-- {
					m := messages[i]
					// Skip the current user message that we JUST inserted to avoid duplication in history payload
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

				// 3. Create a stateful Chat session with retrieved history
				chatSession, err := genaiClient.Chats.Create(ctx, "gemini-3-flash-preview", nil, history)
				if err != nil {
					log.Printf("Error creating chat session via Gemini: %v", err)
					return
				}

				// 4. Send the *new* message to that chat session
				resp, err := chatSession.SendMessage(ctx, genai.Part{Text: userMessage})
				if err != nil {
					log.Printf("Error generating content via Gemini: %v", err)
					return
				}

				if resultText := resp.Text(); resultText != "" {
					responseText = resultText
				}

				// 5. Save the AI's response to the database
				_, err = db.ExecContext(ctx, "INSERT INTO chat_history (phone_number, role, message) VALUES (?, ?, ?)", userPhoneStr, "model", responseText)
				if err != nil {
					log.Printf("Failed to insert model message into history: %v", err)
				}

			} else {
				// Fallback when AI is disabled
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

	// Create table and indexes for history
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

	// Initialize the whatsmeow db store (it uses the same store.db file securely)
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
		// New device pairing
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
		// Existing session
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
