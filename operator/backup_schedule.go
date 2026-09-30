package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type BackupScheduleResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Enabled   bool   `json:"enabled"`
	Schedule  string `json:"schedule"`
	Timezone  string `json:"timezone"`
	Installed string `json:"installed,omitempty"`
}

type backupScheduleState struct {
	Schema          int    `json:"schema"`
	InstalledAt     string `json:"installed_at"`
	LastAttemptSlot string `json:"last_attempt_slot,omitempty"`
	LastResult      string `json:"last_result,omitempty"`
}

func nextBackupSchedule(expression, timezone string, after time.Time) (time.Time, error) {
	if timezone == "" {
		timezone = "Asia/Ho_Chi_Minh"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, err
	}
	schedule, err := cron.ParseStandard(expression)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(after.In(location)), nil
}

func BackupScheduleTick(ctx context.Context, config Config, now time.Time) (BackupResult, bool, error) {
	if err := config.ValidatePaths(); err != nil {
		return BackupResult{}, false, err
	}
	if !config.BackupScheduleEnabled {
		return BackupResult{}, false, nil
	}
	if config.BackupRecipient == "" {
		return BackupResult{}, false, fmt.Errorf("backup encryption recipient is not configured")
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return BackupResult{}, false, err
	}
	schedule, err := cron.ParseStandard(config.BackupSchedule)
	if err != nil {
		return BackupResult{}, false, err
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return BackupResult{}, false, err
	}
	statePath := filepath.Join(config.MetaRoot, "backup-schedule-state.json")
	state := backupScheduleState{Schema: 1, InstalledAt: utcTimestamp(now)}
	if data, readErr := os.ReadFile(statePath); readErr == nil {
		if err := json.Unmarshal(data, &state); err != nil || state.Schema != 1 {
			return BackupResult{}, false, fmt.Errorf("backup schedule state is invalid")
		}
	} else if !os.IsNotExist(readErr) {
		return BackupResult{}, false, readErr
	}
	start, err := time.Parse(time.RFC3339Nano, state.InstalledAt)
	if err != nil {
		start = now
	}
	if state.LastAttemptSlot != "" {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, state.LastAttemptSlot); parseErr == nil {
			start = parsed
		}
	}
	start = start.In(location)
	current := now.In(location).Truncate(time.Minute)
	var due time.Time
	for count := 0; count < 100000; count++ {
		next := schedule.Next(start)
		if next.After(current) {
			break
		}
		due = next
		start = next
	}
	if due.IsZero() {
		return BackupResult{}, false, nil
	}
	state.LastAttemptSlot = due.UTC().Format(time.RFC3339Nano)
	stateData, _ := json.Marshal(state)
	if err := AtomicWriteFile(statePath, append(stateData, '\n'), 0o600); err != nil {
		return BackupResult{}, false, err
	}
	result, err := CreateScheduledBackup(ctx, config, "scheduled", now)
	if err == nil {
		result.Destinations, err = PushBackup(ctx, config, result, now)
	}
	state.LastResult = "ok"
	if err != nil {
		state.LastResult = "failed"
	}
	stateData, _ = json.Marshal(state)
	if writeErr := AtomicWriteFile(statePath, append(stateData, '\n'), 0o600); writeErr != nil && err == nil {
		err = writeErr
	}
	changeStatus := "ok"
	if err != nil {
		changeStatus = "partial"
	}
	if recordErr := RecordChange(config, "backup", changeStatus, result.Archive, "reason=scheduled", now); recordErr != nil && err == nil {
		err = recordErr
	}
	return result, true, err
}

