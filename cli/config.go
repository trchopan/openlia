package cli

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	configSchema           = 1
	defaultVersion         = "0.1.0"
	defaultRemoteRoot      = "/srv/openlia"
	defaultLocalRoot       = ".openlia"
	defaultProject         = "openlia"
	defaultOutputLanguage  = "en"
	defaultTimezone        = "Asia/Ho_Chi_Minh"
	defaultWorkspaceUIPort = 8089
	defaultOpenWebUIPort   = 8090
	defaultOpenWebUIImage  = "ghcr.io/open-webui/open-webui:main"
)

var defaultSkills = []string{
	"inbox-triage",
	"daily-briefing",
	"weekly-review",
	"project-review",
	"decision-analysis",
	"deep-research",
	"personal-finance",
	"workspace-git",
	"claim-review",
}

const (
	RoleOpenLIABrowser = "openlia-browser"
	RoleOpenAIGateway  = "openai-gateway"
)

var allowedServiceRoles = map[string]bool{
	RoleOpenLIABrowser: true,
	RoleOpenAIGateway:  true,
}

func ValidateServiceRole(role string) error {
	if !allowedServiceRoles[role] {
		return fmt.Errorf("invalid service role %q; must be one of: %s, %s", role, RoleOpenLIABrowser, RoleOpenAIGateway)
	}
	return nil
}

type Config struct {
	Schema                  int
	Version                 string
	Mode                    string
	Target                  string
	InstallRoot             string
	Project                 string
	Model                   string
	OutputLanguage          string
	FallbackProviders       []FallbackProviderConfig
	Timezone                string
	Provider                string
	ExternalNetwork         string
	HermesImage             string
	HermesTag               string
	HermesDigest            string
	LochoImage              string
	LochoVersion            string
	APIEnabled              bool
	APIHost                 string
	WorkspaceUIHost         string
	WorkspaceUIPort         int
	WorkspaceUIPublicOrigin string
	WorkspaceUIPasswordHash string
	OpenWebUIHost           string
	OpenWebUIPort           int
	OpenWebUIImage          string
	OpenWebUIAuth           bool
	SecretSource            string
	ReleaseSource           string
	EnabledSkills           []string
	WorkspaceGit            WorkspaceGitConfig
	SkillSources            []SkillSourceConfig
	Services                []ServiceHostConfig
	OpenLIABrowser          OpenLIABrowserConfig
}

type SkillSourceConfig struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
}

type ServiceHostConfig struct {
	Name   string
	Source string
	Roles  map[string]string
}

type FallbackProviderConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	KeyEnv   string `json:"key_env,omitempty"`
}

type WorkspaceGitConfig struct {
	Enabled     bool
	Provider    string
	Remote      string
	Branch      string
	Schedule    string
	AuthorName  string
	AuthorEmail string
}

type OpenLIABrowserConfig struct {
	Configured         bool
	Mode               string
	Target             string
	SSHPort            int
	Root               string
	ExtensionTokenFile string
}

func defaultConfig() Config {
	return Config{
		Schema:          configSchema,
		Version:         defaultVersion,
		Mode:            "ssh",
		InstallRoot:     defaultRemoteRoot,
		Project:         defaultProject,
		Model:           "gpt-5.6-luna",
		OutputLanguage:  defaultOutputLanguage,
		Timezone:        defaultTimezone,
		Provider:        "copilot",
		HermesImage:     "openlia-hermes:v2026.9.14",
		HermesTag:       "v2026.9.14",
		HermesDigest:    "sha256:99641e57ec762c59e54cb44aa6746b7fc68c18b3c5ddb088af54234c613d9294",
		LochoImage:      "openlia-locho:v1.2.0-beta.1",
		LochoVersion:    "1.2.0-beta.1",
		APIHost:         "127.0.0.1",
		WorkspaceUIHost: "",
		WorkspaceUIPort: defaultWorkspaceUIPort,
		OpenWebUIHost:   "",
		OpenWebUIPort:   defaultOpenWebUIPort,
		OpenWebUIImage:  defaultOpenWebUIImage,
		OpenWebUIAuth:   true,
		EnabledSkills:   append([]string(nil), defaultSkills...),
		WorkspaceGit: WorkspaceGitConfig{
			Provider:    "github",
			Branch:      "main",
			Schedule:    "every 5m",
			AuthorName:  "OpenLia Agent",
			AuthorEmail: "openlia@localhost",
		},
		OpenLIABrowser: OpenLIABrowserConfig{Mode: "local", SSHPort: 22},
		Services:       nil,
	}
}

func defaultLocalInstallRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultLocalRoot
	}
	return filepath.Join(home, defaultLocalRoot)
}

func configPath() string {
	if value := os.Getenv("OPENLIA_CONFIG"); value != "" {
		return value
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", ".config", "openlia", "config.toml")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "openlia", "config.toml")
}

func loadConfig() (Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultConfig(), os.ErrNotExist
	}
	if err != nil {
		return Config{}, fmt.Errorf("read operator config: %w", err)
	}
	config, err := parseConfig(string(data))
	if err != nil {
		return Config{}, fmt.Errorf("parse operator config: %w", err)
	}
	return config, nil
}

func loadConfigUnchecked() (Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultConfig(), os.ErrNotExist
	}
	if err != nil {
		return Config{}, fmt.Errorf("read operator config: %w", err)
	}
	return parseConfigUnchecked(string(data))
}

func parseConfig(data string) (Config, error) {
	config, err := parseConfigUnchecked(data)
	if err != nil {
		return Config{}, err
	}
	return config, validateConfig(config)
}

func parseConfigUnchecked(data string) (Config, error) {
	config := defaultConfig()
	section := ""
	var err error
	scanner := bufio.NewScanner(strings.NewReader(data))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section = strings.TrimSpace(line[2 : len(line)-2])
			switch section {
			case "services":
				config.Services = append(config.Services, ServiceHostConfig{Roles: make(map[string]string)})
			case "fallback_providers":
				config.FallbackProviders = append(config.FallbackProviders, FallbackProviderConfig{})
			case "skill_sources":
				config.SkillSources = append(config.SkillSources, SkillSourceConfig{})
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section == "services" && len(config.Services) == 0 {
				config.Services = append(config.Services, ServiceHostConfig{Roles: make(map[string]string)})
			}
			if section == "openlia-browser" {
				config.OpenLIABrowser.Configured = true
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("line %d is not a key/value pair", lineNumber)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(strings.SplitN(value, " #", 2)[0])
		if section == "services" {
			if len(config.Services) == 0 {
				config.Services = append(config.Services, ServiceHostConfig{Roles: make(map[string]string)})
			}
			current := &config.Services[len(config.Services)-1]
			cleanKey := strings.Trim(key, "\"")
			switch cleanKey {
			case "name":
				current.Name, err = parseString(value)
			case "source":
				current.Source, err = parseString(value)
				if err == nil {
					current.Source = expandTilde(current.Source)
				}
			default:
				if strings.Contains(cleanKey, ".") {
					return Config{}, fmt.Errorf("line %d: invalid service key %q; service names cannot contain dots (declare host with 'name = ...')", lineNumber, cleanKey)
				}
				var role string
				role, err = parseString(value)
				if err == nil {
					err = ValidateServiceRole(role)
				}
				if err == nil {
					if current.Roles == nil {
						current.Roles = make(map[string]string)
					}
					current.Roles[cleanKey] = role
				}
			}
		} else if section == "fallback_providers" {
			if len(config.FallbackProviders) == 0 {
				return Config{}, fmt.Errorf("line %d defines fallback provider fields without a table", lineNumber)
			}
			current := &config.FallbackProviders[len(config.FallbackProviders)-1]
			switch strings.Trim(key, "\"") {
			case "provider":
				current.Provider, err = parseString(value)
			case "model":
				current.Model, err = parseString(value)
			case "base_url":
				current.BaseURL, err = parseString(value)
			case "key_env":
				current.KeyEnv, err = parseString(value)
			default:
				return Config{}, fmt.Errorf("line %d contains unknown fallback provider setting %q", lineNumber, key)
			}
		} else if section == "skill_sources" {
			if len(config.SkillSources) == 0 {
				return Config{}, fmt.Errorf("line %d defines skill source fields without a table", lineNumber)
			}
			current := &config.SkillSources[len(config.SkillSources)-1]
			switch strings.Trim(key, "\"") {
			case "name":
				current.Name, err = parseString(value)
			case "repository":
				current.Repository, err = parseString(value)
			case "branch":
				current.Branch, err = parseString(value)
			default:
				return Config{}, fmt.Errorf("line %d contains unknown skill source setting %q", lineNumber, key)
			}
		} else {
			switch section + "." + key {
			case "openlia.schema":
				config.Schema, err = parseInt(value)
			case "openlia.version":
				config.Version, err = parseString(value)
			case "openlia.mode":
				config.Mode, err = parseString(value)
			case "openlia.target":
				config.Target, err = parseString(value)
			case "openlia.root":
				config.InstallRoot, err = parseString(value)
			case "openlia.remote_root":
				config.InstallRoot, err = parseString(value)
			case "openlia.project":
				config.Project, err = parseString(value)
			case "openlia.model":
				config.Model, err = parseString(value)
			case "openlia.output_language":
				config.OutputLanguage, err = parseString(value)
			case "openlia.timezone":
				config.Timezone, err = parseString(value)
			case "openlia.provider":
				config.Provider, err = parseString(value)
			case "openlia.external_network":
				config.ExternalNetwork, err = parseString(value)
			case "workspace-ui.host":
				config.WorkspaceUIHost, err = parseString(value)
			case "workspace-ui.port":
				config.WorkspaceUIPort, err = parseInt(value)
			case "workspace-ui.public_origin":
				config.WorkspaceUIPublicOrigin, err = parseString(value)
			case "workspace-ui.password_hash":
				config.WorkspaceUIPasswordHash, err = parseString(value)
			case "open-webui.host":
				config.OpenWebUIHost, err = parseString(value)
			case "open-webui.port":
				config.OpenWebUIPort, err = parseInt(value)
			case "open-webui.image":
				config.OpenWebUIImage, err = parseString(value)
			case "open-webui.auth":
				config.OpenWebUIAuth, err = parseBool(value)
			case "openlia-browser.mode":
				config.OpenLIABrowser.Mode, err = parseString(value)
			case "openlia-browser.target":
				config.OpenLIABrowser.Target, err = parseString(value)
			case "openlia-browser.ssh_port":
				config.OpenLIABrowser.SSHPort, err = parseInt(value)
			case "openlia-browser.root":
				config.OpenLIABrowser.Root, err = parseString(value)
			case "openlia-browser.extension_token_file":
				config.OpenLIABrowser.ExtensionTokenFile, err = parseString(value)
			case "components.open_webui_image":
				config.OpenWebUIImage, err = parseString(value)
			case "release.source":
				config.ReleaseSource, err = parseString(value)
			case "components.hermes_image":
				config.HermesImage, err = parseString(value)
			case "components.hermes_tag":
				config.HermesTag, err = parseString(value)
			case "components.hermes_digest":
				config.HermesDigest, err = parseString(value)
			case "components.locho_image":
				config.LochoImage, err = parseString(value)
			case "components.locho_version":
				config.LochoVersion, err = parseString(value)
			case "api.enabled":
				config.APIEnabled, err = parseBool(value)
			case "api.host":
				config.APIHost, err = parseString(value)
			case "secrets.source":
				config.SecretSource, err = parseString(value)
			case "skills.enabled":
				config.EnabledSkills, err = parseStringArray(value)
			case "workspace_git.enabled":
				config.WorkspaceGit.Enabled, err = parseBool(value)
			case "workspace_git.provider":
				config.WorkspaceGit.Provider, err = parseString(value)
			case "workspace_git.remote":
				config.WorkspaceGit.Remote, err = parseString(value)
			case "workspace_git.branch":
				config.WorkspaceGit.Branch, err = parseString(value)
			case "workspace_git.schedule":
				config.WorkspaceGit.Schedule, err = parseString(value)
			case "workspace_git.author_name":
				config.WorkspaceGit.AuthorName, err = parseString(value)
			case "workspace_git.author_email":
				config.WorkspaceGit.AuthorEmail, err = parseString(value)
			default:
				return Config{}, fmt.Errorf("line %d contains unknown setting %q", lineNumber, section+"."+key)
			}
		}
		if err != nil {
			return Config{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func parseString(value string) (string, error) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", errors.New("expected a quoted string")
	}
	return strconv.Unquote(value)
}

func parseInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("expected integer: %w", err)
	}
	return parsed, nil
}

func parseBool(value string) (bool, error) {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("expected boolean: %w", err)
	}
	return parsed, nil
}

func parseStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, errors.New("expected an array")
	}
	value = strings.TrimSpace(value[1 : len(value)-1])
	if value == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item, err := parseString(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func validateConfig(config Config) error {
	if config.Schema != configSchema {
		return fmt.Errorf("unsupported config schema %d", config.Schema)
	}
	if !safeVersion(config.Version) || config.Project == "" || config.Model == "" {
		return errors.New("version, project, and model are required")
	}
	if err := validateTimezone(config.Timezone); err != nil {
		return err
	}
	if err := validateOutputLanguage(config.OutputLanguage); err != nil {
		return err
	}
	if config.Mode != "local" && config.Mode != "ssh" {
		return fmt.Errorf("mode must be local or ssh")
	}
	if config.Mode == "local" {
		if config.Target != "" {
			return errors.New("local mode cannot have a target")
		}
	} else if err := validateTarget(config.Target); err != nil && config.Target != "" {
		return err
	}
	if err := validateAbsoluteRoot(config.InstallRoot, "root"); err != nil {
		return err
	}
	if !safeComponent(config.Project) {
		return errors.New("project must contain only letters, numbers, underscore, or hyphen")
	}
	if !safeReference(config.Provider) {
		return errors.New("provider must contain only URL-safe provider identifier characters")
	}
	if config.ExternalNetwork != "" && !safeComponent(config.ExternalNetwork) {
		return errors.New("external network must contain only letters, numbers, underscore, or hyphen")
	}
	if !safeImageRef(config.HermesImage) || !safeImageRef(config.LochoImage) {
		return errors.New("component image references contain unsupported characters")
	}
	if config.OpenWebUIImage != "" && !safeImageRef(config.OpenWebUIImage) {
		return errors.New("component image references contain unsupported characters")
	}
	if config.HermesTag == "" || !validDigest(config.HermesDigest) {
		return errors.New("Hermes tag and digest are required")
	}
	if config.LochoVersion == "" || config.LochoImage == "" || config.HermesImage == "" {
		return errors.New("component versions and image names are required")
	}
	if config.APIHost != "127.0.0.1" {
		return errors.New("API host must remain 127.0.0.1; use SSH or a private network for access")
	}
	if err := validateWorkspaceUI(config.WorkspaceUIHost, config.WorkspaceUIPort, config.WorkspaceUIPublicOrigin, config.WorkspaceUIPasswordHash); err != nil {
		return err
	}
	if config.OpenWebUIHost != "" {
		if err := validateOpenWebUI(config.OpenWebUIHost, config.OpenWebUIPort); err != nil {
			return err
		}
	}
	if config.OpenLIABrowser.Configured {
		if err := validateOpenLIABrowser(config.OpenLIABrowser); err != nil {
			return err
		}
	}
	if config.SecretSource != "" && !filepath.IsAbs(config.SecretSource) {
		return errors.New("secret source must be an absolute path")
	}
	if !safeReference(config.Model) {
		return errors.New("model must contain only URL-safe model identifier characters")
	}
	for index, fallback := range config.FallbackProviders {
		if !safeReference(fallback.Provider) || fallback.Provider == "custom" && fallback.BaseURL == "" {
			return fmt.Errorf("fallback provider %d requires a provider and explicit base_url", index)
		}
		if !safeReference(fallback.Model) {
			return fmt.Errorf("fallback provider %d model must contain only URL-safe model identifier characters", index)
		}
		if err := validateOpenAIEndpoint(fallback.BaseURL); err != nil {
			return fmt.Errorf("fallback provider %d: %w", index, err)
		}
		if fallback.KeyEnv != "" && !safeEnvName(fallback.KeyEnv) {
			return fmt.Errorf("fallback provider %d key_env is invalid", index)
		}
	}
	for _, skill := range config.EnabledSkills {
		if !safeComponent(skill) {
			return fmt.Errorf("invalid skill name %q", skill)
		}
	}
	sourceNames := make(map[string]bool)
	for index, source := range config.SkillSources {
		if !safeComponent(source.Name) {
			return fmt.Errorf("skill source %d has invalid name %q", index, source.Name)
		}
		if sourceNames[source.Name] {
			return fmt.Errorf("duplicate skill source name %q", source.Name)
		}
		sourceNames[source.Name] = true
		if err := validateGitHubRepository(source.Repository); err != nil {
			return fmt.Errorf("skill source %q: %w", source.Name, err)
		}
		if !safeGitBranch(source.Branch) {
			return fmt.Errorf("skill source %q branch is invalid", source.Name)
		}
	}
	if err := validateWorkspaceGit(config.WorkspaceGit); err != nil {
		return err
	}
	hostNames := make(map[string]bool)
	for i, host := range config.Services {
		if host.Name == "" {
			return fmt.Errorf("service host at index %d requires a name", i)
		}
		if !safeComponent(host.Name) {
			return fmt.Errorf("invalid service host name %q", host.Name)
		}
		if hostNames[host.Name] {
			return fmt.Errorf("duplicate service host name %q", host.Name)
		}
		hostNames[host.Name] = true
		if host.Source != "" {
			if !filepath.IsAbs(host.Source) {
				return fmt.Errorf("service host %q source %q must be an absolute path", host.Name, host.Source)
			}
			if isInsideWorkingTree(host.Source) {
				return fmt.Errorf("service host %q source %q must be outside the OpenLia checkout", host.Name, host.Source)
			}
		}
		for svc, role := range host.Roles {
			if !safeComponent(svc) {
				return fmt.Errorf("service host %q has invalid service name %q", host.Name, svc)
			}
			if err := ValidateServiceRole(role); err != nil {
				return fmt.Errorf("service host %q service %q: %w", host.Name, svc, err)
			}
		}
	}
	return nil
}

func validateWorkspaceUI(host string, port int, publicOrigin, passwordHash string) error {
	if host != "" && (net.ParseIP(host) == nil || host != "127.0.0.1" && host != "0.0.0.0") {
		return errors.New("workspace-ui.host must be 127.0.0.1 or 0.0.0.0")
	}
	if port < 1 || port > 65535 {
		return errors.New("workspace-ui.port must be between 1 and 65535")
	}
	if err := validateWorkspaceUIPublicOrigin(publicOrigin); err != nil {
		return err
	}
	if passwordHash != "" {
		if err := validateWorkspaceUIPasswordHash(passwordHash); err != nil {
			return err
		}
	}
	if host == "0.0.0.0" && passwordHash == "" {
		return errors.New("workspace-ui.password_hash is required when workspace-ui.host is 0.0.0.0")
	}
	return nil
}

func validateOpenWebUI(host string, port int) error {
	if host == "" {
		return nil
	}
	if net.ParseIP(host) == nil || host != "127.0.0.1" && host != "0.0.0.0" {
		return errors.New("open-webui.host must be 127.0.0.1 or 0.0.0.0")
	}
	if port < 1 || port > 65535 {
		return errors.New("open-webui.port must be between 1 and 65535")
	}
	return nil
}

func validateOpenLIABrowser(config OpenLIABrowserConfig) error {
	if config.Mode != "local" && config.Mode != "ssh" {
		return errors.New("openlia-browser.mode must be local or ssh")
	}
	if config.Root == "" {
		return errors.New("openlia-browser.root is required")
	}
	if config.ExtensionTokenFile == "" {
		return errors.New("openlia-browser.extension_token_file is required")
	}
	if err := validateAbsoluteRoot(config.ExtensionTokenFile, "openlia-browser.extension_token_file"); err != nil {
		return err
	}
	if err := validateAbsoluteRoot(config.Root, "openlia-browser.root"); err != nil {
		return err
	}
	if config.SSHPort < 1 || config.SSHPort > 65535 {
		return errors.New("openlia-browser.ssh_port must be between 1 and 65535")
	}
	if config.Mode == "local" {
		if config.Target != "" {
			return errors.New("openlia-browser.target must be empty in local mode")
		}
		return nil
	}
	if config.Target == "" {
		return errors.New("openlia-browser.target is required in ssh mode")
	}
	if err := validateTarget(config.Target); err != nil {
		return fmt.Errorf("openlia-browser.target: %w", err)
	}
	if !strings.Contains(config.Target, "@") || strings.HasPrefix(config.Target, "@") || strings.HasSuffix(config.Target, "@") {
		return errors.New("openlia-browser.target must use user@host syntax")
	}
	return nil
}

func validateWorkspaceUIPublicOrigin(value string) error {
	if value == "" {
		return nil
	}
	origin, err := url.Parse(value)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.User != nil || origin.Opaque != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" {
		return errors.New("workspace-ui.public_origin must be an absolute http(s) origin")
	}
	return nil
}

