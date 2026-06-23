package archive

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/pixeldata"
	"github.com/ThalesMMS/dicom-go/pixeldata/jpeg"
	"github.com/ThalesMMS/dicom-go/pixeldata/jpeglossless"
	"github.com/ThalesMMS/dicom-go/pixeldata/rle"
	"github.com/ThalesMMS/dicom-go/transfer"
)

type Catalog struct {
	db       *sql.DB
	rootDir  string
	storeDir string
	trashDir string
}

const CatalogSchemaVersion = 1

const missingUIDSentinel = "(missing)"

type SchemaMigration struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

type ImportReport struct {
	ScannedFiles int
	StoredFiles  int
	Duplicates   int
	InvalidFiles int
	Rejections   []Rejection
}

type ArchiveStats struct {
	InstanceCount int64
	TotalBytes    int64
}

type ImportProgress struct {
	ScannedFiles int
	StoredFiles  int
	Duplicates   int
	InvalidFiles int
	Path         string
}

type ImportOptions struct {
	Limits           ImportLimits
	DecompressImages bool
	OnProgress       func(ImportProgress)
}

type ImportLimits struct {
	MaxFileImportBytes      int64
	MaxZipEntryBytes        int64
	MaxZipTotalBytes        int64
	MaxZipEntryCount        int
	MaxImportTotalFiles     int
	MaxImportPathLength     int
	MaxImportDirectoryDepth int
}

type DecompressReport struct {
	ScannedFiles      int
	DecompressedFiles int
	SkippedFiles      int
	FailedFiles       int
	Rejections        []Rejection
}

type Rejection struct {
	Path   string
	Reason string
}

type Instance struct {
	SHA256            string
	StoredPath        string
	SourcePath        string
	FileSize          int64
	PatientName       string
	PatientID         string
	PatientBirthDate  string
	InstitutionName   string
	StudyDate         string
	StudyTime         string
	SeriesDate        string
	SeriesTime        string
	StudyDescription  string
	Modality          string
	AccessionNumber   string
	StudyInstanceUID  string
	SeriesInstanceUID string
	SeriesNumber      string
	SeriesDescription string
	SOPClassUID       string
	SOPInstanceUID    string
	InstanceNumber    string
	TransferSyntaxUID string
	TransferSyntax    string

	StudyID                 string
	BodyPartExamined        string
	ReferringPhysicianName  string
	PerformingPhysicianName string

	ImportedAt time.Time
}

type Study struct {
	StudyInstanceUID string
	PatientName      string
	PatientID        string
	PatientBirthDate string
	InstitutionName  string
	StudyDate        string
	StudyTime        string
	StudyDescription string
	Modalities       string
	AccessionNumber  string
	Status           string
	Comments         string

	StudyID                 string
	BodyPartExamined        string
	ReferringPhysicianName  string
	PerformingPhysicianName string

	SeriesCount   int
	InstanceCount int
	ImportedAt    time.Time
}

type StudyPageOptions struct {
	Limit  int
	Offset int
}

type StudyPage struct {
	Items  []Study `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

type StudyMetadata struct {
	Status   string
	Comments string
	Report   string
}

type Series struct {
	StudyInstanceUID  string
	SeriesInstanceUID string
	Modality          string
	SeriesNumber      string
	SeriesDescription string
	SeriesDate        string
	SeriesTime        string
	InstanceCount     int
	ImportedAt        time.Time
}

type StudyFilters struct {
	PatientName        string
	PatientNameSoundex bool
	PatientID          string
	AccessionNumber    string
	StudyDescription   string
	StudyDateFrom      string
	StudyDateTo        string
	StudyDateTimeFrom  string
	StudyDateTimeTo    string
	ImportedAtFrom     string
	ImportedAtTo       string
	Modalities         []string
	SourcePath         string
	Status             string
	HasComments        bool

	AllFields           string
	PatientBirthDate    string
	StudyID             string
	StudyInstanceUID    string
	Comments            string
	BodyPart            string
	Modality            string
	ReferringPhysician  string
	PerformingPhysician string
}

type SeriesFilters struct {
	Modality          string
	SeriesNumber      string
	SeriesDescription string
}

var (
	tagMediaStorageSOPClassUID    = core.NewTag(0x0002, 0x0002)
	tagMediaStorageSOPInstanceUID = core.NewTag(0x0002, 0x0003)
	tagTransferSyntaxUID          = core.NewTag(0x0002, 0x0010)
	tagSOPClassUID                = core.NewTag(0x0008, 0x0016)
	tagSOPInstanceUID             = core.NewTag(0x0008, 0x0018)
	errStopImportWalk             = errors.New("stop import walk")
)

func Open(rootDir string) (*Catalog, error) {
	if rootDir == "" {
		return nil, errors.New("archive root directory is required")
	}
	storeDir := filepath.Join(rootDir, "objects")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}
	trashDir := filepath.Join(rootDir, "trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return nil, fmt.Errorf("create trash store: %w", err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(rootDir, "catalog.db"))
	if err != nil {
		return nil, fmt.Errorf("open catalog database: %w", err)
	}
	c := &Catalog{db: db, rootDir: rootDir, storeDir: storeDir, trashDir: trashDir}
	if err := c.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

func (c *Catalog) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// ObjectDir reports the directory containing catalogued DICOM objects.
func (c *Catalog) ObjectDir() string {
	if c == nil {
		return ""
	}
	return c.storeDir
}

// IntegrityCheck runs SQLite's catalog integrity check.
func (c *Catalog) IntegrityCheck(ctx context.Context) error {
	rows, err := c.db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("run catalog integrity check: %w", err)
	}
	defer rows.Close()

	var findings []string
	for rows.Next() {
		var finding string
		if err := rows.Scan(&finding); err != nil {
			return fmt.Errorf("scan catalog integrity check: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(finding), "ok") {
			continue
		}
		findings = append(findings, finding)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate catalog integrity check: %w", err)
	}
	if len(findings) > 0 {
		return fmt.Errorf("catalog integrity check failed: %s", strings.Join(findings, "; "))
	}
	return nil
}

// StoredPaths returns all catalogued object paths.
func (c *Catalog) StoredPaths(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT stored_path FROM instances ORDER BY sha256 ASC`)
	if err != nil {
		return nil, fmt.Errorf("query stored object paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan stored object path: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored object paths: %w", err)
	}
	return paths, nil
}

func (c *Catalog) RebaseStoredPaths(ctx context.Context) (int64, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT sha256, stored_path FROM instances ORDER BY sha256 ASC`)
	if err != nil {
		return 0, fmt.Errorf("query stored object paths for rebase: %w", err)
	}
	defer rows.Close()

	type update struct {
		sha256 string
		path   string
	}
	var updates []update
	for rows.Next() {
		var sha256 string
		var storedPath string
		if err := rows.Scan(&sha256, &storedPath); err != nil {
			return 0, fmt.Errorf("scan stored object path for rebase: %w", err)
		}
		name := filepath.Base(strings.TrimSpace(storedPath))
		if name == "" || name == "." || name == string(os.PathSeparator) {
			return 0, fmt.Errorf("cannot rebase empty stored object path for instance %s", sha256)
		}
		nextPath := filepath.Join(c.storeDir, name)
		if nextPath == storedPath {
			continue
		}
		updates = append(updates, update{sha256: sha256, path: nextPath})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate stored object paths for rebase: %w", err)
	}
	if len(updates) == 0 {
		return 0, nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin stored object path rebase: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE instances SET stored_path = ? WHERE sha256 = ?`, item.path, item.sha256); err != nil {
			return 0, fmt.Errorf("update stored object path: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit stored object path rebase: %w", err)
	}
	committed = true
	return int64(len(updates)), nil
}

