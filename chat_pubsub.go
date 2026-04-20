package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

// GoogleChatEvent represents the structure of a Google Chat event received via Pub/Sub.
type GoogleChatEvent struct {
	Type      string `json:"type"`
	EventTime string `json:"eventTime"`
	Space     struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"space"`
	Message struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Sender struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"sender"`
		Thread struct {
			Name string `json:"name"`
		} `json:"thread"`
	} `json:"message"`
}

// startPubSubMonitor starts listening to the Pub/Sub subscription.
func startPubSubMonitor(ctx context.Context, subscriptionName string, handler func(event GoogleChatEvent)) error {
	client, err := pubsub.NewClient(ctx, pubsub.DetectProjectID)
	if err != nil {
		return fmt.Errorf("failed to create pubsub client: %w", err)
	}
	defer client.Close()

	sub := client.Subscription(subscriptionName)

	log.Printf("Listening on Pub/Sub subscription: %s", subscriptionName)

	// Receive blocks until the context is cancelled or an error occurs.
	err = sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		defer msg.Ack()

		var event GoogleChatEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("Error unmarshaling Pub/Sub message: %v", err)
			return
		}

		log.Printf("Received Google Chat event: %s from space %s", event.Type, event.Space.Name)

		if event.Type == "MESSAGE" {
			handler(event)
		}
	})

	return err
}

// sendGoogleChatMessage sends a message back to Google Chat using the official API.
var sendGoogleChatMessage = func(ctx context.Context, space string, text string, thread string) error {
	chatService, err := chat.NewService(ctx, option.WithScopes("https://www.googleapis.com/auth/chat.messages.create"))
	if err != nil {
		return fmt.Errorf("failed to create chat service: %w", err)
	}

	message := &chat.Message{
		Text: text,
	}
	if thread != "" {
		message.Thread = &chat.Thread{Name: thread}
	}

	call := chatService.Spaces.Messages.Create(space, message)
	if thread != "" {
		call.MessageReplyOption("REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
	}

	_, err = call.Do()
	if err != nil {
		return fmt.Errorf("failed to send chat message: %w", err)
	}

	return nil
}
