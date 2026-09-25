package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"openlia/operator"
)

type Remote struct {
	Config Config
}

type deployment interface {
	rootPath(parts ...string) string
	uploadFile(ctx context.Context, source, destination string, mode os.FileMode) error
	removeFile(ctx context.Context, destination string) error
	uploadRelease(ctx context.Context, archive []byte, digest string) error
	activateRelease(ctx context.Context) error
	bootstrap(ctx context.Context) ([]byte, error)
	deploy(ctx context.Context, action string, start bool, component string) ([]byte, error)
	health(ctx context.Context, allowStopped, providerCheck bool) ([]byte, error)
	operation(ctx context.Context, script string, input []byte, args ...string) ([]byte, error)
	workspaceGit(ctx context.Context, action string, config WorkspaceGitConfig) ([]byte, error)
	uninstall(ctx context.Context) ([]byte, error)
	composeLogs(ctx context.Context, follow bool) ([]byte, error)
}

func newDeployment(config Config) deployment {
	if config.Mode == "local" {
		return Local{Config: config}
	}
	return Remote{Config: config}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (remote Remote) rootPath(parts ...string) string {
	items := append([]string{remote.Config.InstallRoot}, parts...)
	return filepath.Join(items...)
}

func (remote Remote) releasePath() string {
	return remote.rootPath("releases", remote.Config.Version)
}

func (remote Remote) ssh(ctx context.Context, command string, input []byte) ([]byte, error) {
	if err := validateTarget(remote.Config.Target); err != nil {
		return nil, err
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return nil, fmt.Errorf("ssh is required: %w", err)
	}
	process := exec.CommandContext(ctx, ssh, "--", remote.Config.Target, command)
	process.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err = process.Run()
	if err != nil {
		detail := strings.TrimSpace(redact(stderr.String()))
		if parsed := operatorError(stdout.Bytes()); parsed != "" {
			detail = parsed
		}
		if detail != "" {
			return stdout.Bytes(), fmt.Errorf("remote command failed: %s", detail)
		}
		return stdout.Bytes(), fmt.Errorf("remote command failed: %w", err)
	}
	return stdout.Bytes(), nil
}

func (remote Remote) operationCommand(operation string, args ...string) string {
	return remote.operationCommandForRoot(remote.releasePath(), operation, args...)
}

func (remote Remote) operationCommandForRoot(operationRoot, operation string, args ...string) string {
	environment := []string{
		"OPENLIA_LOCAL_MODE='false'",
		"OPENLIA_REPO_ROOT=" + shellQuote(operationRoot),
		"OPENLIA_RUNTIME_ROOT=" + shellQuote(remote.rootPath("runtime")),
		"OPENLIA_INSTALL_ROOT=" + shellQuote(remote.Config.InstallRoot),
		"OPENLIA_PROJECT_NAME=" + shellQuote(remote.Config.Project),
		"OPENLIA_NETWORK_NAME=" + shellQuote(remote.Config.Project+"-private"),
		"OPENLIA_PROVIDER=" + shellQuote(remote.Config.Provider),
		"OPENLIA_EXTERNAL_NETWORK=" + shellQuote(remote.Config.ExternalNetwork),
		"OPENLIA_MODEL=" + shellQuote(remote.Config.Model),
		"OPENLIA_OUTPUT_LANGUAGE=" + shellQuote(remote.Config.OutputLanguage),
		"OPENLIA_FALLBACK_PROVIDERS=" + shellQuote(renderFallbackProvidersJSON(remote.Config.FallbackProviders)),
		"HERMES_TIMEZONE=" + shellQuote(remote.Config.Timezone),
		"OPENLIA_HERMES_IMAGE=" + shellQuote(remote.Config.HermesImage),
		"OPENLIA_LOCHO_IMAGE=" + shellQuote(remote.Config.LochoImage),
		"HERMES_BASE_TAG=" + shellQuote(remote.Config.HermesTag),
		"HERMES_BASE_DIGEST=" + shellQuote(remote.Config.HermesDigest),
		"OPENLIA_API_ENABLED=" + shellQuote(strconv.FormatBool(remote.Config.APIEnabled)),
		"OPENLIA_API_HOST=" + shellQuote(remote.Config.APIHost),
		"OPENLIA_WORKSPACE_UI_HOST=" + shellQuote(remote.Config.WorkspaceUIHost),
		"OPENLIA_WORKSPACE_UI_PORT=" + shellQuote(strconv.Itoa(remote.Config.WorkspaceUIPort)),
		"OPENLIA_WORKSPACE_UI_PUBLIC_ORIGIN=" + shellQuote(remote.Config.WorkspaceUIPublicOrigin),
		"OPENLIA_WORKSPACE_UI_AUTH_REQUIRED=" + shellQuote(strconv.FormatBool(remote.Config.WorkspaceUIHost == "0.0.0.0")),
		"OPENLIA_WORKSPACE_UI_PASSWORD_HASH_FILE=" + shellQuote(remote.rootPath("runtime", "secrets", "workspace-ui-password.hash")),
		"OPENLIA_OPEN_WEBUI_HOST=" + shellQuote(remote.Config.OpenWebUIHost),
		"OPENLIA_OPEN_WEBUI_PORT=" + shellQuote(strconv.Itoa(remote.Config.OpenWebUIPort)),
		"OPENLIA_OPEN_WEBUI_IMAGE=" + shellQuote(remote.Config.OpenWebUIImage),
		"OPENLIA_OPEN_WEBUI_AUTH=" + shellQuote(strconv.FormatBool(remote.Config.OpenWebUIAuth)),
		"OPENLIA_DATA_ROOT=" + shellQuote(remote.rootPath("runtime", "hermes")),
		"OPENLIA_SYSTEM_SKILLS_ROOT=" + shellQuote(remote.rootPath("runtime", "system-skills")),
		"OPENLIA_LOCHO_ROOT=" + shellQuote(remote.rootPath("runtime", "locho")),
		"OPENLIA_SECRET_FILE=" + shellQuote(remote.rootPath("runtime", "secrets", "hermes.env")),
		"OPENLIA_SECRET_DIR=" + shellQuote(remote.rootPath("runtime", "secrets")),
		"OPENLIA_BACKUP_ROOT=" + shellQuote(remote.rootPath("runtime", "backups")),
		"OPENLIA_META_ROOT=" + shellQuote(remote.rootPath("runtime", "meta")),
		"OPENLIA_SKILLS_CACHE_ROOT=" + shellQuote(remote.rootPath("runtime", "skill-cache")),
		"OPENLIA_SKILLS_ENV_ROOT=" + shellQuote(remote.rootPath("runtime", "skill-envs")),
		"OPENLIA_COMPOSE_FILE=" + shellQuote(filepath.Join(operationRoot, "docker", "compose.yaml")),
		"OPENLIA_COMPOSE_PROJECT_DIR=" + shellQuote(filepath.Join(operationRoot, "docker")),
		"OPENLIA_GENERATED_COMPOSE=" + shellQuote(filepath.Join(operationRoot, "docker", "compose.generated.yaml")),
		"OPENLIA_ENABLED_SKILLS=" + shellQuote(strings.Join(remote.Config.EnabledSkills, ",")),
		"OPENLIA_SKILLS_CONFIGURED='true'",
		"OPENLIA_SKILL_SOURCES=" + shellQuote(renderSkillSourcesJSON(remote.Config.SkillSources)),
		"OPENLIA_SERVICE_ROLES=" + shellQuote(renderServicesJSON(remote.Config.Services)),
		"OPENLIA_CONFIGURED_HOSTS=" + shellQuote(configuredHosts(remote.Config.Services)),
	}
	legacyScript := legacyOperationScript(operation)
	scriptCommand := shellQuote(filepath.Join(operationRoot, legacyScript))
	if operatorArgs, ok := operatorArguments(operation, args); ok {
		operatorPaths := map[string]string{
			"amd64": filepath.Join(operationRoot, "operator", "linux-amd64", "openlia-operator"),
			"arm64": filepath.Join(operationRoot, "operator", "linux-arm64", "openlia-operator"),
		}
		if operatorOnlyOperation(operation) {
			return "operator_path=''; case \"$(uname -m)\" in x86_64|amd64) operator_path=" + shellQuote(operatorPaths["amd64"]) + ";; aarch64|arm64) operator_path=" + shellQuote(operatorPaths["arm64"]) + ";; esac; if [ -n \"$operator_path\" ] && [ -x \"$operator_path\" ]; then " + privilegedEnvironmentCommand(environment, "\"$operator_path\"", operatorArgs...) + "; else printf '%s\\n' 'OpenLia Go operator is required for this operation' >&2; exit 3; fi"
		}
		return "operator_path=''; case \"$(uname -m)\" in x86_64|amd64) operator_path=" + shellQuote(operatorPaths["amd64"]) + ";; aarch64|arm64) operator_path=" + shellQuote(operatorPaths["arm64"]) + ";; esac; if [ -n \"$operator_path\" ] && [ -x \"$operator_path\" ]; then " + privilegedEnvironmentCommand(environment, "\"$operator_path\"", operatorArgs...) + "; else " + privilegedEnvironmentCommand(environment, scriptCommand, args...) + "; fi"
	}
	return privilegedEnvironmentCommand(environment, scriptCommand, args...)
}

func (remote Remote) operation(ctx context.Context, script string, input []byte, args ...string) ([]byte, error) {
	return remote.ssh(ctx, remote.operationCommand(script, args...), input)
}

func privilegedEnvironmentCommand(environment []string, executable string, args ...string) string {
	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, shellQuote(arg))
	}
	arguments := strings.Join(quotedArgs, " ")
	return "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n env " + strings.Join(environment, " ") + " " + executable + " " + arguments + "; else env " + strings.Join(environment, " ") + " " + executable + " " + arguments + "; fi"
}

