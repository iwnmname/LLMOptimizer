package router

import (
	"strings"
	"testing"

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
)

func TestChooseModel(t *testing.T) {
	cases := []struct {
		name     string
		task     string
		stats    ContextStats
		override string
		model    string
		tier     string
		reason   string
	}{
		{name: "small change", task: "Fix Add in refactor_helpers.go", stats: ContextStats{SourceBytes: 75},
			model: LightModel, tier: "light", reason: "no complexity rule matched"},
		{name: "keyword", task: "Review CONCURRENCY", model: ollama.DefaultModel, tier: "strong", reason: "concurrency"},
		{name: "Russian keyword", task: "Сделай рефакторинг", model: ollama.DefaultModel, tier: "strong", reason: "рефакторинг"},
		{name: "large source", task: "Fix Add", stats: ContextStats{SourceBytes: 6052},
			model: LightModel, tier: "light", reason: "no complexity rule matched"},
		{name: "one external package", task: "Fix Add", stats: ContextStats{ExternalPackages: 1},
			model: LightModel, tier: "light", reason: "no complexity rule matched"},
		{name: "multiple external packages", task: "Fix Add", stats: ContextStats{SourceBytes: 300, ExternalPackages: 2},
			model: ollama.DefaultModel, tier: "strong", reason: "context spans 2 other package directories"},
		{name: "fixed", task: "Refactor Add", stats: ContextStats{ExternalPackages: 2}, override: " local:fixed ",
			model: "local:fixed", tier: "fixed", reason: "--model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Choose(tc.task, tc.stats, tc.override)
			if got.Model != tc.model || got.Tier != tc.tier || got.ContextStats != tc.stats ||
				!strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("unexpected decision: %+v", got)
			}
		})
	}
}
