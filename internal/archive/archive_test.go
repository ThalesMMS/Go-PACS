package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
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

func TestImportPathWithOptionsReportsProgressAfterEachFile(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	validPath := filepath.Join(sourceDir, "valid.dcm")
	invalidPath := filepath.Join(sourceDir, "invalid.txt")
	if err := os.WriteFile(validPath, testPart10File(t, "PROGRESS^ONE", "P001", "CT", "1.2.3.progress"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidPath, []byte("not dicom"), 0o644); err != nil {
		t.Fatal(err)
	}

	var updates []ImportProgress
	report, err := catalog.ImportPathWithOptions(ctx, sourceDir, ImportOptions{
		OnProgress: func(update ImportProgress) {
			updates = append(updates, update)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ScannedFiles != 2 || report.StoredFiles != 1 || report.InvalidFiles != 1 {
		t.Fatalf("report = %#v, want one stored and one invalid file", report)
	}
	if len(updates) != 2 {
		t.Fatalf("progress updates = %d, want 2", len(updates))
	}
	updatesByPath := map[string]ImportProgress{}
	for _, update := range updates {
		updatesByPath[update.Path] = update
	}
	if _, ok := updatesByPath[validPath]; !ok {
		t.Fatalf("missing valid file progress update in %#v", updates)
	}
	if _, ok := updatesByPath[invalidPath]; !ok {
		t.Fatalf("missing invalid file progress update in %#v", updates)
	}
	final := updates[len(updates)-1]
	if final.ScannedFiles != 2 || final.StoredFiles != 1 || final.InvalidFiles != 1 {
		t.Fatalf("final progress update = %#v", final)
	}
}

func TestStudyMetadataPersistsStatusAndComments(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	studyUID := "1.2.3.study"
	if err := os.WriteFile(filepath.Join(sourceDir, "one.dcm"), testPart10File(t, "ARCHIVE^ONE", "A001", "CT", studyUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	if err := catalog.SetStudyMetadata(ctx, studyUID, StudyMetadata{Status: "Reviewed", Comments: "Discuss with surgeon"}); err != nil {
		t.Fatal(err)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("len(studies) = %d, want 1", len(studies))
	}
	if studies[0].Status != "Reviewed" || studies[0].Comments != "Discuss with surgeon" {
		t.Fatalf("study metadata = %q/%q", studies[0].Status, studies[0].Comments)
	}
}

func TestDeleteStudyRemovesInstancesMetadataAndObjectFiles(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	studyUID := "1.2.3.delete"
	otherStudyUID := "1.2.3.keep"
	files := map[string][]byte{
		"target-one.dcm": testPart10FileWithDetails(t, "DELETE^TARGET", "D001", "CT", studyUID, studyUID+".series.1", studyUID+".instance.1", "1", "Axial", "1"),
		"target-two.dcm": testPart10FileWithDetails(t, "DELETE^TARGET", "D001", "CT", studyUID, studyUID+".series.2", studyUID+".instance.2", "2", "Coronal", "2"),
		"keep.dcm":       testPart10File(t, "KEEP^OTHER", "K001", "MR", otherStudyUID),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetStudyMetadata(ctx, studyUID, StudyMetadata{Status: "Reviewed", Comments: "Delete me"}); err != nil {
		t.Fatal(err)
	}

	targetInstances, err := catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetInstances) != 2 {
		t.Fatalf("len(targetInstances) = %d, want 2", len(targetInstances))
	}
	var targetPaths []string
	for _, instance := range targetInstances {
		targetPaths = append(targetPaths, instance.StoredPath)
		if _, err := os.Stat(instance.StoredPath); err != nil {
			t.Fatalf("target object before delete: %v", err)
		}
	}
	otherInstances, err := catalog.InstancesForStudy(ctx, otherStudyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherInstances) != 1 {
		t.Fatalf("len(otherInstances) = %d, want 1", len(otherInstances))
	}
	otherPath := otherInstances[0].StoredPath

	deleted, err := catalog.DeleteStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteStudy deleted = %d, want 2", deleted)
	}

	targetInstances, err = catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetInstances) != 0 {
		t.Fatalf("target instances after delete = %#v", targetInstances)
	}
	for _, path := range targetPaths {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target object %q exists after delete: %v", path, err)
		}
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("other object should remain: %v", err)
	}
	metadata, err := catalog.StudyMetadata(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Status != "" || metadata.Comments != "" {
		t.Fatalf("metadata after delete = %#v", metadata)
	}
	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != otherStudyUID {
		t.Fatalf("studies after delete = %#v", studies)
	}
}

func TestStudyExistsDetectsImportedStudyUID(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	studyUID := "1.2.3.study"
	if err := os.WriteFile(filepath.Join(sourceDir, "one.dcm"), testPart10File(t, "ARCHIVE^ONE", "A001", "CT", studyUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	exists, err := catalog.StudyExists(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("StudyExists returned false for imported study")
	}
	missing, err := catalog.StudyExists(ctx, "9.9.9.missing")
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		t.Fatal("StudyExists returned true for missing study")
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
	if err := catalog.SetStudyMetadata(ctx, "1.2.3.alice", StudyMetadata{Status: "Interesting", Comments: "Discuss with surgeon"}); err != nil {
		t.Fatal(err)
	}

	studies, err := catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.alice" {
		t.Fatalf("patient filter studies = %#v", studies)
	}

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "alyce", PatientNameSoundex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.alice" {
		t.Fatalf("soundex patient filter studies = %#v", studies)
	}

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "alyce"})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("literal patient filter len = %d, want 0", len(studies))
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

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{HasComments: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.alice" || studies[0].Comments != "Discuss with surgeon" {
		t.Fatalf("comments filter studies = %#v", studies)
	}

	studies, err = catalog.StudiesWithFilters(ctx, StudyFilters{Status: "interesting"})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.alice" || studies[0].Status != "Interesting" {
		t.Fatalf("status filter studies = %#v", studies)
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

func TestStudiesWithFiltersByStudyDateTime(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	recentPath := filepath.Join(sourceDir, "recent.dcm")
	oldPath := filepath.Join(sourceDir, "old.dcm")
	if err := os.WriteFile(recentPath, testPart10FileWithStudyDateTime(t, "Recent^Acquired", "R001", "CT", "1.2.3.recent", "20260604", "113000"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, testPart10FileWithStudyDateTime(t, "Old^Acquired", "O001", "CT", "1.2.3.old", "20260604", "090000"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	studies, err := catalog.StudiesWithFilters(ctx, StudyFilters{StudyDateTimeFrom: "20260604110000", StudyDateTimeTo: "20260604120000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.recent" {
		t.Fatalf("study datetime filter studies = %#v", studies)
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
	writeFile("s1i1.dcm", testPart10FileWithSeriesDetails(t, "SERIES^PATIENT", "SP001", "CT", studyUID, seriesOneUID, "1.2.826.0.1.1.1", "1", "Axial", "1", "20260605", "140102"))
	writeFile("s1i2.dcm", testPart10FileWithSeriesDetails(t, "SERIES^PATIENT", "SP001", "CT", studyUID, seriesOneUID, "1.2.826.0.1.1.2", "1", "Axial", "2", "20260605", "140103"))
	writeFile("s2i1.dcm", testPart10FileWithSeriesDetails(t, "SERIES^PATIENT", "SP001", "MR", studyUID, seriesTwoUID, "1.2.826.0.1.2.1", "2", "Coronal", "1", "20260606", "090001"))

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
	if series[0].SeriesDate != "20260605" || series[0].SeriesTime != "140103" {
		t.Fatalf("series[0] date/time = %q/%q, want 20260605/140103", series[0].SeriesDate, series[0].SeriesTime)
	}
	if series[1].SeriesInstanceUID != seriesTwoUID || series[1].Modality != "MR" || series[1].InstanceCount != 1 {
		t.Fatalf("series[1] = %#v", series[1])
	}
	if series[1].SeriesDate != "20260606" || series[1].SeriesTime != "090001" {
		t.Fatalf("series[1] date/time = %q/%q, want 20260606/090001", series[1].SeriesDate, series[1].SeriesTime)
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

func testPart10FileWithStudyDateTime(t *testing.T, patientName, patientID, modality, studyUID, studyDate, studyTime string) []byte {
	t.Helper()
	return testPart10FileWithStudyDateTimeAndSeries(t, patientName, patientID, modality, studyUID, studyUID+".series", studyUID+".instance", "", "", "", studyDate, studyTime, "", "")
}

func testPart10FileWithDetails(t *testing.T, patientName, patientID, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber string) []byte {
	return testPart10FileWithSeriesDetails(t, patientName, patientID, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber, "", "")
}

func testPart10FileWithSeriesDetails(t *testing.T, patientName, patientID, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber, seriesDate, seriesTime string) []byte {
	return testPart10FileWithStudyDateTimeAndSeries(t, patientName, patientID, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber, "20260604", "134501", seriesDate, seriesTime)
}

func testPart10FileWithStudyDateTimeAndSeries(t *testing.T, patientName, patientID, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber, studyDate, studyTime, seriesDate, seriesTime string) []byte {
	t.Helper()
	dataset := []core.Element{
		stringElement(core.NewTag(0x0008, 0x0016), core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
		stringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
		stringElement(core.NewTag(0x0010, 0x0010), core.VRPN, patientName),
		stringElement(core.NewTag(0x0010, 0x0020), core.VRLO, patientID),
		stringElement(core.NewTag(0x0010, 0x0030), core.VRDA, "19700102"),
		stringElement(core.NewTag(0x0008, 0x0080), core.VRLO, "General Hospital"),
		stringElement(core.NewTag(0x0008, 0x0020), core.VRDA, studyDate),
		stringElement(core.NewTag(0x0008, 0x0030), core.VRTM, studyTime),
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
	if seriesDate != "" {
		dataset = append(dataset, stringElement(core.NewTag(0x0008, 0x0021), core.VRDA, seriesDate))
	}
	if seriesTime != "" {
		dataset = append(dataset, stringElement(core.NewTag(0x0008, 0x0031), core.VRTM, seriesTime))
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
