package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
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

// 1. saveUserIdentity
type SaveUserIdentityArgs struct {
	MarkdownContent string `json:"markdown_content" jsonschema:"description:A complete Markdown formatted string containing the user's name, occupation, interests, etc."`
}

type SaveUserIdentityResults struct {
	Status    string `json:"status"`
	FileSaved string `json:"file_saved"`
}

func saveUserIdentityFunc(ctx tool.Context, args SaveUserIdentityArgs) (SaveUserIdentityResults, error) {
	userFile := filepath.Join("memory", "USER.md")
	err := os.WriteFile(userFile, []byte(args.MarkdownContent), 0644)
	if err != nil {
		return SaveUserIdentityResults{}, err
	}
	return SaveUserIdentityResults{Status: "success", FileSaved: userFile}, nil
}

// 2. updateCheckin
type UpdateCheckinArgs struct {
	MarkdownContent string `json:"markdown_content" jsonschema:"description:The exact complete content to overwrite the CHECKIN file. If clearing all tasks, pass an empty string."`
}

type UpdateCheckinResults struct {
	Status    string `json:"status"`
	FileSaved string `json:"file_saved"`
}

func updateCheckinFunc(ctx tool.Context, args UpdateCheckinArgs) (UpdateCheckinResults, error) {
	checkinFile := filepath.Join("memory", "CHECKIN.md")
	err := os.WriteFile(checkinFile, []byte(args.MarkdownContent), 0644)
	if err != nil {
		return UpdateCheckinResults{}, err
	}
	return UpdateCheckinResults{Status: "success", FileSaved: checkinFile}, nil
}

// 3. writeSkill
type WriteSkillArgs struct {
	SkillName       string `json:"skill_name" jsonschema:"description:A short, descriptive snake-case or kebab-case identifier for the skill."`
	Description     string `json:"description" jsonschema:"description:A crisp 1-2 sentence description explaining exactly when to trigger this skill."`
	MarkdownContent string `json:"markdown_content" jsonschema:"description:A detailed Markdown document capturing the tool usage, methodology, or logic."`
}

type WriteSkillResults struct {
	Status    string `json:"status"`
	FileSaved string `json:"file_saved"`
}

func writeSkillFunc(ctx tool.Context, args WriteSkillArgs) (WriteSkillResults, error) {
	skillDir := filepath.Join("memory", "skills", args.SkillName)
	_ = os.MkdirAll(skillDir, 0755)
	skillPath := filepath.Join(skillDir, "SKILL.md")

	fullFileContent := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s", args.SkillName, args.Description, args.MarkdownContent)

	err := os.WriteFile(skillPath, []byte(fullFileContent), 0644)
	if err != nil {
		return WriteSkillResults{}, err
	}
	return WriteSkillResults{Status: "success", FileSaved: skillPath}, nil
}

// 4. readSkill
type ReadSkillArgs struct {
	SkillName string `json:"skill_name" jsonschema:"description:The exact name of the skill extracted from the system index."`
}

type ReadSkillResults struct {
	Content string `json:"content"`
}

func readSkillFunc(ctx tool.Context, args ReadSkillArgs) (ReadSkillResults, error) {
	skillPath := filepath.Join("memory", "skills", args.SkillName, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return ReadSkillResults{}, fmt.Errorf("skill not found or couldn't be read: %w", err)
	}
	return ReadSkillResults{Content: string(data)}, nil
}

// 5. readFile
type ReadFileArgs struct {
	Filename string `json:"filename" jsonschema:"description:Relative path to the file inside the workspace."`
}

type ReadFileResults struct {
	Content string `json:"content"`
}

func readFileFunc(ctx tool.Context, args ReadFileArgs) (ReadFileResults, error) {
	path := filepath.Clean(filepath.Join(workspaceDir, args.Filename))
	if !strings.HasPrefix(path, workspaceDir) {
		return ReadFileResults{}, fmt.Errorf("access denied: pathological path escaping the workspace")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReadFileResults{}, err
	}
	return ReadFileResults{Content: string(data)}, nil
}

// 6. writeFile
type WriteFileArgs struct {
	Filename string `json:"filename" jsonschema:"description:Relative path to the file inside the workspace."`
	Content  string `json:"content" jsonschema:"description:The exact content to write to the file."`
}

type WriteFileResults struct {
	Status string `json:"status"`
}

func writeFileFunc(ctx tool.Context, args WriteFileArgs) (WriteFileResults, error) {
	path := filepath.Clean(filepath.Join(workspaceDir, args.Filename))
	if !strings.HasPrefix(path, workspaceDir) {
		return WriteFileResults{}, fmt.Errorf("access denied: pathological path escaping the workspace")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	err := os.WriteFile(path, []byte(args.Content), 0644)
	if err != nil {
		return WriteFileResults{}, err
	}
	return WriteFileResults{Status: "file written successfully"}, nil
}

// 7. runCommand
type RunCommandArgs struct {
	Command string `json:"command" jsonschema:"description:The shell command to execute."`
}

type RunCommandResults struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

func runCommandFunc(ctx tool.Context, args RunCommandArgs) (RunCommandResults, error) {
	cmd := exec.Command("bash", "-c", args.Command)
	cmd.Dir = workspaceDir

	stdin, err := cmd.StdinPipe()
	if err == nil {
		stdin.Close()
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return RunCommandResults{Output: string(out), Error: err.Error()}, nil
	}
	return RunCommandResults{Output: string(out)}, nil
}

// Helper to get all tools as ADK objects
func GetAllTools() ([]tool.Tool, error) {
	saveUserIdentityTool, err := functiontool.New(functiontool.Config{
		Name:        "saveUserIdentity",
		Description: "Save or update the User's identity in the local system.",
	}, saveUserIdentityFunc)
	if err != nil {
		return nil, err
	}

	updateCheckinTool, err := functiontool.New(functiontool.Config{
		Name:        "updateCheckin",
		Description: "Updates the autonomous background checkin list by fully rewriting the CHECKIN file.",
	}, updateCheckinFunc)
	if err != nil {
		return nil, err
	}

	writeSkillTool, err := functiontool.New(functiontool.Config{
		Name:        "writeSkill",
		Description: "Save a learned skill, rule, or methodology discussed by the user.",
	}, writeSkillFunc)
	if err != nil {
		return nil, err
	}

	readSkillTool, err := functiontool.New(functiontool.Config{
		Name:        "readSkill",
		Description: "Reads the complete SKILL.md file for a documented skill.",
	}, readSkillFunc)
	if err != nil {
		return nil, err
	}

	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "readFile",
		Description: "Reads the content of a local file in the workspace directory.",
	}, readFileFunc)
	if err != nil {
		return nil, err
	}

	writeFileTool, err := functiontool.New(functiontool.Config{
		Name:        "writeFile",
		Description: "Writes content to a local file in the workspace directory.",
	}, writeFileFunc)
	if err != nil {
		return nil, err
	}

	runCommandTool, err := functiontool.New(functiontool.Config{
		Name:        "runCommand",
		Description: "Executes a shell command on the host securely using bash.",
	}, runCommandFunc)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{
		saveUserIdentityTool,
		updateCheckinTool,
		writeSkillTool,
		readSkillTool,
		readFileTool,
		writeFileTool,
		runCommandTool,
	}, nil
}
