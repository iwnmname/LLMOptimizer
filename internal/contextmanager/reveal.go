package contextmanager

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

const (
	MaxFragments     = 3
	maxFragmentBytes = 3500
)

type Fragment struct {
	Reference
	Source string
}

func (selection Selection) ReferencedSymbols(task, file string, requested []int) []int {
	query := words(task)
	var additional []int
	for _, reference := range selection.Symbols {
		if len(requested)+len(additional) >= MaxFragments {
			break
		}
		if reference.Path == file || slices.Contains(requested, reference.ID) {
			continue
		}
		if _, ok := query[strings.ToLower(reference.Name)]; ok {
			additional = append(additional, reference.ID)
		}
	}
	return additional
}

func (selection Selection) Reveal(fsys fs.FS, file string, requested []int) ([]Fragment, error) {
	if !slices.Contains(selection.Files, file) {
		return nil, fmt.Errorf("selected file %q is outside the repository map", file)
	}
	if len(requested) > MaxFragments {
		return nil, fmt.Errorf("requested more than %d additional symbols", MaxFragments)
	}
	seen := make(map[int]struct{})
	var fragments []Fragment
	size := 0
	for _, id := range requested {
		position := slices.IndexFunc(selection.Symbols, func(reference Reference) bool { return reference.ID == id })
		if position == -1 {
			return nil, fmt.Errorf("symbol %d is outside the repository map", id)
		}
		reference := selection.Symbols[position]
		if _, ok := seen[id]; ok || reference.Path == file {
			continue
		}
		seen[id] = struct{}{}
		content, err := fs.ReadFile(fsys, reference.Path)
		if err != nil {
			return nil, fmt.Errorf("read symbol %d in %q: %w", id, reference.Path, err)
		}
		lines := strings.SplitAfter(string(content), "\n")
		if reference.StartLine < 1 || reference.EndLine < reference.StartLine || reference.EndLine > len(lines) {
			return nil, fmt.Errorf("invalid line range for symbol %d in %q", id, reference.Path)
		}
		source := strings.Join(lines[reference.StartLine-1:reference.EndLine], "")
		if size+len(source) > maxFragmentBytes {
			continue
		}
		size += len(source)
		fragments = append(fragments, Fragment{Reference: reference, Source: source})
	}
	return fragments, nil
}
