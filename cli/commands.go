package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openlia/operator"
)

func Run(args []string, assets fs.FS) int {
	options, remaining, err := splitCommonFlags(args)
	if err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	if len(remaining) == 0 {
		usage(os.Stdout)
		return ExitOK
	}
	if remaining[0] == "--version" || remaining[0] == "version" {
		return writeResult(options, map[string]any{
			"schema":  1,
			"version": defaultVersion,
			"hermes":  "v2026.9.14",
			"locho":   "1.2.0-beta.1",
		}, fmt.Sprintf("openlia %s (Hermes v2026.9.14, Locho 1.2.0-beta.1)", defaultVersion))
	}
	if remaining[0] == "--help" || remaining[0] == "help" {
		usage(os.Stdout)
		return ExitOK
	}

	switch remaining[0] {
	case "init":
		return commandInit(options, remaining[1:], assets)
	case "status":
		return commandHealth(options, remaining[1:], false)
	case "doctor":
		return commandHealth(options, remaining[1:], true)
	case "deploy", "start", "stop", "restart":
		return commandLifecycle(options, remaining[0], remaining[1:])
	case "uninstall":
		return commandUninstall(options, remaining[1:])
	case "logs":
		return commandLogs(options, remaining[1:])
	case "update":
		return commandUpdate(options, remaining[1:], assets)
	case "skills":
		return commandSkills(options, remaining[1:], assets)
	case "skill-sources":
		return commandSkillSources(options, remaining[1:])
	case "auth":
		return commandAuth(options, remaining[1:])
	case "attachments":
		return commandAttachments(options, remaining[1:])
	case "backup":
		return commandBackup(options, remaining[1:])
	case "workspace":
		return commandWorkspace(options, remaining[1:])
	case "workspace-ui":
		return commandWorkspaceUI(options, remaining[1:])
	default:
		return fail(options, ExitUsage, fmt.Sprintf("unknown command %q", remaining[0]), map[string]any{"hint": "run openlia --help"})
	}
}

func splitCommonFlags(args []string) (Options, []string, error) {
	options := Options{}
	remaining := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json":
			options.JSON = true
		case "--non-interactive":
			options.NonInteractive = true
		case "--follow":
			options.Follow = true
		case "--check-providers":
			options.ProviderCheck = true
		case "--help", "-h":
			if len(remaining) == 0 {
				remaining = append(remaining, "help")
			} else {
				remaining = append(remaining, arg)
			}
		default:
			remaining = append(remaining, arg)
		}
	}
	return options, remaining, nil
}

func hasArgument(args []string, wanted string) bool {
	for _, arg := range args {
		if arg == wanted {
			return true
		}
	}
	return false
}

func newFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	return set
}

func configOrError(options Options) (Config, int) {
	config, err := loadConfig()
	if err == nil {
		return config, ExitOK
	}
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fail(options, ExitPrereq, "OpenLia is not initialized; run `openlia init --local` or `openlia init --target user@host`", map[string]any{"config": configPath()})
	}
	return Config{}, fail(options, ExitFailure, err.Error(), nil)
}

func commandInit(options Options, args []string, assets fs.FS) int {
	set := newFlagSet("init")
	local := set.Bool("local", false, "deploy on this machine")
	target := set.String("target", "", "SSH destination such as user@host")
	root := set.String("root", "", "OpenLia installation root")
	project := set.String("project", "", "Compose project name")
	timezone := set.String("timezone", "", "IANA timezone for the Hermes agent")
	model := set.String("model", "", "Hermes model identifier")
	provider := set.String("provider", "", "Hermes provider identifier")
	externalNetwork := set.String("external-network", "", "Existing Docker network for internal services")
	apiEnabled := set.Bool("api", false, "enable the private API listener")
	apiHost := set.String("api-host", "", "API bind address when --api is enabled")
	workspaceGitRemote := set.String("workspace-git-remote", "", "HTTPS GitHub repository for workspace backup")
	workspaceGitBranch := set.String("workspace-git-branch", "", "workspace Git branch")
	workspaceGitSchedule := set.String("workspace-git-schedule", "", "workspace Git automatic pull schedule")
	workspaceGitAuthorName := set.String("workspace-git-author-name", "", "workspace Git commit author name")
	workspaceGitAuthorEmail := set.String("workspace-git-author-email", "", "workspace Git commit author email")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}
	if set.NArg() != 0 {
		return fail(options, ExitUsage, "init does not accept positional arguments", nil)
	}
	config := defaultConfig()
	previousMode := config.Mode
	if existing, err := loadConfig(); err == nil {
		config = existing
		previousMode = existing.Mode
	}
	if *local && *target != "" {
		return fail(options, ExitUsage, "--local and --target are mutually exclusive", nil)
	}
	if *local {
		config.Mode = "local"
		config.Target = ""
	} else if *target != "" {
		config.Mode = "ssh"
		config.Target = *target
	}
	if config.Target == "" && config.Mode != "local" {
		return fail(options, ExitUsage, "--local or --target is required on first initialization", nil)
	}
	if *root != "" {
		config.InstallRoot = *root
	} else if config.Mode != previousMode {
		if config.Mode == "local" {
			config.InstallRoot = defaultLocalInstallRoot()
		} else {
			config.InstallRoot = defaultRemoteRoot
		}
	}
	if *project != "" {
		config.Project = *project
	}
	if *timezone != "" {
		config.Timezone = *timezone
	}
	if *model != "" {
		config.Model = *model
	}
	if *provider != "" {
		config.Provider = *provider
	}
	if *externalNetwork != "" {
		config.ExternalNetwork = *externalNetwork
	}
	if hasArgument(args, "--api") {
		config.APIEnabled = *apiEnabled
	}
	if *apiHost != "" {
		config.APIHost = *apiHost
	}
	if *workspaceGitRemote != "" {
		config.WorkspaceGit.Enabled = true
		config.WorkspaceGit.Remote = *workspaceGitRemote
		config.EnabledSkills = setSkill(config.EnabledSkills, "workspace-git", true)
	}
	if *workspaceGitBranch != "" {
		config.WorkspaceGit.Branch = *workspaceGitBranch
	}
	if *workspaceGitSchedule != "" {
		config.WorkspaceGit.Schedule = *workspaceGitSchedule
	}
	if *workspaceGitAuthorName != "" {
		config.WorkspaceGit.AuthorName = *workspaceGitAuthorName
	}
	if *workspaceGitAuthorEmail != "" {
		config.WorkspaceGit.AuthorEmail = *workspaceGitAuthorEmail
	}
	if config.WorkspaceGit.Enabled {
		config.EnabledSkills = setSkill(config.EnabledSkills, "workspace-git", true)
	}
	sourcePath := config.SecretSource
	if sourcePath != "" {
		if err := validateProtectedSourcePath(sourcePath, "secret source"); err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
	}
	if err := validateConfig(config); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	if config.Mode != "local" && !targetOperatorAssetsAvailable() {
		return fail(options, ExitPrereq, "remote deployment requires Linux operator artifacts; run `make build` first", nil)
	}

	archive, digest, err := releaseArchive(assets)
	if err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	deployment := newDeployment(config)
	ctx, cancel := remoteContext()
	defer cancel()
	if config.WorkspaceUIHost == "0.0.0.0" {
		if err := provisionWorkspaceUIPassword(ctx, deployment, config); err != nil {
			return fail(options, ExitFailure, "workspace-ui password provisioning failed: "+err.Error(), nil)
		}
	}
	if sourcePath != "" {
		if err := deployment.uploadFile(ctx, sourcePath, deployment.rootPath("runtime", "secrets", "hermes.env"), 0o600); err != nil {
			return fail(options, ExitFailure, "could not stage secret source: "+err.Error(), nil)
		}
	}
	for _, host := range config.Services {
		if host.Source != "" && host.Name != "" {
			if err := validateProtectedSourcePath(host.Source, "attachment source"); err != nil {
				return fail(options, ExitUsage, err.Error(), nil)
			}
			target := deployment.rootPath("runtime", "locho", host.Name, "attachments.toml")
			if err := deployment.uploadFile(ctx, host.Source, target, 0o600); err != nil {
				return fail(options, ExitFailure, "could not stage attachment source: "+err.Error(), nil)
			}
		}
	}
	if err := deployment.uploadRelease(ctx, archive, digest); err != nil {
		return fail(options, ExitPrereq, err.Error(), nil)
	}
	if err := deployment.activateRelease(ctx); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if _, err := deployment.bootstrap(ctx); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	// Generate the Compose sidecar file before the full deploy so Hermes starts
	// with the correct Locho gateway endpoint.
	if _, err := deployment.operation(ctx, "attachments", nil, "generate", "--json"); err != nil {
		return fail(options, ExitFailure, "attachment Compose generation failed: "+err.Error(), nil)
	}
	if _, err := deployment.deploy(ctx, "deploy", true, "all"); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}

	if config.WorkspaceGit.Enabled {
		if _, err := deployment.workspaceGit(ctx, "setup", config.WorkspaceGit); err != nil {
			return fail(options, ExitFailure, "workspace Git setup failed: "+err.Error(), nil)
		}
	}
	if err := saveConfig(config); err != nil {
		return fail(options, ExitInternal, "deployment succeeded but operator config could not be saved: "+err.Error(), nil)
	}
	return writeResult(options, map[string]any{
		"schema":         1,
		"ok":             true,
		"action":         "init",
		"mode":           config.Mode,
		"target":         config.Target,
		"root":           config.InstallRoot,
		"release":        config.Version,
		"release_sha256": digest,
		"workspace":      "initialized_only_when_empty",
		"workspace_git":  config.WorkspaceGit.Enabled,
	}, fmt.Sprintf("openlia init: deployed %s in %s mode at %s; workspace preserved when non-empty; workspace_git=%t", config.Version, config.Mode, config.InstallRoot, config.WorkspaceGit.Enabled))
}

