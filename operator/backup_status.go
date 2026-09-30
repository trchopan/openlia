package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupArchiveInfo struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Encrypted bool   `json:"encrypted"`
	SHA256    string `json:"sha256,omitempty"`
}

type BackupStatus struct {
	OK                bool                      `json:"ok"`
	Schedule          string                    `json:"schedule"`
	ScheduleEnabled   bool                      `json:"schedule_enabled"`
	ScheduleInstalled bool                      `json:"schedule_installed"`
	Timezone          string                    `json:"timezone"`
	NextRun           string                    `json:"next_run,omitempty"`
	LatestLocal       *BackupArchiveInfo        `json:"latest_local,omitempty"`
	LocalArchives     []BackupArchiveInfo       `json:"local_archives"`
	Destinations      []BackupDestinationResult `json:"destinations"`
	RemoteRetention   int                       `json:"remote_retention"`
}

func ReadBackupStatus(config Config) (BackupStatus, error) {
	if err := config.ValidatePaths(); err != nil {
		return BackupStatus{}, err
	}
	status := BackupStatus{
		OK:                true,
		Schedule:          config.BackupSchedule,
		ScheduleEnabled:   config.BackupScheduleEnabled,
		ScheduleInstalled: backupScheduleInstalled(config),
		Timezone:          config.Timezone,
		LocalArchives:     []BackupArchiveInfo{},
		Destinations:      []BackupDestinationResult{},
		RemoteRetention:   config.BackupRemoteRetention,
	}
	if status.Timezone == "" {
		status.Timezone = "Asia/Ho_Chi_Minh"
	}
	if status.ScheduleEnabled {
		if next, err := nextBackupSchedule(config.BackupSchedule, status.Timezone, time.Now()); err == nil {
			status.NextRun = next.Format(time.RFC3339)
		}
	}
	entries, err := os.ReadDir(config.BackupRoot)
	if err != nil && !os.IsNotExist(err) {
		return BackupStatus{}, err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "openlia-") || !strings.HasSuffix(entry.Name(), ".tar.gz.age") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		archive := BackupArchiveInfo{Name: entry.Name(), Encrypted: strings.HasSuffix(entry.Name(), ".age")}
		metadataData, readErr := os.ReadFile(filepath.Join(config.BackupRoot, entry.Name()+".json"))
		if readErr == nil {
			var metadata backupMetadata
			if json.Unmarshal(metadataData, &metadata) == nil {
				archive.CreatedAt = metadata.CreatedAt
				archive.Reason = metadata.Reason
				archive.SHA256 = metadata.SHA256
				archive.Encrypted = metadata.Encrypted
			}
		}
		if archive.CreatedAt == "" {
			archive.CreatedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
		status.LocalArchives = append(status.LocalArchives, archive)
	}
	sort.Slice(status.LocalArchives, func(i, j int) bool { return status.LocalArchives[i].CreatedAt > status.LocalArchives[j].CreatedAt })
	if len(status.LocalArchives) > 0 {
		latest := status.LocalArchives[0]
		status.LatestLocal = &latest
	}
	destinationState, err := readBackupDestinationState(config)
	if err != nil {
		return BackupStatus{}, err
	}
	status.Destinations = destinationState.LastCompleted
	return status, nil
}

func latestEncryptedBackup(config Config) (string, error) {
	entries, err := os.ReadDir(config.BackupRoot)
	if err != nil {
		return "", err
	}
	var selected string
	var latest time.Time
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "openlia-") || !strings.HasSuffix(entry.Name(), ".tar.gz.age") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if selected == "" || info.ModTime().After(latest) {
			selected = filepath.Join(config.BackupRoot, entry.Name())
			latest = info.ModTime()
		}
	}
	if selected == "" {
		return "", fmt.Errorf("no encrypted durable backup is available")
	}
	return selected, nil
}

func FormatBackupStatus(status BackupStatus) string {
	state := "enabled"
	if !status.ScheduleEnabled {
		state = "disabled"
	}
	installed := "not installed"
	if status.ScheduleInstalled {
		installed = "installed"
	}
	line := fmt.Sprintf("Backup schedule %s (%s): %s (%s)", state, installed, status.Schedule, status.Timezone)
	if status.NextRun != "" {
		line += "\nNext run: " + status.NextRun
	}
	if status.LatestLocal != nil {
		line += fmt.Sprintf("\nLatest local archive: %s (%s)", status.LatestLocal.Name, status.LatestLocal.CreatedAt)
	} else {
		line += "\nLatest local archive: none"
	}
	for _, destination := range status.Destinations {
		state := "failed"
		if destination.OK {
			state = "uploaded"
		}
		line += fmt.Sprintf("\n%s (%s): %s %s", destination.Name, destination.Type, state, destination.Object)
		if destination.Error != "" {
			line += " — " + destination.Error
		}
	}
	if len(status.Destinations) == 0 {
		line += "\nRemote destinations: no successful upload recorded"
	}
	return line
}
