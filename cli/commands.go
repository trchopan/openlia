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
	case "auth":
		return commandAuth(options, remaining[1:])
	case "attachments":
		return commandAttachments(options, remaining[1:])
	case "backup":
		return commandBackup(options, remaining[1:])
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
			remaining = append(remaining, "help")
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
	if err := set.Parse(args); err != nil {
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
	sourcePath := config.SecretSource
	if sourcePath != "" {
		if err := validateProtectedSourcePath(sourcePath, "secret source"); err != nil {
			return fail(options, ExitUsage, err.Error(), nil)
		}
	}
	if err := validateConfig(config); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}

	archive, digest, err := releaseArchive(assets)
	if err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	deployment := newDeployment(config)
	ctx, cancel := remoteContext()
	defer cancel()
	if sourcePath != "" {
		if err := deployment.uploadFile(ctx, sourcePath, deployment.rootPath("runtime", "secrets", "hermes.env"), 0o600); err != nil {
			return fail(options, ExitFailure, "could not stage secret source: "+err.Error(), nil)
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
	if _, err := deployment.deploy(ctx, "deploy", true, "all"); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
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
	}, fmt.Sprintf("openlia init: deployed %s in %s mode at %s; workspace preserved when non-empty", config.Version, config.Mode, config.InstallRoot))
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
	if action == "deploy" {
		if _, err := deployment.operation(ctx, "ops/attachments.sh", nil, "generate", "--json"); err != nil {
			return fail(options, ExitFailure, "attachment Compose generation failed: "+err.Error(), nil)
		}
	}
	start := action == "start" || action == "restart"
	raw, err := deployment.deploy(ctx, action, start, "all")
	if err != nil {
		return fail(options, ExitFailure, err.Error(), map[string]any{"action": action})
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
	if component == "openlia" {
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
		raw, err := deployment.operation(ctx, "ops/deploy.sh", nil, "profile", "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, "openlia update openlia: profile assets synchronized")
	}
	// Runtime image updates intentionally reuse the pinned Compose definition.
	// The command does not silently change a tag or digest; operators update the
	// desired pin in their operator config before invoking this boundary.
	if component == "locho" {
		if _, err := deployment.operation(ctx, "ops/attachments.sh", nil, "generate", "--json"); err != nil {
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
		return fail(options, ExitUsage, "skills requires list, show, enable, disable, or test", nil)
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
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "skills": entries}, skillListHuman(config))
	case "show":
		if len(args) != 1 || !safeComponent(args[0]) || !contains(defaultSkills, args[0]) {
			return fail(options, ExitUsage, "skills show requires a known skill name", nil)
		}
		content, err := fs.ReadFile(assets, filepath.ToSlash(filepath.Join("profile", "skills", args[0], "SKILL.md")))
		if err != nil {
			return fail(options, ExitInternal, err.Error(), nil)
		}
		if options.JSON {
			return writeResult(options, map[string]any{"schema": 1, "ok": true, "name": args[0], "content": string(content)}, "")
		}
		fmt.Fprint(os.Stdout, string(content))
		return ExitOK
	case "test":
		if len(args) != 1 || !safeComponent(args[0]) || !contains(defaultSkills, args[0]) {
			return fail(options, ExitUsage, "skills test requires a known skill name", nil)
		}
		return testSkill(options, assets, args[0])
	case "enable", "disable":
		if len(args) != 1 || !safeComponent(args[0]) || !contains(defaultSkills, args[0]) {
			return fail(options, ExitUsage, "skills enable/disable requires a known skill name", nil)
		}
		config, code := configOrError(options)
		if code != ExitOK {
			return code
		}
		config.EnabledSkills = setSkill(config.EnabledSkills, args[0], action == "enable")
		if err := saveConfig(config); err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		if config.Target != "" {
			ctx, cancel := remoteContext()
			defer cancel()
			if _, err := newDeployment(config).operation(ctx, "ops/profile.sh", nil, "sync", "--json"); err != nil {
				return fail(options, ExitFailure, err.Error(), nil)
			}
		}
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "skill": args[0], "enabled": action == "enable"}, fmt.Sprintf("skill %s: %s", args[0], action+"d"))
	default:
		return fail(options, ExitUsage, "unknown skills action "+action, nil)
	}
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
		raw, err := newDeployment(config).operation(ctx, "ops/auth.sh", nil, "list", "--json")
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
	}
	return rotateRemoteFile(options, "auth", source)
}

func commandAttachments(options Options, args []string) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "attachments requires list or rotate", nil)
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
		raw, err := deployment.operation(ctx, "ops/attachments.sh", nil, "list", "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, redact(string(raw)))
	}
	if action != "rotate" || len(args) < 1 || !safeComponent(args[0]) {
		return fail(options, ExitUsage, "attachments rotate requires HOST --source PATH", nil)
	}
	source, err := requiredPathFlag(args[1:], "--source")
	if err != nil {
		return fail(options, ExitUsage, "attachments rotate requires --source PATH", nil)
	}
	return rotateRemoteFile(options, "attachments:"+args[0], source)
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
		raw, err := deployment.operation(ctx, "ops/backup.sh", nil, "create", "--json")
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
			raw, err := deployment.operation(ctx, "ops/backup.sh", nil, "restore", "--json")
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
		raw, err := deployment.operation(ctx, "ops/backup.sh", nil, "restore", "--archive", remoteArchive, "--json")
		if err != nil {
			return fail(options, ExitFailure, err.Error(), nil)
		}
		return renderRemote(options, raw, redact(string(raw)))
	default:
		return fail(options, ExitUsage, "unknown backup action "+action, nil)
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
		raw, err = deployment.operation(ctx, "ops/attachments.sh", nil, "rotate", host, "--source", remotePath, "--json")
	} else {
		raw, err = deployment.operation(ctx, "ops/auth.sh", nil, "rotate", "--source", remotePath, "--json")
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
	var scriptName string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			scriptName = entry.Name()
			break
		}
	}
	if scriptName == "" {
		return fail(options, ExitInternal, "skill has no deterministic helper", nil)
	}
	data, err := fs.ReadFile(assets, filepath.ToSlash(filepath.Join(directory, scriptName)))
	if err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	temporary, err := os.CreateTemp("", "openlia-skill-*.py")
	if err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	nameOnDisk := temporary.Name()
	defer os.Remove(nameOnDisk)
	if err := temporary.Chmod(0o700); err != nil {
		temporary.Close()
		return fail(options, ExitInternal, err.Error(), nil)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fail(options, ExitInternal, err.Error(), nil)
	}
	if err := temporary.Close(); err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	output, err := runLocalCommand(ctx, "python3", nameOnDisk, "--self-test")
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
