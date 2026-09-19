package main

import (
	"io/fs"
	"testing"
)

func TestEmbeddedWorkspaceIncludesControlFiles(t *testing.T) {
	for _, path := range []string{"workspace-template/.gitignore", "workspace-template/inbox/.gitkeep"} {
		if _, err := fs.ReadFile(releaseAssets, path); err != nil {
			t.Fatalf("embedded release is missing %s: %v", path, err)
		}
	}
}
