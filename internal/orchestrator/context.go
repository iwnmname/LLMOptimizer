package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/iwnmname/LLMOptimizer/internal/contextmanager"
	"github.com/iwnmname/LLMOptimizer/internal/indexer"
	"github.com/iwnmname/LLMOptimizer/internal/ollama"
)

type modelCall func(context.Context, string, json.RawMessage) (ollama.Result, error)

type ContextResult struct {
	Map              string
	Prompt           string
	Planning         ollama.Result
	RequestedSymbols []int
	AutomaticSymbols []int
	ExpandedSymbols  []contextmanager.Reference
}

func selectContext(ctx context.Context, workspace string, sources fs.FS, task, description string,
	askModel modelCall, result *ContextResult,
) (string, []contextmanager.Fragment, error) {
	index, err := indexer.Build(workspace)
	if err != nil {
		return "", nil, fmt.Errorf("build repository index: %w", err)
	}
	selection := contextmanager.Select(index, task)
	if len(selection.Files) == 0 {
		return "", nil, errors.New("repository map has no Go source files")
	}

	result.Map = selection.Text
	result.Prompt = fmt.Sprintf("Select one file to edit from the repository map. Return its exact path in file. "+
		"Request the IDs of symbols from other files whose implementations are needed, up to %d in symbols. "+
		"Return an empty symbols array if the selected file is enough.\n\n%s\n\nRepository map\n%s",
		contextmanager.MaxFragments, description, result.Map)
	format, err := contextPlanFormat(selection.Files)
	if err != nil {
		return "", nil, err
	}
	result.Planning, err = askModel(ctx, result.Prompt, format)
	if err != nil {
		return "", nil, fmt.Errorf("select repository context: %w", err)
	}

	var plan struct {
		File    string `json:"file"`
		Symbols []int  `json:"symbols"`
	}
	if err := json.Unmarshal([]byte(result.Planning.Text), &plan); err != nil {
		return "", nil, fmt.Errorf("decode context plan: %w", err)
	}
	result.RequestedSymbols = plan.Symbols
	result.AutomaticSymbols = selection.ReferencedSymbols(task, plan.File, plan.Symbols)
	ids := slices.Concat(plan.Symbols, result.AutomaticSymbols)
	fragments, err := selection.Reveal(sources, plan.File, ids)
	if err != nil {
		return "", nil, fmt.Errorf("expand repository context: %w", err)
	}
	for _, fragment := range fragments {
		result.ExpandedSymbols = append(result.ExpandedSymbols, fragment.Reference)
	}
	return plan.File, fragments, nil
}

func contextPlanFormat(files []string) (json.RawMessage, error) {
	choices, err := json.Marshal(files)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"file":{"type":"string","enum":%s},"symbols":{"type":"array","items":{"type":"integer"},"maxItems":%d}},"required":["file","symbols"],"additionalProperties":false}`,
		choices, contextmanager.MaxFragments)), nil
}

func taskDescription(task, instructions string) string {
	description := "Task\n" + task
	if instructions != "" {
		description += "\n\nProject instructions\n" + instructions
	}
	return description
}