func validateWorkspaceGit(gitConfig WorkspaceGitConfig) error {
	if gitConfig.Provider == "" {
		gitConfig.Provider = "github"
	}
	if gitConfig.Provider != "github" {
		return errors.New("workspace Git provider must be github")
	}
	if gitConfig.Remote == "" {
		if gitConfig.Enabled {
			return errors.New("workspace Git remote is required when workspace Git is enabled")
		}
		return nil
	}
	if !gitConfig.Enabled {
		return errors.New("workspace Git must be enabled when a remote is configured")
	}
	if err := validateGitHubRemote(gitConfig.Remote); err != nil {
		return err
	}
	if !safeGitBranch(gitConfig.Branch) {
		return errors.New("workspace Git branch is invalid")
	}
	if gitConfig.Schedule == "" || len(gitConfig.Schedule) > 120 || strings.ContainsAny(gitConfig.Schedule, "\r\n") {
		return errors.New("workspace Git schedule is invalid")
	}
	if gitConfig.AuthorName == "" || len(gitConfig.AuthorName) > 200 || strings.ContainsAny(gitConfig.AuthorName, "\r\n") {
		return errors.New("workspace Git author name is invalid")
	}
	if gitConfig.AuthorEmail == "" || len(gitConfig.AuthorEmail) > 254 || strings.ContainsAny(gitConfig.AuthorEmail, "\r\n") || !strings.Contains(gitConfig.AuthorEmail, "@") {
		return errors.New("workspace Git author email is invalid")
	}
	return nil
}

