package orchestrator

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
	"github.com/iwnmname/LLMOptimizer/internal/router"
)

func TestRunSelectsAndExpandsContextBeforeGenerating(t *testing.T) {
	project, scratch := createTestProject(t)
	const helper = "package sample\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n"
	writeFile(t, filepath.Join(project, "sum.go"), helper)
	source := originalSource + strings.Repeat("\n", 5999-len(originalSource))
	writeFile(t, filepath.Join(project, "calc.go"), source)
	requests := 0
	var selectedID int
	ask := func(_ context.Context, model, prompt string, format json.RawMessage) (ollama.Result, error) {
		requests++
		switch requests {
		case 1:
			if model != router.LightModel {
				t.Fatalf("planning model = %q, want %q", model, router.LightModel)
			}
			if strings.Contains(prompt, "return a - b") || strings.Contains(prompt, "return a + b") ||
				!strings.Contains(prompt, "Repository map") || !strings.Contains(prompt, "Keep the function signature.") {
				t.Fatalf("unexpected planning prompt: %q", prompt)
			}
			if !strings.Contains(string(format), `"enum"`) || !strings.Contains(string(format), `"calc.go"`) {
				t.Fatalf("missing allowed files: %s", format)
			}
			match := regexp.MustCompile(`\[(\d+)\] func Sum`).FindStringSubmatch(prompt)
			if len(match) != 2 {
				t.Fatalf("helper is missing from the map: %q", prompt)
			}
			var err error
			selectedID, err = strconv.Atoi(match[1])
			if err != nil {
				t.Fatal(err)
			}
			plan, err := json.Marshal(map[string]any{"file": "calc.go", "symbols": []int{selectedID}})
			if err != nil {
				t.Fatal(err)
			}
			return ollama.Result{
				Model: model, Text: string(plan), InputTokens: 50, OutputTokens: 10, Duration: time.Millisecond,
			}, nil
		case 2:
			if model != router.LightModel {
				t.Fatalf("generation model = %q, want %q", model, router.LightModel)
			}
			for _, want := range []string{originalSource, "Additional source sum.go (lines 3-5)", "return a + b"} {
				if !strings.Contains(prompt, want) {
					t.Errorf("generation prompt missing %q: %s", want, prompt)
				}
			}
			if strings.Contains(prompt, "Repository map") {
				t.Error("the map was repeated in the generation prompt")
			}
			return ollama.Result{
				Model: model, Text: `{"content":"package sample\n\nfunc Add(a, b int) int { return Sum(a, b) }\n"}`,
				InputTokens: 70, OutputTokens: 20, Duration: 2 * time.Millisecond,
			}, nil
		default:
			t.Fatalf("unexpected model request %d", requests)
			return ollama.Result{}, nil
		}
	}

	result, err := runWithModel(t.Context(), RunInput{Directory: project, Task: "Fix Add using Sum"},
		io.Discard, io.Discard, ask)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || result.File != "calc.go" || result.Verification == nil || result.Verification.ExitCode != 0 {
		t.Fatalf("unexpected run result: %+v (%d requests)", result, requests)
	}
	context := result.Context
	if context == nil || len(context.RequestedSymbols) != 1 || context.RequestedSymbols[0] != selectedID ||
		len(context.ExpandedSymbols) != 1 || context.ExpandedSymbols[0].Path != "sum.go" {
		t.Fatalf("unexpected context trace: %+v", context)
	}
	if result.Route.SourceBytes <= 6000 || result.Route.ExternalPackages != 0 {
		t.Fatalf("unexpected routing context: %+v", result.Route)
	}
	usage := result.TotalUsage()
	if usage.InputTokens != 120 || usage.OutputTokens != 30 || usage.Duration != 3*time.Millisecond {
		t.Fatalf("unexpected total usage: %+v", usage)
	}
	checkTestProjectUnchanged(t, project, scratch, source)
}

func TestRunUsesStrongModelForContextFromTwoOtherPackageDirectories(t *testing.T) {
	project, scratch := createTestProject(t)
	for name, source := range map[string]string{
		"first":  "package helpers\n\nfunc Sum() {}\nfunc Normalize() {}\n",
		"second": "package helpers\n\nfunc Clamp() {}\n",
	} {
		directory := filepath.Join(project, name)
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(directory, "helpers.go"), source)
	}

	calls := 0
	ask := func(_ context.Context, model, _ string, _ json.RawMessage) (ollama.Result, error) {
		calls++
		if calls == 1 {
			if model != router.LightModel {
				t.Fatalf("planning model = %q, want %q", model, router.LightModel)
			}
			return ollama.Result{Model: model, Text: `{"file":"calc.go","symbols":[]}`}, nil
		}
		if calls != 2 || model != ollama.DefaultModel {
			t.Fatalf("generation model = %q, want %q (%d calls)", model, ollama.DefaultModel, calls)
		}
		return ollama.Result{Model: model, Text: `{"content":"package sample\n\nfunc Add(a, b int) int { return a + b }\n"}`}, nil
	}
	result, err := runWithModel(t.Context(), RunInput{Directory: project, Task: "Fix Add. See Sum, Normalize and Clamp."},
		io.Discard, io.Discard, ask)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.Context == nil || len(result.Context.ExpandedSymbols) != 3 ||
		result.Route.ExternalPackages != 2 || result.Verification == nil || result.Verification.ExitCode != 0 {
		t.Fatalf("unexpected run result: %+v (%d calls)", result, calls)
	}
	checkTestProjectUnchanged(t, project, scratch, originalSource)
}

func TestRunRejectsAPlanOutsideTheMap(t *testing.T) {
	project, scratch := createTestProject(t)
	calls := 0
	ask := func(_ context.Context, model, _ string, _ json.RawMessage) (ollama.Result, error) {
		calls++
		return ollama.Result{Model: model, Text: `{"file":"../outside.go","symbols":[]}`, InputTokens: 18}, nil
	}
	result, err := runWithModel(t.Context(), RunInput{Directory: project, Task: "Fix Add"}, io.Discard, io.Discard, ask)
	if err == nil || !strings.Contains(err.Error(), "outside the repository map") || calls != 1 ||
		result.Candidate != nil || result.TotalUsage().InputTokens != 18 {
		t.Fatalf("invalid plan result = %+v, error = %v (%d calls)", result, err, calls)
	}
	checkTestProjectUnchanged(t, project, scratch, originalSource)
}
