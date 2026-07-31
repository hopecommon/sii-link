package securefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Write(path string, data []byte) (returnErr error) {
	directory := filepath.Dir(path)
	tempFile, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create private temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	closed := false
	renamed := false
	defer func() {
		if !closed {
			if err := tempFile.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("close private temporary file: %w", err))
			}
		}
		if !renamed {
			if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				returnErr = errors.Join(returnErr, fmt.Errorf("remove private temporary file: %w", err))
			}
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict private file permissions: %w", err)
	}
	written, err := tempFile.Write(data)
	if err != nil {
		return fmt.Errorf("write private file: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write private file: %w", io.ErrShortWrite)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync private file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close private file: %w", err)
	}
	closed = true
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace private file: %w", err)
	}
	renamed = true
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync private file directory: %w", err)
	}
	return nil
}
