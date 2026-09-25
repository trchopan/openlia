package operator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	MigrationStatusPending    = "pending"
	MigrationStatusScanning   = "scanning"
	MigrationStatusProcessing = "processing"
	MigrationStatusDone       = "done"
	MigrationStatusFailed     = "failed"

	ChunkStatePending  = "pending"
	ChunkStateApproved = "approved"
	ChunkStateRejected = "rejected"
	ChunkStateApplied  = "applied"

	FileStatusNew       = "new"
	FileStatusIdentical = "identical"
	FileStatusConflict  = "conflict"
	FileStatusSecret    = "blocked_secret"
	FileStatusIgnored   = "ignored"
)

// WorkspaceMigrationFile describes a single file considered during migration.
type WorkspaceMigrationFile struct {
	SourcePath      string `json:"source_path"`
	TargetPath      string `json:"target_path"`
	Domain          string `json:"domain"`
	Status          string `json:"status"` // new, identical, conflict, blocked_secret
	SourceHash      string `json:"source_hash"`
	TargetHash      string `json:"target_hash,omitempty"`
	SourceSize      int64  `json:"source_size"`
	OriginalContent string `json:"original_content,omitempty"`
	ProposedContent string `json:"proposed_content,omitempty"`
	Rationale       string `json:"rationale,omitempty"`
}

// WorkspaceMigrationChunk groups related files for human review.
type WorkspaceMigrationChunk struct {
	ID          string                   `json:"id"`
	Domain      string                   `json:"domain"`
	Title       string                   `json:"title"`
	State       string                   `json:"state"` // pending, approved, rejected, applied
	Files       []WorkspaceMigrationFile `json:"files"`
	Explanation string                   `json:"explanation,omitempty"`
}

// WorkspaceMigrationPlan represents the complete plan for a migration run.
type WorkspaceMigrationPlan struct {
	Schema           int                       `json:"schema"`
	ID               string                    `json:"id"`
	CreatedAt        string                    `json:"created_at"`
	TargetFolder     string                    `json:"target_folder,omitempty"`
	WorkspaceRoot    string                    `json:"workspace_root"`
	TotalFiles       int                       `json:"total_files"`
	TotalChunks      int                       `json:"total_chunks"`
	TotalNew         int                       `json:"total_new"`
	TotalIdentical   int                       `json:"total_identical"`
	TotalConflicts   int                       `json:"total_conflicts"`
	TotalBlockedSec  int                       `json:"total_blocked_secrets"`
	Chunks           []WorkspaceMigrationChunk `json:"chunks"`
}

// MigrationStatus tracks background worker progress.
type MigrationStatus struct {
	Schema       int                `json:"schema"`
	ID           string             `json:"id"`
	Status       string             `json:"status"` // pending, scanning, processing, done, failed
	Phase        string             `json:"phase"`
	CurrentCount int                `json:"current_count"`
	TotalCount   int                `json:"total_count"`
	Percent      int                `json:"percent"`
	CreatedAt    string             `json:"created_at"`
	UpdatedAt    string             `json:"updated_at"`
	Error        string             `json:"error,omitempty"`
	PlanSummary  *PlanStatusSummary `json:"plan_summary,omitempty"`
}

type PlanStatusSummary struct {
	TotalFiles   int `json:"total_files"`
	TotalChunks  int `json:"total_chunks"`
	Conflicts    int `json:"conflicts"`
	BlockedFiles int `json:"blocked_files"`
}

// MigrationSummary is a lightweight overview of a migration.
type MigrationSummary struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// MigrationRoot returns the directory on host/container where migration data is stored.
func MigrationRoot(config Config, migrationID string) string {
	return filepath.Join(config.DataRoot, ".openlia", "workspace-migrations", migrationID)
}

