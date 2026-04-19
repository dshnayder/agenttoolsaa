package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var workspaceDir string

func setupDirectories() {
	cwd, _ := os.Getwd()
	workspaceDir = filepath.Join(cwd, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}
	if err := os.MkdirAll("memory/skills", 0755); err != nil {
		log.Fatalf("Failed to create skills directory: %v", err)
	}
}

type ToolProperty struct {
	Type        string
	Description string
}

type ToolDefinition struct {
	Name        string
	Description string
	Properties  map[string]ToolProperty
	Required    []string
}

func GetAvailableTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "saveUserIdentity",
			Description: "Call this function to save or update the User's identity in the local system when they introduce themselves, state their name, occupation, or interests. Provide the identity data fully formatted as a Markdown document. Ensure you update and include all previously known data.",
			Properties: map[string]ToolProperty{
				"markdown_content": {Type: "string", Description: "A complete Markdown formatted string containing the user's name, occupation, interests, etc."},
			},
			Required: []string{"markdown_content"},
		},
		{
			Name:        "updateCheckin",
			Description: "Updates the autonomous background checkin list by fully rewriting the CHECKIN file. Use this to schedule future tasks, add reminders, or remove them when completed.",
			Properties: map[string]ToolProperty{
				"markdown_content": {Type: "string", Description: "The exact complete content to overwrite the CHECKIN file. If clearing all tasks, pass an empty string."},
			},
			Required: []string{"markdown_content"},
		},
		{
			Name:        "writeSkill",
			Description: "Call this function to save a learned skill, rule, or methodology discussed by the user into long-term memory so you can recall how to perform tasks in the future.",
			Properties: map[string]ToolProperty{
				"skill_name":       {Type: "string", Description: "A short, descriptive snake-case or kebab-case identifier for the skill (e.g., format-json)."},
				"description":      {Type: "string", Description: "A crisp 1-2 sentence description explaining exactly when to trigger this skill."},
				"markdown_content": {Type: "string", Description: "A detailed Markdown document capturing the tool usage, methodology, or logic the user taught you."},
			},
			Required: []string{"skill_name", "description", "markdown_content"},
		},
		{
			Name:        "readSkill",
			Description: "Reads the complete SKILL.md file for a documented skill out of your long-term memory.",
			Properties: map[string]ToolProperty{
				"skill_name": {Type: "string", Description: "The exact name of the skill extracted from the system index."},
			},
			Required: []string{"skill_name"},
		},
		{
			Name:        "readFile",
			Description: "Reads the content of a local file in the workspace directory.",
			Properties: map[string]ToolProperty{
				"filename": {Type: "string", Description: "Relative path to the file inside the workspace."},
			},
			Required: []string{"filename"},
		},
		{
			Name:        "writeFile",
			Description: "Writes content to a local file in the workspace directory.",
			Properties: map[string]ToolProperty{
				"filename": {Type: "string", Description: "Relative path to the file inside the workspace."},
				"content":  {Type: "string", Description: "The exact content to write to the file."},
			},
			Required: []string{"filename", "content"},
		},
		{
			Name:        "runCommand",
			Description: "Executes a shell command on the host securely using bash and returns the stdout and stderr output. Piping ('|') and grep are supported.",
			Properties: map[string]ToolProperty{
				"command": {Type: "string", Description: "The shell command to execute."},
			},
			Required: []string{"command"},
		},
	}
}

