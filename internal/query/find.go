package query

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nettimeout"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const DefaultTimeout = 20 * time.Second

var (
	tagPatientName                    = core.NewTag(0x0010, 0x0010)
	tagPatientID                      = core.NewTag(0x0010, 0x0020)
	tagPatientBirthDate               = core.NewTag(0x0010, 0x0030)
	tagPatientComments                = core.NewTag(0x0010, 0x4000)
	tagInstitutionName                = core.NewTag(0x0008, 0x0080)
	tagReferringPhysicianName         = core.NewTag(0x0008, 0x0090)
	tagStudyDate                      = core.NewTag(0x0008, 0x0020)
	tagStudyTime                      = core.NewTag(0x0008, 0x0030)
	tagStudyDescription               = core.NewTag(0x0008, 0x1030)
	tagAccessionNumber                = core.NewTag(0x0008, 0x0050)
	tagModality                       = core.NewTag(0x0008, 0x0060)
	tagStudyInstanceUID               = core.NewTag(0x0020, 0x000D)
	tagSeriesInstanceUID              = core.NewTag(0x0020, 0x000E)
	tagSeriesNumber                   = core.NewTag(0x0020, 0x0011)
	tagSeriesDescription              = core.NewTag(0x0008, 0x103E)
	tagNumberOfStudyRelatedInstances  = core.NewTag(0x0020, 0x1208)
	tagNumberOfSeriesRelatedInstances = core.NewTag(0x0020, 0x1209)
	tagSOPClassUID                    = core.NewTag(0x0008, 0x0016)
	tagSOPInstanceUID                 = core.NewTag(0x0008, 0x0018)
	tagInstanceNumber                 = core.NewTag(0x0020, 0x0013)
	tagModalitiesInStudy              = core.NewTag(0x0008, 0x0061)
	tagStudyStatusID                  = core.NewTag(0x0032, 0x000A)
)

type Criteria struct {
	PatientName            string
	PatientID              string
	PatientBirthDate       string
	StudyDateFrom          string
	StudyDateTo            string
	StudyTimeFrom          string
	StudyTimeTo            string
	StudyDescription       string
	AccessionNumber        string
	ReferringPhysicianName string
	InstitutionName        string
	PatientComments        string
	StudyStatusID          string
	Modality               string
	StudyInstanceUID       string
	CustomFieldKeyword     string
	CustomFieldValue       string
	MaxResults             int
}

type PatientCriteria struct {
	PatientName      string
	PatientID        string
	PatientBirthDate string
	PatientComments  string
	MaxResults       int
}

type SeriesCriteria struct {
	PatientName       string
	PatientID         string
	StudyDateFrom     string
	StudyDateTo       string
	StudyInstanceUID  string
	SeriesInstanceUID string
	Modality          string
	SeriesNumber      string
	SeriesDescription string
	MaxResults        int
}

type ImageCriteria struct {
	PatientName       string
	PatientID         string
	StudyDateFrom     string
	StudyDateTo       string
	StudyInstanceUID  string
	SeriesInstanceUID string
	SOPInstanceUID    string
	SOPClassUID       string
	Modality          string
	InstanceNumber    string
	MaxResults        int
}

type Match struct {
	QueryRetrieveLevel     string
	PatientName            string
	PatientID              string
	PatientBirthDate       string
	StudyDate              string
	StudyTime              string
	ImageCount             string
	StudyDescription       string
	AccessionNumber        string
	ReferringPhysicianName string
	InstitutionName        string
	LocalState             string
	LocalComments          string
	PatientComments        string
	StudyStatusID          string
	StudyInstanceUID       string
	SeriesInstanceUID      string
	Modality               string
	Modalities             string
	SeriesNumber           string
	SeriesDescription      string
	SOPClassUID            string
	SOPInstanceUID         string
	InstanceNumber         string
	SourceNodeID           string
	SourceNodeName         string
	SourceAETitle          string
	SourceHost             string
	SourcePort             uint16
	Status                 uint16
}

type Result struct {
	Matches     []Match
	FinalStatus uint16
	Duration    time.Duration
}