func operatorArguments(operation string, args []string) ([]string, bool) {
	var command string
	switch filepath.ToSlash(operation) {
	case "bootstrap":
		command = "bootstrap"
	case "profile":
		command = "profile"
	case "skill-status":
		command = "skill-status"
	case "skill-fork":
		command = "skill-fork"
	case "skill-migration":
		command = "skill-migration"
	case "backup":
		command = "backup"
	case "attachments":
		command = "attachments"
	case "auth":
		command = "auth"
	case "deploy":
		command = "deploy"
	case "healthcheck":
		command = "healthcheck"
	case "workspace-git":
		command = "workspace-git"
	case "uninstall":
		command = "uninstall"
	case "skill-sources":
		command = "skill-sources"
	case "skills":
		command = "skills"
	case "workspace-migrate":
		command = "workspace-migrate"
	case "ops/bootstrap.sh":
		command = "bootstrap"
	case "ops/profile.sh":
		command = "profile"
	case "ops/skill-status.sh":
		command = "skill-status"
	case "ops/backup.sh":
		command = "backup"
	case "ops/attachments.sh":
		command = "attachments"
	case "ops/auth.sh":
		command = "auth"
	case "ops/deploy.sh":
		command = "deploy"
	case "ops/healthcheck.sh":
		command = "healthcheck"
	case "ops/workspace-git.sh":
		command = "workspace-git"
	case "ops/uninstall.sh":
		command = "uninstall"
	default:
		return nil, false
	}
	return append([]string{command}, args...), true
}

