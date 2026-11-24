package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/iwnmname/LLMOptimizer/internal/experiment"
)

const testLogName = "tests.log"

func startTestExperiment(enabled bool, command string) (*experiment.Run, *os.File, error) {
	if !enabled {
		return nil, nil, nil
	}
	run, err := experiment.Start(command)
	if err != nil {
		return nil, nil, err
	}
	log, err := os.Create(filepath.Join(run.Directory, testLogName))
	if err != nil {
		return nil, nil, fmt.Errorf("create test log: %w", err)
	}
	return run, log, nil
}

func recordExperiment(run *experiment.Run, status string, measurements map[string]any, runErr error, stdout, stderr io.Writer) bool {
	path, err := run.Save(status, measurements, runErr)
	if err != nil {
		fmt.Fprintf(stderr, "Cannot record experiment: %v\n", err)
		return false
	}
	fmt.Fprintf(stdout, "Experiment: %s\n", path)
	return true
}
