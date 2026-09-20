package operator

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the typed view of the OPENLIA_* settings consumed by the
// operator. Secret values are deliberately not part of Config.
type Config struct {
	RepositoryRoot    string
	RuntimeRoot       string
	InstallRoot       string
	ProjectName       string
	NetworkName       string
	ComposeFile       string
	ComposeProjectDir string
	GeneratedCompose  string
	DataRoot          string
	LochoRoot         string
	SecretDir         string
	SecretFile        string
	BackupRoot        string
	MetaRoot          string
	StateFile         string
	LocalMode         bool
	Provider          string
	Model             string
	HermesImage       string
	LochoImage        string
	EnabledSkills     []string
	SkillsConfigured  bool
	ExternalNetwork   string
	APIEnabled        bool
	APIHost           string
}

// LoadConfig reads the process environment.
func LoadConfig() (Config, error) {
	values := make(map[string]string)
	for _, value := range os.Environ() {
		key, item, ok := strings.Cut(value, "=")
		if ok {
			values[key] = item
		}
	}
	return LoadConfigFromEnv(values)
}

// LoadConfigFromEnv is the testable form of LoadConfig. The map contains
// environment names without the surrounding process environment.
func LoadConfigFromEnv(values map[string]string) (Config, error) {
	repositoryRoot := values["OPENLIA_REPO_ROOT"]
	var err error
	if repositoryRoot == "" {
		repositoryRoot, err = os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("determine repository root: %w", err)
		}
	}
	repositoryRoot, err = filepath.Abs(repositoryRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve repository root: %w", err)
	}

	runtimeRoot := strings.TrimRight(getOr(values, "OPENLIA_RUNTIME_ROOT", "/srv/openlia/runtime"), "/")
	if runtimeRoot == "" {
		runtimeRoot = "/"
	}
	installRoot := getOr(values, "OPENLIA_INSTALL_ROOT", strings.TrimSuffix(runtimeRoot, "/runtime"))
	project := getOr(values, "OPENLIA_PROJECT_NAME", "openlia")

	config := Config{
		RepositoryRoot:    repositoryRoot,
		RuntimeRoot:       runtimeRoot,
		InstallRoot:       installRoot,
		ProjectName:       project,
		NetworkName:       getOr(values, "OPENLIA_NETWORK_NAME", project+"-private"),
		ComposeFile:       getOr(values, "OPENLIA_COMPOSE_FILE", filepath.Join(repositoryRoot, "docker", "compose.yaml")),
		ComposeProjectDir: getOr(values, "OPENLIA_COMPOSE_PROJECT_DIR", filepath.Join(repositoryRoot, "docker")),
		GeneratedCompose:  getOr(values, "OPENLIA_GENERATED_COMPOSE", filepath.Join(repositoryRoot, "docker", "compose.generated.yaml")),
		DataRoot:          getOr(values, "OPENLIA_DATA_ROOT", filepath.Join(runtimeRoot, "hermes")),
		LochoRoot:         getOr(values, "OPENLIA_LOCHO_ROOT", filepath.Join(runtimeRoot, "locho")),
		SecretDir:         getOr(values, "OPENLIA_SECRET_DIR", filepath.Join(runtimeRoot, "secrets")),
		SecretFile:        getOr(values, "OPENLIA_SECRET_FILE", filepath.Join(runtimeRoot, "secrets", "hermes.env")),
		BackupRoot:        getOr(values, "OPENLIA_BACKUP_ROOT", filepath.Join(runtimeRoot, "backups")),
		MetaRoot:          getOr(values, "OPENLIA_META_ROOT", filepath.Join(runtimeRoot, "meta")),
		StateFile:         getOr(values, "OPENLIA_STATE_FILE", filepath.Join(runtimeRoot, "meta", "stack-state")),
		Provider:          getOr(values, "OPENLIA_PROVIDER", "openai-api"),
		Model:             getOr(values, "OPENLIA_MODEL", "gpt-5.6-luna"),
		HermesImage:       getOr(values, "OPENLIA_HERMES_IMAGE", "openlia-hermes:v2026.9.14"),
		LochoImage:        getOr(values, "OPENLIA_LOCHO_IMAGE", "openlia-locho:v1.2.0-beta.1"),
		ExternalNetwork:   values["OPENLIA_EXTERNAL_NETWORK"],
		APIHost:           getOr(values, "OPENLIA_API_HOST", "127.0.0.1"),
	}

	if config.LocalMode, err = boolValue(values, "OPENLIA_LOCAL_MODE", false); err != nil {
		return Config{}, err
	}
	if config.SkillsConfigured, err = boolValue(values, "OPENLIA_SKILLS_CONFIGURED", false); err != nil {
		return Config{}, err
	}
	if config.APIEnabled, err = boolValue(values, "OPENLIA_API_ENABLED", false); err != nil {
		return Config{}, err
	}
	if raw := values["OPENLIA_ENABLED_SKILLS"]; raw != "" {
		for _, skill := range strings.Split(raw, ",") {
			if skill == "" {
				return Config{}, fmt.Errorf("OPENLIA_ENABLED_SKILLS contains an empty skill")
			}
			config.EnabledSkills = append(config.EnabledSkills, skill)
		}
	}
	return config, nil
}

