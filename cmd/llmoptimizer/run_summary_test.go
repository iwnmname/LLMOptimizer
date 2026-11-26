package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/contextmanager"
	"github.com/iwnmname/LLMOptimizer/internal/ollama"
	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
	"github.com/iwnmname/LLMOptimizer/internal/router"
)

func TestRunSummaryIncludesModelUsageAndTestResult(t *testing.T) {
	result := orchestrator.RunResult{
		File: "calc.go",
		Context: &orchestrator.ContextResult{
			Route:    router.Choose("Исправь Add", router.ContextStats{}, ""),
			Planning: ollama.Result{Model: router.LightModel, InputTokens: 20, OutputTokens: 4, Duration: time.Millisecond},
			ExpandedSymbols: []contextmanager.Reference{
				{ID: 2, Path: "mathutil/helper.go", Name: "Sum", StartLine: 5, EndLine: 8},
				{ID: 3, Path: "format/display.go", Name: "Display", StartLine: 3, EndLine: 6},
			},
		},
		Route:        router.Choose("Исправь Add", router.ContextStats{SourceBytes: 7000, ExternalPackages: 2}, ""),
		Generation:   ollama.Result{Model: ollama.DefaultModel, InputTokens: 30, OutputTokens: 6, Duration: 2 * time.Millisecond},
		Verification: &orchestrator.VerificationResult{ExitCode: 1, Duration: time.Millisecond},
	}
	measurements := runMeasurements("Исправь Add", result)
	routing, ok := measurements["routing"].(map[string]any)
	if !ok || routing["context"] != result.Context.Route || routing["generation"] != result.Route {
		t.Fatalf("routing measurements = %v", measurements["routing"])
	}
	generation, ok := measurements["generation"].(map[string]any)
	if !ok || generation["model"] != ollama.DefaultModel || generation["input_tokens"] != 30 ||
		measurements["input_tokens"] != 50 {
		t.Fatalf("generation measurements = %v", measurements["generation"])
	}
	text := renderRunSummary("Исправь Add", result, nil)
	for _, want := range []string{
		"Исправь Add",
		"`calc.go`",
		"| Выбор контекста | " + router.LightModel + " | 20 | 4 | 1ms |",
		"| Генерация правки | " + ollama.DefaultModel + " | 30 | 6 | 2ms |",
		"| Всего |  | 50 | 10 | 3ms |",
		"context spans 2 other package directories, 7000 байт кода",
		"`Sum` в `mathutil/helper.go`, строки 5-8",
		"`go test` завершился с кодом `1`",
		"(run.json)",
		"(tests.log)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("summary missing %q:\n%s", want, text)
		}
	}
}

func TestRunSummaryRecordsStartupErrors(t *testing.T) {
	text := renderRunSummary("Исправь Add", orchestrator.RunResult{}, errors.New("Ollama недоступна"))
	if !strings.Contains(text, "Ollama недоступна") || strings.Contains(text, "`go test` завершился") {
		t.Fatalf("unexpected error summary:\n%s", text)
	}
}