func StudyRootFind(ctx context.Context, node nodes.Node, criteria Criteria, callingAETitle string) (Result, error) {
	identifier, err := studyRootStudyIdentifier(criteria)
	if err != nil {
		return Result{}, err
	}
	return studyRootFind(ctx, node, callingAETitle, identifier, criteria.MaxResults, func(identifier *object.Object, status uint16) Match {
		return studyMatchFromIdentifier(identifier, status)
	})
}

func PatientRootFind(ctx context.Context, node nodes.Node, criteria PatientCriteria, callingAETitle string) (Result, error) {
	identifier, err := patientRootPatientIdentifier(criteria)
	if err != nil {
		return Result{}, err
	}
	return find(ctx, node, callingAETitle, findModel{
		name:                "Patient Root FIND",
		sopClassUID:         dimse.PatientRootFindSOPClassUID,
		presentationContext: dimse.PatientRootFindPresentationContext(),
	}, identifier, criteria.MaxResults, func(identifier *object.Object, status uint16) Match {
		return patientMatchFromIdentifier(identifier, status)
	})
}

func StudyRootSeriesFind(ctx context.Context, node nodes.Node, criteria SeriesCriteria, callingAETitle string) (Result, error) {
	identifier, err := studyRootSeriesIdentifier(criteria)
	if err != nil {
		return Result{}, err
	}
	return studyRootFind(ctx, node, callingAETitle, identifier, criteria.MaxResults, func(identifier *object.Object, status uint16) Match {
		return seriesMatchFromIdentifier(identifier, status)
	})
}

func StudyRootImageFind(ctx context.Context, node nodes.Node, criteria ImageCriteria, callingAETitle string) (Result, error) {
	identifier, err := studyRootImageIdentifier(criteria)
	if err != nil {
		return Result{}, err
	}
	return studyRootFind(ctx, node, callingAETitle, identifier, criteria.MaxResults, func(identifier *object.Object, status uint16) Match {
		return imageMatchFromIdentifier(identifier, status)
	})
}

func studyRootFind(ctx context.Context, node nodes.Node, callingAETitle string, identifier *object.Object, maxResults int, match func(*object.Object, uint16) Match) (Result, error) {
	return find(ctx, node, callingAETitle, findModel{
		name:                "Study Root FIND",
		sopClassUID:         dimse.StudyRootFindSOPClassUID,
		presentationContext: dimse.StudyRootFindPresentationContext(),
	}, identifier, maxResults, match)
}

type findModel struct {
	name                string
	sopClassUID         string
	presentationContext ul.PresentationContext
}

func find(ctx context.Context, node nodes.Node, callingAETitle string, model findModel, identifier *object.Object, maxResults int, match func(*object.Object, uint16) Match) (Result, error) {
	if callingAETitle == "" {
		callingAETitle = netverify.DefaultCallingAETitle
	}
	callingAETitle = nodes.NormalizeAETitle(callingAETitle)
	if err := nodes.ValidateAETitle(callingAETitle); err != nil {
		return Result{}, fmt.Errorf("calling AE title: %w", err)
	}
	ctx, cancel := nettimeout.WithDefault(ctx, DefaultTimeout)
	defer cancel()

	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	start := time.Now()
	tlsConfig, err := netverify.TLSConfigForNode(node)
	if err != nil {
		return Result{}, err
	}
	assoc, err := ul.DialContext(ctx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: callingAETitle,
		Contexts:       []ul.PresentationContext{model.presentationContext},
		TLSConfig:      tlsConfig,
	})
	if err != nil {
		return Result{}, fmt.Errorf("associate with %s (%s): %w", node.Name, address, err)
	}
	released := false
	defer func() {
		if !released {
			_ = assoc.Close()
		}
	}()

	pc, err := dimse.AcceptedContextForSOPClass(assoc, model.sopClassUID)
	if err != nil {
		return Result{}, fmt.Errorf("no accepted %s presentation context: %w", model.name, err)
	}

	identifierSyntax := transfer.ImplicitVRLittleEndian
	if pc.TransferSyntaxUID != "" {
		if syntax, ok := transfer.DefaultRegistry.Get(pc.TransferSyntaxUID); ok {
			identifierSyntax = syntax
		} else {
			identifierSyntax = transfer.Syntax{UID: pc.TransferSyntaxUID}
		}
	}
	results, errs := dimse.Find(ctx, assoc, pc.ID, dimse.CFindRequest{
		AffectedSOPClassUID: model.sopClassUID,
		MessageID:           1,
		Priority:            dimse.PriorityMedium,
	}, identifier, identifierSyntax)

	var out Result
	for result := range results {
		out.FinalStatus = result.Status()
		if dimse.CategorizeCFindStatus(result.Status()) != dimse.CFindStatusPending {
			continue
		}
		if result.Identifier == nil {
			continue
		}
		if maxResults <= 0 || len(out.Matches) < maxResults {
			out.Matches = append(out.Matches, match(result.Identifier, result.Status()))
		}
	}
	if err := <-errs; err != nil {
		return Result{}, err
	}

	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), netverify.DefaultReleaseTimeout)
	defer cancelRelease()
	if err := assoc.Release(releaseCtx); err != nil {
		return Result{}, fmt.Errorf("release association with %s (%s): %w", node.Name, address, err)
	}
	released = true
	out.Duration = time.Since(start)
	return out, nil
}