func (c *Catalog) ArchiveStats(ctx context.Context) (ArchiveStats, error) {
	var stats ArchiveStats
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(file_size), 0) FROM instances`).Scan(&stats.InstanceCount, &stats.TotalBytes); err != nil {
		return ArchiveStats{}, fmt.Errorf("query archive stats: %w", err)
	}
	return stats, nil
}

func (c *Catalog) BackupCatalogTo(ctx context.Context, destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("backup catalog destination already exists: %s", destPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup catalog destination: %w", err)
	}
	if _, err := c.db.ExecContext(ctx, `VACUUM INTO ?`, destPath); err != nil {
		return fmt.Errorf("backup catalog database: %w", err)
	}
	return nil
}

func (c *Catalog) ImportPath(ctx context.Context, path string) (ImportReport, error) {
	return c.ImportPathWithOptions(ctx, path, ImportOptions{})
}

func (c *Catalog) ImportPathWithOptions(ctx context.Context, path string, opts ImportOptions) (ImportReport, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ImportReport{}, fmt.Errorf("stat import path: %w", err)
	}
	var report ImportReport
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(path), ".zip") {
			return c.importZip(ctx, path, &report, opts)
		}
		if rejectImportPathLength(&report, path, opts.Limits.MaxImportPathLength) {
			return report, nil
		}
		c.importFile(ctx, path, &report, opts)
		return report, nil
	}
	err = filepath.WalkDir(path, func(entryPath string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			report.Rejections = append(report.Rejections, Rejection{Path: entryPath, Reason: walkErr.Error()})
			return nil
		}
		if entry.IsDir() {
			if entryPath != path && shouldSkipImportDir(path, entryPath, opts.Limits.MaxImportDirectoryDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeType != 0 {
			return nil
		}
		if rejectImportPathLength(&report, entryPath, opts.Limits.MaxImportPathLength) {
			return nil
		}
		if opts.Limits.MaxImportTotalFiles > 0 && report.ScannedFiles >= opts.Limits.MaxImportTotalFiles {
			report.Rejections = append(report.Rejections, Rejection{
				Path:   path,
				Reason: fmt.Sprintf("max_import_total_files exceeded: limit is %d", opts.Limits.MaxImportTotalFiles),
			})
			return errStopImportWalk
		}
		c.importFile(ctx, entryPath, &report, opts)
		return nil
	})
	if errors.Is(err, errStopImportWalk) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	return report, nil
}

func shouldSkipImportDir(root string, dir string, maxDepth int) bool {
	if maxDepth <= 0 {
		return false
	}
	return importPathDepth(root, dir) >= maxDepth
}

func importPathDepth(root string, importPath string) int {
	rel, err := filepath.Rel(root, importPath)
	if err != nil || rel == "." || rel == "" {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

func rejectImportPathLength(report *ImportReport, importPath string, maxLength int) bool {
	if maxLength <= 0 || len(importPath) <= maxLength {
		return false
	}
	report.Rejections = append(report.Rejections, Rejection{
		Path:   importPath,
		Reason: limitExceededReason("max_import_path_length", int64(len(importPath)), int64(maxLength)),
	})
	return true
}

func (c *Catalog) importZip(ctx context.Context, zipPath string, report *ImportReport, opts ImportOptions) (ImportReport, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return *report, fmt.Errorf("open ZIP import file: %w", err)
	}
	defer reader.Close()

	seen := map[string]bool{}
	extractedBytes := int64(0)
	entriesSeen := 0
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return *report, err
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if opts.Limits.MaxZipEntryCount > 0 && entriesSeen >= opts.Limits.MaxZipEntryCount {
			report.Rejections = append(report.Rejections, Rejection{
				Path: zipPath,
				Reason: fmt.Sprintf(
					"ZIP entry count limit exceeded: archive has %d entries, limit is %d",
					len(reader.File),
					opts.Limits.MaxZipEntryCount,
				),
			})
			return *report, nil
		}
		entriesSeen++

		safeName, ok := safeZipEntryName(entry.Name)
		sourcePath := fmt.Sprintf("zip://%s!%s", zipPath, entry.Name)
		if !ok {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: "unsafe ZIP entry path"})
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			continue
		}
		if seen[safeName] {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: "duplicate ZIP entry path"})
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			continue
		}
		seen[safeName] = true
		if rejectImportPathLength(report, safeName, opts.Limits.MaxImportPathLength) {
			report.ScannedFiles++
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			continue
		}

		entrySize := zipEntrySize(entry)
		if opts.Limits.MaxZipEntryBytes > 0 && entrySize > opts.Limits.MaxZipEntryBytes {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{
				Path:   fmt.Sprintf("zip://%s!%s", zipPath, safeName),
				Reason: fmt.Sprintf("ZIP entry size %d exceeds limit %d", entrySize, opts.Limits.MaxZipEntryBytes),
			})
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			continue
		}
		if opts.Limits.MaxZipTotalBytes > 0 {
			projectedBytes := extractedBytes + entrySize
			if projectedBytes > opts.Limits.MaxZipTotalBytes {
				report.ScannedFiles++
				report.Rejections = append(report.Rejections, Rejection{
					Path: fmt.Sprintf("zip://%s!%s", zipPath, safeName),
					Reason: fmt.Sprintf(
						"ZIP total extracted bytes limit exceeded: current total %d plus entry size %d exceeds limit %d",
						extractedBytes,
						entrySize,
						opts.Limits.MaxZipTotalBytes,
					),
				})
				reportImportProgress(opts.OnProgress, *report, sourcePath)
				return *report, nil
			}
		}

		tempPath, extracted, err := c.extractZipEntry(entry, zipReadLimit(opts.Limits, extractedBytes))
		if err != nil {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			continue
		}
		if opts.Limits.MaxZipEntryBytes > 0 && extracted > opts.Limits.MaxZipEntryBytes {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{
				Path:   fmt.Sprintf("zip://%s!%s", zipPath, safeName),
				Reason: limitExceededReason("max_zip_entry_bytes", extracted, opts.Limits.MaxZipEntryBytes),
			})
			_ = os.Remove(tempPath)
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			continue
		}
		if opts.Limits.MaxZipTotalBytes > 0 && extractedBytes+extracted > opts.Limits.MaxZipTotalBytes {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{
				Path:   fmt.Sprintf("zip://%s!%s", zipPath, safeName),
				Reason: limitExceededReason("max_zip_total_bytes", extractedBytes+extracted, opts.Limits.MaxZipTotalBytes),
			})
			_ = os.Remove(tempPath)
			reportImportProgress(opts.OnProgress, *report, sourcePath)
			return *report, nil
		}
		extractedBytes += extracted

		c.importFileWithSource(ctx, tempPath, fmt.Sprintf("zip://%s!%s", zipPath, safeName), report, opts)
		_ = os.Remove(tempPath)
	}
	return *report, nil
}

func (c *Catalog) extractZipEntry(entry *zip.File, maxBytes int64) (string, int64, error) {
	source, err := entry.Open()
	if err != nil {
		return "", 0, fmt.Errorf("open ZIP entry: %w", err)
	}
	defer source.Close()

	temp, err := os.CreateTemp(c.storeDir, ".zip-*.dcm")
	if err != nil {
		return "", 0, fmt.Errorf("create ZIP staging file: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	var copied int64
	if maxBytes > 0 {
		limited := &io.LimitedReader{R: source, N: maxBytes + 1}
		copied, err = io.Copy(temp, limited)
	} else {
		copied, err = io.Copy(temp, source)
	}
	if err != nil {
		return "", copied, fmt.Errorf("read ZIP entry: %w", err)
	}
	if maxBytes > 0 && copied > maxBytes {
		return "", copied, fmt.Errorf("ZIP entry read limit exceeded: %d > %d", copied, maxBytes)
	}
	if err := temp.Close(); err != nil {
		return "", copied, fmt.Errorf("close ZIP staging file: %w", err)
	}
	committed = true
	return tempPath, copied, nil
}

func zipEntrySize(entry *zip.File) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if entry.UncompressedSize64 > uint64(maxInt64) {
		return maxInt64
	}
	return int64(entry.UncompressedSize64)
}

func zipReadLimit(limits ImportLimits, extractedBytes int64) int64 {
	limit := limits.MaxZipEntryBytes
	if limits.MaxZipTotalBytes <= 0 {
		return limit
	}
	remaining := limits.MaxZipTotalBytes - extractedBytes
	if remaining < 0 {
		remaining = 0
	}
	if limit <= 0 || remaining < limit {
		return remaining
	}
	return limit
}

func safeZipEntryName(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "/") {
		return "", false
	}
	clean := pathpkg.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func (c *Catalog) ImportPart10File(ctx context.Context, path string, sourcePath string) (ImportReport, error) {
	return c.ImportPart10FileWithOptions(ctx, path, sourcePath, ImportOptions{})
}

func (c *Catalog) ImportPart10FileWithOptions(ctx context.Context, path string, sourcePath string, opts ImportOptions) (ImportReport, error) {
	if sourcePath == "" {
		sourcePath = path
	}
	var report ImportReport
	c.importFileWithSource(ctx, path, sourcePath, &report, opts)
	return report, nil
}

func (c *Catalog) ImportObject(ctx context.Context, sourcePath string, dataset *object.Object, syntax transfer.Syntax) (ImportReport, error) {
	return c.ImportObjectWithOptions(ctx, sourcePath, dataset, syntax, ImportOptions{})
}

func (c *Catalog) ImportObjectWithOptions(ctx context.Context, sourcePath string, dataset *object.Object, syntax transfer.Syntax, opts ImportOptions) (ImportReport, error) {
	if dataset == nil {
		return ImportReport{}, errors.New("dataset is required")
	}
	if syntax.UID == "" {
		return ImportReport{}, errors.New("transfer syntax is required")
	}
	sopClassUID, ok := dataset.GetUID(tagSOPClassUID)
	if !ok || sopClassUID == "" {
		return ImportReport{}, object.ErrMissingSOPClassUID
	}
	sopInstanceUID, ok := dataset.GetUID(tagSOPInstanceUID)
	if !ok || sopInstanceUID == "" {
		return ImportReport{}, object.ErrMissingSOPInstanceUID
	}
	if sourcePath == "" {
		sourcePath = "network://" + sopInstanceUID
	}

	temp, err := os.CreateTemp(c.storeDir, ".network-*.dcm")
	if err != nil {
		return ImportReport{}, fmt.Errorf("create network import file: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	file := &object.File{
		Meta: object.FromElements([]core.Element{
			uidElement(tagMediaStorageSOPClassUID, sopClassUID),
			uidElement(tagMediaStorageSOPInstanceUID, sopInstanceUID),
			uidElement(tagTransferSyntaxUID, syntax.UID),
		}, std.Dictionary),
		Dataset:        dataset,
		TransferSyntax: syntax,
	}
	if err := object.WriteFile(temp, file); err != nil {
		_ = temp.Close()
		return ImportReport{}, fmt.Errorf("write network import file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return ImportReport{}, fmt.Errorf("close network import file: %w", err)
	}

	report, err := c.ImportPart10FileWithOptions(ctx, tempPath, sourcePath, opts)
	if err != nil {
		return report, err
	}
	committed = true
	_ = os.Remove(tempPath)
	return report, nil
}

func (c *Catalog) DecompressStudy(ctx context.Context, studyInstanceUID string) (DecompressReport, error) {
	instances, err := c.InstancesForStudy(ctx, studyInstanceUID)
	if err != nil {
		return DecompressReport{}, err
	}
	var report DecompressReport
	for _, instance := range instances {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		report.ScannedFiles++
		if !pathUnderRoot(c.storeDir, instance.StoredPath) {
			report.FailedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: instance.StoredPath, Reason: "stored object is outside archive store"})
			continue
		}
		stagedPath, digest, size, changed, err := c.decompressFileToStage(instance.StoredPath)
		if err != nil {
			report.FailedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: instance.StoredPath, Reason: err.Error()})
			continue
		}
		if !changed {
			report.SkippedFiles++
			continue
		}
		removeStaged := true
		func() {
			defer func() {
				if removeStaged {
					_ = os.Remove(stagedPath)
				}
			}()
			summary, err := dicominspect.InspectFile(stagedPath, dicominspect.DefaultOptions())
			if err != nil {
				report.FailedFiles++
				report.Rejections = append(report.Rejections, Rejection{Path: instance.StoredPath, Reason: err.Error()})
				return
			}
			storedPath := filepath.Join(c.storeDir, digest+".dcm")
			if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
				if err := os.Rename(stagedPath, storedPath); err != nil {
					report.FailedFiles++
					report.Rejections = append(report.Rejections, Rejection{Path: instance.StoredPath, Reason: err.Error()})
					return
				}
				removeStaged = false
			} else if err != nil {
				report.FailedFiles++
				report.Rejections = append(report.Rejections, Rejection{Path: instance.StoredPath, Reason: err.Error()})
				return
			}
			next := instanceFromSummary(summary, digest, storedPath, instance.SourcePath, size, time.Now().UTC())
			if err := c.replaceInstance(ctx, instance.SHA256, next); err != nil {
				report.FailedFiles++
				report.Rejections = append(report.Rejections, Rejection{Path: instance.StoredPath, Reason: err.Error()})
				return
			}
			if instance.StoredPath != storedPath {
				_ = os.Remove(instance.StoredPath)
			}
			report.DecompressedFiles++
		}()
	}
	return report, nil
}

func (c *Catalog) Studies(ctx context.Context) ([]Study, error) {
	return c.StudiesWithFilters(ctx, StudyFilters{})
}

func (c *Catalog) StudiesWithFilters(ctx context.Context, filters StudyFilters) ([]Study, error) {
	page, err := c.StudiesPageWithFilters(ctx, filters, StudyPageOptions{})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (c *Catalog) StudiesPageWithFilters(ctx context.Context, filters StudyFilters, opts StudyPageOptions) (StudyPage, error) {
	where, args := studyFilterWhere(filters)
	total, err := c.studyTotal(ctx, where, args)
	if err != nil {
		return StudyPage{}, err
	}
	query := studySelectSQL(where)
	queryArgs := append([]any(nil), args...)
	limit := opts.Limit
	if limit > 0 {
		offset := opts.Offset
		if offset < 0 {
			offset = 0
		}
		query += "\nLIMIT ? OFFSET ?"
		queryArgs = append(queryArgs, limit, offset)
		opts.Offset = offset
	} else {
		opts.Limit = 0
		opts.Offset = 0
	}

	rows, err := c.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return StudyPage{}, fmt.Errorf("query studies: %w", err)
	}
	defer rows.Close()

	var studies []Study
	for rows.Next() {
		study, err := scanStudy(rows)
		if err != nil {
			return StudyPage{}, err
		}
		studies = append(studies, study)
	}
	if err := rows.Err(); err != nil {
		return StudyPage{}, fmt.Errorf("iterate studies: %w", err)
	}
	return StudyPage{Items: studies, Total: total, Limit: opts.Limit, Offset: opts.Offset}, nil
}

func (c *Catalog) studyTotal(ctx context.Context, where string, args []any) (int, error) {
	query := `
