package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

const runUsage = "llmoptimizer run [--dir PATH] --task TEXT --file PATH [--record]"

func runTask(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", ".", "path to the Go project directory")
	task := flags.String("task", "", "description of the requested change")
	file := flags.String("file", "", "file to edit, relative to --dir or absolute inside it")
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
	if flags.NArg() != 0 || taskText == "" || *file == "" {
		fmt.Fprintln(stderr, "Pass a non-empty task with --task and select a file with --file.")
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
	if result.Generation.Model != "" {
		fmt.Fprintf(stdout, "\nModel: %s\nInput tokens: %d\nOutput tokens: %d\nRequest time: %s\n",
			result.Generation.Model, result.Generation.InputTokens, result.Generation.OutputTokens,
			result.Generation.Duration.Round(time.Millisecond))
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

	if recording != nil && !recordExperiment(recording, status, runMeasurements(taskText, result), err, stdout, stderr) {
		return 1
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
	if result.Generation.Model != "" {
		measurements["model"] = result.Generation.Model
		measurements["input_tokens"] = result.Generation.InputTokens
		measurements["output_tokens"] = result.Generation.OutputTokens
		measurements["model_duration_ms"] = result.Generation.Duration.Round(time.Millisecond).Milliseconds()
		if result.Candidate == nil {
			measurements["model_response"] = result.Generation.Text
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
