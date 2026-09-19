package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	configSchema      = 1
	defaultVersion    = "0.1.0"
	defaultRemoteRoot = "/srv/openlia"
	defaultLocalRoot  = ".openlia"
	defaultProject    = "openlia"
	defaultTimezone   = "Asia/Ho_Chi_Minh"
)

var defaultSkills = []string{
	"inbox-triage",
	"daily-briefing",
	"weekly-review",
	"project-review",
	"decision-analysis",
	"deep-research",
	"personal-finance",
}

type Config struct {
	Schema          int
	Version         string
	Mode            string
	Target          string
	InstallRoot     string
	Project         string
	Model           string
	Timezone        string
	Provider        string
	ExternalNetwork string
	HermesImage     string
	HermesTag       string
	HermesDigest    string
	LochoImage      string
	LochoVersion    string
	APIEnabled      bool
	APIHost         string
	SecretSource    string
	ReleaseSource   string
	EnabledSkills   []string
}

func defaultConfig() Config {
	return Config{
		Schema:        configSchema,
		Version:       defaultVersion,
		Mode:          "ssh",
		InstallRoot:   defaultRemoteRoot,
		Project:       defaultProject,
		Model:         "gpt-5.6-luna",
		Timezone:      defaultTimezone,
		Provider:      "openai-api",
		HermesImage:   "openlia-hermes:v2026.9.14",
		HermesTag:     "v2026.9.14",
		HermesDigest:  "sha256:99641e57ec762c59e54cb44aa6746b7fc68c18b3c5ddb088af54234c613d9294",
		LochoImage:    "openlia-locho:v1.2.0-beta.1",
		LochoVersion:  "1.2.0-beta.1",
		APIHost:       "127.0.0.1",
		EnabledSkills: append([]string(nil), defaultSkills...),
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

func parseConfig(data string) (Config, error) {
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
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("line %d is not a key/value pair", lineNumber)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(strings.SplitN(value, " #", 2)[0])
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
		case "openlia.timezone":
			config.Timezone, err = parseString(value)
		case "openlia.provider":
			config.Provider, err = parseString(value)
		case "openlia.external_network":
			config.ExternalNetwork, err = parseString(value)
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
		default:
			return Config{}, fmt.Errorf("line %d contains unknown setting %q", lineNumber, section+"."+key)
		}
		if err != nil {
			return Config{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return config, validateConfig(config)
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
	if config.HermesTag == "" || !validDigest(config.HermesDigest) {
		return errors.New("Hermes tag and digest are required")
	}
	if config.LochoVersion == "" || config.LochoImage == "" || config.HermesImage == "" {
		return errors.New("component versions and image names are required")
	}
	if config.APIHost != "127.0.0.1" {
		return errors.New("API host must remain 127.0.0.1; use SSH or a private network for access")
	}
	if config.SecretSource != "" && !filepath.IsAbs(config.SecretSource) {
		return errors.New("secret source must be an absolute path")
	}
	if !safeReference(config.Model) {
		return errors.New("model must contain only URL-safe model identifier characters")
	}
	for _, skill := range config.EnabledSkills {
		if !safeComponent(skill) {
			return fmt.Errorf("invalid skill name %q", skill)
		}
	}
	return nil
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
	fmt.Fprintf(&builder, "[openlia]\nschema = %d\nversion = %q\nmode = %q\ntarget = %q\nroot = %q\nproject = %q\nmodel = %q\ntimezone = %q\nprovider = %q\nexternal_network = %q\n\n", config.Schema, config.Version, config.Mode, config.Target, config.InstallRoot, config.Project, config.Model, config.Timezone, config.Provider, config.ExternalNetwork)
	fmt.Fprintf(&builder, "[release]\nsource = %q\n\n", config.ReleaseSource)
	fmt.Fprintf(&builder, "[components]\nhermes_image = %q\nhermes_tag = %q\nhermes_digest = %q\nlocho_image = %q\nlocho_version = %q\n\n", config.HermesImage, config.HermesTag, config.HermesDigest, config.LochoImage, config.LochoVersion)
	fmt.Fprintf(&builder, "[api]\nenabled = %t\nhost = %q\n\n", config.APIEnabled, config.APIHost)
	fmt.Fprintf(&builder, "[secrets]\nsource = %q\n\n", config.SecretSource)
	builder.WriteString("[skills]\nenabled = [")
	for index, skill := range config.EnabledSkills {
		if index > 0 {
			builder.WriteString(", ")
		}
		fmt.Fprintf(&builder, "%q", skill)
	}
	builder.WriteString("]\n")
	return builder.String()
}