SELECT COUNT(*)
FROM (
  SELECT 1
  FROM instances
  LEFT JOIN study_metadata sm ON sm.study_uid = COALESCE(NULLIF(instances.study_instance_uid, ''), '(missing)')
`
	if where != "" {
		query += "WHERE " + where + "\n"
	}
	query += "GROUP BY COALESCE(NULLIF(study_instance_uid, ''), '(missing)'))"
	var total int
	if err := c.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count studies: %w", err)
	}
	return total, nil
}

func studySelectSQL(where string) string {
	query := `
SELECT
  COALESCE(NULLIF(study_instance_uid, ''), '(missing)') AS study_instance_uid,
  COALESCE(MAX(NULLIF(patient_name, '')), '') AS patient_name,
  COALESCE(MAX(NULLIF(patient_id, '')), '') AS patient_id,
  COALESCE(MAX(NULLIF(patient_birth_date, '')), '') AS patient_birth_date,
  COALESCE(MAX(NULLIF(institution_name, '')), '') AS institution_name,
  COALESCE(MAX(NULLIF(study_date, '')), '') AS study_date,
  COALESCE(MAX(NULLIF(study_time, '')), '') AS study_time,
  COALESCE(MAX(NULLIF(study_description, '')), '') AS study_description,
  COALESCE(group_concat(DISTINCT NULLIF(modality, '')), '') AS modalities,
  COALESCE(MAX(NULLIF(accession_number, '')), '') AS accession_number,
  COALESCE(MAX(NULLIF(sm.status, '')), '') AS status,
  COALESCE(MAX(NULLIF(sm.comments, '')), '') AS comments,
  COALESCE(MAX(NULLIF(study_id, '')), '') AS study_id,
  COALESCE(MAX(NULLIF(body_part_examined, '')), '') AS body_part_examined,
  COALESCE(MAX(NULLIF(referring_physician_name, '')), '') AS referring_physician_name,
  COALESCE(MAX(NULLIF(performing_physician_name, '')), '') AS performing_physician_name,
  COUNT(DISTINCT COALESCE(NULLIF(series_instance_uid, ''), '(missing)')) AS series_count,
  COUNT(*) AS instance_count,
  COALESCE(MAX(imported_at), '') AS imported_at
FROM instances
LEFT JOIN study_metadata sm ON sm.study_uid = COALESCE(NULLIF(instances.study_instance_uid, ''), '(missing)')
`
	if where != "" {
		query += "WHERE " + where + "\n"
	}
	query += `
GROUP BY COALESCE(NULLIF(study_instance_uid, ''), '(missing)')
ORDER BY imported_at DESC, patient_name ASC, study_instance_uid ASC`
	return query
}

func studyFilterWhere(filters StudyFilters) (string, []any) {
	var clauses []string
	var args []any
	addLike := func(column string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		clauses = append(clauses, column+" COLLATE NOCASE LIKE ?")
		args = append(args, "%"+value+"%")
	}
	if !filters.PatientNameSoundex {
		addLike("patient_name", filters.PatientName)
	}
	addLike("patient_id", filters.PatientID)
	addLike("accession_number", filters.AccessionNumber)
	addLike("study_description", filters.StudyDescription)
	addLike("source_path", filters.SourcePath)
	addLike("sm.status", filters.Status)
	addLike("patient_birth_date", filters.PatientBirthDate)
	addLike("study_id", filters.StudyID)
	addLike("study_instance_uid", filters.StudyInstanceUID)
	addLike("body_part_examined", filters.BodyPart)
	addLike("modality", filters.Modality)
	addLike("referring_physician_name", filters.ReferringPhysician)
	addLike("performing_physician_name", filters.PerformingPhysician)
	addLike("sm.comments", filters.Comments)
	if filters.HasComments {
		clauses = append(clauses, "TRIM(COALESCE(sm.comments, '')) <> ''")
	}

	if allFields := strings.TrimSpace(filters.AllFields); allFields != "" {
		orColumns := []string{
			"patient_name",
			"patient_id",
			"study_description",
			"accession_number",
			"modality",
			"study_instance_uid",
			"study_id",
			"body_part_examined",
			"referring_physician_name",
			"performing_physician_name",
			"patient_birth_date",
			"COALESCE(sm.comments, '')",
		}
		orClauses := make([]string, len(orColumns))
		pattern := "%" + allFields + "%"
		for i, column := range orColumns {
			orClauses[i] = column + " COLLATE NOCASE LIKE ?"
			args = append(args, pattern)
		}
		clauses = append(clauses, "("+strings.Join(orClauses, " OR ")+")")
	}

	if value := strings.TrimSpace(filters.StudyDateFrom); value != "" {
		clauses = append(clauses, "study_date >= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.StudyDateTo); value != "" {
		clauses = append(clauses, "study_date <= ?")
		args = append(args, value)
	}
	studyDateTime := "(study_date || SUBSTR(REPLACE(COALESCE(study_time, ''), '.', '') || '000000', 1, 6))"
	if value := strings.TrimSpace(filters.StudyDateTimeFrom); value != "" {
		clauses = append(clauses, studyDateTime+" >= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.StudyDateTimeTo); value != "" {
		clauses = append(clauses, studyDateTime+" <= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.ImportedAtFrom); value != "" {
		clauses = append(clauses, "imported_at >= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.ImportedAtTo); value != "" {
		clauses = append(clauses, "imported_at <= ?")
		args = append(args, value)
	}

	var modalities []string
	for _, modality := range filters.Modalities {
		modality = strings.TrimSpace(modality)
		if modality != "" {
			modalities = append(modalities, modality)
		}
	}
	if len(modalities) > 0 {
		placeholders := make([]string, len(modalities))
		for i, modality := range modalities {
			placeholders[i] = "?"
			args = append(args, modality)
		}
		clauses = append(clauses, "modality COLLATE NOCASE IN ("+strings.Join(placeholders, ", ")+")")
	}
	if filters.PatientNameSoundex {
		for _, code := range soundexTokenCodes(filters.PatientName) {
			clauses = append(clauses, `EXISTS (
  SELECT 1
  FROM instance_patient_soundex ips
  WHERE ips.instance_sha256 = instances.sha256
    AND ips.code = ?
)`)
			args = append(args, code)
		}
	}

	return strings.Join(clauses, " AND "), args
}

func filterStudiesByPatientNameSoundex(studies []Study, filters StudyFilters) []Study {
	if !filters.PatientNameSoundex {
		return studies
	}
	queryCodes := soundexTokenCodes(filters.PatientName)
	if len(queryCodes) == 0 {
		return studies
	}
	var filtered []Study
	for _, study := range studies {
		studyCodes := soundexTokenCodeSet(study.PatientName)
		if soundexCodesContainAll(studyCodes, queryCodes) {
			filtered = append(filtered, study)
		}
	}
	return filtered
}

func soundexCodesContainAll(haystack map[string]bool, needles []string) bool {
	for _, code := range needles {
		if !haystack[code] {
			return false
		}
	}
	return true
}

func soundexTokenCodeSet(value string) map[string]bool {
	codes := soundexTokenCodes(value)
	out := make(map[string]bool, len(codes))
	for _, code := range codes {
		out[code] = true
	}
	return out
}

func soundexTokenCodes(value string) []string {
	tokens := soundexNameTokens(value)
	codes := make([]string, 0, len(tokens))
	seen := map[string]bool{}
	for _, token := range tokens {
		code := soundexCode(token)
		if code != "" && !seen[code] {
			codes = append(codes, code)
			seen[code] = true
		}
	}
	return codes
}

func soundexNameTokens(value string) []string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}
	for _, r := range strings.ToUpper(value) {
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func soundexCode(token string) string {
	if token == "" {
		return ""
	}
	first := token[0]
	var encoded []byte
	previous := soundexDigit(first)
	for i := 1; i < len(token); i++ {
		digit := soundexDigit(token[i])
		if digit == '0' {
			previous = digit
			continue
		}
		if digit != previous {
			encoded = append(encoded, digit)
		}
		previous = digit
	}
	code := string(append([]byte{first}, encoded...))
	if len(code) >= 4 {
		return code[:4]
	}
	return code + strings.Repeat("0", 4-len(code))
}

func soundexDigit(letter byte) byte {
	switch letter {
	case 'B', 'F', 'P', 'V':
		return '1'
	case 'C', 'G', 'J', 'K', 'Q', 'S', 'X', 'Z':
		return '2'
	case 'D', 'T':
		return '3'
	case 'L':
		return '4'
	case 'M', 'N':
		return '5'
	case 'R':
		return '6'
	default:
		return '0'
	}
}

func scanStudy(rows *sql.Rows) (Study, error) {
	var study Study
	var importedAt string
	if err := rows.Scan(
		&study.StudyInstanceUID,
		&study.PatientName,
		&study.PatientID,
		&study.PatientBirthDate,
		&study.InstitutionName,
		&study.StudyDate,
		&study.StudyTime,
		&study.StudyDescription,
		&study.Modalities,
		&study.AccessionNumber,
		&study.Status,
		&study.Comments,
		&study.StudyID,
		&study.BodyPartExamined,
		&study.ReferringPhysicianName,
		&study.PerformingPhysicianName,
		&study.SeriesCount,
		&study.InstanceCount,
		&importedAt,
	); err != nil {
		return Study{}, fmt.Errorf("scan study: %w", err)
	}
	study.ImportedAt, _ = time.Parse(time.RFC3339Nano, importedAt)
	return study, nil
}

func (c *Catalog) InstancesForStudy(ctx context.Context, studyInstanceUID string) ([]Instance, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT
  sha256, stored_path, source_path, file_size,
  patient_name, patient_id, patient_birth_date, institution_name,
  study_date, study_time, series_date, series_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax,
  study_id, body_part_examined, referring_physician_name, performing_physician_name,
  imported_at
FROM instances
WHERE study_instance_uid = ?
ORDER BY series_instance_uid ASC, sop_instance_uid ASC, imported_at ASC`, storedLookupUID(studyInstanceUID))
	if err != nil {
		return nil, fmt.Errorf("query study instances: %w", err)
	}
	defer rows.Close()

	var instances []Instance
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate study instances: %w", err)
	}
	return instances, nil
}

