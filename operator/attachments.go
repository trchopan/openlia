package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LochoService struct {
	Host     string `json:"host"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Endpoint string `json:"endpoint"`
	Role     string `json:"role"`
}

type AttachmentHost struct {
	Host         string         `json:"host"`
	Configured   bool           `json:"configured"`
	ServiceCount int            `json:"service_count"`
	Services     []LochoService `json:"services"`
	Capabilities string         `json:"capabilities"`
	BrowserPort  int            `json:"-"`
}

type ServiceRegistry struct {
	Schema      int            `json:"schema"`
	GeneratedAt string         `json:"generated_at"`
	Services    []LochoService `json:"services"`
}

type AttachmentListResult struct {
	OK           bool             `json:"ok"`
	Hosts        []AttachmentHost `json:"hosts"`
	Capabilities string           `json:"capabilities"`
}

func ListAttachments(config Config) (AttachmentListResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return AttachmentListResult{}, err
	}
	result := AttachmentListResult{OK: true, Hosts: []AttachmentHost{}, Capabilities: "redacted"}
	entries, err := os.ReadDir(config.LochoRoot)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return AttachmentListResult{}, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if ValidateSafeComponent(entry.Name(), "host") != nil {
			continue
		}
		if len(config.ConfiguredHosts) > 0 {
			configured := false
			for _, h := range config.ConfiguredHosts {
				if h == entry.Name() {
					configured = true
					break
				}
			}
			if !configured {
				continue
			}
		}
		path := filepath.Join(config.LochoRoot, entry.Name(), "attachments.toml")
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		services, browserPort, err := serviceInventory(entry.Name(), path, config.ServiceRoles)
		if err != nil {
			return AttachmentListResult{}, err
		}
		result.Hosts = append(result.Hosts, AttachmentHost{
			Host:         entry.Name(),
			Configured:   true,
			ServiceCount: len(services),
			Services:     services,
			Capabilities: "redacted",
			BrowserPort:  browserPort,
		})
	}
	sort.Slice(result.Hosts, func(i, j int) bool { return result.Hosts[i].Host < result.Hosts[j].Host })
	return result, nil
}

func GenerateAttachments(config Config, composers ...Compose) error {
	return GenerateAttachmentsContext(context.Background(), config, composers...)
}

func GenerateAttachmentsContext(ctx context.Context, config Config, composers ...Compose) error {
	if err := config.ValidatePaths(); err != nil {
		return err
	}
	if config.APIHost != "127.0.0.1" {
		return fmt.Errorf("API host must remain 127.0.0.1")
	}
	if config.ExternalNetwork != "" {
		if err := ValidateSafeComponent(config.ExternalNetwork, "external-network"); err != nil {
			return err
		}
	}
	if err := EnsureDir(config.LochoRoot, 0o700); err != nil {
		return err
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return err
	}
	if len(composers) > 0 {
		return generateAttachmentsWithCompose(ctx, config, composers[0])
	}
	return generateAttachmentsFile(config)
}

func generateAttachmentsWithCompose(ctx context.Context, config Config, compose Compose) error {
	if err := EnsureDir(config.LochoRoot, 0o700); err != nil {
		return err
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return err
	}
	generatedBackup := ""
	wasPresent := false
	previousConfig, configErr := os.ReadFile(filepath.Join(config.DataRoot, "config.yaml"))
	configPresent := configErr == nil
	if info, err := os.Stat(config.GeneratedCompose); err == nil && info.Mode().IsRegular() {
		wasPresent = true
		if _, err := CreateBackup(config, "attachments-generate", time.Now().UTC(), compose); err != nil {
			return err
		}
		generatedBackup, err = backupFile(config, config.GeneratedCompose, "compose-generated", 0o600, time.Now().UTC())
		if err != nil {
			return err
		}
	}
	restore := func() {
		if generatedBackup != "" {
			_ = AtomicCopyFile(generatedBackup, config.GeneratedCompose, 0o600)
		} else if !wasPresent {
			_ = os.Remove(config.GeneratedCompose)
		}
		configPath := filepath.Join(config.DataRoot, "config.yaml")
		if configPresent {
			_ = AtomicWriteFile(configPath, previousConfig, 0o600)
		} else {
			_ = os.Remove(configPath)
		}
	}
	if err := generateAttachmentsFile(config); err != nil {
		restore()
		_ = RecordChange(config, "attachments-generate", "failed", generatedBackup, "Compose sidecar generation failed", time.Now().UTC())
		return err
	}
	if _, err := compose.Run(ctx, "config", "--quiet"); err != nil {
		restore()
		_ = RecordChange(config, "attachments-generate", "failed", generatedBackup, "generated Compose validation failed", time.Now().UTC())
		return fmt.Errorf("generated Compose validation failed")
	}
	currentConfig, currentConfigErr := os.ReadFile(filepath.Join(config.DataRoot, "config.yaml"))
	policyChanged := configPresent && currentConfigErr == nil && string(previousConfig) != string(currentConfig)
	if policyChanged {
		state, stateErr := ReadState(config)
		if stateErr != nil {
			restore()
			return stateErr
		}
		if state == stateRunning {
			if _, restartErr := compose.Run(ctx, "up", "-d", "--no-deps", "--force-recreate", "hermes"); restartErr != nil {
				restore()
				if _, recoveryErr := compose.Run(ctx, "up", "-d", "--no-deps", "--force-recreate", "hermes"); recoveryErr != nil {
					return fmt.Errorf("Hermes browser policy reconciliation failed: %w; rollback restart failed: %v", restartErr, recoveryErr)
				}
				return fmt.Errorf("Hermes browser policy reconciliation failed: %w", restartErr)
			}
		}
	}
	return RecordChange(config, "attachments-generate", "ok", "", "Compose sidecars regenerated", time.Now().UTC())
}

func generateAttachmentsFile(config Config) error {
	if err := validateWorkspaceUI(config.WorkspaceUIHost, config.WorkspaceUIPort, config.WorkspaceUIAuthRequired, config.WorkspaceUIPasswordHashFile); err != nil {
		return err
	}
	workspaceUIHost, workspaceUIPort := config.WorkspaceUIHost, config.WorkspaceUIPort
	hosts, err := ListAttachments(config)
	if err != nil {
		return err
	}
	var allServices []LochoService
	browserURL := ""
	hasPlaywrightBrowserRole := false
	for _, host := range hosts.Hosts {
		for _, svc := range host.Services {
			allServices = append(allServices, svc)
			if svc.Role == "playwright-browser" {
				hasPlaywrightBrowserRole = true
				if browserURL != "" && browserURL != svc.Endpoint {
					return fmt.Errorf("multiple Playwright attachment endpoints are configured")
				}
				browserURL = svc.Endpoint
			}
		}
		if browserURL == "" && host.BrowserPort > 0 {
			browserURL = fmt.Sprintf("http://locho-%s:%d", host.Host, host.BrowserPort)
		}
	}

	registry := ServiceRegistry{
		Schema:      1,
		GeneratedAt: utcTimestamp(time.Now().UTC()),
		Services:    allServices,
	}
	registryBytes, marshalErr := json.MarshalIndent(registry, "", "  ")
	if marshalErr == nil {
		if ensureErr := EnsureDir(config.DataRoot, 0o700); ensureErr == nil {
			_ = AtomicWriteFile(filepath.Join(config.DataRoot, "services.json"), append(registryBytes, '\n'), 0o600)
		}
		if ensureErr := EnsureDir(config.MetaRoot, 0o700); ensureErr == nil {
			_ = AtomicWriteFile(filepath.Join(config.MetaRoot, "services.json"), append(registryBytes, '\n'), 0o600)
		}
	}

	var builder strings.Builder
	hasHermesEnv := config.APIEnabled || config.ExternalNetwork != "" || browserURL != "" || len(allServices) > 0
	hasGeneratedServices := hasHermesEnv || config.WorkspaceUIHost != "" || len(hosts.Hosts) > 0
	if !hasGeneratedServices {
		builder.WriteString("services: {}\n")
	} else {
		builder.WriteString("services:\n")
		if hasHermesEnv {
			builder.WriteString("  hermes:\n")
			if config.APIEnabled {
				fmt.Fprintf(&builder, "    ports:\n      - %q\n", config.APIHost+":8642:8642")
			}
			builder.WriteString("    environment:\n")
			if config.APIEnabled {
				builder.WriteString("      API_SERVER_ENABLED: \"true\"\n      API_SERVER_HOST: \"0.0.0.0\"\n")
			}
			if browserURL != "" {
				fmt.Fprintf(&builder, "      OPENLIA_BROWSER_MCP_URL: %q\n", browserURL)
				builder.WriteString("      OPENLIA_TOOLS_URL: \"http://openlia-tools:8787\"\n")
			}
			for _, svc := range allServices {
				if svc.Role != "unassigned" {
					envKey := fmt.Sprintf("OPENLIA_SERVICE_%s_%s_URL", sanitizeEnvKey(svc.Host), sanitizeEnvKey(svc.Name))
					fmt.Fprintf(&builder, "      %s: %q\n", envKey, svc.Endpoint)
				}
			}
			if config.ExternalNetwork != "" {
				builder.WriteString("    networks:\n      - openlia-private\n      - openlia-external\n")
			}
		}
	}
	if config.WorkspaceUIHost != "" {
		workspace, _ := json.Marshal(filepath.Join(config.DataRoot, "workspace"))
		passwordHashFile, _ := json.Marshal(config.WorkspaceUIPasswordHashFile)
		builder.WriteString("  workspace-ui:\n    image: \"${OPENLIA_WORKSPACE_UI_IMAGE:-openlia-workspace-ui:v0.1.0}\"\n")
		builder.WriteString("    build:\n      context: ..\n      dockerfile: docker/workspace-ui.Dockerfile\n")
		builder.WriteString(fmt.Sprintf("    command: [\"bun\", \"/opt/openlia/workspace-ui/server.js\"]\n    restart: unless-stopped\n    user: \"10000:10000\"\n    read_only: true\n    tmpfs:\n      - /tmp:rw,noexec,nosuid,size=32m\n    security_opt:\n      - no-new-privileges:true\n    cap_drop: [ALL]\n    environment:\n      OPENLIA_WORKSPACE_ROOT: /workspace\n      OPENLIA_WORKSPACE_UI_BIND: 0.0.0.0\n      OPENLIA_WORKSPACE_UI_PORT: \"%d\"\n", workspaceUIPort))
		quotedPublicOrigin, _ := json.Marshal(config.WorkspaceUIPublicOrigin)
		builder.WriteString("      OPENLIA_WORKSPACE_UI_PUBLIC_ORIGIN: ")
		builder.Write(quotedPublicOrigin)
		builder.WriteString("\n")
		if config.WorkspaceUIAuthRequired {
			builder.WriteString("      OPENLIA_WORKSPACE_UI_AUTH_REQUIRED: \"true\"\n      OPENLIA_WORKSPACE_UI_PASSWORD_HASH_FILE: /run/openlia-secrets/workspace-ui-password.hash\n")
		}
		builder.WriteString("    volumes:\n      - type: bind\n        source: ")
		builder.Write(workspace)
		builder.WriteString("\n        target: /workspace")
		if config.WorkspaceUIAuthRequired {
			builder.WriteString("\n      - type: bind\n        source: ")
			builder.Write(passwordHashFile)
			builder.WriteString("\n        target: /run/openlia-secrets/workspace-ui-password.hash\n        read_only: true")
		}
		builder.WriteString(fmt.Sprintf("\n    ports:\n      - %q\n    networks:\n      - openlia-private\n    healthcheck:\n      test: [\"CMD\", \"bun\", \"-e\", \"fetch('http://127.0.0.1:%d/health').then(r => { if (!r.ok) process.exit(1) })\"]\n      interval: 10s\n      timeout: 3s\n      retries: 5\n    deploy:\n      resources:\n        limits:\n          cpus: \"0.5\"\n          memory: 256M\n    logging:\n      driver: \"json-file\"\n      options:\n        max-size: \"20m\"\n        max-file: \"5\"\n", fmt.Sprintf("%s:%d:%d", workspaceUIHost, workspaceUIPort, workspaceUIPort), workspaceUIPort))
	}
	if browserURL != "" {
		dataRoot, _ := json.Marshal(config.DataRoot)
		fmt.Fprintf(&builder, "  openlia-tools:\n    image: \"${OPENLIA_TOOLS_IMAGE:-openlia-tools:v0.1.0}\"\n")
		builder.WriteString("    build:\n      context: ..\n      dockerfile: docker/tools.Dockerfile\n")
		builder.WriteString("    command: [\"bun\", \"/opt/openlia/tools/server.js\"]\n    restart: unless-stopped\n    read_only: true\n    tmpfs:\n      - /tmp\n    security_opt:\n      - no-new-privileges:true\n    cap_drop: [ALL]\n")
		builder.WriteString("    environment:\n      HERMES_HOME: /opt/data\n      OPENLIA_BROWSER_MCP_URL: ")
		quotedBrowserURL, _ := json.Marshal(browserURL)
		builder.Write(quotedBrowserURL)
		builder.WriteString("\n      OPENLIA_TOOLS_DATA_ROOT: /opt/data\n    volumes:\n      - type: bind\n        source: ")
		builder.Write(dataRoot)
		builder.WriteString("\n        target: /opt/data\n    networks:\n      - openlia-private\n")
		builder.WriteString("    deploy:\n      resources:\n        limits:\n          cpus: \"1.0\"\n          memory: 1G\n")
		builder.WriteString("    logging:\n      driver: \"json-file\"\n      options:\n        max-size: \"20m\"\n        max-file: \"5\"\n")
	}
	for _, host := range hosts.Hosts {
		configPath := filepath.Join(config.LochoRoot, host.Host, "attachments.toml")
		quoted, _ := json.Marshal(configPath)
		fmt.Fprintf(&builder, "  locho-%s:\n", host.Host)
		builder.WriteString("    image: \"${OPENLIA_LOCHO_IMAGE:-openlia-locho:v1.2.0-beta.1}\"\n")
		builder.WriteString("    build:\n      context: ..\n      dockerfile: docker/locho.Dockerfile\n      args:\n        LOCHO_BASE_IMAGE: \"${LOCHO_BASE_IMAGE:-debian}\"\n        LOCHO_BASE_TAG: \"${LOCHO_BASE_TAG:-13.4-slim}\"\n        LOCHO_BASE_DIGEST: \"${LOCHO_BASE_DIGEST:-sha256:109e2c65005bf160609e4ba6acf7783752f8502ad218e298253428690b9eaa4b}\"\n        LOCHO_VERSION: \"1.2.0-beta.1\"\n        LOCHO_X86_64_SHA256: \"9d257c856f0a9c8220285db45c28c6227dfa76017d160f74490cfef7bd784ad4\"\n        LOCHO_AARCH64_SHA256: \"1c0e67b130734467783e5e48a69d3003d218a4da624ba5841f3c5e6840f19c18\"\n")
		builder.WriteString("    command: [\"attach\", \"--config\", \"/etc/locho/attachments.toml\"]\n    restart: unless-stopped\n    read_only: true\n    tmpfs:\n      - /tmp\n    security_opt:\n      - no-new-privileges:true\n    cap_drop: [ALL]\n    volumes:\n")
		fmt.Fprintf(&builder, "      - type: bind\n        source: %s\n        target: /etc/locho/attachments.toml\n        read_only: true\n    networks:\n      - openlia-private\n", quoted)
		builder.WriteString("    deploy:\n      resources:\n        limits:\n          cpus: \"0.5\"\n          memory: 512M\n")
		builder.WriteString("    logging:\n      driver: \"json-file\"\n      options:\n        max-size: \"20m\"\n        max-file: \"5\"\n")
	}
	if config.ExternalNetwork != "" {
		fmt.Fprintf(&builder, "networks:\n  openlia-external:\n    name: %q\n    external: true\n", config.ExternalNetwork)
	}
	if err := AtomicWriteFile(config.GeneratedCompose, []byte(builder.String()), 0o600); err != nil {
		return err
	}
	if err := ReconcileBrowserPolicy(config, hasPlaywrightBrowserRole, browserURL); err != nil {
		return err
	}
	return nil
}

func RotateAttachment(config Config, host, source string, now time.Time, composers ...Compose) error {
	return RotateAttachmentContext(context.Background(), config, host, source, now, composers...)
}

func RotateAttachmentContext(ctx context.Context, config Config, host, source string, now time.Time, composers ...Compose) error {
	if len(composers) > 0 {
		return rotateAttachmentWithCompose(ctx, config, composers[0], host, source, now)
	}
	if err := config.ValidatePaths(); err != nil {
		return err
	}
	if err := ValidateSafeComponent(host, "host"); err != nil {
		return err
	}
	if err := validateExternalSource(config, source, "attachment source"); err != nil {
		return err
	}
	if err := validateAttachmentFile(source); err != nil {
		return err
	}
	targetDir := filepath.Join(config.LochoRoot, host)
	if err := EnsureDir(targetDir, 0o700); err != nil {
		return err
	}
	targetFile := filepath.Join(targetDir, "attachments.toml")
	configBackup := ""
	if info, err := os.Stat(targetFile); err == nil && info.Mode().IsRegular() {
		configBackup, err = backupFile(config, targetFile, "locho-"+host, 0o600, now)
		if err != nil {
			return err
		}
	}
	if _, err := CreateBackup(config, "locho-"+host+"-rotate", now); err != nil {
		return err
	}
	if err := AtomicCopyFile(source, targetFile, 0o600); err != nil {
		return err
	}
	if err := GenerateAttachments(config); err != nil {
		if configBackup != "" {
			_ = AtomicCopyFile(configBackup, targetFile, 0o600)
		} else {
			_ = os.Remove(targetFile)
		}
		return err
	}
	return RecordChange(config, "locho-"+host+"-rotate", "ok", configBackup, "single host sidecar replaced", now)
}

func rotateAttachmentWithCompose(ctx context.Context, config Config, compose Compose, host, source string, now time.Time) error {
	if err := config.ValidatePaths(); err != nil {
		return err
	}
	if config.APIHost != "127.0.0.1" {
		return fmt.Errorf("API host must remain 127.0.0.1")
	}
	if err := ValidateSafeComponent(host, "host"); err != nil {
		return err
	}
	if err := validateExternalSource(config, source, "attachment source"); err != nil {
		return err
	}
	if err := validateAttachmentFile(source); err != nil {
		return err
	}
	if err := EnsureDir(config.LochoRoot, 0o700); err != nil {
		return err
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return err
	}
	targetDir := filepath.Join(config.LochoRoot, host)
	targetFile := filepath.Join(targetDir, "attachments.toml")
	configBackup := ""
	if info, err := os.Stat(targetFile); err == nil && info.Mode().IsRegular() {
		configBackup, err = backupFile(config, targetFile, "locho-"+host, 0o600, now)
		if err != nil {
			return err
		}
	}
	if _, err := CreateBackup(config, "locho-"+host+"-rotate", now, compose); err != nil {
		return err
	}
	generatedBackup := ""
	generatedPresent := false
	if info, err := os.Stat(config.GeneratedCompose); err == nil && info.Mode().IsRegular() {
		generatedPresent = true
		generatedBackup, err = backupFile(config, config.GeneratedCompose, "compose-generated", 0o600, now)
		if err != nil {
			return err
		}
	}
	previousState, err := ReadState(config)
	if err != nil {
		return err
	}
	if configBackup != "" {
		_, _ = compose.Run(ctx, "stop", "locho-"+host)
	}
	restore := func() {
		if configBackup != "" {
			_ = AtomicCopyFile(configBackup, targetFile, 0o600)
		} else {
			_ = os.Remove(targetFile)
		}
		if generatedBackup != "" {
			_ = AtomicCopyFile(generatedBackup, config.GeneratedCompose, 0o600)
		} else if !generatedPresent {
			_ = os.Remove(config.GeneratedCompose)
		}
	}
	if err := EnsureDir(targetDir, 0o700); err != nil {
		return err
	}
	if err := AtomicCopyFile(source, targetFile, 0o600); err != nil {
		restore()
		if previousState == stateRunning {
			_, _ = compose.Run(ctx, "up", "-d", "--no-deps", "locho-"+host)
		}
		_ = RecordChange(config, "locho-"+host+"-rotate", "failed", configBackup, "attachment replacement failed", now)
		return fmt.Errorf("attachment replacement failed")
	}
	if err := generateAttachmentsFile(config); err != nil {
		restore()
		if previousState == stateRunning {
			_, _ = compose.Run(ctx, "up", "-d", "--no-deps", "locho-"+host)
		}
		_ = RecordChange(config, "locho-"+host+"-rotate", "failed", configBackup, "sidecar configuration replacement failed", now)
		return fmt.Errorf("sidecar configuration replacement failed; previous attachment was restored")
	}
	if _, err := compose.Run(ctx, "config", "--quiet"); err != nil {
		restore()
		if previousState == stateRunning {
			_, _ = compose.Run(ctx, "up", "-d", "--no-deps", "locho-"+host)
		}
		_ = RecordChange(config, "locho-"+host+"-rotate", "failed", configBackup, "generated Compose validation failed", now)
		return fmt.Errorf("generated Compose validation failed; previous attachment was restored")
	}
	if previousState == stateRunning {
		if res, err := compose.Run(ctx, "up", "-d", "--no-deps", "--remove-orphans", "locho-"+host); err != nil {
			restore()
			_, _ = compose.Run(ctx, "up", "-d", "--no-deps", "locho-"+host)
			msg := strings.TrimSpace(string(res.Stderr))
			if msg == "" {
				msg = strings.TrimSpace(string(res.Stdout))
			}
			_ = RecordChange(config, "locho-"+host+"-rotate", "failed", configBackup, "Locho restart failed: "+msg, now)
			return fmt.Errorf("Locho restart failed (%s): %w; previous attachment was restored", msg, err)
		}
	}
	return RecordChange(config, "locho-"+host+"-rotate", "ok", configBackup, "single host sidecar replaced", now)
}

func backupFile(config Config, source, label string, mode os.FileMode, now time.Time) (string, error) {
	if err := ValidateSafeComponent(label, "backup-label"); err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("cannot back up a missing file")
	}
	if err := EnsureDir(config.BackupRoot, 0o700); err != nil {
		return "", err
	}
	destination := filepath.Join(config.BackupRoot, fmt.Sprintf("%s-%s-%d", label, now.UTC().Format("20060102T150405Z"), os.Getpid()))
	for suffix := 1; ; suffix++ {
		if _, statErr := os.Lstat(destination); os.IsNotExist(statErr) {
			break
		} else if statErr != nil {
			return "", statErr
		}
		destination = filepath.Join(config.BackupRoot, fmt.Sprintf("%s-%s-%d-%d", label, now.UTC().Format("20060102T150405Z"), os.Getpid(), suffix))
	}
	if err := AtomicCopyFile(source, destination, mode); err != nil {
		return "", err
	}
	return destination, nil
}

func serviceInventory(host, path string, roles map[string]string) ([]LochoService, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var services []LochoService
	browserPort := 0
	inService := false
	capability := ""
	listenPort := 0
	flush := func() {
		if !inService {
			return
		}
		parts := strings.Split(capability, ":")
		name := ""
		proto := "http"
		if len(parts) >= 3 {
			name = parts[0]
			proto = parts[1]
		} else if len(parts) == 2 {
			name = parts[0]
			if parts[1] == "http" || parts[1] == "tcp" {
				proto = parts[1]
			}
		} else if len(parts) == 1 {
			name = parts[0]
		}
		if name == "" {
			name = fmt.Sprintf("service-%d", len(services)+1)
		}
		role := "unassigned"
		if assigned, ok := roles[host+"."+name]; ok && assigned != "" {
			role = assigned
		} else if assigned, ok := roles[name]; ok && assigned != "" {
			role = assigned
		}
		endpoint := fmt.Sprintf("http://locho-%s:%d", host, listenPort)
		if proto == "tcp" && role != "playwright-browser" {
			endpoint = fmt.Sprintf("locho-%s:%d", host, listenPort)
		}
		services = append(services, LochoService{
			Host:     host,
			Name:     name,
			Protocol: proto,
			Port:     listenPort,
			Endpoint: endpoint,
			Role:     role,
		})
		if role == "playwright-browser" && browserPort == 0 && listenPort > 0 {
			browserPort = listenPort
		} else if browserPort == 0 && (strings.HasPrefix(capability, "playwright:") || name == "playwright") && listenPort > 0 {
			browserPort = listenPort
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "[[services]]" {
			flush()
			inService = true
			capability = ""
			listenPort = 0
			continue
		}
		if !inService {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "capability":
			if parsed, parseErr := strconv.Unquote(strings.TrimSpace(value)); parseErr == nil {
				capability = parsed
			}
		case "listen_port":
			if parsed, parseErr := strconv.Atoi(strings.TrimSpace(value)); parseErr == nil {
				listenPort = parsed
			}
		}
	}
	flush()
	return services, browserPort, nil
}

func sanitizeEnvKey(name string) string {
	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.ToUpper(builder.String())
}

func validateAttachmentFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("attachment source is not a readable file")
	}
	hasHost, hasListen, services := false, false, 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "host_id ="), strings.HasPrefix(line, "host_id="):
			hasHost = true
		case strings.HasPrefix(line, "listen_host ="), strings.HasPrefix(line, "listen_host="):
			hasListen = true
		case line == "[[services]]":
			services++
		}
	}
	if !hasHost || !hasListen || services == 0 {
		return fmt.Errorf("attachment source is missing required Locho fields")
	}
	return nil
}

func validateExternalSource(config Config, path, label string) error {
	if err := ValidateAbsolutePath(path, label); err != nil {
		return err
	}
	if within(path, filepath.Join(config.RepositoryRoot, "local")) {
		return fmt.Errorf("%s must not be under local/", label)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a readable file", label)
	}
	return nil
}

func FormatAttachmentListHuman(result AttachmentListResult) string {
	if len(result.Hosts) == 0 {
		return "openlia attachments: no hosts configured"
	}
	var builder strings.Builder
	builder.WriteString("openlia attachments:\n")
	for i, host := range result.Hosts {
		if i > 0 {
			builder.WriteString("\n")
		}
		plural := "s"
		if len(host.Services) == 1 {
			plural = ""
		}
		fmt.Fprintf(&builder, "  Host: %s (%d service%s configured)\n", host.Host, len(host.Services), plural)
		if len(host.Services) == 0 {
			builder.WriteString("    (no services declared)\n")
		}
		for _, svc := range host.Services {
			fmt.Fprintf(&builder, "    - %s (%s, port %d) -> %s [role: %s]\n", svc.Name, svc.Protocol, svc.Port, svc.Endpoint, svc.Role)
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}
