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

type AttachmentHost struct {
	Host         string `json:"host"`
	Configured   bool   `json:"configured"`
	ServiceCount int    `json:"service_count"`
	Capabilities string `json:"capabilities"`
	BrowserPort  int    `json:"-"`
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
		path := filepath.Join(config.LochoRoot, entry.Name(), "attachments.toml")
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		count, browserPort, err := serviceInventory(path)
		if err != nil {
			return AttachmentListResult{}, err
		}
		result.Hosts = append(result.Hosts, AttachmentHost{Host: entry.Name(), Configured: true, ServiceCount: count, Capabilities: "redacted", BrowserPort: browserPort})
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
	return RecordChange(config, "attachments-generate", "ok", "", "Compose sidecars regenerated", time.Now().UTC())
}

func generateAttachmentsFile(config Config) error {
	hosts, err := ListAttachments(config)
	if err != nil {
		return err
	}
	browserURL := ""
	for _, host := range hosts.Hosts {
		if host.BrowserPort == 0 {
			continue
		}
		if browserURL != "" {
			return fmt.Errorf("multiple Playwright attachment endpoints are configured")
		}
		browserURL = fmt.Sprintf("http://locho-%s:%d", host.Host, host.BrowserPort)
	}
	var builder strings.Builder
	if len(hosts.Hosts) == 0 && !config.APIEnabled && config.ExternalNetwork == "" {
		builder.WriteString("services: {}\n")
	} else {
		builder.WriteString("services:\n")
		if config.APIEnabled || config.ExternalNetwork != "" || browserURL != "" {
			builder.WriteString("  hermes:\n")
			if config.APIEnabled {
				fmt.Fprintf(&builder, "    ports:\n      - %q\n", config.APIHost+":8642:8642")
			}
			if config.APIEnabled || browserURL != "" {
				builder.WriteString("    environment:\n")
				if config.APIEnabled {
					builder.WriteString("      API_SERVER_ENABLED: \"true\"\n      API_SERVER_HOST: \"0.0.0.0\"\n")
				}
				if browserURL != "" {
					fmt.Fprintf(&builder, "      OPENLIA_BROWSER_MCP_URL: %q\n", browserURL)
					builder.WriteString("      OPENLIA_TOOLS_URL: \"http://openlia-tools:8787\"\n")
				}
			}
			if config.ExternalNetwork != "" {
				builder.WriteString("    networks:\n      - openlia-private\n      - openlia-external\n")
			}
		}
	}
	if browserURL != "" {
		dataRoot, _ := json.Marshal(config.DataRoot)
		fmt.Fprintf(&builder, "  openlia-tools:\n    image: \"${OPENLIA_TOOLS_IMAGE:-openlia-tools:v0.1.0}\"\n")
		builder.WriteString("    build:\n      context: ..\n      dockerfile: docker/tools.Dockerfile\n")
		builder.WriteString("    command: [\"python3\", \"/opt/openlia/tools/openlia_tools.py\"]\n    restart: unless-stopped\n    read_only: true\n    tmpfs:\n      - /tmp\n    security_opt:\n      - no-new-privileges:true\n    cap_drop: [ALL]\n")
		builder.WriteString("    environment:\n      HERMES_HOME: /opt/data\n      OPENLIA_BROWSER_MCP_URL: ")
		quotedBrowserURL, _ := json.Marshal(browserURL)
		builder.Write(quotedBrowserURL)
		builder.WriteString("\n      OPENLIA_TOOLS_DATA_ROOT: /opt/data\n      PYTHONUNBUFFERED: \"1\"\n    volumes:\n      - type: bind\n        source: ")
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
	return AtomicWriteFile(config.GeneratedCompose, []byte(builder.String()), 0o600)
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
		if _, err := compose.Run(ctx, "up", "-d", "--no-deps", "locho-"+host); err != nil {
			restore()
			_, _ = compose.Run(ctx, "up", "-d", "--no-deps", "locho-"+host)
			_ = RecordChange(config, "locho-"+host+"-rotate", "failed", configBackup, "Locho restart failed; previous attachment restored", now)
			return fmt.Errorf("Locho restart failed; previous attachment was restored")
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

func serviceInventory(path string) (int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	count := 0
	browserPort := 0
	inService := false
	capability := ""
	listenPort := 0
	flush := func() {
		if !inService {
			return
		}
		count++
		if browserPort == 0 && strings.HasPrefix(capability, "playwright:") && listenPort > 0 {
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
	return count, browserPort, nil
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
