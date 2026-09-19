package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
		if detail != "" {
			return stdout.Bytes(), fmt.Errorf("remote command failed: %s", detail)
		}
		return stdout.Bytes(), fmt.Errorf("remote command failed: %w", err)
	}
	return stdout.Bytes(), nil
}

func (remote Remote) operationCommand(script string, args ...string) string {
	return remote.operationCommandForRoot(remote.releasePath(), script, args...)
}

func (remote Remote) operationCommandForRoot(operationRoot, script string, args ...string) string {
	environment := []string{
		"OPENLIA_LOCAL_MODE='false'",
		"OPENLIA_RUNTIME_ROOT=" + shellQuote(remote.rootPath("runtime")),
		"OPENLIA_INSTALL_ROOT=" + shellQuote(remote.Config.InstallRoot),
		"OPENLIA_PROJECT_NAME=" + shellQuote(remote.Config.Project),
		"OPENLIA_NETWORK_NAME=" + shellQuote(remote.Config.Project+"-private"),
		"OPENLIA_PROVIDER=" + shellQuote(remote.Config.Provider),
		"OPENLIA_EXTERNAL_NETWORK=" + shellQuote(remote.Config.ExternalNetwork),
		"OPENLIA_MODEL=" + shellQuote(remote.Config.Model),
		"HERMES_TIMEZONE=" + shellQuote(remote.Config.Timezone),
		"OPENLIA_HERMES_IMAGE=" + shellQuote(remote.Config.HermesImage),
		"OPENLIA_LOCHO_IMAGE=" + shellQuote(remote.Config.LochoImage),
		"HERMES_BASE_TAG=" + shellQuote(remote.Config.HermesTag),
		"HERMES_BASE_DIGEST=" + shellQuote(remote.Config.HermesDigest),
		"OPENLIA_API_ENABLED=" + shellQuote(strconv.FormatBool(remote.Config.APIEnabled)),
		"OPENLIA_API_HOST=" + shellQuote(remote.Config.APIHost),
		"OPENLIA_DATA_ROOT=" + shellQuote(remote.rootPath("runtime", "hermes")),
		"OPENLIA_LOCHO_ROOT=" + shellQuote(remote.rootPath("runtime", "locho")),
		"OPENLIA_SECRET_FILE=" + shellQuote(remote.rootPath("runtime", "secrets", "hermes.env")),
		"OPENLIA_SECRET_DIR=" + shellQuote(remote.rootPath("runtime", "secrets")),
		"OPENLIA_BACKUP_ROOT=" + shellQuote(remote.rootPath("runtime", "backups")),
		"OPENLIA_META_ROOT=" + shellQuote(remote.rootPath("runtime", "meta")),
		"OPENLIA_COMPOSE_FILE=" + shellQuote(filepath.Join(operationRoot, "docker", "compose.yaml")),
		"OPENLIA_GENERATED_COMPOSE=" + shellQuote(filepath.Join(operationRoot, "docker", "compose.generated.yaml")),
		"OPENLIA_ENABLED_SKILLS=" + shellQuote(strings.Join(remote.Config.EnabledSkills, ",")),
		"OPENLIA_SKILLS_CONFIGURED='true'",
	}
	scriptCommand := shellQuote(filepath.Join(operationRoot, script))
	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, shellQuote(arg))
	}
	arguments := strings.Join(quotedArgs, " ")
	return "if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo -n env " + strings.Join(environment, " ") + " " + scriptCommand + " " + arguments + "; else env " + strings.Join(environment, " ") + " " + scriptCommand + " " + arguments + "; fi"
}

func (remote Remote) operation(ctx context.Context, script string, input []byte, args ...string) ([]byte, error) {
	return remote.ssh(ctx, remote.operationCommand(script, args...), input)
}

func (remote Remote) uninstall(ctx context.Context) ([]byte, error) {
	current := remote.rootPath("current")
	script := filepath.Join(current, "ops", "uninstall.sh")
	command := "if [ -x " + shellQuote(script) + " ]; then " +
		remote.operationCommandForRoot(current, "ops/uninstall.sh", "--json") +
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
	return remote.operation(ctx, "ops/bootstrap.sh", nil, "--json")
}

