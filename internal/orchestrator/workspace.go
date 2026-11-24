package orchestrator

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"unicode/utf8"
)

type projectFiles struct {
	fs.FS
	workspace string
}

func projectDirectory(projectPath string) (string, error) {
	if projectPath == "" {
		return "", errors.New("project directory is required")
	}
	root, err := filepath.Abs(projectPath)
	if err != nil {
		return "", fmt.Errorf("resolve project directory %q: %w", projectPath, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("inspect project directory %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path %q must be a directory", root)
	}
	return root, nil
}

func withWorkspace(project string, use func(string) error) (err error) {
	workspace, err := os.MkdirTemp("", "llmoptimizer-")
	if err != nil {
		return fmt.Errorf("create temporary workspace: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(workspace); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove temporary workspace: %w", cleanupErr))
		}
	}()

	if err := copyProject(project, workspace); err != nil {
		return fmt.Errorf("copy project: %w", err)
	}
	return use(workspace)
}

func copyProject(project, workspace string) error {
	sourcePath, err := filepath.EvalSymlinks(project)
	if err != nil {
		return err
	}
	destinationPath, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return err
	}
	source, err := os.OpenRoot(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	nestedWorkspace, _ := filepath.Rel(sourcePath, destinationPath)
	return os.CopyFS(destinationPath, projectFiles{
		FS:        source.FS(),
		workspace: filepath.ToSlash(nestedWorkspace),
	})
}

func (files projectFiles) ReadDir(directory string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(files.FS, directory)
	return slices.DeleteFunc(entries, func(entry fs.DirEntry) bool {
		return entry.Name() == ".git" || path.Join(directory, entry.Name()) == files.workspace
	}), err
}

func (files projectFiles) ReadLink(name string) (string, error) {
	target, err := fs.ReadLink(files.FS, name)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(filepath.FromSlash(name))
	if filepath.IsAbs(target) || !filepath.IsLocal(filepath.Join(parent, target)) {
		return "", fmt.Errorf("symbolic link %q must point to a relative path inside the project", name)
	}
	if _, err := fs.Stat(files.FS, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return target, nil
}

func (files projectFiles) Lstat(name string) (fs.FileInfo, error) {
	return fs.Lstat(files.FS, name)
}

func ReadTextFile(fsys fs.FS, name string) (string, error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q must be a regular file", name)
	}
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("%q must contain UTF-8 text", name)
	}
	return string(content), nil
}
