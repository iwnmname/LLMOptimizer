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

const runUsage = "llmoptimizer run [--dir PATH] --task TEXT [--file PATH] [--model NAME] [--record]"

func runTask(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", ".", "path to the Go project directory")
	task := flags.String("task", "", "description of the requested change")
	file := flags.String("file", "", "file to edit (defaults to automatic selection)")
	model := flags.String("model", "", "use this Ollama model for every request (defaults to automatic routing)")
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
		Model:     *model,
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
	if result.Context != nil && result.Context.Route.Model != "" {
		route := result.Context.Route
		fmt.Fprintf(stdout, "\nContext model: %s (%s)\n", route.Model, route.Reason)
	}
	if route := result.Route; route.Model != "" {
		fmt.Fprintf(stdout, "Generation model: %s (%s)\n", route.Model, route.Reason)
	}
	if usage := result.TotalUsage(); usage.Model != "" {
		fmt.Fprintf(stdout, "\nInput tokens: %d\nOutput tokens: %d\nRequest time: %s\n",
			usage.InputTokens, usage.OutputTokens, usage.Duration.Round(time.Millisecond))
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
	if generation := result.Generation; generation.Model != "" {
		measurements["generation"] = map[string]any{
			"model":         generation.Model,
			"input_tokens":  generation.InputTokens,
			"output_tokens": generation.OutputTokens,
			"duration_ms":   generation.Duration.Round(time.Millisecond).Milliseconds(),
		}
		if result.Candidate == nil {
			measurements["model_response"] = generation.Text
		}
	}
	routing := make(map[string]any, 2)
	if result.Route.Model != "" {
		routing["generation"] = result.Route
	}
	if selected := result.Context; selected != nil {
		if selected.Route.Model != "" {
			routing["context"] = selected.Route
		}
		measurements["context"] = map[string]any{
			"map":               selected.Map,
			"model":             selected.Planning.Model,
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
	if len(routing) > 0 {
		measurements["routing"] = routing
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
