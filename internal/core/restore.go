package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidBackupManifest   = errors.New("invalid backup manifest")
	ErrMissingBackupEntry      = errors.New("missing backup entry")
	ErrOverwriteCurrentArchive = errors.New("restore destination is the current archive")
	ErrRestoreVerification     = errors.New("restored archive verification failed")
)

type RestoreResult struct {
	DestDir            string        `json:"destDir"`
	CatalogRestored    bool          `json:"catalogRestored"`
	ObjectsRestored    int           `json:"objectsRestored"`
	StoredPathsRebased int64         `json:"storedPathsRebased"`
	SidecarsRestored   []string      `json:"sidecarsRestored"`
	VerificationPassed bool          `json:"verificationPassed"`
	Verification       *VerifyResult `json:"verification,omitempty"`
	OpenArchiveHint    string        `json:"openArchiveHint"`
}

type backupLayout struct {
	dir         string
	catalogFile string
	objectsDir  string
	sidecars    []string
	objectCount int
}

func (s *Session) RestoreBackup(ctx context.Context, backupDir string, destDir string, allowOverwriteCurrent bool) (RestoreResult, error) {
	backupDir = strings.TrimSpace(backupDir)
	destDir = strings.TrimSpace(destDir)
	if backupDir == "" {
		return RestoreResult{}, errors.New("backup directory is required")
	}
	if destDir == "" {
		return RestoreResult{}, errors.New("restore destination directory is required")
	}
	backupAbs, err := filepath.Abs(backupDir)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve backup path: %w", err)
	}
	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve restore destination: %w", err)
	}
	layout, err := validateBackupLayout(backupAbs)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := validateRestoreDestination(destAbs, s.archiveDir, allowOverwriteCurrent); err != nil {
		return RestoreResult{}, err
	}

	result, err := copyBackupToDestination(ctx, layout, destAbs)
	if err != nil {
		return result, err
	}

	restored, err := Open(destAbs)
	if err != nil {
		return result, fmt.Errorf("open restored archive: %w", err)
	}
	defer restored.Close()
	rebased, err := restored.Catalog().RebaseStoredPaths(ctx)
	if err != nil {
		return result, fmt.Errorf("rebase restored object paths: %w", err)
	}
	result.StoredPathsRebased = rebased
	verification, err := restored.VerifyArchive(ctx)
	if err != nil {
		return result, err
	}
	result.Verification = verification
	result.VerificationPassed = verification.OK
	if !verification.OK {
		return result, fmt.Errorf("%w: %d verification errors", ErrRestoreVerification, len(verification.Errors))
	}
	result.OpenArchiveHint = fmt.Sprintf("Open the restored archive with -archive-dir %s", destAbs)
	return result, nil
}

// validateBackupLayout reads the backup manifest from backupDir, validates the
// manifest fields, verifies all required files exist with correct object counts,
// discovers sidecar files, and returns the validated backup layout.
func validateBackupLayout(backupDir string) (backupLayout, error) {
	data, err := os.ReadFile(filepath.Join(backupDir, backupManifestFileName))
	if err != nil {
		return backupLayout{}, fmt.Errorf("%w: read %s: %v", ErrInvalidBackupManifest, backupManifestFileName, err)
	}
	var manifest BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return backupLayout{}, fmt.Errorf("%w: parse %s: %v", ErrInvalidBackupManifest, backupManifestFileName, err)
	}
	if manifest.Version != 0 && manifest.Version != backupManifestVersion {
		return backupLayout{}, fmt.Errorf("%w: unsupported version %d", ErrInvalidBackupManifest, manifest.Version)
	}
	if strings.TrimSpace(manifest.Timestamp) == "" {
		return backupLayout{}, fmt.Errorf("%w: timestamp is required", ErrInvalidBackupManifest)
	}
	if strings.TrimSpace(manifest.SourceArchivePath) == "" {
		return backupLayout{}, fmt.Errorf("%w: sourceArchivePath is required", ErrInvalidBackupManifest)
	}
	if manifest.ObjectCount < 0 {
		return backupLayout{}, fmt.Errorf("%w: objectCount cannot be negative", ErrInvalidBackupManifest)
	}

	catalogFile := manifest.CatalogFile
	if catalogFile == "" {
		catalogFile = "catalog.db"
	}
	objectsDir := manifest.ObjectsDir
	if objectsDir == "" {
		objectsDir = "objects"
	}
	catalogFile, err = cleanBackupEntry(catalogFile)
	if err != nil {
		return backupLayout{}, fmt.Errorf("%w: invalid catalogFile: %v", ErrInvalidBackupManifest, err)
	}
	objectsDir, err = cleanBackupEntry(objectsDir)
	if err != nil {
		return backupLayout{}, fmt.Errorf("%w: invalid objectsDir: %v", ErrInvalidBackupManifest, err)
	}
	if err := requireRegularFile(filepath.Join(backupDir, catalogFile)); err != nil {
		return backupLayout{}, fmt.Errorf("%w: %s: %v", ErrMissingBackupEntry, catalogFile, err)
	}
	objectCount, err := countRegularFiles(filepath.Join(backupDir, objectsDir))
	if err != nil {
		return backupLayout{}, fmt.Errorf("%w: %s: %v", ErrMissingBackupEntry, objectsDir, err)
	}
	if int64(objectCount) != manifest.ObjectCount {
		return backupLayout{}, fmt.Errorf("%w: objects count = %d, manifest objectCount = %d", ErrMissingBackupEntry, objectCount, manifest.ObjectCount)
	}

	sidecars, err := backupSidecars(backupDir, manifest.SidecarFiles)
	if err != nil {
		return backupLayout{}, err
	}
	return backupLayout{
		dir:         backupDir,
		catalogFile: catalogFile,
		objectsDir:  objectsDir,
		sidecars:    sidecars,
		objectCount: objectCount,
	}, nil
}

