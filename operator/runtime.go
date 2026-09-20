package operator

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

type RuntimeCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type RuntimeReport struct {
	Schema int            `json:"schema"`
	OK     bool           `json:"ok"`
	Checks []RuntimeCheck `json:"checks"`
}

// ValidateRuntime performs the inexpensive host and Docker checks needed
// before mutating runtime state. It intentionally does not inspect secrets.
func ValidateRuntime(ctx context.Context, config Config, runner CommandRunner) (RuntimeReport, error) {
	if err := config.ValidatePaths(); err != nil {
		return RuntimeReport{}, err
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	report := RuntimeReport{Schema: 1, Checks: []RuntimeCheck{}}
	add := func(name string, ok bool, detail string) {
		report.Checks = append(report.Checks, RuntimeCheck{Name: name, OK: ok, Detail: detail})
		if !ok {
			report.OK = false
		}
	}
	report.OK = true

	docker, err := runner.Run(ctx, "docker", "info", "--format", "{{.OSType}}")
	if err != nil {
		add("docker", false, "unavailable")
	} else if strings.TrimSpace(string(docker.Stdout)) != "linux" {
		add("docker", false, "linux_engine_required")
	} else {
		add("docker", true, "linux")
	}
	compose, err := runner.Run(ctx, "docker", "compose", "version")
	if err != nil {
		add("compose", false, "v2_required")
	} else {
		add("compose", true, strings.TrimSpace(string(compose.Stdout)))
	}
	if runtime.GOOS == "windows" {
		add("platform", false, "unsupported")
	} else {
		add("platform", true, runtime.GOOS)
	}
	if !report.OK {
		return report, fmt.Errorf("runtime prerequisites are not satisfied")
	}
	return report, nil
}
