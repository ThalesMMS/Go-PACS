package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

func TestImportPathStoresDicomAndListsStudies(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "one.dcm"), testPart10File(t, "ARCHIVE^ONE", "A001", "CT", "1.2.3.study"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "not-dicom.txt"), []byte("not dicom"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.ImportPath(ctx, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 2 {
		t.Fatalf("ScannedFiles = %d, want 2", report.ScannedFiles)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}
	if report.InvalidFiles != 1 {
		t.Fatalf("InvalidFiles = %d, want 1", report.InvalidFiles)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("len(studies) = %d, want 1", len(studies))
	}
	if studies[0].PatientName != "ARCHIVE^ONE" {
		t.Fatalf("PatientName = %q, want ARCHIVE^ONE", studies[0].PatientName)
	}
	if studies[0].PatientBirthDate != "19700102" {
		t.Fatalf("PatientBirthDate = %q, want 19700102", studies[0].PatientBirthDate)
	}
	if studies[0].InstitutionName != "General Hospital" {
		t.Fatalf("InstitutionName = %q, want General Hospital", studies[0].InstitutionName)
	}
	if studies[0].Modalities != "CT" {
		t.Fatalf("Modalities = %q, want CT", studies[0].Modalities)
	}
	if studies[0].StudyTime != "134501" {
		t.Fatalf("StudyTime = %q, want 134501", studies[0].StudyTime)
	}
	if studies[0].SeriesCount != 1 {
		t.Fatalf("SeriesCount = %d, want 1", studies[0].SeriesCount)
	}
	if studies[0].InstanceCount != 1 {
		t.Fatalf("InstanceCount = %d, want 1", studies[0].InstanceCount)
	}
}

func TestCatalogTracksSchemaMigrations(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := catalog.SchemaMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("len(migrations) = %d, want 1", len(migrations))
	}
	if migrations[0].Version != CatalogSchemaVersion {
		t.Fatalf("migration version = %d, want %d", migrations[0].Version, CatalogSchemaVersion)
	}
	if migrations[0].Name == "" || migrations[0].AppliedAt.IsZero() {
		t.Fatalf("migration = %#v", migrations[0])
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}

	catalog, err = Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	migrations, err = catalog.SchemaMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("len(migrations) after reopen = %d, want 1", len(migrations))
	}
}

func TestStudiesWithFilters(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	alicePath := filepath.Join(sourceDir, "alice.dcm")
	bobPath := filepath.Join(sourceDir, "bob.dcm")
	if err := os.WriteFile(alicePath, testPart10File(t, "Alice^Smith", "A001", "CT", "1.2.3.alice"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bobPath, testPart10File(t, "Bob^Jones", "B001", "MR", "1.2.3.bob"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	studies, err := catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.alice" {
		t.Fatalf("patient filter studies = %#v", studies)
	}

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{Modalities: []string{"mr"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.bob" {
		t.Fatalf("modality filter studies = %#v", studies)
	}

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{StudyDateFrom: "20260605"})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("date filter len = %d, want 0", len(studies))
	}

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{SourcePath: filepath.Base(alicePath)})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.alice" {
		t.Fatalf("source path filter studies = %#v", studies)
	}

	futureImportedAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{ImportedAtFrom: futureImportedAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("imported-at future lower-bound len = %d, want 0", len(studies))
	}

	pastImportedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{ImportedAtFrom: pastImportedAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 2 {
		t.Fatalf("imported-at past lower-bound len = %d, want 2", len(studies))
	}
	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{ImportedAtTo: pastImportedAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("imported-at past upper-bound len = %d, want 0", len(studies))
	}
}

