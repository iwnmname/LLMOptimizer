package indexer

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func parseSource(root, filename string) (string, File, error) {
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(filename)))
	if err != nil {
		return "", File{}, err
	}
	positions := token.NewFileSet()
	parsed, err := parser.ParseFile(positions, filename, source, parser.SkipObjectResolution)
	if err != nil {
		return "", File{}, err
	}

	file := File{Path: filename, Test: strings.HasSuffix(filename, "_test.go")}
	for _, imported := range parsed.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return "", File{}, err
		}
		file.Imports = append(file.Imports, name)
	}
	for _, declaration := range parsed.Decls {
		switch decl := declaration.(type) {
		case *ast.FuncDecl:
			symbol, err := functionSymbol(positions, decl)
			if err != nil {
				return "", File{}, err
			}
			file.Symbols = append(file.Symbols, symbol)
		case *ast.GenDecl:
			if decl.Tok != token.TYPE {
				continue
			}
			for _, spec := range decl.Specs {
				typ := spec.(*ast.TypeSpec)
				signature, err := formatNode(positions, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{typ}})
				if err != nil {
					return "", File{}, err
				}
				file.Symbols = append(file.Symbols, Symbol{
					Name:      typ.Name.Name,
					Kind:      "type",
					Signature: signature,
					StartLine: positions.PositionFor(typ.Pos(), false).Line,
					EndLine:   positions.PositionFor(typ.End(), false).Line,
				})
			}
		}
	}
	return parsed.Name.Name, file, nil
}

func functionSymbol(positions *token.FileSet, decl *ast.FuncDecl) (Symbol, error) {
	signature, err := formatNode(positions, &ast.FuncDecl{Name: decl.Name, Recv: decl.Recv, Type: decl.Type})
	if err != nil {
		return Symbol{}, err
	}
	symbol := Symbol{
		Name:      decl.Name.Name,
		Kind:      "function",
		Signature: signature,
		StartLine: positions.PositionFor(decl.Pos(), false).Line,
		EndLine:   positions.PositionFor(decl.End(), false).Line,
	}
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		symbol.Kind = "method"
		symbol.Receiver, err = formatNode(positions, decl.Recv.List[0].Type)
	}
	return symbol, err
}

func formatNode(positions *token.FileSet, node ast.Node) (string, error) {
	var text bytes.Buffer
	if err := format.Node(&text, positions, node); err != nil {
		return "", err
	}
	return strings.TrimSpace(text.String()), nil
}