func operatorOnlyOperation(operation string) bool {
	switch filepath.ToSlash(operation) {
	case "skill-sources", "skills", "workspace-migrate":
		return true
	default:
		return false
	}
}

func legacyOperationScript(operation string) string {
	switch operation {
	case "bootstrap", "ops/bootstrap.sh":
		return "ops/bootstrap.sh"
	case "profile", "ops/profile.sh":
		return "ops/profile.sh"
	case "skill-status", "ops/skill-status.sh":
		return "ops/skill-status.sh"
	case "skill-fork":
		return "ops/skill-fork.sh"
	case "skill-migration":
		return "ops/skill-migration.sh"
	case "backup", "ops/backup.sh":
		return "ops/backup.sh"
	case "attachments", "ops/attachments.sh":
		return "ops/attachments.sh"
	case "auth", "ops/auth.sh":
		return "ops/auth.sh"
	case "deploy", "ops/deploy.sh":
		return "ops/deploy.sh"
	case "healthcheck", "ops/healthcheck.sh":
		return "ops/healthcheck.sh"
	case "workspace-git", "ops/workspace-git.sh":
		return "ops/workspace-git.sh"
	case "uninstall", "ops/uninstall.sh":
		return "ops/uninstall.sh"
	default:
		return operation
	}
}

