package experiment

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartCreatesUniqueReadableDirectories(t *testing.T) {
	t.Chdir(t.TempDir())
	first, err := Start("run")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if first.Directory == second.Directory {
		t.Fatal("two runs share a directory")
	}
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []*Run{first, second} {
		prefix := run.startedAt.Local().Format("2006-01-02_15-04-05") + "_run-"
		if filepath.Dir(run.Directory) != filepath.Join(current, "experiments") ||
			!strings.HasPrefix(filepath.Base(run.Directory), prefix) {
			t.Fatalf("unexpected run directory: %s", run.Directory)
		}
	}
}

func TestSaveRecordsMeasurementsAndErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, runErr := range []error{nil, errors.New("verification unavailable")} {
		run, err := Start("ask")
		if err != nil {
			t.Fatal(err)
		}
		path, err := run.Save("success", map[string]any{"input_tokens": 12}, runErr)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record report
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != "run.json" || record.Command != "ask" ||
			!record.StartedAt.Equal(run.startedAt) || record.Measurements["input_tokens"] != float64(12) {
			t.Fatalf("unexpected report: %+v", record)
		}
		if runErr == nil && (record.Status != "success" || record.Error != "") {
			t.Fatalf("unexpected successful status: %+v", record)
		}
		if runErr != nil && (record.Status != "error" || record.Error != runErr.Error()) {
			t.Fatalf("unexpected error status: %+v", record)
		}
	}
}

func TestSaveDoesNotLeavePartialReport(t *testing.T) {
	t.Chdir(t.TempDir())
	run, err := Start("ask")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.Save("success", map[string]any{"invalid": make(chan int)}, nil); err == nil {
		t.Fatal("invalid JSON data accepted")
	}
	files, err := os.ReadDir(run.Directory)
	if err != nil || len(files) != 0 {
		t.Fatalf("incomplete report remains: %v, %v", files, err)
	}
}
