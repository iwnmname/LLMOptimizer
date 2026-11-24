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

	"github.com/iwnmname/LLMOptimizer/internal/experiment"
	"github.com/iwnmname/LLMOptimizer/internal/ollama"
	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

const askUsage = "llmoptimizer ask --task TEXT [--file PATH] [--record]"

func ask(args []string, stdout, stderr io.Writer) int {
	return askWithModel(args, stdout, stderr, ollama.Ask)
}

func askWithModel(args []string, stdout, stderr io.Writer, askModel func(context.Context, string) (ollama.Result, error)) int {
	flags := flag.NewFlagSet("ask", flag.ContinueOnError)
	flags.SetOutput(stderr)
	task := flags.String("task", "", "text to send to the local model")
	file := flags.String("file", "", "source file to include in the prompt")
	record := flags.Bool("record", false, "save the request and measurements in experiments/")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: "+askUsage)
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

	prompt := taskText
	var sourcePath string
	if *file != "" {
		var err error
		prompt, sourcePath, err = promptWithFile(taskText, *file)
		if err != nil {
			fmt.Fprintf(stderr, "Cannot include source file: %v\n", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var run *experiment.Run
	if *record {
		var err error
		run, err = experiment.Start("ask")
		if err != nil {
			fmt.Fprintf(stderr, "Cannot start experiment: %v\n", err)
			return 1
		}
	}

	started := time.Now()
	result, err := askModel(ctx, prompt)
	if err != nil {
		fmt.Fprintf(stderr, "Cannot ask model: %v\n", err)
	} else {
		fmt.Fprintln(stdout, result.Text)
		fmt.Fprintf(stdout, "\nModel: %s\nInput tokens: %d\nOutput tokens: %d\nRequest time: %s\n",
			result.Model, result.InputTokens, result.OutputTokens, result.Duration.Round(time.Millisecond))
	}

	if run != nil {
		measurements := map[string]any{"task": taskText}
		if sourcePath != "" {
			measurements["file_path"] = sourcePath
			measurements["prompt"] = prompt
		}
		if err != nil {
			measurements["duration_ms"] = time.Since(started).Round(time.Millisecond).Milliseconds()
		} else {
			measurements["answer"] = result.Text
			measurements["model"] = result.Model
			measurements["input_tokens"] = result.InputTokens
			measurements["output_tokens"] = result.OutputTokens
			measurements["duration_ms"] = result.Duration.Round(time.Millisecond).Milliseconds()
		}
		if !recordExperiment(run, "success", measurements, err, stdout, stderr) {
			return 1
		}
	}
	if err != nil {
		return 1
	}
	return 0
}

func promptWithFile(task, file string) (string, string, error) {
	path, err := filepath.Abs(file)
	if err != nil {
		return "", "", err
	}
	content, err := orchestrator.ReadTextFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return "", "", err
	}
	prompt := fmt.Sprintf("Task\n%s\n\nSource file %s\n%s", task, filepath.Base(path), content)
	return prompt, path, nil
}