// backupSidecars returns unique sidecar filenames from the backup directory. When no manifest sidecars are provided, it discovers which known sidecar files exist. When manifest sidecars are provided, it validates each one.
func backupSidecars(backupDir string, manifestSidecars []string) ([]string, error) {
	if len(manifestSidecars) == 0 {
		var sidecars []string
		for _, name := range archiveSidecarFileNames() {
			if _, err := os.Stat(filepath.Join(backupDir, name)); err == nil {
				sidecars = append(sidecars, name)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: %s: %v", ErrMissingBackupEntry, name, err)
			}
		}
		return sidecars, nil
	}

	seen := map[string]bool{}
	sidecars := make([]string, 0, len(manifestSidecars))
	for _, name := range manifestSidecars {
		cleaned, err := cleanBackupEntry(name)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid sidecar file: %v", ErrInvalidBackupManifest, err)
		}
		if seen[cleaned] {
			continue
		}
		if err := requireRegularFile(filepath.Join(backupDir, cleaned)); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrMissingBackupEntry, cleaned, err)
		}
		seen[cleaned] = true
		sidecars = append(sidecars, cleaned)
	}
	return sidecars, nil
}

// validateRestoreDestination validates that destDir is safe for restoration and is not the current archive unless explicitly allowed.
func validateRestoreDestination(destDir string, currentArchiveDir string, allowOverwriteCurrent bool) error {
	currentAbs, err := filepath.Abs(currentArchiveDir)
	if err != nil {
		return fmt.Errorf("resolve current archive path: %w", err)
	}
	if filepath.Clean(destDir) == filepath.Clean(currentAbs) && !allowOverwriteCurrent {
		return fmt.Errorf("%w: %s", ErrOverwriteCurrentArchive, destDir)
	}
	return validateBackupDestination(destDir)
}

// copyBackupToDestination copies the backup files to the destination directory,
// tracking what was successfully restored. It copies the catalog, objects
// directory, and sidecar files. If copying fails, it returns the partial
// result along with the error.
func copyBackupToDestination(ctx context.Context, layout backupLayout, destDir string) (RestoreResult, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return RestoreResult{}, fmt.Errorf("create restore destination: %w", err)
	}
	result := RestoreResult{DestDir: destDir}
	if _, err := copyFileAtomic(ctx, filepath.Join(layout.dir, layout.catalogFile), filepath.Join(destDir, "catalog.db")); err != nil {
		return result, fmt.Errorf("restore catalog: %w", err)
	}
	result.CatalogRestored = true
	if _, err := copyObjectsDir(ctx, filepath.Join(layout.dir, layout.objectsDir), filepath.Join(destDir, "objects")); err != nil {
		return result, err
	}
	result.ObjectsRestored = layout.objectCount
	for _, name := range layout.sidecars {
		if _, err := copyFileAtomic(ctx, filepath.Join(layout.dir, name), filepath.Join(destDir, name)); err != nil {
			return result, fmt.Errorf("restore sidecar %s: %w", name, err)
		}
		result.SidecarsRestored = append(result.SidecarsRestored, name)
	}
	return result, nil
}

// cleanBackupEntry sanitizes a relative path name for use as a backup entry, rejecting absolute paths, parent directory references, nested paths, and empty names. It returns the sanitized name or an error if validation fails.
func cleanBackupEntry(name string) (string, error) {
	name = filepath.Clean(strings.TrimSpace(name))
	if name == "" || name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe relative path %q", name)
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("nested paths are not supported: %q", name)
	}
	return name, nil
}

// RequireRegularFile returns an error if path is not a regular file.
func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	return nil
}

// countRegularFiles returns the number of regular files in the specified directory.
func countRegularFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		if info.Mode().IsRegular() {
			count++
		}
	}
	return count, nil
}
