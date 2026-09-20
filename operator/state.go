package operator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	StateNeverStarted = "never-started"
	StateRunning      = "running"
	StateStopped      = "stopped"
	stateNeverStarted = StateNeverStarted
	stateRunning      = StateRunning
	stateStopped      = StateStopped
)

// ReadState returns never-started when the state marker is absent.
func ReadState(config Config) (string, error) {
	contents, err := os.ReadFile(config.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return stateNeverStarted, nil
	}
	if err != nil {
		return "", fmt.Errorf("read stack state: %w", err)
	}
	state := strings.TrimRight(string(contents), "\n")
	if state != stateNeverStarted && state != stateRunning && state != stateStopped {
		return "", fmt.Errorf("invalid stack state")
	}
	return state, nil
}

// WriteState atomically writes one of the shell-compatible stack states.
func WriteState(config Config, state string) error {
	if state != stateNeverStarted && state != stateRunning && state != stateStopped {
		return fmt.Errorf("invalid stack state")
	}
	return AtomicWriteFile(config.StateFile, []byte(state+"\n"), 0o600)
}

// RuntimeMetadata preserves meta/runtime.json's existing schema and keys.
type RuntimeMetadata struct {
	Schema       int    `json:"schema"`
	InstallRoot  string `json:"install_root"`
	Project      string `json:"project"`
	Network      string `json:"network"`
	RuntimeRoot  string `json:"runtime_root"`
	HermesData   string `json:"hermes_data"`
	LochoRoot    string `json:"locho_root"`
	SecretSource string `json:"secret_source"`
	CreatedAt    string `json:"created_at"`
}

// LastChange preserves meta/last-change.json's existing schema and keys.
type LastChange struct {
	Schema     int    `json:"schema"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	Backup     string `json:"backup"`
	Detail     string `json:"detail"`
	RecordedAt string `json:"recorded_at"`
}

func WriteRuntimeMetadata(config Config, now time.Time) error {
	metadata := RuntimeMetadata{
		Schema:       1,
		InstallRoot:  config.InstallRoot,
		Project:      config.ProjectName,
		Network:      config.NetworkName,
		RuntimeRoot:  config.RuntimeRoot,
		HermesData:   config.DataRoot,
		LochoRoot:    config.LochoRoot,
		SecretSource: "docker-secret",
		CreatedAt:    utcTimestamp(now),
	}
	contents, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return AtomicWriteFile(config.MetaRoot+"/runtime.json", append(contents, '\n'), 0o600)
}

func RecordChange(config Config, action, status, backup, detail string, now time.Time) error {
	change := LastChange{
		Schema:     1,
		Action:     action,
		Status:     status,
		Backup:     backup,
		Detail:     detail,
		RecordedAt: utcTimestamp(now),
	}
	contents, err := json.Marshal(change)
	if err != nil {
		return err
	}
	return AtomicWriteFile(config.MetaRoot+"/last-change.json", append(contents, '\n'), 0o600)
}

func utcTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05Z07:00")
}
