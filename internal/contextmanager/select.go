package contextmanager

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/iwnmname/LLMOptimizer/internal/indexer"
)

const (
	maxFiles          = 8
	maxSymbolsPerFile = 5
	maxMapBytes       = 6000
	maxSignatureRunes = 200
)

type Selection struct {
	Files   []string
	Symbols []Reference
	Text    string
}

type Reference struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Package   string `json:"package"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type rankedFile struct {
	file         indexer.File
	packageName  string
	dependencies []indexer.Dependency
	symbols      []rankedSymbol
	score        int
}

type rankedSymbol struct {
	symbol indexer.Symbol
	score  int
}

func Select(index indexer.Index, task string) Selection {
	query := words(task)
	var ranked []rankedFile
	for _, pkg := range index.Packages {
		for _, file := range pkg.Files {
			candidate := rankedFile{file: file, packageName: pkg.Name, dependencies: pkg.Dependencies}
			bestSymbol := 0
			for _, symbol := range file.Symbols {
				score := 8*matches(symbol.Name, query) + matches(symbol.Signature, query)
				name := strings.ToLower(symbol.Name)
				for term := range query {
					if len(term) >= 3 && term != name && strings.Contains(name, term) {
						score += 2
					}
				}
				bestSymbol = max(bestSymbol, score)
				candidate.symbols = append(candidate.symbols, rankedSymbol{symbol: symbol, score: score})
			}
			candidate.score = 4*matches(file.Path, query) + bestSymbol
			ranked = append(ranked, candidate)
		}
	}
	slices.SortFunc(ranked, compareFiles)
	if len(ranked) > 0 && ranked[0].score > 0 {
		preferDependencies(ranked)
		slices.SortFunc(ranked, compareFiles)
	}
	return renderSelection(ranked)
}

func preferDependencies(files []rankedFile) {
	primary := files[0]
	directories := make(map[string]struct{})
	for _, dependency := range primary.dependencies {
		if dependency.LocalDirectory != "" && slices.Contains(primary.file.Imports, dependency.ImportPath) {
			directories[dependency.LocalDirectory] = struct{}{}
		}
	}
	for i := 1; i < len(files); i++ {
		if _, ok := directories[path.Dir(files[i].file.Path)]; ok {
			files[i].score++
		}
	}
}

func compareFiles(left, right rankedFile) int {
	if left.score != right.score {
		return cmp.Compare(right.score, left.score)
	}
	if left.file.Test != right.file.Test {
		if left.file.Test {
			return 1
		}
		return -1
	}
	return cmp.Compare(left.file.Path, right.file.Path)
}

func renderSelection(files []rankedFile) Selection {
	var selection Selection
	var text strings.Builder
	for _, file := range files[:min(len(files), maxFiles)] {
		header := fmt.Sprintf("\nFile %s (package %s)\n", file.file.Path, file.packageName)
		if text.Len()+len(header) > maxMapBytes {
			break
		}
		text.WriteString(header)
		selection.Files = append(selection.Files, file.file.Path)
		slices.SortStableFunc(file.symbols, func(left, right rankedSymbol) int {
			return cmp.Compare(right.score, left.score)
		})
		for _, candidate := range file.symbols[:min(len(file.symbols), maxSymbolsPerFile)] {
			symbol := candidate.symbol
			id := len(selection.Symbols) + 1
			line := fmt.Sprintf("[%d] %s (lines %d-%d)\n", id, compactSignature(symbol.Signature),
				symbol.StartLine, symbol.EndLine)
			if text.Len()+len(line) > maxMapBytes {
				break
			}
			text.WriteString(line)
			selection.Symbols = append(selection.Symbols, Reference{
				ID: id, Path: file.file.Path, Name: symbol.Name, Package: file.packageName,
				StartLine: symbol.StartLine, EndLine: symbol.EndLine,
			})
		}
	}
	selection.Text = strings.TrimSpace(text.String())
	return selection
}

func compactSignature(signature string) string {
	runes := []rune(strings.Join(strings.Fields(signature), " "))
	if len(runes) > maxSignatureRunes {
		return string(runes[:maxSignatureRunes]) + "..."
	}
	return string(runes)
}

func matches(text string, query map[string]struct{}) int {
	score := 0
	for term := range words(text) {
		if _, ok := query[term]; ok {
			score++
		}
	}
	return score
}

func words(text string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		result[word] = struct{}{}
	}
	return result
}
