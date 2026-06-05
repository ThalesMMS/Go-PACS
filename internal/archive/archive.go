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

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
)

type Catalog struct {
	db       *sql.DB
	rootDir  string
	storeDir string
}

const CatalogSchemaVersion = 1

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

type ImportOptions struct {
	Limits ImportLimits
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
	ImportedAt        time.Time
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
	SeriesCount      int
	InstanceCount    int
	ImportedAt       time.Time
}

type Series struct {
	StudyInstanceUID  string
	SeriesInstanceUID string
	Modality          string
	SeriesNumber      string
	SeriesDescription string
	InstanceCount     int
	ImportedAt        time.Time
}

type StudyFilters struct {
	PatientName      string
	PatientID        string
	AccessionNumber  string
	StudyDescription string
	StudyDateFrom    string
	StudyDateTo      string
	ImportedAtFrom   string
	ImportedAtTo     string
	Modalities       []string
	SourcePath       string
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
	db, err := sql.Open("sqlite3", filepath.Join(rootDir, "catalog.db"))
	if err != nil {
		return nil, fmt.Errorf("open catalog database: %w", err)
	}
	c := &Catalog{db: db, rootDir: rootDir, storeDir: storeDir}
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
			continue
		}
		if seen[safeName] {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: "duplicate ZIP entry path"})
			continue
		}
		seen[safeName] = true
		if rejectImportPathLength(report, safeName, opts.Limits.MaxImportPathLength) {
			report.ScannedFiles++
			continue
		}

		entrySize := zipEntrySize(entry)
		if opts.Limits.MaxZipEntryBytes > 0 && entrySize > opts.Limits.MaxZipEntryBytes {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{
				Path:   fmt.Sprintf("zip://%s!%s", zipPath, safeName),
				Reason: fmt.Sprintf("ZIP entry size %d exceeds limit %d", entrySize, opts.Limits.MaxZipEntryBytes),
			})
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
				return *report, nil
			}
		}

		tempPath, extracted, err := c.extractZipEntry(entry, zipReadLimit(opts.Limits, extractedBytes))
		if err != nil {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{Path: sourcePath, Reason: err.Error()})
			continue
		}
		if opts.Limits.MaxZipEntryBytes > 0 && extracted > opts.Limits.MaxZipEntryBytes {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{
				Path:   fmt.Sprintf("zip://%s!%s", zipPath, safeName),
				Reason: limitExceededReason("max_zip_entry_bytes", extracted, opts.Limits.MaxZipEntryBytes),
			})
			_ = os.Remove(tempPath)
			continue
		}
		if opts.Limits.MaxZipTotalBytes > 0 && extractedBytes+extracted > opts.Limits.MaxZipTotalBytes {
			report.ScannedFiles++
			report.Rejections = append(report.Rejections, Rejection{
				Path:   fmt.Sprintf("zip://%s!%s", zipPath, safeName),
				Reason: limitExceededReason("max_zip_total_bytes", extractedBytes+extracted, opts.Limits.MaxZipTotalBytes),
			})
			_ = os.Remove(tempPath)
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
	if sourcePath == "" {
		sourcePath = path
	}
	var report ImportReport
	c.importFileWithSource(ctx, path, sourcePath, &report, ImportOptions{})
	return report, nil
}

func (c *Catalog) ImportObject(ctx context.Context, sourcePath string, dataset *object.Object, syntax transfer.Syntax) (ImportReport, error) {
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

	report, err := c.ImportPart10File(ctx, tempPath, sourcePath)
	if err != nil {
		return report, err
	}
	committed = true
	_ = os.Remove(tempPath)
	return report, nil
}

func (c *Catalog) Studies(ctx context.Context) ([]Study, error) {
	return c.StudiesWithFilters(ctx, StudyFilters{})
}

