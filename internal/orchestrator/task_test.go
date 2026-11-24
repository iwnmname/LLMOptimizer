package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareForPlainDirectory(t *testing.T) {
	project := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(project))
	instructions := filepath.Join(project, "AGENTS.md")
	writeFile(t, instructions, "Project fixture")

	task, err := Prepare(t.Context(), project, "  Add tests  ")
	if err != nil {
		t.Fatal(err)
	}
	want := PreparedTask{Description: "Add tests", ProjectRoot: project, InstructionsPath: instructions}
	if task != want {
		t.Fatalf("prepared task = %+v, want %+v", task, want)
	}
	if _, err := Prepare(t.Context(), project, "  "); err == nil {
		t.Fatal("empty task accepted")
	}
}

func TestPrepareForGitDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q", "--template=")
	empty, err := Prepare(t.Context(), repo, "Add tests")
	if err != nil || empty.HeadCommit != "" {
		t.Fatalf("empty repo = %+v, error = %v", empty, err)
	}
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
		"commit", "--allow-empty", "--no-gpg-sign", "-qm", "fixture")
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	task, err := Prepare(t.Context(), nested, "Add tests")
	if err != nil || task.ProjectRoot != root || task.HeadCommit != git("rev-parse", "HEAD") {
		t.Fatalf("prepared task = %+v, error = %v", task, err)
	}
}
