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

func getSkillIndex() string {
	skillsDir := filepath.Join("memory", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return ""
	}

	var index strings.Builder
	index.WriteString("Available Learned Skills (Use the 'readSkill' tool to dynamically load their full logic if contextually relevant):\n")
	hasSkills := false

	for _, entry := range entries {
		if entry.IsDir() {
			skillName := entry.Name()
			skillPath := filepath.Join(skillsDir, skillName, "SKILL.md")
			data, err := os.ReadFile(skillPath)
			if err == nil {
				content := string(data)
				desc := "No description provided."
				lines := strings.Split(content, "\n")
				inFrontmatter := false
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if trimmed == "---" {
						inFrontmatter = !inFrontmatter
						continue
					}
					if inFrontmatter && strings.HasPrefix(trimmed, "description:") {
						desc = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
						break
					}
				}
				index.WriteString(fmt.Sprintf("- %s: %s\n", skillName, desc))
				hasSkills = true
			}
		}
	}

	if !hasSkills {
		return ""
	}
	return index.String() + "\n"
}

func eventHandler(client *whatsmeow.Client) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			userMessage := v.Message.GetConversation()
			if userMessage == "" {
				userMessage = v.Message.GetExtendedTextMessage().GetText()
			}
			if userMessage == "" {
				return
			}

			userPhoneStr := v.Info.Chat.ToNonAD().String()
			log.Printf("Received message from %s: %s", userPhoneStr, userMessage)

			if !v.Info.IsGroup {
				if strings.ToLower(os.Getenv("ALLOW_DM")) != "true" {
					log.Printf("Dropped DM from %s (ALLOW_DM is restricted)", userPhoneStr)
					return
				}
			}

			allowedChats := os.Getenv("ALLOWED_CHATS")
			if allowedChats != "" && !strings.Contains(allowedChats, userPhoneStr) {
				log.Printf("Dropped unauthorized message from %s (Not mapped in ALLOWED_CHATS)", userPhoneStr)
				return
			}

			if v.Info.IsGroup {
				senderJID := v.Info.Sender.ToNonAD().String()
				userMessage = fmt.Sprintf("[%s via Group]: %s", senderJID, userMessage)
			}

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
				sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile.\n\n"
			}

			summaryFile := filepath.Join("memory", fmt.Sprintf("SUMMARY_%s.md", userPhoneStr))
			summaryData, err := os.ReadFile(summaryFile)
			if err == nil && len(summaryData) > 0 {
				sysText += "Here is the summarized history of your older conversations with the user:\n" + string(summaryData) + "\n\n"
			}

			sysText += getSkillIndex()

			// Start WhatsApp "typing..." presence
			_ = client.SendChatPresence(ctx, v.Info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)

			// 4. Query Unified AI Provider (which safely handles tool loops)
			responseText, err := aiProvider.Chat(ctx, userPhoneStr, userMessage, history, sysText)

			// Stop "typing..." presence
			_ = client.SendChatPresence(ctx, v.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
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
				log.Printf("Background Timer Fired: 0 active CHECKIN files found.")
				continue
			}

			for _, file := range files {
				log.Printf("Background Timer Fired: evaluating %s CHECKIN tracking file...", file)
				content, err := os.ReadFile(file)
				if err != nil || len(strings.TrimSpace(string(content))) == 0 || content[0] != '-' {
					continue
				}

				base := filepath.Base(file)
				userPhoneStr := strings.TrimPrefix(base, "CHECKIN_")
				userPhoneStr = strings.TrimSuffix(userPhoneStr, ".md")

				ctx := context.Background()

				history, err := getChatHistory(ctx, userPhoneStr)
				if err != nil {
					log.Printf("No chat history for %s: %v", userPhoneStr, err)
					continue
				}

				sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
				userFile := filepath.Join("memory", fmt.Sprintf("USER_%s.md", userPhoneStr))
				identityData, err := os.ReadFile(userFile)
				if err == nil && len(identityData) > 0 {
					sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile.\n\n"
				}

				summaryFile := filepath.Join("memory", fmt.Sprintf("SUMMARY_%s.md", userPhoneStr))
				summaryData, err := os.ReadFile(summaryFile)
				if err == nil && len(summaryData) > 0 {
					sysText += "Here is the summarized history of your older conversations with the user:\n" + string(summaryData) + "\n\n"
				}

				sysText += getSkillIndex()

				prompt := fmt.Sprintf(`[BACKGROUND SCHEDULED WAKEUP]
Here is the content of your checkin list in YAML format.
Each task consist of:
* 'time': Next run time in RFC3339 format, for example 2026-04-19T10:02:31-04:00
* 'schedule': When to run this task based on user request, for example 'every minute' or 'at 5pm' or 'once'.
	If user asked to run task once then 'schedule' should be 'once'.
	For reccuring tasks 'schedule' should specify user request for recurrence.
* 'description': Description of what to do to perform the task.
Example:
- time: 2026-04-19T10:02:31-04:00
  schedule: once
  description: "Remind me to start the application"
- time: 2026-04-19T10:30:00-04:00
  schedule: "every 1 hour"
  description: "Check if application is healthy"

The logic for processing background tasks is as follows:
1. Iterate over list of tasks.
2. Extract task schedule time and description.
3. Compare task schedule time with current system time %s.
4. If task schedule time is in the past (i.e. less than current system time) then execute the task.
5. Otherwise skip the task.
6. After iterating all tasks no tasks is due then output EXACTLY the single word IGNORE and absolutely nothing else.
7. When you run a one-off task, remember to use updateCheckin to remove it.
8. When you run reccurring task update calculate next run time and update its 'time' field.
9. When you completed a one-off task do not mention that the task is removed, user understands that one-off tasks run once.

CHECKIN LIST:\n%s`, time.Now().Format(time.RFC3339), string(content))

				resultText, err := aiProvider.Chat(ctx, userPhoneStr, prompt, history, sysText)
				if err != nil {
					log.Printf("Background: Error communicating with AI provider: %v", err)
					continue
				}

				trimmedText := strings.TrimSpace(resultText)
				lowerText := strings.ToLower(trimmedText)
				if trimmedText != "" && trimmedText != "IGNORE" && !strings.Contains(lowerText, "no response generated") {
					jid, err := types.ParseJID(userPhoneStr)
					if err == nil {
						msg := &waProto.Message{
							Conversation: proto.String(trimmedText),
						}
						_, err = client.SendMessage(ctx, jid, msg)
						if err == nil {
							// Provide conversational anchors so the Model timeline strictly alternates and doesn't crash GenAI limits
							_ = saveChatMessage(ctx, userPhoneStr, "user", "[SYSTEM WAKEUP ACTION] A background checkin evaluating tasks was executed natively out-of-band.")
							_ = saveChatMessage(ctx, userPhoneStr, "model", trimmedText)
						} else {
							log.Printf("Background: Error sending WhatsApp message: %v", err)
						}
					} else {
						log.Printf("Background: Invalid JID %s: %v", userPhoneStr, err)
					}
				}
			} // End checkin loop

			// START COMPACTION LOOP
			ctx := context.Background()
			activePhones, err := getActivePhones(ctx)
			if err == nil {
				for _, userPhoneStr := range activePhones {
					var total int
					_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_history WHERE phone_number = ?", userPhoneStr).Scan(&total)

					if total >= 20 { // User requested threshold 20
						// Keep the most recent 10 natively
						ids, msgs, err := getMessagesToCompact(ctx, userPhoneStr, 10)
						if err != nil || len(ids) == 0 {
							continue
						}

						var textBlock strings.Builder
						for _, m := range msgs {
							textBlock.WriteString(fmt.Sprintf("%s: %s\n", strings.ToUpper(m.Role), m.Text))
						}

						summaryFile := filepath.Join("memory", fmt.Sprintf("SUMMARY_%s.md", userPhoneStr))
						existingSummary, err := os.ReadFile(summaryFile)
						var oldSummaryText string
						if err == nil && len(existingSummary) > 0 {
							oldSummaryText = fmt.Sprintf("EXISTING PREVIOUS SUMMARY:\n%s\n\n", string(existingSummary))
						}

						prompt := fmt.Sprintf(`[BACKGROUND COMPACTION ROUTINE]
You have reached your temporal memory threshold! Review the following conversation history.
1. Use the 'writeSkill' tool to permanently extract any new rules, instructions, or coding logic that the user explicitly taught you during this snippet.
2. Formulate a dense, narrative paragraph summarizing everything discussed here so we don't forget the context of the conversation. Output ONLY the raw textual summary content. Do not include introductory text like 'Here is the summary'. Merge the EXISTING PREVIOUS SUMMARY (if any) with the NEW CONVERSATION HISTORY into a single cohesive narrative document.

%sNEW CONVERSATION HISTORY:
%s`, oldSummaryText, textBlock.String())

						sysText := "You are a hyper-analytical background archivist. Your sole purpose is to document explicit skills via tools and summarize data contextually."
						blankHistory := []ChatMessage{}

						summaryRes, err := aiProvider.Chat(ctx, userPhoneStr, prompt, blankHistory, sysText)
						if err == nil && strings.TrimSpace(summaryRes) != "" {
							err = deleteCompactedMessages(ctx, ids)
							if err != nil {
								log.Printf("Background: Failed to delete compacted messages: %v", err)
							} else {
								_ = os.WriteFile(summaryFile, []byte(strings.TrimSpace(summaryRes)), 0644)
								log.Printf("Background: Successfully compacted %d messages for %s into markdown summary", len(ids), userPhoneStr)
							}
						}
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