func (c *Catalog) SeriesForStudy(ctx context.Context, studyInstanceUID string) ([]Series, error) {
	return c.SeriesForStudyWithFilters(ctx, studyInstanceUID, SeriesFilters{})
}

func (c *Catalog) SeriesForStudyWithFilters(ctx context.Context, studyInstanceUID string, filters SeriesFilters) ([]Series, error) {
	where, args := seriesFilterWhere(studyInstanceUID, filters)
	rows, err := c.db.QueryContext(ctx, `
SELECT
  COALESCE(MAX(NULLIF(study_instance_uid, '')), '') AS study_instance_uid,
  COALESCE(NULLIF(series_instance_uid, ''), '(missing)') AS series_instance_uid,
  COALESCE(MAX(NULLIF(modality, '')), '') AS modality,
  COALESCE(MAX(NULLIF(series_number, '')), '') AS series_number,
  COALESCE(MAX(NULLIF(series_description, '')), '') AS series_description,
  COALESCE(MAX(NULLIF(series_date, '')), '') AS series_date,
  COALESCE(MAX(NULLIF(series_time, '')), '') AS series_time,
  COUNT(*) AS instance_count,
  COALESCE(MAX(imported_at), '') AS imported_at
FROM instances
WHERE `+where+`
GROUP BY COALESCE(NULLIF(series_instance_uid, ''), '(missing)')
ORDER BY
  CASE WHEN series_number GLOB '[0-9]*' THEN CAST(series_number AS INTEGER) END ASC,
  series_number ASC,
  series_instance_uid ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query study series: %w", err)
	}
	defer rows.Close()

	var series []Series
	for rows.Next() {
		item, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		series = append(series, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate study series: %w", err)
	}
	return series, nil
}

func seriesFilterWhere(studyInstanceUID string, filters SeriesFilters) (string, []any) {
	clauses := []string{"study_instance_uid = ?"}
	args := []any{storedLookupUID(studyInstanceUID)}
	addLike := func(column string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		clauses = append(clauses, column+" COLLATE NOCASE LIKE ?")
		args = append(args, "%"+value+"%")
	}
	addLike("series_number", filters.SeriesNumber)
	addLike("series_description", filters.SeriesDescription)
	if modality := strings.TrimSpace(filters.Modality); modality != "" {
		clauses = append(clauses, "modality COLLATE NOCASE = ?")
		args = append(args, modality)
	}
	return strings.Join(clauses, " AND "), args
}

func (c *Catalog) InstancesForSeries(ctx context.Context, seriesInstanceUID string) ([]Instance, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT
  sha256, stored_path, source_path, file_size,
  patient_name, patient_id, patient_birth_date, institution_name,
  study_date, study_time, series_date, series_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax,
  study_id, body_part_examined, referring_physician_name, performing_physician_name,
  imported_at
FROM instances
WHERE series_instance_uid = ?
ORDER BY
  CASE WHEN instance_number GLOB '[0-9]*' THEN CAST(instance_number AS INTEGER) END ASC,
  instance_number ASC,
  sop_instance_uid ASC,
  imported_at ASC`, storedLookupUID(seriesInstanceUID))
	if err != nil {
		return nil, fmt.Errorf("query series instances: %w", err)
	}
	defer rows.Close()

	var instances []Instance
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate series instances: %w", err)
	}
	return instances, nil
}

