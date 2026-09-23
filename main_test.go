package main

import (
	"io/fs"
	"testing"
)

func TestEmbeddedWorkspaceIncludesControlFiles(t *testing.T) {
	for _, path := range []string{
		"workspace-template/.gitignore",
		"workspace-template/inbox/.gitkeep",
		"docker/.env.example",
		"docker/git-askpass.sh",
		"docker/secret-source.sh",
		"profile/config.yaml",
		"profile/distribution.yaml",
		"profile/cron/scripts/openlia-workspace-git-sync.sh",
	} {
		if _, err := fs.ReadFile(releaseAssets, path); err != nil {
			t.Fatalf("embedded release is missing %s: %v", path, err)
		}
	}
}

func TestEmbeddedBunRuntimeAssets(t *testing.T) {
	for _, path := range []string{
		"packages/openlia-tools/dist/server.js",
		"packages/workspace-ui/dist/server.js",
		"packages/workspace-ui/dist/public/index.html",
		"packages/openlia-job/dist/openlia-job.js",
		"docker/tools.Dockerfile",
		"docker/workspace-ui.Dockerfile",
	} {
		if _, err := fs.ReadFile(releaseAssets, path); err != nil {
			t.Fatalf("embedded Bun runtime is missing %s: %v", path, err)
		}
	}
}
