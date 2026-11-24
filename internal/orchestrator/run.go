package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
)

const fileFormat = `{"type":"object","properties":{"content":{"type":"string"}},"required":["content"],"additionalProperties":false}`

type RunInput struct {
	Directory string
	File      string
	Task      string
}

type RunResult struct {
	Directory    string
	File         string
	Prompt       string
	Generation   ollama.Result
	Candidate    *string
	Verification *VerificationResult
}

func Run(ctx context.Context, input RunInput, stdout, stderr io.Writer) (RunResult, error) {
	return runWithModel(ctx, input, stdout, stderr, ollama.AskWithFormat)
}

func runWithModel(
	ctx context.Context,
	input RunInput,
	stdout, stderr io.Writer,
	askModel func(context.Context, string, json.RawMessage) (ollama.Result, error),
) (RunResult, error) {
	result := RunResult{Directory: input.Directory, File: input.File}
	task := strings.TrimSpace(input.Task)
	if task == "" {
		return result, errors.New("task description is required")
	}
	project, err := projectDirectory(input.Directory)
	if err != nil {
		return result, err
	}
	result.Directory = project

	file := input.File
	if filepath.IsAbs(file) {
		file, err = filepath.Rel(project, file)
		if err != nil {
			return result, fmt.Errorf("resolve file relative to project: %w", err)
		}
	}
	if !filepath.IsLocal(file) {
		return result, fmt.Errorf("file %q must be inside the project directory", input.File)
	}
	file = filepath.Clean(file)
	result.File = file

	err = withWorkspace(project, func(workspace string) error {
		root, err := os.OpenRoot(workspace)
		if err != nil {
			return fmt.Errorf("open workspace: %w", err)
		}
		defer root.Close()

		original, err := ReadTextFile(root.FS(), filepath.ToSlash(file))
		if err != nil {
			return fmt.Errorf("read selected file %q: %w", file, err)
		}
		instructions, err := ReadTextFile(root.FS(), "AGENTS.md")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read project instructions: %w", err)
		}

		prompt := "Update only the selected file. Return its full replacement in the JSON field content. Preserve unrelated code.\n\nTask\n" + task
		if instructions != "" {
			prompt += "\n\nProject instructions\n" + instructions
		}
		prompt += "\n\nSelected file " + filepath.ToSlash(file) + "\n" + original
		result.Prompt = prompt

		result.Generation, err = askModel(ctx, prompt, json.RawMessage(fileFormat))
		if err != nil {
			return err
		}
		var replacement struct {
			Content *string `json:"content"`
		}
		if err := json.Unmarshal([]byte(result.Generation.Text), &replacement); err != nil {
			return fmt.Errorf("decode proposed file: %w", err)
		}
		if replacement.Content == nil {
			return errors.New("model response is missing file content")
		}
		result.Candidate = replacement.Content

		if err := root.WriteFile(file, []byte(*result.Candidate), 0o644); err != nil {
			return fmt.Errorf("write proposed file in workspace: %w", err)
		}
		checked, err := verifyWorkspace(ctx, workspace, stdout, stderr)
		if err != nil {
			return err
		}
		result.Verification = &checked
		return nil
	})
	return result, err
}