func (c *Catalog) InstanceBySOPInstanceUID(ctx context.Context, sopInstanceUID string) (Instance, error) {
	sopInstanceUID = strings.TrimSpace(sopInstanceUID)
	if sopInstanceUID == "" {
		return Instance{}, fmt.Errorf("SOP instance UID is required")
	}
	row := c.db.QueryRowContext(ctx, `
SELECT
  sha256, stored_path, source_path, file_size,
  patient_name, patient_id, patient_birth_date, institution_name,
  study_date, study_time, series_date, series_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax,
  study_id, body_part_examined, referring_physician_name, performing_physician_name,
  imported_at
FROM instances
WHERE sop_instance_uid = ?
ORDER BY imported_at ASC, sha256 ASC
LIMIT 1`, storedLookupUID(sopInstanceUID))
	instance, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, fmt.Errorf("SOP instance UID %q not found", sopInstanceUID)
	}
	if err != nil {
		return Instance{}, err
	}
	return instance, nil
}

func (c *Catalog) SchemaMigrations(ctx context.Context) ([]SchemaMigration, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT version, name, applied_at
FROM schema_migrations
ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("query schema migrations: %w", err)
	}
	defer rows.Close()

	var migrations []SchemaMigration
	for rows.Next() {
		var migration SchemaMigration
		var appliedAt string
		if err := rows.Scan(&migration.Version, &migration.Name, &appliedAt); err != nil {
			return nil, fmt.Errorf("scan schema migration: %w", err)
		}
		migration.AppliedAt, _ = time.Parse(time.RFC3339Nano, appliedAt)
		migrations = append(migrations, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema migrations: %w", err)
	}
	return migrations, nil
}

