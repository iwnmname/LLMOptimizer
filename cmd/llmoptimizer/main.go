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
	"syscall"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const inspectUsage = "llmoptimizer inspect [--dir PATH] --task TEXT"
const verifyUsage = "llmoptimizer verify [--dir PATH] [--record]"

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "Usage:\n  " + inspectUsage + "\n  " + verifyUsage + "\n  " + askUsage + "\n  " + runUsage + "\n  " + indexUsage

	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Fprintln(stdout, usage)
		return 0
	case "inspect":
		return inspect(args[1:], stdout, stderr)
	case "verify":
		return verify(args[1:], stdout, stderr)
	case "ask":
		return ask(args[1:], stdout, stderr)
	case "run":
		return runTask(args[1:], stdout, stderr)
	case "index":
		return indexRepository(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", args[0])
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

func inspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", ".", "path to the project directory")
	task := flags.String("task", "", "description of the requested change")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: "+inspectUsage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Pass the task description with --task.")
		return 2
	}

	prepared, err := orchestrator.Prepare(context.Background(), *directory, *task)
	if err != nil {
		fmt.Fprintf(stderr, "Cannot prepare task: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Project directory: %s\n", prepared.ProjectRoot)
	if prepared.HeadCommit != "" {
		fmt.Fprintf(stdout, "HEAD commit: %s\n", prepared.HeadCommit)
	}
	fmt.Fprintf(stdout, "Task: %s\n", prepared.Description)
	if prepared.InstructionsPath == "" {
		fmt.Fprintln(stdout, "Root AGENTS.md: absent")
	} else {
		fmt.Fprintf(stdout, "Root AGENTS.md: %s\n", prepared.InstructionsPath)
	}
	return 0
}

func verify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", ".", "path to the Go project directory")
	record := flags.Bool("record", false, "save test output and measurements in experiments/")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: "+verifyUsage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Pass the project directory with --dir.")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	run, log, err := startTestExperiment(*record, "verify")
	if err != nil {
		fmt.Fprintf(stderr, "Cannot start experiment: %v\n", err)
		return 1
	}
	testOut, testErr := stdout, stderr
	if log != nil {
		testOut, testErr = io.MultiWriter(stdout, log), io.MultiWriter(stderr, log)
	}

	result, err := orchestrator.Verify(ctx, *directory, testOut, testErr)
	if log != nil {
		err = errors.Join(err, log.Close())
	}
	if err != nil {
		fmt.Fprintf(stderr, "Cannot verify project: %v\n", err)
	} else {
		fmt.Fprintf(stdout, "Go tests finished in %s (exit code %d)\n", result.Duration.Round(time.Millisecond), result.ExitCode)
	}

	if run != nil {
		projectPath, _ := filepath.Abs(*directory)
		measurements := map[string]any{"project_directory": projectPath, "test_log": testLogName}
		if result.Duration > 0 {
			measurements["duration_ms"] = result.Duration.Round(time.Millisecond).Milliseconds()
		}
		status := "success"
		if err == nil {
			measurements["exit_code"] = result.ExitCode
			if result.ExitCode != 0 {
				status = "failed"
			}
		}
		if !recordExperiment(run, status, measurements, err, stdout, stderr) {
			return 1
		}
	}
	if err != nil {
		return 1
	}
	return result.ExitCode
}
