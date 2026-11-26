package orchestrator

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

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
)

const originalSource = "package sample\n\nfunc Add(a, b int) int { return a - b }\n"

func TestRunVerifiesProposalInWorkspace(t *testing.T) {
	cases := []struct {
		name     string
		operator string
		exitCode int
	}{
		{name: "passes", operator: "+", exitCode: 0},
		{name: "test fails", operator: "*", exitCode: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, scratch := createTestProject(t)
			replacement := strings.Replace(originalSource, "a - b", "a "+tc.operator+" b", 1)
			payload, err := json.Marshal(map[string]string{"content": replacement})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			generate := func(_ context.Context, model, prompt string, format json.RawMessage) (ollama.Result, error) {
				calls++
				if model != "fixture:fixed" {
					t.Fatalf("generation model = %q", model)
				}
				for _, want := range []string{"Fix Add", "calc.go", originalSource, "Keep the function signature."} {
					if !strings.Contains(prompt, want) {
						t.Errorf("prompt missing %q: %s", want, prompt)
					}
				}
				if !json.Valid(format) || !bytes.Contains(format, []byte(`"content"`)) {
					t.Errorf("invalid file response format: %s", format)
				}
				return ollama.Result{Model: "fixture", Text: string(payload), InputTokens: 17, OutputTokens: 9}, nil
			}

			var output bytes.Buffer
			result, err := runWithModel(t.Context(), RunInput{Directory: project, File: "calc.go", Task: "Fix Add", Model: "fixture:fixed"},
				&output, io.Discard, generate)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || result.Candidate == nil || *result.Candidate != replacement {
				t.Fatalf("unexpected generation: %+v (%d requests)", result, calls)
			}
			if result.Generation.InputTokens != 17 || result.Generation.OutputTokens != 9 {
				t.Fatalf("unexpected token counts: %+v", result.Generation)
			}
			if result.Verification == nil || result.Verification.ExitCode != tc.exitCode {
				t.Fatalf("verification = %+v, want exit code %d\n%s", result.Verification, tc.exitCode, output.String())
			}
			if tc.exitCode != 0 && !strings.Contains(output.String(), "unexpected sum") {
				t.Fatalf("missing failed test output: %s", output.String())
			}
			checkTestProjectUnchanged(t, project, scratch, originalSource)
		})
	}
}

func TestRunRejectsInvalidChanges(t *testing.T) {
	cases := []struct {
		name      string
		file      string
		response  string
		modelErr  error
		wantError string
	}{
		{name: "outside file", file: "../calc.go", wantError: "inside the project directory"},
		{name: "missing file", file: "missing.go", wantError: "read selected file"},
		{name: "malformed response", file: "calc.go", response: "not JSON", wantError: "decode proposed file"},
		{name: "missing content", file: "calc.go", response: "{}", wantError: "missing file content"},
		{name: "model error", file: "calc.go", modelErr: errors.New("model unavailable"), wantError: "model unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, scratch := createTestProject(t)
			called := false
			generate := func(context.Context, string, string, json.RawMessage) (ollama.Result, error) {
				called = true
				return ollama.Result{Model: "fixture", Text: tc.response}, tc.modelErr
			}
			result, err := runWithModel(t.Context(), RunInput{Directory: project, File: tc.file, Task: "Fix Add"},
				io.Discard, io.Discard, generate)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if called && tc.file != "calc.go" {
				t.Fatal("model called for invalid source path")
			}
			if result.Candidate != nil || result.Verification != nil {
				t.Fatalf("invalid change was applied: %+v", result)
			}
			checkTestProjectUnchanged(t, project, scratch, originalSource)
		})
	}
}

func TestVerifyReportsTheOriginalTestFailure(t *testing.T) {
	project, scratch := createTestProject(t)
	var output bytes.Buffer
	result, err := Verify(t.Context(), project, &output, io.Discard)
	if err != nil || result.ExitCode != 1 || !strings.Contains(output.String(), "unexpected sum") {
		t.Fatalf("result = %+v, error = %v\n%s", result, err, output.String())
	}
	checkTestProjectUnchanged(t, project, scratch, originalSource)
}

func createTestProject(t *testing.T) (project, scratch string) {
	t.Helper()
	project, scratch = t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", scratch)
	t.Setenv("GOWORK", "off")
	writeFile(t, filepath.Join(project, "go.mod"), "module example.test/fixture\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(project, "calc.go"), originalSource)
	writeFile(t, filepath.Join(project, "AGENTS.md"), "Keep the function signature.\n")
	writeFile(t, filepath.Join(project, "calc_test.go"), `package sample

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if err := os.WriteFile("test-marker", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if Add(2, 3) != 5 || Add(6, 7) != 13 {
		t.Fatal("unexpected sum")
	}
}
`)
	return project, scratch
}

func checkTestProjectUnchanged(t *testing.T, project, scratch, expectedSource string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(project, "calc.go"))
	if err != nil || string(got) != expectedSource {
		t.Fatalf("original source changed: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(project, "test-marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("test wrote to the original project: %v", err)
	}
	dirs, err := filepath.Glob(filepath.Join(scratch, "llmoptimizer-*"))
	if err != nil || len(dirs) != 0 {
		t.Fatalf("temporary workspaces remain: %v, %v", dirs, err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