func InstallBackupSchedule(config Config, now time.Time) (BackupScheduleResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return BackupScheduleResult{}, err
	}
	if config.BackupRecipient == "" {
		return BackupScheduleResult{}, fmt.Errorf("backup encryption recipient is required before enabling the schedule")
	}
	if !config.BackupScheduleEnabled {
		return RemoveBackupSchedule(config)
	}
	if _, err := cron.ParseStandard(config.BackupSchedule); err != nil {
		return BackupScheduleResult{}, fmt.Errorf("invalid backup schedule: %w", err)
	}
	for _, destination := range config.BackupDestinations {
		if destination.Type != "rsync" {
			continue
		}
		if _, err := exec.LookPath("rsync"); err != nil {
			return BackupScheduleResult{}, fmt.Errorf("rsync destination %s requires rsync on the target", destination.Name)
		}
		if destination.IdentityFile != "" {
			info, err := os.Stat(destination.IdentityFile)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
				return BackupScheduleResult{}, fmt.Errorf("rsync identity for destination %s must be a protected regular file", destination.Name)
			}
		}
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return BackupScheduleResult{}, err
	}
	config = scheduleRuntimeConfig(config)
	configData, err := json.Marshal(config)
	if err != nil {
		return BackupScheduleResult{}, err
	}
	configPath := filepath.Join(config.MetaRoot, "backup-schedule-config.json")
	if err := AtomicWriteFile(configPath, append(configData, '\n'), 0o600); err != nil {
		return BackupScheduleResult{}, err
	}
	if runtime.GOOS == "linux" {
		var err error
		if config.LocalMode {
			err = installUserSystemdBackupSchedule(config)
		} else {
			err = installSystemdBackupSchedule(config, configPath)
		}
		if err != nil {
			return BackupScheduleResult{}, err
		}
	} else if runtime.GOOS == "darwin" && config.LocalMode {
		if err := installLaunchdBackupSchedule(config); err != nil {
			return BackupScheduleResult{}, err
		}
	} else {
		return BackupScheduleResult{}, fmt.Errorf("managed backup scheduling is supported on Linux targets and local macOS deployments")
	}
	statePath := filepath.Join(config.MetaRoot, "backup-schedule-state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		state := backupScheduleState{Schema: 1, InstalledAt: utcTimestamp(now)}
		data, _ := json.Marshal(state)
		if err := AtomicWriteFile(statePath, append(data, '\n'), 0o600); err != nil {
			return BackupScheduleResult{}, err
		}
	}
	return BackupScheduleResult{OK: true, Action: "schedule-install", Enabled: true, Schedule: config.BackupSchedule, Timezone: config.Timezone, Installed: utcTimestamp(now)}, nil
}

func RemoveBackupSchedule(config Config) (BackupScheduleResult, error) {
	if runtime.GOOS == "linux" {
		var err error
		if config.LocalMode {
			err = removeUserSystemdBackupSchedule(config)
		} else {
			err = removeSystemdBackupSchedule(config)
		}
		if err != nil {
			return BackupScheduleResult{}, err
		}
	} else if runtime.GOOS == "darwin" && config.LocalMode {
		if err := removeLaunchdBackupSchedule(config); err != nil {
			return BackupScheduleResult{}, err
		}
	}
	return BackupScheduleResult{OK: true, Action: "schedule-remove", Enabled: false, Schedule: config.BackupSchedule, Timezone: config.Timezone}, nil
}

