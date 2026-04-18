package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"google.golang.org/genai"
)

var workspaceDir string

func setupDirectories() {
	cwd, _ := os.Getwd()
	workspaceDir = filepath.Join(cwd, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}
	if err := os.MkdirAll("memory", 0755); err != nil {
		log.Fatalf("Failed to create memory directory: %v", err)
	}
}

func getToolDeclarations() []*genai.Tool {
	return []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
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
				},
				{
					Name:        "updateCheckin",
					Description: "Updates the autonomous background checkin list by fully rewriting the CHECKIN file. Use this to schedule future tasks, add reminders, or remove them when completed.",
					Parameters: &genai.Schema{
						Type: "object",
						Properties: map[string]*genai.Schema{
							"markdown_content": {
								Type:        "string",
								Description: "The exact complete content to overwrite the CHECKIN file. If clearing all tasks, pass an empty string.",
							},
						},
						Required: []string{"markdown_content"},
					},
				},
				{
					Name:        "readFile",
					Description: "Reads the content of a local file in the workspace directory.",
					Parameters: &genai.Schema{
						Type: "object",
						Properties: map[string]*genai.Schema{
							"filename": {
								Type:        "string",
								Description: "Relative path to the file inside the workspace.",
							},
						},
						Required: []string{"filename"},
					},
				},
				{
					Name:        "writeFile",
					Description: "Writes content to a local file in the workspace directory.",
					Parameters: &genai.Schema{
						Type: "object",
						Properties: map[string]*genai.Schema{
							"filename": {
								Type:        "string",
								Description: "Relative path to the file inside the workspace.",
							},
							"content": {
								Type:        "string",
								Description: "The exact content to write to the file.",
							},
						},
						Required: []string{"filename", "content"},
					},
				},
				{
					Name:        "runCommand",
					Description: "Executes a shell command on the host securely using bash and returns the stdout and stderr output. Piping ('|') and grep are supported.",
					Parameters: &genai.Schema{
						Type: "object",
						Properties: map[string]*genai.Schema{
							"command": {
								Type:        "string",
								Description: "The shell command to execute.",
							},
						},
						Required: []string{"command"},
					},
				},
			},
		},
	}
}

// executeFunctionCall processes the function call and returns a FunctionResponse Part
func executeFunctionCall(fc *genai.FunctionCall, userPhone string) genai.Part {
	name := fc.Name
	args := fc.Args
	var result map[string]any

	log.Printf("Executing tool: %s", name)

	switch name {
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
					// ensure directory paths exist inside the workspace naturally
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
				cmd.Dir = workspaceDir // execute strictly inside the workspace

				out, err := cmd.CombinedOutput()
				if err != nil {
					// CombinedOutput catches stderr as well
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

	return genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			Name:     name,
			Response: result,
		},
	}
}