func workspaceGitArguments(action string, gitConfig WorkspaceGitConfig) []string {
	return []string{
		action,
		"--remote", gitConfig.Remote,
		"--branch", gitConfig.Branch,
		"--schedule", gitConfig.Schedule,
		"--author-name", gitConfig.AuthorName,
		"--author-email", gitConfig.AuthorEmail,
		"--json",
	}
}

func (remote Remote) workspaceGit(ctx context.Context, action string, gitConfig WorkspaceGitConfig) ([]byte, error) {
	return remote.operation(ctx, "workspace-git", nil, workspaceGitArguments(action, gitConfig)...)
}

func (remote Remote) uninstall(ctx context.Context) ([]byte, error) {
	current := remote.rootPath("current")
	script := filepath.Join(current, "ops", "uninstall.sh")
	operatorAMD64 := filepath.Join(current, "operator", "linux-amd64", "openlia-operator")
	operatorARM64 := filepath.Join(current, "operator", "linux-arm64", "openlia-operator")
	command := "if [ -x " + shellQuote(operatorAMD64) + " ] || [ -x " + shellQuote(operatorARM64) + " ] || [ -x " + shellQuote(script) + " ]; then " +
		remote.operationCommandForRoot(current, "uninstall", "--json") +
		"; else " + privilegedCommand(remote.legacyUninstallCommand(current)) + "; fi"
	return remote.ssh(ctx, command, nil)
}

func privilegedCommand(command string) string {
	return "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n sh -c " + shellQuote(command) + "; else " + command + "; fi"
}

func (remote Remote) legacyUninstallCommand(current string) string {
	installRoot := shellQuote(remote.Config.InstallRoot)
	composeFile := shellQuote(filepath.Join(current, "docker", "compose.yaml"))
	generatedCompose := shellQuote(filepath.Join(current, "docker", "compose.generated.yaml"))
	composeDirectory := shellQuote(filepath.Join(current, "docker"))
	project := shellQuote(remote.Config.Project)
	network := shellQuote(remote.Config.Project + "-private")
	marker := shellQuote(remote.rootPath("runtime", "meta", "runtime.json"))
	composeBase := "docker compose --project-name " + project + " --project-directory " + composeDirectory + " -f " + composeFile
	return "set -eu; " +
		"if [ ! -e " + installRoot + " ]; then printf '%s\\n' '{\"ok\":true,\"action\":\"uninstall\",\"state\":\"absent\",\"images\":\"preserved\"}'; exit 0; fi; " +
		"test -d " + installRoot + "; test ! -L " + installRoot + "; test -f " + marker + "; " +
		"if [ -f " + composeFile + " ]; then if [ -f " + generatedCompose + " ]; then " + composeBase + " -f " + generatedCompose + " down --remove-orphans; else " + composeBase + " down --remove-orphans; fi; else containers=$(docker ps -aq --filter label=com.docker.compose.project=" + project + "); if [ -n \"$containers\" ]; then docker rm -f $containers >/dev/null; fi; fi; " +
		"remaining=$(docker ps -aq --filter label=com.docker.compose.project=" + project + "); test -z \"$remaining\"; " +
		"if docker network inspect " + network + " >/dev/null 2>&1; then network_project=$(docker network inspect " + network + " --format '{{index .Labels \"com.docker.compose.project\"}}' 2>/dev/null || true); if [ \"$network_project\" = " + project + " ]; then docker network rm " + network + " >/dev/null; else test -z \"$network_project\"; fi; fi; " +
		"if docker network inspect " + network + " >/dev/null 2>&1; then exit 1; fi; " +
		"rm -rf -- " + installRoot + "; test ! -e " + installRoot + "; printf '%s\\n' '{\"ok\":true,\"action\":\"uninstall\",\"state\":\"removed\",\"containers\":\"removed\",\"network\":\"removed\",\"images\":\"preserved\"}'"
}

