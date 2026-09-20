package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectorySHA256IsDeterministicAndExcludesPythonCache(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(filepath.Join(root, "nested", "__pycache__"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "nested", "b.txt"), []byte("b"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "nested", "__pycache__", "ignored.pyc"), []byte("one"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	firstHash, err := DirectorySHA256(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := DirectorySHA256(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("same content produced different hashes: %s != %s", firstHash, secondHash)
	}
	if err := os.WriteFile(filepath.Join(first, "nested", "__pycache__", "ignored.pyc"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	unchanged, err := DirectorySHA256(first)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != firstHash {
		t.Fatal("Python cache changes affected the directory hash")
	}
	if err := os.WriteFile(filepath.Join(first, "nested", "b.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := DirectorySHA256(first)
	if err != nil {
		t.Fatal(err)
	}
	if changed == firstHash {
		t.Fatal("file content change did not affect the directory hash")
	}
}
