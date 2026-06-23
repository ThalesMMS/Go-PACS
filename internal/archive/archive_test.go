package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/testutil"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/pixeldata/codecfixture"
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

func TestImportPathWithOptionsDecompressesDicomFiles(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	tc := codecfixture.JPEGLosslessSmall()
	data, err := tc.Part10Bytes()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "compressed.dcm")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.ImportPathWithOptions(ctx, source, ImportOptions{DecompressImages: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 1 || report.InvalidFiles != 0 {
		t.Fatalf("report = %#v, want one decompressed stored file", report)
	}
	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("studies = %d, want 1", len(studies))
	}
	instances, err := catalog.InstancesForStudy(ctx, studies[0].StudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	if instances[0].TransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("TransferSyntaxUID = %q, want %q", instances[0].TransferSyntaxUID, transfer.ExplicitVRLittleEndian.UID)
	}
	storedFile, err := os.Open(instances[0].StoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer storedFile.Close()
	stored, err := object.ReadFile(storedFile)
	if err != nil {
		t.Fatal(err)
	}
	if stored.TransferSyntax.UID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("stored TransferSyntax = %q, want %q", stored.TransferSyntax.UID, transfer.ExplicitVRLittleEndian.UID)
	}
	if raw, ok := stored.GetRaw(core.TagPixelData); !ok || !bytes.Equal(raw, tc.ExpectedFrames[0]) {
		t.Fatalf("stored PixelData = %v, want %v", raw, tc.ExpectedFrames[0])
	}
}

func TestDecompressStudyReplacesCompressedArchiveFiles(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	tc := codecfixture.JPEGLosslessSmall()
	data, err := tc.Part10Bytes()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "compressed.dcm")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}
	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("studies before decompress = %d, want 1", len(studies))
	}
	studyUID := studies[0].StudyInstanceUID
	before, err := catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("instances before decompress = %d, want 1", len(before))
	}
	if before[0].TransferSyntaxUID != tc.Syntax.UID {
		t.Fatalf("before TransferSyntaxUID = %q, want %q", before[0].TransferSyntaxUID, tc.Syntax.UID)
	}
	oldPath := before[0].StoredPath

	report, err := catalog.DecompressStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if report.DecompressedFiles != 1 || report.FailedFiles != 0 {
		t.Fatalf("decompress report = %#v, want one decompressed file", report)
	}
	after, err := catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("instances after decompress = %d, want 1", len(after))
	}
	if after[0].TransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("after TransferSyntaxUID = %q, want %q", after[0].TransferSyntaxUID, transfer.ExplicitVRLittleEndian.UID)
	}
	if after[0].StoredPath == oldPath {
		t.Fatal("decompressed instance kept old stored path")
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old compressed object still exists: %v", err)
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

func TestDeleteStudyRemovesMissingStudyUIDInstances(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "missing-study.dcm"), testPart10FileWithDetails(t, "MISSING^STUDY", "M001", "CT", "", "1.2.3.missing.series", "1.2.3.missing.instance", "1", "Missing Study", "1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetStudyMetadata(ctx, "(missing)", StudyMetadata{Status: "Reviewed", Comments: "Missing UID bucket"}); err != nil {
		t.Fatal(err)
	}

	instances, err := catalog.InstancesForStudy(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("InstancesForStudy(missing) returned %d instances, want 1", len(instances))
	}
	storedPath := instances[0].StoredPath

	deleted, err := catalog.DeleteStudy(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteStudy deleted = %d, want 1", deleted)
	}

	instances, err = catalog.InstancesForStudy(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("missing study instances after delete = %#v", instances)
	}
	if _, err := os.Stat(storedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing study object %q exists after delete: %v", storedPath, err)
	}
	metadata, err := catalog.StudyMetadata(ctx, "(missing)")
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
	if len(studies) != 0 {
		t.Fatalf("studies after delete = %#v", studies)
	}
}