func patientRootPatientIdentifier(criteria PatientCriteria) (*object.Object, error) {
	keys := map[string]string{}
	addKey(keys, "PatientName", criteria.PatientName)
	addKey(keys, "PatientID", criteria.PatientID)
	addKey(keys, "PatientBirthDate", criteria.PatientBirthDate)
	addKey(keys, "PatientComments", criteria.PatientComments)

	returnKeys := []string{
		"PatientName",
		"PatientID",
		"PatientBirthDate",
		"PatientComments",
	}
	var missingReturnKeys []string
	for _, key := range returnKeys {
		if _, exists := keys[key]; !exists {
			missingReturnKeys = append(missingReturnKeys, key)
		}
	}
	elements, err := dimse.BuildPatientRootPatientFindKeys(keys, missingReturnKeys...)
	if err != nil {
		return nil, err
	}
	return object.FromElements(elements, std.Dictionary), nil
}

func studyRootStudyIdentifier(criteria Criteria) (*object.Object, error) {
	keys := map[string]string{}
	addKey(keys, "PatientName", criteria.PatientName)
	addKey(keys, "PatientID", criteria.PatientID)
	addKey(keys, "PatientBirthDate", criteria.PatientBirthDate)
	addKey(keys, "StudyDate", studyDateRange(criteria.StudyDateFrom, criteria.StudyDateTo))
	addKey(keys, "StudyTime", studyTimeRange(criteria.StudyTimeFrom, criteria.StudyTimeTo))
	addKey(keys, "StudyDescription", criteria.StudyDescription)
	addKey(keys, "AccessionNumber", criteria.AccessionNumber)
	addKey(keys, "ReferringPhysicianName", criteria.ReferringPhysicianName)
	addKey(keys, "InstitutionName", criteria.InstitutionName)
	addKey(keys, "PatientComments", criteria.PatientComments)
	addKey(keys, "StudyStatusID", criteria.StudyStatusID)
	addKey(keys, "ModalitiesInStudy", criteria.Modality)
	addKey(keys, "StudyInstanceUID", criteria.StudyInstanceUID)
	if strings.TrimSpace(criteria.CustomFieldValue) != "" && strings.TrimSpace(criteria.CustomFieldKeyword) == "" {
		return nil, fmt.Errorf("custom DICOM field keyword is required")
	}
	addKey(keys, criteria.CustomFieldKeyword, criteria.CustomFieldValue)

	returnKeys := []string{
		"PatientName",
		"PatientID",
		"PatientBirthDate",
		"StudyDate",
		"StudyTime",
		"NumberOfStudyRelatedInstances",
		"StudyDescription",
		"AccessionNumber",
		"ReferringPhysicianName",
		"InstitutionName",
		"PatientComments",
		"StudyStatusID",
		"StudyInstanceUID",
		"ModalitiesInStudy",
	}
	var missingReturnKeys []string
	for _, key := range returnKeys {
		if _, exists := keys[key]; !exists {
			missingReturnKeys = append(missingReturnKeys, key)
		}
	}
	elements, err := dimse.BuildStudyRootStudyFindKeys(keys, missingReturnKeys...)
	if err != nil {
		return nil, err
	}
	return object.FromElements(elements, std.Dictionary), nil
}

