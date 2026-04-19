# Autonomous AI Agent

A fully autonomous, self-scheduling conversational AI agent capable of monitoring, managing, and fixing systems directly from WhatsApp group chats. 

Designed for deep-level system administrators, SREs, and homelab enthusiasts, this agent possesses raw unconstrained shell access (`bash`), local filesystem toolings, and fully autonomous background `cron`-style loops. It can proactively monitor system hardware or debug Kubernetes (`k8s`) clusters via `kubectl` and notify you in your private WhatsApp collaborative chat groups if issues arise.

## Core Features
1. **Dynamic Provider Engine**: Runs highly efficiently entirely on local edge-compute utilizing **Ollama** or seamlessly hooks into Cloud APIs like **Gemini** depending solely on your terminal environment variables!
2. **Autonomous Background Polling**: The AI wakes up completely independently every 60 seconds. You can chat with it to literally tell it to "remind you", "evaluate a server process in 10 minutes", or "watch the K8s cluster until nodes match the sleeping state" and it will continuously evaluate your background logic loops organically!
3. **Anthropic SKILLS.md Methodology**: The agent tracks its own contextual memory and lazily-loads rules identically to Anthropic's Progressive Disclosure models. Teach the bot how you deploy specific workloads inside the WhatsApp chat, and it will permanently index the knowledge inside `memory/skills/` natively as YAML-formatted Markdown files without clogging your token windows.
4. **Infinite Compaction Compression**: Utilizes automated semantic compression to endlessly collapse the chat history so the LLM retains your conversation memory indefinitely while avoiding token exhaustion.

## Prerequisites
- Go 1.20+
- `sqlite3` installed on host hardware
- A WhatsApp mobile account to scan the linkage QR code.

## Installation & Setup

1. Clone this repository locally to the machine or cluster node you want the agent to evaluate.
2. Ensure you establish the Go dependencies: 
   ```bash
   go mod tidy
   ```
3. Boot the application using the local environment mapping of your preferred provider logic:
   - **Local Inference (Ollama)**: 
     ```bash
     OLLAMA_MODEL="gemma:2b" go run .
     ```
   - **Cloud API**: 
     ```bash
     GEMINI_API_KEY="your_api_key" go run .
     ```
4. **Scan the QR Code**: The very first time you boot the script, your terminal will render an explicit WhatsApp Web QR Code link. Open your WhatsApp Mobile application -> Linked Devices -> Link a Device, and scan the terminal QR!

## Access Control & Security configuration (ACL)

By default, the Agent is configured dynamically as a highly secure, zero-trust endpoint. The bot natively possesses full unconstrained execution-level capacities across the host machine. **You MUST structurally lock your instance access down.**

### 1. Group-Based ACL (Recommended Collaboration)
Direct Messages (DMs) are disabled natively out of the box to prevent stray prompt-injection mapping! The Bot should be invited to a localized collaborative Administrator WhatsApp Group.

*By default messages from any group are processed and answered. If you want to restrict the bot to only respond to messages from specific groups, you need to add the group JIDs to the ALLOWED_CHATS environment variable.*

*How to map an allowed group:*
1. Make a private WhatsApp Group, and invite the Bot's linked phone number to it.
2. In the group, send the message `#hello`. 
3. Review your Golang host terminal output. You will see an explicit rejection log that reveals your group's internal Jabber ID:
   `Dropped unauthorized message from <120363XXXXX@g.us> (Not mapped in ALLOWED_CHATS)`
4. Shut down the service, and map your newly exposed group JID directly to the access list:
   ```bash
   ALLOWED_CHATS="120363XXXXX@g.us" go run .
   ```
The bot will now completely drop any messages functionally processed outside of that precise end-to-end encrypted cluster framework.

### 2. Enabling Direct Messages
If you wish to manage the bot from a direct 1-on-1 WhatsApp thread bypassing Groups entirely, you must instruct the bot to  permit DMs by setting `ALLOW_DM` to `"true"`.

```bash
ALLOW_DM="true" ALLOWED_CHATS="1234567890@s.whatsapp.net" go run .
```

## Directory Structure Overview
- `memory/store.db` - Auto-generated localized SQLite tracker mapping temporal conversation timelines.
- `memory/skills/` - Isolated dynamically rendered indexing directories structurally teaching your Bot new workflow methods.
- `memory/SUMMARY_*.md` - Compressed long-term narrative logs.
- `workspace/` - Security constraint lock box where explicit filesystem tools write out dynamically evaluated file configurations.

## Outstanding items
[ ] Make checkin scheduler deterministic. Currently it runs most tasks immediately regardless of scheduled time
[ ] Improve skill generation.