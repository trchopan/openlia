package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFileReplacesContentsAndLeavesNoTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "state.json")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(destination, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new" {
		t.Fatalf("contents = %q", contents)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("temporary file was left behind: %v", entries)
	}
}

func TestAtomicWriteFileRequiresExistingParent(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "missing", "state")
	if err := AtomicWriteFile(destination, []byte("data"), 0o600); err == nil {
		t.Fatal("AtomicWriteFile succeeded with a missing parent")
	}
}
