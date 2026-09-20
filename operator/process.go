package operator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// CommandResult keeps stdout and stderr separate so JSON output can remain
// machine-readable while diagnostics stay on stderr.
type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// CommandRunner is the seam used by Compose and command tests.
type CommandRunner interface {
	Run(context.Context, string, ...string) (CommandResult, error)
}

// ExecRunner runs a host process without invoking a shell.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	}
	return result, fmt.Errorf("run %s: %w", name, err)
}

type Compose struct {
	Config Config
	Runner CommandRunner
}

func NewCompose(config Config, runner CommandRunner) Compose {
	if runner == nil {
		runner = ExecRunner{}
	}
	return Compose{Config: config, Runner: runner}
}

// Arguments returns the stable global Compose arguments used by the runtime.
func (c Compose) Arguments(args ...string) []string {
	result := []string{"compose", "--project-name", c.Config.ProjectName, "--project-directory", c.Config.ComposeProjectDir, "-f", c.Config.ComposeFile}
	if info, err := os.Stat(c.Config.GeneratedCompose); err == nil && info.Mode().IsRegular() {
		result = append(result, "-f", c.Config.GeneratedCompose)
	}
	return append(result, args...)
}

func (c Compose) Run(ctx context.Context, args ...string) (CommandResult, error) {
	arguments := c.Arguments(args...)
	if c.Runner == nil {
		c.Runner = ExecRunner{}
	}
	return c.Runner.Run(ctx, "docker", arguments...)
}

func (c Compose) Quiet(ctx context.Context, args ...string) error {
	_, err := c.Run(ctx, args...)
	return err
}

func (c Compose) ServiceRunning(ctx context.Context, service string) bool {
	result, err := c.Run(ctx, "ps", "--services", "--filter", "status=running")
	if err != nil {
		return false
	}
	for _, line := range bytes.Split(result.Stdout, []byte{'\n'}) {
		if string(line) == service {
			return true
		}
	}
	return false
}

func (c Compose) Docker(ctx context.Context, args ...string) (CommandResult, error) {
	runner := c.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return runner.Run(ctx, "docker", args...)
}

func (c Compose) Host(ctx context.Context, name string, args ...string) (CommandResult, error) {
	runner := c.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return runner.Run(ctx, name, args...)
}
