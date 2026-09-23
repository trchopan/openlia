package operator

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the typed view of the OPENLIA_* settings consumed by the
// operator. Secret values are deliberately not part of Config.
type Config struct {
	RepositoryRoot              string
	RuntimeRoot                 string
	InstallRoot                 string
	ProjectName                 string
	NetworkName                 string
	ComposeFile                 string
	ComposeProjectDir           string
	GeneratedCompose            string
	DataRoot                    string
	SystemSkillsRoot            string
	LochoRoot                   string
	SecretDir                   string
	SecretFile                  string
	BackupRoot                  string
	BackupRetention             int
	MetaRoot                    string
	StateFile                   string
	LocalMode                   bool
	Provider                    string
	Model                       string
	FallbackProviders           []FallbackProviderConfig
	HermesImage                 string
	LochoImage                  string
	EnabledSkills               []string
	SkillsConfigured            bool
	SkillSources                []SkillSourceConfig
	SkillsCacheRoot             string
	SkillsEnvRoot               string
	ExternalNetwork             string
	APIEnabled                  bool
	APIHost                     string
	WorkspaceUIHost             string
	WorkspaceUIPort             int
	WorkspaceUIPublicOrigin     string
	WorkspaceUIAuthRequired     bool
	WorkspaceUIPasswordHashFile string
	OpenWebUIHost               string
	OpenWebUIPort               int
	OpenWebUIImage              string
	OpenWebUIAuth               bool
	OpenWebUIDataRoot           string
	ServiceRoles                map[string]string
	ConfiguredHosts             []string
}

type FallbackProviderConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	KeyEnv   string `json:"key_env,omitempty"`
}

// SkillSourceConfig is transported in OPENLIA_SKILL_SOURCES as a JSON array.
// Example: [{"id":"team","url":"https://github.com/acme/skills.git","ref":"main","manifest":"openlia-skills.json"}].
type SkillSourceConfig struct {
	ID       string `json:"name"`
	URL      string `json:"repository"`
	Ref      string `json:"branch"`
	Manifest string `json:"manifest,omitempty"`
}

