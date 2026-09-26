package operator

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// EnsureDir creates a directory and applies mode to the directory itself.
func EnsureDir(path string, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set directory mode %s: %w", path, err)
	}
	return nil
}

func ensureRuntimeOwner(path string, uid, gid int, mode fs.FileMode) error {
	if os.Geteuid() == 0 {
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("set owner %s: %w", path, err)
		}
	} else if uid != os.Getuid() || gid != os.Getgid() {
		return fmt.Errorf("runtime identity %d:%d requires root, current operator is %d:%d", uid, gid, os.Getuid(), os.Getgid())
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set mode %s: %w", path, err)
	}
	return nil
}

func ensureRuntimeTreeOwner(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if err := os.Chown(path, uid, gid); err != nil {
			if os.Geteuid() != 0 && uid == os.Getuid() && gid == os.Getgid() {
				return nil
			}
			return fmt.Errorf("set owner %s: %w", path, err)
		}
		return nil
	})
}

// AtomicWriteFile replaces destination only after the complete contents have
// been written and synced. The parent directory must already exist, matching
// the failure boundary of the shell atomic write helper.
func AtomicWriteFile(destination string, contents []byte, mode fs.FileMode) error {
	parent := filepath.Dir(destination)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return fmt.Errorf("atomic replacement parent does not exist: %w", err)
	}
	temporary, err := os.CreateTemp(parent, filepath.Base(destination)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create atomic temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set atomic temporary mode: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write atomic temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync atomic temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close atomic temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("activate atomic file: %w", err)
	}
	removeTemporary = false
	return nil
}

// AtomicWrite is a concise alias used by filesystem-oriented callers.
func AtomicWrite(destination string, contents []byte, mode fs.FileMode) error {
	return AtomicWriteFile(destination, contents, mode)
}

// AtomicCopyFile copies a regular file through the same atomic replacement
// path as AtomicWriteFile.
func AtomicCopyFile(source, destination string, mode fs.FileMode) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()
	contents, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	return AtomicWriteFile(destination, contents, mode)
}

// CopyDir copies a directory without following symlinks. It is used for an
// initially absent skill; AtomicCopyDir handles replacement of an existing one.
func CopyDir(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("stat source directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source directory does not exist")
	}
	if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	return copyDirContents(source, destination)
}

// AtomicCopyDir replaces an existing directory through a sibling temporary
// directory. User data is not modified until the new tree is complete.
func AtomicCopyDir(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source directory does not exist")
	}
	destinationInfo, err := os.Lstat(destination)
	if err != nil || !destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("destination directory does not exist")
	}
	parent := filepath.Dir(destination)
	temporary, err := os.MkdirTemp(parent, filepath.Base(destination)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create directory replacement: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := copyDirContents(source, temporary); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(parent, filepath.Base(destination)+".backup-*")
	if err != nil {
		return fmt.Errorf("create directory backup: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		_ = os.RemoveAll(temporary)
		return fmt.Errorf("prepare directory backup: %w", err)
	}
	if err := os.Rename(destination, backup); err != nil {
		return fmt.Errorf("move directory aside: %w", err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Rename(backup, destination)
		return fmt.Errorf("activate directory replacement: %w", err)
	}
	removeTemporary = false
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove replaced directory: %w", err)
	}
	return nil
}

func copyDirContents(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err == nil {
			_, err = io.Copy(output, input)
		}
		if closeErr := input.Close(); err == nil {
			err = closeErr
		}
		if output != nil {
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
		return err
	})
}
