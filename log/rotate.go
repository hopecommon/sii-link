package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
	closed   bool
}

func newRotatingWriter(path string, maxBytes int64, backups int) (*rotatingWriter, error) {
	if path == "" {
		return nil, errors.New("log path must not be empty")
	}
	if maxBytes <= 0 {
		return nil, errors.New("log max bytes must be greater than zero")
	}
	if backups < 0 {
		return nil, errors.New("log backup count must not be negative")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	file, size, err := openLogFile(path)
	if err != nil {
		return nil, err
	}
	return &rotatingWriter{
		path:     path,
		maxBytes: maxBytes,
		backups:  backups,
		file:     file,
		size:     size,
	}, nil
}

func openLogFile(path string) (*os.File, int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("open log file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, 0, fmt.Errorf("set log file permissions: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, fmt.Errorf("stat log file: %w", err)
	}
	return file, info.Size(), nil
}

func (writer *rotatingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.closed {
		return 0, os.ErrClosed
	}

	written := 0
	for len(data) > 0 {
		if writer.size >= writer.maxBytes || (writer.size > 0 && int64(len(data)) > writer.maxBytes-writer.size) {
			if err := writer.rotate(); err != nil {
				return written, err
			}
		}

		remaining := writer.maxBytes - writer.size
		chunk := data
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		n, err := writer.file.Write(chunk)
		writer.size += int64(n)
		written += n
		data = data[n:]
		if err != nil {
			return written, fmt.Errorf("write log file: %w", err)
		}
		if n == 0 {
			return written, errors.New("write log file: no progress")
		}
	}
	return written, nil
}

func (writer *rotatingWriter) rotate() error {
	if err := writer.file.Close(); err != nil {
		return fmt.Errorf("close log file before rotation: %w", err)
	}

	if writer.backups == 0 {
		if err := os.Remove(writer.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove active log during rotation: %w", err)
		}
	} else {
		oldest := writer.path + "." + strconv.Itoa(writer.backups)
		if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove oldest log backup: %w", err)
		}
		for index := writer.backups - 1; index >= 1; index-- {
			source := writer.path + "." + strconv.Itoa(index)
			target := writer.path + "." + strconv.Itoa(index+1)
			if err := os.Rename(source, target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("rotate log backup %d: %w", index, err)
			}
		}
		if err := os.Rename(writer.path, writer.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rotate active log: %w", err)
		}
	}

	file, size, err := openLogFile(writer.path)
	if err != nil {
		return err
	}
	writer.file = file
	writer.size = size
	return nil
}

func (writer *rotatingWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.closed {
		return nil
	}
	writer.closed = true
	if err := writer.file.Close(); err != nil {
		return fmt.Errorf("close log file: %w", err)
	}
	return nil
}
