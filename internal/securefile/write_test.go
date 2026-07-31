package securefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-data.json")
	if err := Write(path, []byte("client data")); err != nil {
		t.Fatalf("write private file: %v", err)
	}
	assertPrivateFile(t, path, "client data")
}

func TestWriteTightensExistingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-data.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new")); err != nil {
		t.Fatalf("write private file: %v", err)
	}
	assertPrivateFile(t, path, "new")
}

func TestWriteReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	path := filepath.Join(directory, "client-data.json")
	if err := os.WriteFile(target, []byte("target data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	if err := Write(path, []byte("client data")); err != nil {
		t.Fatalf("write private file: %v", err)
	}
	assertPrivateFile(t, path, "client data")
	targetData, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(targetData) != "target data" {
		t.Fatalf("symlink target contents = %q, want unchanged", targetData)
	}
}

func assertPrivateFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("file contents = %q, want %q", data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file permissions = %04o, want 0600", got)
	}
}