func commandHealth(options Options, args []string, strict bool) int {
	if len(args) != 0 {
		return fail(options, ExitUsage, "status and doctor do not accept positional arguments", nil)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	raw, err := newDeployment(config).health(ctx, !strict, options.ProviderCheck)
	if err != nil {
		if len(raw) == 0 {
			return fail(options, ExitFailure, err.Error(), map[string]any{"mode": config.Mode, "target": config.Target, "root": config.InstallRoot})
		}
		if options.JSON {
			var health any
			if unmarshalErr := json.Unmarshal(raw, &health); unmarshalErr != nil {
				health = map[string]any{"output": redact(string(raw))}
			}
			returnCode := ExitFailure
			return failWithPayload(options, returnCode, "deployment health checks failed", map[string]any{
				"mode":   config.Mode,
				"target": config.Target,
				"root":   config.InstallRoot,
				"health": health,
			})
		}
		fmt.Fprint(os.Stdout, redact(string(raw)))
		return ExitFailure
	}
	if options.JSON {
		var health any
		if json.Unmarshal(raw, &health) != nil {
			health = map[string]any{"output": redact(string(raw))}
		}
		return writeResult(options, map[string]any{
			"schema":  1,
			"ok":      true,
			"mode":    config.Mode,
			"target":  config.Target,
			"root":    config.InstallRoot,
			"openlia": config.Version,
			"hermes":  map[string]any{"tag": config.HermesTag, "digest": config.HermesDigest},
			"locho":   map[string]any{"version": config.LochoVersion},
			"health":  health,
		}, fmt.Sprintf("target=%s\n%s", config.Target, redact(string(raw))))
	}
	return writeResult(options, nil, fmt.Sprintf("mode=%s root=%s\n%s", config.Mode, config.InstallRoot, redact(string(raw))))
}

func failWithPayload(options Options, code int, message string, fields map[string]any) int {
	message = redact(message)
	if options.JSON {
		payload := map[string]any{"schema": 1, "ok": false, "error": message}
		for key, value := range fields {
			payload[key] = value
		}
		if err := writeJSON(payload); err != nil {
			fmt.Fprintf(os.Stderr, "openlia: %s\n", redact(err.Error()))
		}
	} else {
		fmt.Fprintf(os.Stderr, "openlia: %s\n", message)
	}
	return code
}

func commandLifecycle(options Options, action string, args []string) int {
	if len(args) != 0 {
		return fail(options, ExitUsage, action+" does not accept positional arguments", nil)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	if config.WorkspaceUIHost == "0.0.0.0" {
		if err := provisionWorkspaceUIPassword(ctx, deployment, config); err != nil {
			return fail(options, ExitFailure, "workspace-ui password provisioning failed: "+err.Error(), nil)
		}
	}
	if action == "deploy" {
		for _, host := range config.Services {
			if host.Source != "" && host.Name != "" {
				if err := syncAttachmentSource(ctx, deployment, host.Name, host.Source); err != nil {
					return fail(options, ExitFailure, fmt.Sprintf("attachment sync failed for host %q: %s", host.Name, err.Error()), nil)
				}
			}
		}
		if _, err := deployment.operation(ctx, "attachments", nil, "generate", "--json"); err != nil {
			return fail(options, ExitFailure, "attachment Compose generation failed: "+err.Error(), nil)
		}
	}
	start := action == "start" || action == "restart"
	raw, err := deployment.deploy(ctx, action, start, "all")
	if err != nil {
		return fail(options, ExitFailure, err.Error(), map[string]any{"action": action})
	}
	if action == "deploy" && config.WorkspaceGit.Enabled {
		if _, err := deployment.operation(ctx, "profile", nil, "sync", "--json"); err != nil {
			return fail(options, ExitFailure, "workspace Git profile synchronization failed: "+err.Error(), map[string]any{"action": action})
		}
		if _, err := deployment.workspaceGit(ctx, "ensure", config.WorkspaceGit); err != nil {
			return fail(options, ExitFailure, "workspace Git reconciliation failed: "+err.Error(), map[string]any{"action": action})
		}
	}
	return renderRemote(options, raw, "openlia "+action+": "+redact(string(raw)))
}

func commandUninstall(options Options, args []string) int {
	set := newFlagSet("uninstall")
	local := set.Bool("local", false, "uninstall a local deployment")
	target := set.String("target", "", "SSH destination such as user@host")
	root := set.String("root", "", "specific OpenLia installation root")
	project := set.String("project", "", "Compose project name")
	if err := set.Parse(args); err != nil {
		return ExitUsage
	}
	if set.NArg() != 0 {
		return fail(options, ExitUsage, "uninstall does not accept positional arguments", nil)
	}
	if (*local && *target != "") || (!*local && *target == "") || *root == "" || *project == "" {
		return fail(options, ExitUsage, "uninstall requires either --local or --target, plus --root and --project", nil)
	}
	config := defaultConfig()
	if *local {
		config.Mode = "local"
	} else {
		config.Mode = "ssh"
		config.Target = *target
	}
	config.InstallRoot = *root
	config.Project = *project
	if err := validateConfig(config); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	if !options.NonInteractive {
		expected := "uninstall " + config.InstallRoot
		fmt.Fprintf(os.Stderr, "Permanently remove OpenLia from %s at %s? Type %q to continue: ", config.Mode, config.InstallRoot, expected)
		answer, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
		if readErr != nil || strings.TrimSpace(answer) != expected {
			return fail(options, ExitFailure, "uninstall cancelled", nil)
		}
	}
	ctx, cancel := remoteContext()
	defer cancel()
	raw, err := newDeployment(config).uninstall(ctx)
	if err != nil {
		return fail(options, ExitFailure, err.Error(), map[string]any{"mode": config.Mode, "target": config.Target, "root": config.InstallRoot})
	}
	return renderRemote(options, raw, "openlia uninstall: "+redact(string(raw)))
}

func commandLogs(options Options, args []string) int {
	if len(args) != 0 {
		return fail(options, ExitUsage, "logs does not accept positional arguments", nil)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	raw, err := newDeployment(config).composeLogs(ctx, options.Follow)
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if options.JSON {
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "logs": redact(string(raw)), "follow": options.Follow}, "")
	}
	fmt.Fprint(os.Stdout, redact(string(raw)))
	return ExitOK
}

func commandUpdate(options Options, args []string, assets fs.FS) int {
	component := ""
	if len(args) > 1 {
		return fail(options, ExitUsage, "update accepts at most one component", nil)
	}
	if len(args) == 1 {
		component = args[0]
	}
	if component == "" {
		return writeResult(options, map[string]any{
			"schema":    1,
			"ok":        true,
			"read_only": true,
			"updates":   []any{},
			"message":   "No release index is configured; choose an explicit component after changing its pinned desired version.",
			"current":   map[string]string{"openlia": defaultVersion, "hermes": "v2026.9.14", "locho": "1.2.0-beta.1"},
		}, "No release index configured. No component was changed.")
	}
	if component != "openlia" && component != "hermes" && component != "locho" {
		return fail(options, ExitUsage, "component must be openlia, hermes, or locho", nil)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	if config.WorkspaceUIHost == "0.0.0.0" {
		if err := provisionWorkspaceUIPassword(ctx, deployment, config); err != nil {
			return fail(options, ExitFailure, "workspace-ui password provisioning failed: "+err.Error(), nil)
		}
	}
	if component == "openlia" {
		if config.Mode != "local" && !targetOperatorAssetsAvailable() {
			return fail(options, ExitPrereq, "remote OpenLia updates require Linux operator artifacts; run `make build` first", nil)
		}
		archive, digest, err := releaseArchive(assets)
		if err != nil {
			return fail(options, ExitInternal, err.Error(), nil)
		}
		if err := deployment.uploadRelease(ctx, archive, digest); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if err := deployment.activateRelease(ctx); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		profileArgs := []string{"profile"}
		if options.JSON {
			profileArgs = append(profileArgs, "--json")
		}
		raw, err := deployment.operation(ctx, "deploy", nil, profileArgs...)
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if config.WorkspaceUIHost != "" || len(config.Services) > 0 {
			if _, err := deployment.operation(ctx, "attachments", nil, "generate", "--json"); err != nil {
				return fail(options, ExitFailure, "optional runtime Compose generation failed: "+err.Error(), nil)
			}
			raw, err = deployment.deploy(ctx, "deploy", false, "all")
			if err != nil {
				return fail(options, ExitFailure, "optional runtime reconciliation failed: "+err.Error(), nil)
			}
		}
		return renderRemote(options, raw, "openlia update openlia: profile assets synchronized")
	}
	// Runtime image updates intentionally reuse the pinned Compose definition.
	// The command does not silently change a tag or digest; operators update the
	// desired pin in their operator config before invoking this boundary.
	if component == "locho" {
		if _, err := deployment.operation(ctx, "attachments", nil, "generate", "--json"); err != nil {
			return fail(options, ExitFailure, "attachment Compose generation failed: "+err.Error(), nil)
		}
	}
	raw, err := deployment.deploy(ctx, "deploy", true, component)
	if err != nil {
		return fail(options, ExitFailure, err.Error(), map[string]any{"component": component})
	}
	return renderRemote(options, raw, "openlia update "+component+": pinned runtime reconciled")
}

func commandSkills(options Options, args []string, assets fs.FS) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "skills requires list, show, audit, install, update, uninstall, test, reset, fork-refresh, status, fork, migrate, enable, or disable", nil)
	}
	action := args[0]
	args = args[1:]
	switch action {
	case "list":
		if len(args) != 0 {
			return fail(options, ExitUsage, "skills list takes no positional arguments", nil)
		}
		config, err := loadConfig()
		if errors.Is(err, os.ErrNotExist) {
			config = defaultConfig()
		} else if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		entries := make([]map[string]any, 0, len(defaultSkills))
		for _, skill := range defaultSkills {
			content, readErr := fs.ReadFile(assets, filepath.ToSlash(filepath.Join("profile", "skills", skill, "SKILL.md")))
			if readErr != nil {
				return fail(options, ExitInternal, "missing bundled skill "+skill, nil)
			}
			entries = append(entries, map[string]any{"name": skill, "enabled": contains(config.EnabledSkills, skill), "available": len(content) > 0})
		}
		if len(config.SkillSources) == 0 {
			return writeResult(options, map[string]any{"schema": 1, "ok": true, "skills": entries}, skillListHuman(config))
		}
		ctx, cancel := remoteContext()
		defer cancel()
		raw, operationErr := newDeployment(config).operation(ctx, "skills", nil, "list", "--json")
		if operationErr != nil {
			return fail(options, ExitFailure, operationErr.Error(), nil)
		}
		var external []map[string]any
		if err := json.Unmarshal(raw, &external); err != nil {
			return fail(options, ExitFailure, "could not parse external skill catalog", nil)
		}
		for _, skill := range entries {
			skill["origin"] = "bundled"
		}
		lines := []string{skillListHuman(config)}
		for _, skill := range external {
			skill["origin"] = "external"
			entries = append(entries, skill)
			lines = append(lines, fmt.Sprintf("%s/%s: available=%t installed=%t update_available=%t", skill["source"], skill["name"], true, skill["installed"], skill["update_available"]))
		}
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "skills": entries}, strings.Join(lines, "\n"))
	case "show":
		if len(args) != 1 || !validSkillIdentifier(args[0]) {
			return fail(options, ExitUsage, "skills show requires a skill name or source/skill identifier", nil)
		}
		if safeComponent(args[0]) && contains(defaultSkills, args[0]) {
			content, err := fs.ReadFile(assets, filepath.ToSlash(filepath.Join("profile", "skills", args[0], "SKILL.md")))
			if err != nil {
				return fail(options, ExitInternal, err.Error(), nil)
			}
			if options.JSON {
				return writeResult(options, map[string]any{"schema": 1, "ok": true, "name": args[0], "content": string(content)}, "")
			}
			fmt.Fprint(os.Stdout, string(content))
			return ExitOK
		}
		if config, err := loadConfig(); err == nil {
			return runSkillOperator(options, config, "show", args)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if !safeComponent(args[0]) || !contains(defaultSkills, args[0]) {
			return fail(options, ExitPrereq, "external skills require an initialized deployment", nil)
		}
		return fail(options, ExitPrereq, "external skills require an initialized deployment", nil)
	case "status":
		if len(args) > 1 || (len(args) == 1 && !safeComponent(args[0])) {
			return fail(options, ExitUsage, "skills status accepts at most one safe skill name", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		statusArgs := []string{}
		if len(args) == 1 {
			statusArgs = append(statusArgs, "--skill", args[0])
		}
		if options.JSON {
			statusArgs = append(statusArgs, "--json")
		}
		ctx, cancel := remoteContext()
		defer cancel()
		raw, err := newDeployment(config).operation(ctx, "skill-status", nil, statusArgs...)
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, "openlia skills status: read-only skill provenance inspection")
	case "fork":
		if len(args) != 1 || !safeComponent(args[0]) {
			return fail(options, ExitUsage, "skills fork requires an installed safe skill name", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		ctx, cancel := remoteContext()
		defer cancel()
		raw, err := newDeployment(config).operation(ctx, "skill-fork", nil, args[0], "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, "openlia skills fork: active skill preserved and customization lineage recorded")
	case "migrate":
		return commandSkillMigration(options, args)
	case "test":
		if len(args) != 1 || !validSkillIdentifier(args[0]) {
			return fail(options, ExitUsage, "skills test requires a skill name or source/skill identifier", nil)
		}
		if safeComponent(args[0]) && contains(defaultSkills, args[0]) {
			return testSkill(options, assets, args[0])
		}
		if config, err := loadConfig(); err == nil {
			return runSkillOperator(options, config, "test", args)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if !safeComponent(args[0]) || !contains(defaultSkills, args[0]) {
			return fail(options, ExitPrereq, "external skill tests require an initialized deployment", nil)
		}
		return fail(options, ExitPrereq, "external skill tests require an initialized deployment", nil)
	case "audit", "fork-refresh":
		if len(args) != 1 || !validSkillIdentifier(args[0]) {
			return fail(options, ExitUsage, "skills "+action+" requires a skill name or source/skill identifier", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		return runSkillOperator(options, config, action, args)
	case "update":
		if len(args) != 1 || !validSkillIdentifier(args[0]) {
			return fail(options, ExitUsage, "skills update requires a skill name or source/skill identifier", nil)
		}
		if options.NonInteractive {
			return fail(options, ExitUsage, "skill update requires explicit interactive approval", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		commit, code := showSkillPlan(options, config, "plan-update", args[0])
		if code != ExitOK {
			return code
		}
		if !confirmExact(options, "Update external skill "+args[0]+"?", "update "+args[0]) {
			return fail(options, approvalExitCode(options), "skill update cancelled; explicit interactive approval is required", nil)
		}
		return runSkillOperator(options, config, action, append(args, "--approve", "--commit", commit))
	case "install":
		disabled, args := removeArgument(args, "--disabled")
		if len(args) != 1 || !validExternalSkillIdentifier(args[0]) {
			return fail(options, ExitUsage, "skills install requires source/skill [--disabled]", nil)
		}
		if options.NonInteractive {
			return fail(options, ExitUsage, "skill install requires explicit interactive approval", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		commit, code := showSkillPlan(options, config, "plan-install", args[0])
		if code != ExitOK {
			return code
		}
		if !confirmExact(options, "Install external skill "+args[0]+"?", "install "+args[0]) {
			return fail(options, approvalExitCode(options), "skill install cancelled; explicit interactive approval is required", nil)
		}
		name := strings.Split(args[0], "/")[1]
		previousEnabled := append([]string(nil), config.EnabledSkills...)
		if !disabled {
			config.EnabledSkills = setSkill(config.EnabledSkills, name, true)
			if err := saveConfig(config); err != nil {
				return fail(options, ExitFailure, err.Error(), nil)
			}
		}
		result := runSkillOperator(options, config, action, append(args, "--approve", "--commit", commit))
		if result != ExitOK && !disabled {
			config.EnabledSkills = previousEnabled
			_ = saveConfig(config)
		}
		return result
	case "uninstall", "reset":
		if len(args) != 1 || !validSkillIdentifier(args[0]) {
			return fail(options, ExitUsage, "skills "+action+" requires a skill name or source/skill identifier", nil)
		}
		expected := action + " skill " + args[0]
		if !confirmExact(options, "Destructive skill "+action+" for "+args[0]+"?", expected) {
			return fail(options, approvalExitCode(options), "skill "+action+" cancelled; exact interactive confirmation is required", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		return runSkillOperator(options, config, action, append(args, "--approve"))
	case "enable", "disable":
		if len(args) != 1 || !safeComponent(args[0]) {
			return fail(options, ExitUsage, "skills enable/disable requires an installed safe skill name", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		config.EnabledSkills = setSkill(config.EnabledSkills, args[0], action == "enable")
		if err := saveConfig(config); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if config.Mode == "local" || config.Target != "" {
			ctx, cancel := remoteContext()
			defer cancel()
			if _, err := newDeployment(config).operation(ctx, "profile", nil, "sync", "--json"); err != nil {
				return fail(options, ExitFailure, err.Error(), nil)
			}
		}
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "skill": args[0], "enabled": action == "enable"}, fmt.Sprintf("skill %s: %s", args[0], action+"d"))
	default:
		return fail(options, ExitUsage, "unknown skills action "+action, nil)
	}
}

func commandSkillSources(options Options, args []string) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "skill-sources requires add, list, remove, check, or fetch", nil)
	}
	action := args[0]
	args = args[1:]
	config, err := loadConfig()
	if errors.Is(err, os.ErrNotExist) {
		return fail(options, ExitPrereq, "OpenLia is not initialized", nil)
	}
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	switch action {
	case "list":
		if len(args) != 0 {
			return fail(options, ExitUsage, "skill-sources list takes no arguments", nil)
		}
		lines := make([]string, 0, len(config.SkillSources))
		for _, source := range config.SkillSources {
			lines = append(lines, fmt.Sprintf("%s: %s (%s)", source.Name, source.Repository, source.Branch))
		}
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "skill_sources": config.SkillSources}, strings.Join(lines, "\n"))
	case "add":
		repository, remaining, extractErr := extractFlag(args, "--repository")
		if extractErr != nil {
			return fail(options, ExitUsage, extractErr.Error(), nil)
		}
		branch, remaining, extractErr := extractFlag(remaining, "--branch")
		if extractErr != nil {
			return fail(options, ExitUsage, extractErr.Error(), nil)
		}
		if branch == "" {
			branch = "main"
		}
		if repository == "" && len(remaining) == 2 {
			repository = remaining[1]
			remaining = remaining[:1]
		}
		if len(remaining) != 1 || !safeComponent(remaining[0]) || repository == "" || !safeGitBranch(branch) {
			return fail(options, ExitUsage, "skill-sources add requires NAME --repository https://github.com/OWNER/REPO [--branch BRANCH]", nil)
		}
		name := remaining[0]
		for _, source := range config.SkillSources {
			if source.Name == name {
				return fail(options, ExitUsage, "skill source name already exists", nil)
			}
		}
		config.SkillSources = append(config.SkillSources, SkillSourceConfig{Name: name, Repository: repository, Branch: branch})
		if err := saveConfig(config); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if config.Mode == "local" || config.Target != "" {
			ctx, cancel := remoteContext()
			defer cancel()
			raw, fetchErr := newDeployment(config).operation(ctx, "skill-sources", nil, "fetch", name, "--json")
			if fetchErr != nil {
				config.SkillSources = config.SkillSources[:len(config.SkillSources)-1]
				if saveErr := saveConfig(config); saveErr != nil {
					return fail(options, ExitFailure, "source validation failed and configuration rollback failed: "+fetchErr.Error()+"; "+saveErr.Error(), nil)
				}
				return fail(options, ExitFailure, "source was not added: "+fetchErr.Error(), nil)
			}
			return renderSkillOperation(options, raw, "source added and fetched")
		}
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "skill_source": config.SkillSources[len(config.SkillSources)-1]}, "skill source configured: "+name+"; fetch it after initializing OpenLia")
	case "remove":
		if len(args) != 1 || !safeComponent(args[0]) {
			return fail(options, ExitUsage, "skill-sources remove requires a safe source name", nil)
		}
		kept := make([]SkillSourceConfig, 0, len(config.SkillSources))
		found := false
		for _, source := range config.SkillSources {
			if source.Name == args[0] {
				found = true
			} else {
				kept = append(kept, source)
			}
		}
		if !found {
			return fail(options, ExitUsage, "unknown skill source "+args[0], nil)
		}
		if config.Mode == "local" || config.Target != "" {
			ctx, cancel := remoteContext()
			defer cancel()
			raw, operationErr := newDeployment(config).operation(ctx, "skill-sources", nil, "used", args[0], "--json")
			if operationErr != nil {
				return fail(options, ExitFailure, "could not verify installed skills before removing source: "+operationErr.Error(), nil)
			}
			var usage struct {
				InstalledSkills []string `json:"installed_skills"`
			}
			if err := json.Unmarshal(raw, &usage); err != nil {
				return fail(options, ExitFailure, "could not parse installed skill catalog", nil)
			}
			if len(usage.InstalledSkills) > 0 {
				return fail(options, ExitFailure, "skill source is still used by installed skill "+usage.InstalledSkills[0]+"; uninstall it first", nil)
			}
		}
		config.SkillSources = kept
		if err := saveConfig(config); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "removed": args[0]}, "skill source removed: "+args[0])
	case "check", "fetch":
		if len(args) > 1 || (len(args) == 1 && !safeComponent(args[0])) {
			return fail(options, ExitUsage, "skill-sources "+action+" accepts at most one source name", nil)
		}
		ctx, cancel := remoteContext()
		defer cancel()
		opArgs := append([]string{action}, args...)
		opArgs = append(opArgs, "--json")
		raw, err := newDeployment(config).operation(ctx, "skill-sources", nil, opArgs...)
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderSkillOperation(options, raw, "skill source "+action+" completed")
	default:
		return fail(options, ExitUsage, "unknown skill-sources action "+action, nil)
	}
}

func runSkillOperator(options Options, config Config, action string, args []string) int {
	ctx, cancel := remoteContext()
	defer cancel()
	opArgs := append([]string{action}, args...)
	opArgs = append(opArgs, "--json")
	raw, err := newDeployment(config).operation(ctx, "skills", nil, opArgs...)
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return renderSkillOperation(options, raw, "skill "+action+" completed")
}

func renderSkillOperation(options Options, raw []byte, fallback string) int {
	if options.JSON {
		return renderRemote(options, raw, "")
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return renderRemote(options, raw, fallback)
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return renderRemote(options, raw, fallback)
	}
	fmt.Fprintln(os.Stdout, string(formatted))
	return ExitOK
}

func showSkillPlan(options Options, config Config, action, identifier string) (string, int) {
	ctx, cancel := remoteContext()
	defer cancel()
	raw, err := newDeployment(config).operation(ctx, "skills", nil, action, identifier, "--json")
	if err != nil {
		return "", fail(options, ExitFailure, err.Error(), nil)
	}
	if options.JSON {
		var envelope struct {
			Skill struct {
				Commit string `json:"commit"`
			} `json:"skill"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Skill.Commit == "" {
			return "", fail(options, ExitFailure, "could not parse skill installation plan", nil)
		}
		return envelope.Skill.Commit, ExitOK
	}
	var plan struct {
		Action           string `json:"action"`
		InstalledCommit  string `json:"installed_commit"`
		InstalledVersion string `json:"installed_version"`
		Skill            struct {
			Source  string `json:"source"`
			Name    string `json:"name"`
			Commit  string `json:"commit"`
			Version string `json:"version"`
		} `json:"skill"`
		Audit struct {
			Checks []struct {
				Name string `json:"name"`
			} `json:"checks"`
		} `json:"audit"`
	}
	if json.Unmarshal(raw, &plan) != nil {
		return "", fail(options, ExitFailure, "could not parse skill installation plan", nil)
	}
	fmt.Fprintf(os.Stdout, "%s %s/%s\n", titleWord(plan.Action), plan.Skill.Source, plan.Skill.Name)
	if plan.InstalledCommit != "" {
		fmt.Fprintf(os.Stdout, "Current: %s (%s)\n", plan.InstalledVersion, plan.InstalledCommit)
	}
	fmt.Fprintf(os.Stdout, "Proposed: %s (%s)\nAudit: passed (%d checks)\n", plan.Skill.Version, plan.Skill.Commit, len(plan.Audit.Checks))
	return plan.Skill.Commit, ExitOK
}

func removeArgument(args []string, target string) (bool, []string) {
	found := false
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == target {
			found = true
			continue
		}
		result = append(result, arg)
	}
	return found, result
}

func titleWord(value string) string {
	if value == "" {
		return "Skill"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func validExternalSkillIdentifier(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && safeComponent(parts[0]) && safeComponent(parts[1])
}

func validSkillIdentifier(value string) bool {
	return safeComponent(value) || validExternalSkillIdentifier(value)
}

func confirmExact(options Options, prompt, expected string) bool {
	if options.NonInteractive {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s Type %q to continue: ", prompt, expected)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return err == nil && strings.TrimSpace(answer) == expected
}

func approvalExitCode(options Options) int {
	if options.NonInteractive {
		return ExitUsage
	}
	return ExitFailure
}

func commandSkillMigration(options Options, args []string) int {
	if len(args) != 2 || !safeComponent(args[1]) {
		return fail(options, ExitUsage, "skills migrate requires prepare, show, apply, or reject plus a safe name", nil)
	}
	action, identifier := args[0], args[1]
	if action != "prepare" && action != "show" && action != "apply" && action != "reject" {
		return fail(options, ExitUsage, "skills migrate requires prepare, show, apply, or reject", nil)
	}
	if action == "apply" {
		if options.NonInteractive {
			return fail(options, ExitUsage, "migration apply requires interactive confirmation", nil)
		}
		expected := "apply migration " + identifier
		fmt.Fprintf(os.Stderr, "Apply migration proposal %s? Type %q to continue: ", identifier, expected)
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(answer) != expected {
			return fail(options, ExitFailure, "migration apply cancelled", nil)
		}
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	raw, err := newDeployment(config).operation(ctx, "skill-migration", nil, action, identifier, "--json")
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return renderRemote(options, raw, "openlia skills migrate: operation completed")
}

func commandAuth(options Options, args []string) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "auth requires list, setup, or rotate", nil)
	}
	action := args[0]
	args = args[1:]
	if action == "list" {
		if len(args) != 0 {
			return fail(options, ExitUsage, "auth list takes no positional arguments", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		ctx, cancel := remoteContext()
		defer cancel()
		raw, err := newDeployment(config).operation(ctx, "auth", nil, "list", "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, redact(string(raw)))
	}
	if action != "setup" && action != "rotate" {
		return fail(options, ExitUsage, "unknown auth action "+action, nil)
	}
	if len(args) != 0 {
		return fail(options, ExitUsage, "auth "+action+" does not accept arguments; configure [secrets].source instead", nil)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	source := config.SecretSource
	if source == "" {
		if action != "setup" || options.NonInteractive {
			return fail(options, ExitUsage, "auth "+action+" requires [secrets].source; values are never command arguments", nil)
		}
		fmt.Fprint(os.Stderr, "Protected dotenv source path: ")
		input, scanErr := bufio.NewReader(os.Stdin).ReadString('\n')
		if scanErr != nil {
			return fail(options, ExitUsage, "could not read a protected source path", nil)
		}
		source = strings.TrimSpace(input)
		config.SecretSource = source
		if err := validateProtectedSourcePath(source, "secret source"); err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
		if err := saveConfig(config); err != nil {
			return fail(options, ExitFailure, "could not save secret source configuration: "+err.Error(), nil)
		}
	}
	return rotateRemoteFile(options, "auth", source)
}

func commandAttachments(options Options, args []string) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "attachments requires list, map, or rotate", nil)
	}
	action := args[0]
	args = args[1:]
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	if action == "list" {
		if len(args) != 0 {
			return fail(options, ExitUsage, "attachments list takes no positional arguments", nil)
		}
		raw, err := deployment.operation(ctx, "attachments", nil, "list", "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if options.JSON {
			return renderRemote(options, raw, "")
		}
		var result operator.AttachmentListResult
		if err := json.Unmarshal(raw, &result); err == nil {
			return writeResult(options, map[string]any{"schema": 1, "ok": true, "hosts": result.Hosts}, operator.FormatAttachmentListHuman(result))
		}
		return renderRemote(options, raw, redact(string(raw)))
	}
	if action == "map" {
		role, remaining, err := extractFlag(args, "--role")
		if err != nil || role == "" {
			return fail(options, ExitUsage, "attachments map requires [HOST] SERVICE --role ROLE", nil)
		}
		if err := ValidateServiceRole(role); err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
		host := ""
		service := ""
		if len(remaining) == 1 {
			if len(config.Services) == 1 {
				host = config.Services[0].Name
				service = remaining[0]
			} else if len(config.Services) == 0 {
				return fail(options, ExitUsage, "no service hosts configured; declare a [[services]] host first", nil)
			} else {
				return fail(options, ExitUsage, "multiple service hosts configured; specify host: openlia attachments map HOST SERVICE --role ROLE", nil)
			}
		} else if len(remaining) == 2 {
			host = remaining[0]
			service = remaining[1]
		} else {
			return fail(options, ExitUsage, "attachments map requires [HOST] SERVICE --role ROLE", nil)
		}
		if !safeComponent(host) || !safeComponent(service) {
			return fail(options, ExitUsage, "host and service names must be safe identifiers", nil)
		}
		foundIndex := -1
		for i, h := range config.Services {
			if h.Name == host {
				foundIndex = i
				break
			}
		}
		if foundIndex >= 0 {
			if config.Services[foundIndex].Roles == nil {
				config.Services[foundIndex].Roles = make(map[string]string)
			}
			config.Services[foundIndex].Roles[service] = role
		} else {
			config.Services = append(config.Services, ServiceHostConfig{
				Name:  host,
				Roles: map[string]string{service: role},
			})
		}
		if err := saveConfig(config); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		deployment = newDeployment(config)
		raw, err := deployment.operation(ctx, "attachments", nil, "generate", "--json")
		if err != nil {
			return fail(options, ExitFailure, fmt.Sprintf("service role saved, but attachment generation failed: %s", err.Error()), nil)
		}
		return renderRemote(options, raw, fmt.Sprintf("openlia attachments: mapped %s.%s to %s", host, service, role))
	}
	if action == "rotate" {
		host := ""
		source := ""
		sourceVal, remaining, err := extractFlag(args, "--source")
		if err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
		if len(remaining) == 0 {
			if len(config.Services) == 1 && config.Services[0].Name != "" {
				host = config.Services[0].Name
				source = config.Services[0].Source
			} else if len(config.Services) == 0 {
				return fail(options, ExitUsage, "attachments rotate requires HOST --source PATH", nil)
			} else {
				return fail(options, ExitUsage, "multiple service hosts configured; specify HOST: attachments rotate HOST --source PATH", nil)
			}
		} else if len(remaining) == 1 {
			host = remaining[0]
			if !safeComponent(host) {
				return fail(options, ExitUsage, "invalid host name", nil)
			}
			source = sourceVal
			if source == "" {
				for _, h := range config.Services {
					if h.Name == host && h.Source != "" {
						source = h.Source
						break
					}
				}
			}
		} else {
			return fail(options, ExitUsage, "attachments rotate requires [HOST] [--source PATH]", nil)
		}
		if sourceVal != "" {
			source = sourceVal
		}
		if source == "" {
			return fail(options, ExitUsage, "attachments rotate requires --source PATH or source configured in config.toml", nil)
		}
		return rotateRemoteFile(options, "attachments:"+host, source)
	}
	return fail(options, ExitUsage, "attachments requires list, map, or rotate", nil)
}

func commandBackup(options Options, args []string) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "backup requires create or restore", nil)
	}
	action := args[0]
	args = args[1:]
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	switch action {
	case "create":
		if len(args) != 0 {
			return fail(options, ExitUsage, "backup create takes no positional arguments", nil)
		}
		raw, err := deployment.operation(ctx, "backup", nil, "create", "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, redact(string(raw)))
	case "restore":
		archive, err := backupArchiveArgument(args)
		if err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
		if !options.NonInteractive {
			fmt.Fprint(os.Stderr, "Restore the selected OpenLia backup and stop the stack? Type 'yes' to continue: ")
			answer, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
			if readErr != nil || strings.TrimSpace(strings.ToLower(answer)) != "yes" {
				return fail(options, ExitFailure, "backup restore cancelled", nil)
			}
		}
		if archive == "" {
			raw, err := deployment.operation(ctx, "backup", nil, "restore", "--json")
			if err != nil {
				return fail(options, ExitFailure, err.Error(), nil)
			}
			return renderRemote(options, raw, redact(string(raw)))
		}
		if !filepath.IsAbs(archive) || filepath.Base(archive) == "." || !strings.HasSuffix(archive, ".tar.gz") {
			return fail(options, ExitUsage, "backup archive must be an absolute .tar.gz file path", nil)
		}
		if isInsideWorkingTree(archive) {
			return fail(options, ExitUsage, "backup archive must be outside the OpenLia checkout", nil)
		}
		remoteArchive := deployment.rootPath("runtime", "backups", filepath.Base(archive))
		if err := deployment.uploadFile(ctx, archive, remoteArchive, 0o600); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		defer deployment.removeFile(context.Background(), remoteArchive)
		raw, err := deployment.operation(ctx, "backup", nil, "restore", "--archive", remoteArchive, "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, redact(string(raw)))
	default:
		return fail(options, ExitUsage, "unknown backup action "+action, nil)
	}
}

func commandWorkspace(options Options, args []string) int {
	if len(args) == 0 || args[0] != "git" {
		return fail(options, ExitUsage, "workspace requires the git subcommand", nil)
	}
	args = args[1:]
	if len(args) == 0 {
		return fail(options, ExitUsage, "workspace git requires setup or status", nil)
	}
	action := args[0]
	args = args[1:]
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}

	set := newFlagSet("workspace git " + action)
	remote := set.String("remote", "", "HTTPS GitHub repository URL")
	branch := set.String("branch", "", "workspace Git branch")
	schedule := set.String("schedule", "", "workspace Git automatic pull schedule")
	authorName := set.String("author-name", "", "workspace Git commit author name")
	authorEmail := set.String("author-email", "", "workspace Git commit author email")
	if err := set.Parse(args); err != nil {
		return ExitUsage
	}
	if set.NArg() != 0 {
		return fail(options, ExitUsage, "workspace git does not accept positional arguments", nil)
	}

	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	switch action {
	case "status":
		if len(args) != 0 || !config.WorkspaceGit.Enabled {
			return fail(options, ExitUsage, "workspace Git is not configured", nil)
		}
		raw, err := deployment.workspaceGit(ctx, "status", config.WorkspaceGit)
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, redact(string(raw)))
	case "setup":
		if *remote != "" {
			config.WorkspaceGit.Remote = *remote
			config.WorkspaceGit.Enabled = true
			config.EnabledSkills = setSkill(config.EnabledSkills, "workspace-git", true)
		}
		if *branch != "" {
			config.WorkspaceGit.Branch = *branch
		}
		if *schedule != "" {
			config.WorkspaceGit.Schedule = *schedule
		}
		if *authorName != "" {
			config.WorkspaceGit.AuthorName = *authorName
		}
		if *authorEmail != "" {
			config.WorkspaceGit.AuthorEmail = *authorEmail
		}
		if config.WorkspaceGit.Enabled {
			config.EnabledSkills = setSkill(config.EnabledSkills, "workspace-git", true)
		}
		if !config.WorkspaceGit.Enabled {
			return fail(options, ExitUsage, "workspace git setup requires --remote or an existing workspace_git.remote setting", nil)
		}
		if err := validateConfig(config); err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
		if _, err := deployment.operation(ctx, "profile", nil, "sync", "--json"); err != nil {
			return fail(options, ExitFailure, "workspace Git skill synchronization failed: "+err.Error(), nil)
		}
		raw, err := deployment.workspaceGit(ctx, "setup", config.WorkspaceGit)
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if err := saveConfig(config); err != nil {
			return fail(options, ExitInternal, "workspace Git setup succeeded but operator config could not be saved: "+err.Error(), nil)
		}
		return renderRemote(options, raw, "openlia workspace git: setup completed")
	default:
		return fail(options, ExitUsage, "unknown workspace git action "+action, nil)
	}
}

func backupArchiveArgument(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		return args[0], nil
	}
	if len(args) == 2 && args[0] == "--archive" && args[1] != "" {
		return args[1], nil
	}
	return "", errors.New("backup restore accepts an optional archive path or --archive PATH")
}

func requiredPathFlag(args []string, name string) (string, error) {
	for index, arg := range args {
		if arg == name && index+1 < len(args) && args[index+1] != "" {
			return args[index+1], nil
		}
	}
	return "", errors.New("missing path")
}

func requiredValueFlag(args []string, name string) (string, error) {
	for index, arg := range args {
		if arg == name && index+1 < len(args) && args[index+1] != "" {
			return args[index+1], nil
		}
	}
	return "", fmt.Errorf("missing %s", name)
}

func rotateRemoteFile(options Options, kind, source string) int {
	if err := validateProtectedSourcePath(source, "source"); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	var err error
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	remotePath := deployment.rootPath("runtime", "meta", ".openlia-incoming")
	if err := deployment.uploadFile(ctx, source, remotePath, 0o600); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	defer deployment.removeFile(context.Background(), remotePath)
	var raw []byte
	if strings.HasPrefix(kind, "attachments:") {
		host := strings.TrimPrefix(kind, "attachments:")
		raw, err = deployment.operation(ctx, "attachments", nil, "rotate", host, "--source", remotePath, "--json")
	} else {
		raw, err = deployment.operation(ctx, "auth", nil, "rotate", "--source", remotePath, "--json")
	}
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return renderRemote(options, raw, kind+" rotation completed; values were not displayed")
}

func validateProtectedSourcePath(path, label string) error {
	if !filepath.IsAbs(path) || isInsideWorkingTree(path) {
		return fmt.Errorf("%s must be an absolute file outside the OpenLia checkout", label)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%s must be a regular mode-0600 file", label)
	}
	return nil
}

func isInsideWorkingTree(file string) bool {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return false
	}
	workingDirectory, err = filepath.Abs(workingDirectory)
	if err != nil {
		return false
	}
	file, err = filepath.Abs(file)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(workingDirectory, file)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func renderRemote(options Options, raw []byte, human string) int {
	clean := redact(string(raw))
	if options.JSON {
		var payload any
		if err := json.Unmarshal([]byte(clean), &payload); err != nil {
			payload = map[string]any{"schema": 1, "ok": true, "output": clean}
		}
		return writeResult(options, payload, human)
	}
	fmt.Fprint(os.Stdout, clean)
	if human != "" && !strings.HasSuffix(human, "\n") {
		fmt.Fprintln(os.Stdout)
	}
	return ExitOK
}

func skillListHuman(config Config) string {
	lines := make([]string, 0, len(defaultSkills))
	for _, skill := range defaultSkills {
		state := "disabled"
		if contains(config.EnabledSkills, skill) {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", skill, state))
	}
	return strings.Join(lines, "\n")
}

func testSkill(options Options, assets fs.FS, name string) int {
	directory := filepath.ToSlash(filepath.Join("profile", "skills", name, "scripts"))
	entries, err := fs.ReadDir(assets, directory)
	if err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	temporary, err := os.MkdirTemp("", "openlia-skill-*")
	if err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	defer os.RemoveAll(temporary)
	var scriptName string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			data, readErr := fs.ReadFile(assets, filepath.ToSlash(filepath.Join(directory, entry.Name())))
			if readErr != nil {
				return fail(options, ExitInternal, readErr.Error(), nil)
			}
			path := filepath.Join(temporary, entry.Name())
			if writeErr := os.WriteFile(path, data, 0o700); writeErr != nil {
				return fail(options, ExitInternal, writeErr.Error(), nil)
			}
			if scriptName == "" {
				scriptName = path
			}
		}
	}
	if scriptName == "" {
		return fail(options, ExitInternal, "skill has no deterministic helper", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pythonBin := "python3"
	if venv := os.Getenv("VIRTUAL_ENV"); venv != "" {
		candidate := filepath.Join(venv, "bin", "python3")
		if _, statErr := os.Stat(candidate); statErr == nil {
			pythonBin = candidate
		}
	} else if _, statErr := os.Stat(".venv/bin/python3"); statErr == nil {
		pythonBin = ".venv/bin/python3"
	}
	output, err := runLocalCommand(ctx, pythonBin, scriptName, "--self-test")
	if err != nil {
		return fail(options, ExitFailure, "skill test failed: "+err.Error(), map[string]any{"skill": name})
	}
	return writeResult(options, map[string]any{"schema": 1, "ok": true, "skill": name, "output": redact(string(output))}, name+": self-test passed")
}

func setSkill(skills []string, target string, enabled bool) []string {
	result := make([]string, 0, len(skills)+1)
	for _, skill := range skills {
		if skill != target && !contains(result, skill) {
			result = append(result, skill)
		}
	}
	if enabled {
		result = append(result, target)
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func syncAttachmentSource(ctx context.Context, deployment deployment, host, source string) error {
	if err := validateProtectedSourcePath(source, "attachment source"); err != nil {
		return err
	}
	remotePath := deployment.rootPath("runtime", "meta", ".openlia-incoming")
	if err := deployment.uploadFile(ctx, source, remotePath, 0o600); err != nil {
		return err
	}
	defer deployment.removeFile(context.Background(), remotePath)
	_, err := deployment.operation(ctx, "attachments", nil, "rotate", host, "--source", remotePath, "--json")
	return err
}

func extractFlag(args []string, name string) (string, []string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			if i+1 >= len(args) || args[i+1] == "" {
				return "", nil, fmt.Errorf("missing value for %s", name)
			}
			val := args[i+1]
			rem := append([]string(nil), args[:i]...)
			rem = append(rem, args[i+2:]...)
			return val, rem, nil
		}
	}
	return "", args, nil
}