func (remote Remote) uploadRelease(ctx context.Context, archive []byte, digest string) error {
	release := remote.releasePath()
	if !safeVersion(remote.Config.Version) {
		return fmt.Errorf("invalid release version")
	}
	checksum := shellQuote(digest + "\n")
	marker := "{\"schema\":1,\"install_root\":\"" + strings.ReplaceAll(remote.Config.InstallRoot, "\\", "\\\\") + "\",\"project\":\"" + strings.ReplaceAll(remote.Config.Project, "\\", "\\\\") + "\",\"network\":\"" + strings.ReplaceAll(remote.Config.Project+"-private", "\\", "\\\\") + "\"}\n"
	rootCommand := "set -eu; mkdir -p " + shellQuote(release) + " " + shellQuote(remote.rootPath("runtime", "meta")) + "; printf %s " + shellQuote(marker) + " > " + shellQuote(remote.rootPath("runtime", "meta", "runtime.json")) + "; chmod 600 " + shellQuote(remote.rootPath("runtime", "meta", "runtime.json")) + "; tar -xzf - -C " + shellQuote(release) + "; printf %s " + checksum + " > " + shellQuote(filepath.Join(release, "release.sha256")) + "; chmod 600 " + shellQuote(filepath.Join(release, "release.sha256"))
	command := "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n sh -c " + shellQuote(rootCommand) + "; else " + rootCommand + "; fi"
	_, err := remote.ssh(ctx, command, archive)
	return err
}

func (remote Remote) activateRelease(ctx context.Context) error {
	current := remote.rootPath("current")
	marker := "{\"schema\":1,\"install_root\":\"" + strings.ReplaceAll(remote.Config.InstallRoot, "\\", "\\\\") + "\",\"project\":\"" + strings.ReplaceAll(remote.Config.Project, "\\", "\\\\") + "\",\"network\":\"" + strings.ReplaceAll(remote.Config.Project+"-private", "\\", "\\\\") + "\"}\n"
	rootCommand := "set -eu; mkdir -p " + shellQuote(remote.rootPath("releases")) + " " + shellQuote(remote.rootPath("runtime", "meta")) + "; ln -sfn " + shellQuote(remote.releasePath()) + " " + shellQuote(current) + "; printf %s " + shellQuote(marker) + " > " + shellQuote(remote.rootPath("runtime", "meta", "runtime.json")) + "; chmod 600 " + shellQuote(remote.rootPath("runtime", "meta", "runtime.json"))
	command := "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n sh -c " + shellQuote(rootCommand) + "; else " + rootCommand + "; fi"
	_, err := remote.ssh(ctx, command, nil)
	return err
}

func (remote Remote) bootstrap(ctx context.Context) ([]byte, error) {
	return remote.operation(ctx, "bootstrap", nil, "--json")
}

func (remote Remote) deploy(ctx context.Context, action string, start bool, component string) ([]byte, error) {
	args := []string{action, "--json", "--component", component}
	if start {
		args = append(args, "--start")
	}
	return remote.operation(ctx, "deploy", nil, args...)
}

func (remote Remote) health(ctx context.Context, allowStopped, providerCheck bool) ([]byte, error) {
	args := []string{"--json"}
	if allowStopped {
		args = append(args, "--allow-stopped")
	}
	if providerCheck {
		args = append(args, "--provider-check")
	}
	return remote.operation(ctx, "healthcheck", nil, args...)
}

func (remote Remote) composeLogs(ctx context.Context, follow bool) ([]byte, error) {
	base := filepath.Join(remote.releasePath(), "docker", "compose.yaml")
	generated := filepath.Join(remote.releasePath(), "docker", "compose.generated.yaml")
	command := "set -eu; compose='docker compose --project-name " + shellQuote(remote.Config.Project) + " --project-directory " + shellQuote(filepath.Join(remote.releasePath(), "docker")) + " -f " + shellQuote(base) + "'; if [ -f " + shellQuote(generated) + " ]; then compose=\"$compose -f " + shellQuote(generated) + "\"; fi; $compose logs --tail 200"
	if follow {
		command += " --follow"
	}
	return remote.ssh(ctx, command, nil)
}

func (remote Remote) uploadFile(ctx context.Context, source, destination string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read upload source: %w", err)
	}
	if err := validateAbsoluteRoot(destination, "upload destination"); err != nil {
		return err
	}
	temporary := destination + ".tmp-openlia"
	rootCommand := "set -eu; mkdir -p " + shellQuote(filepath.Dir(destination)) + "; umask 077; cat > " + shellQuote(temporary) + "; chmod " + shellQuote(fmt.Sprintf("%o", mode.Perm())) + " " + shellQuote(temporary) + "; mv -f " + shellQuote(temporary) + " " + shellQuote(destination)
	command := "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n sh -c " + shellQuote(rootCommand) + "; else " + rootCommand + "; fi"
	_, err = remote.ssh(ctx, command, data)
	return err
}

