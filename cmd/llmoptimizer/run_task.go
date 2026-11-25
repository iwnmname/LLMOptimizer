package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

const runUsage = "llmoptimizer run [--dir PATH] --task TEXT [--file PATH] [--record]"

func runTask(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", ".", "path to the Go project directory")
	task := flags.String("task", "", "description of the requested change")
	file := flags.String("file", "", "file to edit (defaults to automatic selection)")
	record := flags.Bool("record", false, "save model and test results in experiments/")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: "+runUsage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	taskText := strings.TrimSpace(*task)
	if flags.NArg() != 0 || taskText == "" {
		fmt.Fprintln(stderr, "Pass a non-empty task with --task.")
		return 2
	}

	recording, log, err := startTestExperiment(*record, "run")
	if err != nil {
		fmt.Fprintf(stderr, "Cannot start experiment: %v\n", err)
		return 1
	}
	testOut, testErr := stdout, stderr
	if log != nil {
		testOut, testErr = io.MultiWriter(stdout, log), io.MultiWriter(stderr, log)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := orchestrator.Run(ctx, orchestrator.RunInput{
		Directory: *directory,
		File:      *file,
		Task:      taskText,
	}, testOut, testErr)
	if log != nil {
		err = errors.Join(err, log.Close())
	}

	if result.Candidate != nil {
		fmt.Fprintf(stdout, "\nProposed file %s\n%s\n", result.File, *result.Candidate)
	}
	if result.Context != nil && result.Context.Planning.Model != "" {
		fmt.Fprintf(stdout, "\nContext selection: %d input tokens, %d output tokens, %d additional fragments\n",
			result.Context.Planning.InputTokens, result.Context.Planning.OutputTokens, len(result.Context.ExpandedSymbols))
	}
	if usage := result.TotalUsage(); usage.Model != "" {
		fmt.Fprintf(stdout, "\nModel: %s\nInput tokens: %d\nOutput tokens: %d\nRequest time: %s\n",
			usage.Model, usage.InputTokens, usage.OutputTokens, usage.Duration.Round(time.Millisecond))
	}

	status, exitCode := "success", 0
	if checked := result.Verification; checked != nil {
		fmt.Fprintf(stdout, "Go tests finished in %s (exit code %d)\n",
			checked.Duration.Round(time.Millisecond), checked.ExitCode)
		exitCode = checked.ExitCode
		if exitCode != 0 {
			status = "failed"
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "Cannot run task: %v\n", err)
		exitCode = 1
	}

	if recording != nil {
		if !recordExperiment(recording, status, runMeasurements(taskText, result), err, stdout, stderr) {
			return 1
		}
		path := filepath.Join(recording.Directory, "README.md")
		if writeErr := os.WriteFile(path, []byte(renderRunSummary(taskText, result, err)), 0o600); writeErr != nil {
			fmt.Fprintf(stderr, "Cannot write experiment summary: %v\n", writeErr)
			return 1
		}
		fmt.Fprintf(stdout, "Summary: %s\n", path)
	}
	return exitCode
}

func runMeasurements(task string, result orchestrator.RunResult) map[string]any {
	measurements := map[string]any{
		"task":              task,
		"project_directory": result.Directory,
		"file_path":         result.File,
		"test_log":          testLogName,
	}
	if result.Prompt != "" {
		measurements["prompt"] = result.Prompt
	}
	if usage := result.TotalUsage(); usage.Model != "" {
		measurements["model"] = usage.Model
		measurements["input_tokens"] = usage.InputTokens
		measurements["output_tokens"] = usage.OutputTokens
		measurements["model_duration_ms"] = usage.Duration.Round(time.Millisecond).Milliseconds()
	}
	if result.Generation.Model != "" && result.Candidate == nil {
		measurements["model_response"] = result.Generation.Text
	}
	if selected := result.Context; selected != nil {
		measurements["context"] = map[string]any{
			"map":               selected.Map,
			"planning_prompt":   selected.Prompt,
			"planning_response": selected.Planning.Text,
			"input_tokens":      selected.Planning.InputTokens,
			"output_tokens":     selected.Planning.OutputTokens,
			"duration_ms":       selected.Planning.Duration.Round(time.Millisecond).Milliseconds(),
			"requested_symbols": selected.RequestedSymbols,
			"automatic_symbols": selected.AutomaticSymbols,
			"expanded_symbols":  selected.ExpandedSymbols,
		}
	}
	if result.Candidate != nil {
		measurements["candidate"] = *result.Candidate
	}
	if checked := result.Verification; checked != nil {
		measurements["exit_code"] = checked.ExitCode
		measurements["test_duration_ms"] = checked.Duration.Round(time.Millisecond).Milliseconds()
	}
	return measurements
}
