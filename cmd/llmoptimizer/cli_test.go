package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

func TestCLIHelpAndMissingArguments(t *testing.T) {
	var stdout bytes.Buffer
	if code := run([]string{"--help"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	for _, command := range []string{"inspect", "ask", "verify", "run"} {
		if !strings.Contains(stdout.String(), "llmoptimizer "+command) {
			t.Errorf("help is missing %s", command)
		}
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{args: []string{"unknown"}, code: 2},
		{args: []string{"ask"}, code: 2},
		{args: []string{"inspect"}, code: 1},
		{args: []string{"run"}, code: 2},
		{args: []string{"verify", "extra"}, code: 2},
	} {
		if code := run(tc.args, io.Discard, io.Discard); code != tc.code {
			t.Errorf("run(%v) exit code = %d, want %d", tc.args, code, tc.code)
		}
	}
}

func TestAskIncludesFileAndRecordsOnlyWhenRequested(t *testing.T) {
	t.Chdir(t.TempDir())
	const source = "package sample\nconst Result = 42\n"
	file := "sample file.go"
	writeCLIFile(t, file, source)
	generate := func(ctx context.Context, prompt string) (ollama.Result, error) {
		if ctx.Err() != nil || !strings.Contains(prompt, "Read constant") || !strings.HasSuffix(prompt, source) {
			t.Fatalf("unexpected prompt or context: %q (%v)", prompt, ctx.Err())
		}
		return ollama.Result{Model: "fixture", Text: "42", InputTokens: 13, OutputTokens: 1}, nil
	}
	args := []string{"--task", "Read constant", "--file", file}
	var stdout, stderr bytes.Buffer
	if code := askWithModel(args, &stdout, &stderr, generate); code != 0 {
		t.Fatalf("ask exit code = %d: %s", code, stderr.String())
	}
	if _, err := os.Stat("experiments"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created experiments without --record: %v", err)
	}
	stdout.Reset()
	if code := askWithModel(append(args, "--record"), &stdout, &stderr, generate); code != 0 ||
		!strings.Contains(stdout.String(), "Input tokens: 13") {
		t.Fatalf("recorded ask exit code = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	_, record := readCLIReport(t)
	if record.Command != "ask" || record.Status != "success" || record.Measurements["answer"] != "42" ||
		record.Measurements["input_tokens"] != float64(13) || record.Measurements["task"] != "Read constant" {
		t.Fatalf("unexpected ask report: %+v", record)
	}
	if prompt, ok := record.Measurements["prompt"].(string); !ok || !strings.HasSuffix(prompt, source) {
		t.Fatalf("wrong prompt in report: %q", prompt)
	}
	filePath, ok := record.Measurements["file_path"].(string)
	if !ok || !filepath.IsAbs(filePath) || filepath.Base(filePath) != file {
		t.Fatalf("wrong file path: %q", filePath)
	}
}

func TestVerifySavesFailedTestLog(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GOWORK", "off")
	project := t.TempDir()
	writeCLIFile(t, filepath.Join(project, "go.mod"), "module example.test/cli\n\ngo 1.25.0\n")
	writeCLIFile(t, filepath.Join(project, "fixture_test.go"),
		"package cli\nimport \"testing\"\nfunc TestFixture(t *testing.T) { t.Fatal(\"test log fixture\") }\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"verify", "--dir", project, "--record"}, &stdout, &stderr); code != 1 {
		t.Fatalf("verify exit code = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	path, record := readCLIReport(t)
	if record.Command != "verify" || record.Status != "failed" || record.Measurements["exit_code"] != float64(1) {
		t.Fatalf("unexpected verify report: %+v", record)
	}
	log, err := os.ReadFile(filepath.Join(filepath.Dir(path), testLogName))
	if err != nil || !bytes.Contains(log, []byte("test log fixture")) {
		t.Fatalf("test log = %q, error = %v", log, err)
	}
}

func TestRunMeasurementsIncludeModelAndVerification(t *testing.T) {
	candidate := "package sample"
	measurements := runMeasurements("Fix", orchestrator.RunResult{
		Directory: "project",
		File:      "calc.go",
		Prompt:    "Fix calc.go",
		Candidate: &candidate,
		Generation: ollama.Result{
			Model: "fixture", InputTokens: 30, OutputTokens: 5, Duration: 10 * time.Millisecond,
		},
		Verification: &orchestrator.VerificationResult{ExitCode: 1, Duration: 20 * time.Millisecond},
	})
	for key, value := range map[string]any{
		"candidate": candidate, "prompt": "Fix calc.go", "model": "fixture", "input_tokens": 30,
		"model_duration_ms": int64(10), "test_duration_ms": int64(20), "exit_code": 1, "test_log": testLogName,
	} {
		if measurements[key] != value {
			t.Errorf("%s = %v, want %v", key, measurements[key], value)
		}
	}
}

type cliReport struct {
	Command      string         `json:"command"`
	Status       string         `json:"status"`
	Measurements map[string]any `json:"measurements"`
}

func readCLIReport(t *testing.T) (string, cliReport) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("experiments", "*", "run.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("reports = %v, error = %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var record cliReport
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return paths[0], record
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
