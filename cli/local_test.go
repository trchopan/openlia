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

func TestReleaseCollectsTargetOperatorAssets(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"openlia-operator-linux-amd64", "openlia-operator-linux-arm64"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("OPENLIA_OPERATOR_ASSET_DIR", directory)

	files, err := collectReleaseFiles(fstest.MapFS{"profile/config.yaml": {Data: []byte("profile\n")}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, file := range files {
		got[file.Name] = string(file.Data)
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		name := "operator/linux-" + architecture + "/openlia-operator"
		if got[name] != "openlia-operator-linux-"+architecture {
			t.Fatalf("release asset %s = %q", name, got[name])
		}
	}
}
