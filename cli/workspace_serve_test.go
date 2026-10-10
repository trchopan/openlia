package cli

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestWorkspaceServeRejectsZipArchiveWithGuidance(t *testing.T) {
	tempDir := t.TempDir()
	zipPath := filepath.Join(tempDir, "archive.zip")
	if err := os.WriteFile(zipPath, []byte("PK\x03\x04fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	options := Options{JSON: true}
	assets := fstest.MapFS{}
	code := commandWorkspaceServe(options, []string{zipPath}, assets)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d), got %d", ExitUsage, code)
	}
}

func TestWorkspaceServeRejectsMissingDirectory(t *testing.T) {
	options := Options{JSON: true}
	assets := fstest.MapFS{}
	code := commandWorkspaceServe(options, []string{"/path/to/missing/nonexistent/directory"}, assets)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d), got %d", ExitUsage, code)
	}
}

func TestExtractWorkspaceUIBundle(t *testing.T) {
	fakeAssets := fstest.MapFS{
		"packages/workspace-ui/dist/server.js":        &fstest.MapFile{Data: []byte("console.log('test');")},
		"packages/workspace-ui/dist/public/index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html></html>")},
	}

	cacheDir, err := extractWorkspaceUIBundle(fakeAssets)
	if err != nil {
		t.Fatalf("extractWorkspaceUIBundle failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	serverData, err := os.ReadFile(filepath.Join(cacheDir, "server.js"))
	if err != nil || string(serverData) != "console.log('test');" {
		t.Fatalf("extracted server.js incorrect or missing: %v", err)
	}

	htmlData, err := os.ReadFile(filepath.Join(cacheDir, "public", "index.html"))
	if err != nil || string(htmlData) != "<!DOCTYPE html><html></html>" {
		t.Fatalf("extracted index.html incorrect or missing: %v", err)
	}

	// Calling again should reuse cached directory
	cacheDir2, err := extractWorkspaceUIBundle(fakeAssets)
	if err != nil {
		t.Fatalf("extractWorkspaceUIBundle second call failed: %v", err)
	}
	if cacheDir != cacheDir2 {
		t.Fatalf("expected same cache dir, got %s and %s", cacheDir, cacheDir2)
	}
}
