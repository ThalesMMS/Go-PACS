package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/testutil"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

func TestAnonymizeStudyStoresSafeCopyWithNewUIDsAndKeepsOriginal(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	sourceDir := t.TempDir()

	studyUID := "1.2.826.0.1.3680043.10.543.9001"
	seriesUID := studyUID + ".1"
	firstSOP := seriesUID + ".1"
	secondSOP := seriesUID + ".2"
	writeCorePart10(t, filepath.Join(sourceDir, "first.dcm"), studyUID, seriesUID, firstSOP)
	writeCorePart10(t, filepath.Join(sourceDir, "second.dcm"), studyUID, seriesUID, secondSOP)
	if _, err := sess.ImportPath(ctx, sourceDir, nil); err != nil {
		t.Fatal(err)
	}

	outcome, err := sess.AnonymizeStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}

	if outcome.SourceStudyUID != studyUID {
		t.Fatalf("SourceStudyUID = %q, want %q", outcome.SourceStudyUID, studyUID)
	}
	if outcome.NewStudyUID == "" || outcome.NewStudyUID == studyUID || !strings.HasPrefix(outcome.NewStudyUID, "2.25.") {
		t.Fatalf("NewStudyUID = %q, want new 2.25.* UID", outcome.NewStudyUID)
	}
	if outcome.StoredFiles != 2 {
		t.Fatalf("StoredFiles = %d, want 2", outcome.StoredFiles)
	}
	if outcome.FailedFiles != 0 {
		t.Fatalf("FailedFiles = %d, want 0", outcome.FailedFiles)
	}

	original, err := sess.Catalog().InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) != 2 {
		t.Fatalf("original instances = %d, want 2", len(original))
	}
	copies, err := sess.Catalog().InstancesForStudy(ctx, outcome.NewStudyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 2 {
		t.Fatalf("anonymized instances = %d, want 2", len(copies))
	}

	seenSOP := map[string]bool{}
	for _, inst := range copies {
		if inst.PatientName == "CORE^PATIENT" || inst.PatientID == "C001" || inst.AccessionNumber == "ACC-1" {
			t.Fatalf("anonymized instance kept PHI: %#v", inst)
		}
		if inst.StudyInstanceUID != outcome.NewStudyUID {
			t.Fatalf("copy StudyInstanceUID = %q, want %q", inst.StudyInstanceUID, outcome.NewStudyUID)
		}
		if inst.SeriesInstanceUID == seriesUID {
			t.Fatalf("copy SeriesInstanceUID = original %q", seriesUID)
		}
		if inst.SOPInstanceUID == firstSOP || inst.SOPInstanceUID == secondSOP {
			t.Fatalf("copy SOPInstanceUID = original %q", inst.SOPInstanceUID)
		}
		if seenSOP[inst.SOPInstanceUID] {
			t.Fatalf("duplicate anonymized SOPInstanceUID %q", inst.SOPInstanceUID)
		}
		seenSOP[inst.SOPInstanceUID] = true
		if inst.StoredPath == "" {
			t.Fatal("anonymized StoredPath is empty")
		}
		if _, err := os.Stat(inst.StoredPath); err != nil {
			t.Fatalf("anonymized copy missing on disk: %v", err)
		}
	}
}

func writeCorePart10(t *testing.T, path, studyUID, seriesUID, sopUID string) {
	t.Helper()
	file := &object.File{
		Dataset: object.FromElements([]core.Element{
			testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
			testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
			testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "CORE^PATIENT"),
			testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "C001"),
			testutil.StringElement(core.NewTag(0x0008, 0x0050), core.VRSH, "ACC-1"),
			testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
			testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
			testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
			testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
		}, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