func backupScheduleInstalled(config Config) bool {
	unitName := "openlia-backup-" + config.ProjectName
	var path string
	switch runtime.GOOS {
	case "linux":
		if config.LocalMode {
			home, err := os.UserHomeDir()
			if err != nil {
				return false
			}
			path = filepath.Join(home, ".config", "systemd", "user", unitName+".timer")
		} else {
			path = filepath.Join("/etc/systemd/system", unitName+".timer")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		path = filepath.Join(home, "Library", "LaunchAgents", "com.openlia.backup."+config.ProjectName+".plist")
	default:
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func installUserSystemdBackupSchedule(config Config) error {
	if config.OperatorConfigFile == "" {
		return fmt.Errorf("operator config path is not available for the local schedule")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl is required to install the local backup schedule")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unitDirectory := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDirectory, 0o700); err != nil {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	unitName := "openlia-backup-" + config.ProjectName
	service := "[Unit]\nDescription=OpenLia encrypted backup\n\n[Service]\nType=oneshot\nStandardOutput=null\nStandardError=journal\nEnvironment=OPENLIA_CONFIG=" + systemdEscape(config.OperatorConfigFile) + "\nExecStart=" + systemdEscape(program) + " backup tick\n"
	timer := "[Unit]\nDescription=OpenLia backup schedule\n\n[Timer]\nOnCalendar=*-*-* *:*:00\nPersistent=true\nAccuracySec=1s\nUnit=" + unitName + ".service\n\n[Install]\nWantedBy=timers.target\n"
	if err := AtomicWriteFile(filepath.Join(unitDirectory, unitName+".service"), []byte(service), 0o600); err != nil {
		return err
	}
	if err := AtomicWriteFile(filepath.Join(unitDirectory, unitName+".timer"), []byte(timer), 0o600); err != nil {
		return err
	}
	return reloadAndEnableSystemdTimer(runSystemctlUser, unitName)
}

func removeUserSystemdBackupSchedule(config Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unitName := "openlia-backup-" + config.ProjectName
	unitDirectory := filepath.Join(home, ".config", "systemd", "user")
	timerPath := filepath.Join(unitDirectory, unitName+".timer")
	servicePath := filepath.Join(unitDirectory, unitName+".service")
	_, timerErr := os.Stat(timerPath)
	_, serviceErr := os.Stat(servicePath)
	if os.IsNotExist(timerErr) && os.IsNotExist(serviceErr) {
		return nil
	}
	_ = runSystemctlUser("stop", unitName+".service")
	_ = runSystemctlUser("disable", "--now", unitName+".timer")
	for _, path := range []string{timerPath, servicePath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return runSystemctlUser("daemon-reload")
}

func runSystemctlUser(args ...string) error {
	commandArgs := append([]string{"--user"}, args...)
	command := exec.Command("systemctl", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func reloadAndEnableSystemdTimer(run func(...string) error, unitName string) error {
	if err := run("daemon-reload"); err != nil {
		return err
	}
	return run("enable", "--now", unitName+".timer")
}

func scheduleRuntimeConfig(config Config) Config {
	current := filepath.Join(config.InstallRoot, "current")
	config.RepositoryRoot = current
	config.ComposeFile = filepath.Join(current, "docker", "compose.yaml")
	config.ComposeProjectDir = filepath.Join(current, "docker")
	config.GeneratedCompose = filepath.Join(current, "docker", "compose.generated.yaml")
	return config
}

func installSystemdBackupSchedule(config Config, configPath string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl is required to install the target backup schedule")
	}
	flockPath, err := exec.LookPath("flock")
	if err != nil {
		return fmt.Errorf("flock is required to serialize the target backup schedule")
	}
	architecture := runtime.GOARCH
	if architecture != "amd64" && architecture != "arm64" {
		return fmt.Errorf("unsupported target architecture for backup schedule: %s", architecture)
	}
	unitName := "openlia-backup-" + config.ProjectName
	operatorPath := filepath.Join(config.InstallRoot, "current", "operator", "linux-"+architecture, "openlia-operator")
	servicePath := filepath.Join("/etc/systemd/system", unitName+".service")
	timerPath := filepath.Join("/etc/systemd/system", unitName+".timer")
	lockPath := config.InstallRoot + ".operation.lock"
	service := "[Unit]\nDescription=OpenLia encrypted backup\nAfter=network-online.target\n\n[Service]\nType=oneshot\nStandardOutput=null\nStandardError=journal\nEnvironment=OPENLIA_OPERATOR_CONFIG_FILE=" + systemdEscape(configPath) + "\nExecStart=" + systemdEscape(flockPath) + " -n " + systemdEscape(lockPath) + " " + systemdEscape(operatorPath) + " backup tick\n"
	timer := "[Unit]\nDescription=OpenLia backup schedule\n\n[Timer]\nOnCalendar=*-*-* *:*:00\nPersistent=true\nAccuracySec=1s\nUnit=" + unitName + ".service\n\n[Install]\nWantedBy=timers.target\n"
	if err := AtomicWriteFile(servicePath, []byte(service), 0o644); err != nil {
		return err
	}
	if err := AtomicWriteFile(timerPath, []byte(timer), 0o644); err != nil {
		return err
	}
	if err := reloadAndEnableSystemdTimer(runSystemctl, unitName); err != nil {
		return fmt.Errorf("enable backup timer: %w", err)
	}
	return nil
}

func removeSystemdBackupSchedule(config Config) error {
	unitName := "openlia-backup-" + config.ProjectName
	servicePath := filepath.Join("/etc/systemd/system", unitName+".service")
	timerPath := filepath.Join("/etc/systemd/system", unitName+".timer")
	_, serviceErr := os.Stat(servicePath)
	_, timerErr := os.Stat(timerPath)
	if os.IsNotExist(serviceErr) && os.IsNotExist(timerErr) {
		return nil
	}
	_ = runSystemctl("disable", "--now", unitName+".timer")
	for _, path := range []string{timerPath, servicePath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return runSystemctl("daemon-reload")
}

func runSystemctl(args ...string) error {
	command := exec.Command("systemctl", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func systemdEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), " ", "\\x20")
}

func installLaunchdBackupSchedule(config Config) error {
	if config.OperatorConfigFile == "" {
		return fmt.Errorf("operator config path is not available for launchd")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(home, "Library", "Logs"), 0o700); err != nil {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	label := "com.openlia.backup." + config.ProjectName
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	logPath := filepath.Join(home, "Library", "Logs", "openlia-backup-"+config.ProjectName+".log")
	plist := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>backup</string><string>tick</string></array><key>StartInterval</key><integer>60</integer><key>EnvironmentVariables</key><dict><key>OPENLIA_CONFIG</key><string>%s</string></dict><key>StandardOutPath</key><string>/dev/null</string><key>StandardErrorPath</key><string>%s</string><key>RunAtLoad</key><true/></dict></plist>\n", xmlEscape(label), xmlEscape(program), xmlEscape(config.OperatorConfigFile), xmlEscape(logPath))
	if err := AtomicWriteFile(plistPath, []byte(plist), 0o600); err != nil {
		return err
	}
	return exec.Command("launchctl", "load", "-w", plistPath).Run()
}

func removeLaunchdBackupSchedule(config Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	label := "com.openlia.backup." + config.ProjectName
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	_ = exec.Command("launchctl", "unload", "-w", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	logPath := filepath.Join(home, "Library", "Logs", "openlia-backup-"+config.ProjectName+".log")
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return strings.ReplaceAll(value, "\"", "&quot;")
}