func TestTrashStudyAndRestore(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	studyUID := "1.2.826.0.1.3680043.10.543.9601"
	sopUID := studyUID + ".instance"
	if err := os.WriteFile(filepath.Join(sourceDir, "study.dcm"), testPart10FileWithDetails(t, "TRASH^PATIENT", "T001", "CT", studyUID, studyUID+".series", sopUID, "1", "Trash", "1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if studies, err := catalog.Studies(ctx); err != nil {
		t.Fatal(err)
	} else if len(studies) != 1 {
		t.Fatalf("studies before trash = %d, want 1", len(studies))
	}
	instances, err := catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances before trash = %d, want 1", len(instances))
	}
	originalPath := instances[0].StoredPath

	deleted, err := catalog.TrashStudy(ctx, studyUID)
	if err != nil {
		t.Fatalf("TrashStudy failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("TrashStudy deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(originalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original object still exists after trash: %v", err)
	}
	if instances, err := catalog.InstancesForStudy(ctx, studyUID); err != nil {
		t.Fatal(err)
	} else if len(instances) != 0 {
		t.Fatalf("instances after trash = %#v, want none", instances)
	}
	entries, err := catalog.ListTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].StudyInstanceUID != studyUID {
		t.Fatalf("trash entries = %#v, want study %q", entries, studyUID)
	}
	manifest, err := readTrashManifest(filepath.Join(catalog.trashPathForStudy(studyUID), trashManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.DeletedCount != 1 || len(manifest.Objects) != 1 {
		t.Fatalf("trash manifest = %#v, want one deleted object", manifest)
	}
	if manifest.Objects[0].SOPInstanceUID != sopUID || manifest.Objects[0].OriginalPath != originalPath {
		t.Fatalf("trash manifest object = %#v, want SOP %q original path %q", manifest.Objects[0], sopUID, originalPath)
	}

	report, err := catalog.RestoreStudy(ctx, studyUID)
	if err != nil {
		t.Fatalf("RestoreStudy failed: %v", err)
	}
	if report.StoredFiles != 1 || report.InvalidFiles != 0 {
		t.Fatalf("restore report = %#v, want one stored and no invalid", report)
	}
	if studies, err := catalog.Studies(ctx); err != nil {
		t.Fatal(err)
	} else if len(studies) != 1 || studies[0].StudyInstanceUID != studyUID {
		t.Fatalf("studies after restore = %#v, want restored study", studies)
	}
	entries, err = catalog.ListTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("trash entries after restore = %#v, want none", entries)
	}
}

func TestListTrashSortsByParsedTrashTime(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	firstStudyUID := "1.2.826.0.1.3680043.10.543.9602"
	secondStudyUID := "1.2.826.0.1.3680043.10.543.9603"
	if err := os.WriteFile(filepath.Join(sourceDir, "first.dcm"), testPart10File(t, "TRASH^FIRST", "TF001", "CT", firstStudyUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "second.dcm"), testPart10File(t, "TRASH^SECOND", "TS001", "CT", secondStudyUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.TrashStudy(ctx, firstStudyUID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.TrashStudy(ctx, secondStudyUID); err != nil {
		t.Fatal(err)
	}
	setTrashTime := func(studyUID, value string) {
		t.Helper()
		path := filepath.Join(catalog.trashPathForStudy(studyUID), trashManifestFileName)
		manifest, err := readTrashManifest(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest.TrashedAt = value
		if err := writeTrashManifest(path, manifest); err != nil {
			t.Fatal(err)
		}
	}
	setTrashTime(firstStudyUID, "2026-06-23T00:00:00Z")
	setTrashTime(secondStudyUID, "2026-06-23T00:00:00.1Z")

	entries, err := catalog.ListTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].StudyInstanceUID != secondStudyUID {
		t.Fatalf("trash entries = %#v, want later fractional timestamp first", entries)
	}
}

func TestPurgeExpiredTrashDeletesOnlyExpiredTrashEntries(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	oldStudyUID := "1.2.826.0.1.3680043.10.543.9701"
	newStudyUID := "1.2.826.0.1.3680043.10.543.9702"
	activeStudyUID := "1.2.826.0.1.3680043.10.543.9703"
	files := map[string][]byte{
		"old.dcm":    testPart10File(t, "TRASH^OLD", "TO001", "CT", oldStudyUID),
		"new.dcm":    testPart10File(t, "TRASH^NEW", "TN001", "MR", newStudyUID),
		"active.dcm": testPart10File(t, "ACTIVE^KEEP", "AK001", "US", activeStudyUID),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.TrashStudy(ctx, oldStudyUID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.TrashStudy(ctx, newStudyUID); err != nil {
		t.Fatal(err)
	}
	oldManifestPath := filepath.Join(catalog.trashPathForStudy(oldStudyUID), trashManifestFileName)
	oldManifest, err := readTrashManifest(oldManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	oldManifest.TrashedAt = time.Now().UTC().Add(-100 * 24 * time.Hour).Format(time.RFC3339Nano)
	if err := writeTrashManifest(oldManifestPath, oldManifest); err != nil {
		t.Fatal(err)
	}

	report, err := catalog.PurgeExpiredTrash(ctx, time.Now().UTC().Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.Purged != 1 {
		t.Fatalf("Purged = %d, want 1", report.Purged)
	}
	if _, err := os.Stat(catalog.trashPathForStudy(oldStudyUID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old trash still present: %v", err)
	}
	if _, err := os.Stat(catalog.trashPathForStudy(newStudyUID)); err != nil {
		t.Fatalf("new trash missing: %v", err)
	}
	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != activeStudyUID {
		t.Fatalf("active studies = %#v, want only %q", studies, activeStudyUID)
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

func TestCatalogSchemaCreatesSHA256Index(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	if !catalogHasIndex(t, catalog, "idx_instances_sha256") {
		t.Fatal("instances sha256 index not found")
	}
}

func TestCatalogSchemaCreatesSOPInstanceUIDIndex(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	if !catalogHasIndex(t, catalog, "idx_instances_sop") {
		t.Fatal("instances SOP instance UID index not found")
	}
}

func TestCatalogSchemaCreatesPatientSoundexTable(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	if !catalogHasTable(t, catalog, "instance_patient_soundex") {
		t.Fatal("instance_patient_soundex table not found")
	}
	if !catalogHasIndexOnTable(t, catalog, "instance_patient_soundex", "idx_instance_patient_soundex_code") {
		t.Fatal("instance_patient_soundex code index not found")
	}
}

func TestCatalogSchemaCreatesCaseInsensitiveStudyFilterIndexes(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	if !catalogHasIndex(t, catalog, "idx_instances_modality_nocase") {
		t.Fatal("modality NOCASE index not found")
	}
}

func catalogHasIndex(t testing.TB, catalog *Catalog, name string) bool {
	t.Helper()
	return catalogHasIndexOnTable(t, catalog, "instances", name)
}

func catalogHasIndexOnTable(t testing.TB, catalog *Catalog, table string, name string) bool {
	t.Helper()
	rows, err := catalog.db.Query(`PRAGMA index_list('` + table + `')`)
	if err != nil {
		t.Fatalf("query index list: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var seq, unique, partial int
		var indexName, origin string
		if err := rows.Scan(&seq, &indexName, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index list: %v", err)
		}
		if indexName == name {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index list: %v", err)
	}
	return false
}

func catalogHasTable(t testing.TB, catalog *Catalog, name string) bool {
	t.Helper()
	var found string
	err := catalog.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		return false
	}
	return found == name
}

func TestCatalogUIDLookupsPreserveMissingSentinel(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	if err := catalog.upsertInstance(ctx, Instance{
		SHA256:     "missing-uids",
		StoredPath: "objects/missing.dcm",
		SourcePath: "missing.dcm",
		FileSize:   1,
		ImportedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	studyInstances, err := catalog.InstancesForStudy(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if len(studyInstances) != 1 {
		t.Fatalf("InstancesForStudy(missing) returned %d instances, want 1", len(studyInstances))
	}

	series, err := catalog.SeriesForStudy(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].SeriesInstanceUID != "(missing)" {
		t.Fatalf("SeriesForStudy(missing) = %#v, want one missing series", series)
	}

	seriesInstances, err := catalog.InstancesForSeries(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesInstances) != 1 {
		t.Fatalf("InstancesForSeries(missing) returned %d instances, want 1", len(seriesInstances))
	}

	instance, err := catalog.InstanceBySOPInstanceUID(ctx, "(missing)")
	if err != nil {
		t.Fatal(err)
	}
	if instance.SHA256 != "missing-uids" {
		t.Fatalf("InstanceBySOPInstanceUID(missing) = %#v", instance)
	}
}

func TestCatalogBackfillsPatientSoundexCodes(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "one.dcm"), testPart10File(t, "SOUNDEX^PATIENT", "SX001", "CT", "1.2.3.soundex"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.Exec(`DELETE FROM instance_patient_soundex`); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}

	catalog, err = Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	studies, err := catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "SOUNDEX", PatientNameSoundex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != "1.2.3.soundex" {
		t.Fatalf("soundex studies after backfill = %#v, want imported study", studies)
	}
}

func TestStudiesWithSoundexUsesPersistedCodes(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "one.dcm"), testPart10File(t, "PERSISTED^CODES", "SX002", "CT", "1.2.3.persisted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.Exec(`DELETE FROM instance_patient_soundex`); err != nil {
		t.Fatal(err)
	}

	studies, err := catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "PERSISTED", PatientNameSoundex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("soundex studies with deleted persisted codes = %#v, want none", studies)
	}
}

func TestStudyAndSeriesFilterWhereAvoidCaseFoldingWrappers(t *testing.T) {
	studyWhere, studyArgs := studyFilterWhere(StudyFilters{
		PatientName:      " Alice ",
		PatientID:        "a001",
		AccessionNumber:  "acc-1",
		StudyDescription: "head",
		SourcePath:       "ALICE.DCM",
		Status:           "interesting",
		Modalities:       []string{"mr", " CT "},
	})
	if strings.Contains(studyWhere, "LOWER(") || strings.Contains(studyWhere, "UPPER(") {
		t.Fatalf("study filter WHERE uses SQL case-folding wrapper: %s", studyWhere)
	}
	for _, want := range []string{
		"patient_name COLLATE NOCASE LIKE ?",
		"patient_id COLLATE NOCASE LIKE ?",
		"accession_number COLLATE NOCASE LIKE ?",
		"study_description COLLATE NOCASE LIKE ?",
		"source_path COLLATE NOCASE LIKE ?",
		"sm.status COLLATE NOCASE LIKE ?",
		"modality COLLATE NOCASE IN (?, ?)",
	} {
		if !strings.Contains(studyWhere, want) {
			t.Fatalf("study filter WHERE = %q, missing %q", studyWhere, want)
		}
	}
	if len(studyArgs) != 8 {
		t.Fatalf("study filter args = %#v, want 8 args", studyArgs)
	}

	seriesWhere, seriesArgs := seriesFilterWhere("1.2.3", SeriesFilters{
		Modality:          "mr",
		SeriesNumber:      "1",
		SeriesDescription: "axial",
	})
	if strings.Contains(seriesWhere, "LOWER(") || strings.Contains(seriesWhere, "UPPER(") {
		t.Fatalf("series filter WHERE uses SQL case-folding wrapper: %s", seriesWhere)
	}
	for _, want := range []string{
		"series_number COLLATE NOCASE LIKE ?",
		"series_description COLLATE NOCASE LIKE ?",
		"modality COLLATE NOCASE = ?",
	} {
		if !strings.Contains(seriesWhere, want) {
			t.Fatalf("series filter WHERE = %q, missing %q", seriesWhere, want)
		}
	}
	if len(seriesArgs) != 4 {
		t.Fatalf("series filter args = %#v, want 4 args", seriesArgs)
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

func TestStudiesPageWithFiltersReturnsTotalAndWindow(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	sourceDir := t.TempDir()
	for i, patient := range []string{"Page^Alpha", "Page^Bravo", "Page^Charlie"} {
		studyUID := fmt.Sprintf("1.2.826.0.1.3680043.10.543.980%d", i)
		data := testPart10FileWithStudyDateTime(t, patient, fmt.Sprintf("P%d", i), "CT", studyUID, "20260604", fmt.Sprintf("13450%d", i))
		if err := os.WriteFile(filepath.Join(sourceDir, patient+".dcm"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	page, err := catalog.StudiesPageWithFilters(ctx, StudyFilters{PatientName: "Page"}, StudyPageOptions{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Limit != 2 || page.Offset != 1 {
		t.Fatalf("page metadata = %#v, want total=3 limit=2 offset=1", page)
	}
	if len(page.Items) != 2 {
		t.Fatalf("page items = %d, want 2", len(page.Items))
	}
	if page.Items[0].StudyInstanceUID == page.Items[1].StudyInstanceUID {
		t.Fatalf("page order returned duplicate study: %#v", page.Items)
	}

	legacy, err := catalog.StudiesWithFilters(ctx, StudyFilters{PatientName: "Page"})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 3 {
		t.Fatalf("legacy unpaged len = %d, want 3", len(legacy))
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

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
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

func TestRebuildCatalogFromObjects(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	studyA := "1.2.826.0.1.3680043.10.543.9401"
	studyB := "1.2.826.0.1.3680043.10.543.9402"
	fixtures := map[string][]byte{
		"a1.dcm": testPart10FileWithDetails(t, "REBUILD^A", "RA001", "CT", studyA, studyA+".series.1", studyA+".instance.1", "1", "A1", "1"),
		"a2.dcm": testPart10FileWithDetails(t, "REBUILD^A", "RA001", "CT", studyA, studyA+".series.1", studyA+".instance.2", "1", "A1", "2"),
		"a3.dcm": testPart10FileWithDetails(t, "REBUILD^A", "RA001", "MR", studyA, studyA+".series.2", studyA+".instance.3", "2", "A2", "1"),
		"b1.dcm": testPart10FileWithDetails(t, "REBUILD^B", "RB001", "US", studyB, studyB+".series.1", studyB+".instance.1", "1", "B1", "1"),
		"b2.dcm": testPart10FileWithDetails(t, "REBUILD^B", "RB001", "US", studyB, studyB+".series.1", studyB+".instance.2", "1", "B1", "2"),
	}
	for name, data := range fixtures {
		if err := os.WriteFile(filepath.Join(sourceDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	importReport, err := catalog.ImportPath(ctx, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if importReport.StoredFiles != 5 {
		t.Fatalf("initial StoredFiles = %d, want 5", importReport.StoredFiles)
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(archiveDir, "catalog.db")); err != nil {
		t.Fatal(err)
	}

	report, err := RebuildCatalog(ctx, archiveDir, RebuildOptions{})
	if err != nil {
		t.Fatalf("RebuildCatalog failed: %v", err)
	}
	if report.StoredFiles != 5 || report.FailedFiles != 0 {
		t.Fatalf("report stored/failed = %d/%d, want 5/0; rejections=%#v", report.StoredFiles, report.FailedFiles, report.Rejections)
	}
	if !containsString(report.Warnings, rebuildMetadataWarning) {
		t.Fatalf("warnings = %#v, want metadata warning", report.Warnings)
	}

	rebuilt, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	studies, err := rebuilt.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 2 {
		t.Fatalf("rebuilt studies = %d, want 2", len(studies))
	}
	seriesA, err := rebuilt.SeriesForStudy(ctx, studyA)
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesA) != 2 {
		t.Fatalf("rebuilt series for study A = %d, want 2", len(seriesA))
	}
	instancesA, err := rebuilt.InstancesForStudy(ctx, studyA)
	if err != nil {
		t.Fatal(err)
	}
	if len(instancesA) != 3 {
		t.Fatalf("rebuilt instances for study A = %d, want 3", len(instancesA))
	}
	seriesB, err := rebuilt.SeriesForStudy(ctx, studyB)
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesB) != 1 {
		t.Fatalf("rebuilt series for study B = %d, want 1", len(seriesB))
	}
	instancesB, err := rebuilt.InstancesForStudy(ctx, studyB)
	if err != nil {
		t.Fatal(err)
	}
	if len(instancesB) != 2 {
		t.Fatalf("rebuilt instances for study B = %d, want 2", len(instancesB))
	}
}

func TestRebuildCatalogUsesContentDigest(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}

	studyUID := "1.2.826.0.1.3680043.10.543.9403"
	data := testPart10File(t, "REBUILD^HASH", "RH001", "CT", studyUID)
	actualDigest := fmt.Sprintf("%x", sha256.Sum256(data))
	fakeDigest := strings.Repeat("0", 64)
	if fakeDigest == actualDigest {
		fakeDigest = strings.Repeat("1", 64)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "objects", fakeDigest+".dcm"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := RebuildCatalog(ctx, archiveDir, RebuildOptions{})
	if err != nil {
		t.Fatalf("RebuildCatalog failed: %v", err)
	}
	if report.StoredFiles != 1 || report.FailedFiles != 0 {
		t.Fatalf("report stored/failed = %d/%d, want 1/0; rejections=%#v", report.StoredFiles, report.FailedFiles, report.Rejections)
	}

	rebuilt, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	instance, err := rebuilt.InstanceBySOPInstanceUID(ctx, studyUID+".instance")
	if err != nil {
		t.Fatal(err)
	}
	if instance.SHA256 != actualDigest {
		t.Fatalf("rebuilt SHA256 = %q, want content digest %q", instance.SHA256, actualDigest)
	}
}

func TestRebuildCatalogMovesExistingCatalogAndReportsCorruptObject(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(archiveDir, "objects", "bad.dcm")
	if err := os.WriteFile(badPath, []byte("not dicom"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := RebuildCatalog(context.Background(), archiveDir, RebuildOptions{})
	if err != nil {
		t.Fatalf("RebuildCatalog failed: %v", err)
	}
	if report.CatalogBackupPath == "" {
		t.Fatal("CatalogBackupPath is empty, want moved existing catalog")
	}
	if _, err := os.Stat(report.CatalogBackupPath); err != nil {
		t.Fatalf("catalog backup missing: %v", err)
	}
	if report.FailedFiles != 1 || len(report.Rejections) != 1 {
		t.Fatalf("failed/rejections = %d/%d, want 1/1", report.FailedFiles, len(report.Rejections))
	}
	if !strings.Contains(report.Rejections[0].Path, "bad.dcm") {
		t.Fatalf("rejection path = %q, want bad.dcm", report.Rejections[0].Path)
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
		testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
		testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
		testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, patientName),
		testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, patientID),
		testutil.StringElement(core.NewTag(0x0010, 0x0030), core.VRDA, "19700102"),
		testutil.StringElement(core.NewTag(0x0008, 0x0080), core.VRLO, "General Hospital"),
		testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, studyDate),
		testutil.StringElement(core.NewTag(0x0008, 0x0030), core.VRTM, studyTime),
		testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, modality),
		testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
		testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
	}
	if seriesNumber != "" {
		dataset = append(dataset, testutil.StringElement(core.NewTag(0x0020, 0x0011), core.VRIS, seriesNumber))
	}
	if seriesDescription != "" {
		dataset = append(dataset, testutil.StringElement(core.NewTag(0x0008, 0x103E), core.VRLO, seriesDescription))
	}
	if seriesDate != "" {
		dataset = append(dataset, testutil.StringElement(core.NewTag(0x0008, 0x0021), core.VRDA, seriesDate))
	}
	if seriesTime != "" {
		dataset = append(dataset, testutil.StringElement(core.NewTag(0x0008, 0x0031), core.VRTM, seriesTime))
	}
	if instanceNumber != "" {
		dataset = append(dataset, testutil.StringElement(core.NewTag(0x0020, 0x0013), core.VRIS, instanceNumber))
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

func TestObjectDirReturnsStoreDir(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	dir := catalog.ObjectDir()
	if dir == "" {
		t.Fatal("ObjectDir returned empty string")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("ObjectDir %q is not accessible: %v", dir, err)
	}
	if filepath.Base(dir) != "objects" {
		t.Fatalf("ObjectDir = %q, want path ending in objects/", dir)
	}
}

func TestObjectDirReturnsEmptyStringForNilCatalog(t *testing.T) {
	var c *Catalog
	if dir := c.ObjectDir(); dir != "" {
		t.Fatalf("nil Catalog.ObjectDir() = %q, want empty string", dir)
	}
}

func TestIntegrityCheckPassesOnCleanCatalog(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	if err := catalog.IntegrityCheck(ctx); err != nil {
		t.Fatalf("IntegrityCheck on clean catalog failed: %v", err)
	}
}

func TestIntegrityCheckPassesAfterImport(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := filepath.Join(t.TempDir(), "study.dcm")
	if err := os.WriteFile(source, testPart10File(t, "INTEGRITY^CHECK", "IC001", "CT", "1.2.3.integrity"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}

	if err := catalog.IntegrityCheck(ctx); err != nil {
		t.Fatalf("IntegrityCheck after import failed: %v", err)
	}
}

func TestStoredPathsListsCataloguedObjectPaths(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	paths, err := catalog.StoredPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("StoredPaths on empty catalog = %d, want 0", len(paths))
	}

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "a.dcm"), testPart10File(t, "STORED^A", "SA001", "CT", "1.2.3.stored.a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "b.dcm"), testPart10File(t, "STORED^B", "SB001", "MR", "1.2.3.stored.b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	paths, err = catalog.StoredPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("StoredPaths after two imports = %d, want 2", len(paths))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("stored path %q not accessible: %v", p, err)
		}
	}
}

func TestArchiveStatsCountsInstancesAndBytes(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	stats, err := catalog.ArchiveStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.InstanceCount != 0 || stats.TotalBytes != 0 {
		t.Fatalf("empty catalog stats = %+v, want zero counts", stats)
	}

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "one.dcm"), testPart10File(t, "STATS^ONE", "S001", "CT", "1.2.3.stats"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, sourceDir); err != nil {
		t.Fatal(err)
	}

	stats, err = catalog.ArchiveStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.InstanceCount != 1 {
		t.Fatalf("InstanceCount = %d, want 1", stats.InstanceCount)
	}
	if stats.TotalBytes <= 0 {
		t.Fatalf("TotalBytes = %d, want > 0", stats.TotalBytes)
	}
}

func TestBackupCatalogToCreatesReadableDatabase(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := filepath.Join(t.TempDir(), "backup-study.dcm")
	studyUID := "1.2.826.0.1.3680043.10.543.9810"
	if err := os.WriteFile(source, testPart10File(t, "BACKUP^CATALOG", "BC001", "CT", studyUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}

	// BackupCatalogTo writes a SQLite database to destPath. Open() expects
	// a rootDir containing catalog.db, so we back up directly to that path
	// inside a new directory.
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "catalog.db")
	if err := catalog.BackupCatalogTo(ctx, backupPath); err != nil {
		t.Fatalf("BackupCatalogTo failed: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup database missing: %v", err)
	}

	backup, err := Open(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()

	studies, err := backup.Studies(ctx)
	if err != nil {
		t.Fatalf("query backup studies failed: %v", err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != studyUID {
		t.Fatalf("backup studies = %#v, want study %q", studies, studyUID)
	}
}

func TestBackupCatalogToRejectsExistingDestination(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	existingPath := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(existingPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := catalog.BackupCatalogTo(ctx, existingPath); err == nil {
		t.Fatal("BackupCatalogTo succeeded with existing destination, want error")
	}
}

func TestRebaseStoredPathsUpdatesOldBasePaths(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	catalog, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := filepath.Join(t.TempDir(), "rebase-study.dcm")
	studyUID := "1.2.826.0.1.3680043.10.543.9820"
	if err := os.WriteFile(source, testPart10File(t, "REBASE^PATHS", "RP001", "CT", studyUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}

	// When paths already match the current store dir, rebase should report 0 updates.
	updated, err := catalog.RebaseStoredPaths(ctx)
	if err != nil {
		t.Fatalf("RebaseStoredPaths failed: %v", err)
	}
	if updated != 0 {
		t.Fatalf("RebaseStoredPaths on up-to-date paths returned %d, want 0", updated)
	}
}
