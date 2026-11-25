package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildIndexesSymbolsAndLinksImportsAcrossModules(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":            "module example.test/root\n",
		"extras/go.mod":     "module \"example.test/feature\"\n",
		"extras/lib/lib.go": "package lib\nfunc Label() string { return \"feature\" }\n",
		"internal/store/store.go": `package store

type Store struct {
	name string
}

func (s *Store) Load() string {
	return s.name
}

func Open[T any](value T) T { return value }
`,
		"internal/store/store_test.go": "package store_test\nimport \"example.test/root/internal/store\"\n",
		"cmd/app/main.go": `package main

import (
	"fmt"
	"example.test/root/internal/store"
	"example.test/feature/lib"
)

func main() {}
`,
		"cmd/app/other.go": "package main\nimport \"fmt\"\n",
	} {
		writeFixture(t, root, name, content)
	}

	index, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	wantModules := []Module{{Path: "example.test/feature", Directory: "extras"}, {Path: "example.test/root", Directory: "."}}
	if !reflect.DeepEqual(index.Modules, wantModules) || len(index.Diagnostics) != 0 {
		t.Fatalf("modules = %+v, diagnostics = %+v", index.Modules, index.Diagnostics)
	}
	app := findPackage(t, index, "cmd/app", "main")
	wantDependencies := []Dependency{
		{ImportPath: "example.test/feature/lib", LocalDirectory: "extras/lib"},
		{ImportPath: "example.test/root/internal/store", LocalDirectory: "internal/store"},
		{ImportPath: "fmt"},
	}
	if app.ImportPath != "example.test/root/cmd/app" || !reflect.DeepEqual(app.Dependencies, wantDependencies) {
		t.Fatalf("application package = %+v", app)
	}

	pkg := findPackage(t, index, "internal/store", "store")
	if len(pkg.Files) != 1 || pkg.Files[0].Path != "internal/store/store.go" || len(pkg.Files[0].Symbols) != 3 {
		t.Fatalf("store files = %+v", pkg.Files)
	}
	symbols := pkg.Files[0].Symbols
	if typ := symbols[0]; typ.Kind != "type" || typ.Name != "Store" || typ.StartLine != 3 || typ.EndLine != 5 ||
		!strings.Contains(typ.Signature, "type Store struct") {
		t.Fatalf("type = %+v", typ)
	}
	wantMethod := Symbol{Name: "Load", Kind: "method", Receiver: "*Store", Signature: "func (s *Store) Load() string",
		StartLine: 7, EndLine: 9}
	if symbols[1] != wantMethod {
		t.Fatalf("method = %+v, want %+v", symbols[1], wantMethod)
	}
	if function := symbols[2]; function.Kind != "function" || function.Signature != "func Open[T any](value T) T" {
		t.Fatalf("function = %+v", function)
	}

	externalTest := findPackage(t, index, "internal/store", "store_test")
	if externalTest.ImportPath != "example.test/root/internal/store_test" || !externalTest.Files[0].Test ||
		len(externalTest.Dependencies) != 1 || externalTest.Dependencies[0].LocalDirectory != "internal/store" {
		t.Fatalf("external test package = %+v", externalTest)
	}
}

func TestBuildRecoversFromInvalidSourceAndSkipsDependencyTrees(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"app.go":                       "package app\nfunc Works() {}\n",
		"broken.go":                    "package app\nfunc Bad(\n",
		".git/hidden.go":               "package hidden\n",
		"vendor/dependency/ignored.go": "package dependency\n",
		"nested/go.mod":                "go 1.25.0\n",
		"nested/child.go":              "package child\n",
	} {
		writeFixture(t, root, name, content)
	}
	index, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Packages) != 2 || len(index.Modules) != 0 || len(index.Diagnostics) != 2 ||
		index.Diagnostics[0].Path != "nested/go.mod" || index.Diagnostics[1].Path != "broken.go" {
		t.Fatalf("unexpected index: %+v", index)
	}
	pkg := findPackage(t, index, ".", "app")
	if pkg.ImportPath != "" || len(pkg.Files) != 1 || pkg.Files[0].Symbols[0].Name != "Works" {
		t.Fatalf("package outside a module = %+v", pkg)
	}
}

func findPackage(t *testing.T, index Index, directory, name string) Package {
	t.Helper()
	for _, pkg := range index.Packages {
		if pkg.Directory == directory && pkg.Name == name {
			return pkg
		}
	}
	t.Fatalf("package %s in %s not indexed", name, directory)
	return Package{}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
