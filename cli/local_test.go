package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestNewDeploymentSelectsLocalBackend(t *testing.T) {
	config := defaultConfig()
	config.Mode = "local"
	config.Target = ""
	if _, ok := newDeployment(config).(Local); !ok {
		t.Fatalf("backend = %T, want Local", newDeployment(config))
	}
}

func TestLocalReleaseInstallAndActivate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "openlia")
	config := defaultConfig()
	config.Mode = "local"
	config.Target = ""
	config.InstallRoot = root
	assets := fstest.MapFS{
		"ops/example.sh":      {Data: []byte("#!/usr/bin/env bash\n")},
		"profile/config.yaml": {Data: []byte("secrets: {}\n")},
	}
	archive, digest, err := releaseArchive(assets)
	if err != nil {
		t.Fatal(err)
	}
	local := Local{Config: config}
	if err := local.uploadRelease(context.Background(), archive, digest); err != nil {
		t.Fatal(err)
	}
	if err := local.activateRelease(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if current != filepath.Join(root, "releases", config.Version) {
		t.Fatalf("current = %q", current)
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "meta", "runtime.json")); err != nil {
		t.Fatalf("local marker missing: %v", err)
	}
}
