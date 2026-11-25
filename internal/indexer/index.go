package indexer

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

type Index struct {
	Root        string       `json:"root"`
	Modules     []Module     `json:"modules"`
	Packages    []Package    `json:"packages"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type Module struct {
	Path      string `json:"path"`
	Directory string `json:"directory"`
}

type Package struct {
	Name         string       `json:"name"`
	Directory    string       `json:"directory"`
	ImportPath   string       `json:"import_path,omitempty"`
	Files        []File       `json:"files"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

type File struct {
	Path    string   `json:"path"`
	Test    bool     `json:"test,omitempty"`
	Imports []string `json:"imports,omitempty"`
	Symbols []Symbol `json:"symbols,omitempty"`
}

type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Dependency struct {
	ImportPath     string `json:"import_path"`
	LocalDirectory string `json:"local_directory,omitempty"`
}

type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type packageKey struct {
	directory string
	name      string
}

func Build(directory string) (Index, error) {
	if directory == "" {
		return Index{}, errors.New("project directory is required")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return Index{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Index{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Index{}, err
	}
	if !info.IsDir() {
		return Index{}, fmt.Errorf("%q must be a directory", root)
	}

	moduleFiles, sourceFiles, err := discoverFiles(root)
	if err != nil {
		return Index{}, err
	}

	index := Index{Root: root, Modules: []Module{}, Packages: []Package{}}
	modules := make(map[string]string)
	for _, file := range moduleFiles {
		directory := path.Dir(file)
		module, err := readModule(root, file)
		modules[directory] = module
		if err != nil {
			index.Diagnostics = append(index.Diagnostics, Diagnostic{Path: file, Message: err.Error()})
			continue
		}
		index.Modules = append(index.Modules, Module{Path: module, Directory: directory})
	}

	packages := make(map[packageKey]*Package)
	for _, source := range sourceFiles {
		name, file, err := parseSource(root, source)
		if err != nil {
			index.Diagnostics = append(index.Diagnostics, Diagnostic{Path: source, Message: err.Error()})
			continue
		}
		key := packageKey{directory: path.Dir(source), name: name}
		pkg := packages[key]
		if pkg == nil {
			pkg = &Package{
				Name:       name,
				Directory:  key.directory,
				ImportPath: packageImportPath(key.directory, modules),
			}
			packages[key] = pkg
		}
		pkg.Files = append(pkg.Files, file)
	}

	for _, pkg := range packages {
		index.Packages = append(index.Packages, *pkg)
	}
	slices.SortFunc(index.Packages, func(left, right Package) int {
		if left.Directory != right.Directory {
			return cmp.Compare(left.Directory, right.Directory)
		}
		return cmp.Compare(left.Name, right.Name)
	})
	linkDependencies(index.Packages)
	return index, nil
}

func discoverFiles(root string) (modules, sources []string, err error) {
	err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if filename != root && (entry.Name() == ".git" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || (entry.Name() != "go.mod" && !strings.HasSuffix(entry.Name(), ".go")) {
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		if entry.Name() == "go.mod" {
			modules = append(modules, filepath.ToSlash(relative))
		} else {
			sources = append(sources, filepath.ToSlash(relative))
		}
		return nil
	})
	return modules, sources, err
}

func readModule(root, file string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		return "", err
	}
	module := modfile.ModulePath(data)
	if module == "" {
		return "", errors.New("missing or invalid module path")
	}
	return module, nil
}

func packageImportPath(directory string, modules map[string]string) string {
	for current := directory; ; current = path.Dir(current) {
		if module, ok := modules[current]; ok {
			if module == "" || directory == current {
				return module
			}
			if current == "." {
				return path.Join(module, directory)
			}
			return path.Join(module, strings.TrimPrefix(directory, current+"/"))
		}
		if current == "." {
			return ""
		}
	}
}

func linkDependencies(packages []Package) {
	local := make(map[string]string)
	for i := range packages {
		pkg := &packages[i]
		if pkg.ImportPath == "" {
			continue
		}
		hasSource := slices.ContainsFunc(pkg.Files, func(file File) bool { return !file.Test })
		if !hasSource {
			if strings.HasSuffix(pkg.Name, "_test") {
				pkg.ImportPath += "_test"
			}
			continue
		}
		if other, exists := local[pkg.ImportPath]; exists && other != pkg.Directory {
			local[pkg.ImportPath] = ""
		} else {
			local[pkg.ImportPath] = pkg.Directory
		}
	}

	for i := range packages {
		pkg := &packages[i]
		imports := make(map[string]struct{})
		for _, file := range pkg.Files {
			for _, dependency := range file.Imports {
				imports[dependency] = struct{}{}
			}
		}
		for _, dependency := range slices.Sorted(maps.Keys(imports)) {
			pkg.Dependencies = append(pkg.Dependencies, Dependency{
				ImportPath:     dependency,
				LocalDirectory: local[dependency],
			})
		}
	}
}