func (c *Catalog) init() error {
	if _, err := c.db.Exec(`
CREATE TABLE IF NOT EXISTS instances (
  sha256 TEXT PRIMARY KEY,
  stored_path TEXT NOT NULL,
  source_path TEXT NOT NULL,
  file_size INTEGER NOT NULL,
  patient_name TEXT NOT NULL DEFAULT '',
  patient_id TEXT NOT NULL DEFAULT '',
  patient_birth_date TEXT NOT NULL DEFAULT '',
  institution_name TEXT NOT NULL DEFAULT '',
  study_date TEXT NOT NULL DEFAULT '',
  study_time TEXT NOT NULL DEFAULT '',
  series_date TEXT NOT NULL DEFAULT '',
  series_time TEXT NOT NULL DEFAULT '',
  study_description TEXT NOT NULL DEFAULT '',
  modality TEXT NOT NULL DEFAULT '',
  accession_number TEXT NOT NULL DEFAULT '',
  study_instance_uid TEXT NOT NULL DEFAULT '',
  series_instance_uid TEXT NOT NULL DEFAULT '',
  series_number TEXT NOT NULL DEFAULT '',
  series_description TEXT NOT NULL DEFAULT '',
  sop_class_uid TEXT NOT NULL DEFAULT '',
  sop_instance_uid TEXT NOT NULL DEFAULT '',
  instance_number TEXT NOT NULL DEFAULT '',
  transfer_syntax_uid TEXT NOT NULL DEFAULT '',
  transfer_syntax TEXT NOT NULL DEFAULT '',
  imported_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_instances_study ON instances(study_instance_uid);
CREATE INDEX IF NOT EXISTS idx_instances_series ON instances(series_instance_uid);
CREATE INDEX IF NOT EXISTS idx_instances_patient ON instances(patient_name, patient_id);
CREATE INDEX IF NOT EXISTS idx_instances_sha256 ON instances(sha256);
CREATE INDEX IF NOT EXISTS idx_instances_sop ON instances(sop_instance_uid);
CREATE INDEX IF NOT EXISTS idx_instances_imported_at ON instances(imported_at);
CREATE INDEX IF NOT EXISTS idx_instances_modality_nocase ON instances(modality COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_instances_patient_name_nocase ON instances(patient_name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_instances_patient_id_nocase ON instances(patient_id COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_instances_accession_nocase ON instances(accession_number COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_instances_study_description_nocase ON instances(study_description COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_instances_study_date ON instances(study_date);
CREATE TABLE IF NOT EXISTS instance_patient_soundex (
  instance_sha256 TEXT NOT NULL,
  code TEXT NOT NULL,
  PRIMARY KEY(instance_sha256, code)
);
CREATE INDEX IF NOT EXISTS idx_instance_patient_soundex_code ON instance_patient_soundex(code, instance_sha256);
CREATE TABLE IF NOT EXISTS study_metadata (
  study_uid TEXT PRIMARY KEY,
  status TEXT NOT NULL DEFAULT '',
  comments TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_study_metadata_status_nocase ON study_metadata(status COLLATE NOCASE);
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL,
  token_id TEXT NOT NULL,
  remote_addr TEXT NOT NULL,
  operation TEXT NOT NULL,
  uid_scope TEXT,
  status INTEGER NOT NULL,
  bytes INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  error_summary TEXT,
  occurred_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_log_occurred_at ON audit_log(occurred_at);
	`); err != nil {
		return fmt.Errorf("initialize catalog schema: %w", err)
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "series_number", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "series_description", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "instance_number", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "patient_birth_date", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "institution_name", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "study_time", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "series_date", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "series_time", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "study_id", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "body_part_examined", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "referring_physician_name", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "performing_physician_name", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := c.ensureInstanceColumn(column.name, column.definition); err != nil {
			return err
		}
	}
	if err := c.ensureStudyMetadataColumn("report", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := c.recordSchemaMigration(CatalogSchemaVersion, "create_instances_and_metadata_columns"); err != nil {
		return err
	}
	if err := c.backfillInstancePatientSoundex(); err != nil {
		return err
	}
	c.backfillInstanceExtraTags()
	return nil
}

func (c *Catalog) recordSchemaMigration(version int, name string) error {
	if _, err := c.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("initialize schema migration tracking: %w", err)
	}
	if _, err := c.db.Exec(`
INSERT OR IGNORE INTO schema_migrations(version, name, applied_at)
VALUES (?, ?, ?)`, version, name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record schema migration: %w", err)
	}
	return nil
}

func (c *Catalog) SetStudyMetadata(ctx context.Context, studyInstanceUID string, metadata StudyMetadata) error {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" {
		return errors.New("study instance UID is required")
	}
	metadata.Status = strings.TrimSpace(metadata.Status)
	metadata.Comments = strings.TrimSpace(metadata.Comments)
	metadata.Report = strings.TrimSpace(metadata.Report)
	if metadata.Status == "" && metadata.Comments == "" && metadata.Report == "" {
		if _, err := c.db.ExecContext(ctx, `DELETE FROM study_metadata WHERE study_uid = ?`, studyInstanceUID); err != nil {
			return fmt.Errorf("delete study metadata: %w", err)
		}
		return nil
	}
	if _, err := c.db.ExecContext(ctx, `
INSERT INTO study_metadata (study_uid, status, comments, report, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(study_uid) DO UPDATE SET
  status = excluded.status,
  comments = excluded.comments,
  report = excluded.report,
  updated_at = excluded.updated_at
`, studyInstanceUID, metadata.Status, metadata.Comments, metadata.Report, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("upsert study metadata: %w", err)
	}
	return nil
}

func (c *Catalog) StudyMetadata(ctx context.Context, studyInstanceUID string) (StudyMetadata, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" {
		return StudyMetadata{}, errors.New("study instance UID is required")
	}
	var metadata StudyMetadata
	err := c.db.QueryRowContext(ctx, `SELECT status, comments, report FROM study_metadata WHERE study_uid = ?`, studyInstanceUID).Scan(&metadata.Status, &metadata.Comments, &metadata.Report)
	if errors.Is(err, sql.ErrNoRows) {
		return StudyMetadata{}, nil
	}
	if err != nil {
		return StudyMetadata{}, fmt.Errorf("query study metadata: %w", err)
	}
	return metadata, nil
}

func (c *Catalog) DeleteStudy(ctx context.Context, studyInstanceUID string) (int, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" {
		return 0, errors.New("study instance UID is required")
	}
	storedStudyUID := storedLookupUID(studyInstanceUID)
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin delete study transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	paths, err := storedPathsForStudy(ctx, tx, storedStudyUID)
	if err != nil {
		return 0, err
	}
	if err := c.validateStoredObjectPaths(paths); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM instance_patient_soundex WHERE instance_sha256 IN (SELECT sha256 FROM instances WHERE study_instance_uid = ?)`, storedStudyUID); err != nil {
		return 0, fmt.Errorf("delete study soundex codes: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM instances WHERE study_instance_uid = ?`, storedStudyUID)
	if err != nil {
		return 0, fmt.Errorf("delete study instances: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM study_metadata WHERE study_uid = ? OR study_uid = ?`, studyInstanceUID, storedStudyUID); err != nil {
		return 0, fmt.Errorf("delete study metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit delete study: %w", err)
	}
	committed = true

	if err := removeStoredObjectPaths(paths); err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted study instances: %w", err)
	}
	return int(deleted), nil
}

func (c *Catalog) StudyExists(ctx context.Context, studyInstanceUID string) (bool, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" || studyInstanceUID == "(missing)" {
		return false, nil
	}
	var one int
	err := c.db.QueryRowContext(ctx, `SELECT 1 FROM instances WHERE study_instance_uid = ? LIMIT 1`, studyInstanceUID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query study existence: %w", err)
	}
	return true, nil
}