func (remote Remote) deploy(ctx context.Context, action string, start bool, component string) ([]byte, error) {
	args := []string{action, "--json", "--component", component}
	if start {
		args = append(args, "--start")
	}
	return remote.operation(ctx, "ops/deploy.sh", nil, args...)
}

func (remote Remote) health(ctx context.Context, allowStopped, providerCheck bool) ([]byte, error) {
	args := []string{"--json"}
	if allowStopped {
		args = append(args, "--allow-stopped")
	}
	if providerCheck {
		args = append(args, "--provider-check")
	}
	return remote.operation(ctx, "ops/healthcheck.sh", nil, args...)
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
		"OPENLIA_RUNTIME_ROOT=" + filepath.Join(config.InstallRoot, "runtime"),
		"OPENLIA_INSTALL_ROOT=" + config.InstallRoot,
		"OPENLIA_PROJECT_NAME=" + config.Project,
		"OPENLIA_NETWORK_NAME=" + config.Project + "-private",
		"OPENLIA_PROVIDER=" + config.Provider,
		"OPENLIA_EXTERNAL_NETWORK=" + config.ExternalNetwork,
		"OPENLIA_MODEL=" + config.Model,
		"HERMES_TIMEZONE=" + config.Timezone,
		"OPENLIA_HERMES_IMAGE=" + config.HermesImage,
		"OPENLIA_LOCHO_IMAGE=" + config.LochoImage,
		"HERMES_BASE_TAG=" + config.HermesTag,
		"HERMES_BASE_DIGEST=" + config.HermesDigest,
		"OPENLIA_API_ENABLED=" + strconv.FormatBool(config.APIEnabled),
		"OPENLIA_API_HOST=" + config.APIHost,
		"OPENLIA_DATA_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "hermes"),
		"OPENLIA_LOCHO_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "locho"),
		"OPENLIA_SECRET_FILE=" + filepath.Join(config.InstallRoot, "runtime", "secrets", "hermes.env"),
		"OPENLIA_SECRET_DIR=" + filepath.Join(config.InstallRoot, "runtime", "secrets"),
		"OPENLIA_BACKUP_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "backups"),
		"OPENLIA_META_ROOT=" + filepath.Join(config.InstallRoot, "runtime", "meta"),
		"OPENLIA_COMPOSE_FILE=" + filepath.Join(operationRoot, "docker", "compose.yaml"),
		"OPENLIA_GENERATED_COMPOSE=" + filepath.Join(operationRoot, "docker", "compose.generated.yaml"),
		"OPENLIA_ENABLED_SKILLS=" + strings.Join(config.EnabledSkills, ","),
		"OPENLIA_SKILLS_CONFIGURED=true",
	}
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
	return local.command(ctx, local.releasePath(), script, args...)
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
	return local.operation(ctx, "ops/bootstrap.sh", nil, "--json")
}

func (local Local) deploy(ctx context.Context, action string, start bool, component string) ([]byte, error) {
	args := []string{action, "--json", "--component", component}
	if start {
		args = append(args, "--start")
	}
	return local.operation(ctx, "ops/deploy.sh", nil, args...)
}

func (local Local) health(ctx context.Context, allowStopped, providerCheck bool) ([]byte, error) {
	args := []string{"--json"}
	if allowStopped {
		args = append(args, "--allow-stopped")
	}
	if providerCheck {
		args = append(args, "--provider-check")
	}
	return local.operation(ctx, "ops/healthcheck.sh", nil, args...)
}

func (local Local) uninstall(ctx context.Context) ([]byte, error) {
	return local.command(ctx, local.rootPath("current"), "ops/uninstall.sh", "--json")
}

func (local Local) composeLogs(ctx context.Context, follow bool) ([]byte, error) {
	base := filepath.Join(local.releasePath(), "docker", "compose.yaml")
	generated := filepath.Join(local.releasePath(), "docker", "compose.generated.yaml")
	args := []string{"compose", "--project-name", local.Config.Project, "--project-directory", filepath.Join(local.releasePath(), "docker"), "-f", base}
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