func validateGitHubRemote(remote string) error {
	if err := validateGitHubRepository(remote); err != nil {
		return fmt.Errorf("workspace Git remote %w", err)
	}
	return nil
}

func validateGitHubRepository(remote string) error {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must be an HTTPS github.com repository URL without credentials, query, or fragment")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || !safeGitHubSegment(parts[0]) || !safeGitHubSegment(strings.TrimSuffix(parts[1], ".git")) {
		return errors.New("must use https://github.com/OWNER/REPOSITORY[.git]")
	}
	return nil
}

func validateOpenAIEndpoint(value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return errors.New("gateway base URL must not contain whitespace")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("gateway base URL must be an HTTP(S) URL without credentials, query, or fragment")
	}
	return nil
}

func safeEnvName(value string) bool {
	if value == "" || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z') || value[0] == '_') {
		return false
	}
	for _, character := range value[1:] {
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_') {
			return false
		}
	}
	return true
}

func safeGitHubSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("._-", character) {
			return false
		}
	}
	return true
}

func safeGitBranch(value string) bool {
	if value == "" || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "-") || strings.HasSuffix(value, ".") || strings.HasSuffix(value, "/") || strings.Contains(value, "..") || strings.Contains(value, "//") || strings.Contains(value, "@{") || strings.ContainsAny(value, " ~^:?*[\\\"\r\n") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("._/-", character) {
			return false
		}
	}
	return true
}

