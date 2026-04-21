# Autonomous AI Agent

A fully autonomous, self-scheduling conversational AI agent capable of monitoring, managing, and fixing systems directly from Google Chat. 

Designed for deep-level system administrators, SREs, and homelab enthusiasts, this agent possesses raw unconstrained shell access (`bash`), local filesystem toolings, and fully autonomous background `cron`-style loops. It can proactively monitor system hardware or debug Kubernetes (`k8s`) clusters via `kubectl` and notify you in your Google Chat spaces if issues arise.

## Core Features
1. **Dynamic Provider Engine**: Runs highly efficiently entirely on local edge-compute utilizing **Ollama** or seamlessly hooks into Cloud APIs like **Gemini** depending solely on your terminal environment variables!
2. **Autonomous Background Polling**: The AI wakes up completely independently every 60 seconds. You can chat with it to literally tell it to "remind you", "evaluate a server process in 10 minutes", or "watch the K8s cluster until nodes match the sleeping state" and it will continuously evaluate your background logic loops organically!
3. **Anthropic SKILLS.md Methodology**: The agent tracks its own contextual memory and lazily-loads rules identically to Anthropic's Progressive Disclosure models. Teach the bot how you deploy specific workloads inside Google Chat, and it will permanently index the knowledge inside `memory/skills/` natively as YAML-formatted Markdown files without clogging your token windows.
4. **Infinite Compaction Compression**: Utilizes automated semantic compression to endlessly collapse the chat history so the LLM retains your conversation memory indefinitely while avoiding token exhaustion.

## Prerequisites
- Go 1.20+

## Installation & Setup

1. Clone this repository locally to the machine or cluster node you want the agent to evaluate.
2. Ensure you establish the Go dependencies: 
   ```bash
   go mod tidy
   ```
3. Boot the application using the local environment mapping of your preferred provider logic:
   - **Local Inference (Ollama)**: 
     ```bash
     OLLAMA_MODEL="gemma:2b" PUBSUB_SUBSCRIPTION="your_subscription_name" go run .
     ```
   - **Cloud API**: 
     ```bash
     GEMINI_API_KEY="your_api_key" PUBSUB_SUBSCRIPTION="your_subscription_name" go run .
     ```

*Note: The application uses Google Cloud Pub/Sub for communication. Ensure you have configured `PUBSUB_SUBSCRIPTION` and have proper Application Default Credentials (ADC) set up.*

## Configuration

### Pub/Sub Subscription
The application listens for messages on a Google Cloud Pub/Sub subscription. You can configure it by setting the `PUBSUB_SUBSCRIPTION` environment variable:

```bash
PUBSUB_SUBSCRIPTION="projects/your-project-id/subscriptions/your-subscription-name" go run .
```

### Background Notifications
For background tasks (checkins), the bot needs to know where to send notifications. It will automatically use the space name of the last received message. Alternatively, you can configure a default space by setting the `TARGET_SPACE` environment variable:

```bash
TARGET_SPACE="spaces/XXXXXXXXX" go run .
```

*Note: If `TARGET_SPACE` is not set and the bot hasn't received any messages yet, background notifications will be skipped.*

## Directory Structure Overview
- `memory/HISTORY.json` - Auto-generated localized JSON tracker mapping temporal conversation timelines.
- `memory/skills/` - Isolated dynamically rendered indexing directories structurally teaching your Bot new workflow methods.
- `memory/SUMMARY_*.md` - Compressed long-term narrative logs.
- `workspace/` - Security constraint lock box where explicit filesystem tools write out dynamically evaluated file configurations.

## Outstanding items

* [ ] Make checkin scheduler deterministic. Currently it runs most tasks immediately regardless of scheduled time
* [ ] Improve skill generation.