func (s *SkillSourceConfig) UnmarshalJSON(data []byte) error {
	var value struct {
		Name, Repository, Branch string
		ID, URL, Ref             string
		Manifest                 string
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.Name != "" && value.ID != "" && value.Name != value.ID || value.Repository != "" && value.URL != "" && value.Repository != value.URL || value.Branch != "" && value.Ref != "" && value.Branch != value.Ref {
		return fmt.Errorf("conflicting canonical and legacy skill source fields")
	}
	s.ID, s.URL, s.Ref, s.Manifest = value.Name, value.Repository, value.Branch, value.Manifest
	if s.ID == "" {
		s.ID = value.ID
	}
	if s.URL == "" {
		s.URL = value.URL
	}
	if s.Ref == "" {
		s.Ref = value.Ref
	}
	return nil
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
	fallbackProviders := []FallbackProviderConfig{}
	if rawFallbacks := values["OPENLIA_FALLBACK_PROVIDERS"]; rawFallbacks != "" {
		if err := json.Unmarshal([]byte(rawFallbacks), &fallbackProviders); err != nil {
			return Config{}, fmt.Errorf("OPENLIA_FALLBACK_PROVIDERS must be valid JSON: %w", err)
		}
	}
	skillSources := []SkillSourceConfig{}
	if rawSources := values["OPENLIA_SKILL_SOURCES"]; rawSources != "" {
		if err := json.Unmarshal([]byte(rawSources), &skillSources); err != nil {
			return Config{}, fmt.Errorf("OPENLIA_SKILL_SOURCES must be valid JSON: %w", err)
		}
	}

	config := Config{
		RepositoryRoot:          repositoryRoot,
		RuntimeRoot:             runtimeRoot,
		InstallRoot:             installRoot,
		ProjectName:             project,
		NetworkName:             getOr(values, "OPENLIA_NETWORK_NAME", project+"-private"),
		ComposeFile:             getOr(values, "OPENLIA_COMPOSE_FILE", filepath.Join(repositoryRoot, "docker", "compose.yaml")),
		ComposeProjectDir:       getOr(values, "OPENLIA_COMPOSE_PROJECT_DIR", filepath.Join(repositoryRoot, "docker")),
		GeneratedCompose:        getOr(values, "OPENLIA_GENERATED_COMPOSE", filepath.Join(repositoryRoot, "docker", "compose.generated.yaml")),
		DataRoot:                getOr(values, "OPENLIA_DATA_ROOT", filepath.Join(runtimeRoot, "hermes")),
		SystemSkillsRoot:        getOr(values, "OPENLIA_SYSTEM_SKILLS_ROOT", filepath.Join(runtimeRoot, "system-skills")),
		LochoRoot:               getOr(values, "OPENLIA_LOCHO_ROOT", filepath.Join(runtimeRoot, "locho")),
		SecretDir:               getOr(values, "OPENLIA_SECRET_DIR", filepath.Join(runtimeRoot, "secrets")),
		SecretFile:              getOr(values, "OPENLIA_SECRET_FILE", filepath.Join(runtimeRoot, "secrets", "hermes.env")),
		BackupRoot:              getOr(values, "OPENLIA_BACKUP_ROOT", filepath.Join(runtimeRoot, "backups")),
		BackupRetention:         5,
		MetaRoot:                getOr(values, "OPENLIA_META_ROOT", filepath.Join(runtimeRoot, "meta")),
		StateFile:               getOr(values, "OPENLIA_STATE_FILE", filepath.Join(runtimeRoot, "meta", "stack-state")),
		Provider:                getOr(values, "OPENLIA_PROVIDER", "copilot"),
		Model:                   getOr(values, "OPENLIA_MODEL", "gpt-5.6-luna"),
		FallbackProviders:       fallbackProviders,
		SkillSources:            skillSources,
		SkillsCacheRoot:         getOr(values, "OPENLIA_SKILLS_CACHE_ROOT", filepath.Join(runtimeRoot, "skill-cache")),
		SkillsEnvRoot:           getOr(values, "OPENLIA_SKILLS_ENV_ROOT", filepath.Join(runtimeRoot, "skill-envs")),
		HermesImage:             getOr(values, "OPENLIA_HERMES_IMAGE", "openlia-hermes:v2026.9.14"),
		LochoImage:              getOr(values, "OPENLIA_LOCHO_IMAGE", "openlia-locho:v1.2.0-beta.1"),
		ExternalNetwork:         values["OPENLIA_EXTERNAL_NETWORK"],
		APIHost:                 getOr(values, "OPENLIA_API_HOST", "127.0.0.1"),
		WorkspaceUIPort:         8089,
		WorkspaceUIPublicOrigin: values["OPENLIA_WORKSPACE_UI_PUBLIC_ORIGIN"],
	}

	if rawRetention := values["OPENLIA_BACKUP_RETENTION"]; rawRetention != "" {
		parsed, err := strconv.Atoi(rawRetention)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("OPENLIA_BACKUP_RETENTION must be a positive integer")
		}
		config.BackupRetention = parsed
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
	config.WorkspaceUIHost = values["OPENLIA_WORKSPACE_UI_HOST"]
	if rawPort := values["OPENLIA_WORKSPACE_UI_PORT"]; rawPort != "" {
		config.WorkspaceUIPort, err = strconv.Atoi(rawPort)
		if err != nil {
			return Config{}, fmt.Errorf("OPENLIA_WORKSPACE_UI_PORT must be an integer")
		}
	}
	if config.WorkspaceUIAuthRequired, err = boolValue(values, "OPENLIA_WORKSPACE_UI_AUTH_REQUIRED", false); err != nil {
		return Config{}, err
	}
	config.WorkspaceUIPasswordHashFile = getOr(values, "OPENLIA_WORKSPACE_UI_PASSWORD_HASH_FILE", filepath.Join(runtimeRoot, "secrets", "workspace-ui-password.hash"))
	if err := validateWorkspaceUIPublicOrigin(config.WorkspaceUIPublicOrigin); err != nil {
		return Config{}, err
	}
	if err := validateWorkspaceUI(config.WorkspaceUIHost, config.WorkspaceUIPort, config.WorkspaceUIAuthRequired, config.WorkspaceUIPasswordHashFile); err != nil {
		return Config{}, err
	}
	openWebUIPort := 8090
	if rawPort := values["OPENLIA_OPEN_WEBUI_PORT"]; rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed < 1 || parsed > 65535 {
			return Config{}, fmt.Errorf("OPENLIA_OPEN_WEBUI_PORT must be between 1 and 65535")
		}
		openWebUIPort = parsed
	}
	openWebUIAuth, err := boolValue(values, "OPENLIA_OPEN_WEBUI_AUTH", true)
	if err != nil {
		return Config{}, err
	}
	config.OpenWebUIHost = values["OPENLIA_OPEN_WEBUI_HOST"]
	config.OpenWebUIPort = openWebUIPort
	config.OpenWebUIImage = getOr(values, "OPENLIA_OPEN_WEBUI_IMAGE", "ghcr.io/open-webui/open-webui:main")
	config.OpenWebUIAuth = openWebUIAuth
	config.OpenWebUIDataRoot = getOr(values, "OPENLIA_OPEN_WEBUI_DATA_ROOT", filepath.Join(runtimeRoot, "open-webui"))
	if err := validateOpenWebUI(config.OpenWebUIHost, config.OpenWebUIPort); err != nil {
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
	config.ServiceRoles = make(map[string]string)
	if rawRoles := values["OPENLIA_SERVICE_ROLES"]; rawRoles != "" {
		var roles map[string]string
		if err := json.Unmarshal([]byte(rawRoles), &roles); err == nil {
			config.ServiceRoles = roles
		}
	}
	if rawHosts := values["OPENLIA_CONFIGURED_HOSTS"]; rawHosts != "" {
		for _, host := range strings.Split(rawHosts, ",") {
			host = strings.TrimSpace(host)
			if host != "" {
				config.ConfiguredHosts = append(config.ConfiguredHosts, host)
			}
		}
	}
	return config, nil
}

func validateWorkspaceUI(host string, port int, authRequired bool, passwordHashFile string) error {
	if host != "" && host != "127.0.0.1" && host != "0.0.0.0" {
		return fmt.Errorf("workspace-ui.host must be 127.0.0.1 or 0.0.0.0")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("workspace-ui.port must be between 1 and 65535")
	}
	if authRequired && host != "0.0.0.0" {
		return fmt.Errorf("workspace-ui authentication is only required for host 0.0.0.0")
	}
	if host == "0.0.0.0" && !authRequired {
		return fmt.Errorf("workspace-ui authentication is required when host is 0.0.0.0")
	}
	if authRequired {
		if err := ValidateAbsolutePath(passwordHashFile, "workspace-ui-password-hash-file"); err != nil {
			return err
		}
	}
	return nil
}

func validateOpenWebUI(host string, port int) error {
	if host == "" {
		return nil
	}
	if host != "127.0.0.1" && host != "0.0.0.0" {
		return fmt.Errorf("open-webui.host must be 127.0.0.1 or 0.0.0.0")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("open-webui.port must be between 1 and 65535")
	}
	return nil
}

func validateWorkspaceUIPublicOrigin(value string) error {
	if value == "" {
		return nil
	}
	origin, err := url.Parse(value)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.User != nil || origin.Opaque != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" {
		return fmt.Errorf("workspace-ui.public_origin must be an absolute http(s) origin")
	}
	return nil
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
	if err := validateWorkspaceUIPublicOrigin(c.WorkspaceUIPublicOrigin); err != nil {
		return err
	}
	if err := validateWorkspaceUI(c.WorkspaceUIHost, c.WorkspaceUIPort, c.WorkspaceUIAuthRequired, c.WorkspaceUIPasswordHashFile); err != nil {
		return err
	}
	if err := validateOpenWebUI(c.OpenWebUIHost, c.OpenWebUIPort); err != nil {
		return err
	}
	if err := validateFallbackProviders(c.FallbackProviders); err != nil {
		return err
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
		{"system-skills-root", c.SystemSkillsRoot},
		{"locho-root", c.LochoRoot},
		{"secret-file", c.SecretFile},
		{"backup-root", c.BackupRoot},
		{"meta-root", c.MetaRoot},
		{"state-file", c.StateFile},
		{"secret-dir", c.SecretDir},
		{"skills-cache-root", c.SkillsCacheRoot},
		{"skills-env-root", c.SkillsEnvRoot},
	}
	if c.OpenWebUIHost != "" {
		paths = append(paths, struct {
			label string
			path  string
		}{"open-webui-data-root", c.OpenWebUIDataRoot})
	}
	if err := validateSkillSources(c.SkillSources); err != nil {
		return err
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

func validateSkillSources(sources []SkillSourceConfig) error {
	seen := make(map[string]bool)
	for index, source := range sources {
		if err := ValidateSafeComponent(source.ID, "skill-source-id"); err != nil {
			return fmt.Errorf("skill source %d: %w", index, err)
		}
		if seen[source.ID] {
			return fmt.Errorf("duplicate skill source id %q", source.ID)
		}
		seen[source.ID] = true
		if err := validateGitHubSkillURL(source.URL); err != nil {
			return fmt.Errorf("skill source %s: %w", source.ID, err)
		}
		if source.Ref == "" || strings.HasPrefix(source.Ref, "-") || strings.ContainsAny(source.Ref, " \\~^:?*[\r\n\t") || strings.Contains(source.Ref, "..") || strings.Contains(source.Ref, "@{") || strings.HasSuffix(source.Ref, ".") || strings.HasSuffix(source.Ref, "/") {
			return fmt.Errorf("skill source %s has an invalid ref", source.ID)
		}
		manifest := source.Manifest
		if manifest == "" {
			manifest = "openlia-skills.json"
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(manifest)))
		if clean != manifest || manifest == "." || strings.HasPrefix(manifest, "/") || strings.HasPrefix(manifest, "../") || strings.Contains(manifest, "/../") {
			return fmt.Errorf("skill source %s has an invalid manifest path", source.ID)
		}
	}
	return nil
}

func validateGitHubSkillURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("URL must be credential-free GitHub HTTPS")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || strings.TrimSuffix(parts[1], ".git") == "" {
		return fmt.Errorf("URL must identify a GitHub owner and repository")
	}
	for _, part := range []string{parts[0], strings.TrimSuffix(parts[1], ".git")} {
		for _, character := range part {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.') {
				return fmt.Errorf("URL contains an invalid GitHub path")
			}
		}
	}
	return nil
}

func validateFallbackProviders(values []FallbackProviderConfig) error {
	for index, fallback := range values {
		if fallback.Provider == "" || (fallback.Provider != "custom" && fallback.Provider != "openai-api" && ValidateSafeComponent(fallback.Provider, "fallback-provider") != nil) {
			return fmt.Errorf("fallback provider %d has an invalid provider", index)
		}
		if fallback.Model == "" {
			return fmt.Errorf("fallback provider %d requires a model", index)
		}
		if fallback.Provider == "custom" && fallback.BaseURL == "" {
			return fmt.Errorf("fallback provider %d requires an explicit base URL", index)
		}
		if fallback.BaseURL != "" {
			parsed, err := url.Parse(fallback.BaseURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("fallback provider %d has an invalid base URL", index)
			}
		}
		if fallback.KeyEnv != "" && !validEnvKey(fallback.KeyEnv) {
			return fmt.Errorf("fallback provider %d has an invalid key_env", index)
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
