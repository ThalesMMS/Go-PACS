package dicominspect

import (
	"bytes"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/testutil"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

func TestInspectReaderSummarizesPart10File(t *testing.T) {
	data := testPart10File(t)

	summary, err := InspectReader("sample.dcm", bytes.NewReader(data), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}

	if summary.FileName != "sample.dcm" {
		t.Fatalf("FileName = %q, want sample.dcm", summary.FileName)
	}
	if summary.PatientName != "PORT^PATIENT" {
		t.Fatalf("PatientName = %q, want PORT^PATIENT", summary.PatientName)
	}
	if summary.PatientID != "PORT001" {
		t.Fatalf("PatientID = %q, want PORT001", summary.PatientID)
	}
	if summary.PatientBirthDate != "19700102" {
		t.Fatalf("PatientBirthDate = %q, want 19700102", summary.PatientBirthDate)
	}
	if summary.InstitutionName != "General Hospital" {
		t.Fatalf("InstitutionName = %q, want General Hospital", summary.InstitutionName)
	}
	if summary.Modality != "CT" {
		t.Fatalf("Modality = %q, want CT", summary.Modality)
	}
	if summary.StudyTime != "134501" {
		t.Fatalf("StudyTime = %q, want 134501", summary.StudyTime)
	}
	if summary.SeriesDate != "20260605" {
		t.Fatalf("SeriesDate = %q, want 20260605", summary.SeriesDate)
	}
	if summary.SeriesTime != "140102" {
		t.Fatalf("SeriesTime = %q, want 140102", summary.SeriesTime)
	}
	if summary.SeriesNumber != "7" {
		t.Fatalf("SeriesNumber = %q, want 7", summary.SeriesNumber)
	}
	if summary.SeriesDescription != "Axial" {
		t.Fatalf("SeriesDescription = %q, want Axial", summary.SeriesDescription)
	}
	if summary.InstanceNumber != "3" {
		t.Fatalf("InstanceNumber = %q, want 3", summary.InstanceNumber)
	}
	if summary.TransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("TransferSyntaxUID = %q, want %q", summary.TransferSyntaxUID, transfer.ExplicitVRLittleEndian.UID)
	}
	if summary.ElementCount == 0 {
		t.Fatal("ElementCount = 0, want meta and dataset elements")
	}
	if !hasElement(summary.Elements, "(0010,0010)", "PatientName", "PORT^PATIENT") {
		t.Fatal("missing summarized PatientName element")
	}
}

func TestInspectReaderRejectsNonDicomInput(t *testing.T) {
	_, err := InspectReader("not-dicom.txt", bytes.NewReader([]byte("not dicom")), DefaultOptions())
	if err == nil {
		t.Fatal("InspectReader accepted non-DICOM input")
	}
}

func hasElement(elements []ElementSummary, tag, keyword, value string) bool {
	for _, elem := range elements {
		if elem.Tag == tag && elem.Keyword == keyword && elem.Value == value {
			return true
		}
	}
	return false
}

func testPart10File(t *testing.T) []byte {
	t.Helper()
	dataset := []core.Element{
		testutil.StringElement(tagSOPClassUID, core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
		testutil.StringElement(tagSOPInstanceUID, core.VRUI, "1.2.826.0.1.3680043.10.543.1001"),
		testutil.StringElement(tagPatientName, core.VRPN, "PORT^PATIENT"),
		testutil.StringElement(tagPatientID, core.VRLO, "PORT001"),
		testutil.StringElement(tagPatientBirthDate, core.VRDA, "19700102"),
		testutil.StringElement(tagInstitutionName, core.VRLO, "General Hospital"),
		testutil.StringElement(tagStudyDate, core.VRDA, "20260604"),
		testutil.StringElement(tagStudyTime, core.VRTM, "134501"),
		testutil.StringElement(core.NewTag(0x0008, 0x0021), core.VRDA, "20260605"),
		testutil.StringElement(core.NewTag(0x0008, 0x0031), core.VRTM, "140102"),
		testutil.StringElement(tagModality, core.VRCS, "CT"),
		testutil.StringElement(tagAccessionNumber, core.VRSH, "ACC-001"),
		testutil.StringElement(tagSeriesDescription, core.VRLO, "Axial"),
		testutil.StringElement(tagStudyInstanceUID, core.VRUI, "1.2.826.0.1.3680043.10.543.2001"),
		testutil.StringElement(tagSeriesUID, core.VRUI, "1.2.826.0.1.3680043.10.543.3001"),
		testutil.StringElement(tagSeriesNumber, core.VRIS, "7"),
		testutil.StringElement(tagInstanceNumber, core.VRIS, "3"),
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