func studyRootSeriesIdentifier(criteria SeriesCriteria) (*object.Object, error) {
	if strings.TrimSpace(criteria.StudyInstanceUID) == "" {
		return nil, fmt.Errorf("study instance UID is required for series query")
	}
	keys := map[string]string{
		"StudyInstanceUID":  strings.TrimSpace(criteria.StudyInstanceUID),
		"SeriesInstanceUID": strings.TrimSpace(criteria.SeriesInstanceUID),
	}
	addKey(keys, "PatientName", criteria.PatientName)
	addKey(keys, "PatientID", criteria.PatientID)
	addKey(keys, "StudyDate", studyDateRange(criteria.StudyDateFrom, criteria.StudyDateTo))
	addKey(keys, "Modality", criteria.Modality)
	addKey(keys, "SeriesNumber", criteria.SeriesNumber)
	addKey(keys, "SeriesDescription", criteria.SeriesDescription)

	returnKeys := []string{
		"PatientName",
		"PatientID",
		"StudyDate",
		"StudyInstanceUID",
		"SeriesInstanceUID",
		"Modality",
		"SeriesNumber",
		"SeriesDescription",
		"NumberOfSeriesRelatedInstances",
	}
	var missingReturnKeys []string
	for _, key := range returnKeys {
		if _, exists := keys[key]; !exists {
			missingReturnKeys = append(missingReturnKeys, key)
		}
	}
	elements, err := dimse.BuildStudyRootSeriesFindKeys(keys, missingReturnKeys...)
	if err != nil {
		return nil, err
	}
	return object.FromElements(elements, std.Dictionary), nil
}

func studyRootImageIdentifier(criteria ImageCriteria) (*object.Object, error) {
	if strings.TrimSpace(criteria.StudyInstanceUID) == "" {
		return nil, fmt.Errorf("study instance UID is required for image query")
	}
	if strings.TrimSpace(criteria.SeriesInstanceUID) == "" {
		return nil, fmt.Errorf("series instance UID is required for image query")
	}
	keys := map[string]string{
		"StudyInstanceUID":  strings.TrimSpace(criteria.StudyInstanceUID),
		"SeriesInstanceUID": strings.TrimSpace(criteria.SeriesInstanceUID),
		"SOPInstanceUID":    strings.TrimSpace(criteria.SOPInstanceUID),
	}
	addKey(keys, "PatientName", criteria.PatientName)
	addKey(keys, "PatientID", criteria.PatientID)
	addKey(keys, "StudyDate", studyDateRange(criteria.StudyDateFrom, criteria.StudyDateTo))
	addKey(keys, "Modality", criteria.Modality)
	addKey(keys, "SOPClassUID", criteria.SOPClassUID)
	addKey(keys, "InstanceNumber", criteria.InstanceNumber)

	returnKeys := []string{
		"PatientName",
		"PatientID",
		"StudyDate",
		"StudyInstanceUID",
		"SeriesInstanceUID",
		"SOPClassUID",
		"SOPInstanceUID",
		"Modality",
		"InstanceNumber",
	}
	var missingReturnKeys []string
	for _, key := range returnKeys {
		if _, exists := keys[key]; !exists {
			missingReturnKeys = append(missingReturnKeys, key)
		}
	}
	elements, err := dimse.BuildStudyRootImageFindKeys(keys, missingReturnKeys...)
	if err != nil {
		return nil, err
	}
	return object.FromElements(elements, std.Dictionary), nil
}

func addKey(keys map[string]string, keyword, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	keys[keyword] = value
}

func studyDateRange(from, to string) string {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	switch {
	case from == "" && to == "":
		return ""
	case from == to:
		return from
	default:
		return from + "-" + to
	}
}

func studyTimeRange(from, to string) string {
	return studyDateRange(from, to)
}

