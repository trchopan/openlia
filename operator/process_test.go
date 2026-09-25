package operator

import (
	"context"
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