// ExecuteTool routes an API-agnostic tool request cleanly.
func ExecuteTool(name string, args map[string]any, userPhone string) map[string]any {
	var result map[string]any

	log.Printf("Executing tool: %s, Args: %+v", name, args)

	switch name {
	case "saveUserIdentity":
		if contentObj, ok := args["markdown_content"]; ok {
			if mdStr, isStr := contentObj.(string); isStr {
				userFile := filepath.Join("memory", fmt.Sprintf("USER_%s.md", userPhone))
				_ = os.WriteFile(userFile, []byte(mdStr), 0644)
				result = map[string]any{"status": "success", "file_saved": userFile}
			} else {
				result = map[string]any{"error": "invalid content type"}
			}
		} else {
			result = map[string]any{"error": "missing markdown_content"}
		}

	case "updateCheckin":
		if contentObj, ok := args["markdown_content"]; ok {
			if mdStr, isStr := contentObj.(string); isStr {
				checkinFile := filepath.Join("memory", fmt.Sprintf("CHECKIN_%s.md", userPhone))
				_ = os.WriteFile(checkinFile, []byte(mdStr), 0644)
				result = map[string]any{"status": "success", "file_saved": checkinFile}
			} else {
				result = map[string]any{"error": "invalid content type"}
			}
		} else {
			result = map[string]any{"error": "missing markdown_content"}
		}

	case "writeSkill":
		if nameObj, okName := args["skill_name"]; okName {
			if descObj, okDesc := args["description"]; okDesc {
				if contentObj, okBody := args["markdown_content"]; okBody {
					if skillStr, isStr1 := nameObj.(string); isStr1 {
						if descStr, isStr2 := descObj.(string); isStr2 {
							if mdStr, isStr3 := contentObj.(string); isStr3 {
								// Construct Anthropic native format
								skillDir := filepath.Join("memory", "skills", skillStr)
								_ = os.MkdirAll(skillDir, 0755)
								skillPath := filepath.Join(skillDir, "SKILL.md")

								fullFileContent := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s", skillStr, descStr, mdStr)

								_ = os.WriteFile(skillPath, []byte(fullFileContent), 0644)
								result = map[string]any{"status": "success", "file_saved": skillPath}
							}
						}
					}
				}
			}
		}
		if result == nil {
			result = map[string]any{"error": "missing or invalid properties for writeSkill"}
		}

	case "readSkill":
		if nameObj, okName := args["skill_name"]; okName {
			if skillStr, isStr := nameObj.(string); isStr {
				skillPath := filepath.Join("memory", "skills", skillStr, "SKILL.md")
				data, err := os.ReadFile(skillPath)
				if err != nil {
					result = map[string]any{"error": "skill not found or couldn't be read: " + err.Error()}
				} else {
					result = map[string]any{"content": string(data)}
				}
			}
		}
		if result == nil {
			result = map[string]any{"error": "missing or invalid properties for readSkill"}
		}

	case "readFile":
		if fileObj, ok := args["filename"]; ok {
			if fileStr, isStr := fileObj.(string); isStr {
				path := filepath.Clean(filepath.Join(workspaceDir, fileStr))
				if !strings.HasPrefix(path, workspaceDir) {
					result = map[string]any{"error": "access denied: pathological path escaping the workspace"}
				} else {
					data, err := os.ReadFile(path)
					if err != nil {
						result = map[string]any{"error": err.Error()}
					} else {
						result = map[string]any{"content": string(data)}
					}
				}
			} else {
				result = map[string]any{"error": "invalid filename type"}
			}
		} else {
			result = map[string]any{"error": "missing filename"}
		}

	case "writeFile":
		if fileObj, ok := args["filename"]; ok {
			if contentObj, hasContent := args["content"]; hasContent {
				fileStr, _ := fileObj.(string)
				contentStr, _ := contentObj.(string)
				path := filepath.Clean(filepath.Join(workspaceDir, fileStr))
				if !strings.HasPrefix(path, workspaceDir) {
					result = map[string]any{"error": "access denied: pathological path escaping the workspace"}
				} else {
					_ = os.MkdirAll(filepath.Dir(path), 0755)
					err := os.WriteFile(path, []byte(contentStr), 0644)
					if err != nil {
						result = map[string]any{"error": err.Error()}
					} else {
						result = map[string]any{"status": "file written successfully"}
					}
				}
			} else {
				result = map[string]any{"error": "missing content"}
			}
		} else {
			result = map[string]any{"error": "missing filename"}
		}

	case "runCommand":
		if cmdObj, ok := args["command"]; ok {
			if cmdStr, isStr := cmdObj.(string); isStr {
				cmd := exec.Command("bash", "-c", cmdStr)
				cmd.Dir = workspaceDir

				// Still safely close pure stdin so background calls don't hang
				stdin, err := cmd.StdinPipe()
				if err == nil {
					stdin.Close()
				}

				out, err := cmd.CombinedOutput()
				if err != nil {
					result = map[string]any{"error": err.Error(), "output": string(out)}
				} else {
					result = map[string]any{"output": string(out)}
				}
			} else {
				result = map[string]any{"error": "invalid command type"}
			}
		} else {
			result = map[string]any{"error": "missing command"}
		}

	default:
		result = map[string]any{"error": "unknown function executed"}
	}

	return result
}
