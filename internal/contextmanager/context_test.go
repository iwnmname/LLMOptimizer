package contextmanager

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"github.com/iwnmname/LLMOptimizer/internal/indexer"
)

func TestSelectRanksTaskMatchesAndDirectImports(t *testing.T) {
	index := indexer.Index{Packages: []indexer.Package{
		{Name: "noise", Files: []indexer.File{{Path: "a_noise.go"}}},
		{Name: "handler", Dependencies: []indexer.Dependency{
			{ImportPath: "example.test/session", LocalDirectory: "internal/session"},
		}, Files: []indexer.File{
			{
				Path: "app/handler.go", Imports: []string{"example.test/session"},
				Symbols: []indexer.Symbol{{Name: "HandleLogin", Signature: "func HandleLogin()", StartLine: 3, EndLine: 5}},
			},
		}},
		{Name: "session", Files: []indexer.File{{Path: "internal/session/session.go"}}},
	}}
	selection := Select(index, "Исправь HandleLogin")
	want := []string{"app/handler.go", "internal/session/session.go", "a_noise.go"}
	if !reflect.DeepEqual(selection.Files, want) || !strings.Contains(selection.Text, "[1] func HandleLogin() (lines 3-5)") {
		t.Fatalf("selection = %+v", selection)
	}
	if selection.Symbols[0] != (Reference{ID: 1, Path: "app/handler.go", Name: "HandleLogin", Package: "handler", StartLine: 3, EndLine: 5}) {
		t.Fatalf("reference = %+v", selection.Symbols[0])
	}
}

func TestSelectHonorsMapLimits(t *testing.T) {
	pkg := indexer.Package{Name: "sample"}
	for i := 0; i < maxFiles+4; i++ {
		file := indexer.File{Path: fmt.Sprintf("source%d.go", i)}
		for j := 0; j < maxSymbolsPerFile+2; j++ {
			file.Symbols = append(file.Symbols, indexer.Symbol{
				Name: "Long", Signature: "type Long " + strings.Repeat("д", maxSignatureRunes*2), StartLine: 1, EndLine: 2,
			})
		}
		pkg.Files = append(pkg.Files, file)
	}
	selection := Select(indexer.Index{Packages: []indexer.Package{pkg}}, "Long")
	if len(selection.Files) > maxFiles || len(selection.Text) > maxMapBytes || !utf8.ValidString(selection.Text) {
		t.Fatalf("map exceeds budget: %d files, %d bytes", len(selection.Files), len(selection.Text))
	}
	counts := make(map[string]int)
	for _, symbol := range selection.Symbols {
		counts[symbol.Path]++
		if counts[symbol.Path] > maxSymbolsPerFile {
			t.Fatalf("too many symbols for %s", symbol.Path)
		}
	}
}

func TestReferencedSymbolsFindsNamedHelpers(t *testing.T) {
	selection := Selection{Symbols: []Reference{
		{ID: 1, Path: "target.go", Name: "Add"},
		{ID: 2, Path: "helper.go", Name: "Sum"},
		{ID: 3, Path: "helper.go", Name: "Format"},
	}}
	if got := selection.ReferencedSymbols("Fix Add by calling Sum", "target.go", nil); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("automatic symbols = %v", got)
	}
	if got := selection.ReferencedSymbols("Fix Add by calling Sum", "target.go", []int{2}); len(got) != 0 {
		t.Fatalf("already requested symbol repeated: %v", got)
	}
}

func TestRevealReturnsOriginalLinesWithinBudget(t *testing.T) {
	selection := Selection{
		Files: []string{"target.go", "helper.go", "large.go"},
		Symbols: []Reference{
			{ID: 1, Path: "target.go", StartLine: 1, EndLine: 1},
			{ID: 2, Path: "helper.go", StartLine: 2, EndLine: 4},
			{ID: 3, Path: "large.go", StartLine: 1, EndLine: 1},
		},
	}
	files := fstest.MapFS{
		"helper.go": {Data: []byte("package sample\r\nfunc Help() {\r\n\tprintln(42)\r\n}\r\n")},
		"large.go":  {Data: []byte(strings.Repeat("x", maxFragmentBytes+1))},
	}
	fragments, err := selection.Reveal(files, "target.go", []int{1, 3, 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []Fragment{{Reference: selection.Symbols[1], Source: "func Help() {\r\n\tprintln(42)\r\n}\r\n"}}
	if !reflect.DeepEqual(fragments, want) {
		t.Fatalf("fragments = %+v, want %+v", fragments, want)
	}
	duplicates, err := selection.Reveal(files, "target.go", []int{2, 2})
	if err != nil || len(duplicates) != 1 {
		t.Fatalf("duplicate requests = %v, error = %v", duplicates, err)
	}
	if _, err := selection.Reveal(files, "outside.go", nil); err == nil {
		t.Fatal("accepted a file outside the map")
	}
	if _, err := selection.Reveal(files, "target.go", []int{99}); err == nil {
		t.Fatal("accepted a symbol outside the map")
	}
}