type Local struct {
	Config Config
}

func (local Local) rootPath(parts ...string) string {
	items := append([]string{local.Config.InstallRoot}, parts...)
	return filepath.Join(items...)
}

func operationEnvironment(config Config, operationRoot string) []string {
	return []string{
		"OPENLIA_LOCAL_MODE=true",
		"OPENLIA_REPO_ROOT=" + operationRoot,
		"OPENLIA_RUNTIME_ROOT=" + filepath.Join(config.InstallRoot, "runtime"),
		"OPENLIA_INSTALL_ROOT=" + config.InstallRoot,
		"OPENLIA_PROJECT_NAME=" + config.Project,
		"OPENLIA_NETWORK_NAME=" + config.Project + "-private",
		"OPENLIA_PROVIDER=" + config.Provider,
		"OPENLIA_EXTERNAL_NETWORK=" + config.ExternalNetwork,
		"OPENLIA_MODEL=" + config.Model,
		"OPENLIA_OUTPUT_LANGUAGE=" + config.OutputLanguage,
		"OPENLIA_FALLBACK_PROVIDERS=" + renderFallbackProvidersJSON(config.FallbackProviders),
		"HERMES_TIMEZONE=" + config.Timezone,
		"OPENLIA_HERMES_IMAGE=" + config.HermesImage,
		"OPENLIA_LOCHO_IMAGE=" + config.LochoImage,
		"HERMES_BASE_TAG=" + config.HermesTag,
		"HERMES_BASE_DIGEST=" + config.HermesDigest,
		"OPENLIA_API_ENABLED=" + strconv.FormatBool(config.APIEnabled),
		"OPENLIA_API_HOST=" + config.APIHost,
		"OPENLIA_WORKSPACE_UI_HOST=" + config.WorkspaceUIHost,
		"OPENLIA_WORKSPACE_UI_PORT=" + strconv.Itoa(config.WorkspaceUIPort),
		"OPENLIA_WORKSPACE_UI_PUBLIC_ORIGIN=" + config.WorkspaceUIPublicOrigin,
		"OPENLIA_WORKSPACE_UI_AUTH_REQUIRED=" + strconv.FormatBool(config.WorkspaceUIHost == "0.0.0.0"),
		"OPENLIA_WORKSPACE_UI_PASSWORD_HASH_FILE=" + filepath.Join(config.InstallRoot, "runtime", "secrets", "workspace-ui-password.hash"),
		"OPENLIA_OPEN_WEBUI_HOST=" + config.OpenWebUIHost,
		"OPENLIA_OPEN_WEBUI_PORT=" + strconv.Itoa(config.OpenWebUIPort),
		"OPENLIA_OPEN_WEBUI_IMAGE=" + config.OpenWebUIImage,
		"OPENLIA_OPEN_WEBUI_AUTH=" + strconv.FormatBool(config.OpenWebUIAuth),
		"OPENLIA_DATA_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "hermes"),
		"OPENLIA_SYSTEM_SKILLS_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "system-skills"),
		"OPENLIA_LOCHO_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "locho"),
		"OPENLIA_SECRET_FILE=" + filepath.Join(config.InstallRoot, "runtime", "secrets", "hermes.env"),
		"OPENLIA_SECRET_DIR=" + filepath.Join(config.InstallRoot, "runtime", "secrets"),
		"OPENLIA_BACKUP_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "backups"),
		"OPENLIA_META_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "meta"),
		"OPENLIA_SKILLS_CACHE_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "skill-cache"),
		"OPENLIA_SKILLS_ENV_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "skill-envs"),
		"OPENLIA_COMPOSE_FILE=" + filepath.Join(operationRoot, "docker", "compose.yaml"),
		"OPENLIA_COMPOSE_PROJECT_DIR=" + filepath.Join(operationRoot, "docker"),
		"OPENLIA_GENERATED_COMPOSE=" + filepath.Join(operationRoot, "docker", "compose.generated.yaml"),
		"OPENLIA_ENABLED_SKILLS=" + strings.Join(config.EnabledSkills, ","),
		"OPENLIA_SKILLS_CONFIGURED=true",
		"OPENLIA_SKILL_SOURCES=" + renderSkillSourcesJSON(config.SkillSources),
		"OPENLIA_SERVICE_ROLES=" + renderServicesJSON(config.Services),
		"OPENLIA_CONFIGURED_HOSTS=" + configuredHosts(config.Services),
	}
}

