package main

import (
	"context"
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

			log.Printf("Received message from %s: %s", v.Info.Sender.User, userMessage)

			ctx := context.Background()
			responseText := "Sorry, I couldn't generate a response."
			if ai {
				// Call Gemini
				resp, err := genaiClient.Models.GenerateContent(ctx, "gemini-3-flash-preview", genai.Text(userMessage), nil)
				if err != nil {
					log.Printf("Error generating content via Gemini: %v", err)
					return
				}

				if resultText := resp.Text(); resultText != "" {
					responseText = resultText
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

	// waLog controls logging for the whatsmeow module
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	// SQLite store initialization
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
		// No session ID stored, need to link device
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				// Display QR code in terminal for scanning
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("Please scan the QR code above from the 'Linked Devices' screen in WhatsApp")
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		// Session exists, restore connection
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		fmt.Println("Successfully connected to WhatsApp!")
	}

	// Keep running until Ctrl-C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
