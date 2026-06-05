package query

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

func TestPatientRootFindAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "FINDSCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.PatientRootFindSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.PatientRootFindSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		req, identifier, err := dimse.ReceiveCFindRequest(assoc, pc.ID, transfer.ImplicitVRLittleEndian)
		if err != nil {
			done <- err
			return
		}
		if req.AffectedSOPClassUID != dimse.PatientRootFindSOPClassUID {
			done <- errors.New("expected Patient Root FIND SOP Class")
			return
		}
		level, _ := identifier.GetString(core.NewTag(0x0008, 0x0052))
		if level != dimse.QueryRetrieveLevelPatient {
			done <- errors.New("expected PATIENT query level")
			return
		}
		patientName, _ := identifier.GetString(tagPatientName)
		if patientName != "FIND^*" {
			done <- errors.New("expected PatientName=FIND^*")
			return
		}
		if err := dimse.SendCFindResponse(assoc, pc.ID, dimse.CFindResponse{
			AffectedSOPClassUID:       dimse.PatientRootFindSOPClassUID,
			MessageIDBeingRespondedTo: req.MessageID,
			Status:                    dimse.StatusPending,
		}, patientMatch("FIND^PATIENT", "P001", "19700102", "remote comment"), transfer.ImplicitVRLittleEndian); err != nil {
			done <- err
			return
		}
		if err := dimse.SendCFindResponse(assoc, pc.ID, dimse.CFindResponse{
			AffectedSOPClassUID:       dimse.PatientRootFindSOPClassUID,
			MessageIDBeingRespondedTo: req.MessageID,
			Status:                    dimse.StatusSuccess,
		}, nil, transfer.ImplicitVRLittleEndian); err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	node := nodes.Node{
		Name:    "findscp",
		AETitle: "FINDSCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	result, err := PatientRootFind(ctx, node, PatientCriteria{
		PatientName: "FIND^*",
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("len(Matches) = %d, want 1", len(result.Matches))
	}
	match := result.Matches[0]
	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelPatient {
		t.Fatalf("QueryRetrieveLevel = %q, want PATIENT", match.QueryRetrieveLevel)
	}
	if match.PatientName != "FIND^PATIENT" || match.PatientID != "P001" {
		t.Fatalf("patient match = %+v", match)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestStudyRootFindAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "FINDSCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootFindSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootFindSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCFind(ctx, assoc, pc.ID, dimse.CFindHandlerFunc(func(_ context.Context, req dimse.CFindRequestContext) ([]*object.Object, error) {
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelStudy {
				return nil, errors.New("expected STUDY query level")
			}
			patientID, _ := req.Identifier.GetString(tagPatientID)
			if patientID != "P001" {
				return nil, errors.New("expected PatientID=P001")
			}
			description, _ := req.Identifier.GetString(tagStudyDescription)
			if description != "Query test" {
				return nil, errors.New("expected StudyDescription=Query test")
			}
			modalities, _ := req.Identifier.GetString(tagModalitiesInStudy)
			if modalities != "CT" {
				return nil, errors.New("expected ModalitiesInStudy=CT")
			}
			return []*object.Object{
				studyMatch("FIND^PATIENT", "P001", "CT\\MR", "1.2.3.study"),
			}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	node := nodes.Node{
		Name:    "findscp",
		AETitle: "FINDSCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	result, err := StudyRootFind(ctx, node, Criteria{
		PatientID:        "P001",
		StudyDescription: "Query test",
		Modality:         "CT",
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("len(Matches) = %d, want 1", len(result.Matches))
	}
	match := result.Matches[0]
	if match.PatientName != "FIND^PATIENT" {
		t.Fatalf("PatientName = %q, want FIND^PATIENT", match.PatientName)
	}
	if match.Modalities != "CT\\MR" {
		t.Fatalf("Modalities = %q, want CT\\MR", match.Modalities)
	}
	if result.FinalStatus != dimse.StatusSuccess {
		t.Fatalf("FinalStatus = 0x%04X, want success", result.FinalStatus)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestStudyRootStudyIdentifierIncludesClinicalSearchFields(t *testing.T) {
	identifier, err := studyRootStudyIdentifier(Criteria{
		PatientBirthDate:       "19700102",
		ReferringPhysicianName: "REFER^DOC",
		InstitutionName:        "General Hospital",
		PatientComments:        "remote comment",
		StudyStatusID:          "VERIFIED",
	})
	if err != nil {
		t.Fatal(err)
	}

	birthDate, _ := identifier.GetString(tagPatientBirthDate)
	if birthDate != "19700102" {
		t.Fatalf("PatientBirthDate = %q, want 19700102", birthDate)
	}
	referring, _ := identifier.GetString(tagReferringPhysicianName)
	if referring != "REFER^DOC" {
		t.Fatalf("ReferringPhysicianName = %q, want REFER^DOC", referring)
	}
	institution, _ := identifier.GetString(tagInstitutionName)
	if institution != "General Hospital" {
		t.Fatalf("InstitutionName = %q, want General Hospital", institution)
	}
	comments, _ := identifier.GetString(tagPatientComments)
	if comments != "remote comment" {
		t.Fatalf("PatientComments = %q, want remote comment", comments)
	}
	studyStatus, _ := identifier.GetString(tagStudyStatusID)
	if studyStatus != "VERIFIED" {
		t.Fatalf("StudyStatusID = %q, want VERIFIED", studyStatus)
	}
	if _, ok := identifier.GetString(tagStudyTime); !ok {
		t.Fatal("identifier missing StudyTime return key")
	}
	if _, ok := identifier.GetString(tagNumberOfStudyRelatedInstances); !ok {
		t.Fatal("identifier missing NumberOfStudyRelatedInstances return key")
	}
}

func TestStudyRootStudyIdentifierIncludesCustomDICOMField(t *testing.T) {
	tagStudyID := core.NewTag(0x0020, 0x0010)
	identifier, err := studyRootStudyIdentifier(Criteria{
		CustomFieldKeyword: "StudyID",
		CustomFieldValue:   "ABC123",
	})
	if err != nil {
		t.Fatal(err)
	}

	studyID, _ := identifier.GetString(tagStudyID)
	if studyID != "ABC123" {
		t.Fatalf("StudyID = %q, want ABC123", studyID)
	}
}

func TestStudyRootStudyIdentifierIncludesStudyTimeRange(t *testing.T) {
	identifier, err := studyRootStudyIdentifier(Criteria{
		StudyTimeFrom: "120000",
		StudyTimeTo:   "235959",
	})
	if err != nil {
		t.Fatal(err)
	}

	studyTime, _ := identifier.GetString(tagStudyTime)
	if studyTime != "120000-235959" {
		t.Fatalf("StudyTime = %q, want 120000-235959", studyTime)
	}
}

func TestStudyRootStudyIdentifierRejectsUnknownCustomDICOMField(t *testing.T) {
	_, err := studyRootStudyIdentifier(Criteria{
		CustomFieldKeyword: "NotAKeyword",
		CustomFieldValue:   "ABC123",
	})
	if err == nil {
		t.Fatal("expected unknown custom DICOM keyword error")
	}
}

func TestPatientRootPatientIdentifierIncludesClinicalSearchFields(t *testing.T) {
	identifier, err := patientRootPatientIdentifier(PatientCriteria{
		PatientName:      "FIND^*",
		PatientID:        "P001",
		PatientBirthDate: "19700102",
		PatientComments:  "remote comment",
	})
	if err != nil {
		t.Fatal(err)
	}

	level, _ := identifier.GetString(core.NewTag(0x0008, 0x0052))
	if level != dimse.QueryRetrieveLevelPatient {
		t.Fatalf("QueryRetrieveLevel = %q, want PATIENT", level)
	}
	patientName, _ := identifier.GetString(tagPatientName)
	if patientName != "FIND^*" {
		t.Fatalf("PatientName = %q, want FIND^*", patientName)
	}
	patientID, _ := identifier.GetString(tagPatientID)
	if patientID != "P001" {
		t.Fatalf("PatientID = %q, want P001", patientID)
	}
	birthDate, _ := identifier.GetString(tagPatientBirthDate)
	if birthDate != "19700102" {
		t.Fatalf("PatientBirthDate = %q, want 19700102", birthDate)
	}
	comments, _ := identifier.GetString(tagPatientComments)
	if comments != "remote comment" {
		t.Fatalf("PatientComments = %q, want remote comment", comments)
	}
}

func TestStudyMatchFromIdentifierIncludesClinicalDisplayFields(t *testing.T) {
	identifier := object.FromElements([]core.Element{
		stringElement(tagPatientName, core.VRPN, "DISPLAY^PATIENT"),
		stringElement(tagPatientBirthDate, core.VRDA, "19700102"),
		stringElement(tagStudyTime, core.VRTM, "134501"),
		stringElement(tagNumberOfStudyRelatedInstances, core.VRIS, "42"),
		stringElement(tagReferringPhysicianName, core.VRPN, "REFER^DOC"),
		stringElement(tagInstitutionName, core.VRLO, "General Hospital"),
		stringElement(tagPatientComments, core.VRLT, "remote comment"),
		stringElement(tagStudyStatusID, core.VRCS, "VERIFIED"),
	}, std.Dictionary)

	match := studyMatchFromIdentifier(identifier, dimse.StatusPending)

	if match.PatientBirthDate != "19700102" {
		t.Fatalf("PatientBirthDate = %q, want 19700102", match.PatientBirthDate)
	}
	if match.StudyTime != "134501" {
		t.Fatalf("StudyTime = %q, want 134501", match.StudyTime)
	}
	if match.ImageCount != "42" {
		t.Fatalf("ImageCount = %q, want 42", match.ImageCount)
	}
	if match.ReferringPhysicianName != "REFER^DOC" {
		t.Fatalf("ReferringPhysicianName = %q, want REFER^DOC", match.ReferringPhysicianName)
	}
	if match.InstitutionName != "General Hospital" {
		t.Fatalf("InstitutionName = %q, want General Hospital", match.InstitutionName)
	}
	if match.PatientComments != "remote comment" {
		t.Fatalf("PatientComments = %q, want remote comment", match.PatientComments)
	}
	if match.StudyStatusID != "VERIFIED" {
		t.Fatalf("StudyStatusID = %q, want VERIFIED", match.StudyStatusID)
	}
}

func TestPatientMatchFromIdentifierIncludesClinicalDisplayFields(t *testing.T) {
	identifier := patientMatch("DISPLAY^PATIENT", "P001", "19700102", "remote comment")

	match := patientMatchFromIdentifier(identifier, dimse.StatusPending)

	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelPatient {
		t.Fatalf("QueryRetrieveLevel = %q, want PATIENT", match.QueryRetrieveLevel)
	}
	if match.PatientName != "DISPLAY^PATIENT" {
		t.Fatalf("PatientName = %q, want DISPLAY^PATIENT", match.PatientName)
	}
	if match.PatientID != "P001" {
		t.Fatalf("PatientID = %q, want P001", match.PatientID)
	}
	if match.PatientBirthDate != "19700102" {
		t.Fatalf("PatientBirthDate = %q, want 19700102", match.PatientBirthDate)
	}
	if match.PatientComments != "remote comment" {
		t.Fatalf("PatientComments = %q, want remote comment", match.PatientComments)
	}
}

func TestStudyRootSeriesFindAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "FINDSCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootFindSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootFindSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCFind(ctx, assoc, pc.ID, dimse.CFindHandlerFunc(func(_ context.Context, req dimse.CFindRequestContext) ([]*object.Object, error) {
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelSeries {
				return nil, errors.New("expected SERIES query level")
			}
			studyUID, _ := req.Identifier.GetString(tagStudyInstanceUID)
			if studyUID != "1.2.3.study" {
				return nil, errors.New("expected StudyInstanceUID=1.2.3.study")
			}
			modality, _ := req.Identifier.GetString(tagModality)
			if modality != "CT" {
				return nil, errors.New("expected Modality=CT")
			}
			return []*object.Object{
				seriesMatch("FIND^PATIENT", "P001", "1.2.3.study", "1.2.3.series", "CT", "7", "Series query"),
			}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	node := nodes.Node{
		Name:    "findscp",
		AETitle: "FINDSCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	result, err := StudyRootSeriesFind(ctx, node, SeriesCriteria{
		StudyInstanceUID: "1.2.3.study",
		Modality:         "CT",
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("len(Matches) = %d, want 1", len(result.Matches))
	}
	match := result.Matches[0]
	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelSeries {
		t.Fatalf("QueryRetrieveLevel = %q, want SERIES", match.QueryRetrieveLevel)
	}
	if match.SeriesInstanceUID != "1.2.3.series" {
		t.Fatalf("SeriesInstanceUID = %q, want 1.2.3.series", match.SeriesInstanceUID)
	}
	if match.Modality != "CT" {
		t.Fatalf("Modality = %q, want CT", match.Modality)
	}
	if match.SeriesNumber != "7" {
		t.Fatalf("SeriesNumber = %q, want 7", match.SeriesNumber)
	}
	if match.SeriesDescription != "Series query" {
		t.Fatalf("SeriesDescription = %q, want Series query", match.SeriesDescription)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestStudyRootImageFindAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "FINDSCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootFindSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootFindSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCFind(ctx, assoc, pc.ID, dimse.CFindHandlerFunc(func(_ context.Context, req dimse.CFindRequestContext) ([]*object.Object, error) {
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelImage {
				return nil, errors.New("expected IMAGE query level")
			}
			studyUID, _ := req.Identifier.GetString(tagStudyInstanceUID)
			if studyUID != "1.2.3.study" {
				return nil, errors.New("expected StudyInstanceUID=1.2.3.study")
			}
			seriesUID, _ := req.Identifier.GetString(tagSeriesInstanceUID)
			if seriesUID != "1.2.3.series" {
				return nil, errors.New("expected SeriesInstanceUID=1.2.3.series")
			}
			sopUID, _ := req.Identifier.GetString(tagSOPInstanceUID)
			if sopUID != "1.2.3.instance" {
				return nil, errors.New("expected SOPInstanceUID=1.2.3.instance")
			}
			return []*object.Object{
				imageMatch("FIND^PATIENT", "P001", "1.2.3.study", "1.2.3.series", testImageStorageSOPClassUID, "1.2.3.instance", "CT", "12"),
			}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	node := nodes.Node{
		Name:    "findscp",
		AETitle: "FINDSCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	result, err := StudyRootImageFind(ctx, node, ImageCriteria{
		StudyInstanceUID:  "1.2.3.study",
		SeriesInstanceUID: "1.2.3.series",
		SOPInstanceUID:    "1.2.3.instance",
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("len(Matches) = %d, want 1", len(result.Matches))
	}
	match := result.Matches[0]
	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelImage {
		t.Fatalf("QueryRetrieveLevel = %q, want IMAGE", match.QueryRetrieveLevel)
	}
	if match.SOPClassUID != testImageStorageSOPClassUID {
		t.Fatalf("SOPClassUID = %q, want %q", match.SOPClassUID, testImageStorageSOPClassUID)
	}
	if match.SOPInstanceUID != "1.2.3.instance" {
		t.Fatalf("SOPInstanceUID = %q, want 1.2.3.instance", match.SOPInstanceUID)
	}
	if match.InstanceNumber != "12" {
		t.Fatalf("InstanceNumber = %q, want 12", match.InstanceNumber)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestStudyDateRange(t *testing.T) {
	tests := []struct {
		from string
		to   string
		want string
	}{
		{"", "", ""},
		{"20260101", "", "20260101-"},
		{"", "20261231", "-20261231"},
		{"20260101", "20261231", "20260101-20261231"},
		{"20260101", "20260101", "20260101"},
	}
	for _, tt := range tests {
		if got := studyDateRange(tt.from, tt.to); got != tt.want {
			t.Fatalf("studyDateRange(%q, %q) = %q, want %q", tt.from, tt.to, got, tt.want)
		}
	}
}

func studyMatch(patientName, patientID, modalities, studyUID string) *object.Object {
	return object.FromElements([]core.Element{
		stringElement(tagPatientName, core.VRPN, patientName),
		stringElement(tagPatientID, core.VRLO, patientID),
		stringElement(tagStudyDate, core.VRDA, "20260604"),
		stringElement(tagStudyDescription, core.VRLO, "Query test"),
		stringElement(tagAccessionNumber, core.VRSH, "ACC-QUERY"),
		stringElement(tagStudyInstanceUID, core.VRUI, studyUID),
		stringElement(tagModalitiesInStudy, core.VRCS, modalities),
	}, std.Dictionary)
}

func patientMatch(patientName, patientID, birthDate, comments string) *object.Object {
	return object.FromElements([]core.Element{
		stringElement(tagPatientName, core.VRPN, patientName),
		stringElement(tagPatientID, core.VRLO, patientID),
		stringElement(tagPatientBirthDate, core.VRDA, birthDate),
		stringElement(tagPatientComments, core.VRLT, comments),
	}, std.Dictionary)
}

func seriesMatch(patientName, patientID, studyUID, seriesUID, modality, seriesNumber, seriesDescription string) *object.Object {
	return object.FromElements([]core.Element{
		stringElement(tagPatientName, core.VRPN, patientName),
		stringElement(tagPatientID, core.VRLO, patientID),
		stringElement(tagStudyDate, core.VRDA, "20260604"),
		stringElement(tagStudyInstanceUID, core.VRUI, studyUID),
		stringElement(tagSeriesInstanceUID, core.VRUI, seriesUID),
		stringElement(tagModality, core.VRCS, modality),
		stringElement(tagSeriesNumber, core.VRIS, seriesNumber),
		stringElement(tagSeriesDescription, core.VRLO, seriesDescription),
	}, std.Dictionary)
}

const testImageStorageSOPClassUID = "1.2.840.10008.5.1.4.1.1.2"

func imageMatch(patientName, patientID, studyUID, seriesUID, sopClassUID, sopInstanceUID, modality, instanceNumber string) *object.Object {
	return object.FromElements([]core.Element{
		stringElement(tagPatientName, core.VRPN, patientName),
		stringElement(tagPatientID, core.VRLO, patientID),
		stringElement(tagStudyDate, core.VRDA, "20260604"),
		stringElement(tagStudyInstanceUID, core.VRUI, studyUID),
		stringElement(tagSeriesInstanceUID, core.VRUI, seriesUID),
		stringElement(tagSOPClassUID, core.VRUI, sopClassUID),
		stringElement(tagSOPInstanceUID, core.VRUI, sopInstanceUID),
		stringElement(tagModality, core.VRCS, modality),
		stringElement(tagInstanceNumber, core.VRIS, instanceNumber),
	}, std.Dictionary)
}

func stringElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: vr},
		Value:  core.StringValue{value},
	}
}
