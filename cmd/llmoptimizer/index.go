package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/iwnmname/LLMOptimizer/internal/indexer"
)

const indexUsage = "llmoptimizer index [--dir PATH] [--output PATH]"

func indexRepository(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("index", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", ".", "path to the Go project directory")
	path := flags.String("output", "", "path to save the JSON index")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: "+indexUsage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Pass the project directory with --dir.")
		return 2
	}

	index, err := indexer.Build(*directory)
	if err != nil {
		fmt.Fprintf(stderr, "Cannot index project: %v\n", err)
		return 1
	}
	if *path != "" {
		if err := saveIndex(*path, index); err != nil {
			fmt.Fprintf(stderr, "Cannot save index to %q: %v\n", *path, err)
			return 1
		}
		fmt.Fprintf(stdout, "Index saved to %s\n", *path)
		return 0
	}
	if err := writeIndex(stdout, index); err != nil {
		fmt.Fprintf(stderr, "Cannot write index: %v\n", err)
		return 1
	}
	return 0
}

func writeIndex(output io.Writer, index indexer.Index) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(index)
}

func saveIndex(path string, index indexer.Index) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".index-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())

	writeErr := writeIndex(file, index)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
