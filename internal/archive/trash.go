package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const trashManifestFileName = "manifest.json"

var ErrStudyNotFound = errors.New("study not found")

type TrashManifestObject struct {
	SHA256         string `json:"sha256"`
	SOPInstanceUID string `json:"sopInstanceUID"`
	OriginalPath   string `json:"originalPath"`
}

type TrashManifest struct {
	StudyInstanceUID string                `json:"studyInstanceUID"`
	PatientName      string                `json:"patientName"`
	PatientID        string                `json:"patientID"`
	StudyDate        string                `json:"studyDate"`
	StudyDescription string                `json:"studyDescription"`
	Modalities       string                `json:"modalities"`
	SeriesCount      int                   `json:"seriesCount"`
	InstanceCount    int                   `json:"instanceCount"`
	TrashedAt        string                `json:"trashedAt"`
	DeletedCount     int                   `json:"deletedCount"`
	Objects          []TrashManifestObject `json:"objects"`
}

type TrashEntry struct {
	StudyInstanceUID string `json:"studyInstanceUID"`
	PatientName      string `json:"patientName"`
	PatientID        string `json:"patientID"`
	StudyDate        string `json:"studyDate"`
	StudyDescription string `json:"studyDescription"`
	Modalities       string `json:"modalities"`
	SeriesCount      int    `json:"seriesCount"`
	InstanceCount    int    `json:"instanceCount"`
	TrashedAt        string `json:"trashedAt"`
	DeletedCount     int    `json:"deletedCount"`
}

