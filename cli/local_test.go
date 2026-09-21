package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestLocalComposeLogsUsesDockerCompose(t *testing.T) {
	root := filepath.Join(t.TempDir(), "openlia")
	config := defaultConfig()
	config.Mode = "local"
	config.Target = ""
	config.InstallRoot = root
	dockerDirectory := filepath.Join(root, "releases", config.Version, "docker")
	if err := os.MkdirAll(dockerDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dockerDirectory, "compose.yaml")
	generated := filepath.Join(dockerDirectory, "compose.generated.yaml")
	for _, path := range []string{base, generated} {
		if err := os.WriteFile(path, []byte("services: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	binDirectory := t.TempDir()
	docker := filepath.Join(binDirectory, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, err := (Local{Config: config}).composeLogs(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"compose",
		"--project-name",
		config.Project,
		"--project-directory",
		dockerDirectory,
		"-f",
		base,
		"-f",
		generated,
		"logs",
		"--tail",
		"200",
		"--follow",
	}
	got := strings.Split(strings.TrimSpace(string(output)), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("docker compose arguments = %v, want %v", got, want)
	}
}