func renderFallbackProvidersJSON(values []FallbackProviderConfig) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func renderSkillSourcesJSON(values []SkillSourceConfig) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func configuredHosts(services []ServiceHostConfig) string {
	if len(services) == 0 {
		return ""
	}
	hosts := make([]string, 0, len(services))
	for _, host := range services {
		if host.Name != "" {
			hosts = append(hosts, host.Name)
		}
	}
	return strings.Join(hosts, ",")
}

func renderServicesJSON(services []ServiceHostConfig) string {
	if len(services) == 0 {
		return "{}"
	}
	m := make(map[string]string)
	for _, host := range services {
		for svc, role := range host.Roles {
			m[svc] = role
			if host.Name != "" {
				m[host.Name+"."+svc] = role
			}
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (local Local) command(ctx context.Context, operationRoot, script string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "bash", append([]string{filepath.Join(operationRoot, script)}, args...)...)
	command.Env = append(os.Environ(), operationEnvironment(local.Config, operationRoot)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(redact(stderr.String()))
		if detail != "" {
			return stdout.Bytes(), fmt.Errorf("local command failed: %s", detail)
		}
		return stdout.Bytes(), fmt.Errorf("local command failed: %w", err)
	}
	return stdout.Bytes(), nil
}

func (local Local) operation(ctx context.Context, script string, input []byte, args ...string) ([]byte, error) {
	if len(input) != 0 {
		return nil, fmt.Errorf("local operations do not accept stdin")
	}
	if operatorArgs, ok := operatorArguments(script, args); ok {
		return local.operator(ctx, local.releasePath(), operatorArgs...)
	}
	return local.command(ctx, local.releasePath(), script, args...)
}

func (local Local) operator(ctx context.Context, operationRoot string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previous := make(map[string]string)
	present := make(map[string]bool)
	for _, value := range operationEnvironment(local.Config, operationRoot) {
		key, item, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		previous[key] = os.Getenv(key)
		_, present[key] = os.LookupEnv(key)
		if err := os.Setenv(key, item); err != nil {
			return nil, err
		}
	}
	defer func() {
		for key, value := range previous {
			if present[key] {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}()

	var stdout, stderr bytes.Buffer
	code := operator.RunContext(ctx, args, nil, &stdout, &stderr)
	if code != operator.ExitOK {
		detail := strings.TrimSpace(redact(stderr.String()))
		if parsed := operatorError(stdout.Bytes()); parsed != "" {
			detail = parsed
		} else if detail == "" {
			detail = strings.TrimSpace(redact(stdout.String()))
		}
		if detail == "" {
			detail = fmt.Sprintf("operator exited with status %d", code)
		}
		return stdout.Bytes(), fmt.Errorf("local operator failed: %s", detail)
	}
	return stdout.Bytes(), nil
}

func operatorError(data []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &payload) == nil && payload.Error != "" {
		return redact(payload.Error)
	}
	return ""
}

func (local Local) workspaceGit(ctx context.Context, action string, gitConfig WorkspaceGitConfig) ([]byte, error) {
	return local.operation(ctx, "workspace-git", nil, workspaceGitArguments(action, gitConfig)...)
}

func (local Local) releasePath() string {
	return local.rootPath("releases", local.Config.Version)
}

func (local Local) uploadRelease(ctx context.Context, archive []byte, digest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !safeVersion(local.Config.Version) {
		return fmt.Errorf("invalid release version")
	}
	actual := sha256.Sum256(archive)
	if fmt.Sprintf("%x", actual) != digest {
		return fmt.Errorf("release archive digest mismatch")
	}
	release := local.releasePath()
	if err := os.MkdirAll(release, 0o700); err != nil {
		return fmt.Errorf("create local release: %w", err)
	}
	meta := local.rootPath("runtime", "meta")
	if err := os.MkdirAll(meta, 0o700); err != nil {
		return fmt.Errorf("create local metadata directory: %w", err)
	}
	marker := fmt.Sprintf("{\"schema\":1,\"install_root\":%q,\"project\":%q,\"network\":%q}\n", local.Config.InstallRoot, local.Config.Project, local.Config.Project+"-private")
	if err := os.WriteFile(filepath.Join(meta, "runtime.json"), []byte(marker), 0o600); err != nil {
		return fmt.Errorf("write local installation marker: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read release archive: %w", nextErr)
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("release archive contains unsafe path")
		}
		destination := filepath.Join(release, name)
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("release archive contains unsupported entry")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create release directory: %w", err)
		}
		file, createErr := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
		if createErr != nil {
			return fmt.Errorf("create release file: %w", createErr)
		}
		_, copyErr := io.Copy(file, tarReader)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("write release file: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close release file: %w", closeErr)
		}
	}
	return os.WriteFile(filepath.Join(release, "release.sha256"), []byte(digest+"\n"), 0o600)
}

