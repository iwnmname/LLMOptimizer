package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/contextmanager"
	"github.com/iwnmname/LLMOptimizer/internal/ollama"
	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

func TestRunSummaryIncludesModelUsageAndTestResult(t *testing.T) {
	result := orchestrator.RunResult{
		File: "calc.go",
		Context: &orchestrator.ContextResult{
			Planning: ollama.Result{Model: "fixture", InputTokens: 20, OutputTokens: 4, Duration: time.Millisecond},
			ExpandedSymbols: []contextmanager.Reference{
				{ID: 2, Path: "helper.go", Name: "Sum", StartLine: 5, EndLine: 8},
			},
		},
		Generation:   ollama.Result{Model: "fixture", InputTokens: 30, OutputTokens: 6, Duration: 2 * time.Millisecond},
		Verification: &orchestrator.VerificationResult{ExitCode: 1, Duration: time.Millisecond},
	}
	text := renderRunSummary("Исправь Add", result, nil)
	for _, want := range []string{
		"Исправь Add",
		"`calc.go`",
		"| Выбор контекста | 20 | 4 | 1ms |",
		"| Генерация правки | 30 | 6 | 2ms |",
		"| Всего | 50 | 10 | 3ms |",
		"`Sum` в `helper.go`, строки 5-8",
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
