package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterBoundsFilesAndPreservesNewestData(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sii-link.log")
	writer, err := newRotatingWriter(path, 32, 2)
	if err != nil {
		t.Fatalf("newRotatingWriter() error = %v", err)
	}
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	for _, message := range []string{
		"first-record-000\n",
		"second-record-00\n",
		"third-record-000\n",
		"newest-record-00\n",
	} {
		if _, err := writer.Write([]byte(message)); err != nil {
			t.Fatalf("Write(%q) error = %v", message, err)
		}
	}

	for _, suffix := range []string{"", ".1", ".2"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path+suffix, err)
		}
		if info.Size() > 32 {
			t.Errorf("%s size = %d, want <= 32", suffix, info.Size())
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", suffix, info.Mode().Perm())
		}
	}

	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected third backup: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "newest-record") {
		t.Fatalf("active log %q does not contain newest record", data)
	}
}

func TestNewRotatingWriterRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sii-link.log")
	for _, test := range []struct {
		name     string
		maxBytes int64
		backups  int
	}{
		{name: "zero size", maxBytes: 0, backups: 2},
		{name: "negative backups", maxBytes: 32, backups: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newRotatingWriter(path, test.maxBytes, test.backups); err == nil {
				t.Fatal("newRotatingWriter() error = nil, want validation error")
			}
		})
	}
}