// Secret inspection patterns
var (
	envFileNamePattern    = regexp.MustCompile(`(?i)(^\.env|\.env\.)`)
	secretFileNamePattern = regexp.MustCompile(`(?i)(id_rsa|id_ed25519|\.pem$|\.key$|\.p12$|\.pfx$)`)
	secretContentPatterns = []*regexp.Regexp{
		regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
		regexp.MustCompile(`\b(?:sk|ghp|gho|ghu|github_pat)_[A-Za-z0-9_\-]{20,}`),
		regexp.MustCompile(`(?i)(?:api[_-]?key|secret[_-]?token|access[_-]?token|private[_-]?key)\s*[:=]\s*["']?[A-Za-z0-9_\-]{16,}["']?`),
	}
)

// IsIgnoredPath returns true if relative path is considered cache or tool noise.
func IsIgnoredPath(relPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == ".obsidian" || part == ".git" || part == "node_modules" ||
			part == "__pycache__" || part == ".venv" || part == ".trash" ||
			part == ".DS_Store" || part == "Thumbs.db" {
			return true
		}
	}
	return false
}

// DetectSecretContent returns true if file content appears to contain confidential credentials.
func DetectSecretContent(filename string, data []byte) bool {
	base := filepath.Base(filename)
	if envFileNamePattern.MatchString(base) || secretFileNamePattern.MatchString(base) {
		return true
	}
	for _, pattern := range secretContentPatterns {
		if pattern.Match(data) {
			return true
		}
	}
	return false
}

// ComputeHash computes SHA-256 of data.
func ComputeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CreateCleanTarball packages a source directory into a tar.gz, omitting ignored files and secrets.
func CreateCleanTarball(sourceDir string, outWriter io.Writer) (int, int, error) {
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return 0, 0, err
	}
	info, err := os.Stat(absSource)
	if err != nil || !info.IsDir() {
		return 0, 0, fmt.Errorf("source path is not a directory: %s", sourceDir)
	}

	gzWriter := gzip.NewWriter(outWriter)
	defer gzWriter.Close()
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	packagedCount := 0
	blockedCount := 0

	err = filepath.Walk(absSource, func(path string, f os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(absSource, path)
		if err != nil || rel == "." {
			return nil
		}
		if IsIgnoredPath(rel) {
			if f.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if f.IsDir() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if DetectSecretContent(rel, data) {
			blockedCount++
			return nil // strictly exclude secret file
		}

		header, err := tar.FileInfoHeader(f, f.Name())
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Size = int64(len(data))
		header.ModTime = f.ModTime()

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(data); err != nil {
			return err
		}
		packagedCount++
		return nil
	})

	if err != nil {
		return packagedCount, blockedCount, err
	}
	return packagedCount, blockedCount, nil
}

// ExtractTarball extracts a tar.gz into targetDir safely (preventing zip slip).
func ExtractTarball(tarGzPath, targetDir string) error {
	file, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	cleanTarget := filepath.Clean(targetDir)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		destPath := filepath.Join(cleanTarget, header.Name)
		if !strings.HasPrefix(filepath.Clean(destPath), cleanTarget+string(filepath.Separator)) {
			return fmt.Errorf("illegal file path in tar archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
				return err
			}
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}
	return nil
}

// IndexWorkspace reads all existing files and SHA-256 hashes in OpenLia workspace.
func IndexWorkspace(workspaceRoot string) (map[string]string, error) {
	index := make(map[string]string)
	if _, err := os.Stat(workspaceRoot); os.IsNotExist(err) {
		return index, nil
	}

	err := filepath.Walk(workspaceRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(workspaceRoot, path)
		if err != nil || rel == "." {
			return nil
		}
		cleanRel := filepath.ToSlash(rel)
		if strings.HasPrefix(cleanRel, ".git/") || cleanRel == ".DS_Store" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		index[cleanRel] = ComputeHash(data)
		return nil
	})
	return index, err
}