func (local Local) activateRelease(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(local.rootPath("releases"), 0o700); err != nil {
		return fmt.Errorf("create local releases directory: %w", err)
	}
	current := local.rootPath("current")
	if info, err := os.Lstat(current); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("local current path is not a symlink")
		}
		if err := os.Remove(current); err != nil {
			return fmt.Errorf("replace local current link: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect local current path: %w", err)
	}
	if err := os.Symlink(local.releasePath(), current); err != nil {
		return err
	}
	return nil
}

func (local Local) bootstrap(ctx context.Context) ([]byte, error) {
	return local.operation(ctx, "bootstrap", nil, "--json")
}

func (local Local) deploy(ctx context.Context, action string, start bool, component string) ([]byte, error) {
	args := []string{action, "--json", "--component", component}
	if start {
		args = append(args, "--start")
	}
	return local.operation(ctx, "deploy", nil, args...)
}

func (local Local) health(ctx context.Context, allowStopped, providerCheck bool) ([]byte, error) {
	args := []string{"--json"}
	if allowStopped {
		args = append(args, "--allow-stopped")
	}
	if providerCheck {
		args = append(args, "--provider-check")
	}
	return local.operation(ctx, "healthcheck", nil, args...)
}

func (local Local) uninstall(ctx context.Context) ([]byte, error) {
	return local.operator(ctx, local.rootPath("current"), "uninstall", "--json")
}

func (local Local) composeLogs(ctx context.Context, follow bool) ([]byte, error) {
	base := filepath.Join(local.releasePath(), "docker", "compose.yaml")
	generated := filepath.Join(local.releasePath(), "docker", "compose.generated.yaml")
	args := []string{"docker", "compose", "--project-name", local.Config.Project, "--project-directory", filepath.Join(local.releasePath(), "docker"), "-f", base}
	if _, err := os.Stat(generated); err == nil {
		args = append(args, "-f", generated)
	}
	args = append(args, "logs", "--tail", "200")
	if follow {
		args = append(args, "--follow")
	}
	return local.run(ctx, args...)
}

func (local Local) run(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Env = append(os.Environ(), operationEnvironment(local.Config, local.releasePath())...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(redact(stderr.String()))
		if detail != "" {
			return stdout.Bytes(), fmt.Errorf("local command failed: %s", detail)
		}
		return stdout.Bytes(), fmt.Errorf("local command failed: %w", err)
	}
	return stdout.Bytes(), nil
}

func (local Local) uploadFile(ctx context.Context, source, destination string, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateAbsoluteRoot(destination, "upload destination"); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read upload source: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create upload directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".openlia-upload-*")
	if err != nil {
		return fmt.Errorf("create upload temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode.Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write upload: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("activate upload: %w", err)
	}
	return nil
}

func (local Local) removeFile(ctx context.Context, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateAbsoluteRoot(destination, "local file"); err != nil {
		return err
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (remote Remote) removeFile(ctx context.Context, destination string) error {
	if err := validateAbsoluteRoot(destination, "remote file"); err != nil {
		return err
	}
	command := "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n rm -f -- " + shellQuote(destination) + "; else rm -f -- " + shellQuote(destination) + "; fi"
	_, err := remote.ssh(ctx, command, nil)
	return err
}

func remoteContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Minute)
}

func runLocalCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s", name, strings.TrimSpace(redact(stderr.String())))
	}
	return stdout.Bytes(), nil
}