type PurgeExpiredTrashReport struct {
	Scanned int      `json:"scanned"`
	Purged  int      `json:"purged"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

func validateStudyUID(studyInstanceUID string) (string, error) {
	uid := strings.TrimSpace(studyInstanceUID)
	if uid == "" {
		return "", errors.New("study instance UID is required")
	}
	return uid, nil
}

func (c *Catalog) TrashStudy(ctx context.Context, studyInstanceUID string) (int, error) {
	uid, err := validateStudyUID(studyInstanceUID)
	if err != nil {
		return 0, err
	}
	studyInstanceUID = uid
	instances, err := c.InstancesForStudy(ctx, studyInstanceUID)
	if err != nil {
		return 0, err
	}
	if len(instances) == 0 {
		return 0, fmt.Errorf("%w: %q", ErrStudyNotFound, studyInstanceUID)
	}
	paths := make([]string, 0, len(instances))
	for _, instance := range instances {
		paths = append(paths, instance.StoredPath)
	}
	if err := c.validateStoredObjectPaths(paths); err != nil {
		return 0, err
	}

	trashPath := c.trashPathForStudy(studyInstanceUID)
	if _, err := os.Stat(trashPath); err == nil {
		return 0, fmt.Errorf("trash entry already exists for study %q", studyInstanceUID)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("stat trash entry: %w", err)
	}
	objectsPath := c.trashObjectsPath(studyInstanceUID)
	if err := os.MkdirAll(objectsPath, 0o755); err != nil {
		return 0, fmt.Errorf("create trash object directory: %w", err)
	}
	manifest := trashManifestFromInstances(studyInstanceUID, instances)
	for _, instance := range instances {
		if err := copyFile(filepath.Join(objectsPath, filepath.Base(instance.StoredPath)), instance.StoredPath); err != nil {
			_ = os.RemoveAll(trashPath)
			return 0, fmt.Errorf("copy object to trash: %w", err)
		}
	}
	if err := writeTrashManifest(filepath.Join(trashPath, trashManifestFileName), manifest); err != nil {
		_ = os.RemoveAll(trashPath)
		return 0, err
	}
	deleted, err := c.DeleteStudy(ctx, studyInstanceUID)
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (c *Catalog) ListTrash(ctx context.Context) ([]TrashEntry, error) {
	entries, err := os.ReadDir(c.trashDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read trash directory: %w", err)
	}
	var result []TrashEntry
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		manifest, err := readTrashManifest(filepath.Join(c.trashDir, entry.Name(), trashManifestFileName))
		if err != nil {
			return nil, err
		}
		result = append(result, trashEntryFromManifest(manifest))
	}
	sort.Slice(result, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, result[i].TrashedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, result[j].TrashedAt)
		if leftErr == nil && rightErr == nil {
			return left.After(right)
		}
		return result[i].TrashedAt > result[j].TrashedAt
	})
	return result, nil
}

func (c *Catalog) RestoreStudy(ctx context.Context, studyInstanceUID string) (ImportReport, error) {
	uid, err := validateStudyUID(studyInstanceUID)
	if err != nil {
		return ImportReport{}, err
	}
	studyInstanceUID = uid
	trashPath := c.trashPathForStudy(studyInstanceUID)
	objectsPath := c.trashObjectsPath(studyInstanceUID)
	if _, err := readTrashManifest(filepath.Join(trashPath, trashManifestFileName)); err != nil {
		return ImportReport{}, err
	}
	report, err := c.ImportPathWithOptions(ctx, objectsPath, ImportOptions{})
	if err != nil {
		return report, err
	}
	if report.InvalidFiles > 0 {
		return report, fmt.Errorf("restore study from trash had %d invalid files", report.InvalidFiles)
	}
	if err := os.RemoveAll(trashPath); err != nil {
		return report, fmt.Errorf("remove restored trash entry: %w", err)
	}
	return report, nil
}

func (c *Catalog) PurgeStudy(ctx context.Context, studyInstanceUID string) error {
	uid, err := validateStudyUID(studyInstanceUID)
	if err != nil {
		return err
	}
	studyInstanceUID = uid
	if err := ctx.Err(); err != nil {
		return err
	}
	trashPath := c.trashPathForStudy(studyInstanceUID)
	if _, err := readTrashManifest(filepath.Join(trashPath, trashManifestFileName)); err != nil {
		return err
	}
	if err := os.RemoveAll(trashPath); err != nil {
		return fmt.Errorf("purge trash entry: %w", err)
	}
	return nil
}

func (c *Catalog) PurgeExpiredTrash(ctx context.Context, cutoff time.Time) (PurgeExpiredTrashReport, error) {
	entries, err := os.ReadDir(c.trashDir)
	if errors.Is(err, os.ErrNotExist) {
		return PurgeExpiredTrashReport{}, nil
	}
	if err != nil {
		return PurgeExpiredTrashReport{}, fmt.Errorf("read trash directory: %w", err)
	}
	var report PurgeExpiredTrashReport
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !entry.IsDir() {
			continue
		}
		report.Scanned++
		trashPath := filepath.Join(c.trashDir, entry.Name())
		manifest, err := readTrashManifest(filepath.Join(trashPath, trashManifestFileName))
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			report.Skipped++
			continue
		}
		trashedAt, err := time.Parse(time.RFC3339Nano, manifest.TrashedAt)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("parse trash timestamp for %s: %v", manifest.StudyInstanceUID, err))
			report.Skipped++
			continue
		}
		if !trashedAt.Before(cutoff) {
			report.Skipped++
			continue
		}
		if err := os.RemoveAll(trashPath); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("purge trash entry %s: %v", manifest.StudyInstanceUID, err))
			report.Skipped++
			continue
		}
		report.Purged++
	}
	return report, nil
}

func (c *Catalog) trashPathForStudy(studyInstanceUID string) string {
	return filepath.Join(c.trashDir, url.PathEscape(studyInstanceUID))
}

func (c *Catalog) trashObjectsPath(studyInstanceUID string) string {
	return filepath.Join(c.trashPathForStudy(studyInstanceUID), "objects")
}

func trashManifestFromInstances(studyInstanceUID string, instances []Instance) TrashManifest {
	first := instances[0]
	series := map[string]bool{}
	modalities := map[string]bool{}
	objects := make([]TrashManifestObject, 0, len(instances))
	for _, instance := range instances {
		seriesUID := instance.SeriesInstanceUID
		if seriesUID == "" {
			seriesUID = missingUIDSentinel
		}
		series[seriesUID] = true
		if instance.Modality != "" {
			modalities[instance.Modality] = true
		}
		objects = append(objects, TrashManifestObject{
			SHA256:         instance.SHA256,
			SOPInstanceUID: instance.SOPInstanceUID,
			OriginalPath:   instance.StoredPath,
		})
	}
	return TrashManifest{
		StudyInstanceUID: studyInstanceUID,
		PatientName:      first.PatientName,
		PatientID:        first.PatientID,
		StudyDate:        first.StudyDate,
		StudyDescription: first.StudyDescription,
		Modalities:       strings.Join(sortedKeys(modalities), ","),
		SeriesCount:      len(series),
		InstanceCount:    len(instances),
		TrashedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		DeletedCount:     len(instances),
		Objects:          objects,
	}
}

func trashEntryFromManifest(manifest TrashManifest) TrashEntry {
	return TrashEntry{
		StudyInstanceUID: manifest.StudyInstanceUID,
		PatientName:      manifest.PatientName,
		PatientID:        manifest.PatientID,
		StudyDate:        manifest.StudyDate,
		StudyDescription: manifest.StudyDescription,
		Modalities:       manifest.Modalities,
		SeriesCount:      manifest.SeriesCount,
		InstanceCount:    manifest.InstanceCount,
		TrashedAt:        manifest.TrashedAt,
		DeletedCount:     manifest.DeletedCount,
	}
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeTrashManifest(path string, manifest TrashManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trash manifest: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.json")
	if err != nil {
		return fmt.Errorf("create temporary trash manifest: %w", err)
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
		return fmt.Errorf("write trash manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close trash manifest: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace trash manifest: %w", err)
	}
	removeTemp = false
	return nil
}

func readTrashManifest(path string) (TrashManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TrashManifest{}, fmt.Errorf("read trash manifest: %w", err)
	}
	var manifest TrashManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return TrashManifest{}, fmt.Errorf("parse trash manifest: %w", err)
	}
	return manifest, nil
}

func copyFile(dstPath string, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close copied file: %w", err)
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set copied file mode: %w", err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("replace copied file: %w", err)
	}
	removeTemp = false
	return nil
}
