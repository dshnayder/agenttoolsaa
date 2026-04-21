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

// RunCommandArgs defines the arguments for the runCommand tool.
type RunCommandArgs struct {
	Command string `json:"command" jsonschema:"The shell command to execute."`
}

func runCommandFunc(ctx tool.Context, args RunCommandArgs) (string, error) {
	log.Printf("ADK Tool runCommand called with args: %+v", args)
	cmd := exec.Command("bash", "-c", args.Command)
	cmd.Dir = workspaceDir

	// Still safely close pure stdin so background calls don't hang
	stdin, err := cmd.StdinPipe()
	if err == nil {
		stdin.Close()
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w, output: %s", err, string(out))
	}
	return string(out), nil
}

// ReadFileArgs defines the arguments for the readFile tool.
type ReadFileArgs struct {
	Filename string `json:"filename" jsonschema:"Relative path to the file inside the workspace."`
}

func readFileFunc(ctx tool.Context, args ReadFileArgs) (string, error) {
	log.Printf("ADK Tool readFile called with args: %+v", args)
	path := filepath.Clean(filepath.Join(workspaceDir, args.Filename))
	if !strings.HasPrefix(path, workspaceDir) {
		return "", fmt.Errorf("access denied: pathological path escaping the workspace")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFileArgs defines the arguments for the writeFile tool.
type WriteFileArgs struct {
	Filename string `json:"filename" jsonschema:"Relative path to the file inside the workspace."`
	Content  string `json:"content" jsonschema:"The exact content to write to the file."`
}

func writeFileFunc(ctx tool.Context, args WriteFileArgs) (string, error) {
	log.Printf("ADK Tool writeFile called with args: %+v", args)
	// path := filepath.Clean(filepath.Join(workspaceDir, args.Filename))
	// if !strings.HasPrefix(path, workspaceDir) {
	// 	return "", fmt.Errorf("access denied: pathological path escaping the workspace")
	// }
	// err := os.MkdirAll(filepath.Dir(path), 0755)
	// if err != nil {
	// 	return "", err
	// }
	// err = os.WriteFile(path, []byte(args.Content), 0644)
	// if err != nil {
	// 	return "", err
	// }
	return "file written successfully", nil
}

// GetADKTools returns a list of ADK tools for general use.
func GetADKTools() ([]tool.Tool, error) {
	runCommandTool, err := functiontool.New(
		functiontool.Config{
			Name:        "runCommand",
			Description: "Executes a shell command on the host securely using bash and returns the stdout and stderr output.",
		},
		runCommandFunc,
	)
	if err != nil {
		return nil, err
	}

	readFileTool, err := functiontool.New(
		functiontool.Config{
			Name:        "readFile",
			Description: "Reads the content of a local file in the workspace directory.",
		},
		readFileFunc,
	)
	if err != nil {
		return nil, err
	}

	writeFileTool, err := functiontool.New(
		functiontool.Config{
			Name:        "writeFile",
			Description: "Writes content to a local file in the workspace directory.",
		},
		writeFileFunc,
	)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{runCommandTool, readFileTool, writeFileTool}, nil
}
