package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

const (
	backupManifestFileName = "manifest.json"
	backupManifestVersion  = 1
)

type BackupManifest struct {
	Version              int      `json:"version"`
	Timestamp            string   `json:"timestamp"`
	SourceArchivePath    string   `json:"sourceArchivePath"`
	CatalogFile          string   `json:"catalogFile"`
	ObjectsDir           string   `json:"objectsDir"`
	SidecarFiles         []string `json:"sidecarFiles"`
	CatalogSchemaVersion int      `json:"catalogSchemaVersion"`
	ObjectCount          int64    `json:"objectCount"`
	ObjectBytes          int64    `json:"objectBytes"`
	TotalBytes           int64    `json:"totalBytes"`
}

type BackupResult struct {
	DestinationDir string         `json:"destinationDir"`
	Manifest       BackupManifest `json:"manifest"`
	Verification   *VerifyResult  `json:"verification"`
}

func (s *Session) BackupArchive(ctx context.Context, destDir string) (*BackupResult, error) {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return nil, errors.New("backup destination directory is required")
	}
	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return nil, fmt.Errorf("resolve backup destination: %w", err)
	}
	sourceAbs, err := filepath.Abs(s.archiveDir)
	if err != nil {
		return nil, fmt.Errorf("resolve source archive path: %w", err)
	}
	if err := validateBackupDestination(destAbs); err != nil {
		return nil, err
	}

	verification, err := s.VerifyArchive(ctx)
	if err != nil {
		return nil, err
	}
	if !verification.OK {
		return &BackupResult{DestinationDir: destAbs, Verification: verification}, errors.New("archive verification failed")
	}

	stats, err := s.catalog.ArchiveStats(ctx)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return nil, fmt.Errorf("create backup destination: %w", err)
	}

	var totalBytes int64
	catalogDest := filepath.Join(destAbs, "catalog.db")
	if err := s.catalog.BackupCatalogTo(ctx, catalogDest); err != nil {
		return nil, err
	}
	catalogInfo, err := os.Stat(catalogDest)
	if err != nil {
		return nil, fmt.Errorf("stat backup catalog: %w", err)
	}
	totalBytes += catalogInfo.Size()

	objectBytes, err := copyObjectsDir(ctx, s.catalog.ObjectDir(), filepath.Join(destAbs, "objects"))
	if err != nil {
		return nil, err
	}
	totalBytes += objectBytes

	var sidecars []string
	for _, name := range archiveSidecarFileNames() {
		copied, exists, err := copyOptionalFileAtomic(ctx, filepath.Join(s.archiveDir, name), filepath.Join(destAbs, name))
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		sidecars = append(sidecars, name)
		totalBytes += copied
	}

	manifest := BackupManifest{
		Version:              backupManifestVersion,
		Timestamp:            time.Now().UTC().Format(time.RFC3339Nano),
		SourceArchivePath:    sourceAbs,
		CatalogFile:          "catalog.db",
		ObjectsDir:           "objects",
		SidecarFiles:         sidecars,
		CatalogSchemaVersion: archive.CatalogSchemaVersion,
		ObjectCount:          stats.InstanceCount,
		ObjectBytes:          stats.TotalBytes,
		TotalBytes:           totalBytes,
	}
	if err := writeJSONAtomic(filepath.Join(destAbs, backupManifestFileName), manifest); err != nil {
		return nil, err
	}
	return &BackupResult{
		DestinationDir: destAbs,
		Manifest:       manifest,
		Verification:   verification,
	}, nil
}

// validateBackupDestination validates that destDir is suitable for a backup operation. It returns nil if destDir does not exist or is an empty directory, otherwise an error.
func validateBackupDestination(destDir string) error {
	info, err := os.Stat(destDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat backup destination: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup destination is not a directory: %s", destDir)
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return fmt.Errorf("read backup destination: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("backup destination must be empty: %s", destDir)
	}
	return nil
}

// CopyObjectsDir recursively copies the directory tree rooted at srcRoot to dstRoot, validating that all entries are regular files, and returns the total bytes copied.
func copyObjectsDir(ctx context.Context, srcRoot string, dstRoot string) (int64, error) {
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return 0, fmt.Errorf("create backup objects directory: %w", err)
	}
	var total int64
	err := filepath.WalkDir(srcRoot, func(srcPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, srcPath)
		if err != nil {
			return fmt.Errorf("resolve object path: %w", err)
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dstRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		if entry.Type()&fs.ModeType != 0 {
			return fmt.Errorf("unsupported object store entry: %s", srcPath)
		}
		copied, err := copyFileAtomic(ctx, srcPath, dstPath)
		if err != nil {
			return err
		}
		total += copied
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("copy backup objects: %w", err)
	}
	return total, nil
}

// archiveSidecarFileNames returns the sidecar filenames to be backed up with an archive.
func archiveSidecarFileNames() []string {
	return []string{configFileName, nodesFileName, autoQueryProfilesFileName, historyFileName}
}

// copyOptionalFileAtomic copies srcPath to dstPath, returning the bytes copied and whether the source file existed. If the source does not exist, no error is returned; otherwise, any copy error is propagated.
func copyOptionalFileAtomic(ctx context.Context, srcPath string, dstPath string) (int64, bool, error) {
	copied, err := copyFileAtomic(ctx, srcPath, dstPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	return copied, true, err
}

// copyFileAtomic atomically copies srcPath to dstPath, preserving the source file's permissions and respecting context cancellation. It returns the number of bytes written and any error encountered.
func copyFileAtomic(ctx context.Context, srcPath string, dstPath string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("source is not a regular file: %s", srcPath)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return 0, fmt.Errorf("create backup file directory: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return 0, fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("create temporary backup file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	written, copyErr := io.Copy(tmp, src)
	closeErr := tmp.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("copy backup file: %w", copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close backup file: %w", closeErr)
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		return 0, fmt.Errorf("set backup file mode: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return 0, fmt.Errorf("replace backup file: %w", err)
	}
	removeTemp = false
	return written, nil
}

// writeJSONAtomic atomically writes v as indented JSON to path.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
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
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close backup manifest: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace backup manifest: %w", err)
	}
	removeTemp = false
	return nil
}
