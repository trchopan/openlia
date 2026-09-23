package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"
)

const (
	workspaceUIPasswordMemoryKiB = 64 * 1024
	workspaceUIPasswordTimeCost  = 3
	workspaceUIPasswordParallel  = 1
	workspaceUIPasswordSaltBytes = 16
	workspaceUIPasswordKeyBytes  = 32
	workspaceUIPasswordMinRunes  = 12
)

func validateWorkspaceUIPasswordHash(value string) error {
	parts := strings.Split(value, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return fmt.Errorf("workspace-ui.password_hash must be an Argon2id PHC string")
	}
	params := make(map[string]uint32, 3)
	for _, item := range strings.Split(parts[3], ",") {
		parameter, raw, ok := strings.Cut(item, "=")
		if !ok || (parameter != "m" && parameter != "t" && parameter != "p") || raw == "" {
			return fmt.Errorf("workspace-ui.password_hash has invalid Argon2id parameters")
		}
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || parsed == 0 {
			return fmt.Errorf("workspace-ui.password_hash has invalid Argon2id parameters")
		}
		params[parameter] = uint32(parsed)
	}
	if len(params) != 3 || params["m"] < 32*1024 || params["m"] > 128*1024 || params["t"] < 2 || params["t"] > 5 || params["p"] < 1 || params["p"] > 4 {
		return fmt.Errorf("workspace-ui.password_hash Argon2id parameters are outside the supported limits")
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[4])
	derivedKey, keyErr := base64.RawStdEncoding.DecodeString(parts[5])
	invalidSalt := saltErr != nil || len(salt) < workspaceUIPasswordSaltBytes
	invalidDigest := keyErr != nil || len(derivedKey) != workspaceUIPasswordKeyBytes
	if invalidSalt || invalidDigest {
		return fmt.Errorf("workspace-ui.password_hash has invalid Argon2id data")
	}
	return nil
}

func hashWorkspaceUIPassword(value string) (string, error) {
	if utf8.RuneCountInString(value) < workspaceUIPasswordMinRunes {
		return "", fmt.Errorf("workspace-ui password must be at least %d characters", workspaceUIPasswordMinRunes)
	}
	salt := make([]byte, workspaceUIPasswordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate workspace-ui password salt: %w", err)
	}
	derivedKey := argon2.IDKey([]byte(value), salt, workspaceUIPasswordTimeCost, workspaceUIPasswordMemoryKiB, workspaceUIPasswordParallel, workspaceUIPasswordKeyBytes)
	hash := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", workspaceUIPasswordMemoryKiB, workspaceUIPasswordTimeCost, workspaceUIPasswordParallel, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derivedKey))
	return hash, validateWorkspaceUIPasswordHash(hash)
}

func readWorkspaceUIPassword(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("workspace-ui password setup requires an interactive terminal")
	}
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read workspace-ui password: %w", err)
	}
	return string(value), nil
}

func provisionWorkspaceUIPassword(ctx context.Context, deployment deployment, config Config) error {
	if config.WorkspaceUIHost != "0.0.0.0" {
		return nil
	}
	if err := validateWorkspaceUIPasswordHash(config.WorkspaceUIPasswordHash); err != nil {
		return err
	}
	temporary, err := os.CreateTemp("", ".openlia-workspace-ui-password-*")
	if err != nil {
		return fmt.Errorf("create password verifier: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(config.WorkspaceUIPasswordHash + "\n"); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write password verifier: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := deployment.uploadFile(ctx, temporaryName, deployment.rootPath("runtime", "secrets", "workspace-ui-password.hash"), 0o444); err != nil {
		return fmt.Errorf("upload password verifier: %w", err)
	}
	return nil
}

func rollbackWorkspaceUIPassword(ctx context.Context, deployment deployment, config Config, previousHash string) {
	if previousHash != "" {
		config.WorkspaceUIPasswordHash = previousHash
		if err := provisionWorkspaceUIPassword(ctx, deployment, config); err != nil {
			return
		}
		if _, err := deployment.operation(ctx, "attachments", nil, "generate", "--json"); err != nil {
			return
		}
		_, _ = deployment.deploy(ctx, "deploy", false, "workspace-ui")
		return
	}
	_ = deployment.removeFile(ctx, deployment.rootPath("runtime", "secrets", "workspace-ui-password.hash"))
}

func commandWorkspaceUI(options Options, args []string) int {
	if len(args) != 1 || args[0] != "password" {
		return fail(options, ExitUsage, "workspace-ui requires the password subcommand", nil)
	}
	if options.NonInteractive {
		return fail(options, ExitUsage, "workspace-ui password requires an interactive terminal", nil)
	}
	config, err := loadConfigUnchecked()
	if errors.Is(err, os.ErrNotExist) {
		return fail(options, ExitPrereq, "workspace-ui password requires an existing config.toml", nil)
	}
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if config.WorkspaceUIHost == "" {
		return fail(options, ExitUsage, "workspace-ui must be enabled before setting a password", nil)
	}
	first, err := readWorkspaceUIPassword("New workspace-ui password: ")
	if err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	second, err := readWorkspaceUIPassword("Repeat workspace-ui password: ")
	if err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	if first != second {
		return fail(options, ExitUsage, "workspace-ui passwords do not match", nil)
	}
	hash, err := hashWorkspaceUIPassword(first)
	if err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	previousHash := config.WorkspaceUIPasswordHash
	config.WorkspaceUIPasswordHash = hash
	if err := validateConfig(config); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	ctx, cancel := remoteContext()
	defer cancel()
	deployment := newDeployment(config)
	if err := provisionWorkspaceUIPassword(ctx, deployment, config); err != nil {
		return fail(options, ExitFailure, "workspace-ui password provisioning failed: "+err.Error(), nil)
	}
	if _, err := deployment.operation(ctx, "attachments", nil, "generate", "--json"); err != nil {
		rollbackWorkspaceUIPassword(ctx, deployment, config, previousHash)
		return fail(options, ExitFailure, "workspace-ui Compose generation failed: "+err.Error(), nil)
	}
	raw, err := deployment.deploy(ctx, "deploy", false, "workspace-ui")
	if err != nil {
		rollbackWorkspaceUIPassword(ctx, deployment, config, previousHash)
		return fail(options, ExitFailure, "workspace-ui restart failed: "+err.Error(), nil)
	}
	if err := saveConfig(config); err != nil {
		rollbackWorkspaceUIPassword(ctx, deployment, config, previousHash)
		return fail(options, ExitFailure, "workspace-ui password was deployed but config.toml could not be saved: "+err.Error(), nil)
	}
	return renderRemote(options, raw, "workspace-ui password updated; the previous password is no longer valid")
}
