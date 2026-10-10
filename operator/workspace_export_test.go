package operator

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceExport(t *testing.T) {
	tempDir := t.TempDir()
	dataRoot := filepath.Join(tempDir, "hermes")
	wsDir := filepath.Join(dataRoot, "workspace")
	skDir := filepath.Join(dataRoot, "skills")

	if err := os.MkdirAll(filepath.Join(wsDir, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "workspace.yaml"), []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "inbox", "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Noise file to skip
	if err := os.WriteFile(filepath.Join(wsDir, ".DS_Store"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(skDir, "my-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skDir, "my-skill", "SKILL.md"), []byte("# Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exportFile := filepath.Join(tempDir, "export.zip")
	config := Config{
		DataRoot: dataRoot,
	}

	result, err := WorkspaceExport(config, exportFile)
	if err != nil {
		t.Fatalf("WorkspaceExport failed: %v", err)
	}

	if !result.OK {
		t.Errorf("result.OK = false, want true")
	}
	if result.FilesCount != 3 {
		t.Errorf("result.FilesCount = %d, want 3 (workspace.yaml, inbox/note.md, my-skill/SKILL.md)", result.FilesCount)
	}
	if result.SizeBytes <= 0 {
		t.Errorf("result.SizeBytes = %d, want > 0", result.SizeBytes)
	}

	// Verify zip contents
	r, err := zip.OpenReader(exportFile)
	if err != nil {
		t.Fatalf("zip.OpenReader failed: %v", err)
	}
	defer r.Close()

	found := make(map[string]string)
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		found[f.Name] = string(data)
	}

	if _, hasNoise := found["workspace/.DS_Store"]; hasNoise {
		t.Errorf("zip contains .DS_Store, which should be omitted")
	}
	if found["workspace/workspace.yaml"] != "name: test\n" {
		t.Errorf("workspace.yaml content mismatch: got %q", found["workspace/workspace.yaml"])
	}
	if found["workspace/inbox/note.md"] != "# Note\n" {
		t.Errorf("inbox/note.md content mismatch: got %q", found["workspace/inbox/note.md"])
	}
	if found["skills/my-skill/SKILL.md"] != "# Skill\n" {
		t.Errorf("skills/my-skill/SKILL.md content mismatch: got %q", found["skills/my-skill/SKILL.md"])
	}
}
