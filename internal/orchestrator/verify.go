package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type VerificationResult struct {
	ExitCode int
	Duration time.Duration
}

func Verify(ctx context.Context, projectPath string, stdout, stderr io.Writer) (result VerificationResult, err error) {
	project, err := projectDirectory(projectPath)
	if err != nil {
		return result, err
	}

	err = withWorkspace(project, func(workspace string) error {
		var err error
		result, err = verifyWorkspace(ctx, workspace, stdout, stderr)
		return err
	})
	return result, err
}

func verifyWorkspace(ctx context.Context, workspace string, stdout, stderr io.Writer) (VerificationResult, error) {
	var result VerificationResult
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	command.Dir = workspace
	command.Stdout = stdout
	command.Stderr = stderr

	started := time.Now()
	runErr := command.Run()
	result.Duration = time.Since(started)
	if runErr == nil {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() >= 0 {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("run Go tests: %w", runErr)
}