// Starter template references in OpenLia Personal OS
var CanonicalDomains = []string{
	"tasks",
	"projects",
	"goals",
	"areas",
	"decisions",
	"monitors",
	"people",
	"ideas",
	"travel",
	"shopping",
	"finance",
	"calendar",
	"knowledge/claims",
	"knowledge",
	"archive",
	"inbox",
}

// ClassifyAndAdaptFile applies deterministic heuristic classification and template alignment.
func ClassifyAndAdaptFile(relPath string, data []byte) (targetPath string, domain string, adaptedContent string, rationale string) {
	cleanPath := filepath.ToSlash(relPath)
	lowerPath := strings.ToLower(cleanPath)
	contentStr := string(data)
	lowerContent := strings.ToLower(contentStr)
	base := filepath.Base(cleanPath)
	title := strings.TrimSuffix(base, filepath.Ext(base))
	title = strings.ReplaceAll(title, "-", " ")
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.Title(title)

	// Protected root files (AGENTS.md, README.md, SOUL.md, IDENTITY.md, USER.md)
	if cleanPath == "AGENTS.md" || cleanPath == "README.md" || cleanPath == "SOUL.md" || cleanPath == "IDENTITY.md" || cleanPath == "USER.md" || cleanPath == "DREAMS.md" {
		targetName := strings.ToLower(strings.TrimSuffix(cleanPath, ".md"))
		return "knowledge/agent/" + targetName + ".md", "knowledge", contentStr, "Archived agent identity metadata under knowledge/agent/"
	}

	// 1. Directory prefix prioritization
	if strings.HasPrefix(lowerPath, "travel/") {
		cleanRel := strings.TrimPrefix(cleanPath, "travel/")
		if !strings.Contains(contentStr, "## Overview") {
			adapted := fmt.Sprintf("# %s\n\n## Overview\n%s\n\n## Packing & Preparation Checklist\n- [ ] Review travel requirements\n", title, contentStr)
			return "travel/" + cleanRel, "travel", adapted, "Adapted to travel/trip-template.md"
		}
		return "travel/" + cleanRel, "travel", contentStr, "Direct travel record"
	}

	if strings.HasPrefix(lowerPath, "learning/") {
		cleanRel := strings.TrimPrefix(cleanPath, "learning/")
		return "knowledge/learning/" + cleanRel, "knowledge", contentStr, "Preserved under knowledge/learning/"
	}

	if strings.HasPrefix(lowerPath, "maintenance/") {
		cleanRel := strings.TrimPrefix(cleanPath, "maintenance/")
		return "knowledge/maintenance/" + cleanRel, "knowledge", contentStr, "Preserved under knowledge/maintenance/"
	}

	if strings.HasPrefix(lowerPath, "memory/") {
		cleanRel := strings.TrimPrefix(cleanPath, "memory/")
		return "archive/memory/" + cleanRel, "archive", contentStr, "Preserved under archive/memory/"
	}

	if strings.HasPrefix(lowerPath, "skills/") {
		cleanRel := strings.TrimPrefix(cleanPath, "skills/")
		return "knowledge/skills/" + cleanRel, "knowledge", contentStr, "Preserved skill reference under knowledge/skills/"
	}

	// 2. Calendar: daily notes or event notes
	if strings.Contains(lowerPath, "calendar") || regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`).MatchString(base) {
		if !strings.Contains(contentStr, "## Details") {
			adapted := fmt.Sprintf("# %s\n\n## Details\n- Date: %s\n\n## Purpose & Agenda\n- Notes from %s\n\n## Discussion & Raw Notes\n%s\n", title, base, cleanPath, contentStr)
			return "calendar/" + base, "calendar", adapted, "Mapped to calendar event note"
		}
		return "calendar/" + base, "calendar", contentStr, "Mapped to calendar note"
	}

	// 3. Ideas: personal ideas or brainstorming
	if strings.Contains(lowerPath, "idea") || strings.Contains(lowerContent, "## concept") || strings.Contains(lowerContent, "hypothesis") {
		if !strings.Contains(contentStr, "## Concept") {
			adapted := fmt.Sprintf("# %s\n\n## Concept\n%s\n\n## Status\nseed\n\n## Potential Value & Opportunity\n- Migrated from %s\n\n## Next Exploration Step\n- [ ] Review concept\n", title, contentStr, cleanPath)
			return "ideas/" + base, "ideas", adapted, "Adapted to ideas/idea-template.md"
		}
		return "ideas/" + base, "ideas", contentStr, "Direct idea note"
	}

	// 4. People / Contacts / Profile
	if strings.Contains(lowerPath, "profile") || strings.Contains(lowerPath, "contact") || strings.Contains(lowerPath, "people") || strings.Contains(lowerContent, "## identity") {
		if !strings.Contains(contentStr, "## Relationship & Context") {
			adapted := fmt.Sprintf("# %s\n\n## Relationship & Context\n%s\n\n## Important Dates\n- Recorded: %s\n\n## Interaction Notes & Context\n- Migrated from %s\n", title, contentStr, time.Now().Format("2006-01-02"), cleanPath)
			return "people/" + base, "people", adapted, "Adapted to people/person-template.md"
		}
		return "people/" + base, "people", contentStr, "Direct people record"
	}

	// 5. Tasks: checklists, action items, to-dos
	if strings.Contains(lowerPath, "task") || strings.Contains(lowerPath, "todo") || strings.Contains(lowerContent, "- [ ]") {
		if !strings.Contains(contentStr, "## Action") {
			adapted := fmt.Sprintf("# %s\n\n## Action\n%s\n\n## Status\ntodo\n\n## Context\n- Priority: medium\n- Migrated from: %s\n", title, contentStr, cleanPath)
			return "tasks/" + base, "tasks", adapted, "Adapted to tasks/task-template.md"
		}
		return "tasks/" + base, "tasks", contentStr, "Direct task record"
	}

	// 6. Learning / Lessons / Knowledge
	if strings.HasPrefix(lowerPath, "learning/") || strings.Contains(lowerPath, "lesson") || strings.Contains(lowerPath, "research") {
		cleanRel := strings.TrimPrefix(cleanPath, "learning/")
		return "knowledge/learning/" + cleanRel, "knowledge", contentStr, "Preserved under knowledge/learning/"
	}

	// 7. Maintenance / Assets / Schedules
	if strings.HasPrefix(lowerPath, "maintenance/") {
		cleanRel := strings.TrimPrefix(cleanPath, "maintenance/")
		return "knowledge/maintenance/" + cleanRel, "knowledge", contentStr, "Preserved under knowledge/maintenance/"
	}

	// 8. Memory / Historical logs
	if strings.HasPrefix(lowerPath, "memory/") {
		cleanRel := strings.TrimPrefix(cleanPath, "memory/")
		return "archive/memory/" + cleanRel, "archive", contentStr, "Preserved under archive/memory/"
	}

	// 9. Skills reference
	if strings.HasPrefix(lowerPath, "skills/") {
		cleanRel := strings.TrimPrefix(cleanPath, "skills/")
		return "knowledge/skills/" + cleanRel, "knowledge", contentStr, "Preserved skill reference under knowledge/skills/"
	}

	// 10. Decisions
	if strings.Contains(lowerPath, "decision") || strings.Contains(lowerContent, "trade-off") || strings.Contains(lowerContent, "options considered") {
		return "decisions/" + base, "decisions", contentStr, "Mapped to decisions domain"
	}

	// Default fallback: preserve path or place in inbox
	if strings.HasSuffix(lowerPath, ".md") || strings.HasSuffix(lowerPath, ".txt") {
		return "inbox/" + base, "inbox", contentStr, "Routed to inbox for initial review"
	}
	return "knowledge/" + base, "knowledge", contentStr, "Filed under knowledge"
}

// BuildMigrationPlan creates the chunks and file mapping plan.
func BuildMigrationPlan(migrationID string, sourceDir string, workspaceRoot string, chunkSize int, useLLM bool, config Config) (*WorkspaceMigrationPlan, error) {
	if chunkSize <= 0 {
		chunkSize = 5
	}
	workspaceIndex, err := IndexWorkspace(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("index workspace: %w", err)
	}

	plan := &WorkspaceMigrationPlan{
		Schema:        1,
		ID:            migrationID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		WorkspaceRoot: workspaceRoot,
	}

	var scannedFiles []WorkspaceMigrationFile

	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil || rel == "." {
			return nil
		}
		if IsIgnoredPath(rel) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		sourceHash := ComputeHash(data)
		isSecret := DetectSecretContent(rel, data)

		if isSecret {
			plan.TotalBlockedSec++
			scannedFiles = append(scannedFiles, WorkspaceMigrationFile{
				SourcePath: filepath.ToSlash(rel),
				Domain:     "quarantine",
				Status:     FileStatusSecret,
				SourceHash: sourceHash,
				SourceSize: int64(len(data)),
				Rationale:  "Detected confidential tokens, private key, or .env file",
			})
			return nil
		}

		targetPath, domain, proposedContent, rationale := ClassifyAndAdaptFile(rel, data)

		status := FileStatusNew
		targetHash := ""
		if existingHash, exists := workspaceIndex[targetPath]; exists {
			targetHash = existingHash
			if existingHash == sourceHash {
				status = FileStatusIdentical
				plan.TotalIdentical++
			} else {
				status = FileStatusConflict
				plan.TotalConflicts++
			}
		} else {
			plan.TotalNew++
		}

		scannedFiles = append(scannedFiles, WorkspaceMigrationFile{
			SourcePath:      filepath.ToSlash(rel),
			TargetPath:      targetPath,
			Domain:          domain,
			Status:          status,
			SourceHash:      sourceHash,
			TargetHash:      targetHash,
			SourceSize:      int64(len(data)),
			OriginalContent: string(data),
			ProposedContent: proposedContent,
			Rationale:       rationale,
		})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk source files: %w", err)
	}
	plan.TotalFiles = len(scannedFiles)

	// Partition files by domain and chunkSize
	domainGroups := make(map[string][]WorkspaceMigrationFile)
	for _, f := range scannedFiles {
		domainGroups[f.Domain] = append(domainGroups[f.Domain], f)
	}

	chunkCounter := 1
	for _, domain := range CanonicalDomains {
		files, exists := domainGroups[domain]
		if !exists || len(files) == 0 {
			continue
		}
		for i := 0; i < len(files); i += chunkSize {
			end := i + chunkSize
			if end > len(files) {
				end = len(files)
			}
			chunkFiles := files[i:end]
			chunkID := fmt.Sprintf("chunk-%d-%s", chunkCounter, strings.ReplaceAll(domain, "/", "-"))
			plan.Chunks = append(plan.Chunks, WorkspaceMigrationChunk{
				ID:          chunkID,
				Domain:      domain,
				Title:       fmt.Sprintf("Migrate %d file(s) into %s/", len(chunkFiles), domain),
				State:       ChunkStatePending,
				Files:       chunkFiles,
				Explanation: fmt.Sprintf("Domain batch %d for %s", (i/chunkSize)+1, domain),
			})
			chunkCounter++
		}
		delete(domainGroups, domain)
	}

	// Any remaining domains (e.g. quarantine or custom)
	for domain, files := range domainGroups {
		for i := 0; i < len(files); i += chunkSize {
			end := i + chunkSize
			if end > len(files) {
				end = len(files)
			}
			chunkFiles := files[i:end]
			chunkID := fmt.Sprintf("chunk-%d-%s", chunkCounter, strings.ReplaceAll(domain, "/", "-"))
			plan.Chunks = append(plan.Chunks, WorkspaceMigrationChunk{
				ID:          chunkID,
				Domain:      domain,
				Title:       fmt.Sprintf("Handle %d file(s) in %s", len(chunkFiles), domain),
				State:       ChunkStatePending,
				Files:       chunkFiles,
				Explanation: "Uncategorized or quarantined files",
			})
			chunkCounter++
		}
	}
	plan.TotalChunks = len(plan.Chunks)
	return plan, nil
}

// StageMigrationChunks writes chunk files into chunks/<chunk-id>/ for interactive review & edits.
func StageMigrationChunks(migrationDir string, plan *WorkspaceMigrationPlan) error {
	chunksDir := filepath.Join(migrationDir, "chunks")
	if err := os.MkdirAll(chunksDir, 0o700); err != nil {
		return err
	}

	for _, chunk := range plan.Chunks {
		chunkDir := filepath.Join(chunksDir, chunk.ID)
		if err := os.MkdirAll(chunkDir, 0o700); err != nil {
			return err
		}
		for _, f := range chunk.Files {
			if f.Status == FileStatusSecret {
				continue
			}
			filePath := filepath.Join(chunkDir, filepath.Base(f.TargetPath))
			content := f.ProposedContent
			if content == "" {
				content = f.OriginalContent
			}
			if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunMigrationWorker unpacks source.tar.gz, runs the planner, writes plan.json and chunks, and updates status.json.
func RunMigrationWorker(ctx context.Context, config Config, migrationID string) error {
	migDir := MigrationRoot(config, migrationID)
	if err := os.MkdirAll(migDir, 0o700); err != nil {
		return err
	}

	writeStatus := func(st string, phase string, cur, tot int, errStr string, summary *PlanStatusSummary) {
		pct := 0
		if tot > 0 {
			pct = (cur * 100) / tot
		}
		status := MigrationStatus{
			Schema:       1,
			ID:           migrationID,
			Status:       st,
			Phase:        phase,
			CurrentCount: cur,
			TotalCount:   tot,
			Percent:      pct,
			CreatedAt:    time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			Error:        errStr,
			PlanSummary:  summary,
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		_ = os.WriteFile(filepath.Join(migDir, "status.json"), data, 0o600)
	}

	writeStatus(MigrationStatusScanning, "unpacking_source", 0, 100, "", nil)

	tarGzPath := filepath.Join(migDir, "source.tar.gz")
	sourceDir := filepath.Join(migDir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		writeStatus(MigrationStatusFailed, "mkdir_source", 0, 100, err.Error(), nil)
		return err
	}

	if err := ExtractTarball(tarGzPath, sourceDir); err != nil {
		writeStatus(MigrationStatusFailed, "extracting_archive", 0, 100, err.Error(), nil)
		return err
	}

	writeStatus(MigrationStatusProcessing, "indexing_and_planning", 50, 100, "", nil)

	workspaceRoot := filepath.Join(config.DataRoot, "workspace")
	plan, err := BuildMigrationPlan(migrationID, sourceDir, workspaceRoot, 5, true, config)
	if err != nil {
		writeStatus(MigrationStatusFailed, "building_plan", 0, 100, err.Error(), nil)
		return err
	}

	if err := StageMigrationChunks(migDir, plan); err != nil {
		writeStatus(MigrationStatusFailed, "staging_chunks", 0, 100, err.Error(), nil)
		return err
	}

	planData, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		writeStatus(MigrationStatusFailed, "saving_plan", 0, 100, err.Error(), nil)
		return err
	}
	if err := os.WriteFile(filepath.Join(migDir, "plan.json"), planData, 0o600); err != nil {
		writeStatus(MigrationStatusFailed, "writing_plan_file", 0, 100, err.Error(), nil)
		return err
	}

	summary := &PlanStatusSummary{
		TotalFiles:   plan.TotalFiles,
		TotalChunks:  plan.TotalChunks,
		Conflicts:    plan.TotalConflicts,
		BlockedFiles: plan.TotalBlockedSec,
	}
	writeStatus(MigrationStatusDone, "completed", 100, 100, "", summary)
	return nil
}

// GetMigrationStatus loads status.json from disk.
func GetMigrationStatus(config Config, migrationID string) (MigrationStatus, error) {
	migDir := MigrationRoot(config, migrationID)
	data, err := os.ReadFile(filepath.Join(migDir, "status.json"))
	if err != nil {
		return MigrationStatus{}, err
	}
	var st MigrationStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return MigrationStatus{}, err
	}
	return st, nil
}

// GetMigrationPlan loads plan.json from disk.
func GetMigrationPlan(config Config, migrationID string) (WorkspaceMigrationPlan, error) {
	migDir := MigrationRoot(config, migrationID)
	data, err := os.ReadFile(filepath.Join(migDir, "plan.json"))
	if err != nil {
		return WorkspaceMigrationPlan{}, err
	}
	var plan WorkspaceMigrationPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return WorkspaceMigrationPlan{}, err
	}
	return plan, nil
}

// ApplyMigrationChunk applies all files from a chunk into the target workspace root.
func ApplyMigrationChunk(config Config, migrationID string, chunkID string, overrides map[string]string) error {
	migDir := MigrationRoot(config, migrationID)
	plan, err := GetMigrationPlan(config, migrationID)
	if err != nil {
		return err
	}

	workspaceRoot := filepath.Join(config.DataRoot, "workspace")
	cleanWorkspace := filepath.Clean(workspaceRoot)

	var targetChunk *WorkspaceMigrationChunk
	for i := range plan.Chunks {
		if plan.Chunks[i].ID == chunkID {
			targetChunk = &plan.Chunks[i]
			break
		}
	}
	if targetChunk == nil {
		return fmt.Errorf("chunk %s not found in migration %s", chunkID, migrationID)
	}

	stagedChunkDir := filepath.Join(migDir, "chunks", chunkID)

	for _, file := range targetChunk.Files {
		if file.Status == FileStatusSecret {
			continue // Never write secrets
		}

		destPath := filepath.Join(cleanWorkspace, file.TargetPath)
		if !strings.HasPrefix(filepath.Clean(destPath), cleanWorkspace+string(filepath.Separator)) {
			return fmt.Errorf("illegal target path outside workspace: %s", file.TargetPath)
		}

		var content []byte
		// 1. Check in-memory overrides
		if overrideContent, has := overrides[file.TargetPath]; has {
			content = []byte(overrideContent)
		} else {
			// 2. Check staged file edited on disk
			stagedPath := filepath.Join(stagedChunkDir, filepath.Base(file.TargetPath))
			if stagedData, err := os.ReadFile(stagedPath); err == nil {
				content = stagedData
			} else if file.ProposedContent != "" {
				content = []byte(file.ProposedContent)
			} else {
				content = []byte(file.OriginalContent)
			}
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
			return fmt.Errorf("mkdir for %s: %w", destPath, err)
		}
		if err := AtomicWriteFile(destPath, content, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
	}

	targetChunk.State = ChunkStateApplied
	planData, err := json.MarshalIndent(plan, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(migDir, "plan.json"), planData, 0o600)
	}
	return nil
}

// ListMigrations returns summaries of all migrations stored on the host.
func ListMigrations(config Config) ([]MigrationSummary, error) {
	baseDir := filepath.Join(config.DataRoot, ".openlia", "workspace-migrations")
	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return []MigrationSummary{}, nil
	}
	if err != nil {
		return nil, err
	}

	var results []MigrationSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		statusPath := filepath.Join(baseDir, entry.Name(), "status.json")
		if data, err := os.ReadFile(statusPath); err == nil {
			var st MigrationStatus
			if err := json.Unmarshal(data, &st); err == nil {
				results = append(results, MigrationSummary{
					ID:        st.ID,
					Status:    st.Status,
					CreatedAt: st.CreatedAt,
					UpdatedAt: st.UpdatedAt,
				})
			}
		}
	}
	return results, nil
}
