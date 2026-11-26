package router

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
)

const LightModel = "qwen3.5:4b-q4_K_M"
const strongExternalPackages = 2

type ContextStats struct {
	SourceBytes      int `json:"source_bytes"`
	ExternalPackages int `json:"external_packages"`
}

type Decision struct {
	Model  string `json:"model"`
	Tier   string `json:"tier"`
	Reason string `json:"reason"`
	ContextStats
}

func Choose(task string, stats ContextStats, override string) Decision {
	if model := strings.TrimSpace(override); model != "" {
		return Decision{Model: model, Tier: "fixed", Reason: "model set with --model", ContextStats: stats}
	}

	words := strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	for _, word := range words {
		switch word {
		case "architecture", "refactor", "refactoring", "migration", "migrate",
			"concurrency", "concurrent", "goroutine", "goroutines", "deadlock",
			"архитектура", "архитектуру", "рефакторинг", "миграция", "миграцию",
			"конкурентность", "параллельность", "горутина", "горутины", "дедлок":
			return Decision{Model: ollama.DefaultModel, Tier: "strong",
				Reason: fmt.Sprintf("task keyword %q", word), ContextStats: stats}
		}
	}
	if stats.ExternalPackages >= strongExternalPackages {
		return Decision{Model: ollama.DefaultModel, Tier: "strong",
			Reason: fmt.Sprintf("context spans %d other package directories", stats.ExternalPackages), ContextStats: stats}
	}
	return Decision{Model: LightModel, Tier: "light", Reason: "no complexity rule matched", ContextStats: stats}
}
