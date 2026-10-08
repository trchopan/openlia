package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type deploymentSelectors struct {
	mode       string
	modeSet    bool
	target     string
	targetSet  bool
	root       string
	rootSet    bool
	project    string
	projectSet bool
}

func resolveMaintenanceDeployment(selectors deploymentSelectors) (Config, error) {
	config, err := loadConfig()
	if err == nil {
		mismatches := make([]string, 0, 4)
		if selectors.modeSet {
			if config.Mode != selectors.mode {
				mismatches = append(mismatches, fmt.Sprintf("mode: config=%q, argument=%q", config.Mode, selectors.mode))
			}
		}
		if selectors.targetSet && config.Target != selectors.target {
			mismatches = append(mismatches, fmt.Sprintf("target: config=%q, argument=%q", config.Target, selectors.target))
		}
		if selectors.rootSet && config.InstallRoot != selectors.root {
			mismatches = append(mismatches, fmt.Sprintf("root: config=%q, argument=%q", config.InstallRoot, selectors.root))
		}
		if selectors.projectSet && config.Project != selectors.project {
			mismatches = append(mismatches, fmt.Sprintf("project: config=%q, argument=%q", config.Project, selectors.project))
		}
		if len(mismatches) > 0 {
			return Config{}, fmt.Errorf("deployment arguments conflict with %s:\n  %s", configPath(), strings.Join(mismatches, "\n  "))
		}
		return config, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load %s: %w", configPath(), err)
	}

	if !selectors.modeSet || selectors.root == "" || selectors.project == "" {
		return Config{}, fmt.Errorf("no OpenLia config found at %s; specify --local or --target, --root, and --project", configPath())
	}
	if selectors.mode == "ssh" && selectors.target == "" {
		return Config{}, errors.New("explicit remote deployment selection requires --target")
	}
	if selectors.mode == "local" && selectors.target != "" {
		return Config{}, errors.New("--local and --target are mutually exclusive")
	}

	config = defaultConfig()
	config.Mode = selectors.mode
	config.Target = selectors.target
	config.InstallRoot = selectors.root
	config.Project = selectors.project
	return config, nil
}

func deploymentSummary(config Config) string {
	target := config.Target
	if config.Mode == "local" {
		target = "local machine"
	}
	return fmt.Sprintf(
		"Deployment: %s\nMode:       %s\nTarget:     %s\nRoot:       %s\nProject:    %s\nConfig:     %s",
		config.Project,
		config.Mode,
		target,
		config.InstallRoot,
		config.Project,
		configPath(),
	)
}
