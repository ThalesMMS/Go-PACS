package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
)

const rebuildMetadataWarning = "study_metadata (status, comments, reports) cannot be recovered from DICOM objects"

type RebuildReport struct {
	ScannedFiles       int64       `json:"scannedFiles"`
	StoredFiles        int64       `json:"storedFiles"`
	SkippedFiles       int64       `json:"skippedFiles"`
	FailedFiles        int64       `json:"failedFiles"`
	Rejections         []Rejection `json:"rejections,omitempty"`
	Warnings           []string    `json:"warnings,omitempty"`
	CatalogBackupPath  string      `json:"catalogBackupPath,omitempty"`
	VerificationPassed bool        `json:"verificationPassed,omitempty"`
}

type RebuildOptions struct {
	VerifyFunc func(context.Context, *Catalog) error
}

func RebuildCatalog(ctx context.Context, rootDir string, opts RebuildOptions) (RebuildReport, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return RebuildReport{}, errors.New("archive root directory is required")
	}
	rootInfo, err := os.Stat(rootDir)
	if err != nil {
		return RebuildReport{}, fmt.Errorf("stat archive root: %w", err)
	}
	if !rootInfo.IsDir() {
		return RebuildReport{}, fmt.Errorf("archive root is not a directory: %s", rootDir)
	}
	objectsDir := filepath.Join(rootDir, "objects")
	objectsInfo, err := os.Stat(objectsDir)
	if err != nil {
		return RebuildReport{}, fmt.Errorf("stat object store: %w", err)
	}
	if !objectsInfo.IsDir() {
		return RebuildReport{}, fmt.Errorf("object store is not a directory: %s", objectsDir)
	}

	report := RebuildReport{Warnings: []string{rebuildMetadataWarning}}
	backupPath, err := moveCatalogDBAside(rootDir)
	if err != nil {
		return report, err
	}
	report.CatalogBackupPath = backupPath

	catalog, err := Open(rootDir)
	if err != nil {
		return report, err
	}
	defer catalog.Close()

	err = filepath.WalkDir(objectsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeType != 0 {
			report.SkippedFiles++
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".dcm") {
			report.SkippedFiles++
			return nil
		}
		report.ScannedFiles++
		if err := rebuildObject(ctx, catalog, path); err != nil {
			report.FailedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: path, Reason: err.Error()})
			return nil
		}
		report.StoredFiles++
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("walk object store: %w", err)
	}
	if opts.VerifyFunc != nil {
		if err := opts.VerifyFunc(ctx, catalog); err != nil {
			return report, err
		}
		report.VerificationPassed = true
	}
	return report, nil
}

func rebuildObject(ctx context.Context, catalog *Catalog, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat object: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("object is not a regular file")
	}
	digest := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if digest == "" {
		return fmt.Errorf("object filename does not contain a SHA-256 digest")
	}
	summary, err := dicominspect.InspectFile(path, dicominspect.DefaultOptions())
	if err != nil {
		return err
	}
	instance := instanceFromSummary(summary, digest, path, "rebuild:"+path, info.Size(), time.Now().UTC())
	if err := catalog.upsertInstance(ctx, instance); err != nil {
		return err
	}
	return nil
}

func moveCatalogDBAside(rootDir string) (string, error) {
	catalogPath := filepath.Join(rootDir, "catalog.db")
	if _, err := os.Stat(catalogPath); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("stat existing catalog: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	for i := 0; i < 1000; i++ {
		suffix := stamp
		if i > 0 {
			suffix = fmt.Sprintf("%s.%d", stamp, i)
		}
		backupPath := filepath.Join(rootDir, "catalog.db.backup."+suffix)
		if _, err := os.Stat(backupPath); err == nil {
			continue
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat catalog backup path: %w", err)
		}
		if err := os.Rename(catalogPath, backupPath); err != nil {
			return "", fmt.Errorf("move existing catalog aside: %w", err)
		}
		return backupPath, nil
	}
	return "", errors.New("could not allocate catalog backup filename")
}
