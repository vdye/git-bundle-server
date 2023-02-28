package common

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

type FileSystem interface {
	GetLocalExecutable(name string) (string, error)

	FileExists(filename string) (bool, error)
	WriteFile(filename string, content []byte) error
	DeleteFile(filename string) (bool, error)
	ReadFileLines(filename string) ([]string, error)
}

type fileSystem struct{}

func NewFileSystem() FileSystem {
	return &fileSystem{}
}

func (f *fileSystem) GetLocalExecutable(name string) (string, error) {
	thisExePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get path to current executable: %w", err)
	}
	exeDir := filepath.Dir(thisExePath)
	if err != nil {
		return "", fmt.Errorf("failed to get parent dir of current executable: %w", err)
	}

	programPath := filepath.Join(exeDir, name)
	programExists, err := f.FileExists(programPath)
	if err != nil {
		return "", fmt.Errorf("could not determine whether path to '%s' exists: %w", name, err)
	} else if !programExists {
		return "", fmt.Errorf("could not find path to '%s'", name)
	}

	return programPath, nil
}

func (f *fileSystem) FileExists(filename string) (bool, error) {
	_, err := os.Stat(filename)
	if err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, fmt.Errorf("error checking for file existence with 'stat': %w", err)
	}
}

func (f *fileSystem) WriteFile(filename string, content []byte) error {
	// Get filename parent path
	parentDir := path.Dir(filename)
	err := os.MkdirAll(parentDir, 0o755)
	if err != nil {
		return fmt.Errorf("error creating parent directories: %w", err)
	}

	err = os.WriteFile(filename, content, 0o644)
	if err != nil {
		return fmt.Errorf("could not write file: %w", err)
	}
	return nil
}

func (f *fileSystem) DeleteFile(filename string) (bool, error) {
	err := os.Remove(filename)
	if err == nil {
		return true, nil
	}

	pathErr, ok := err.(*os.PathError)
	if ok && pathErr.Err == syscall.ENOENT {
		return false, nil
	} else {
		return false, err
	}
}

func (f *fileSystem) ReadFileLines(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		pathErr, ok := err.(*os.PathError)
		if ok && pathErr.Err == syscall.ENOENT {
			// If the file doesn't exist, return empty result rather than an
			// error
			return []string{}, nil
		} else {
			return nil, err
		}
	}

	var l []string
	reader := bufio.NewReader(file)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		l = append(l, scanner.Text())
	}

	return l, nil
}
