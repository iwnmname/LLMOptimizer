package orchestrator

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyProjectSkipsGitAndTheDestination(t *testing.T) {
	project := t.TempDir()
	git := filepath.Join(project, ".git")
	if err := os.Mkdir(git, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(git, "config"), "git metadata")
	writeFile(t, filepath.Join(project, "source.go"), "current source")
	view, err := os.MkdirTemp(project, "local-copy-")
	if err != nil {
		t.Fatal(err)
	}

	if err := copyProject(project, view); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(view, "source.go")); err != nil || string(got) != "current source" {
		t.Fatalf("source = %q, error = %v", got, err)
	}
	for _, relative := range []string{".git", filepath.Base(view)} {
		if _, err := os.Lstat(filepath.Join(view, relative)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("copied excluded path %s: %v", relative, err)
		}
	}
}

func TestCopyProjectPreservesRelativeSymlinks(t *testing.T) {
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "source.txt"), "fixture")
	link := filepath.Join(project, "link.txt")
	if err := os.Symlink("source.txt", link); err != nil {
		t.Fatal(err)
	}

	view := t.TempDir()
	if err := copyProject(project, view); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(view, "link.txt")); err != nil || got != "source.txt" {
		t.Fatalf("link = %q, error = %v", got, err)
	}
}

func TestWithWorkspaceRemovesCopyAfterFailure(t *testing.T) {
	project, scratch := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", scratch)
	expected := errors.New("verification interrupted")
	err := withWorkspace(project, func(path string) error {
		writeFile(t, filepath.Join(path, "temporary.txt"), "temporary")
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("workspace error = %v", err)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary copy remains: %v, %v", entries, err)
	}
}