func validateTarget(target string) error {
	if target == "" || strings.ContainsAny(target, " \t\r\n'\";$&|()<>`") {
		return errors.New("target must be a non-empty SSH destination without shell metacharacters")
	}
	return nil
}

func validateTimezone(value string) error {
	if value == "" || value == "Local" || strings.HasPrefix(value, "/") || strings.Contains(value, "..") {
		return errors.New("timezone must be a valid IANA timezone, not Local or a filesystem path")
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("/_+-", character) {
			return errors.New("timezone must be a valid IANA timezone, not Local or a filesystem path")
		}
	}
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("timezone must be a valid IANA timezone: %w", err)
	}
	return nil
}

func validateOutputLanguage(value string) error {
	parts := strings.Split(value, "-")
	if len(parts) == 0 || len(parts[0]) < 2 || len(parts[0]) > 8 {
		return errors.New("output language must be a BCP 47 language tag")
	}
	for _, character := range parts[0] {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return errors.New("output language must be a BCP 47 language tag")
		}
	}
	for _, part := range parts[1:] {
		if len(part) < 1 || len(part) > 8 {
			return errors.New("output language must be a BCP 47 language tag")
		}
		for _, character := range part {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
				return errors.New("output language must be a BCP 47 language tag")
			}
		}
	}
	return nil
}

