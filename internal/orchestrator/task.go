package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PreparedTask struct {
	Description      string
	ProjectRoot      string
	HeadCommit       string
	InstructionsPath string
}

func Prepare(ctx context.Context, projectPath, description string) (PreparedTask, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return PreparedTask{}, errors.New("task description is required")
	}
	root, err := projectDirectory(projectPath)
	if err != nil {
		return PreparedTask{}, err
	}

	prepared := PreparedTask{
		Description: description,
		ProjectRoot: root,
	}
	if gitRoot := gitOutput(ctx, root, "rev-parse", "--show-toplevel"); gitRoot != "" {
		prepared.ProjectRoot = gitRoot
		prepared.HeadCommit = gitOutput(ctx, gitRoot, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	}
	if err := ctx.Err(); err != nil {
		return PreparedTask{}, err
	}

	path := filepath.Join(prepared.ProjectRoot, "AGENTS.md")
	instructions, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return prepared, nil
	case err != nil:
		return PreparedTask{}, fmt.Errorf("inspect project instructions at %q: %w", path, err)
	case !instructions.Mode().IsRegular():
		return PreparedTask{}, fmt.Errorf("project instructions at %q must be a regular file", path)
	}
	prepared.InstructionsPath = path
	return prepared, nil
}

func gitOutput(ctx context.Context, directory string, args ...string) string {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(output), "\n")
}