func studyMatchFromIdentifier(identifier *object.Object, status uint16) Match {
	return Match{
		QueryRetrieveLevel:     dimse.QueryRetrieveLevelStudy,
		PatientName:            getString(identifier, tagPatientName),
		PatientID:              getString(identifier, tagPatientID),
		PatientBirthDate:       getString(identifier, tagPatientBirthDate),
		StudyDate:              getString(identifier, tagStudyDate),
		StudyTime:              getString(identifier, tagStudyTime),
		ImageCount:             getString(identifier, tagNumberOfStudyRelatedInstances),
		StudyDescription:       getString(identifier, tagStudyDescription),
		AccessionNumber:        getString(identifier, tagAccessionNumber),
		ReferringPhysicianName: getString(identifier, tagReferringPhysicianName),
		InstitutionName:        getString(identifier, tagInstitutionName),
		PatientComments:        getString(identifier, tagPatientComments),
		StudyStatusID:          getString(identifier, tagStudyStatusID),
		StudyInstanceUID:       getString(identifier, tagStudyInstanceUID),
		Modalities:             strings.Join(getStrings(identifier, tagModalitiesInStudy), "\\"),
		Status:                 status,
	}
}

func patientMatchFromIdentifier(identifier *object.Object, status uint16) Match {
	return Match{
		QueryRetrieveLevel: dimse.QueryRetrieveLevelPatient,
		PatientName:        getString(identifier, tagPatientName),
		PatientID:          getString(identifier, tagPatientID),
		PatientBirthDate:   getString(identifier, tagPatientBirthDate),
		PatientComments:    getString(identifier, tagPatientComments),
		Status:             status,
	}
}

func seriesMatchFromIdentifier(identifier *object.Object, status uint16) Match {
	return Match{
		QueryRetrieveLevel:     dimse.QueryRetrieveLevelSeries,
		PatientName:            getString(identifier, tagPatientName),
		PatientID:              getString(identifier, tagPatientID),
		PatientBirthDate:       getString(identifier, tagPatientBirthDate),
		StudyDate:              getString(identifier, tagStudyDate),
		StudyTime:              getString(identifier, tagStudyTime),
		ImageCount:             getString(identifier, tagNumberOfSeriesRelatedInstances),
		ReferringPhysicianName: getString(identifier, tagReferringPhysicianName),
		InstitutionName:        getString(identifier, tagInstitutionName),
		StudyInstanceUID:       getString(identifier, tagStudyInstanceUID),
		SeriesInstanceUID:      getString(identifier, tagSeriesInstanceUID),
		Modality:               getString(identifier, tagModality),
		SeriesNumber:           getString(identifier, tagSeriesNumber),
		SeriesDescription:      getString(identifier, tagSeriesDescription),
		Status:                 status,
	}
}

func imageMatchFromIdentifier(identifier *object.Object, status uint16) Match {
	return Match{
		QueryRetrieveLevel:     dimse.QueryRetrieveLevelImage,
		PatientName:            getString(identifier, tagPatientName),
		PatientID:              getString(identifier, tagPatientID),
		PatientBirthDate:       getString(identifier, tagPatientBirthDate),
		StudyDate:              getString(identifier, tagStudyDate),
		StudyTime:              getString(identifier, tagStudyTime),
		ReferringPhysicianName: getString(identifier, tagReferringPhysicianName),
		InstitutionName:        getString(identifier, tagInstitutionName),
		StudyInstanceUID:       getString(identifier, tagStudyInstanceUID),
		SeriesInstanceUID:      getString(identifier, tagSeriesInstanceUID),
		Modality:               getString(identifier, tagModality),
		SOPClassUID:            getString(identifier, tagSOPClassUID),
		SOPInstanceUID:         getString(identifier, tagSOPInstanceUID),
		InstanceNumber:         getString(identifier, tagInstanceNumber),
		Status:                 status,
	}
}

func getString(obj *object.Object, tag core.Tag) string {
	value, ok := obj.GetString(tag)
	if !ok {
		return ""
	}
	return value
}

func getStrings(obj *object.Object, tag core.Tag) []string {
	values, ok := obj.GetStrings(tag)
	if !ok {
		return nil
	}
	return values
}