func (c *Catalog) StudiesWithFilters(ctx context.Context, filters StudyFilters) ([]Study, error) {
	where, args := studyFilterWhere(filters)
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
  COUNT(DISTINCT COALESCE(NULLIF(series_instance_uid, ''), '(missing)')) AS series_count,
  COUNT(*) AS instance_count,
  COALESCE(MAX(imported_at), '') AS imported_at
FROM instances
`
	if where != "" {
		query += "WHERE " + where + "\n"
	}
	query += `
GROUP BY COALESCE(NULLIF(study_instance_uid, ''), '(missing)')
ORDER BY imported_at DESC, patient_name ASC`

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query studies: %w", err)
	}
	defer rows.Close()

	var studies []Study
	for rows.Next() {
		study, err := scanStudy(rows)
		if err != nil {
			return nil, err
		}
		studies = append(studies, study)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate studies: %w", err)
	}
	return studies, nil
}

func studyFilterWhere(filters StudyFilters) (string, []any) {
	var clauses []string
	var args []any
	addLike := func(column string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		clauses = append(clauses, "LOWER("+column+") LIKE ?")
		args = append(args, "%"+strings.ToLower(value)+"%")
	}
	addLike("patient_name", filters.PatientName)
	addLike("patient_id", filters.PatientID)
	addLike("accession_number", filters.AccessionNumber)
	addLike("study_description", filters.StudyDescription)
	addLike("source_path", filters.SourcePath)

	if value := strings.TrimSpace(filters.StudyDateFrom); value != "" {
		clauses = append(clauses, "study_date >= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.StudyDateTo); value != "" {
		clauses = append(clauses, "study_date <= ?")
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
		modality = strings.ToUpper(strings.TrimSpace(modality))
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
		clauses = append(clauses, "UPPER(modality) IN ("+strings.Join(placeholders, ", ")+")")
	}

	return strings.Join(clauses, " AND "), args
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
  study_date, study_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax, imported_at
FROM instances
WHERE COALESCE(NULLIF(study_instance_uid, ''), '(missing)') = ?
ORDER BY series_instance_uid ASC, sop_instance_uid ASC, imported_at ASC`, studyInstanceUID)
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
	clauses := []string{"COALESCE(NULLIF(study_instance_uid, ''), '(missing)') = ?"}
	args := []any{studyInstanceUID}
	addLike := func(column string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		clauses = append(clauses, "LOWER("+column+") LIKE ?")
		args = append(args, "%"+strings.ToLower(value)+"%")
	}
	addLike("series_number", filters.SeriesNumber)
	addLike("series_description", filters.SeriesDescription)
	if modality := strings.ToUpper(strings.TrimSpace(filters.Modality)); modality != "" {
		clauses = append(clauses, "UPPER(modality) = ?")
		args = append(args, modality)
	}
	return strings.Join(clauses, " AND "), args
}

func (c *Catalog) InstancesForSeries(ctx context.Context, seriesInstanceUID string) ([]Instance, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT
  sha256, stored_path, source_path, file_size,
  patient_name, patient_id, patient_birth_date, institution_name,
  study_date, study_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax, imported_at
FROM instances
WHERE COALESCE(NULLIF(series_instance_uid, ''), '(missing)') = ?
ORDER BY
  CASE WHEN instance_number GLOB '[0-9]*' THEN CAST(instance_number AS INTEGER) END ASC,
  instance_number ASC,
  sop_instance_uid ASC,
  imported_at ASC`, seriesInstanceUID)
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
  study_date, study_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax, imported_at
FROM instances
WHERE COALESCE(NULLIF(sop_instance_uid, ''), '(missing)') = ?
ORDER BY imported_at ASC, sha256 ASC
LIMIT 1`, sopInstanceUID)
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
CREATE INDEX IF NOT EXISTS idx_instances_imported_at ON instances(imported_at);
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
	} {
		if err := c.ensureInstanceColumn(column.name, column.definition); err != nil {
			return err
		}
	}
	if err := c.recordSchemaMigration(CatalogSchemaVersion, "create_instances_and_metadata_columns"); err != nil {
		return err
	}
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

	instance := Instance{
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
		ImportedAt:        time.Now().UTC(),
	}
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

func (c *Catalog) upsertInstance(ctx context.Context, instance Instance) error {
	_, err := c.db.ExecContext(ctx, `
INSERT INTO instances (
  sha256, stored_path, source_path, file_size,
  patient_name, patient_id, patient_birth_date, institution_name,
  study_date, study_time, study_description, modality, accession_number,
  study_instance_uid, series_instance_uid, series_number, series_description,
  sop_class_uid, sop_instance_uid, instance_number,
  transfer_syntax_uid, transfer_syntax, imported_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		instance.ImportedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert instance: %w", err)
	}
	return nil
}