func storedPathsForStudy(ctx context.Context, tx *sql.Tx, studyInstanceUID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT stored_path FROM instances WHERE study_instance_uid = ?`, studyInstanceUID)
	if err != nil {
		return nil, fmt.Errorf("query study object paths: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan study object path: %w", err)
		}
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate study object paths: %w", err)
	}
	return paths, nil
}

func (c *Catalog) validateStoredObjectPaths(paths []string) error {
	for _, path := range paths {
		if !pathUnderRoot(c.storeDir, path) {
			return fmt.Errorf("refusing to remove object outside archive store: %s", path)
		}
	}
	return nil
}

func pathUnderRoot(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == "." || rel == "" || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func removeStoredObjectPaths(paths []string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stored object %s: %w", path, err)
		}
	}
	return nil
}

func (c *Catalog) ensureInstanceColumn(name string, definition string) error {
	rows, err := c.db.Query(`PRAGMA table_info(instances)`)
	if err != nil {
		return fmt.Errorf("inspect catalog schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan catalog schema: %w", err)
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate catalog schema: %w", err)
	}
	if _, err := c.db.Exec("ALTER TABLE instances ADD COLUMN " + name + " " + definition); err != nil {
		return fmt.Errorf("migrate catalog schema: add %s: %w", name, err)
	}
	return nil
}

func (c *Catalog) ensureStudyMetadataColumn(name string, definition string) error {
	rows, err := c.db.Query(`PRAGMA table_info(study_metadata)`)
	if err != nil {
		return fmt.Errorf("inspect study metadata schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan study metadata schema: %w", err)
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate study metadata schema: %w", err)
	}
	if _, err := c.db.Exec("ALTER TABLE study_metadata ADD COLUMN " + name + " " + definition); err != nil {
		return fmt.Errorf("migrate study metadata schema: add %s: %w", name, err)
	}
	return nil
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func scanInstance(rows sqlScanner) (Instance, error) {
	var instance Instance
	var importedAt string
	if err := rows.Scan(
		&instance.SHA256,
		&instance.StoredPath,
		&instance.SourcePath,
		&instance.FileSize,
		&instance.PatientName,
		&instance.PatientID,
		&instance.PatientBirthDate,
		&instance.InstitutionName,
		&instance.StudyDate,
		&instance.StudyTime,
		&instance.SeriesDate,
		&instance.SeriesTime,
		&instance.StudyDescription,
		&instance.Modality,
		&instance.AccessionNumber,
		&instance.StudyInstanceUID,
		&instance.SeriesInstanceUID,
		&instance.SeriesNumber,
		&instance.SeriesDescription,
		&instance.SOPClassUID,
		&instance.SOPInstanceUID,
		&instance.InstanceNumber,
		&instance.TransferSyntaxUID,
		&instance.TransferSyntax,
		&instance.StudyID,
		&instance.BodyPartExamined,
		&instance.ReferringPhysicianName,
		&instance.PerformingPhysicianName,
		&importedAt,
	); err != nil {
		return Instance{}, fmt.Errorf("scan instance: %w", err)
	}
	instance.ImportedAt, _ = time.Parse(time.RFC3339Nano, importedAt)
	return instance, nil
}

func scanSeries(rows *sql.Rows) (Series, error) {
	var series Series
	var importedAt string
	if err := rows.Scan(
		&series.StudyInstanceUID,
		&series.SeriesInstanceUID,
		&series.Modality,
		&series.SeriesNumber,
		&series.SeriesDescription,
		&series.SeriesDate,
		&series.SeriesTime,
		&series.InstanceCount,
		&importedAt,
	); err != nil {
		return Series{}, fmt.Errorf("scan series: %w", err)
	}
	series.ImportedAt, _ = time.Parse(time.RFC3339Nano, importedAt)
	return series, nil
}

func (c *Catalog) importFile(ctx context.Context, path string, report *ImportReport, opts ImportOptions) {
	c.importFileWithSource(ctx, path, path, report, opts)
}

func (c *Catalog) importFileWithSource(ctx context.Context, path string, sourcePath string, report *ImportReport, opts ImportOptions) {
	if sourcePath == "" {
		sourcePath = path
	}
	if err := ctx.Err(); err != nil {
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}
	report.ScannedFiles++
	defer func() {
		reportImportProgress(opts.OnProgress, *report, sourcePath)
	}()

	info, err := os.Stat(path)
	if err != nil {
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}
	if !info.Mode().IsRegular() {
		return
	}
	if opts.Limits.MaxFileImportBytes > 0 && info.Size() > opts.Limits.MaxFileImportBytes {
		report.Rejections = append(report.Rejections, Rejection{
			Path:   sourcePath,
			Reason: limitExceededReason("max_file_import_bytes", info.Size(), opts.Limits.MaxFileImportBytes),
		})
		return
	}

	stagedPath, digest, size, err := c.stage(path, opts.Limits.MaxFileImportBytes)
	if err != nil {
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}
	removeStaged := true
	defer func() {
		if removeStaged {
			_ = os.Remove(stagedPath)
		}
	}()
	if opts.DecompressImages {
		nextPath, nextDigest, nextSize, changed, err := c.decompressFileToStage(stagedPath)
		if err != nil {
			report.InvalidFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
			return
		}
		if changed {
			_ = os.Remove(stagedPath)
			stagedPath, digest, size = nextPath, nextDigest, nextSize
			if opts.Limits.MaxFileImportBytes > 0 && size > opts.Limits.MaxFileImportBytes {
				report.Rejections = append(report.Rejections, Rejection{
					Path:   sourcePath,
					Reason: limitExceededReason("max_file_import_bytes", size, opts.Limits.MaxFileImportBytes),
				})
				return
			}
		}
	}

	summary, err := dicominspect.InspectFile(stagedPath, dicominspect.DefaultOptions())
	if err != nil {
		report.InvalidFiles++
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}

	storedPath := filepath.Join(c.storeDir, digest+".dcm")
	duplicate, err := c.hasInstance(ctx, digest)
	if err != nil {
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}
	if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(stagedPath, storedPath); err != nil {
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
			return
		}
		removeStaged = false
	} else if err != nil {
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}

	instance := instanceFromSummary(summary, digest, storedPath, sourcePath, size, time.Now().UTC())
	if err := c.upsertInstance(ctx, instance); err != nil {
		report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
		return
	}
	if duplicate {
		report.Duplicates++
		return
	}
	report.StoredFiles++
}

func instanceFromSummary(summary dicominspect.Summary, digest string, storedPath string, sourcePath string, size int64, importedAt time.Time) Instance {
	return Instance{
		SHA256:            digest,
		StoredPath:        storedPath,
		SourcePath:        sourcePath,
		FileSize:          size,
		PatientName:       summary.PatientName,
		PatientID:         summary.PatientID,
		PatientBirthDate:  summary.PatientBirthDate,
		InstitutionName:   summary.InstitutionName,
		StudyDate:         summary.StudyDate,
		StudyTime:         summary.StudyTime,
		SeriesDate:        summary.SeriesDate,
		SeriesTime:        summary.SeriesTime,
		StudyDescription:  summary.StudyDescription,
		Modality:          summary.Modality,
		AccessionNumber:   summary.AccessionNumber,
		StudyInstanceUID:  summary.StudyInstanceUID,
		SeriesInstanceUID: summary.SeriesInstanceUID,
		SeriesNumber:      summary.SeriesNumber,
		SeriesDescription: summary.SeriesDescription,
		SOPClassUID:       summary.SOPClassUID,
		SOPInstanceUID:    summary.SOPInstanceUID,
		InstanceNumber:    summary.InstanceNumber,
		TransferSyntaxUID: summary.TransferSyntaxUID,
		TransferSyntax:    summary.TransferSyntax,

		StudyID:                 summary.StudyID,
		BodyPartExamined:        summary.BodyPartExamined,
		ReferringPhysicianName:  summary.ReferringPhysicianName,
		PerformingPhysicianName: summary.PerformingPhysicianName,

		ImportedAt: importedAt,
	}
}

func reportImportProgress(onProgress func(ImportProgress), report ImportReport, path string) {
	if onProgress == nil {
		return
	}
	onProgress(ImportProgress{
		ScannedFiles: report.ScannedFiles,
		StoredFiles:  report.StoredFiles,
		Duplicates:   report.Duplicates,
		InvalidFiles: report.InvalidFiles,
		Path:         path,
	})
}

func limitExceededReason(limit string, got, max int64) string {
	return fmt.Sprintf("%s exceeded: %d > %d", limit, got, max)
}

func uidElement(tag core.Tag, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: core.VRUI},
		Value:  core.StringValue{value},
	}
}

func (c *Catalog) stage(path string, maxBytes int64) (string, string, int64, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", "", 0, fmt.Errorf("open source file: %w", err)
	}
	defer source.Close()

	temp, err := os.CreateTemp(c.storeDir, ".import-*.dcm")
	if err != nil {
		return "", "", 0, fmt.Errorf("create staged file: %w", err)
	}
	closeTemp := true
	defer func() {
		if closeTemp {
			_ = temp.Close()
		}
	}()

	hash := sha256.New()
	var size int64
	if maxBytes > 0 {
		limited := &io.LimitedReader{R: source, N: maxBytes + 1}
		size, err = io.Copy(io.MultiWriter(temp, hash), limited)
	} else {
		size, err = io.Copy(io.MultiWriter(temp, hash), source)
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return "", "", 0, fmt.Errorf("copy source file: %w", err)
	}
	if maxBytes > 0 && size > maxBytes {
		_ = os.Remove(temp.Name())
		return "", "", 0, fmt.Errorf("%s", limitExceededReason("max_file_import_bytes", size, maxBytes))
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return "", "", 0, fmt.Errorf("close staged file: %w", err)
	}
	closeTemp = false
	return temp.Name(), hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (c *Catalog) decompressFileToStage(path string) (string, string, int64, bool, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("open DICOM file for decompression: %w", err)
	}
	file, err := object.ReadFile(source)
	closeErr := source.Close()
	if err != nil {
		return "", "", 0, false, fmt.Errorf("read DICOM file for decompression: %w", err)
	}
	if closeErr != nil {
		return "", "", 0, false, fmt.Errorf("close DICOM file for decompression: %w", closeErr)
	}
	if !shouldDecompressSyntax(file.TransferSyntax) {
		return "", "", 0, false, nil
	}
	registry, err := decompressionRegistry()
	if err != nil {
		return "", "", 0, false, err
	}
	decompressed, err := pixeldata.DecompressFile(file, pixeldata.DecompressOptions{Registry: registry})
	if err != nil {
		return "", "", 0, false, fmt.Errorf("decompress DICOM file: %w", err)
	}

	temp, err := os.CreateTemp(c.storeDir, ".decompress-*.dcm")
	if err != nil {
		return "", "", 0, false, fmt.Errorf("create decompressed staged file: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	hash := sha256.New()
	if err := object.WriteFile(io.MultiWriter(temp, hash), decompressed); err != nil {
		return "", "", 0, false, fmt.Errorf("write decompressed DICOM file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", "", 0, false, fmt.Errorf("close decompressed staged file: %w", err)
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("stat decompressed staged file: %w", err)
	}
	committed = true
	return tempPath, hex.EncodeToString(hash.Sum(nil)), info.Size(), true, nil
}

func shouldDecompressSyntax(syntax transfer.Syntax) bool {
	return syntax.RequiresCodec()
}

func decompressionRegistry() (pixeldata.Registry, error) {
	registry := pixeldata.NewMemoryRegistry()
	for _, register := range []func(pixeldata.Registry) error{
		jpeg.Register,
		jpeglossless.Register,
		rle.Register,
	} {
		if err := register(registry); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (c *Catalog) hasInstance(ctx context.Context, sha string) (bool, error) {
	var exists int
	err := c.db.QueryRowContext(ctx, `SELECT 1 FROM instances WHERE sha256 = ?`, sha).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check duplicate instance: %w", err)
	}
	return true, nil
}

func storedLookupUID(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == missingUIDSentinel {
		return ""
	}
	return uid
}

func (c *Catalog) upsertInstance(ctx context.Context, instance Instance) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert instance: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
INSERT INTO instances (
  sha256, stored_path, source_path, file_size,
  patient_name, patient_id, patient_birth_date, institution_name,
  study_date, study_time, series_date, series_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax,
  study_id, body_part_examined, referring_physician_name, performing_physician_name,
  imported_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(sha256) DO UPDATE SET
  source_path = excluded.source_path,
  imported_at = excluded.imported_at
`,
		instance.SHA256,
		instance.StoredPath,
		instance.SourcePath,
		instance.FileSize,
		instance.PatientName,
		instance.PatientID,
		instance.PatientBirthDate,
		instance.InstitutionName,
		instance.StudyDate,
		instance.StudyTime,
		instance.SeriesDate,
		instance.SeriesTime,
		instance.StudyDescription,
		instance.Modality,
		instance.AccessionNumber,
		instance.StudyInstanceUID,
		instance.SeriesInstanceUID,
		instance.SeriesNumber,
		instance.SeriesDescription,
		instance.SOPClassUID,
		instance.SOPInstanceUID,
		instance.InstanceNumber,
		instance.TransferSyntaxUID,
		instance.TransferSyntax,
		instance.StudyID,
		instance.BodyPartExamined,
		instance.ReferringPhysicianName,
		instance.PerformingPhysicianName,
		instance.ImportedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert instance: %w", err)
	}
	if err := replaceInstancePatientSoundex(ctx, tx, instance.SHA256, soundexTokenCodes(instance.PatientName)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert instance: %w", err)
	}
	committed = true
	return nil
}

