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

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

var adkRunner *runner.Runner

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

func handleGoogleChatEvent(event GoogleChatEvent) {
	userMessage := event.Message.Text
	if userMessage == "" {
		return
	}

	identifier := event.Space.Name
	log.Printf("Received message from %s: %s", identifier, userMessage)

	// Save target space for background notifications
	targetSpaceFile := filepath.Join("memory", "TARGET_SPACE")
	_ = os.WriteFile(targetSpaceFile, []byte(identifier), 0644)

	ctx := context.Background()

	if err := saveChatMessage(ctx, "user", userMessage); err != nil {
		log.Printf("Failed to insert user message into history: %v", err)
	}

	sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
	
	userFile := filepath.Join("memory", "USER.md")
	identityData, err := os.ReadFile(userFile)
	if err == nil && len(identityData) > 0 {
		sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile.\n\n"
	}

	summaryFile := filepath.Join("memory", "SUMMARY.md")
	summaryData, err := os.ReadFile(summaryFile)
	if err == nil && len(summaryData) > 0 {
		sysText += "Here is the summarized history of your older conversations with the user:\n" + string(summaryData) + "\n\n"
	}

	sysText += getSkillIndex()

	prompt := fmt.Sprintf("%s\n\nUser Message: %s", sysText, userMessage)
	var responseText string
	content := &genai.Content{
		Parts: []*genai.Part{{Text: prompt}},
	}
	for ev, err := range adkRunner.Run(ctx, "single_user", "single_session", content, agent.RunConfig{}) {
		if err != nil {
			log.Printf("Error from ADK runner: %v", err)
			continue
		}
		if ev.LLMResponse.Content != nil {
			for _, part := range ev.LLMResponse.Content.Parts {
				responseText += part.Text
			}
		}
	}


	if responseText == "" {
		responseText = "Sorry, I couldn't generate a response."
	}

	if err := saveChatMessage(ctx, "model", responseText); err != nil {
		log.Printf("Failed to insert model message into history: %v", err)
	}

	log.Printf("Sending reply: %s", responseText)

	err = sendGoogleChatMessage(ctx, event.Space.Name, responseText, event.Message.Thread.Name)
	if err != nil {
		log.Printf("Error sending message to Google Chat: %v", err)
	}
}

func startBackgroundTimer() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			file := filepath.Join("memory", "CHECKIN.md")
			content, err := os.ReadFile(file)
			if err == nil && len(strings.TrimSpace(string(content))) > 0 && content[0] == '-' {
				log.Printf("Background Timer Fired: evaluating %s CHECKIN tracking file...", file)

				ctx := context.Background()



				sysText := fmt.Sprintf("Current System Time: %s\n\n", time.Now().Format(time.RFC3339))
				
				userFile := filepath.Join("memory", "USER.md")
				identityData, err := os.ReadFile(userFile)
				if err == nil && len(identityData) > 0 {
					sysText += "Here is what you currently know about the user's identity:\n" + string(identityData) + "\n\nUse this context when replying, and if they provide new info, use saveUserIdentity to update this profile.\n\n"
				}

				summaryFile := filepath.Join("memory", "SUMMARY.md")
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

				targetSpace := ""
				targetSpaceFile := filepath.Join("memory", "TARGET_SPACE")
				data, err := os.ReadFile(targetSpaceFile)
				if err == nil && len(data) > 0 {
					targetSpace = string(data)
				} else {
					targetSpace = os.Getenv("TARGET_SPACE")
				}

				if targetSpace == "" {
					log.Printf("Background: Target space not set (via message or TARGET_SPACE env var), cannot send notification.")
					continue
				}

				var responseText string
				content := &genai.Content{
					Parts: []*genai.Part{{Text: prompt}},
				}
				for ev, err := range adkRunner.Run(ctx, "background_user", "background_session", content, agent.RunConfig{}) {
					if err != nil {
						log.Printf("Background: Error from ADK runner: %v", err)
						continue
					}
					if ev.LLMResponse.Content != nil {
						for _, part := range ev.LLMResponse.Content.Parts {
							responseText += part.Text
						}
					}
				}


				trimmedText := strings.TrimSpace(responseText)
				lowerText := strings.ToLower(trimmedText)
				if trimmedText != "" && trimmedText != "IGNORE" && !strings.Contains(lowerText, "no response generated") {
					err = sendGoogleChatMessage(ctx, targetSpace, trimmedText, "")
					if err == nil {
						_ = saveChatMessage(ctx, "user", "[SYSTEM WAKEUP ACTION] A background checkin evaluating tasks was executed natively out-of-band.")
						_ = saveChatMessage(ctx, "model", trimmedText)
					} else {
						log.Printf("Background: Error sending Google Chat message: %v", err)
					}
				}
			} // End checkin loop

			// START COMPACTION LOOP
			ctx := context.Background()
			total, err := getChatHistoryCount()
			if err != nil {
				log.Printf("Background: Failed to get chat history count: %v", err)
				continue
			}

			if total >= 20 {
				ids, msgs, err := getMessagesToCompact(ctx, 10)
				if err != nil || len(ids) == 0 {
					continue
				}

				var textBlock strings.Builder
				for _, m := range msgs {
					textBlock.WriteString(fmt.Sprintf("%s: %s\n", strings.ToUpper(m.Role), m.Text))
				}

				summaryFile := filepath.Join("memory", "SUMMARY.md")
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

				var responseText string
				for ev, err := range adkRunner.Run(ctx, "compaction_session", prompt, nil, agent.RunConfig{}) {
					if err != nil {
						log.Printf("Compaction: Error from ADK runner: %v", err)
						continue
					}
					if ev.LLMResponse.Content != nil {
						for _, part := range ev.LLMResponse.Content.Parts {
							responseText += part.Text
						}
					}
				}
				if responseText != "" {
					err = deleteCompactedMessages(ctx, ids)
					if err != nil {
						log.Printf("Background: Failed to delete compacted messages: %v", err)
					} else {
						_ = os.WriteFile(summaryFile, []byte(strings.TrimSpace(responseText)), 0644)
						log.Printf("Background: Successfully compacted %d messages into markdown summary", len(ids))
					}
				}
			}
		}
	}()
}

func main() {
	var err error

	// Determine LLM provider


	// Establish necessary system directories
	setupDirectories()

	// Initialize JSON history storage
	if err := initDB(filepath.Join("memory", "HISTORY.json")); err != nil {
		log.Fatalf("History initialization failed: %v", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-c
		log.Println("Shutting down...")
		cancel()
	}()

	subscriptionName := os.Getenv("PUBSUB_SUBSCRIPTION")
	if subscriptionName == "" {
		subscriptionName = "gchat-sub"
		log.Println("PUBSUB_SUBSCRIPTION environment variable missing, defaulting to " + subscriptionName)
	}

	// Initialize ADK Agent
	agent, err := createAgent(ctx)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessionService := session.InMemoryService()

	adkRunner, err = runner.New(runner.Config{
		Agent:          agent,
		SessionService: sessionService,
		AppName:        "chat-bot",
	})
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}

	startBackgroundTimer()
	log.Println("Background timer successfully armed.")

	log.Printf("Starting Pub/Sub monitor on %s", subscriptionName)
	err = startPubSubMonitor(ctx, subscriptionName, handleGoogleChatEvent)
	if err != nil {
		log.Fatalf("Pub/Sub monitor failed: %v", err)
	}
}
