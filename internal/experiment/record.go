package experiment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Run struct {
	Directory string
	command   string
	startedAt time.Time
}

type report struct {
	Command      string         `json:"command"`
	StartedAt    time.Time      `json:"started_at"`
	Status       string         `json:"status"`
	Error        string         `json:"error,omitempty"`
	Measurements map[string]any `json:"measurements"`
}

func Start(command string) (*Run, error) {
	root, err := filepath.Abs("experiments")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	prefix := startedAt.Format("2006-01-02_15-04-05") + "_" + command + "-"
	directory, err := os.MkdirTemp(root, prefix)
	if err != nil {
		return nil, err
	}
	return &Run{Directory: directory, command: command, startedAt: startedAt.UTC()}, nil
}

func (run *Run) Save(status string, measurements map[string]any, runErr error) (string, error) {
	entry := report{
		Command:      run.command,
		StartedAt:    run.startedAt,
		Status:       status,
		Measurements: measurements,
	}
	if runErr != nil {
		entry.Status = "error"
		entry.Error = runErr.Error()
	}

	file, err := os.CreateTemp(run.Directory, ".run-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	writeErr := encoder.Encode(entry)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return "", fmt.Errorf("write run report: %w", err)
	}
	destination := filepath.Join(run.Directory, "run.json")
	if err := os.Rename(file.Name(), destination); err != nil {
		return "", err
	}
	return destination, nil
}