func validateAbsoluteRoot(root, label string) error {
	if !strings.HasPrefix(root, "/") || strings.ContainsAny(root, "\r\n\t'\";$&|()<>`") || strings.Contains(root, "/../") || strings.HasSuffix(root, "/..") {
		return fmt.Errorf("%s must be a safe absolute path", label)
	}
	if root == "/" || root == "/srv" || root == "/opt" || root == "/var" || root == "/home" {
		return fmt.Errorf("%s is too broad", label)
	}
	return nil
}

func safeComponent(value string) bool {
	if value == "" || value == "." || value == ".." || strings.HasPrefix(value, ".") {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func safeVersion(value string) bool {
	if value == "" || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "-") {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune(".-_+", character) {
			return false
		}
	}
	return true
}

func safeReference(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("._:/-", character) {
			return false
		}
	}
	return true
}

func safeImageRef(value string) bool {
	return safeReference(value) && !strings.Contains(value, "..")
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func saveConfig(config Config) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	content := renderConfig(config)
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config.toml.tmp-*")
	if err != nil {
		return fmt.Errorf("create config temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("activate operator config: %w", err)
	}
	return nil
}

func renderConfig(config Config) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "[openlia]\nschema = %d\nversion = %q\nmode = %q\ntarget = %q\nroot = %q\nproject = %q\nmodel = %q\noutput_language = %q\ntimezone = %q\nprovider = %q\nexternal_network = %q\n\n", config.Schema, config.Version, config.Mode, config.Target, config.InstallRoot, config.Project, config.Model, config.OutputLanguage, config.Timezone, config.Provider, config.ExternalNetwork)
	if config.OpenLIABrowser.Configured {
		fmt.Fprintf(&builder, "[openlia-browser]\nmode = %q\ntarget = %q\nssh_port = %d\nroot = %q\nextension_token_file = %q\n\n", config.OpenLIABrowser.Mode, config.OpenLIABrowser.Target, config.OpenLIABrowser.SSHPort, config.OpenLIABrowser.Root, config.OpenLIABrowser.ExtensionTokenFile)
	}
	if config.WorkspaceUIHost != "" {
		fmt.Fprintf(&builder, "[workspace-ui]\nhost = %q\nport = %d\n", config.WorkspaceUIHost, config.WorkspaceUIPort)
		if config.WorkspaceUIPublicOrigin != "" {
			fmt.Fprintf(&builder, "public_origin = %q\n", config.WorkspaceUIPublicOrigin)
		}
		if config.WorkspaceUIPasswordHash != "" {
			fmt.Fprintf(&builder, "password_hash = %q\n", config.WorkspaceUIPasswordHash)
		}
		builder.WriteString("\n")
	}
	if config.OpenWebUIHost != "" {
		fmt.Fprintf(&builder, "[open-webui]\nhost = %q\nport = %d\n", config.OpenWebUIHost, config.OpenWebUIPort)
		if config.OpenWebUIImage != "" && config.OpenWebUIImage != defaultOpenWebUIImage {
			fmt.Fprintf(&builder, "image = %q\n", config.OpenWebUIImage)
		}
		if !config.OpenWebUIAuth {
			builder.WriteString("auth = false\n")
		}
		builder.WriteString("\n")
	}
	for _, fallback := range config.FallbackProviders {
		builder.WriteString("[[fallback_providers]]\n")
		fmt.Fprintf(&builder, "provider = %q\nmodel = %q\n", fallback.Provider, fallback.Model)
		if fallback.BaseURL != "" {
			fmt.Fprintf(&builder, "base_url = %q\n", fallback.BaseURL)
		}
		if fallback.KeyEnv != "" {
			fmt.Fprintf(&builder, "key_env = %q\n", fallback.KeyEnv)
		}
		builder.WriteString("\n")
	}
	fmt.Fprintf(&builder, "[release]\nsource = %q\n\n", config.ReleaseSource)
	fmt.Fprintf(&builder, "[components]\nhermes_image = %q\nhermes_tag = %q\nhermes_digest = %q\nlocho_image = %q\nlocho_version = %q\n\n", config.HermesImage, config.HermesTag, config.HermesDigest, config.LochoImage, config.LochoVersion)
	fmt.Fprintf(&builder, "[api]\nenabled = %t\nhost = %q\n\n", config.APIEnabled, config.APIHost)
	fmt.Fprintf(&builder, "[secrets]\nsource = %q\n\n", config.SecretSource)
	for _, source := range config.SkillSources {
		builder.WriteString("[[skill_sources]]\n")
		fmt.Fprintf(&builder, "name = %q\nrepository = %q\nbranch = %q\n\n", source.Name, source.Repository, source.Branch)
	}
	builder.WriteString("[skills]\nenabled = [")
	for index, skill := range config.EnabledSkills {
		if index > 0 {
			builder.WriteString(", ")
		}
		fmt.Fprintf(&builder, "%q", skill)
	}
	builder.WriteString("]\n")
	fmt.Fprintf(&builder, "\n[workspace_git]\nenabled = %t\nprovider = %q\nremote = %q\nbranch = %q\nschedule = %q\nauthor_name = %q\nauthor_email = %q\n", config.WorkspaceGit.Enabled, config.WorkspaceGit.Provider, config.WorkspaceGit.Remote, config.WorkspaceGit.Branch, config.WorkspaceGit.Schedule, config.WorkspaceGit.AuthorName, config.WorkspaceGit.AuthorEmail)
	for _, host := range config.Services {
		builder.WriteString("\n[[services]]\n")
		if host.Source != "" {
			fmt.Fprintf(&builder, "source = %q\n", host.Source)
		}
		if host.Name != "" {
			fmt.Fprintf(&builder, "name = %q\n", host.Name)
		}
		keys := make([]string, 0, len(host.Roles))
		for k := range host.Roles {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&builder, "%q = %q\n", k, host.Roles[k])
		}
	}
	return builder.String()
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