func (c *Catalog) replaceInstance(ctx context.Context, oldSHA string, instance Instance) error {
	if err := c.upsertInstance(ctx, instance); err != nil {
		return err
	}
	if oldSHA == "" || oldSHA == instance.SHA256 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace instance: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM instance_patient_soundex WHERE instance_sha256 = ?`, oldSHA); err != nil {
		return fmt.Errorf("delete replaced instance soundex: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM instances WHERE sha256 = ?`, oldSHA); err != nil {
		return fmt.Errorf("delete replaced instance: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace instance: %w", err)
	}
	committed = true
	return nil
}

func replaceInstancePatientSoundex(ctx context.Context, tx *sql.Tx, sha string, codes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM instance_patient_soundex WHERE instance_sha256 = ?`, sha); err != nil {
		return fmt.Errorf("delete instance patient soundex: %w", err)
	}
	for _, code := range codes {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO instance_patient_soundex(instance_sha256, code) VALUES (?, ?)`, sha, code); err != nil {
			return fmt.Errorf("insert instance patient soundex: %w", err)
		}
	}
	return nil
}

func (c *Catalog) backfillInstancePatientSoundex() error {
	rows, err := c.db.Query(`SELECT sha256, patient_name FROM instances WHERE patient_name <> ''`)
	if err != nil {
		return fmt.Errorf("query patient soundex backfill rows: %w", err)
	}
	type row struct {
		sha         string
		patientName string
	}
	var rowsToBackfill []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.sha, &item.patientName); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan patient soundex backfill row: %w", err)
		}
		rowsToBackfill = append(rowsToBackfill, item)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close patient soundex backfill rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate patient soundex backfill rows: %w", err)
	}
	if len(rowsToBackfill) == 0 {
		return nil
	}

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("begin patient soundex backfill: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, item := range rowsToBackfill {
		for _, code := range soundexTokenCodes(item.patientName) {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO instance_patient_soundex(instance_sha256, code) VALUES (?, ?)`, item.sha, code); err != nil {
				return fmt.Errorf("insert patient soundex backfill row: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit patient soundex backfill: %w", err)
	}
	committed = true
	return nil
}

// backfillInstanceExtraTags populates study_id, body_part_examined,
// referring_physician_name and performing_physician_name for rows imported
// before those columns existed. It is fully best-effort: rows whose stored file
// is missing or fails to parse are skipped, and any error short-circuits the
// pass without failing catalog open.
func (c *Catalog) backfillInstanceExtraTags() {
	const maxBackfillRows = 5000
	rows, err := c.db.Query(`
SELECT sha256, stored_path
FROM instances
WHERE stored_path <> ''
  AND study_id = ''
  AND body_part_examined = ''
  AND referring_physician_name = ''
  AND performing_physician_name = ''
LIMIT ?`, maxBackfillRows)
	if err != nil {
		return
	}
	type row struct {
		sha        string
		storedPath string
	}
	var rowsToBackfill []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.sha, &item.storedPath); err != nil {
			_ = rows.Close()
			return
		}
		rowsToBackfill = append(rowsToBackfill, item)
	}
	if err := rows.Close(); err != nil {
		return
	}
	if err := rows.Err(); err != nil {
		return
	}
	if len(rowsToBackfill) == 0 {
		return
	}

	tx, err := c.db.Begin()
	if err != nil {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, item := range rowsToBackfill {
		summary, err := dicominspect.InspectFile(item.storedPath, dicominspect.DefaultOptions())
		if err != nil {
			continue
		}
		if summary.StudyID == "" && summary.BodyPartExamined == "" &&
			summary.ReferringPhysicianName == "" && summary.PerformingPhysicianName == "" {
			continue
		}
		if _, err := tx.Exec(`
UPDATE instances
SET study_id = ?, body_part_examined = ?, referring_physician_name = ?, performing_physician_name = ?
WHERE sha256 = ?`,
			summary.StudyID,
			summary.BodyPartExamined,
			summary.ReferringPhysicianName,
			summary.PerformingPhysicianName,
			item.sha,
		); err != nil {
			return
		}
	}
	if err := tx.Commit(); err != nil {
		return
	}
	committed = true
}
