package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexWritesTheSameJSONToFile(t *testing.T) {
	project := t.TempDir()
	writeCLIFile(t, filepath.Join(project, "app.go"), "package sample\nfunc Add(a, b int) int { return a + b }\n")
	var terminal, stderr bytes.Buffer
	if code := run([]string{"index", "--dir", project}, &terminal, &stderr); code != 0 {
		t.Fatalf("terminal index failed: %s", stderr.String())
	}

	path := filepath.Join(t.TempDir(), "index.json")
	writeCLIFile(t, path, "previous index")
	var confirmation bytes.Buffer
	if code := run([]string{"index", "--dir", project, "--output", path}, &confirmation, &stderr); code != 0 {
		t.Fatalf("saved index failed: %s", stderr.String())
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, terminal.Bytes()) {
		t.Fatalf("saved index differs from terminal output: %s", saved)
	}
	if confirmation.String() != "Index saved to "+path+"\n" {
		t.Fatalf("unexpected output: %q", confirmation.String())
	}
}