func getOr(values map[string]string, key, fallback string) string {
	if value := values[key]; value != "" {
		return value
	}
	return fallback
}

func boolValue(values map[string]string, key string, fallback bool) (bool, error) {
	value, ok := values[key]
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false: %w", key, err)
	}
	return parsed, nil
}

// ValidatePaths applies the path and component constraints used by the shell
// operations before they touch the filesystem.
func (c Config) ValidatePaths() error {
	if err := ValidateAbsolutePath(c.RuntimeRoot, "runtime-root"); err != nil {
		return err
	}
	if err := ValidateSafeComponent(c.ProjectName, "project-name"); err != nil {
		return err
	}
	if err := ValidateSafeComponent(c.NetworkName, "network-name"); err != nil {
		return err
	}
	if c.ExternalNetwork != "" {
		if err := ValidateSafeComponent(c.ExternalNetwork, "external-network"); err != nil {
			return err
		}
	}
	if c.APIHost != "127.0.0.1" {
		return fmt.Errorf("API host must remain 127.0.0.1")
	}
	if err := ValidateAbsolutePath(c.ComposeFile, "compose-file"); err != nil {
		return err
	}
	if err := ValidateAbsolutePath(c.GeneratedCompose, "generated-compose"); err != nil {
		return err
	}
	if err := ValidateAbsolutePath(c.ComposeProjectDir, "compose-project-dir"); err != nil {
		return err
	}

	repositoryRoot, err := filepath.Abs(c.RepositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	if within(c.ComposeFile, filepath.Join(repositoryRoot, "local")) || within(c.GeneratedCompose, filepath.Join(repositoryRoot, "local")) {
		return fmt.Errorf("compose files must not be under local/")
	}
	if tooBroad(c.RuntimeRoot) {
		return fmt.Errorf("runtime-root is too broad: %s", c.RuntimeRoot)
	}
	if within(c.RuntimeRoot, repositoryRoot) {
		return fmt.Errorf("runtime-root must not be inside the repository")
	}
	if err := ValidateAbsolutePath(c.InstallRoot, "install-root"); err != nil {
		return err
	}
	if tooBroad(c.InstallRoot) {
		return fmt.Errorf("install-root is too broad: %s", c.InstallRoot)
	}

	paths := []struct {
		label string
		path  string
	}{
		{"data-root", c.DataRoot},
		{"locho-root", c.LochoRoot},
		{"secret-file", c.SecretFile},
		{"backup-root", c.BackupRoot},
		{"meta-root", c.MetaRoot},
		{"state-file", c.StateFile},
		{"secret-dir", c.SecretDir},
	}
	for _, item := range paths {
		if err := ValidateAbsolutePath(item.path, item.label); err != nil {
			return err
		}
		if !within(item.path, c.RuntimeRoot) {
			return fmt.Errorf("runtime paths must remain below runtime-root")
		}
		if within(item.path, filepath.Join(repositoryRoot, "local")) {
			return fmt.Errorf("runtime path must not touch local/")
		}
	}
	for _, skill := range c.EnabledSkills {
		if err := ValidateSafeComponent(skill, "skill"); err != nil {
			return err
		}
	}
	return nil
}

// ValidateAbsolutePath rejects path syntax that the shell implementation
// treats as unsafe rather than silently cleaning it.
func ValidateAbsolutePath(path, label string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s must be absolute", label)
	}
	if strings.ContainsAny(path, "\n\r\t") {
		return fmt.Errorf("%s contains unsupported whitespace", label)
	}
	if hasUnsafeComponent(path) {
		return fmt.Errorf("%s contains an unsafe path component", label)
	}
	return nil
}

// ValidateSafeComponent validates project, host, skill, and network names.
func ValidateSafeComponent(value, label string) error {
	if value == "" || value == "." || value == ".." || strings.HasPrefix(value, ".") {
		return fmt.Errorf("invalid %s", label)
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-') {
			return fmt.Errorf("invalid %s", label)
		}
	}
	return nil
}

func hasUnsafeComponent(path string) bool {
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == "." || component == ".." {
			return true
		}
	}
	return false
}

func tooBroad(path string) bool {
	switch path {
	case "/", "/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib64", "/media", "/mnt", "/opt", "/proc", "/root", "/run", "/sbin", "/srv", "/sys", "/tmp", "/usr", "/var":
		return true
	default:
		return false
	}
}

func within(path, parent string) bool {
	pathAbs, pathErr := filepath.Abs(path)
	parentAbs, parentErr := filepath.Abs(parent)
	if pathErr != nil || parentErr != nil {
		return false
	}
	relative, err := filepath.Rel(parentAbs, pathAbs)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
