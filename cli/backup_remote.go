package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type remoteBackupArchive struct {
	Destination string    `json:"destination"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	Size        int64     `json:"size"`
}

func listRemoteBackupArchives(ctx context.Context, config Config) ([]remoteBackupArchive, error) {
	var archives []remoteBackupArchive
	var failures []string
	for _, destination := range config.BackupDestinations {
		items, err := listRemoteBackupDestination(ctx, config, destination)
		if err != nil {
			failures = append(failures, destination.Name+": "+err.Error())
			continue
		}
		archives = append(archives, items...)
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].CreatedAt.After(archives[j].CreatedAt) })
	if len(failures) > 0 {
		return archives, fmt.Errorf("some backup destinations could not be listed: %s", strings.Join(failures, "; "))
	}
	return archives, nil
}

func listRemoteBackupDestination(ctx context.Context, config Config, destination BackupDestinationConfig) ([]remoteBackupArchive, error) {
	switch destination.Type {
	case "s3":
		return listS3BackupArchives(ctx, config, destination)
	case "rsync":
		return listRsyncBackupArchives(ctx, config, destination)
	default:
		return nil, fmt.Errorf("unsupported backup destination type %q", destination.Type)
	}
}

func listS3BackupArchives(ctx context.Context, config Config, destination BackupDestinationConfig) ([]remoteBackupArchive, error) {
	awsConfig, err := loadS3AWSConfig(ctx, destination.Region, destination.ReaderCredentialsFile, destination.ReaderAWSProfile, "reader")
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.UsePathStyle = destination.PathStyle
		if destination.Endpoint != "" {
			options.BaseEndpoint = aws.String(destination.Endpoint)
		}
	})
	prefix := backupObjectPrefix(config, destination)
	var archives []remoteBackupArchive
	var continuation *string
	for {
		page, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(destination.Bucket), Prefix: aws.String(prefix), ContinuationToken: continuation})
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			if object.Key == nil || !strings.HasSuffix(*object.Key, ".tar.gz.age") {
				continue
			}
			created := time.Time{}
			if object.LastModified != nil {
				created = *object.LastModified
			}
			size := int64(0)
			if object.Size != nil {
				size = *object.Size
			}
			archives = append(archives, remoteBackupArchive{Destination: destination.Name, Name: filepath.Base(*object.Key), CreatedAt: created, Size: size})
		}
		if !aws.ToBool(page.IsTruncated) || page.NextContinuationToken == nil {
			break
		}
		continuation = page.NextContinuationToken
	}
	return archives, nil
}

func listRsyncBackupArchives(ctx context.Context, config Config, destination BackupDestinationConfig) ([]remoteBackupArchive, error) {
	host, directory, err := splitCLIRsyncTarget(destination.RsyncTarget)
	if err != nil {
		return nil, err
	}
	directory = filepath.Join(directory, config.Project+"-"+backupNamespace(config))
	command := "find " + shellQuote(directory) + " -type f -name 'openlia-*.tar.gz.age' -exec basename {} \\; | sort -r"
	args := rsyncSSHArgs(destination)
	args = append(args, "--", host, command)
	output, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return nil, err
	}
	var archives []remoteBackupArchive
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasPrefix(name, "openlia-") || !strings.HasSuffix(name, ".tar.gz.age") {
			continue
		}
		created := backupNameTime(name)
		archives = append(archives, remoteBackupArchive{Destination: destination.Name, Name: name, CreatedAt: created})
	}
	return archives, nil
}

func fetchRemoteBackup(ctx context.Context, config Config, destination BackupDestinationConfig, name, output string) error {
	if filepath.Base(name) != name || !strings.HasPrefix(name, "openlia-") || !strings.HasSuffix(name, ".tar.gz.age") {
		return fmt.Errorf("remote backup name is invalid")
	}
	switch destination.Type {
	case "s3":
		return fetchS3Backup(ctx, config, destination, name, output)
	case "rsync":
		return fetchRsyncBackup(ctx, config, destination, name, output)
	default:
		return fmt.Errorf("unsupported backup destination type %q", destination.Type)
	}
}

func fetchS3Backup(ctx context.Context, config Config, destination BackupDestinationConfig, name, output string) error {
	awsConfig, err := loadS3AWSConfig(ctx, destination.Region, destination.ReaderCredentialsFile, destination.ReaderAWSProfile, "reader")
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.UsePathStyle = destination.PathStyle
		if destination.Endpoint != "" {
			options.BaseEndpoint = aws.String(destination.Endpoint)
		}
	})
	key := backupObjectPrefix(config, destination) + name
	response, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(destination.Bucket), Key: aws.String(key)})
	if err != nil {
		return err
	}
	if err := writeRemoteFile(output, response.Body); err != nil {
		_ = response.Body.Close()
		return err
	}
	if err := response.Body.Close(); err != nil {
		return err
	}
	metadata, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(destination.Bucket), Key: aws.String(key + ".json")})
	if err != nil {
		return fmt.Errorf("fetch archive metadata: %w", err)
	}
	metadataErr := writeRemoteFile(output+".json", metadata.Body)
	closeErr := metadata.Body.Close()
	if metadataErr != nil {
		return metadataErr
	}
	return closeErr
}

func fetchRsyncBackup(ctx context.Context, config Config, destination BackupDestinationConfig, name, output string) error {
	host, directory, err := splitCLIRsyncTarget(destination.RsyncTarget)
	if err != nil {
		return err
	}
	directory = filepath.Join(directory, config.Project+"-"+backupNamespace(config))
	source := host + ":" + strings.TrimSuffix(directory, "/") + "/" + name
	if err := rsyncDownload(ctx, destination, source, output); err != nil {
		return err
	}
	metadataSource := host + ":" + strings.TrimSuffix(directory, "/") + "/" + name + ".json"
	if err := rsyncDownload(ctx, destination, metadataSource, output+".json"); err != nil {
		return fmt.Errorf("download archive metadata: %w", err)
	}
	return nil
}

func rsyncDownload(ctx context.Context, destination BackupDestinationConfig, source, output string) error {
	if _, err := exec.LookPath("rsync"); err != nil {
		return fmt.Errorf("rsync is required on the operator machine: %w", err)
	}
	sshArgs := []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"}
	identity := destination.OperatorIdentityFile
	if identity != "" {
		sshArgs = append(sshArgs, "-i", identity)
	}
	quoted := make([]string, len(sshArgs))
	for index, arg := range sshArgs {
		quoted[index] = shellQuote(arg)
	}
	cmd := exec.CommandContext(ctx, "rsync", "--archive", "--protect-args", "-e", strings.Join(quoted, " "), "--", source, output)
	result, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync download failed: %s", strings.TrimSpace(string(result)))
	}
	return nil
}

func writeRemoteFile(path string, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, reader)
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	return nil
}

func backupObjectPrefix(config Config, destination BackupDestinationConfig) string {
	prefix := strings.Trim(destination.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return prefix + config.Project + "-" + backupNamespace(config) + "/"
}

func backupNamespace(config Config) string {
	digest := sha256.Sum256([]byte(config.Project + "\x00" + config.InstallRoot))
	return hex.EncodeToString(digest[:])[:16]
}

func backupNameTime(name string) time.Time {
	if len(name) < len("openlia-20060102T150405Z") {
		return time.Time{}
	}
	stamp := strings.TrimPrefix(name, "openlia-")
	stamp = stamp[:len("20060102T150405Z")]
	parsed, err := time.Parse("20060102T150405Z", stamp)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func splitCLIRsyncTarget(target string) (string, string, error) {
	separator := strings.IndexByte(target, ':')
	if separator <= 0 || separator == len(target)-1 {
		return "", "", fmt.Errorf("rsync target must use user@host:/absolute/path syntax")
	}
	return target[:separator], target[separator+1:], nil
}

func rsyncSSHArgs(destination BackupDestinationConfig) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"}
	identity := destination.OperatorIdentityFile
	if identity != "" {
		args = append(args, "-i", identity)
	}
	return args
}