func TestCatalogListsSeriesAndInstancesForStudy(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	writeFile := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(sourceDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	studyUID := "1.2.826.0.1"
	seriesOneUID := "1.2.826.0.1.1"
	seriesTwoUID := "1.2.826.0.1.2"
	writeFile("s1i1.dcm", testPart10FileWithDetails(t, "SERIES^PATIENT", "SP001", "CT", studyUID, seriesOneUID, "1.2.826.0.1.1.1", "1", "Axial", "1"))
	writeFile("s1i2.dcm", testPart10FileWithDetails(t, "SERIES^PATIENT", "SP001", "CT", studyUID, seriesOneUID, "1.2.826.0.1.1.2", "1", "Axial", "2"))
	writeFile("s2i1.dcm", testPart10FileWithDetails(t, "SERIES^PATIENT", "SP001", "MR", studyUID, seriesTwoUID, "1.2.826.0.1.2.1", "2", "Coronal", "1"))

	report, err := catalog.ImportPath(ctx, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 3 {
		t.Fatalf("StoredFiles = %d, want 3", report.StoredFiles)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("len(studies) = %d, want 1", len(studies))
	}
	if studies[0].SeriesCount != 2 {
		t.Fatalf("SeriesCount = %d, want 2", studies[0].SeriesCount)
	}
	if studies[0].InstanceCount != 3 {
		t.Fatalf("InstanceCount = %d, want 3", studies[0].InstanceCount)
	}

	series, err := catalog.SeriesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("len(series) = %d, want 2", len(series))
	}
	if series[0].SeriesInstanceUID != seriesOneUID || series[0].SeriesDescription != "Axial" || series[0].InstanceCount != 2 {
		t.Fatalf("series[0] = %#v", series[0])
	}
	if series[1].SeriesInstanceUID != seriesTwoUID || series[1].Modality != "MR" || series[1].InstanceCount != 1 {
		t.Fatalf("series[1] = %#v", series[1])
	}

	filteredSeries, err := catalog.SeriesForStudyWithFilters(ctx, studyUID, SeriesFilters{Modality: "mr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredSeries) != 1 || filteredSeries[0].SeriesInstanceUID != seriesTwoUID {
		t.Fatalf("modality filtered series = %#v", filteredSeries)
	}
	filteredSeries, err = catalog.SeriesForStudyWithFilters(ctx, studyUID, SeriesFilters{SeriesDescription: "axi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredSeries) != 1 || filteredSeries[0].SeriesInstanceUID != seriesOneUID {
		t.Fatalf("description filtered series = %#v", filteredSeries)
	}
	filteredSeries, err = catalog.SeriesForStudyWithFilters(ctx, studyUID, SeriesFilters{SeriesNumber: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredSeries) != 1 || filteredSeries[0].SeriesInstanceUID != seriesTwoUID {
		t.Fatalf("number filtered series = %#v", filteredSeries)
	}

	instances, err := catalog.InstancesForSeries(ctx, seriesOneUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("len(instances) = %d, want 2", len(instances))
	}
	if instances[0].SOPInstanceUID != "1.2.826.0.1.1.1" || instances[0].InstanceNumber != "1" {
		t.Fatalf("instances[0] = %#v", instances[0])
	}
	if instances[1].SOPInstanceUID != "1.2.826.0.1.1.2" || instances[1].InstanceNumber != "2" {
		t.Fatalf("instances[1] = %#v", instances[1])
	}

	instance, err := catalog.InstanceBySOPInstanceUID(ctx, "1.2.826.0.1.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if instance.SeriesInstanceUID != seriesOneUID || instance.InstanceNumber != "2" {
		t.Fatalf("InstanceBySOPInstanceUID = %#v", instance)
	}

	studyInstances, err := catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(studyInstances) != 3 {
		t.Fatalf("len(studyInstances) = %d, want 3", len(studyInstances))
	}
}

func TestImportPathImportsZipEntriesAndContinuesAfterInvalidDicom(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	zipPath := filepath.Join(t.TempDir(), "incoming.zip")
	writeZip(t, zipPath, []zipEntry{
		{Name: "valid/no-extension", Data: testPart10File(t, "ZIP^PATIENT", "Z001", "CT", "1.2.3.zip")},
		{Name: "invalid.txt", Data: []byte("not dicom")},
	})

	report, err := catalog.ImportPath(ctx, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 2 {
		t.Fatalf("ScannedFiles = %d, want 2", report.ScannedFiles)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}
	if report.InvalidFiles != 1 {
		t.Fatalf("InvalidFiles = %d, want 1", report.InvalidFiles)
	}

	instances, err := catalog.InstancesForStudy(ctx, "1.2.3.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].SourcePath != "zip://"+zipPath+"!valid/no-extension" {
		t.Fatalf("SourcePath = %q", instances[0].SourcePath)
	}
}

func TestImportPathWithOptionsRejectsOversizedFile(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	oversizedPath := filepath.Join(sourceDir, "too-big.dcm")
	if err := os.WriteFile(oversizedPath, bytes.Repeat([]byte{0}, 17), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.ImportPathWithOptions(ctx, sourceDir, ImportOptions{
		Limits: ImportLimits{MaxFileImportBytes: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 1 {
		t.Fatalf("ScannedFiles = %d, want 1", report.ScannedFiles)
	}
	if report.StoredFiles != 0 {
		t.Fatalf("StoredFiles = %d, want 0", report.StoredFiles)
	}
	if report.InvalidFiles != 0 {
		t.Fatalf("InvalidFiles = %d, want 0", report.InvalidFiles)
	}
	if !hasRejectionContaining(report, "max_file_import_bytes exceeded: 17 > 16") {
		t.Fatalf("Rejections = %#v", report.Rejections)
	}
}

func TestImportPathWithOptionsEnforcesTotalFileLimit(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "a.dcm"), []byte("not dicom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "b.dcm"), []byte("not dicom"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.ImportPathWithOptions(ctx, sourceDir, ImportOptions{
		Limits: ImportLimits{MaxImportTotalFiles: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 1 {
		t.Fatalf("ScannedFiles = %d, want 1", report.ScannedFiles)
	}
	if report.InvalidFiles != 1 {
		t.Fatalf("InvalidFiles = %d, want 1", report.InvalidFiles)
	}
	if !hasRejectionContaining(report, "max_import_total_files exceeded: limit is 1") {
		t.Fatalf("Rejections = %#v", report.Rejections)
	}
}

func TestImportPathWithOptionsRejectsOverlongPath(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "too-long.dcm")
	if err := os.WriteFile(sourcePath, testPart10File(t, "LONG^PATH", "LP001", "CT", "1.2.3.longpath"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.ImportPathWithOptions(ctx, sourceDir, ImportOptions{
		Limits: ImportLimits{MaxImportPathLength: len(sourcePath) - 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 0 {
		t.Fatalf("StoredFiles = %d, want 0", report.StoredFiles)
	}
	if report.InvalidFiles != 0 {
		t.Fatalf("InvalidFiles = %d, want 0", report.InvalidFiles)
	}
	if !hasRejectionContaining(report, "max_import_path_length exceeded") {
		t.Fatalf("Rejections = %#v", report.Rejections)
	}
}

func TestImportPathWithOptionsHonorsDirectoryDepthLimit(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	nestedDir := filepath.Join(sourceDir, "level1", "level2")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "root.dcm"), testPart10File(t, "ROOT^DEPTH", "RD001", "CT", "1.2.3.depth"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "nested.dcm"), testPart10File(t, "NESTED^DEPTH", "ND001", "MR", "1.2.3.depth.nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.ImportPathWithOptions(ctx, sourceDir, ImportOptions{
		Limits: ImportLimits{MaxImportDirectoryDepth: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 1 {
		t.Fatalf("ScannedFiles = %d, want 1", report.ScannedFiles)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}
	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.depth" {
		t.Fatalf("studies = %#v", studies)
	}
}

func TestImportPathWithOptionsEnforcesZipLimits(t *testing.T) {
	ctx := context.Background()

	t.Run("entry size", func(t *testing.T) {
		catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
		if err != nil {
			t.Fatal(err)
		}
		defer catalog.Close()

		zipPath := filepath.Join(t.TempDir(), "large-entry.zip")
		writeZip(t, zipPath, []zipEntry{{Name: "large.dcm", Data: []byte("abcd")}})

		report, err := catalog.ImportPathWithOptions(ctx, zipPath, ImportOptions{
			Limits: ImportLimits{MaxZipEntryBytes: 3},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.ScannedFiles != 1 {
			t.Fatalf("ScannedFiles = %d, want 1", report.ScannedFiles)
		}
		if report.InvalidFiles != 0 {
			t.Fatalf("InvalidFiles = %d, want 0", report.InvalidFiles)
		}
		if !hasRejectionContaining(report, "ZIP entry size 4 exceeds limit 3") {
			t.Fatalf("Rejections = %#v", report.Rejections)
		}
	})

	t.Run("total size", func(t *testing.T) {
		catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
		if err != nil {
			t.Fatal(err)
		}
		defer catalog.Close()

		zipPath := filepath.Join(t.TempDir(), "total-limit.zip")
		writeZip(t, zipPath, []zipEntry{
			{Name: "first.dcm", Data: []byte("abcd")},
			{Name: "second.dcm", Data: []byte("efgh")},
		})

		report, err := catalog.ImportPathWithOptions(ctx, zipPath, ImportOptions{
			Limits: ImportLimits{MaxZipTotalBytes: 5},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.ScannedFiles != 2 {
			t.Fatalf("ScannedFiles = %d, want 2", report.ScannedFiles)
		}
		if report.InvalidFiles != 1 {
			t.Fatalf("InvalidFiles = %d, want 1", report.InvalidFiles)
		}
		if !hasRejectionContaining(report, "ZIP total extracted bytes limit exceeded") {
			t.Fatalf("Rejections = %#v", report.Rejections)
		}
	})

	t.Run("entry count", func(t *testing.T) {
		catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
		if err != nil {
			t.Fatal(err)
		}
		defer catalog.Close()

		zipPath := filepath.Join(t.TempDir(), "many.zip")
		writeZip(t, zipPath, []zipEntry{
			{Name: "first.dcm", Data: []byte("ab")},
			{Name: "second.dcm", Data: []byte("cd")},
		})

		report, err := catalog.ImportPathWithOptions(ctx, zipPath, ImportOptions{
			Limits: ImportLimits{MaxZipEntryCount: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.ScannedFiles != 1 {
			t.Fatalf("ScannedFiles = %d, want 1", report.ScannedFiles)
		}
		if report.InvalidFiles != 1 {
			t.Fatalf("InvalidFiles = %d, want 1", report.InvalidFiles)
		}
		if !hasRejectionContaining(report, "ZIP entry count limit exceeded") {
			t.Fatalf("Rejections = %#v", report.Rejections)
		}
	})
}

func TestImportPathRejectsUnsafeZipEntries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, err := Open(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	zipPath := filepath.Join(root, "slip.zip")
	writeZip(t, zipPath, []zipEntry{
		{Name: "../escape.dcm", Data: []byte("not dicom")},
		{Name: "/absolute.dcm", Data: []byte("not dicom")},
	})

	report, err := catalog.ImportPath(ctx, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 2 {
		t.Fatalf("ScannedFiles = %d, want 2", report.ScannedFiles)
	}
	if report.StoredFiles != 0 {
		t.Fatalf("StoredFiles = %d, want 0", report.StoredFiles)
	}
	if report.InvalidFiles != 0 {
		t.Fatalf("InvalidFiles = %d, want 0", report.InvalidFiles)
	}
	if len(report.Rejections) != 2 {
		t.Fatalf("len(Rejections) = %d, want 2", len(report.Rejections))
	}
	if _, err := os.Stat(filepath.Join(root, "escape.dcm")); !os.IsNotExist(err) {
		t.Fatalf("unsafe ZIP entry wrote escape.dcm: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "absolute.dcm")); !os.IsNotExist(err) {
		t.Fatalf("unsafe ZIP entry wrote absolute.dcm: %v", err)
	}
}

func hasRejectionContaining(report ImportReport, fragment string) bool {
	for _, rejection := range report.Rejections {
		if strings.Contains(rejection.Reason, fragment) {
			return true
		}
	}
	return false
}

func TestImportPathCountsDuplicateContent(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := filepath.Join(t.TempDir(), "one.dcm")
	if err := os.WriteFile(source, testPart10File(t, "DUP^PATIENT", "D001", "MR", "1.2.3.dup"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ImportPath(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 0 {
		t.Fatalf("StoredFiles = %d, want 0", report.StoredFiles)
	}
	if report.Duplicates != 1 {
		t.Fatalf("Duplicates = %d, want 1", report.Duplicates)
	}
}

func TestImportPathCountsDuplicateContentFromZip(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	data := testPart10File(t, "ZIP^DUP", "ZD001", "MR", "1.2.3.zipdup")
	source := filepath.Join(t.TempDir(), "one.dcm")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "dups.zip")
	writeZip(t, zipPath, []zipEntry{{Name: "one.dcm", Data: data}})
	report, err := catalog.ImportPath(ctx, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 0 {
		t.Fatalf("StoredFiles = %d, want 0", report.StoredFiles)
	}
	if report.Duplicates != 1 {
		t.Fatalf("Duplicates = %d, want 1", report.Duplicates)
	}
}

type zipEntry struct {
	Name string
	Data []byte
}

func writeZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		w, err := writer.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entry.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func testPart10File(t *testing.T, patientName, patientID, modality, studyUID string) []byte {
	t.Helper()
	return testPart10FileWithDetails(t, patientName, patientID, modality, studyUID, studyUID+".series", studyUID+".instance", "", "", "")
}

func testPart10FileWithDetails(t *testing.T, patientName, patientID, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber string) []byte {
	t.Helper()
	dataset := []core.Element{
		stringElement(core.NewTag(0x0008, 0x0016), core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
		stringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
		stringElement(core.NewTag(0x0010, 0x0010), core.VRPN, patientName),
		stringElement(core.NewTag(0x0010, 0x0020), core.VRLO, patientID),
		stringElement(core.NewTag(0x0010, 0x0030), core.VRDA, "19700102"),
		stringElement(core.NewTag(0x0008, 0x0080), core.VRLO, "General Hospital"),
		stringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		stringElement(core.NewTag(0x0008, 0x0030), core.VRTM, "134501"),
		stringElement(core.NewTag(0x0008, 0x0060), core.VRCS, modality),
		stringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
		stringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
	}
	if seriesNumber != "" {
		dataset = append(dataset, stringElement(core.NewTag(0x0020, 0x0011), core.VRIS, seriesNumber))
	}
	if seriesDescription != "" {
		dataset = append(dataset, stringElement(core.NewTag(0x0008, 0x103E), core.VRLO, seriesDescription))
	}
	if instanceNumber != "" {
		dataset = append(dataset, stringElement(core.NewTag(0x0020, 0x0013), core.VRIS, instanceNumber))
	}
	file := &object.File{
		Dataset:        object.FromElements(dataset, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func stringElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: vr},
		Value:  core.StringValue{value},
	}
}
