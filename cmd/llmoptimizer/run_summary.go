package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/iwnmname/LLMOptimizer/internal/ollama"
	"github.com/iwnmname/LLMOptimizer/internal/orchestrator"
)

func renderRunSummary(task string, result orchestrator.RunResult, runErr error) string {
	var summary strings.Builder
	summary.WriteString("# Итог прогона\n\n")
	fmt.Fprintf(&summary, "> %s\n\n", strings.ReplaceAll(task, "\n", "\n> "))
	if result.File != "" {
		fmt.Fprintf(&summary, "Выбранный файл `%s`.\n\n", result.File)
	}

	if usage := result.TotalUsage(); usage.Model != "" {
		fmt.Fprintf(&summary, "## Запросы к модели\n\nМодель `%s`.\n\n", usage.Model)
		summary.WriteString("| Запрос | Входные токены | Выходные токены | Время |\n| --- | ---: | ---: | ---: |\n")
		writeRow := func(label string, row ollama.Result) {
			fmt.Fprintf(&summary, "| %s | %d | %d | %s |\n", label, row.InputTokens, row.OutputTokens,
				row.Duration.Round(time.Millisecond))
		}
		if result.Context != nil && result.Context.Planning.Model != "" {
			writeRow("Выбор контекста", result.Context.Planning)
		}
		if result.Generation.Model != "" {
			writeRow("Генерация правки", result.Generation)
		}
		writeRow("Всего", usage)
		summary.WriteString("\n")
	}

	if selected := result.Context; selected != nil {
		summary.WriteString("## Раскрытый контекст\n\n")
		fmt.Fprintf(&summary, "Дополнительных фрагментов раскрыто %d.\n\n", len(selected.ExpandedSymbols))
		for _, fragment := range selected.ExpandedSymbols {
			fmt.Fprintf(&summary, "- `%s` в `%s`, строки %d-%d.\n", fragment.Name, fragment.Path,
				fragment.StartLine, fragment.EndLine)
		}
		if len(selected.ExpandedSymbols) > 0 {
			summary.WriteString("\n")
		}
	}

	summary.WriteString("## Итог проверки\n\n")
	if checked := result.Verification; checked != nil {
		fmt.Fprintf(&summary, "`go test` завершился с кодом `%d` за %s.\n\n", checked.ExitCode,
			checked.Duration.Round(time.Millisecond))
	}
	if runErr != nil {
		fmt.Fprintf(&summary, "Ошибка прогона\n\n> %s\n\n", strings.ReplaceAll(runErr.Error(), "\n", "\n> "))
	}
	summary.WriteString("[Полный отчёт](run.json) · [Вывод Go-тестов](tests.log)\n")
	return summary.String()
}
