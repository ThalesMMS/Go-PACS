package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type LoadError struct {
	Err          error
	BackupExists bool
}

func (e *LoadError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	if e.BackupExists {
		return e.Err.Error() + " (backup available)"
	}
	return e.Err.Error()
}

func (e *LoadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func BackupPath(path string) string {
	return path + ".bak"
}

func CheckBackupExists(path string) bool {
	info, err := os.Stat(BackupPath(path))
	return err == nil && info.Mode().IsRegular()
}

func WriteWithBackup(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create JSON store directory: %w", err)
	}
	if current, err := os.ReadFile(path); err == nil && json.Valid(current) {
		if err := writeAtomic(BackupPath(path), current, 0o644); err != nil {
			return fmt.Errorf("write JSON store backup: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read JSON store before backup: %w", err)
	}
	if err := writeAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write JSON store: %w", err)
	}
	return nil
}

func RecoverFromBackup(path string) error {
	backup := BackupPath(path)
	info, err := os.Stat(backup)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("JSON store backup does not exist: %s", backup)
	}
	if err != nil {
		return fmt.Errorf("stat JSON store backup: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("JSON store backup is not a regular file: %s", backup)
	}
	src, err := os.Open(backup)
	if err != nil {
		return fmt.Errorf("open JSON store backup: %w", err)
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("read JSON store backup: %w", err)
	}
	if err := writeAtomic(path, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("recover JSON store backup: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}
