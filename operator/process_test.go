package operator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type environmentRecordingRunner struct {
	environment []string
}

func (r *environmentRecordingRunner) Run(_ context.Context, _ string, _ ...string) (CommandResult, error) {
	return CommandResult{}, nil
}

func (r *environmentRecordingRunner) RunWithEnv(_ context.Context, environment []string, _ string, _ ...string) (CommandResult, error) {
	r.environment = environment
	return CommandResult{}, nil
}

func TestComposeRunPassesResolvedRuntimeEnvironment(t *testing.T) {
	config := testConfig(t.TempDir(), t.TempDir())
	runner := &environmentRecordingRunner{}
	compose := NewCompose(config, runner)
	if _, err := compose.Run(context.Background(), "config", "--quiet"); err != nil {
		t.Fatal(err)
	}
	environment := strings.Join(runner.environment, "\n")
	for _, expected := range []string{
		"OPENLIA_DATA_ROOT=" + config.DataRoot,
		"OPENLIA_SYSTEM_SKILLS_ROOT=" + config.SystemSkillsRoot,
		"OPENLIA_SKILLS_CACHE_ROOT=" + config.SkillsCacheRoot,
		"OPENLIA_SKILLS_ENV_ROOT=" + config.SkillsEnvRoot,
		"OPENLIA_SECRET_DIR=" + config.SecretDir,
		"OPENLIA_NETWORK_NAME=" + config.NetworkName,
	} {
		if !strings.Contains(environment, expected) {
			t.Fatalf("Compose environment missing %q:\n%s", expected, environment)
		}
	}
}

func TestExecRunnerStreamsStderrWhileRetainingDiagnostics(t *testing.T) {
	script := filepath.Join(t.TempDir(), "stderr.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' progress >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var streamed bytes.Buffer
	result, err := (ExecRunner{Stderr: &streamed}).Run(context.Background(), script)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stderr) != "progress\n" || streamed.String() != "progress\n" {
		t.Fatalf("stderr was not retained and streamed: result=%q streamed=%q", result.Stderr, streamed.String())
	}
}
