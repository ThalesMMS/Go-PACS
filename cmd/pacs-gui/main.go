package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
	studyexport "github.com/ThalesMMS/Go-PACS/internal/export"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

const maxTaskHistory = ops.MaxHistoryEntries
const defaultWindowWidth float32 = 1600
const defaultWindowHeight float32 = 900

var queryModalityCodes = []string{
	"CR", "CT", "MG", "XA", "RF", "NM", "DX", "ES", "PT",
	"SR", "SC", "MR", "AU", "OT", "RG", "DR", "XC", "VL", "US",
}

const (
	queryDatePresetAny                = "Any date"
	queryDatePresetOn                 = "On"
	queryDatePresetBetween            = "Between"
	queryDatePresetTodayAM            = "Today AM"
	queryDatePresetTodayPM            = "Today PM"
	queryDatePresetToday              = "Today"
	queryDatePresetYesterday          = "Yesterday"
	queryDatePresetDayBeforeYesterday = "Day Before Yesterday"
	queryDatePresetLast2Days          = "Last 2 days"
	queryDatePresetLast7Days          = "Last 7 days"
	queryDatePresetLastMonth          = "Last month"
	queryDatePresetLast3Months        = "Last 3 months"
	queryDatePresetLast30Min          = "Last 30 min"
	queryDatePresetLast1Hour          = "Last 1 hour"
	queryDatePresetLast2Hours         = "Last 2 hours"
	queryDatePresetLast3Hours         = "Last 3 hours"
	queryDatePresetLast6Hours         = "Last 6 hours"
	queryDatePresetLast8Hours         = "Last 8 hours"
	queryDatePresetLast12Hours        = "Last 12 hours"
	queryDatePresetLast24Hours        = "Last 24 hours"
	queryDatePresetLastNHours         = "Last N hours"
)

const (
	queryQuickSearchPatientName        = "Name"
	queryQuickSearchPatientID          = "Patient ID"
	queryQuickSearchAccession          = "Accession Number"
	queryQuickSearchBirthdate          = "Birthdate"
	queryQuickSearchDescription        = "Description"
	queryQuickSearchReferringPhysician = "Referring Physician"
	queryQuickSearchInstitution        = "Institution"
	queryQuickSearchComments           = "Comments"
	queryQuickSearchCustomDICOMField   = "Custom DICOM field"
	queryQuickSearchStatus             = "Status"
)

const (
	queryWorkspaceTitle      = "DICOM Query/Retrieve"
	queryActionLabelQuery    = "Query"
	queryActionLabelPatient  = "Query Patient"
	queryActionLabelSeries   = "Series"
	queryActionLabelImages   = "Images"
	queryActionLabelRetrieve = "Retrieve"
	queryActionLabelVerify   = "Verify"
	dicomNodesTitle          = "DICOM Nodes:"
	dicomNodesInstruction    = "Drag sources into the priority order for retrieving"
)

const (
	toolbarLabelOpen           = "Open"
	toolbarLabelInspect        = "Inspect"
	toolbarLabelImport         = "Import"
	toolbarLabelExport         = "Export"
	toolbarLabelFolder         = "Folder"
	toolbarLabelRefresh        = "Refresh"
	toolbarLabelQuery          = "Query"
	toolbarLabelSendStudy      = "Send Study"
	toolbarLabelSendSeries     = "Send Series"
	toolbarLabelSendImage      = "Send Image"
	toolbarLabelRetrieveSeries = "Get Series"
	toolbarLabelRetrieveImage  = "Get Image"
	toolbarLabelCancel         = "Cancel"
	toolbarLabelAnonymize      = "Anonymize"
	toolbarLabelMetaData       = "Meta-Data"
	toolbarLabelAdd            = "Add"
	toolbarLabelEdit           = "Edit"
	toolbarLabelDelete         = "Delete"
	toolbarLabelVerify         = "Verify"
	toolbarLabelListen         = "Listen"
	toolbarLabelStop           = "Stop"
	toolbarLabelSettings       = "Settings"
)

const networkTabTitle = "Network"

const (
	networkActionLabelAll        = "All"
	networkActionLabelNone       = "None"
	networkActionLabelSave       = "Save..."
	networkActionLabelLoad       = "Load..."
	networkActionLabelVerify     = "Verify"
	networkActionLabelAddNewNode = "Add new node"
	networkActionLabelEdit       = "Edit"
	networkActionLabelDelete     = "Delete"
)

const (
	queryRefreshModeDont    = "Don't refresh"
	queryRefreshButtonLabel = "Refresh"
	queryAutoRetrieveLabel  = "Auto-Retrieve"
	queryKeepOnTopLabel     = "Keep this window on top of all other windows"
)

const (
	autoQueryTabTitle           = "Auto Q/R"
	autoQueryProfileDefault     = "Default Instance"
	autoQueryRefreshEvery30Min  = "Refresh every 30 min"
	autoQueryCountdownDormant   = "Next: --:--"
	autoQuerySettingsButtonText = "Settings"
)

const (
	autoQueryRetrieveLevelStudy           = "Study"
	autoQueryRetrieveLevelSeries          = "Series"
	autoQueryRetrieveLevelImage           = "Image"
	autoQueryDefaultMaxMatches            = "25"
	autoQueryDuplicatePolicySkipExisting  = "Skip existing"
	autoQueryDuplicatePolicyKeepDuplicate = "Keep duplicate"
	autoQueryDuplicatePolicyReject        = "Reject duplicates"
)

const (
	settingsLabelDICOMCommunicationTimeout = "DICOM Communications Timeout (s)"
	settingsLabelDICOMConnectionTimeout    = "Connection Timeout (s)"
	receivePreferredSyntaxAutoLabel        = "Auto"
	receivePreferredSyntaxExplicitLabel    = "Explicit VR Little Endian"
	receivePreferredSyntaxImplicitLabel    = "Implicit VR Little Endian"
	sendSyntaxAutoLabel                    = "Auto"
	sendSyntaxExplicitLabel                = "Explicit VR Little Endian"
	sendSyntaxImplicitLabel                = "Implicit VR Little Endian"
	studyStatusPresetCustomLabel           = "Custom"
	studyStatusPresetReviewedLabel         = "Reviewed"
	studyStatusPresetInterestingLabel      = "Interesting"
	studyStatusPresetFollowUpLabel         = "Follow-up"
	studyStatusPresetTeachingLabel         = "Teaching"
	studyStatusPresetProblemLabel          = "Problem"
)

type queryRunKind string

const (
	queryRunStudy   queryRunKind = "study"
	queryRunPatient queryRunKind = "patient"
	queryRunSeries  queryRunKind = "series"
	queryRunImage   queryRunKind = "image"
)

type lastQueryRequest struct {
	kind    queryRunKind
	study   query.Criteria
	patient query.PatientCriteria
	series  query.SeriesCriteria
	image   query.ImageCriteria
}

type nodeVerifyStatus string

const (
	nodeVerifyOK   nodeVerifyStatus = "ok"
	nodeVerifyFail nodeVerifyStatus = "fail"
)

type querySourceStatus string

const (
	querySourceOK   querySourceStatus = "ok"
	querySourceFail querySourceStatus = "fail"
)

const queryLocalStatePresent = "local"

const (
	queryLocalStateRetrieved      = "retrieved"
	queryLocalStateRetrieveFailed = "retrieve_failed"
)

var (
	sourceStatusIdleColor = color.NRGBA{R: 92, G: 92, B: 92, A: 255}
	sourceStatusOKColor   = color.NRGBA{R: 66, G: 166, B: 82, A: 255}
	sourceStatusFailColor = color.NRGBA{R: 206, G: 76, B: 70, A: 255}

	queryStatusOKColor          = color.NRGBA{R: 66, G: 166, B: 82, A: 255}
	queryStatusPendingColor     = color.NRGBA{R: 211, G: 166, B: 64, A: 255}
	queryStatusFailColor        = color.NRGBA{R: 206, G: 76, B: 70, A: 255}
	queryLocalStatePresentColor = color.NRGBA{R: 66, G: 166, B: 82, A: 255}

	studyStatusReviewedColor    = color.NRGBA{R: 66, G: 166, B: 82, A: 255}
	studyStatusInterestingColor = color.NRGBA{R: 211, G: 166, B: 64, A: 255}
	studyStatusFollowUpColor    = color.NRGBA{R: 86, G: 148, B: 214, A: 255}
	studyStatusTeachingColor    = color.NRGBA{R: 144, G: 116, B: 206, A: 255}
	studyStatusProblemColor     = color.NRGBA{R: 206, G: 76, B: 70, A: 255}
	studyStatusCustomColor      = color.NRGBA{R: 140, G: 140, B: 140, A: 255}
)

const (
	archiveQuickSearchPatientName = "Patient Name"
	archiveQuickSearchPatientID   = "Patient ID"
	archiveQuickSearchAccession   = "Accession"
)

var queryDatePresetOptions = flattenQueryDatePresetColumns(queryDatePresetColumns())

var queryQuickSearchOptions = []string{
	queryQuickSearchPatientName,
	queryQuickSearchPatientID,
	queryQuickSearchAccession,
	queryQuickSearchBirthdate,
	queryQuickSearchDescription,
	queryQuickSearchReferringPhysician,
	queryQuickSearchComments,
	queryQuickSearchInstitution,
	queryQuickSearchCustomDICOMField,
	queryQuickSearchStatus,
}

var queryRefreshModeOptions = []string{
	queryRefreshModeDont,
}

var autoQueryRefreshModeOptions = []string{
	queryRefreshModeDont,
	autoQueryRefreshEvery30Min,
}

var autoQueryRetrieveLevelOptions = []string{
	autoQueryRetrieveLevelStudy,
	autoQueryRetrieveLevelSeries,
	autoQueryRetrieveLevelImage,
}

var autoQueryDuplicatePolicyOptions = []string{
	autoQueryDuplicatePolicySkipExisting,
	autoQueryDuplicatePolicyKeepDuplicate,
	autoQueryDuplicatePolicyReject,
}

func newQueryAutoRetrieveCheck(state *uiState) *widget.Check {
	check := widget.NewCheck(queryAutoRetrieveLabel, func(enabled bool) {
		if state != nil {
			state.queryAutoRetrieve = enabled
		}
	})
	if state != nil && state.queryAutoRetrieve {
		check.SetChecked(true)
	}
	return check
}

func newQueryKeepOnTopCheck(state *uiState) *widget.Check {
	check := widget.NewCheck(queryKeepOnTopLabel, func(enabled bool) {
		if state != nil {
			state.queryKeepOnTop = enabled
		}
	})
	if state != nil && state.queryKeepOnTop {
		check.SetChecked(true)
	}
	return check
}

func newAutoQueryAutoRetrieveCheck(state *uiState) *widget.Check {
	check := widget.NewCheck(queryAutoRetrieveLabel, func(enabled bool) {
		if state != nil {
			state.autoQueryAutoRetrieve = enabled
		}
	})
	if state != nil && state.autoQueryAutoRetrieve {
		check.SetChecked(true)
	}
	return check
}

type autoQuerySettings struct {
	RetrieveLevel       string
	MaxMatches          string
	DuplicatePolicy     string
	RequireConfirmation bool
}

type autoQueryAutoRetrievePlan struct {
	Enabled              bool
	RequiresConfirmation bool
	Candidates           []query.Match
	SkippedLocal         int
	RejectedLocal        int
	Limited              bool
	Message              string
	Err                  error
}

func autoQuerySettingsForState(state *uiState) autoQuerySettings {
	settings := autoQuerySettings{
		RetrieveLevel:       autoQueryRetrieveLevelStudy,
		MaxMatches:          autoQueryDefaultMaxMatches,
		DuplicatePolicy:     autoQueryDuplicatePolicySkipExisting,
		RequireConfirmation: true,
	}
	if state == nil {
		return settings
	}
	if stringInList(state.autoQueryRetrieveLevel, autoQueryRetrieveLevelOptions) {
		settings.RetrieveLevel = state.autoQueryRetrieveLevel
	}
	if value := strings.TrimSpace(state.autoQueryMaxMatches); value != "" {
		settings.MaxMatches = value
	}
	if stringInList(state.autoQueryDuplicatePolicy, autoQueryDuplicatePolicyOptions) {
		settings.DuplicatePolicy = state.autoQueryDuplicatePolicy
	}
	if state.autoQuerySettingsConfigured {
		settings.RequireConfirmation = state.autoQueryRequireConfirmation
	}
	return settings
}

func planAutoQueryAutoRetrieve(state *uiState, matches []query.Match) autoQueryAutoRetrievePlan {
	if state == nil || !state.autoQueryAutoRetrieve {
		return autoQueryAutoRetrievePlan{Message: "Auto Q/R auto-retrieve is off"}
	}
	settings := autoQuerySettingsForState(state)
	plan := autoQueryAutoRetrievePlan{
		Enabled:              true,
		RequiresConfirmation: settings.RequireConfirmation,
		Message:              "No Auto Q/R retrieve candidates",
	}
	maxMatches, err := parseOptionalMaxResults(settings.MaxMatches)
	if err != nil {
		plan.Err = err
		plan.Message = "Auto Q/R auto-retrieve max matches is invalid"
		return plan
	}
	level := autoQueryRetrieveDICOMLevel(settings.RetrieveLevel)
	seen := map[string]bool{}
	for _, match := range matches {
		if queryLocalStateAvailable(match.LocalState) {
			if settings.DuplicatePolicy == autoQueryDuplicatePolicyReject {
				plan.RejectedLocal++
				continue
			}
			if settings.DuplicatePolicy == autoQueryDuplicatePolicySkipExisting {
				plan.SkippedLocal++
				continue
			}
		}
		candidate, ok := autoQueryRetrieveCandidate(match, level)
		if !ok {
			continue
		}
		key := autoQueryRetrieveCandidateKey(candidate)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if maxMatches == 0 {
			plan.Limited = true
			continue
		}
		if len(plan.Candidates) >= maxMatches {
			plan.Limited = true
			continue
		}
		plan.Candidates = append(plan.Candidates, candidate)
	}
	if plan.RejectedLocal > 0 {
		plan.Candidates = nil
		plan.Err = fmt.Errorf("Auto Q/R auto-retrieve rejected %d local duplicate matches", plan.RejectedLocal)
		plan.Message = "Auto Q/R auto-retrieve stopped by duplicate policy"
		return plan
	}
	if len(plan.Candidates) > 0 {
		plan.Message = fmt.Sprintf("Auto Q/R auto-retrieve planned %d %s retrieves", len(plan.Candidates), strings.ToLower(level))
	}
	return plan
}

func autoQueryMatchesHandler(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) queryMatchesHandler {
	return func() {
		var matches []query.Match
		if state != nil {
			matches = state.queries
		}
		plan := planAutoQueryAutoRetrieve(state, matches)
		if !plan.Enabled {
			return
		}
		if plan.Err != nil {
			if status != nil {
				status.SetText(plan.Message)
			}
			if w != nil {
				dialog.ShowError(plan.Err, w)
			}
			return
		}
		if len(plan.Candidates) == 0 {
			if status != nil {
				status.SetText(plan.Message)
			}
			return
		}
		if plan.RequiresConfirmation {
			if status != nil {
				status.SetText(fmt.Sprintf("Auto Q/R auto-retrieve awaiting confirmation for %d matches", len(plan.Candidates)))
			}
			if w == nil {
				return
			}
			dialog.ShowConfirm("Auto Q/R Auto-Retrieve", autoQueryAutoRetrieveConfirmationMessage(plan), func(ok bool) {
				if ok {
					runAutoQueryAutoRetrieve(w, status, tables, state, plan.Candidates)
				}
			}, w)
			return
		}
		runAutoQueryAutoRetrieve(w, status, tables, state, plan.Candidates)
	}
}

func autoQueryAutoRetrieveConfirmationMessage(plan autoQueryAutoRetrievePlan) string {
	parts := []string{fmt.Sprintf("Retrieve %d matching rows automatically?", len(plan.Candidates))}
	if plan.SkippedLocal > 0 {
		parts = append(parts, fmt.Sprintf("%d local duplicates will be skipped.", plan.SkippedLocal))
	}
	if plan.Limited {
		parts = append(parts, "The configured max matches limit was applied.")
	}
	return strings.Join(parts, "\n")
}

func autoQueryRetrieveDICOMLevel(level string) string {
	switch strings.TrimSpace(level) {
	case autoQueryRetrieveLevelSeries:
		return "SERIES"
	case autoQueryRetrieveLevelImage:
		return "IMAGE"
	default:
		return "STUDY"
	}
}

func autoQueryRetrieveCandidate(match query.Match, level string) (query.Match, bool) {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return query.Match{}, false
	}
	match.QueryRetrieveLevel = level
	switch level {
	case "IMAGE":
		if !queryUIDAvailable(match.SeriesInstanceUID) || !queryUIDAvailable(match.SOPInstanceUID) {
			return query.Match{}, false
		}
	case "SERIES":
		if !queryUIDAvailable(match.SeriesInstanceUID) {
			return query.Match{}, false
		}
		match.SOPInstanceUID = ""
	default:
		match.SeriesInstanceUID = ""
		match.SOPInstanceUID = ""
	}
	return match, true
}

func autoQueryRetrieveCandidateKey(match query.Match) string {
	switch strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel)) {
	case "IMAGE":
		return strings.Join([]string{"IMAGE", strings.TrimSpace(match.StudyInstanceUID), strings.TrimSpace(match.SeriesInstanceUID), strings.TrimSpace(match.SOPInstanceUID)}, "\x00")
	case "SERIES":
		return strings.Join([]string{"SERIES", strings.TrimSpace(match.StudyInstanceUID), strings.TrimSpace(match.SeriesInstanceUID)}, "\x00")
	default:
		return strings.Join([]string{"STUDY", strings.TrimSpace(match.StudyInstanceUID)}, "\x00")
	}
}

func applyAutoQuerySettings(state *uiState, settings autoQuerySettings) {
	if state == nil {
		return
	}
	if !stringInList(settings.RetrieveLevel, autoQueryRetrieveLevelOptions) {
		settings.RetrieveLevel = autoQueryRetrieveLevelStudy
	}
	if strings.TrimSpace(settings.MaxMatches) == "" {
		settings.MaxMatches = autoQueryDefaultMaxMatches
	}
	if !stringInList(settings.DuplicatePolicy, autoQueryDuplicatePolicyOptions) {
		settings.DuplicatePolicy = autoQueryDuplicatePolicySkipExisting
	}
	state.autoQueryRetrieveLevel = settings.RetrieveLevel
	state.autoQueryMaxMatches = strings.TrimSpace(settings.MaxMatches)
	state.autoQueryDuplicatePolicy = settings.DuplicatePolicy
	state.autoQueryRequireConfirmation = settings.RequireConfirmation
	state.autoQuerySettingsConfigured = true
}

func autoQuerySettingsFromProfile(profile autoquery.Profile) autoQuerySettings {
	return autoQuerySettings{
		RetrieveLevel:       profile.Settings.RetrieveLevel,
		MaxMatches:          profile.Settings.MaxMatches,
		DuplicatePolicy:     profile.Settings.DuplicatePolicy,
		RequireConfirmation: profile.Settings.RequireConfirmation,
	}
}

func normalizeAutoQueryProfiles(profiles []autoquery.Profile) []autoquery.Profile {
	var defaultProfile autoquery.Profile
	hasDefault := false
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), autoquery.DefaultProfileName) {
			defaultProfile = profile
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		defaultProfile = autoquery.DefaultProfile()
	}
	defaultProfile.Name = autoquery.DefaultProfileName
	normalized := []autoquery.Profile{defaultProfile}
	seen := map[string]bool{strings.ToLower(autoquery.DefaultProfileName): true}
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		profile.Name = name
		normalized = append(normalized, profile)
		seen[key] = true
	}
	return normalized
}

func autoQueryProfileNames(state *uiState) []string {
	profiles := []autoquery.Profile{autoquery.DefaultProfile()}
	if state != nil && len(state.autoQueryProfiles) > 0 {
		profiles = state.autoQueryProfiles
	}
	profiles = normalizeAutoQueryProfiles(profiles)
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	return names
}

func selectedAutoQueryProfileName(state *uiState) string {
	if state != nil && strings.TrimSpace(state.autoQueryProfileName) != "" {
		return strings.TrimSpace(state.autoQueryProfileName)
	}
	return autoquery.DefaultProfileName
}

func autoQueryProfileLocked(state *uiState) bool {
	return state != nil && state.autoQueryProfileLocked
}

func setAutoQueryProfileLocked(state *uiState, locked bool) {
	if state == nil {
		return
	}
	state.autoQueryProfileLocked = locked
}

func selectAutoQueryProfile(state *uiState, name string) bool {
	if state == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = autoquery.DefaultProfileName
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), name) {
			state.autoQueryProfiles = profiles
			state.autoQueryProfileName = profile.Name
			state.autoQueryProfileLocked = profile.Locked
			state.autoQueryLast = lastQueryRequest{}
			stopAutoQueryRefresh(state)
			applyAutoQuerySettings(state, autoQuerySettingsFromProfile(profile))
			applyAutoQueryCriteria(state, profile.Criteria)
			applyAutoQuerySources(state, profile.Sources)
			return true
		}
	}
	return false
}

func nextAutoQueryProfileName(profiles []autoquery.Profile) string {
	existing := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		existing[strings.ToLower(strings.TrimSpace(profile.Name))] = true
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("Instance %d", i)
		if !existing[strings.ToLower(name)] {
			return name
		}
	}
}

func addAutoQueryProfile(state *uiState) autoquery.Profile {
	if state == nil {
		return autoquery.DefaultProfile()
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	state.autoQueryProfiles = profiles
	profile := autoQueryProfileFromState(state)
	profile.Name = nextAutoQueryProfileName(profiles)
	state.autoQueryProfiles = append(profiles, profile)
	state.autoQueryProfileName = profile.Name
	return profile
}

func removeSelectedAutoQueryProfile(state *uiState) bool {
	if state == nil {
		return false
	}
	selected := selectedAutoQueryProfileName(state)
	if strings.EqualFold(selected, autoquery.DefaultProfileName) {
		return false
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	filtered := profiles[:0]
	removed := false
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), selected) {
			removed = true
			continue
		}
		filtered = append(filtered, profile)
	}
	if !removed {
		return false
	}
	state.autoQueryProfiles = normalizeAutoQueryProfiles(filtered)
	_ = selectAutoQueryProfile(state, autoquery.DefaultProfileName)
	return true
}

func renameSelectedAutoQueryProfile(state *uiState, newName string) error {
	if state == nil {
		return errors.New("Auto Q/R profile state is unavailable")
	}
	current := selectedAutoQueryProfileName(state)
	if strings.EqualFold(current, autoquery.DefaultProfileName) {
		return errors.New("Default Auto Q/R profile cannot be renamed")
	}
	if autoQueryProfileLocked(state) {
		return errors.New("Auto Q/R profile is locked")
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("Auto Q/R profile name cannot be empty")
	}
	if strings.EqualFold(newName, autoquery.DefaultProfileName) {
		return fmt.Errorf("Auto Q/R profile %q already exists", autoquery.DefaultProfileName)
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	index := -1
	for i, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), current) {
			index = i
			continue
		}
		if strings.EqualFold(strings.TrimSpace(profile.Name), newName) {
			return fmt.Errorf("Auto Q/R profile %q already exists", newName)
		}
	}
	if index < 0 {
		return fmt.Errorf("Auto Q/R profile %q not found", current)
	}
	profiles[index].Name = newName
	state.autoQueryProfiles = profiles
	state.autoQueryProfileName = newName
	return nil
}

func saveAutoQueryProfileList(state *uiState) error {
	if state == nil || state.autoQueryProfileStore == nil {
		return nil
	}
	state.autoQueryProfiles = normalizeAutoQueryProfiles(state.autoQueryProfiles)
	return state.autoQueryProfileStore.Save(state.autoQueryProfiles)
}

func autoQuerySourceFromNode(node nodes.Node, enabled bool) autoquery.Source {
	return autoquery.Source{
		NodeID:  strings.TrimSpace(node.ID),
		Name:    strings.TrimSpace(node.Name),
		Host:    strings.TrimSpace(node.Host),
		Port:    node.Port,
		Enabled: enabled,
	}
}

func autoQuerySourceKey(source autoquery.Source) string {
	if strings.TrimSpace(source.NodeID) != "" {
		return "id:" + strings.TrimSpace(source.NodeID)
	}
	return fmt.Sprintf("endpoint:%s:%s:%d", strings.TrimSpace(source.Name), strings.TrimSpace(source.Host), source.Port)
}

func autoQueryNodeKey(node nodes.Node) string {
	if strings.TrimSpace(node.ID) != "" {
		return "id:" + strings.TrimSpace(node.ID)
	}
	return fmt.Sprintf("endpoint:%s:%s:%d", strings.TrimSpace(node.Name), strings.TrimSpace(node.Host), node.Port)
}

func autoQuerySourcesForNodes(sources []autoquery.Source, nodeList []nodes.Node) []autoquery.Source {
	if len(nodeList) == 0 {
		return nil
	}
	indexByKey := make(map[string]int, len(nodeList))
	for index, node := range nodeList {
		indexByKey[autoQueryNodeKey(node)] = index
	}
	var out []autoquery.Source
	seen := make(map[string]bool, len(nodeList))
	for _, source := range sources {
		key := autoQuerySourceKey(source)
		index, ok := indexByKey[key]
		if !ok || seen[key] {
			continue
		}
		out = append(out, autoQuerySourceFromNode(nodeList[index], source.Enabled))
		seen[key] = true
	}
	for _, node := range nodeList {
		key := autoQueryNodeKey(node)
		if seen[key] {
			continue
		}
		out = append(out, autoQuerySourceFromNode(node, querySourceChecked(node)))
		seen[key] = true
	}
	return out
}

func applyAutoQuerySources(state *uiState, sources []autoquery.Source) {
	if state == nil {
		return
	}
	state.autoQuerySources = autoQuerySourcesForNodes(sources, state.nodes)
	state.selectedAutoQuerySourceRow = -1
}

func autoQuerySourcesForState(state *uiState) []autoquery.Source {
	if state == nil {
		return nil
	}
	return autoQuerySourcesForNodes(state.autoQuerySources, state.nodes)
}

func autoQueryCriteriaForState(state *uiState) autoquery.Criteria {
	criteria := autoquery.Criteria{
		SearchField: queryQuickSearchPatientName,
		DatePreset:  queryDatePresetAny,
		RefreshMode: autoQueryRefreshEvery30Min,
	}
	if state == nil {
		return criteria
	}
	if stringInList(state.autoQuerySearchField, queryQuickSearchOptions) {
		criteria.SearchField = state.autoQuerySearchField
	}
	criteria.SearchText = strings.TrimSpace(state.autoQuerySearchText)
	if stringInList(state.autoQueryDatePreset, queryDatePresetOptions) {
		criteria.DatePreset = state.autoQueryDatePreset
	}
	criteria.Modalities = normalizedAutoQueryModalities(state.autoQueryModalities)
	if stringInList(state.autoQueryRefreshMode, autoQueryRefreshModeOptions) {
		criteria.RefreshMode = state.autoQueryRefreshMode
	}
	return criteria
}

func applyAutoQueryCriteria(state *uiState, criteria autoquery.Criteria) {
	if state == nil {
		return
	}
	if !stringInList(criteria.SearchField, queryQuickSearchOptions) {
		criteria.SearchField = queryQuickSearchPatientName
	}
	if !stringInList(criteria.DatePreset, queryDatePresetOptions) {
		criteria.DatePreset = queryDatePresetAny
	}
	if !stringInList(criteria.RefreshMode, autoQueryRefreshModeOptions) {
		criteria.RefreshMode = autoQueryRefreshEvery30Min
	}
	state.autoQuerySearchField = criteria.SearchField
	state.autoQuerySearchText = strings.TrimSpace(criteria.SearchText)
	state.autoQueryDatePreset = criteria.DatePreset
	state.autoQueryModalities = normalizedAutoQueryModalities(criteria.Modalities)
	state.autoQueryRefreshMode = criteria.RefreshMode
}

func autoQueryCriteriaFromControls(field string, search string, datePreset string, modalityChecks map[string]*widget.Check, refreshMode string) autoquery.Criteria {
	return autoquery.Criteria{
		SearchField: field,
		SearchText:  strings.TrimSpace(search),
		DatePreset:  datePreset,
		Modalities:  selectedQueryModalities(modalityChecks),
		RefreshMode: refreshMode,
	}
}

func autoQueryCriteriaEqual(lhs autoquery.Criteria, rhs autoquery.Criteria) bool {
	return lhs.SearchField == rhs.SearchField &&
		strings.TrimSpace(lhs.SearchText) == strings.TrimSpace(rhs.SearchText) &&
		lhs.DatePreset == rhs.DatePreset &&
		lhs.RefreshMode == rhs.RefreshMode &&
		strings.Join(normalizedAutoQueryModalities(lhs.Modalities), "\\") == strings.Join(normalizedAutoQueryModalities(rhs.Modalities), "\\")
}

func selectedQueryModalities(modalityChecks map[string]*widget.Check) []string {
	var selected []string
	for _, code := range queryModalityCodes {
		check := modalityChecks[code]
		if check != nil && check.Checked {
			selected = append(selected, code)
		}
	}
	return selected
}

func normalizedAutoQueryModalities(modalities []string) []string {
	wanted := make(map[string]bool, len(modalities))
	for _, modality := range modalities {
		modality = strings.ToUpper(strings.TrimSpace(modality))
		if modality != "" {
			wanted[modality] = true
		}
	}
	var normalized []string
	for _, code := range queryModalityCodes {
		if wanted[code] {
			normalized = append(normalized, code)
		}
	}
	return normalized
}

func autoQueryProfileFromState(state *uiState) autoquery.Profile {
	settings := autoQuerySettingsForState(state)
	return autoquery.Profile{
		Name:   selectedAutoQueryProfileName(state),
		Locked: autoQueryProfileLocked(state),
		Settings: autoquery.Settings{
			RetrieveLevel:       settings.RetrieveLevel,
			MaxMatches:          settings.MaxMatches,
			DuplicatePolicy:     settings.DuplicatePolicy,
			RequireConfirmation: settings.RequireConfirmation,
		},
		Criteria: autoQueryCriteriaForState(state),
		Sources:  autoQuerySourcesForState(state),
	}
}

func loadAutoQueryProfiles(state *uiState, profiles []autoquery.Profile) {
	if state == nil {
		return
	}
	state.autoQueryProfiles = normalizeAutoQueryProfiles(profiles)
	_ = selectAutoQueryProfile(state, autoquery.DefaultProfileName)
}

func saveAutoQueryDefaultProfile(state *uiState) error {
	return saveAutoQuerySelectedProfile(state)
}

func saveAutoQuerySelectedProfile(state *uiState) error {
	if state == nil || state.autoQueryProfileStore == nil {
		return nil
	}
	current := autoQueryProfileFromState(state)
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	replaced := false
	for i, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), current.Name) {
			profiles[i] = current
			replaced = true
			break
		}
	}
	if !replaced {
		profiles = append(profiles, current)
	}
	state.autoQueryProfiles = profiles
	return state.autoQueryProfileStore.Save(profiles)
}

func saveAutoQueryProfileCriteria(w fyne.Window, status *widget.Label, state *uiState, criteria autoquery.Criteria) bool {
	if autoQueryProfileLocked(state) {
		if autoQueryCriteriaEqual(criteria, autoQueryCriteriaForState(state)) {
			return true
		}
		if status != nil {
			status.SetText("Auto Q/R profile is locked")
		}
		return false
	}
	applyAutoQueryCriteria(state, criteria)
	if err := saveAutoQuerySelectedProfile(state); err != nil {
		if status != nil {
			status.SetText("Auto Q/R profile save failed")
		}
		if w != nil {
			dialog.ShowError(err, w)
		}
		return false
	}
	return true
}

func applyAutoQueryProfileRename(w fyne.Window, status *widget.Label, state *uiState, newName string) bool {
	if err := renameSelectedAutoQueryProfile(state, newName); err != nil {
		if status != nil {
			status.SetText(err.Error())
		}
		if w != nil {
			dialog.ShowError(err, w)
		}
		return false
	}
	if err := saveAutoQueryProfileList(state); err != nil {
		if status != nil {
			status.SetText("Auto Q/R profile rename save failed")
		}
		if w != nil {
			dialog.ShowError(err, w)
		}
		return false
	}
	if status != nil {
		status.SetText("Auto Q/R profile renamed")
	}
	return true
}

func stringInList(value string, list []string) bool {
	for _, item := range list {
		if value == item {
			return true
		}
	}
	return false
}

func autoQuerySettingsFormItems(state *uiState) []*widget.FormItem {
	items, _ := newAutoQuerySettingsForm(state)
	return items
}

func newAutoQuerySettingsForm(state *uiState) ([]*widget.FormItem, func()) {
	settings := autoQuerySettingsForState(state)
	retrieveLevel := widget.NewSelect(autoQueryRetrieveLevelOptions, nil)
	retrieveLevel.SetSelected(settings.RetrieveLevel)
	maxMatches := widget.NewEntry()
	maxMatches.SetText(settings.MaxMatches)
	maxMatches.SetPlaceHolder(autoQueryDefaultMaxMatches)
	duplicatePolicy := widget.NewSelect(autoQueryDuplicatePolicyOptions, nil)
	duplicatePolicy.SetSelected(settings.DuplicatePolicy)
	requireConfirmation := widget.NewCheck("", nil)
	requireConfirmation.SetChecked(settings.RequireConfirmation)
	items := []*widget.FormItem{
		widget.NewFormItem("Retrieve Level", retrieveLevel),
		widget.NewFormItem("Max Matches", maxMatches),
		widget.NewFormItem("Duplicate Policy", duplicatePolicy),
		widget.NewFormItem("Require Confirmation", requireConfirmation),
	}
	apply := func() {
		applyAutoQuerySettings(state, autoQuerySettings{
			RetrieveLevel:       retrieveLevel.Selected,
			MaxMatches:          maxMatches.Text,
			DuplicatePolicy:     duplicatePolicy.Selected,
			RequireConfirmation: requireConfirmation.Checked,
		})
	}
	return items, apply
}

func showAutoQuerySettingsDialog(w fyne.Window, status *widget.Label, state *uiState) {
	items, apply := newAutoQuerySettingsForm(state)
	if w == nil {
		apply()
		if err := saveAutoQueryDefaultProfile(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R settings save failed")
			}
			return
		}
		if status != nil {
			status.SetText("Auto Q/R settings saved")
		}
		return
	}
	form := dialog.NewForm("Auto Q/R Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		apply()
		if err := saveAutoQueryDefaultProfile(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R settings save failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if status != nil {
			status.SetText("Auto Q/R settings saved")
		}
	}, w)
	form.Resize(fyne.NewSize(460, 0))
	form.Show()
}

func queryActionButtonLabels() []string {
	return []string{
		queryActionLabelQuery,
		queryActionLabelPatient,
		queryActionLabelRetrieve,
		queryActionLabelVerify,
	}
}

func queryAdvancedActionButtonLabels() []string {
	return []string{
		queryActionLabelSeries,
		queryActionLabelImages,
	}
}

func configureQueryQuickSearchPlaceholder(entry *widget.Entry, field string) {
	if entry == nil {
		return
	}
	if field == queryQuickSearchCustomDICOMField {
		entry.SetPlaceHolder("StudyID=ABC123")
		return
	}
	entry.SetPlaceHolder("Search")
}

func newQueryQuickSearchFieldStrip(selectWidget *widget.Select, entry *widget.Entry) fyne.CanvasObject {
	buttons := map[string]*widget.Button{}
	refreshButtons := func() {
		if selectWidget == nil {
			return
		}
		for field, button := range buttons {
			if selectWidget.Selected == field {
				button.Importance = widget.HighImportance
			} else {
				button.Importance = widget.LowImportance
			}
			button.Refresh()
		}
	}
	if selectWidget != nil {
		selectWidget.OnChanged = func(field string) {
			configureQueryQuickSearchPlaceholder(entry, field)
			refreshButtons()
		}
	}
	objects := make([]fyne.CanvasObject, 0, len(queryQuickSearchOptions))
	for _, field := range queryQuickSearchOptions {
		field := field
		button := widget.NewButton(field, func() {
			if selectWidget != nil {
				selectWidget.SetSelected(field)
			}
		})
		button.Importance = widget.LowImportance
		buttons[field] = button
		objects = append(objects, button)
	}
	configureQueryQuickSearchPlaceholder(entry, "")
	refreshButtons()
	return container.NewHBox(objects...)
}

func newQuerySearchBar(entry *widget.Entry, submit func()) fyne.CanvasObject {
	if entry != nil {
		entry.OnSubmitted = func(_ string) {
			if submit != nil {
				submit()
			}
		}
	}
	submitButton := widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		if submit != nil {
			submit()
		}
	})
	submitButton.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, widget.NewIcon(theme.SearchIcon()), submitButton, entry)
}

func newQueryRefreshButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), tapped)
	button.Importance = widget.LowImportance
	return button
}

func mainToolbarButtonLabels() []string {
	var labels []string
	for _, group := range mainToolbarButtonGroups() {
		labels = append(labels, group...)
	}
	return labels
}

func mainToolbarButtonGroups() [][]string {
	return [][]string{
		{
			toolbarLabelOpen,
			toolbarLabelInspect,
			toolbarLabelImport,
			toolbarLabelExport,
			toolbarLabelFolder,
			toolbarLabelRefresh,
		},
		{
			toolbarLabelQuery,
			toolbarLabelSendStudy,
			toolbarLabelSendSeries,
			toolbarLabelSendImage,
		},
		{
			toolbarLabelRetrieveSeries,
			toolbarLabelRetrieveImage,
			toolbarLabelCancel,
		},
		{
			toolbarLabelAnonymize,
			toolbarLabelMetaData,
			toolbarLabelAdd,
			toolbarLabelEdit,
			toolbarLabelDelete,
			toolbarLabelVerify,
		},
		{
			toolbarLabelListen,
			toolbarLabelStop,
			toolbarLabelSettings,
		},
	}
}

func mainToolbarDisabledLabels() []string {
	return []string{
		toolbarLabelAnonymize,
	}
}

func compactToolbarButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon(label, icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

func disabledToolbarButton(label string, icon fyne.Resource) *widget.Button {
	button := compactToolbarButton(label, icon, nil)
	button.Disable()
	return button
}

func groupedToolbarActions(buttons map[string]fyne.CanvasObject) *fyne.Container {
	objects := []fyne.CanvasObject{}
	for groupIndex, group := range mainToolbarButtonGroups() {
		if groupIndex > 0 {
			objects = append(objects, widget.NewSeparator())
		}
		for _, label := range group {
			if button := buttons[label]; button != nil {
				objects = append(objects, button)
			}
		}
	}
	return container.NewHBox(objects...)
}

func selectAppTabByText(tabs *container.AppTabs, text string) bool {
	if tabs == nil {
		return false
	}
	for _, item := range tabs.Items {
		if item.Text == text {
			tabs.Select(item)
			return true
		}
	}
	return false
}

var archiveQuickSearchOptions = []string{
	archiveQuickSearchPatientName,
	archiveQuickSearchPatientID,
	archiveQuickSearchAccession,
}

func archiveQuickSearchModeText(field string) string {
	field = strings.TrimSpace(field)
	return "Search by " + archiveQuickSearchPlaceholder(field)
}

func archiveQuickSearchPlaceholder(field string) string {
	field = strings.TrimSpace(field)
	if !stringInList(field, archiveQuickSearchOptions) {
		return archiveQuickSearchPatientName
	}
	return field
}

func main() {
	run()
}

func configureAppAppearance(a fyne.App) {
	if a == nil {
		return
	}
	a.Settings().SetTheme(theme.DarkTheme())
}

func defaultWindowSize() fyne.Size {
	return fyne.NewSize(defaultWindowWidth, defaultWindowHeight)
}

func run() {
	archiveDir := flag.String("archive-dir", defaultArchiveDir(), "directory for the local archive catalog and object store")
	flag.Parse()

	a := app.NewWithID("com.thalesmms.gopacs")
	configureAppAppearance(a)
	w := a.NewWindow("go-pacs")
	w.Resize(defaultWindowSize())

	catalog, err := archive.Open(*archiveDir)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	defer catalog.Close()

	configPath := filepath.Join(*archiveDir, "config.json")
	appCfg, err := appconfig.Load(configPath)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	operationHistoryPath := filepath.Join(*archiveDir, "tasks.json")
	operationHistory, err := ops.LoadHistory(operationHistoryPath)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	nodeStore := nodes.NewStore(filepath.Join(*archiveDir, "nodes.json"))
	nodeList, err := nodeStore.List()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	autoQueryProfileStore := autoquery.NewStore(filepath.Join(*archiveDir, "auto-query-profiles.json"))
	autoQueryProfiles, err := autoQueryProfileStore.List()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	state := &uiState{
		catalog:               catalog,
		nodeStore:             nodeStore,
		autoQueryProfileStore: autoQueryProfileStore,
		nodes:                 nodeList,
		appConfig:             appCfg,
		appConfigPath:         configPath,
		operations:            operationHistory,
		operationHistoryPath:  operationHistoryPath,
	}
	loadAutoQueryProfiles(state, autoQueryProfiles)
	status := widget.NewLabel("Ready")
	status.Wrapping = fyne.TextTruncate

	summary := newSummaryPanel()
	elementTable := newElementTable(state)
	studyTable := newStudyTable(state)
	seriesTable := newSeriesTable(state)
	instanceTable := newInstanceTable(state)
	taskTable := newTaskTable(state)
	taskDetail := newTaskDetail()
	state.operationTable = taskTable
	state.operationDetail = taskDetail
	updateTaskDetail(state)
	tables := archiveTables{
		studies:   studyTable,
		series:    seriesTable,
		instances: instanceTable,
	}
	wireArchiveTables(w, status, tables, state)
	nodeTable := newNodeTable(status, state)
	queryTab := newQueryTab(w, status, tables, nodeTable, state)
	autoQueryTab := newAutoQueryTab(w, status, tables, nodeTable, state)
	archiveControlSet := newArchiveControlSet(w, status, tables, state)
	archiveControls := archiveControlSet.archiveControls
	var tabs *container.AppTabs

	openButton := compactToolbarButton(toolbarLabelOpen, theme.FolderOpenIcon(), func() {
		openFileDialog(w, status, summary, elementTable, state)
	})
	inspectArchiveButton := compactToolbarButton(toolbarLabelInspect, theme.SearchIcon(), func() {
		inspectSelectedArchiveInstance(w, status, summary, elementTable, state)
	})
	importFileButton := compactToolbarButton(toolbarLabelImport, theme.ContentAddIcon(), func() {
		importFileDialog(w, status, tables, state)
	})
	exportButton := compactToolbarButton(toolbarLabelExport, theme.DocumentSaveIcon(), func() {
		exportStudiesCSV(w, status, state)
	})
	importFolderButton := compactToolbarButton(toolbarLabelFolder, theme.FolderIcon(), func() {
		importFolderDialog(w, status, tables, state)
	})
	refreshButton := compactToolbarButton(toolbarLabelRefresh, theme.ViewRefreshIcon(), func() {
		refreshStudies(w, status, tables, state)
	})
	queryButton := compactToolbarButton(toolbarLabelQuery, theme.SearchReplaceIcon(), func() {
		if selectAppTabByText(tabs, "Query") {
			status.SetText("Showing Query workspace")
		}
	})
	sendStudyButton := compactToolbarButton(toolbarLabelSendStudy, theme.UploadIcon(), func() {
		sendSelectedStudy(w, status, state)
	})
	sendSeriesButton := compactToolbarButton(toolbarLabelSendSeries, theme.UploadIcon(), func() {
		sendSelectedSeries(w, status, state)
	})
	sendImageButton := compactToolbarButton(toolbarLabelSendImage, theme.UploadIcon(), func() {
		sendSelectedInstance(w, status, state)
	})
	retrieveSeriesButton := compactToolbarButton(toolbarLabelRetrieveSeries, theme.DownloadIcon(), func() {
		retrieveSelectedSeries(w, status, tables, state)
	})
	retrieveImageButton := compactToolbarButton(toolbarLabelRetrieveImage, theme.DownloadIcon(), func() {
		retrieveSelectedInstance(w, status, tables, state)
	})
	cancelRetrieveButton := compactToolbarButton(toolbarLabelCancel, theme.MediaStopIcon(), func() {
		cancelActiveRetrieve(status, state)
	})
	anonymizeButton := disabledToolbarButton(toolbarLabelAnonymize, theme.VisibilityOffIcon())
	metaDataButton := compactToolbarButton(toolbarLabelMetaData, theme.SearchIcon(), func() {
		if selectAppTabByText(tabs, "Inspector") {
			status.SetText("Showing metadata inspector")
		}
	})
	addNodeButton := compactToolbarButton(toolbarLabelAdd, theme.ContentAddIcon(), func() {
		showAddNodeDialog(w, status, nodeTable, state)
	})
	editNodeButton := compactToolbarButton(toolbarLabelEdit, theme.DocumentCreateIcon(), func() {
		showEditNodeDialog(w, status, nodeTable, state)
	})
	deleteStudyButton := compactToolbarButton(toolbarLabelDelete, theme.DeleteIcon(), func() {
		showDeleteStudyDialog(w, status, tables, state)
	})
	echoButton := compactToolbarButton(toolbarLabelVerify, theme.ConfirmIcon(), func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	startReceiverButton := compactToolbarButton(toolbarLabelListen, theme.DownloadIcon(), func() {
		startReceiver(w, status, state)
	})
	stopReceiverButton := compactToolbarButton(toolbarLabelStop, theme.MediaStopIcon(), func() {
		stopReceiver(w, status, tables, state)
	})
	settingsButton := compactToolbarButton(toolbarLabelSettings, theme.SettingsIcon(), func() {
		showSettingsDialog(w, status, tables, state)
	})

	title := widget.NewLabelWithStyle("go-pacs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.TextStyle.Monospace = false
	actions := groupedToolbarActions(map[string]fyne.CanvasObject{
		toolbarLabelOpen:           openButton,
		toolbarLabelInspect:        inspectArchiveButton,
		toolbarLabelImport:         importFileButton,
		toolbarLabelExport:         exportButton,
		toolbarLabelFolder:         importFolderButton,
		toolbarLabelRefresh:        refreshButton,
		toolbarLabelQuery:          queryButton,
		toolbarLabelSendStudy:      sendStudyButton,
		toolbarLabelSendSeries:     sendSeriesButton,
		toolbarLabelSendImage:      sendImageButton,
		toolbarLabelRetrieveSeries: retrieveSeriesButton,
		toolbarLabelRetrieveImage:  retrieveImageButton,
		toolbarLabelCancel:         cancelRetrieveButton,
		toolbarLabelAnonymize:      anonymizeButton,
		toolbarLabelMetaData:       metaDataButton,
		toolbarLabelAdd:            addNodeButton,
		toolbarLabelEdit:           editNodeButton,
		toolbarLabelDelete:         deleteStudyButton,
		toolbarLabelVerify:         echoButton,
		toolbarLabelListen:         startReceiverButton,
		toolbarLabelStop:           stopReceiverButton,
		toolbarLabelSettings:       settingsButton,
	})
	actionsScroll := container.NewHScroll(actions)
	actionsScroll.SetMinSize(fyne.NewSize(0, actions.MinSize().Height))
	toolbar := container.NewVBox(container.NewBorder(nil, nil, title, archiveControlSet.toolbarSearch, actionsScroll), status)

	state.archiveSeriesSummary = compactWorkbenchLabel()
	state.archiveInstancesSummary = compactWorkbenchLabel()
	seriesAndInstances := container.NewVSplit(
		labeledTableWithFooter("Series", seriesTable, state.archiveSeriesSummary),
		labeledTableWithFooter("Instances", instanceTable, state.archiveInstancesSummary),
	)
	seriesAndInstances.SetOffset(0.42)
	archiveBrowser := container.NewVSplit(
		labeledTable("Studies", studyTable),
		seriesAndInstances,
	)
	archiveBrowser.SetOffset(0.46)
	archiveTab := newArchiveWorkbench(w, status, tables, archiveControls, archiveBrowser, state)
	networkTab := newNetworkTab(w, status, nodeTable, state)
	tasksBrowser := container.NewVSplit(
		labeledTable("Tasks", taskTable),
		container.NewStack(taskDetail),
	)
	tasksBrowser.SetOffset(0.55)
	tasksTab := container.NewBorder(nil, nil, nil, nil, tasksBrowser)
	inspectorTab := container.NewBorder(
		summary.container,
		nil,
		nil,
		nil,
		container.NewStack(elementTable),
	)
	tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Archive", theme.StorageIcon(), archiveTab),
		container.NewTabItemWithIcon(networkTabTitle, theme.ComputerIcon(), networkTab),
		container.NewTabItemWithIcon("Query", theme.SearchReplaceIcon(), queryTab),
		container.NewTabItemWithIcon(autoQueryTabTitle, theme.HistoryIcon(), autoQueryTab),
		container.NewTabItemWithIcon("Tasks", theme.HistoryIcon(), tasksTab),
		container.NewTabItemWithIcon("Inspector", theme.SearchIcon(), inspectorTab),
	)
	registerNetworkDeleteShortcut(w, tabs, status, nodeTable, state)

	content := container.NewBorder(
		toolbar,
		nil,
		nil,
		nil,
		tabs,
	)
	w.SetContent(content)
	refreshStudies(w, status, tables, state)
	if state.appConfig.ReceiverAutoStart {
		startReceiver(w, status, state)
	}
	w.ShowAndRun()
}

type uiState struct {
	elements                     []dicominspect.ElementSummary
	studies                      []archive.Study
	archiveRows                  []archiveBrowserRow
	collapsedPatientGroups       map[string]bool
	collapsedArchiveStudies      map[string]bool
	archiveSeriesByStudy         map[string][]archive.Series
	series                       []archive.Series
	instances                    []archive.Instance
	nodes                        []nodes.Node
	nodeTableRows                []int
	queries                      []query.Match
	catalog                      *archive.Catalog
	nodeStore                    *nodes.Store
	autoQueryProfileStore        *autoquery.Store
	autoQueryProfiles            []autoquery.Profile
	autoQueryProfileName         string
	autoQueryProfileLocked       bool
	autoQuerySources             []autoquery.Source
	receiver                     *receive.Server
	appConfig                    appconfig.Config
	appConfigPath                string
	operations                   []ops.Summary
	operationTable               *widget.Table
	operationDetail              *widget.Entry
	operationHistoryPath         string
	archiveAlbumList             *widget.List
	selectedArchiveAlbum         archiveAlbumID
	archiveSourceList            *widget.List
	archiveSourceMoveUpButton    *widget.Button
	archiveSourceMoveDownButton  *widget.Button
	archiveActivity              *widget.Label
	archiveActivityList          *widget.List
	archiveActivityProgress      *widget.ProgressBar
	archiveCancelRetrieveButton  *widget.Button
	archiveClearActivityButton   *widget.Button
	archiveEditStudyButton       *widget.Button
	archiveSummaryTitle          *widget.Label
	archiveSummary               *widget.Label
	archiveResultSummary         *widget.Label
	archiveSeriesSummary         *widget.Label
	archiveInstancesSummary      *widget.Label
	queryDestinationLabel        *widget.Label
	queryResultSummaryLabel      *widget.Label
	querySelectedDetailsLabel    *widget.Label
	autoQueryResultSummaryLabel  *widget.Label
	autoQueryCountdownLabel      *widget.Label
	querySourceList              *widget.List
	queryMoveDestinationSelect   *widget.SelectEntry
	lastQuery                    lastQueryRequest
	autoQueryLast                lastQueryRequest
	queryMoveDestination         string
	queryAutoRetrieve            bool
	queryKeepOnTop               bool
	autoQueryAutoRetrieve        bool
	autoQueryRetrieveLevel       string
	autoQueryMaxMatches          string
	autoQueryDuplicatePolicy     string
	autoQueryRequireConfirmation bool
	autoQuerySettingsConfigured  bool
	autoQuerySearchField         string
	autoQuerySearchText          string
	autoQueryDatePreset          string
	autoQueryModalities          []string
	autoQueryRefreshMode         string
	autoQueryRefreshCancel       context.CancelFunc
	autoQueryNextRefresh         time.Time
	nodeVerifyStatuses           map[string]nodeVerifyStatus
	querySourceStatuses          map[string]querySourceStatus
	queryRetrieveRows            map[string]string
	receiverStartedAt            time.Time
	activeRetrieveCancel         context.CancelFunc
	retrieveActivityNode         string
	retrieveActivityProgress     retrieve.Progress
	activeQueryActivityLabel     string
	activeSendActivityLabel      string
	activeImportActivityLabel    string
	selectedOperationRow         int
	studyFilters                 archive.StudyFilters
	seriesFilters                archive.SeriesFilters
	selectedStudyRow             int
	selectedSeriesRow            int
	selectedInstanceRow          int
	selectedNodeRow              int
	selectedAutoQuerySourceRow   int
	selectedQueryRow             int
	selectedQueryVirtual         bool
	selectedQueryVirtualMatch    query.Match
	collapsedQueryGroups         map[string]bool
	archiveSortActive            bool
	archiveSortColumn            int
	archiveSortDescending        bool
	seriesSortActive             bool
	seriesSortColumn             int
	seriesSortDescending         bool
	instanceSortActive           bool
	instanceSortColumn           int
	instanceSortDescending       bool
	nodeSortActive               bool
	nodeSortColumn               int
	nodeSortDescending           bool
	querySortActive              bool
	querySortColumn              int
	querySortDescending          bool
}

type archiveTables struct {
	studies   *widget.Table
	series    *widget.Table
	instances *widget.Table
}

func recordOperation(state *uiState, summary ops.Summary) {
	if state == nil {
		return
	}
	state.operations = append([]ops.Summary{summary}, state.operations...)
	if len(state.operations) > maxTaskHistory {
		state.operations = state.operations[:maxTaskHistory]
	}
	if state.operationHistoryPath != "" {
		_ = ops.SaveHistory(state.operationHistoryPath, state.operations)
	}
	state.selectedOperationRow = 0
	updateTaskDetail(state)
	if state.operationTable != nil {
		state.operationTable.Refresh()
	}
	refreshArchiveChrome(state)
}

type archiveActivityRow struct {
	Text           string
	OperationIndex int
	Dismissible    bool
}

func clearRecentOperations(status *widget.Label, state *uiState) {
	if state == nil {
		return
	}
	state.operations = nil
	state.selectedOperationRow = -1
	if state.operationHistoryPath != "" {
		if err := ops.SaveHistory(state.operationHistoryPath, state.operations); err != nil {
			if status != nil {
				status.SetText("Activity history clear failed")
			}
			return
		}
	}
	updateTaskDetail(state)
	if state.operationTable != nil {
		state.operationTable.Refresh()
	}
	refreshArchiveChrome(state)
	if status != nil {
		status.SetText("Activity history cleared")
	}
}

func dismissArchiveActivityRow(status *widget.Label, state *uiState, rowIndex int) {
	if state == nil {
		return
	}
	rows := archiveActivityRows(state)
	if rowIndex < 0 || rowIndex >= len(rows) || !rows[rowIndex].Dismissible {
		if status != nil {
			status.SetText("No dismissible activity row")
		}
		return
	}
	dismissOperationAt(status, state, rows[rowIndex].OperationIndex)
}

func dismissOperationAt(status *widget.Label, state *uiState, operationIndex int) {
	if state == nil || operationIndex < 0 || operationIndex >= len(state.operations) {
		if status != nil {
			status.SetText("No dismissible activity row")
		}
		return
	}
	state.operations = append(state.operations[:operationIndex], state.operations[operationIndex+1:]...)
	switch {
	case len(state.operations) == 0:
		state.selectedOperationRow = -1
	case state.selectedOperationRow > operationIndex:
		state.selectedOperationRow--
	case state.selectedOperationRow >= len(state.operations):
		state.selectedOperationRow = len(state.operations) - 1
	}
	if state.operationHistoryPath != "" {
		if err := ops.SaveHistory(state.operationHistoryPath, state.operations); err != nil {
			if status != nil {
				status.SetText("Activity row dismiss failed")
			}
			return
		}
	}
	updateTaskDetail(state)
	if state.operationTable != nil {
		state.operationTable.Refresh()
	}
	refreshArchiveChrome(state)
	if status != nil {
		status.SetText("Activity row dismissed")
	}
}

func newTaskDetail() *widget.Entry {
	detail := widget.NewMultiLineEntry()
	detail.Wrapping = fyne.TextWrapWord
	detail.Disable()
	return detail
}

func newTaskTable(state *uiState) *widget.Table {
	headers := []string{"Kind", "Status", "Counts", "Duration", "Failures"}
	var table *widget.Table
	table = widget.NewTable(
		func() (int, int) {
			return len(state.operations) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, headers[id.Col], true, false)
				return
			}
			summary := state.operations[id.Row-1]
			selected := id.Row-1 == state.selectedOperationRow
			applyTextTableCell(cell, id.Row, taskCell(summary, id.Col), false, selected)
		},
	)
	table.SetColumnWidth(0, 120)
	table.SetColumnWidth(1, 100)
	table.SetColumnWidth(2, 420)
	table.SetColumnWidth(3, 100)
	table.SetColumnWidth(4, 500)
	applyCompactTableRows(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			return
		}
		state.selectedOperationRow = id.Row - 1
		updateTaskDetail(state)
		table.Refresh()
	}
	return table
}

func updateTaskDetail(state *uiState) {
	if state == nil || state.operationDetail == nil {
		return
	}
	if len(state.operations) == 0 {
		state.operationDetail.SetText("")
		return
	}
	if state.selectedOperationRow < 0 || state.selectedOperationRow >= len(state.operations) {
		state.selectedOperationRow = 0
	}
	state.operationDetail.SetText(taskDetailText(state.operations[state.selectedOperationRow]))
}

func taskDetailText(summary ops.Summary) string {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func taskCell(summary ops.Summary, col int) string {
	switch col {
	case 0:
		return string(summary.Kind)
	case 1:
		return string(summary.Status)
	case 2:
		return countsCell(summary.Counts)
	case 3:
		return (time.Duration(summary.DurationMS) * time.Millisecond).String()
	case 4:
		if len(summary.Failures) == 0 {
			return ""
		}
		return summary.Failures[0].Message
	default:
		return ""
	}
}

func countsCell(counts ops.Counts) string {
	var parts []string
	appendCount := func(label string, value *uint64) {
		if value != nil && *value > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", label, *value))
		}
	}
	appendCount("requested", counts.Requested)
	appendCount("matched", counts.Matched)
	appendCount("sent", counts.Sent)
	appendCount("received", counts.Received)
	appendCount("stored", counts.Stored)
	appendCount("duplicates", counts.Duplicates)
	appendCount("skipped", counts.Skipped)
	appendCount("failed", counts.Failed)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func labeledTable(title string, table *widget.Table) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	label.Wrapping = fyne.TextTruncate
	return container.NewBorder(label, nil, nil, nil, container.NewStack(table))
}

func labeledTableWithFooter(title string, table *widget.Table, footer fyne.CanvasObject) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	label.Wrapping = fyne.TextTruncate
	return container.NewBorder(label, footer, nil, nil, container.NewStack(table))
}

type summaryPanel struct {
	container *fyne.Container
	fields    map[string]*widget.Label
}

func newSummaryPanel() summaryPanel {
	fieldNames := []string{
		"File",
		"Patient",
		"Patient ID",
		"Study Date",
		"Modality",
		"Accession",
		"Study UID",
		"Series UID",
		"Series Number",
		"Series Description",
		"SOP Instance",
		"Instance Number",
		"Transfer Syntax",
	}
	fields := make(map[string]*widget.Label, len(fieldNames))
	cards := make([]fyne.CanvasObject, 0, len(fieldNames))
	for _, name := range fieldNames {
		value := widget.NewLabel("-")
		value.Wrapping = fyne.TextWrapBreak
		fields[name] = value
		cards = append(cards, widget.NewCard(name, "", value))
	}
	return summaryPanel{
		container: container.NewGridWithColumns(5, cards...),
		fields:    fields,
	}
}

func (p summaryPanel) set(summary dicominspect.Summary) {
	p.fields["File"].SetText(emptyDash(summary.FileName))
	p.fields["Patient"].SetText(emptyDash(summary.PatientName))
	p.fields["Patient ID"].SetText(emptyDash(summary.PatientID))
	p.fields["Study Date"].SetText(emptyDash(summary.StudyDate))
	p.fields["Modality"].SetText(emptyDash(summary.Modality))
	p.fields["Accession"].SetText(emptyDash(summary.AccessionNumber))
	p.fields["Study UID"].SetText(emptyDash(summary.StudyInstanceUID))
	p.fields["Series UID"].SetText(emptyDash(summary.SeriesInstanceUID))
	p.fields["Series Number"].SetText(emptyDash(summary.SeriesNumber))
	p.fields["Series Description"].SetText(emptyDash(summary.SeriesDescription))
	p.fields["SOP Instance"].SetText(emptyDash(summary.SOPInstanceUID))
	p.fields["Instance Number"].SetText(emptyDash(summary.InstanceNumber))
	p.fields["Transfer Syntax"].SetText(emptyDash(transferSyntaxLabel(summary)))
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func transferSyntaxLabel(summary dicominspect.Summary) string {
	if summary.TransferSyntax == "" {
		return summary.TransferSyntaxUID
	}
	if summary.TransferSyntaxUID == "" {
		return summary.TransferSyntax
	}
	return fmt.Sprintf("%s (%s)", summary.TransferSyntax, summary.TransferSyntaxUID)
}

func openFileDialog(w fyne.Window, status *widget.Label, summary summaryPanel, table *widget.Table, state *uiState) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()

		status.SetText("Inspecting " + reader.URI().Name())
		result, err := dicominspect.InspectReader(reader.URI().Name(), reader, dicominspect.DefaultOptions())
		if err != nil {
			status.SetText("Inspection failed")
			dialog.ShowError(err, w)
			return
		}
		state.elements = result.Elements
		summary.set(result)
		table.Refresh()
		status.SetText(fmt.Sprintf("%d elements loaded", result.ElementCount))
	}, w)
	picker.Show()
}

func inspectSelectedArchiveInstance(w fyne.Window, status *widget.Label, summary summaryPanel, table *widget.Table, state *uiState) {
	instance, ok := selectedInstance(state)
	if !ok {
		status.SetText("Select an image to inspect")
		return
	}
	if strings.TrimSpace(instance.StoredPath) == "" {
		status.SetText("Selected image has no stored path")
		return
	}
	label := instance.SOPInstanceUID
	if strings.TrimSpace(label) == "" || label == "(missing)" {
		label = filepath.Base(instance.StoredPath)
	}
	status.SetText("Inspecting archived image " + label)
	go func(path string) {
		result, err := dicominspect.InspectFile(path, dicominspect.DefaultOptions())
		fyne.Do(func() {
			if err != nil {
				status.SetText("Inspection failed")
				dialog.ShowError(err, w)
				return
			}
			state.elements = result.Elements
			summary.set(result)
			table.Refresh()
			status.SetText(fmt.Sprintf("%d elements loaded", result.ElementCount))
		})
	}(instance.StoredPath)
}

func newArchiveWorkbench(w fyne.Window, status *widget.Label, tables archiveTables, archiveControls fyne.CanvasObject, archiveBrowser fyne.CanvasObject, state *uiState) fyne.CanvasObject {
	sidebar := newArchiveSidebar(w, status, tables, state)
	summary := newArchiveSummaryPane(w, status, tables, state)
	state.archiveResultSummary = compactWorkbenchLabel()
	center := container.NewBorder(archiveControls, state.archiveResultSummary, nil, nil, archiveBrowser)
	centerAndSummary := container.NewHSplit(center, summary)
	centerAndSummary.SetOffset(0.80)
	workbench := container.NewHSplit(sidebar, centerAndSummary)
	workbench.SetOffset(0.17)
	refreshArchiveChrome(state)
	return workbench
}

func newArchiveSidebar(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	if state.selectedArchiveAlbum == "" {
		state.selectedArchiveAlbum = archiveAlbumDatabase
	}
	state.archiveAlbumList = newArchiveAlbumList(w, status, tables, state)
	state.archiveSourceList = newArchiveSourceList(state)
	state.archiveSourceMoveUpButton, state.archiveSourceMoveDownButton = newArchiveSourcePriorityButtons(w, status, state)
	state.archiveActivity = compactWorkbenchLabel()
	state.archiveActivity.Hide()
	state.archiveActivityList = newArchiveActivityList(status, state)
	state.archiveActivityProgress = widget.NewProgressBar()
	state.archiveActivityProgress.Hide()
	state.archiveCancelRetrieveButton = widget.NewButtonWithIcon("Cancel Retrieve", theme.MediaStopIcon(), func() {
		cancelActiveRetrieve(status, state)
	})
	state.archiveCancelRetrieveButton.Hide()
	state.archiveClearActivityButton = newActivityDismissButton(func() {
		clearRecentOperations(status, state)
	})
	state.archiveClearActivityButton.Hide()
	content := container.NewVBox(
		workbenchSectionTitle("Albums"),
		state.archiveAlbumList,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, workbenchSectionTitle("Sources"), container.NewHBox(state.archiveSourceMoveUpButton, state.archiveSourceMoveDownButton)),
		state.archiveSourceList,
		widget.NewSeparator(),
		workbenchSectionTitle("Activity"),
		state.archiveActivityList,
		state.archiveActivityProgress,
		state.archiveCancelRetrieveButton,
		state.archiveClearActivityButton,
	)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(220, 0))
	return scroll
}

func newArchiveSourcePriorityButtons(w fyne.Window, status *widget.Label, state *uiState) (*widget.Button, *widget.Button) {
	moveSource := func(delta int) {
		changed, err := moveQuerySource(state, delta)
		if err != nil {
			if status != nil {
				status.SetText("Source order update failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if changed {
			refreshArchiveChrome(state)
			refreshQueryDestination(state)
			refreshQueryResultSummary(state)
			refreshQuerySourceList(state)
			if status != nil {
				status.SetText("Updated source priority")
			}
		}
	}
	moveUpButton := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		moveSource(-1)
	})
	moveDownButton := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
		moveSource(1)
	})
	moveUpButton.Importance = widget.LowImportance
	moveDownButton.Importance = widget.LowImportance
	return moveUpButton, moveDownButton
}

type archiveSourceListItem struct {
	*fyne.Container
	selectionIcon *widget.Icon
	sourceIcon    *widget.Icon
	label         *widget.Label
}

func newArchiveSourceListItem() *archiveSourceListItem {
	selectionIcon := widget.NewIcon(theme.NavigateNextIcon())
	selectionIcon.Hide()
	sourceIcon := widget.NewIcon(theme.StorageIcon())
	label := compactWorkbenchLabel()
	row := container.NewBorder(nil, nil, container.NewHBox(selectionIcon, sourceIcon), nil, label)
	return &archiveSourceListItem{
		Container:     row,
		selectionIcon: selectionIcon,
		sourceIcon:    sourceIcon,
		label:         label,
	}
}

func newArchiveSourceList(state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(archiveSourceRows(state))
		},
		func() fyne.CanvasObject {
			return newArchiveSourceListItem()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			item := obj.(*archiveSourceListItem)
			rows := archiveSourceRows(state)
			if int(id) < 0 || int(id) >= len(rows) {
				item.label.SetText("")
				item.selectionIcon.Hide()
				return
			}
			row := rows[id]
			item.label.SetText(row.Text)
			item.sourceIcon.SetResource(row.Icon)
			if row.Selected {
				item.selectionIcon.SetResource(theme.NavigateNextIcon())
				item.selectionIcon.Show()
			} else {
				item.selectionIcon.Hide()
			}
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		rows := archiveSourceRows(state)
		if id < 0 || id >= len(rows) {
			return
		}
		row := rows[id]
		if row.NodeIndex < 0 {
			list.Unselect(id)
			return
		}
		state.selectedNodeRow = row.NodeIndex
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
	}
	return list
}

func newArchiveAlbumList(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(archiveAlbumRows(state.studies, time.Now(), state.selectedArchiveAlbum))
		},
		func() fyne.CanvasObject {
			return compactWorkbenchLabel()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			rows := archiveAlbumRows(state.studies, time.Now(), state.selectedArchiveAlbum)
			label := obj.(*widget.Label)
			if id < 0 || id >= len(rows) {
				label.SetText("")
				return
			}
			label.SetText(rows[id].Text)
		},
	)
	list.HideSeparators = true
	list.OnSelected = func(id widget.ListItemID) {
		rows := archiveAlbumRows(state.studies, time.Now(), state.selectedArchiveAlbum)
		if id < 0 || id >= len(rows) {
			return
		}
		row := rows[id]
		if !row.Filterable {
			if status != nil {
				status.SetText(fmt.Sprintf("%s album is not available yet", row.Label))
			}
			list.Unselect(id)
			return
		}
		filters, ok := archiveFiltersWithAlbum(state.studyFilters, row.ID, time.Now())
		if !ok {
			list.Unselect(id)
			return
		}
		state.selectedArchiveAlbum = row.ID
		state.studyFilters = filters
		state.seriesFilters = archive.SeriesFilters{}
		refreshStudies(w, status, tables, state)
		list.Refresh()
	}
	return list
}

func newArchiveActivityList(status *widget.Label, state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(archiveActivityRows(state))
		},
		func() fyne.CanvasObject {
			label := compactWorkbenchLabel()
			dismiss := newActivityDismissButton(nil)
			return container.NewHBox(label, dismiss)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			rows := archiveActivityRows(state)
			box := obj.(*fyne.Container)
			label := box.Objects[0].(*widget.Label)
			dismiss := box.Objects[1].(*widget.Button)
			if id < 0 || id >= len(rows) {
				label.SetText("")
				dismiss.Hide()
				return
			}
			row := rows[id]
			label.SetText(row.Text)
			if row.Dismissible {
				rowIndex := id
				dismiss.OnTapped = func() {
					dismissArchiveActivityRow(status, state, rowIndex)
				}
				dismiss.Show()
			} else {
				dismiss.OnTapped = nil
				dismiss.Hide()
			}
		},
	)
	list.HideSeparators = true
	return list
}

func newActivityDismissButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", theme.ContentClearIcon(), tapped)
	button.Importance = widget.LowImportance
	return button
}

func newArchiveSummaryPane(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	state.archiveSummaryTitle = workbenchSectionTitle("Selected Study")
	state.archiveSummary = compactWorkbenchLabel()
	state.archiveSummary.Wrapping = fyne.TextWrapWord
	state.archiveEditStudyButton = widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		showStudyMetadataDialog(w, status, tables, state)
	})
	state.archiveEditStudyButton.Importance = widget.LowImportance
	state.archiveEditStudyButton.Disable()
	header := container.NewBorder(nil, nil, state.archiveSummaryTitle, state.archiveEditStudyButton)
	scroll := container.NewVScroll(state.archiveSummary)
	scroll.SetMinSize(fyne.NewSize(260, 0))
	return container.NewBorder(header, nil, nil, nil, scroll)
}

func compactWorkbenchLabel() *widget.Label {
	label := widget.NewLabel("")
	label.Wrapping = fyne.TextTruncate
	label.TextStyle.Monospace = true
	return label
}

func workbenchSectionTitle(title string) *widget.Label {
	return widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

func workbenchCenteredTitle(title string) *widget.Label {
	return widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
}

func refreshArchiveChrome(state *uiState) {
	if state == nil {
		return
	}
	if state.archiveAlbumList != nil {
		state.archiveAlbumList.Refresh()
	}
	if state.archiveSourceList != nil {
		state.archiveSourceList.Refresh()
	}
	if state.archiveActivity != nil {
		state.archiveActivity.SetText(strings.Join(archiveActivityLines(state), "\n"))
	}
	if state.archiveActivityList != nil {
		state.archiveActivityList.Refresh()
	}
	if state.archiveActivityProgress != nil {
		if state.activeRetrieveCancel == nil {
			state.archiveActivityProgress.SetValue(0)
			state.archiveActivityProgress.Hide()
		} else {
			state.archiveActivityProgress.SetValue(retrieveProgressFraction(state.retrieveActivityProgress))
			state.archiveActivityProgress.Show()
		}
	}
	if state.archiveCancelRetrieveButton != nil {
		if state.activeRetrieveCancel == nil {
			state.archiveCancelRetrieveButton.Hide()
		} else {
			state.archiveCancelRetrieveButton.Show()
		}
	}
	if state.archiveClearActivityButton != nil {
		if len(state.operations) == 0 {
			state.archiveClearActivityButton.Hide()
		} else {
			state.archiveClearActivityButton.Show()
		}
	}
	if state.archiveEditStudyButton != nil {
		if _, ok := selectedStudy(state); ok && state.catalog != nil {
			state.archiveEditStudyButton.Enable()
		} else {
			state.archiveEditStudyButton.Disable()
		}
	}
	if state.archiveSummary != nil {
		state.archiveSummary.SetText(archiveSummaryText(state))
	}
	if state.archiveSummaryTitle != nil {
		state.archiveSummaryTitle.SetText(archiveSummaryTitleText(state))
	}
	if state.archiveResultSummary != nil {
		state.archiveResultSummary.SetText(archiveResultSummaryText(state))
	}
	if state.archiveSeriesSummary != nil {
		state.archiveSeriesSummary.SetText(archiveSeriesSummaryText(state))
	}
	if state.archiveInstancesSummary != nil {
		state.archiveInstancesSummary.SetText(archiveInstancesSummaryText(state))
	}
}

func archiveSummaryTitleText(state *uiState) string {
	study, ok := selectedStudy(state)
	if !ok {
		return "Selected Study"
	}
	if patientName := strings.TrimSpace(study.PatientName); patientName != "" {
		return patientName
	}
	if patientID := strings.TrimSpace(study.PatientID); patientID != "" {
		return patientID
	}
	return "Selected Study"
}

func archiveResultSummaryText(state *uiState) string {
	if state == nil || len(state.studies) == 0 {
		return "0 patients, 0 studies, 0 series, 0 images"
	}
	patientKeys := map[string]bool{}
	seriesCount := 0
	imageCount := 0
	for _, study := range state.studies {
		patientKeys[archivePatientKey(study)] = true
		seriesCount += study.SeriesCount
		imageCount += study.InstanceCount
	}
	summary := fmt.Sprintf(
		"%d patients, %d studies, %d series, %d images",
		len(patientKeys),
		len(state.studies),
		seriesCount,
		imageCount,
	)
	if label := activeArchiveAlbumSummaryLabel(state.selectedArchiveAlbum); label != "" {
		summary += " - Album: " + label
	}
	if label := activeArchiveSourceSummaryLabel(state); label != "" {
		summary += " - Source: " + label
	}
	return summary
}

func archiveSeriesSummaryText(state *uiState) string {
	if state == nil || len(state.series) == 0 {
		return "0 series, 0 images"
	}
	imageCount := 0
	for _, series := range state.series {
		imageCount += series.InstanceCount
	}
	return fmt.Sprintf("%d series, %d images", len(state.series), imageCount)
}

func archiveInstancesSummaryText(state *uiState) string {
	if state == nil {
		return "0 images"
	}
	return fmt.Sprintf("%d images", len(state.instances))
}

func archiveAlbumLines(studies []archive.Study, now time.Time) []string {
	return []string{
		railCountLine("Database", len(studies)),
		railCountLine("Cases with comments", 0),
		railCountLine("Interesting Cases", 0),
		railCountLine("Just Acquired (last hour)", countStudiesImportedSince(studies, now.Add(-time.Hour))),
		railCountLine("Just Opened", 0),
		railCountLine("Today", countStudiesImportedToday(studies, now, "")),
		railCountLine("Today CT", countStudiesImportedToday(studies, now, "CT")),
	}
}

func activeArchiveAlbumSummaryLabel(id archiveAlbumID) string {
	switch id {
	case "", archiveAlbumDatabase:
		return ""
	default:
		return archiveAlbumLabel(id)
	}
}

func archiveAlbumLabel(id archiveAlbumID) string {
	switch id {
	case archiveAlbumDatabase:
		return "Database"
	case archiveAlbumComments:
		return "Cases with comments"
	case archiveAlbumInteresting:
		return "Interesting Cases"
	case archiveAlbumLastHour:
		return "Just Acquired (last hour)"
	case archiveAlbumOpened:
		return "Just Opened"
	case archiveAlbumToday:
		return "Today"
	case archiveAlbumTodayCT:
		return "Today CT"
	default:
		return ""
	}
}

func activeArchiveSourceSummaryLabel(state *uiState) string {
	if state == nil || state.selectedNodeRow < 0 || state.selectedNodeRow >= len(state.nodes) {
		return ""
	}
	return archiveNodeSourceLabel(state.nodes[state.selectedNodeRow])
}

func archiveSourceLines(state *uiState) []string {
	rows := archiveSourceRows(state)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		prefix := "  "
		if row.Selected {
			prefix = "▶ "
		}
		lines = append(lines, prefix+row.LegacyText)
	}
	return lines
}

func archiveNodeSourceLabel(node nodes.Node) string {
	return fmt.Sprintf("%s %s:%d", node.Name, node.Host, node.Port)
}

type archiveSourceRow struct {
	Text       string
	LegacyText string
	Icon       fyne.Resource
	Selected   bool
	NodeIndex  int
}

func archiveSourceRows(state *uiState) []archiveSourceRow {
	rows := []archiveSourceRow{{
		Text:       "Documents DB",
		LegacyText: "▣ Documents DB",
		Icon:       theme.StorageIcon(),
		NodeIndex:  -1,
	}}
	if state == nil {
		return rows
	}
	if state.receiver != nil {
		snapshot := state.receiver.Snapshot()
		rows = append(rows, archiveSourceRow{
			Text:       fmt.Sprintf("Receiver %s %s", snapshot.AETitle, snapshot.Address),
			LegacyText: fmt.Sprintf("● Receiver %s %s", snapshot.AETitle, snapshot.Address),
			Icon:       theme.ComputerIcon(),
			NodeIndex:  -1,
		})
	} else {
		rows = append(rows, archiveSourceRow{
			Text:       fmt.Sprintf("Receiver %s stopped", localAETitle(state)),
			LegacyText: fmt.Sprintf("● Receiver %s stopped", localAETitle(state)),
			Icon:       theme.ComputerIcon(),
			NodeIndex:  -1,
		})
	}
	for index, node := range state.nodes {
		text := archiveNodeSourceLabel(node)
		rows = append(rows, archiveSourceRow{
			Text:       text,
			LegacyText: "◉ " + text,
			Icon:       theme.DesktopIcon(),
			Selected:   index == state.selectedNodeRow,
			NodeIndex:  index,
		})
	}
	return rows
}

func archiveActivityLines(state *uiState) []string {
	rows := archiveActivityRows(state)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, row.Text)
	}
	return lines
}

func archiveActivityRows(state *uiState) []archiveActivityRow {
	if state == nil {
		return []archiveActivityRow{{Text: "No recent activity", OperationIndex: -1}}
	}
	var rows []archiveActivityRow
	if state.activeRetrieveCancel != nil {
		rows = append(rows, archiveActivityRow{
			Text:           retrieveProgressText(state.retrieveActivityNode, state.retrieveActivityProgress),
			OperationIndex: -1,
		})
	}
	if strings.TrimSpace(state.activeQueryActivityLabel) != "" {
		rows = append(rows, archiveActivityRow{
			Text:           "Query " + state.activeQueryActivityLabel,
			OperationIndex: -1,
		})
	}
	if strings.TrimSpace(state.activeSendActivityLabel) != "" {
		rows = append(rows, archiveActivityRow{
			Text:           "Send " + state.activeSendActivityLabel,
			OperationIndex: -1,
		})
	}
	if strings.TrimSpace(state.activeImportActivityLabel) != "" {
		rows = append(rows, archiveActivityRow{
			Text:           "Import " + state.activeImportActivityLabel,
			OperationIndex: -1,
		})
	}
	for i, summary := range state.operations {
		if i >= 4 {
			break
		}
		counts := shortTaskCounts(summary.Counts)
		if counts != "" {
			rows = append(rows, archiveActivityRow{
				Text:           fmt.Sprintf("%s %s %s", summary.Kind, summary.Status, counts),
				OperationIndex: i,
				Dismissible:    true,
			})
			continue
		}
		rows = append(rows, archiveActivityRow{
			Text:           fmt.Sprintf("%s %s", summary.Kind, summary.Status),
			OperationIndex: i,
			Dismissible:    true,
		})
	}
	if len(rows) == 0 {
		rows = append(rows, archiveActivityRow{Text: "No recent activity", OperationIndex: -1})
	}
	return rows
}

func archiveSummaryText(state *uiState) string {
	if state == nil {
		return "No study selected"
	}
	study, ok := selectedStudy(state)
	if !ok {
		return fmt.Sprintf("No study selected\n\n%d studies in archive", len(state.studies))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", emptyDash(study.PatientName))
	fmt.Fprintf(&b, "Patient ID: %s\n", emptyDash(study.PatientID))
	fmt.Fprintf(&b, "DOB: %s\n", emptyDash(study.PatientBirthDate))
	fmt.Fprintf(&b, "Study: %s\n", archiveSummaryStudyLine(study))
	if strings.TrimSpace(study.StudyDescription) != "" {
		fmt.Fprintf(&b, "%s\n", study.StudyDescription)
	}
	fmt.Fprintf(&b, "Institution: %s\n", emptyDash(study.InstitutionName))
	fmt.Fprintf(&b, "Accession: %s\n", emptyDash(study.AccessionNumber))
	fmt.Fprintf(&b, "Source: %s\n", emptyDash(archiveSummarySource(state)))
	fmt.Fprintf(&b, "Status: %s\n", emptyDash(studyCell(study, archiveStudyTableColumnStatus)))
	fmt.Fprintf(&b, "Comments: %s\n", emptyDash(studyCell(study, archiveStudyTableColumnComments)))
	fmt.Fprintf(&b, "Series: %d\n", study.SeriesCount)
	fmt.Fprintf(&b, "Images: %d\n", study.InstanceCount)
	if !study.ImportedAt.IsZero() {
		fmt.Fprintf(&b, "Added: %s\n", study.ImportedAt.Format("2006-01-02 15:04"))
	}
	if lines := patientStudySummaryLines(state, state.selectedStudyRow); len(lines) > 0 {
		fmt.Fprintf(&b, "\nPatient studies\n")
		for _, line := range lines {
			fmt.Fprintf(&b, "%s\n", line)
		}
	}
	if series, ok := selectedSeries(state); ok {
		fmt.Fprintf(&b, "\nSelected series\n")
		fmt.Fprintf(&b, "%s %s %s\n", emptyDash(series.SeriesNumber), emptyDash(series.Modality), emptyDash(series.SeriesDescription))
		fmt.Fprintf(&b, "Series images: %d\n", series.InstanceCount)
	}
	fmt.Fprintf(&b, "\nLoaded images: %d", len(state.instances))
	return b.String()
}

func archiveSummaryStudyLine(study archive.Study) string {
	parts := []string{
		strings.TrimSpace(study.StudyDate),
		strings.TrimSpace(dicomTimeCell(study.StudyTime)),
		strings.TrimSpace(study.Modalities),
	}
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return "-"
	}
	return strings.Join(nonEmpty, " ")
}

func archiveSummarySource(state *uiState) string {
	if state == nil {
		return ""
	}
	for _, instance := range state.instances {
		if source := strings.TrimSpace(instance.SourcePath); source != "" {
			return source
		}
	}
	return ""
}

func patientStudySummaryLines(state *uiState, selectedStudyIndex int) []string {
	if state == nil || selectedStudyIndex < 0 || selectedStudyIndex >= len(state.studies) {
		return nil
	}
	selected := state.studies[selectedStudyIndex]
	selectedKey := archivePatientKey(selected)
	var lines []string
	appendStudy := func(study archive.Study, selected bool) {
		description := strings.TrimSpace(study.StudyDescription)
		if description == "" {
			description = strings.TrimSpace(study.StudyInstanceUID)
		}
		prefix := "  "
		if selected {
			prefix = "▶ "
		}
		lines = append(lines,
			prefix+strings.TrimSpace(fmt.Sprintf("%s %s", emptyDash(description), emptyDash(study.Modalities))),
			fmt.Sprintf("%s %d images", emptyDash(study.StudyDate), study.InstanceCount),
		)
	}
	appendStudy(selected, true)
	for index, study := range state.studies {
		if index == selectedStudyIndex {
			continue
		}
		if archivePatientKey(study) == selectedKey {
			appendStudy(study, false)
		}
	}
	return lines
}

type archiveAlbumID string

const (
	archiveAlbumDatabase    archiveAlbumID = "database"
	archiveAlbumComments    archiveAlbumID = "comments"
	archiveAlbumInteresting archiveAlbumID = "interesting"
	archiveAlbumLastHour    archiveAlbumID = "last-hour"
	archiveAlbumOpened      archiveAlbumID = "opened"
	archiveAlbumToday       archiveAlbumID = "today"
	archiveAlbumTodayCT     archiveAlbumID = "today-ct"
)

type archiveAlbumRow struct {
	ID         archiveAlbumID
	Label      string
	Count      int
	Text       string
	Filterable bool
}

func railCountLine(label string, count int) string {
	return fmt.Sprintf("%-33s%d", label, count)
}

func archiveAlbumRows(studies []archive.Study, now time.Time, selected archiveAlbumID) []archiveAlbumRow {
	rows := []archiveAlbumRow{
		{ID: archiveAlbumDatabase, Label: archiveAlbumLabel(archiveAlbumDatabase), Count: len(studies), Filterable: true},
		{ID: archiveAlbumComments, Label: archiveAlbumLabel(archiveAlbumComments), Count: countStudiesWithComments(studies), Filterable: true},
		{ID: archiveAlbumInteresting, Label: archiveAlbumLabel(archiveAlbumInteresting), Count: countStudiesWithStatus(studies, studyStatusPresetInterestingLabel), Filterable: true},
		{ID: archiveAlbumLastHour, Label: archiveAlbumLabel(archiveAlbumLastHour), Count: countStudiesImportedSince(studies, now.Add(-time.Hour)), Filterable: true},
		{ID: archiveAlbumOpened, Label: archiveAlbumLabel(archiveAlbumOpened), Count: 0},
		{ID: archiveAlbumToday, Label: archiveAlbumLabel(archiveAlbumToday), Count: countStudiesImportedToday(studies, now, ""), Filterable: true},
		{ID: archiveAlbumTodayCT, Label: archiveAlbumLabel(archiveAlbumTodayCT), Count: countStudiesImportedToday(studies, now, "CT"), Filterable: true},
	}
	for i := range rows {
		prefix := "  "
		if selected != "" && rows[i].ID == selected {
			prefix = "▶ "
		}
		rows[i].Text = prefix + railCountLine(rows[i].Label, rows[i].Count)
	}
	return rows
}

func archiveAlbumFilters(id archiveAlbumID, now time.Time) (archive.StudyFilters, bool) {
	switch id {
	case archiveAlbumDatabase:
		return archive.StudyFilters{}, true
	case archiveAlbumComments:
		return archive.StudyFilters{HasComments: true}, true
	case archiveAlbumInteresting:
		return archive.StudyFilters{Status: studyStatusPresetInterestingLabel}, true
	case archiveAlbumLastHour:
		return archive.StudyFilters{
			ImportedAtFrom: now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		}, true
	case archiveAlbumToday, archiveAlbumTodayCT:
		start, end := localDayBounds(now)
		filters := archive.StudyFilters{
			ImportedAtFrom: start.UTC().Format(time.RFC3339Nano),
			ImportedAtTo:   end.UTC().Format(time.RFC3339Nano),
		}
		if id == archiveAlbumTodayCT {
			filters.Modalities = []string{"CT"}
		}
		return filters, true
	default:
		return archive.StudyFilters{}, false
	}
}

func archiveFiltersWithAlbum(base archive.StudyFilters, id archiveAlbumID, now time.Time) (archive.StudyFilters, bool) {
	albumFilters, ok := archiveAlbumFilters(id, now)
	if !ok {
		return archive.StudyFilters{}, false
	}
	base.ImportedAtFrom = albumFilters.ImportedAtFrom
	base.ImportedAtTo = albumFilters.ImportedAtTo
	base.Modalities = albumFilters.Modalities
	base.Status = albumFilters.Status
	base.HasComments = albumFilters.HasComments
	return base, true
}

func localDayBounds(now time.Time) (time.Time, time.Time) {
	year, month, day := now.Local().Date()
	location := now.Local().Location()
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	return start, start.AddDate(0, 0, 1).Add(-time.Nanosecond)
}

func countStudiesImportedSince(studies []archive.Study, since time.Time) int {
	count := 0
	for _, study := range studies {
		if !study.ImportedAt.IsZero() && !study.ImportedAt.Before(since) {
			count++
		}
	}
	return count
}

func countStudiesWithComments(studies []archive.Study) int {
	count := 0
	for _, study := range studies {
		if strings.TrimSpace(study.Comments) != "" {
			count++
		}
	}
	return count
}

func countStudiesWithStatus(studies []archive.Study, status string) int {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return 0
	}
	count := 0
	for _, study := range studies {
		if strings.ToLower(strings.TrimSpace(study.Status)) == status {
			count++
		}
	}
	return count
}

func countStudiesImportedToday(studies []archive.Study, now time.Time, modality string) int {
	count := 0
	for _, study := range studies {
		if !sameLocalDate(study.ImportedAt, now) {
			continue
		}
		if modality != "" && !hasModality(study.Modalities, modality) {
			continue
		}
		count++
	}
	return count
}

func sameLocalDate(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func hasModality(modalities string, modality string) bool {
	modality = strings.ToUpper(strings.TrimSpace(modality))
	for _, part := range strings.FieldsFunc(modalities, func(r rune) bool {
		return r == ',' || r == '\\' || r == '/' || r == ';' || r == ' '
	}) {
		if strings.ToUpper(strings.TrimSpace(part)) == modality {
			return true
		}
	}
	return false
}

func shortTaskCounts(counts ops.Counts) string {
	var parts []string
	appendCount := func(label string, value *uint64) {
		if value != nil {
			parts = append(parts, fmt.Sprintf("%s %d", label, *value))
		}
	}
	appendCount("matched", counts.Matched)
	appendCount("stored", counts.Stored)
	appendCount("received", counts.Received)
	appendCount("sent", counts.Sent)
	appendCount("failed", counts.Failed)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func importFileDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		path := reader.URI().Path()
		_ = reader.Close()
		importPathAsync(w, status, tables, state, path)
	}, w)
	picker.Show()
}

func importFolderDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	picker := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if uri == nil {
			return
		}
		importPathAsync(w, status, tables, state, uri.Path())
	}, w)
	picker.Show()
}

func importPathAsync(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, path string) {
	if path == "" {
		return
	}
	pathLabel := filepath.Base(path)
	status.SetText("Importing " + pathLabel)
	beginImportActivity(state, pathLabel)
	started := time.Now()
	go func() {
		report, err := state.catalog.ImportPathWithOptions(context.Background(), path, importOptionsFromConfig(state.appConfig))
		summary := ops.ImportSummary(report, time.Since(started))
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveImportActivity(state)
			if err != nil {
				status.SetText("Import failed")
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, summary)
			if studyErr != nil {
				status.SetText("Import completed, refresh failed")
				dialog.ShowError(studyErr, w)
				return
			}
			setStudies(state, tables, studies)
			status.SetText(fmt.Sprintf(
				"Scanned %d, stored %d, duplicates %d, invalid %d",
				report.ScannedFiles,
				report.StoredFiles,
				report.Duplicates,
				report.InvalidFiles,
			))
		})
	}()
}

type archiveControlSet struct {
	toolbarSearch   fyne.CanvasObject
	archiveControls fyne.CanvasObject
}

func newArchiveControls(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	return newArchiveControlSet(w, status, tables, state).archiveControls
}

func newArchiveControlSet(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) archiveControlSet {
	quickSearchField := widget.NewSelect(archiveQuickSearchOptions, nil)
	quickSearchField.SetSelected(archiveQuickSearchPatientName)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder(archiveQuickSearchPlaceholder(quickSearchField.Selected))
	patientName := widget.NewEntry()
	patientName.SetPlaceHolder("Patient")
	patientID := widget.NewEntry()
	patientID.SetPlaceHolder("Patient ID")
	accession := widget.NewEntry()
	accession.SetPlaceHolder("Accession")
	studyDescription := widget.NewEntry()
	studyDescription.SetPlaceHolder("Description")
	modality := widget.NewEntry()
	modality.SetPlaceHolder("CT,MR")
	studyDateFrom := widget.NewEntry()
	studyDateFrom.SetPlaceHolder("20260101")
	studyDateTo := widget.NewEntry()
	studyDateTo.SetPlaceHolder("20261231")
	importedAtFrom := widget.NewEntry()
	importedAtFrom.SetPlaceHolder("2026-06-04T00:00:00Z")
	importedAtTo := widget.NewEntry()
	importedAtTo.SetPlaceHolder("2026-06-04T23:59:59Z")
	sourcePath := widget.NewEntry()
	sourcePath.SetPlaceHolder("source path")
	seriesModality := widget.NewEntry()
	seriesModality.SetPlaceHolder("CT")
	seriesNumber := widget.NewEntry()
	seriesNumber.SetPlaceHolder("1")
	seriesDescription := widget.NewEntry()
	seriesDescription.SetPlaceHolder("Axial")

	applyingProgrammaticSearchText := false
	applyAdvancedFilters := func() {
		state.studyFilters = archive.StudyFilters{
			PatientName:      patientName.Text,
			PatientID:        patientID.Text,
			AccessionNumber:  accession.Text,
			StudyDescription: studyDescription.Text,
			StudyDateFrom:    studyDateFrom.Text,
			StudyDateTo:      studyDateTo.Text,
			ImportedAtFrom:   importedAtFrom.Text,
			ImportedAtTo:     importedAtTo.Text,
			Modalities:       splitModalities(modality.Text),
			SourcePath:       sourcePath.Text,
		}
		state.seriesFilters = archive.SeriesFilters{
			Modality:          seriesModality.Text,
			SeriesNumber:      seriesNumber.Text,
			SeriesDescription: seriesDescription.Text,
		}
		applyingProgrammaticSearchText = true
		switch quickSearchField.Selected {
		case archiveQuickSearchPatientID:
			quickSearch.SetText(strings.TrimSpace(patientID.Text))
		case archiveQuickSearchAccession:
			quickSearch.SetText(strings.TrimSpace(accession.Text))
		default:
			quickSearch.SetText(strings.TrimSpace(patientName.Text))
		}
		applyingProgrammaticSearchText = false
		refreshStudies(w, status, tables, state)
	}
	applyButton := widget.NewButtonWithIcon("Apply Filters", theme.SearchIcon(), applyAdvancedFilters)
	soundexCheck := widget.NewCheck("Soundex", nil)
	searchModeLabel := compactWorkbenchLabel()
	searchModeLabel.SetText(archiveQuickSearchModeText(quickSearchField.Selected))
	applyQuickSearch := func() {
		filters, ok := archiveFiltersWithQuickSearchFieldAndSoundex(state.studyFilters, quickSearchField.Selected, quickSearch.Text, soundexCheck.Checked)
		if !ok {
			status.SetText("Search failed")
			if w != nil {
				dialog.ShowError(fmt.Errorf("unsupported archive search field %q", quickSearchField.Selected), w)
			}
			return
		}
		state.studyFilters = filters
		patientName.SetText(state.studyFilters.PatientName)
		patientID.SetText(state.studyFilters.PatientID)
		accession.SetText(state.studyFilters.AccessionNumber)
		refreshStudies(w, status, tables, state)
	}
	quickSearchField.OnChanged = func(field string) {
		searchModeLabel.SetText(archiveQuickSearchModeText(field))
		quickSearch.SetPlaceHolder(archiveQuickSearchPlaceholder(field))
		if strings.TrimSpace(quickSearch.Text) != "" {
			applyQuickSearch()
		}
	}
	quickSearch.OnChanged = func(_ string) {
		if applyingProgrammaticSearchText {
			return
		}
		applyQuickSearch()
	}
	quickSearch.OnSubmitted = func(_ string) {
		applyQuickSearch()
	}
	soundexCheck.OnChanged = func(_ bool) {
		if quickSearchField.Selected == archiveQuickSearchPatientName && strings.TrimSpace(quickSearch.Text) != "" {
			applyQuickSearch()
		}
	}
	searchButton := widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		applyQuickSearch()
	})
	searchButton.Importance = widget.LowImportance
	clearButton := widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		applyingProgrammaticSearchText = true
		quickSearch.SetText("")
		quickSearchField.SetSelected(archiveQuickSearchPatientName)
		applyingProgrammaticSearchText = false
		patientName.SetText("")
		patientID.SetText("")
		accession.SetText("")
		studyDescription.SetText("")
		modality.SetText("")
		studyDateFrom.SetText("")
		studyDateTo.SetText("")
		importedAtFrom.SetText("")
		importedAtTo.SetText("")
		sourcePath.SetText("")
		seriesModality.SetText("")
		seriesNumber.SetText("")
		seriesDescription.SetText("")
		state.studyFilters = archive.StudyFilters{}
		state.seriesFilters = archive.SeriesFilters{}
		refreshStudies(w, status, tables, state)
	})
	exportButton := widget.NewButtonWithIcon("Export CSV", theme.DocumentSaveIcon(), func() {
		exportStudiesCSV(w, status, state)
	})
	exportJSONButton := widget.NewButtonWithIcon("Export JSON", theme.DocumentSaveIcon(), func() {
		exportStudiesJSON(w, status, state)
	})
	exportSeriesCSVButton := widget.NewButtonWithIcon("Export Series CSV", theme.DocumentSaveIcon(), func() {
		exportSeriesCSV(w, status, state)
	})
	exportSeriesJSONButton := widget.NewButtonWithIcon("Export Series JSON", theme.DocumentSaveIcon(), func() {
		exportSeriesJSON(w, status, state)
	})
	exportImagesCSVButton := widget.NewButtonWithIcon("Export Images CSV", theme.DocumentSaveIcon(), func() {
		exportImagesCSV(w, status, state)
	})
	exportImagesJSONButton := widget.NewButtonWithIcon("Export Images JSON", theme.DocumentSaveIcon(), func() {
		exportImagesJSON(w, status, state)
	})

	quickSearchBox := container.NewVBox(quickSearch, soundexCheck, searchModeLabel)
	quickSearchCluster := container.NewBorder(
		nil,
		nil,
		labeledControl("Search", quickSearchField),
		container.NewHBox(searchButton),
		quickSearchBox,
	)
	quickRow := container.NewBorder(nil, nil, nil, quickSearchCluster)
	filters := container.NewVBox(
		container.NewGridWithColumns(3,
			labeledEntry("Patient", patientName),
			labeledEntry("Patient ID", patientID),
			labeledEntry("Modality", modality),
		),
		container.NewGridWithColumns(3,
			labeledEntry("Study Date From", studyDateFrom),
			labeledEntry("Study Date To", studyDateTo),
			labeledEntry("Source", sourcePath),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Accession", accession),
			labeledEntry("Description", studyDescription),
			labeledEntry("Imported From", importedAtFrom),
			labeledEntry("Imported To", importedAtTo),
		),
		container.NewGridWithColumns(3,
			labeledEntry("Series Modality", seriesModality),
			labeledEntry("Series #", seriesNumber),
			labeledEntry("Series Description", seriesDescription),
		),
		container.NewHBox(layout.NewSpacer(), applyButton, clearButton),
	)
	advancedFilters := widget.NewAccordion(widget.NewAccordionItem("Advanced Filters", filters))
	exportActions := container.NewHScroll(container.NewHBox(exportButton, exportJSONButton, exportSeriesCSVButton, exportSeriesJSONButton, exportImagesCSVButton, exportImagesJSONButton))
	return archiveControlSet{
		toolbarSearch:   quickRow,
		archiveControls: container.NewVBox(advancedFilters, exportActions),
	}
}

func labeledEntry(label string, entry *widget.Entry) fyne.CanvasObject {
	return labeledControl(label, entry)
}

func labeledControl(label string, control fyne.CanvasObject) fyne.CanvasObject {
	text := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	text.Wrapping = fyne.TextTruncate
	return container.NewBorder(nil, nil, text, nil, control)
}

type disableableControl interface {
	Disable()
	Enable()
}

func setDisableableControl(control disableableControl, disabled bool) {
	if control == nil {
		return
	}
	if disabled {
		control.Disable()
		return
	}
	control.Enable()
}

func archiveFiltersWithQuickSearch(filters archive.StudyFilters, query string) archive.StudyFilters {
	filters, _ = archiveFiltersWithQuickSearchField(filters, archiveQuickSearchPatientName, query)
	return filters
}

func archiveFiltersWithQuickSearchField(filters archive.StudyFilters, field string, query string) (archive.StudyFilters, bool) {
	return archiveFiltersWithQuickSearchFieldAndSoundex(filters, field, query, false)
}

func archiveFiltersWithQuickSearchFieldAndSoundex(filters archive.StudyFilters, field string, query string, soundex bool) (archive.StudyFilters, bool) {
	query = strings.TrimSpace(query)
	filters.PatientNameSoundex = false
	switch field {
	case archiveQuickSearchPatientName:
		filters.PatientName = query
		filters.PatientNameSoundex = query != "" && soundex
	case archiveQuickSearchPatientID:
		filters.PatientID = query
	case archiveQuickSearchAccession:
		filters.AccessionNumber = query
	default:
		return archive.StudyFilters{}, false
	}
	return filters, true
}

func queryDatePresetRange(preset string, now time.Time) (string, string, bool) {
	from, to, _, _, ok := queryDateTimePresetRange(preset, now)
	return from, to, ok
}

func queryDatePresetColumns() [][]string {
	columns := [][]string{
		{
			queryDatePresetAny,
			queryDatePresetTodayAM,
			queryDatePresetTodayPM,
			queryDatePresetToday,
			queryDatePresetYesterday,
			queryDatePresetDayBeforeYesterday,
			queryDatePresetLast2Days,
			queryDatePresetLast7Days,
		},
		{
			queryDatePresetLastMonth,
			queryDatePresetLast3Months,
			queryDatePresetOn,
			queryDatePresetBetween,
		},
		{
			queryDatePresetLast30Min,
			queryDatePresetLast1Hour,
			queryDatePresetLast2Hours,
			queryDatePresetLast3Hours,
			queryDatePresetLast6Hours,
			queryDatePresetLast8Hours,
			queryDatePresetLast12Hours,
			queryDatePresetLast24Hours,
			queryDatePresetLastNHours,
		},
	}
	out := make([][]string, len(columns))
	for i, column := range columns {
		out[i] = append([]string(nil), column...)
	}
	return out
}

func flattenQueryDatePresetColumns(columns [][]string) []string {
	var options []string
	for _, column := range columns {
		options = append(options, column...)
	}
	return options
}

func queryDatePresetPreservesManualRange(preset string) bool {
	return preset == queryDatePresetBetween
}

func queryDateTimePresetRange(preset string, now time.Time) (string, string, string, string, bool) {
	return queryDateTimePresetRangeWithInputs(preset, "", "", now)
}

func queryDateTimePresetRangeWithLastHours(preset string, hours string, now time.Time) (string, string, string, string, bool) {
	return queryDateTimePresetRangeWithInputs(preset, "", hours, now)
}

func queryDateTimePresetRangeWithInputs(preset string, onDate string, hours string, now time.Time) (string, string, string, string, bool) {
	formatDay := func(value time.Time) string {
		return value.Local().Format("20060102")
	}
	formatTime := func(value time.Time) string {
		return value.Local().Format("150405")
	}
	lastDurationRange := func(duration time.Duration) (string, string, string, string, bool) {
		if duration <= 0 {
			return "", "", "", "", false
		}
		from := now.Add(-duration)
		return formatDay(from), formatDay(now), formatTime(from), formatTime(now), true
	}
	switch preset {
	case queryDatePresetAny:
		return "", "", "", "", true
	case queryDatePresetOn:
		onDate = strings.TrimSpace(onDate)
		if onDate == "" {
			return "", "", "", "", false
		}
		return onDate, onDate, "", "", true
	case queryDatePresetTodayAM:
		day := formatDay(now)
		return day, day, "000000", "115959", true
	case queryDatePresetTodayPM:
		day := formatDay(now)
		return day, day, "120000", "235959", true
	case queryDatePresetToday:
		day := formatDay(now)
		return day, day, "", "", true
	case queryDatePresetYesterday:
		day := formatDay(now.AddDate(0, 0, -1))
		return day, day, "", "", true
	case queryDatePresetDayBeforeYesterday:
		day := formatDay(now.AddDate(0, 0, -2))
		return day, day, "", "", true
	case queryDatePresetLast2Days:
		return formatDay(now.AddDate(0, 0, -1)), formatDay(now), "", "", true
	case queryDatePresetLast7Days:
		return formatDay(now.AddDate(0, 0, -6)), formatDay(now), "", "", true
	case queryDatePresetLastMonth:
		return formatDay(now.AddDate(0, -1, 1)), formatDay(now), "", "", true
	case queryDatePresetLast3Months:
		return formatDay(now.AddDate(0, -3, 1)), formatDay(now), "", "", true
	case queryDatePresetLast30Min:
		return lastDurationRange(30 * time.Minute)
	case queryDatePresetLast1Hour:
		return lastDurationRange(time.Hour)
	case queryDatePresetLast2Hours:
		return lastDurationRange(2 * time.Hour)
	case queryDatePresetLast3Hours:
		return lastDurationRange(3 * time.Hour)
	case queryDatePresetLast6Hours:
		return lastDurationRange(6 * time.Hour)
	case queryDatePresetLast8Hours:
		return lastDurationRange(8 * time.Hour)
	case queryDatePresetLast12Hours:
		return lastDurationRange(12 * time.Hour)
	case queryDatePresetLast24Hours:
		return lastDurationRange(24 * time.Hour)
	case queryDatePresetLastNHours:
		count, err := strconv.Atoi(strings.TrimSpace(hours))
		if err != nil || count <= 0 {
			return "", "", "", "", false
		}
		return lastDurationRange(time.Duration(count) * time.Hour)
	default:
		return "", "", "", "", false
	}
}

func queryCriteriaWithQuickSearch(criteria query.Criteria, field string, value string) (query.Criteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	case queryQuickSearchAccession:
		criteria.AccessionNumber = value
	case queryQuickSearchBirthdate:
		criteria.PatientBirthDate = value
	case queryQuickSearchDescription:
		criteria.StudyDescription = value
	case queryQuickSearchReferringPhysician:
		criteria.ReferringPhysicianName = value
	case queryQuickSearchInstitution:
		criteria.InstitutionName = value
	case queryQuickSearchComments:
		criteria.PatientComments = value
	case queryQuickSearchCustomDICOMField:
		keyword, fieldValue, ok := parseCustomDICOMSearch(value)
		if !ok {
			return query.Criteria{}, false
		}
		criteria.CustomFieldKeyword = keyword
		criteria.CustomFieldValue = fieldValue
	case queryQuickSearchStatus:
		criteria.StudyStatusID = value
	default:
		return query.Criteria{}, false
	}
	return criteria, true
}

func parseCustomDICOMSearch(value string) (string, string, bool) {
	keyword, fieldValue, ok := strings.Cut(value, "=")
	if !ok {
		return "", "", false
	}
	keyword = strings.TrimSpace(keyword)
	fieldValue = strings.TrimSpace(fieldValue)
	if keyword == "" || fieldValue == "" {
		return "", "", false
	}
	return keyword, fieldValue, true
}

func queryPatientCriteriaWithQuickSearch(criteria query.PatientCriteria, field string, value string) (query.PatientCriteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	case queryQuickSearchBirthdate:
		criteria.PatientBirthDate = value
	case queryQuickSearchComments:
		criteria.PatientComments = value
	default:
		return query.PatientCriteria{}, false
	}
	return criteria, true
}

func querySeriesCriteriaWithQuickSearch(criteria query.SeriesCriteria, field string, value string) (query.SeriesCriteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	case queryQuickSearchDescription:
		criteria.SeriesDescription = value
	default:
		return query.SeriesCriteria{}, false
	}
	return criteria, true
}

func queryImageCriteriaWithQuickSearch(criteria query.ImageCriteria, field string, value string) (query.ImageCriteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	default:
		return query.ImageCriteria{}, false
	}
	return criteria, true
}

func queryModalityCriteriaText(manual string, checks map[string]*widget.Check) string {
	selected := selectedQueryModalities(checks)
	if len(selected) > 0 {
		return strings.Join(selected, "\\")
	}
	return strings.TrimSpace(manual)
}

func newQueryModalityChecks() map[string]*widget.Check {
	checks := make(map[string]*widget.Check, len(queryModalityCodes))
	for _, code := range queryModalityCodes {
		checks[code] = widget.NewCheck(code, nil)
	}
	return checks
}

func queryModalityColumns() [][]string {
	columns := [][]string{
		queryModalityCodes[:10],
		queryModalityCodes[10:],
	}
	out := make([][]string, len(columns))
	for i, column := range columns {
		out[i] = append([]string(nil), column...)
	}
	return out
}

func queryModalityGrid(checks map[string]*widget.Check) fyne.CanvasObject {
	columns := make([]fyne.CanvasObject, 0, 2)
	for _, codes := range queryModalityColumns() {
		columnChecks := make([]fyne.CanvasObject, 0, len(codes))
		for _, code := range codes {
			if check := checks[code]; check != nil {
				columnChecks = append(columnChecks, check)
			}
		}
		columns = append(columns, container.NewVBox(columnChecks...))
	}
	return container.NewGridWithColumns(2, columns...)
}

type queryDatePresetRadioGrid struct {
	groups    []*widget.RadioGroup
	selected  string
	updating  bool
	onChanged func(string)
}

func newQueryDatePresetRadioGrid(onChanged func(string)) *queryDatePresetRadioGrid {
	grid := &queryDatePresetRadioGrid{onChanged: onChanged}
	for groupIndex, options := range queryDatePresetColumns() {
		index := groupIndex
		group := widget.NewRadioGroup(options, func(preset string) {
			grid.selectFromGroup(index, preset)
		})
		group.Required = true
		grid.groups = append(grid.groups, group)
	}
	return grid
}

func (grid *queryDatePresetRadioGrid) CanvasObject() fyne.CanvasObject {
	if grid == nil {
		return container.NewGridWithColumns(3)
	}
	objects := make([]fyne.CanvasObject, 0, len(grid.groups))
	for _, group := range grid.groups {
		objects = append(objects, group)
	}
	return container.NewGridWithColumns(3, objects...)
}

func (grid *queryDatePresetRadioGrid) Selected() string {
	if grid == nil {
		return ""
	}
	return grid.selected
}

func (grid *queryDatePresetRadioGrid) SetSelected(preset string) {
	if grid == nil {
		return
	}
	grid.setSelected(preset, true)
}

func (grid *queryDatePresetRadioGrid) SetDisabled(disabled bool) {
	if grid == nil {
		return
	}
	for _, group := range grid.groups {
		setDisableableControl(group, disabled)
	}
}

func (grid *queryDatePresetRadioGrid) selectFromGroup(_ int, preset string) {
	if grid == nil || grid.updating || strings.TrimSpace(preset) == "" {
		return
	}
	grid.setSelected(preset, true)
}

func (grid *queryDatePresetRadioGrid) setSelected(preset string, notify bool) {
	if grid == nil || strings.TrimSpace(preset) == "" {
		return
	}
	grid.selected = preset
	grid.updating = true
	for _, group := range grid.groups {
		if stringSliceContains(group.Options, preset) {
			group.SetSelected(preset)
		} else {
			group.SetSelected("")
		}
	}
	grid.updating = false
	if notify && grid.onChanged != nil {
		grid.onChanged(preset)
	}
}

func stringSliceContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func splitModalities(value string) []string {
	var modalities []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			modalities = append(modalities, part)
		}
	}
	return modalities
}

func exportStudiesCSV(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.studies) == 0 {
		status.SetText("No studies to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteStudiesCSV(writer, state.studies); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d studies to %s", len(state.studies), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-studies.csv")
	picker.Show()
}

func exportStudiesJSON(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.studies) == 0 {
		status.SetText("No studies to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteStudiesJSON(writer, state.studies); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d studies to %s", len(state.studies), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-studies.json")
	picker.Show()
}

func exportSeriesCSV(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.series) == 0 {
		status.SetText("No series to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteSeriesCSV(writer, state.series); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d series to %s", len(state.series), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-series.csv")
	picker.Show()
}

func exportSeriesJSON(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.series) == 0 {
		status.SetText("No series to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteSeriesJSON(writer, state.series); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d series to %s", len(state.series), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-series.json")
	picker.Show()
}

func exportImagesCSV(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.instances) == 0 {
		status.SetText("No images to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteInstancesCSV(writer, state.instances); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d images to %s", len(state.instances), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-images.csv")
	picker.Show()
}

func exportImagesJSON(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.instances) == 0 {
		status.SetText("No images to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteInstancesJSON(writer, state.instances); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d images to %s", len(state.instances), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-images.json")
	picker.Show()
}

func refreshStudies(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	studies, err := loadStudies(context.Background(), state)
	if err != nil {
		status.SetText("Refresh failed")
		dialog.ShowError(err, w)
		return
	}
	setStudies(state, tables, studies)
	status.SetText(fmt.Sprintf("%d studies in local archive", len(studies)))
}

func loadStudies(ctx context.Context, state *uiState) ([]archive.Study, error) {
	return state.catalog.StudiesWithFilters(ctx, state.studyFilters)
}

func setStudies(state *uiState, tables archiveTables, studies []archive.Study) {
	selectedStudyUID := selectedArchiveStudyUID(state)
	state.studies = studies
	state.collapsedPatientGroups = retainCollapsedPatientGroups(state.collapsedPatientGroups, studies)
	state.collapsedArchiveStudies = retainCollapsedArchiveStudies(state.collapsedArchiveStudies, studies)
	state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
	state.selectedStudyRow = archiveStudyIndexByUID(studies, selectedStudyUID)
	clearArchiveDetails(state, tables)
	if tables.studies != nil {
		tables.studies.Refresh()
	}
	refreshArchiveChrome(state)
}

func retainCollapsedPatientGroups(collapsed map[string]bool, studies []archive.Study) map[string]bool {
	if len(collapsed) == 0 || len(studies) == 0 {
		return nil
	}
	valid := map[string]bool{}
	for _, study := range studies {
		valid[archivePatientKey(study)] = true
	}
	retained := map[string]bool{}
	for key, isCollapsed := range collapsed {
		if isCollapsed && valid[key] {
			retained[key] = true
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

func retainCollapsedArchiveStudies(collapsed map[string]bool, studies []archive.Study) map[string]bool {
	if len(collapsed) == 0 || len(studies) == 0 {
		return nil
	}
	valid := map[string]bool{}
	for _, study := range studies {
		if studyUID := strings.TrimSpace(study.StudyInstanceUID); studyUID != "" {
			valid[studyUID] = true
		}
	}
	retained := map[string]bool{}
	for studyUID, isCollapsed := range collapsed {
		if isCollapsed && valid[strings.TrimSpace(studyUID)] {
			retained[studyUID] = true
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

func clearArchiveDetails(state *uiState, tables archiveTables) {
	state.series = nil
	state.instances = nil
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	if tables.series != nil {
		tables.series.Refresh()
	}
	if tables.instances != nil {
		tables.instances.Refresh()
	}
	refreshArchiveChrome(state)
}

func setSeries(state *uiState, tables archiveTables, series []archive.Series) {
	state.series = series
	state.instances = nil
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	if study, ok := selectedStudy(state); ok && strings.TrimSpace(study.StudyInstanceUID) != "" {
		if state.archiveSeriesByStudy == nil {
			state.archiveSeriesByStudy = map[string][]archive.Series{}
		}
		state.archiveSeriesByStudy[study.StudyInstanceUID] = series
		state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(state.studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
		tables.studies.Refresh()
	}
	tables.series.Refresh()
	tables.instances.Refresh()
	refreshArchiveChrome(state)
}

func setInstances(state *uiState, tables archiveTables, instances []archive.Instance) {
	state.instances = instances
	state.selectedInstanceRow = -1
	tables.instances.Refresh()
	refreshArchiveChrome(state)
}

func showStudyMetadataDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to edit")
		}
		return
	}
	statusEntry := widget.NewEntry()
	statusEntry.SetText(study.Status)
	statusPreset := widget.NewSelect(studyStatusPresetOptions(), func(label string) {
		if value := studyStatusPresetValue(label); value != "" {
			statusEntry.SetText(value)
		}
	})
	statusPreset.SetSelected(studyStatusPresetLabel(study.Status))
	commentsEntry := widget.NewMultiLineEntry()
	commentsEntry.SetText(study.Comments)
	form := dialog.NewForm(
		"Study Status/Comments",
		"Save",
		"Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Status Preset", statusPreset),
			widget.NewFormItem("Status", statusEntry),
			widget.NewFormItem("Comments", commentsEntry),
		},
		func(save bool) {
			if !save {
				return
			}
			metadata := archive.StudyMetadata{Status: statusEntry.Text, Comments: commentsEntry.Text}
			if err := saveSelectedStudyMetadata(context.Background(), status, tables, state, metadata); err != nil {
				if status != nil {
					status.SetText("Study metadata update failed")
				}
				dialog.ShowError(err, w)
			}
		},
		w,
	)
	form.Resize(fyne.NewSize(540, 360))
	form.Show()
}

func saveSelectedStudyMetadata(ctx context.Context, status *widget.Label, tables archiveTables, state *uiState, metadata archive.StudyMetadata) error {
	if state == nil || state.catalog == nil {
		return errors.New("archive catalog unavailable")
	}
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to edit")
		}
		return errors.New("study selection required")
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		return errors.New("selected study has no Study Instance UID")
	}
	if err := state.catalog.SetStudyMetadata(ctx, studyUID, metadata); err != nil {
		return err
	}
	stored, err := state.catalog.StudyMetadata(ctx, studyUID)
	if err != nil {
		return err
	}
	state.studies[state.selectedStudyRow].Status = stored.Status
	state.studies[state.selectedStudyRow].Comments = stored.Comments
	if tables.studies != nil {
		tables.studies.Refresh()
	}
	refreshArchiveChrome(state)
	if status != nil {
		status.SetText("Updated study status/comments")
	}
	return nil
}

func showDeleteStudyDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to delete")
		}
		return
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		if status != nil {
			status.SetText("Selected study has no Study Instance UID")
		}
		return
	}
	message := fmt.Sprintf("Delete study %s from the local archive?", studyUID)
	if strings.TrimSpace(study.StudyDescription) != "" {
		message = fmt.Sprintf("%s\n\n%s", message, study.StudyDescription)
	}
	dialog.ShowConfirm("Delete Study", message, func(ok bool) {
		if !ok {
			return
		}
		if _, err := deleteSelectedStudy(context.Background(), status, tables, state); err != nil {
			if status != nil {
				status.SetText("Delete study failed")
			}
			dialog.ShowError(err, w)
		}
	}, w)
}

func deleteSelectedStudy(ctx context.Context, status *widget.Label, tables archiveTables, state *uiState) (int, error) {
	if state == nil || state.catalog == nil {
		return 0, errors.New("archive catalog unavailable")
	}
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to delete")
		}
		return 0, errors.New("study selection required")
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		return 0, errors.New("selected study has no Study Instance UID")
	}
	deleted, err := state.catalog.DeleteStudy(ctx, studyUID)
	if err != nil {
		return 0, err
	}
	if state.archiveSeriesByStudy != nil {
		delete(state.archiveSeriesByStudy, studyUID)
	}
	if state.collapsedArchiveStudies != nil {
		delete(state.collapsedArchiveStudies, studyUID)
	}
	studies, err := loadStudies(ctx, state)
	if err != nil {
		if status != nil {
			status.SetText("Study deleted; refresh failed")
		}
		return deleted, err
	}
	setStudies(state, tables, studies)
	if status != nil {
		status.SetText(deletedStudyStatusText(studyUID, deleted))
	}
	return deleted, nil
}

func deletedStudyStatusText(studyUID string, deleted int) string {
	noun := "objects"
	if deleted == 1 {
		noun = "object"
	}
	return fmt.Sprintf("Deleted study %s (%d %s)", studyUID, deleted, noun)
}

func localAETitle(state *uiState) string {
	if state == nil || strings.TrimSpace(state.appConfig.LocalAETitle) == "" {
		return netverify.DefaultCallingAETitle
	}
	return state.appConfig.LocalAETitle
}

func queryMoveDestination(state *uiState, node nodes.Node) string {
	if state != nil && strings.TrimSpace(state.queryMoveDestination) != "" {
		return strings.TrimSpace(state.queryMoveDestination)
	}
	if strings.TrimSpace(node.PreferredMoveDestination) != "" {
		return strings.TrimSpace(node.PreferredMoveDestination)
	}
	if state != nil && state.receiver != nil {
		if aeTitle := strings.TrimSpace(state.receiver.AETitle()); aeTitle != "" {
			return aeTitle
		}
	}
	return localAETitle(state)
}

func queryMoveDestinationOptions(state *uiState) []string {
	seen := map[string]bool{}
	var options []string
	appendOption := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		options = append(options, value)
	}
	appendOption(localAETitle(state))
	if state != nil && state.receiver != nil {
		appendOption(state.receiver.AETitle())
	}
	if node, ok := selectedQueryNode(state); ok {
		appendOption(node.PreferredMoveDestination)
	}
	if state != nil {
		appendOption(state.queryMoveDestination)
	}
	return options
}

func queryDestinationText(state *uiState) string {
	moveDestination := localAETitle(state)
	receiveAddress := ""
	source := "no source selected"
	method := "Auto C-MOVE/C-GET"
	if state != nil {
		receiveAddress = state.appConfig.ReceiverAddress
		if state.receiver != nil {
			snapshot := state.receiver.Snapshot()
			if strings.TrimSpace(snapshot.Address) != "" {
				receiveAddress = snapshot.Address
			}
			if strings.TrimSpace(snapshot.AETitle) != "" {
				moveDestination = snapshot.AETitle
			}
		}
		if node, ok := selectedQueryNode(state); ok {
			source = "source " + node.Name
			moveDestination = queryMoveDestination(state, node)
			method = retrieveMethodSummary(node)
		} else if strings.TrimSpace(state.queryMoveDestination) != "" {
			moveDestination = strings.TrimSpace(state.queryMoveDestination)
		}
	}
	return fmt.Sprintf("Retrieve to: %s via %s (%s, %s)", emptyDash(moveDestination), emptyDash(receiveAddress), source, method)
}

func newQueryMoveDestinationEntry(state *uiState) *widget.SelectEntry {
	entry := widget.NewSelectEntry(queryMoveDestinationOptions(state))
	entry.SetPlaceHolder("Move destination AE")
	entry.OnChanged = func(value string) {
		if state != nil {
			state.queryMoveDestination = strings.TrimSpace(value)
		}
		refreshQueryDestination(state)
	}
	options := queryMoveDestinationOptions(state)
	if len(options) > 0 {
		entry.SetText(options[0])
	}
	return entry
}

func refreshQueryDestination(state *uiState) {
	if state == nil {
		return
	}
	if state.queryMoveDestinationSelect != nil {
		options := queryMoveDestinationOptions(state)
		state.queryMoveDestinationSelect.SetOptions(options)
		if strings.TrimSpace(state.queryMoveDestinationSelect.Text) == "" && len(options) > 0 {
			state.queryMoveDestinationSelect.SetText(options[0])
		}
		state.queryMoveDestinationSelect.Refresh()
	}
	if state.queryDestinationLabel != nil {
		state.queryDestinationLabel.SetText(queryDestinationText(state))
	}
}

func queryResultSummaryText(state *uiState) string {
	count := 0
	if state != nil {
		count = len(state.queries)
	}
	noun := queryResultSummaryNoun(state, count)
	source := queryResultSourceSummaryText(state)
	lines := []string{fmt.Sprintf("%d %s found", count, noun), source}
	if selected := querySelectedRetrieveSummaryText(state); selected != "" {
		lines = append(lines, selected)
	}
	return strings.Join(lines, "\n")
}

func queryResultSourceSummaryText(state *uiState) string {
	if sourceLabels := queryResultSourceLabels(state); len(sourceLabels) > 1 {
		return fmt.Sprintf("%d sources: %s", len(sourceLabels), strings.Join(sourceLabels, ", "))
	} else if len(sourceLabels) == 1 {
		return sourceLabels[0]
	}
	if node, ok := selectedQueryNode(state); ok {
		return queryNodeSourceSummaryLabel(node)
	}
	return "no source selected"
}

func queryResultSourceLabels(state *uiState) []string {
	if state == nil {
		return nil
	}
	seen := map[string]bool{}
	var labels []string
	for _, match := range state.queries {
		label := queryMatchSourceSummaryLabel(match)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	return labels
}

func queryMatchSourceSummaryLabel(match query.Match) string {
	name := strings.TrimSpace(match.SourceNodeName)
	host := strings.TrimSpace(match.SourceHost)
	if name != "" {
		return name
	}
	if host != "" && match.SourcePort != 0 {
		return fmt.Sprintf("%s:%d", host, match.SourcePort)
	}
	return ""
}

func queryNodeSourceSummaryLabel(node nodes.Node) string {
	return fmt.Sprintf("%s / %s:%d", emptyDash(node.Name), emptyDash(node.Host), node.Port)
}

func queryResultSummaryNoun(state *uiState, count int) string {
	singular := "study"
	plural := "studies"
	if state != nil {
		switch state.lastQuery.kind {
		case queryRunPatient:
			singular, plural = "patient", "patients"
		case queryRunSeries:
			singular, plural = "series", "series"
		case queryRunImage:
			singular, plural = "image", "images"
		}
	}
	if count == 1 {
		return singular
	}
	return plural
}

func querySelectedRetrieveSummaryText(state *uiState) string {
	match, ok := selectedQuery(state)
	if !ok {
		return ""
	}
	_, label, ok := queryRetrieveLevelAndLabel(match)
	if !ok {
		return ""
	}
	if status := queryRetrieveRowStatus(state, match); status != "" {
		return "Selected: " + status
	}
	source := ""
	if node, ok := nodeForQueryMatch(state, match); ok && strings.TrimSpace(node.Name) != "" {
		source = " from " + strings.TrimSpace(node.Name)
	}
	return "Selected: retrieve " + label + source
}

func queryRetrieveRowStatus(state *uiState, match query.Match) string {
	if state == nil || len(state.queryRetrieveRows) == 0 {
		return ""
	}
	return state.queryRetrieveRows[queryRetrieveStatusKey(match)]
}

func recordQueryRetrieveRowStatus(state *uiState, match query.Match, text string) {
	if state == nil {
		return
	}
	key := queryRetrieveStatusKey(match)
	if key == "" {
		return
	}
	if state.queryRetrieveRows == nil {
		state.queryRetrieveRows = map[string]string{}
	}
	state.queryRetrieveRows[key] = text
}

func queryRetrieveStatusKey(match query.Match) string {
	level, _, ok := queryRetrieveLevelAndLabel(match)
	if !ok {
		return ""
	}
	match.QueryRetrieveLevel = level
	return autoQueryRetrieveCandidateKey(match)
}

func refreshQueryResultSummary(state *uiState) {
	if state == nil || state.queryResultSummaryLabel == nil {
		return
	}
	state.queryResultSummaryLabel.SetText(queryResultSummaryText(state))
}

func querySelectedDetailsText(state *uiState) string {
	match, ok := selectedQuery(state)
	if !ok {
		return "No query result selected"
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "" {
		level = "STUDY"
	}
	return strings.Join([]string{
		"Level: " + level,
		"Study UID: " + emptyDash(match.StudyInstanceUID),
		"Series UID: " + emptyDash(match.SeriesInstanceUID),
		"SOP Class UID: " + emptyDash(match.SOPClassUID),
		"SOP Instance UID: " + emptyDash(match.SOPInstanceUID),
		"Source: " + emptyDash(queryCell(match, queryTableColumnSource)),
	}, "\n")
}

func refreshQuerySelectedDetails(state *uiState) {
	if state == nil || state.querySelectedDetailsLabel == nil {
		return
	}
	state.querySelectedDetailsLabel.SetText(querySelectedDetailsText(state))
}

type querySourceListCell struct {
	*fyne.Container
	check     *widget.Check
	verifyDot *canvas.Circle
	queryDot  *canvas.Circle
}

func newQuerySourceListCell() *querySourceListCell {
	verifyDot := newSourceStatusDot()
	queryDot := newSourceStatusDot()
	check := widget.NewCheck("", nil)
	dots := container.NewHBox(sourceStatusDotBox(verifyDot), sourceStatusDotBox(queryDot))
	return &querySourceListCell{
		Container: container.NewBorder(nil, nil, dots, nil, check),
		check:     check,
		verifyDot: verifyDot,
		queryDot:  queryDot,
	}
}

func newSourceStatusDot() *canvas.Circle {
	dot := canvas.NewCircle(sourceStatusIdleColor)
	dot.StrokeColor = color.NRGBA{R: 26, G: 26, B: 26, A: 255}
	dot.StrokeWidth = 1
	return dot
}

func sourceStatusDotBox(dot *canvas.Circle) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(10, 10), dot)
}

func configureQuerySourceCell(cell *querySourceListCell, state *uiState, id widget.ListItemID, onChanged func()) {
	if cell == nil {
		return
	}
	configureQuerySourceCheck(cell.check, state, id, onChanged)
	if state == nil || id < 0 || id >= len(state.nodes) {
		applySourceStatusDot(cell.verifyDot, sourceStatusIdleColor)
		applySourceStatusDot(cell.queryDot, sourceStatusIdleColor)
		return
	}
	node := state.nodes[id]
	applySourceStatusDot(cell.verifyDot, nodeVerifyStatusDotColor(state, node))
	applySourceStatusDot(cell.queryDot, querySourceStatusDotColor(state, node))
}

func applySourceStatusDot(dot *canvas.Circle, fill color.NRGBA) {
	if dot == nil {
		return
	}
	dot.FillColor = fill
	dot.Refresh()
}

func nodeVerifyStatusDotColor(state *uiState, node nodes.Node) color.NRGBA {
	if state == nil {
		return sourceStatusIdleColor
	}
	switch state.nodeVerifyStatuses[nodeVerifyKey(node)] {
	case nodeVerifyOK:
		return sourceStatusOKColor
	case nodeVerifyFail:
		return sourceStatusFailColor
	default:
		return sourceStatusIdleColor
	}
}

func querySourceStatusDotColor(state *uiState, node nodes.Node) color.NRGBA {
	if state == nil {
		return sourceStatusIdleColor
	}
	switch state.querySourceStatuses[nodeVerifyKey(node)] {
	case querySourceOK:
		return sourceStatusOKColor
	case querySourceFail:
		return sourceStatusFailColor
	default:
		return sourceStatusIdleColor
	}
}

func querySourceRows(state *uiState) []string {
	if state == nil || len(state.nodes) == 0 {
		return []string{"No remote sources configured"}
	}
	rows := make([]string, 0, len(state.nodes))
	for index, node := range state.nodes {
		prefix := "  "
		if index == state.selectedNodeRow {
			prefix = "▶ "
		}
		check := "[x]"
		if !node.Enabled() || !node.QueryEnabled() {
			check = "[ ]"
		}
		rows = append(rows, fmt.Sprintf("%s%s %s%s %s:%d", prefix, check, querySourceMarkers(state, node), node.Name, node.Host, node.Port))
	}
	return rows
}

func querySourceChecked(node nodes.Node) bool {
	return node.Enabled() && node.QueryEnabled()
}

func querySourceCheckLabel(state *uiState, index int) string {
	if state == nil || index < 0 || index >= len(state.nodes) {
		return ""
	}
	node := state.nodes[index]
	prefix := "  "
	if index == state.selectedNodeRow {
		prefix = "▶ "
	}
	return fmt.Sprintf("%s%s %s:%d%s", prefix, node.Name, node.Host, node.Port, querySourceDisabledSuffix(node))
}

func querySourceDisabledSuffix(node nodes.Node) string {
	if !node.Enabled() {
		return " (disabled)"
	}
	if !node.QueryEnabled() {
		return " (query off)"
	}
	return ""
}

type autoQuerySourceEntry struct {
	node      nodes.Node
	nodeIndex int
	source    autoquery.Source
}

func autoQuerySourceEntries(state *uiState) []autoQuerySourceEntry {
	if state == nil || len(state.nodes) == 0 {
		return nil
	}
	sources := autoQuerySourcesForState(state)
	state.autoQuerySources = sources
	type nodeEntry struct {
		node  nodes.Node
		index int
	}
	nodeByKey := make(map[string]nodeEntry, len(state.nodes))
	for index, node := range state.nodes {
		nodeByKey[autoQueryNodeKey(node)] = nodeEntry{node: node, index: index}
	}
	var entries []autoQuerySourceEntry
	for _, source := range sources {
		node, ok := nodeByKey[autoQuerySourceKey(source)]
		if !ok {
			continue
		}
		entries = append(entries, autoQuerySourceEntry{node: node.node, nodeIndex: node.index, source: source})
	}
	return entries
}

func autoQuerySourceChecked(entry autoQuerySourceEntry) bool {
	return entry.source.Enabled && entry.node.Enabled() && entry.node.QueryEnabled()
}

func autoQuerySourceRows(state *uiState) []string {
	entries := autoQuerySourceEntries(state)
	if len(entries) == 0 {
		return []string{"No remote sources configured"}
	}
	rows := make([]string, 0, len(entries))
	for index, entry := range entries {
		prefix := "  "
		if index == state.selectedAutoQuerySourceRow {
			prefix = "▶ "
		}
		check := "[ ]"
		if autoQuerySourceChecked(entry) {
			check = "[x]"
		}
		rows = append(rows, fmt.Sprintf("%s%s %s%s %s:%d", prefix, check, querySourceMarkers(state, entry.node), entry.node.Name, entry.node.Host, entry.node.Port))
	}
	return rows
}

func autoQuerySourceCheckLabel(state *uiState, index int) string {
	entries := autoQuerySourceEntries(state)
	if state == nil || index < 0 || index >= len(entries) {
		return ""
	}
	node := entries[index].node
	prefix := "  "
	if index == state.selectedAutoQuerySourceRow {
		prefix = "▶ "
	}
	return fmt.Sprintf("%s%s %s:%d%s", prefix, node.Name, node.Host, node.Port, querySourceDisabledSuffix(node))
}

func setAutoQuerySourceEnabled(state *uiState, row int, enabled bool) bool {
	entries := autoQuerySourceEntries(state)
	if state == nil || row < 0 || row >= len(entries) {
		return false
	}
	sources := autoQuerySourcesForState(state)
	if row >= len(sources) || sources[row].Enabled == enabled {
		return false
	}
	sources[row].Enabled = enabled
	state.autoQuerySources = sources
	return true
}

func moveAutoQuerySource(state *uiState, delta int) bool {
	sources := autoQuerySourcesForState(state)
	if state == nil || delta == 0 || len(sources) == 0 {
		return false
	}
	row := state.selectedAutoQuerySourceRow
	if row < 0 || row >= len(sources) {
		return false
	}
	nextRow := row + delta
	if nextRow < 0 || nextRow >= len(sources) {
		return false
	}
	sources[row], sources[nextRow] = sources[nextRow], sources[row]
	state.autoQuerySources = sources
	state.selectedAutoQuerySourceRow = nextRow
	return true
}

func autoQuerySourceNodes(state *uiState) []nodes.Node {
	entries := autoQuerySourceEntries(state)
	var out []nodes.Node
	for _, entry := range entries {
		if autoQuerySourceChecked(entry) {
			out = append(out, entry.node)
		}
	}
	return out
}

func configureAutoQuerySourceCheck(check *widget.Check, state *uiState, id widget.ListItemID, onChanged func()) {
	if check == nil {
		return
	}
	check.OnChanged = nil
	entries := autoQuerySourceEntries(state)
	if state == nil || id < 0 || id >= len(entries) {
		check.Text = ""
		check.SetChecked(false)
		check.Disable()
		check.Refresh()
		return
	}
	check.Enable()
	check.Text = autoQuerySourceCheckLabel(state, id)
	check.SetChecked(autoQuerySourceChecked(entries[id]))
	if autoQueryProfileLocked(state) {
		check.Disable()
	}
	check.OnChanged = func(checked bool) {
		if autoQueryProfileLocked(state) {
			check.SetChecked(autoQuerySourceChecked(entries[id]))
			return
		}
		state.selectedAutoQuerySourceRow = id
		state.selectedNodeRow = entries[id].nodeIndex
		changed := setAutoQuerySourceEnabled(state, id, checked)
		if changed && onChanged != nil {
			onChanged()
		}
	}
	check.Refresh()
}

func configureAutoQuerySourceCell(cell *querySourceListCell, state *uiState, id widget.ListItemID, onChanged func()) {
	if cell == nil {
		return
	}
	configureAutoQuerySourceCheck(cell.check, state, id, onChanged)
	entries := autoQuerySourceEntries(state)
	if state == nil || id < 0 || id >= len(entries) {
		applySourceStatusDot(cell.verifyDot, sourceStatusIdleColor)
		applySourceStatusDot(cell.queryDot, sourceStatusIdleColor)
		return
	}
	node := entries[id].node
	applySourceStatusDot(cell.verifyDot, nodeVerifyStatusDotColor(state, node))
	applySourceStatusDot(cell.queryDot, querySourceStatusDotColor(state, node))
}

func setQuerySourceEnabled(state *uiState, row int, enabled bool) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	if enabled {
		next.Disabled = false
		next.QueryDisabled = false
	} else {
		next.QueryDisabled = true
	}
	if next == state.nodes[row] {
		return false, nil
	}
	original := state.nodes[row]
	state.nodes[row] = next
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes[row] = original
			return true, err
		}
	}
	return true, nil
}

func setAllNodesEnabled(state *uiState, enabled bool) (bool, error) {
	if state == nil || len(state.nodes) == 0 {
		return false, nil
	}
	next := append([]nodes.Node(nil), state.nodes...)
	changed := false
	for index := range next {
		disabled := !enabled
		if next[index].Disabled != disabled {
			next[index].Disabled = disabled
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	original := append([]nodes.Node(nil), state.nodes...)
	state.nodes = next
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes = original
			return true, err
		}
	}
	refreshNodeTableRows(state)
	return true, nil
}

func moveQuerySource(state *uiState, delta int) (bool, error) {
	if state == nil || delta == 0 || len(state.nodes) == 0 {
		return false, nil
	}
	row := state.selectedNodeRow
	if row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	nextRow := row + delta
	if nextRow < 0 || nextRow >= len(state.nodes) {
		return false, nil
	}
	next := append([]nodes.Node(nil), state.nodes...)
	next[row], next[nextRow] = next[nextRow], next[row]
	original := state.nodes
	originalRow := state.selectedNodeRow
	state.nodes = next
	state.selectedNodeRow = nextRow
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes = original
			state.selectedNodeRow = originalRow
			return true, err
		}
	}
	refreshNodeTableRows(state)
	return true, nil
}

func configureQuerySourceCheck(check *widget.Check, state *uiState, id widget.ListItemID, onChanged func()) {
	if check == nil {
		return
	}
	check.OnChanged = nil
	if state == nil || id < 0 || id >= len(state.nodes) {
		check.Text = ""
		check.SetChecked(false)
		check.Disable()
		check.Refresh()
		return
	}
	check.Enable()
	check.Text = querySourceCheckLabel(state, id)
	check.SetChecked(querySourceChecked(state.nodes[id]))
	check.OnChanged = func(checked bool) {
		state.selectedNodeRow = id
		changed, err := setQuerySourceEnabled(state, id, checked)
		if err != nil {
			check.SetChecked(querySourceChecked(state.nodes[id]))
			return
		}
		if changed && onChanged != nil {
			onChanged()
		}
	}
	check.Refresh()
}

func nodeVerifyKey(node nodes.Node) string {
	if strings.TrimSpace(node.ID) != "" {
		return "id:" + strings.TrimSpace(node.ID)
	}
	return fmt.Sprintf("endpoint:%s:%s:%d", strings.TrimSpace(node.Name), strings.TrimSpace(node.Host), node.Port)
}

func recordNodeVerifyStatus(state *uiState, node nodes.Node, status nodeVerifyStatus) {
	if state == nil {
		return
	}
	if state.nodeVerifyStatuses == nil {
		state.nodeVerifyStatuses = map[string]nodeVerifyStatus{}
	}
	state.nodeVerifyStatuses[nodeVerifyKey(node)] = status
}

func nodeVerifyStatusMarker(state *uiState, node nodes.Node) string {
	if state == nil {
		return ""
	}
	switch state.nodeVerifyStatuses[nodeVerifyKey(node)] {
	case nodeVerifyOK:
		return "✓ "
	case nodeVerifyFail:
		return "! "
	default:
		return ""
	}
}

func recordQuerySourceStatus(state *uiState, node nodes.Node, status querySourceStatus) {
	if state == nil {
		return
	}
	if state.querySourceStatuses == nil {
		state.querySourceStatuses = map[string]querySourceStatus{}
	}
	state.querySourceStatuses[nodeVerifyKey(node)] = status
}

func querySourceStatusMarker(state *uiState, node nodes.Node) string {
	if state == nil {
		return ""
	}
	switch state.querySourceStatuses[nodeVerifyKey(node)] {
	case querySourceOK:
		return "Q✓ "
	case querySourceFail:
		return "Q! "
	default:
		return ""
	}
}

func querySourceMarkers(state *uiState, node nodes.Node) string {
	return nodeVerifyStatusMarker(state, node) + querySourceStatusMarker(state, node)
}

func refreshQuerySourceList(state *uiState) {
	if state == nil || state.querySourceList == nil {
		return
	}
	state.querySourceList.Refresh()
}

func beginRetrieve(state *uiState, nodeName string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	state.activeRetrieveCancel = cancel
	state.retrieveActivityNode = nodeName
	state.retrieveActivityProgress = retrieve.Progress{}
	refreshArchiveChrome(state)
	return ctx, cancel
}

func beginQueryActivity(state *uiState, label string) {
	if state == nil {
		return
	}
	state.activeQueryActivityLabel = strings.TrimSpace(label)
	refreshArchiveChrome(state)
}

func clearActiveQueryActivity(state *uiState) {
	if state == nil {
		return
	}
	state.activeQueryActivityLabel = ""
	refreshArchiveChrome(state)
}

func beginSendActivity(state *uiState, label string) {
	if state == nil {
		return
	}
	state.activeSendActivityLabel = strings.TrimSpace(label)
	refreshArchiveChrome(state)
}

func clearActiveSendActivity(state *uiState) {
	if state == nil {
		return
	}
	state.activeSendActivityLabel = ""
	refreshArchiveChrome(state)
}

func beginImportActivity(state *uiState, label string) {
	if state == nil {
		return
	}
	state.activeImportActivityLabel = strings.TrimSpace(label)
	refreshArchiveChrome(state)
}

func clearActiveImportActivity(state *uiState) {
	if state == nil {
		return
	}
	state.activeImportActivityLabel = ""
	refreshArchiveChrome(state)
}

func clearActiveRetrieve(state *uiState) {
	state.activeRetrieveCancel = nil
	refreshArchiveChrome(state)
}

func cancelActiveRetrieve(status *widget.Label, state *uiState) {
	if state.activeRetrieveCancel == nil {
		status.SetText("No active retrieve")
		return
	}
	cancel := state.activeRetrieveCancel
	state.activeRetrieveCancel = nil
	cancel()
	status.SetText("Cancelling active retrieve")
	refreshArchiveChrome(state)
}

func retrieveProgressCallback(status *widget.Label, state *uiState, nodeName string) func(retrieve.Progress) {
	return func(update retrieve.Progress) {
		fyne.Do(func() {
			if state != nil {
				state.retrieveActivityNode = nodeName
				state.retrieveActivityProgress = update
				refreshArchiveChrome(state)
			}
			status.SetText(fmt.Sprintf(
				"Retrieve %s progress: status=0x%04X remaining %d completed %d failed %d warnings %d",
				nodeName,
				update.FinalStatus,
				update.Remaining,
				update.Completed,
				update.Failed,
				update.Warnings,
			))
		})
	}
}

func retrieveProgressFraction(progress retrieve.Progress) float64 {
	done := int(progress.Completed) + int(progress.Failed) + int(progress.Warnings)
	total := done + int(progress.Remaining)
	if total == 0 {
		return 0
	}
	return float64(done) / float64(total)
}

func retrieveProgressText(nodeName string, progress retrieve.Progress) string {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		nodeName = "active"
	}
	done := int(progress.Completed) + int(progress.Failed) + int(progress.Warnings)
	total := done + int(progress.Remaining)
	if total == 0 {
		return fmt.Sprintf("Retrieve %s active", nodeName)
	}
	return fmt.Sprintf("Retrieve %s %d/%d done, failed %d, warnings %d", nodeName, done, total, progress.Failed, progress.Warnings)
}

func retrieveMethodName(outcome retrieve.Outcome) string {
	if outcome.Method != "" {
		return outcome.Method
	}
	return retrieve.MethodMove
}

func retrieveOptionsForNode(status *widget.Label, state *uiState, node nodes.Node) retrieve.Options {
	moveDestination := queryMoveDestination(state, node)
	return retrieve.Options{
		CallingAETitle:      localAETitle(state),
		Method:              retrieveOptionMethod(node),
		MoveDestination:     moveDestination,
		ReceiveAddress:      state.appConfig.ReceiverAddress,
		Receiver:            state.receiver,
		MaxStoreObjectBytes: optionalInt64Value(state.appConfig.MaxStoreObjectBytes),
		OnProgress:          retrieveProgressCallback(status, state, node.Name),
	}
}

func retrieveOptionMethod(node nodes.Node) string {
	switch node.RetrieveMethodOrDefault() {
	case nodes.RetrieveMethodMove:
		return retrieve.MethodMove
	case nodes.RetrieveMethodGet:
		return retrieve.MethodGet
	default:
		return ""
	}
}

func retrieveMethodSummary(node nodes.Node) string {
	switch node.RetrieveMethodOrDefault() {
	case nodes.RetrieveMethodMove:
		return retrieve.MethodMove
	case nodes.RetrieveMethodGet:
		return retrieve.MethodGet
	default:
		return "Auto C-MOVE/C-GET"
	}
}

func retrieveReceiverAddressIssue(state *uiState, node nodes.Node) string {
	if isLoopbackHost(node.Host) {
		return ""
	}
	address := state.appConfig.ReceiverAddress
	if state.receiver != nil {
		address = state.receiver.Snapshot().Address
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || !isLoopbackHost(host) {
		return ""
	}
	return fmt.Sprintf(
		"Receiver address %s is loopback; remote node %s cannot C-STORE to it. Use 0.0.0.0:11113 and configure the remote Move Destination to this Mac's LAN IP.",
		address,
		node.Name,
	)
}

func receiverAddressParts(address string) (string, string) {
	address = strings.TrimSpace(address)
	defaultHost, defaultPort, _ := net.SplitHostPort(receive.DefaultAddress)
	if address == "" {
		return defaultHost, defaultPort
	}
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		return host, port
	}
	return address, defaultPort
}

func receiverAddressFromParts(host string, port string) (string, error) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		defaultHost, _, _ := net.SplitHostPort(receive.DefaultAddress)
		host = defaultHost
	}
	if port == "" {
		_, defaultPort, _ := net.SplitHostPort(receive.DefaultAddress)
		port = defaultPort
	}
	portValue, err := parsePort(port)
	if err != nil {
		return "", fmt.Errorf("receiver port: %w", err)
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", portValue)), nil
}

func listenerAddressSummaryLines(hostname string, addresses []string, port string) []string {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "-"
	}
	lines := []string{"Host Name: " + hostname}
	endpoints := listenerReachableEndpoints(addresses, port)
	if len(endpoints) == 0 {
		return append(lines, "Reachable Addresses: -")
	}
	lines = append(lines, "Reachable Addresses:")
	for _, endpoint := range endpoints {
		lines = append(lines, "  "+endpoint)
	}
	return lines
}

func listenerReachableEndpoints(addresses []string, port string) []string {
	port = strings.TrimSpace(port)
	if port == "" {
		_, port, _ = net.SplitHostPort(receive.DefaultAddress)
	}
	var endpoints []string
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		endpoints = append(endpoints, net.JoinHostPort(address, port))
	}
	return endpoints
}

func listenerHostSelectOptions(currentHost string, addresses []string) []string {
	seen := map[string]bool{}
	var options []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		options = append(options, value)
	}
	add(currentHost)
	for _, address := range addresses {
		add(address)
	}
	return options
}

func newListenerHostSelect(receiverHost *widget.Entry, addresses []string, refresh func()) *widget.Select {
	options := listenerHostSelectOptions(receiverHost.Text, addresses)
	selectWidget := widget.NewSelect(options, func(host string) {
		host = strings.TrimSpace(host)
		if receiverHost != nil && host != "" {
			receiverHost.SetText(host)
		}
		if refresh != nil {
			refresh()
		}
	})
	if len(options) > 0 {
		selectWidget.SetSelected(options[0])
	}
	return selectWidget
}

func listenerSettingsStatusText(aeTitle string, host string, port string, activate bool, running *receive.Snapshot) string {
	if running != nil {
		return fmt.Sprintf("Running: %s on %s; stored %d objects", emptyDash(running.AETitle), emptyDash(running.Address), running.Stored)
	}
	address, err := receiverAddressFromParts(host, port)
	if err != nil {
		address = strings.TrimSpace(host)
		if strings.TrimSpace(port) != "" {
			address = net.JoinHostPort(address, strings.TrimSpace(port))
		}
	}
	stateText := "Stopped"
	if activate {
		stateText = "Will start on Save"
	}
	return fmt.Sprintf("%s: %s on %s", stateText, emptyDash(aeTitle), emptyDash(address))
}

func localReachableIPv4Addresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var addresses []string
	seen := map[string]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifaceAddresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, ifaceAddress := range ifaceAddresses {
			var ip net.IP
			switch value := ifaceAddress.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			text := ip.String()
			if !seen[text] {
				seen[text] = true
				addresses = append(addresses, text)
			}
		}
	}
	return addresses
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func showSettingsDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	localAE := widget.NewEntry()
	localAE.SetText(localAETitle(state))
	receiverHostValue, receiverPortValue := receiverAddressParts(state.appConfig.ReceiverAddress)
	receiverHost := widget.NewEntry()
	receiverHost.SetText(receiverHostValue)
	receiverPort := widget.NewEntry()
	receiverPort.SetText(receiverPortValue)
	activateListener := widget.NewCheck("Activate DICOM listener", nil)
	activateListener.SetChecked(state.appConfig.ReceiverAutoStart || state.receiver != nil)
	hostName, _ := os.Hostname()
	listenerAddresses := localReachableIPv4Addresses()
	listenerAddressSummary := compactWorkbenchLabel()
	listenerStatus := compactWorkbenchLabel()
	runningSnapshot := func() *receive.Snapshot {
		if state.receiver == nil {
			return nil
		}
		snapshot := state.receiver.Snapshot()
		return &snapshot
	}
	updateListenerStatus := func() {
		listenerStatus.SetText(listenerSettingsStatusText(localAE.Text, receiverHost.Text, receiverPort.Text, activateListener.Checked, runningSnapshot()))
	}
	updateListenerAddressSummary := func() {
		listenerAddressSummary.SetText(strings.Join(listenerAddressSummaryLines(hostName, listenerAddresses, receiverPort.Text), "\n"))
	}
	refreshListenerAddressControls := func() {
		updateListenerAddressSummary()
		updateListenerStatus()
	}
	hostSelect := newListenerHostSelect(receiverHost, listenerAddresses, refreshListenerAddressControls)
	copyAddressesButton := compactToolbarButton("Copy", theme.ContentCopyIcon(), func() {
		endpoints := listenerReachableEndpoints(listenerAddresses, receiverPort.Text)
		if len(endpoints) == 0 {
			status.SetText("No reachable listener addresses to copy")
			return
		}
		fyne.CurrentApp().Clipboard().SetContent(strings.Join(endpoints, "\n"))
		status.SetText("Copied listener addresses")
	})
	receiverPort.OnChanged = func(_ string) {
		refreshListenerAddressControls()
	}
	receiverHost.OnChanged = func(_ string) {
		updateListenerStatus()
	}
	localAE.OnChanged = func(_ string) {
		updateListenerStatus()
	}
	activateListener.OnChanged = func(_ bool) {
		updateListenerStatus()
	}
	updateListenerAddressSummary()
	updateListenerStatus()
	additionalAEs := widget.NewEntry()
	additionalAEs.SetText(strings.Join(state.appConfig.AdditionalAETitles, ", "))
	preferredReceiveSyntax := widget.NewSelect(receivePreferredSyntaxLabels(), nil)
	preferredReceiveSyntax.SetSelected(receivePreferredSyntaxLabel(state.appConfig.ReceivePreferredTransferSyntax))
	dicomCommunicationTimeout := widget.NewEntry()
	dicomCommunicationTimeout.SetText(strconv.Itoa(timeoutSecondsOrDefault(state.appConfig.DICOMCommunicationTimeoutSeconds, appconfig.DefaultDICOMCommunicationTimeoutSeconds)))
	dicomConnectionTimeout := widget.NewEntry()
	dicomConnectionTimeout.SetText(strconv.Itoa(timeoutSecondsOrDefault(state.appConfig.DICOMConnectionTimeoutSeconds, appconfig.DefaultDICOMConnectionTimeoutSeconds)))
	maxFileImportBytes := widget.NewEntry()
	maxFileImportBytes.SetText(formatOptionalInt64(state.appConfig.MaxFileImportBytes))
	maxZipEntryBytes := widget.NewEntry()
	maxZipEntryBytes.SetText(formatOptionalInt64(state.appConfig.MaxZipEntryBytes))
	maxZipTotalBytes := widget.NewEntry()
	maxZipTotalBytes.SetText(formatOptionalInt64(state.appConfig.MaxZipTotalBytes))
	maxZipEntryCount := widget.NewEntry()
	maxZipEntryCount.SetText(formatOptionalInt(state.appConfig.MaxZipEntryCount))
	maxStoreObjectBytes := widget.NewEntry()
	maxStoreObjectBytes.SetText(formatOptionalInt64(state.appConfig.MaxStoreObjectBytes))
	maxImportTotalFiles := widget.NewEntry()
	maxImportTotalFiles.SetText(formatOptionalInt(state.appConfig.MaxImportTotalFiles))
	maxImportPathLength := widget.NewEntry()
	maxImportPathLength.SetText(formatOptionalInt(state.appConfig.MaxImportPathLength))
	maxImportDirectoryDepth := widget.NewEntry()
	maxImportDirectoryDepth.SetText(formatOptionalInt(state.appConfig.MaxImportDirectoryDepth))

	form := dialog.NewForm("Settings", "Save", "Cancel", []*widget.FormItem{
		widget.NewFormItem("AETitle", localAE),
		widget.NewFormItem("Listener", activateListener),
		widget.NewFormItem("Listener Status", listenerStatus),
		widget.NewFormItem("Receiver Host", receiverHost),
		widget.NewFormItem("Use Address", hostSelect),
		widget.NewFormItem("Receiver Port", receiverPort),
		widget.NewFormItem("Local Addresses", container.NewBorder(nil, nil, nil, copyAddressesButton, listenerAddressSummary)),
		widget.NewFormItem("AE Aliases", additionalAEs),
		widget.NewFormItem("Preferred Syntax", preferredReceiveSyntax),
		widget.NewFormItem(settingsLabelDICOMCommunicationTimeout, dicomCommunicationTimeout),
		widget.NewFormItem(settingsLabelDICOMConnectionTimeout, dicomConnectionTimeout),
		widget.NewFormItem("Max File Import Bytes", maxFileImportBytes),
		widget.NewFormItem("Max ZIP Entry Bytes", maxZipEntryBytes),
		widget.NewFormItem("Max ZIP Total Bytes", maxZipTotalBytes),
		widget.NewFormItem("Max ZIP Entry Count", maxZipEntryCount),
		widget.NewFormItem("Max Store Object Bytes", maxStoreObjectBytes),
		widget.NewFormItem("Max Import Total Files", maxImportTotalFiles),
		widget.NewFormItem("Max Import Path Length", maxImportPathLength),
		widget.NewFormItem("Max Import Directory Depth", maxImportDirectoryDepth),
	}, func(ok bool) {
		if !ok {
			return
		}
		communicationTimeout, err := parsePositiveSeconds(dicomCommunicationTimeout.Text, "DICOM communications timeout")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		connectionTimeout, err := parsePositiveSeconds(dicomConnectionTimeout.Text, "connection timeout")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		fileLimit, err := parseOptionalInt64Limit(maxFileImportBytes.Text, "max file import bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		zipEntryLimit, err := parseOptionalInt64Limit(maxZipEntryBytes.Text, "max ZIP entry bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		zipTotalLimit, err := parseOptionalInt64Limit(maxZipTotalBytes.Text, "max ZIP total bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		zipEntryCount, err := parseOptionalIntLimit(maxZipEntryCount.Text, "max ZIP entry count")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		storeObjectLimit, err := parseOptionalInt64Limit(maxStoreObjectBytes.Text, "max store object bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		importTotalFiles, err := parseOptionalIntLimit(maxImportTotalFiles.Text, "max import total files")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		importPathLength, err := parseOptionalIntLimit(maxImportPathLength.Text, "max import path length")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		importDirectoryDepth, err := parseOptionalIntLimit(maxImportDirectoryDepth.Text, "max import directory depth")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		receiverAddress, err := receiverAddressFromParts(receiverHost.Text, receiverPort.Text)
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		cfg := appconfig.Config{
			LocalAETitle:                     localAE.Text,
			ReceiverAddress:                  receiverAddress,
			ReceiverAutoStart:                activateListener.Checked,
			AdditionalAETitles:               parseAETitleList(additionalAEs.Text),
			ReceivePreferredTransferSyntax:   receivePreferredSyntaxValue(preferredReceiveSyntax.Selected),
			DICOMCommunicationTimeoutSeconds: communicationTimeout,
			DICOMConnectionTimeoutSeconds:    connectionTimeout,
			MaxFileImportBytes:               fileLimit,
			MaxZipEntryBytes:                 zipEntryLimit,
			MaxZipTotalBytes:                 zipTotalLimit,
			MaxZipEntryCount:                 zipEntryCount,
			MaxStoreObjectBytes:              storeObjectLimit,
			MaxImportTotalFiles:              importTotalFiles,
			MaxImportPathLength:              importPathLength,
			MaxImportDirectoryDepth:          importDirectoryDepth,
		}
		if err := appconfig.Save(state.appConfigPath, cfg); err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		normalized, err := appconfig.Load(state.appConfigPath)
		if err != nil {
			status.SetText("Settings reload failed")
			dialog.ShowError(err, w)
			return
		}
		state.appConfig = normalized
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if state.appConfig.ReceiverAutoStart && state.receiver == nil {
			startReceiver(w, status, state)
			return
		}
		if !state.appConfig.ReceiverAutoStart && state.receiver != nil {
			stopReceiver(w, status, tables, state)
			return
		}
		if state.receiver != nil {
			status.SetText("Settings saved; restart receiver to apply listener changes")
			return
		}
		status.SetText("Settings saved")
	}, w)
	form.Resize(fyne.NewSize(720, 600))
	form.Show()
}

func importOptionsFromConfig(cfg appconfig.Config) archive.ImportOptions {
	return archive.ImportOptions{
		Limits: archive.ImportLimits{
			MaxFileImportBytes:      optionalInt64Value(cfg.MaxFileImportBytes),
			MaxZipEntryBytes:        optionalInt64Value(cfg.MaxZipEntryBytes),
			MaxZipTotalBytes:        optionalInt64Value(cfg.MaxZipTotalBytes),
			MaxZipEntryCount:        optionalIntValue(cfg.MaxZipEntryCount),
			MaxImportTotalFiles:     optionalIntValue(cfg.MaxImportTotalFiles),
			MaxImportPathLength:     optionalIntValue(cfg.MaxImportPathLength),
			MaxImportDirectoryDepth: optionalIntValue(cfg.MaxImportDirectoryDepth),
		},
	}
}

func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func optionalIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func receivePreferredSyntaxLabels() []string {
	return []string{
		receivePreferredSyntaxAutoLabel,
		receivePreferredSyntaxExplicitLabel,
		receivePreferredSyntaxImplicitLabel,
	}
}

func receivePreferredSyntaxLabel(value string) string {
	switch strings.TrimSpace(value) {
	case receive.PreferredTransferSyntaxExplicitVRLittleEndian:
		return receivePreferredSyntaxExplicitLabel
	case receive.PreferredTransferSyntaxImplicitVRLittleEndian:
		return receivePreferredSyntaxImplicitLabel
	default:
		return receivePreferredSyntaxAutoLabel
	}
}

func receivePreferredSyntaxValue(label string) string {
	switch strings.TrimSpace(label) {
	case receivePreferredSyntaxExplicitLabel:
		return receive.PreferredTransferSyntaxExplicitVRLittleEndian
	case receivePreferredSyntaxImplicitLabel:
		return receive.PreferredTransferSyntaxImplicitVRLittleEndian
	default:
		return receive.PreferredTransferSyntaxAuto
	}
}

func sendSyntaxOptions() []string {
	return []string{
		sendSyntaxAutoLabel,
		sendSyntaxExplicitLabel,
		sendSyntaxImplicitLabel,
	}
}

func sendSyntaxLabel(value string) string {
	switch strings.TrimSpace(value) {
	case nodes.SendTransferSyntaxExplicitVRLittleEndian:
		return sendSyntaxExplicitLabel
	case nodes.SendTransferSyntaxImplicitVRLittleEndian:
		return sendSyntaxImplicitLabel
	default:
		return sendSyntaxAutoLabel
	}
}

func sendSyntaxValue(label string) string {
	switch strings.TrimSpace(label) {
	case sendSyntaxExplicitLabel:
		return nodes.SendTransferSyntaxExplicitVRLittleEndian
	case sendSyntaxImplicitLabel:
		return nodes.SendTransferSyntaxImplicitVRLittleEndian
	default:
		return nodes.SendTransferSyntaxAuto
	}
}

func studyStatusPresetOptions() []string {
	return []string{
		studyStatusPresetCustomLabel,
		studyStatusPresetReviewedLabel,
		studyStatusPresetInterestingLabel,
		studyStatusPresetFollowUpLabel,
		studyStatusPresetTeachingLabel,
		studyStatusPresetProblemLabel,
	}
}

func studyStatusPresetValue(label string) string {
	switch strings.TrimSpace(label) {
	case studyStatusPresetReviewedLabel:
		return studyStatusPresetReviewedLabel
	case studyStatusPresetInterestingLabel:
		return studyStatusPresetInterestingLabel
	case studyStatusPresetFollowUpLabel:
		return studyStatusPresetFollowUpLabel
	case studyStatusPresetTeachingLabel:
		return studyStatusPresetTeachingLabel
	case studyStatusPresetProblemLabel:
		return studyStatusPresetProblemLabel
	default:
		return ""
	}
}

func studyStatusPresetLabel(status string) string {
	status = strings.TrimSpace(status)
	for _, option := range studyStatusPresetOptions() {
		if option != studyStatusPresetCustomLabel && strings.EqualFold(status, option) {
			return option
		}
	}
	return studyStatusPresetCustomLabel
}

func timeoutSecondsOrDefault(value int, defaultValue int) int {
	if value > 0 {
		return value
	}
	return defaultValue
}

func dicomCommunicationTimeoutDuration(cfg appconfig.Config) time.Duration {
	seconds := timeoutSecondsOrDefault(cfg.DICOMCommunicationTimeoutSeconds, appconfig.DefaultDICOMCommunicationTimeoutSeconds)
	return time.Duration(seconds) * time.Second
}

func dicomConnectionTimeoutDuration(cfg appconfig.Config) time.Duration {
	seconds := timeoutSecondsOrDefault(cfg.DICOMConnectionTimeoutSeconds, appconfig.DefaultDICOMConnectionTimeoutSeconds)
	return time.Duration(seconds) * time.Second
}

func withDICOMCommunicationTimeout(ctx context.Context, state *uiState) (context.Context, context.CancelFunc) {
	cfg := appconfig.Config{}
	if state != nil {
		cfg = state.appConfig
	}
	return context.WithTimeout(ctx, dicomCommunicationTimeoutDuration(cfg))
}

func withDICOMConnectionTimeout(ctx context.Context, state *uiState) (context.Context, context.CancelFunc) {
	cfg := appconfig.Config{}
	if state != nil {
		cfg = state.appConfig
	}
	return context.WithTimeout(ctx, dicomConnectionTimeoutDuration(cfg))
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func parseOptionalInt64Limit(value string, name string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "unlimited") {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer or blank", name)
	}
	return &parsed, nil
}

func parseOptionalIntLimit(value string, name string) (*int, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "unlimited") {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer or blank", name)
	}
	return &parsed, nil
}

func parsePositiveSeconds(value string, name string) (int, error) {
	value = strings.TrimSpace(value)
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of seconds", name)
	}
	return parsed, nil
}

func parseAETitleList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	var aeTitles []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			aeTitles = append(aeTitles, field)
		}
	}
	return aeTitles
}

func nodeDraftFromFormState(name string, aeTitle string, host string, port uint16, enabled bool, queryEnabled bool, retrieveMethod string, sendEnabled bool, sendTransferSyntax string, moveDestination string, notes string) nodes.Draft {
	return nodes.Draft{
		Name:                     name,
		AETitle:                  aeTitle,
		Host:                     host,
		Port:                     port,
		Disabled:                 !enabled,
		QueryDisabled:            !queryEnabled,
		SendDisabled:             !sendEnabled,
		RetrieveMethod:           retrieveMethod,
		SendTransferSyntax:       sendTransferSyntax,
		PreferredMoveDestination: moveDestination,
		Notes:                    notes,
	}
}

func retrieveMethodOptions() []string {
	return []string{nodes.RetrieveMethodAuto, nodes.RetrieveMethodMove, nodes.RetrieveMethodGet}
}

func networkNodeActionLabels() []string {
	return []string{
		networkActionLabelAll,
		networkActionLabelNone,
		networkActionLabelSave,
		networkActionLabelLoad,
		networkActionLabelVerify,
		networkActionLabelAddNewNode,
		networkActionLabelEdit,
		networkActionLabelDelete,
	}
}

func networkDeleteShortcutHint() string {
	return "Press Delete key to remove a node"
}

func networkDeleteShortcutApplies(tabTitle string) bool {
	return tabTitle == networkTabTitle
}

func registerNetworkDeleteShortcut(w fyne.Window, tabs *container.AppTabs, status *widget.Label, nodeTable *widget.Table, state *uiState) {
	if w == nil || tabs == nil {
		return
	}
	deleteShortcut := &desktop.CustomShortcut{KeyName: fyne.KeyDelete}
	w.Canvas().AddShortcut(deleteShortcut, func(fyne.Shortcut) {
		selected := tabs.Selected()
		if selected == nil || !networkDeleteShortcutApplies(selected.Text) {
			return
		}
		deleteSelectedNode(w, status, nodeTable, state)
	})
}

func nodesJSONData(nodeList []nodes.Node) ([]byte, error) {
	data, err := json.MarshalIndent(nodeList, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode nodes export: %w", err)
	}
	return append(data, '\n'), nil
}

func exportNodesToPath(state *uiState, path string) error {
	if state == nil {
		return errors.New("nodes export state is unavailable")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("nodes export path cannot be empty")
	}
	data, err := nodesJSONData(state.nodes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create nodes export directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write nodes export: %w", err)
	}
	return nil
}

func importNodesFromPath(state *uiState, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("nodes import path cannot be empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read nodes import: %w", err)
	}
	return importNodesFromJSON(state, data)
}

func importNodesFromJSON(state *uiState, data []byte) error {
	if state == nil {
		return errors.New("nodes import state is unavailable")
	}
	var imported []nodes.Node
	if err := json.Unmarshal(data, &imported); err != nil {
		return fmt.Errorf("parse nodes import: %w", err)
	}
	originalNodes := append([]nodes.Node(nil), state.nodes...)
	originalRows := append([]int(nil), state.nodeTableRows...)
	originalSelection := state.selectedNodeRow
	state.nodes = append([]nodes.Node(nil), imported...)
	state.selectedNodeRow = -1
	refreshNodeTableRows(state)
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes = originalNodes
			state.nodeTableRows = originalRows
			state.selectedNodeRow = originalSelection
			return fmt.Errorf("persist nodes import: %w", err)
		}
	}
	return nil
}

func showSaveNodesDialog(w fyne.Window, status *widget.Label, state *uiState) {
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			if status != nil {
				status.SetText("Node export failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		data, err := nodesJSONData(state.nodes)
		if err != nil {
			if status != nil {
				status.SetText("Node export failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if _, err := writer.Write(data); err != nil {
			if status != nil {
				status.SetText("Node export failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if status != nil {
			status.SetText(fmt.Sprintf("Exported %d nodes to %s", len(state.nodes), writer.URI().Name()))
		}
	}, w)
	picker.SetFileName("go-pacs-nodes.json")
	picker.Show()
}

func showLoadNodesDialog(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			if status != nil {
				status.SetText("Node import failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			if status != nil {
				status.SetText("Node import failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if err := importNodesFromJSON(state, data); err != nil {
			if status != nil {
				status.SetText("Node import failed")
			}
			dialog.ShowError(err, w)
			return
		}
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if table != nil {
			table.Refresh()
		}
		if status != nil {
			status.SetText(fmt.Sprintf("Imported %d nodes from %s", len(state.nodes), reader.URI().Name()))
		}
	}, w)
	picker.Show()
}

func newNetworkTab(w fyne.Window, status *widget.Label, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("DICOM Nodes for DICOM Query/Retrieve and DICOM Send", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	hint := widget.NewLabel(networkDeleteShortcutHint())
	hint.Alignment = fyne.TextAlignTrailing
	header := container.NewBorder(nil, nil, title, hint)

	refreshAfterBulk := func(changed bool, err error, action string) {
		if err != nil {
			if status != nil {
				status.SetText("Node update failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if changed && status != nil {
			status.SetText(action)
		}
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if nodeTable != nil {
			nodeTable.Refresh()
		}
	}
	allButton := widget.NewButton(networkActionLabelAll, func() {
		changed, err := setAllNodesEnabled(state, true)
		refreshAfterBulk(changed, err, "Enabled all nodes")
	})
	noneButton := widget.NewButton(networkActionLabelNone, func() {
		changed, err := setAllNodesEnabled(state, false)
		refreshAfterBulk(changed, err, "Disabled all nodes")
	})
	saveButton := widget.NewButtonWithIcon(networkActionLabelSave, theme.DocumentSaveIcon(), func() {
		showSaveNodesDialog(w, status, state)
	})
	loadButton := widget.NewButtonWithIcon(networkActionLabelLoad, theme.FolderOpenIcon(), func() {
		showLoadNodesDialog(w, status, nodeTable, state)
	})
	verifyButton := widget.NewButtonWithIcon(networkActionLabelVerify, theme.ConfirmIcon(), func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	addButton := widget.NewButtonWithIcon(networkActionLabelAddNewNode, theme.ContentAddIcon(), func() {
		showAddNodeDialog(w, status, nodeTable, state)
	})
	editButton := widget.NewButtonWithIcon(networkActionLabelEdit, theme.DocumentCreateIcon(), func() {
		showEditNodeDialog(w, status, nodeTable, state)
	})
	deleteButton := widget.NewButtonWithIcon(networkActionLabelDelete, theme.DeleteIcon(), func() {
		deleteSelectedNode(w, status, nodeTable, state)
	})
	footer := container.NewBorder(
		nil,
		nil,
		container.NewHBox(allButton, noneButton),
		container.NewHBox(editButton, deleteButton, addButton),
		container.NewCenter(container.NewHBox(saveButton, loadButton, verifyButton)),
	)
	return container.NewBorder(header, footer, nil, nil, container.NewStack(nodeTable))
}

func showAddNodeDialog(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	enabled := widget.NewCheck("", nil)
	enabled.SetChecked(true)
	queryEnabled := widget.NewCheck("", nil)
	queryEnabled.SetChecked(true)
	retrieveMethod := widget.NewSelect(retrieveMethodOptions(), nil)
	retrieveMethod.SetSelected(nodes.RetrieveMethodAuto)
	sendEnabled := widget.NewCheck("", nil)
	sendEnabled.SetChecked(true)
	sendSyntax := widget.NewSelect(sendSyntaxOptions(), nil)
	sendSyntax.SetSelected(sendSyntaxAutoLabel)
	name := widget.NewEntry()
	name.SetPlaceHolder("pacs")
	aeTitle := widget.NewEntry()
	aeTitle.SetPlaceHolder("REMOTEAE")
	host := widget.NewEntry()
	host.SetPlaceHolder("127.0.0.1")
	port := widget.NewEntry()
	port.SetPlaceHolder("104")
	moveDestination := widget.NewEntry()
	moveDestination.SetPlaceHolder(localAETitle(state))
	notes := widget.NewMultiLineEntry()
	notes.SetPlaceHolder("Optional notes")

	form := dialog.NewForm("Add Remote Node", "Add", "Cancel", []*widget.FormItem{
		widget.NewFormItem("Enabled", enabled),
		widget.NewFormItem("Query", queryEnabled),
		widget.NewFormItem("Retrieve", retrieveMethod),
		widget.NewFormItem("Send", sendEnabled),
		widget.NewFormItem("Send Syntax", sendSyntax),
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Called AE", aeTitle),
		widget.NewFormItem("Host", host),
		widget.NewFormItem("Port", port),
		widget.NewFormItem("Move Destination", moveDestination),
		widget.NewFormItem("Notes", notes),
	}, func(ok bool) {
		if !ok {
			return
		}
		portValue, err := parsePort(port.Text)
		if err != nil {
			status.SetText("Add node failed")
			dialog.ShowError(err, w)
			return
		}
		node, err := state.nodeStore.Add(nodeDraftFromFormState(name.Text, aeTitle.Text, host.Text, portValue, enabled.Checked, queryEnabled.Checked, retrieveMethod.Selected, sendEnabled.Checked, sendSyntaxValue(sendSyntax.Selected), moveDestination.Text, notes.Text))
		if err != nil {
			status.SetText("Add node failed")
			dialog.ShowError(err, w)
			return
		}
		state.nodes = append(state.nodes, node)
		refreshNodeTableRows(state)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Added node " + node.Name)
	}, w)
	form.Resize(fyne.NewSize(560, 460))
	form.Show()
}

func showEditNodeDialog(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	node, ok := selectedNode(state)
	if !ok {
		status.SetText("Select a remote node to edit")
		return
	}
	row := state.selectedNodeRow
	enabled := widget.NewCheck("", nil)
	enabled.SetChecked(node.Enabled())
	queryEnabled := widget.NewCheck("", nil)
	queryEnabled.SetChecked(node.QueryEnabled())
	retrieveMethod := widget.NewSelect(retrieveMethodOptions(), nil)
	retrieveMethod.SetSelected(node.RetrieveMethodOrDefault())
	sendEnabled := widget.NewCheck("", nil)
	sendEnabled.SetChecked(node.SendEnabled())
	sendSyntax := widget.NewSelect(sendSyntaxOptions(), nil)
	sendSyntax.SetSelected(sendSyntaxLabel(node.SendTransferSyntaxOrDefault()))
	name := widget.NewEntry()
	name.SetText(node.Name)
	aeTitle := widget.NewEntry()
	aeTitle.SetText(node.AETitle)
	host := widget.NewEntry()
	host.SetText(node.Host)
	port := widget.NewEntry()
	port.SetText(strconv.Itoa(int(node.Port)))
	moveDestination := widget.NewEntry()
	moveDestination.SetText(node.PreferredMoveDestination)
	notes := widget.NewMultiLineEntry()
	notes.SetText(node.Notes)

	form := dialog.NewForm("Edit Remote Node", "Save", "Cancel", []*widget.FormItem{
		widget.NewFormItem("Enabled", enabled),
		widget.NewFormItem("Query", queryEnabled),
		widget.NewFormItem("Retrieve", retrieveMethod),
		widget.NewFormItem("Send", sendEnabled),
		widget.NewFormItem("Send Syntax", sendSyntax),
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Called AE", aeTitle),
		widget.NewFormItem("Host", host),
		widget.NewFormItem("Port", port),
		widget.NewFormItem("Move Destination", moveDestination),
		widget.NewFormItem("Notes", notes),
	}, func(ok bool) {
		if !ok {
			return
		}
		portValue, err := parsePort(port.Text)
		if err != nil {
			status.SetText("Edit node failed")
			dialog.ShowError(err, w)
			return
		}
		updated, err := state.nodeStore.Update(node.ID, nodeDraftFromFormState(name.Text, aeTitle.Text, host.Text, portValue, enabled.Checked, queryEnabled.Checked, retrieveMethod.Selected, sendEnabled.Checked, sendSyntaxValue(sendSyntax.Selected), moveDestination.Text, notes.Text))
		if err != nil {
			status.SetText("Edit node failed")
			dialog.ShowError(err, w)
			return
		}
		if row >= 0 && row < len(state.nodes) && state.nodes[row].ID == updated.ID {
			state.nodes[row] = updated
		} else {
			nodeList, err := state.nodeStore.List()
			if err != nil {
				status.SetText("Edit node saved, refresh failed")
				dialog.ShowError(err, w)
				return
			}
			state.nodes = nodeList
			state.selectedNodeRow = -1
		}
		refreshNodeTableRows(state)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Edited node " + updated.Name)
	}, w)
	form.Resize(fyne.NewSize(560, 460))
	form.Show()
}

func deleteSelectedNode(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	node, ok := selectedNode(state)
	if !ok {
		status.SetText("Select a remote node to delete")
		return
	}
	dialog.ShowConfirm("Delete Remote Node", fmt.Sprintf("Delete %s?", node.Name), func(ok bool) {
		if !ok {
			return
		}
		if err := state.nodeStore.Delete(node.ID); err != nil {
			status.SetText("Delete node failed")
			dialog.ShowError(err, w)
			return
		}
		row := state.selectedNodeRow
		if row >= 0 && row < len(state.nodes) && state.nodes[row].ID == node.ID {
			state.nodes = append(state.nodes[:row], state.nodes[row+1:]...)
		} else {
			nodeList, err := state.nodeStore.List()
			if err != nil {
				status.SetText("Delete node saved, refresh failed")
				dialog.ShowError(err, w)
				return
			}
			state.nodes = nodeList
		}
		state.selectedNodeRow = -1
		refreshNodeTableRows(state)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Deleted node " + node.Name)
	}, w)
}

func parsePort(value string) (uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("port is required")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	if port == 0 || port > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return uint16(port), nil
}

func verifySelectedNode(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	node, ok := selectedEnabledNode(state)
	if !ok {
		status.SetText("No enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText("Verifying " + node.Name)
	go func() {
		ctx, cancel := withDICOMConnectionTimeout(context.Background(), state)
		defer cancel()
		result, err := netverify.Echo(ctx, node, callingAE)
		fyne.Do(func() {
			if err != nil {
				recordNodeVerifyStatus(state, node, nodeVerifyFail)
				status.SetText("C-ECHO failed for " + node.Name)
				refreshQuerySourceList(state)
				dialog.ShowError(err, w)
				return
			}
			recordNodeVerifyStatus(state, node, nodeVerifyOK)
			status.SetText(fmt.Sprintf("C-ECHO %s status=0x%04X in %s", result.NodeName, result.Status, result.Duration.Round(time.Millisecond)))
			refreshQuerySourceList(state)
			if table != nil {
				table.Refresh()
			}
		})
	}()
}

func startReceiver(w fyne.Window, status *widget.Label, state *uiState) {
	if state.receiver != nil {
		snapshot := state.receiver.Snapshot()
		status.SetText(fmt.Sprintf("Receiver already listening on %s as %s", snapshot.Address, snapshot.AETitle))
		return
	}
	allowedCallingAEs := configuredNodeAETitles(state.nodes)
	allowedRemoteHosts := configuredNodeIPHosts(state.nodes)
	server, err := receive.Start(context.Background(), receive.Config{
		Catalog:                 state.catalog,
		Address:                 state.appConfig.ReceiverAddress,
		AETitle:                 localAETitle(state),
		AllowedCalledAETitles:   state.appConfig.AdditionalAETitles,
		AllowedCallingAETitles:  allowedCallingAEs,
		AllowedRemoteHosts:      allowedRemoteHosts,
		MaxStoreObjectBytes:     optionalInt64Value(state.appConfig.MaxStoreObjectBytes),
		PreferredTransferSyntax: state.appConfig.ReceivePreferredTransferSyntax,
	})
	if err != nil {
		status.SetText("Receiver start failed")
		dialog.ShowError(err, w)
		return
	}
	state.receiver = server
	state.receiverStartedAt = time.Now()
	refreshArchiveChrome(state)
	refreshQueryDestination(state)
	refreshQueryResultSummary(state)
	refreshQuerySourceList(state)
	if len(allowedCallingAEs) > 0 {
		status.SetText(fmt.Sprintf("Receiver listening on %s as %s; allowing %d remote AEs and %d remote IPs", server.Addr(), server.AETitle(), len(allowedCallingAEs), len(allowedRemoteHosts)))
		return
	}
	status.SetText(fmt.Sprintf("Receiver listening on %s as %s; no Calling AE allowlist", server.Addr(), server.AETitle()))
}

func configuredNodeAETitles(nodeList []nodes.Node) []string {
	seen := map[string]bool{}
	var aeTitles []string
	for _, node := range nodeList {
		aeTitle := nodes.NormalizeAETitle(node.AETitle)
		if aeTitle == "" || seen[aeTitle] {
			continue
		}
		seen[aeTitle] = true
		aeTitles = append(aeTitles, aeTitle)
	}
	return aeTitles
}

func configuredNodeIPHosts(nodeList []nodes.Node) []string {
	seen := map[string]bool{}
	var hosts []string
	for _, node := range nodeList {
		host := strings.TrimSpace(node.Host)
		ip := net.ParseIP(host)
		if ip == nil {
			continue
		}
		host = ip.String()
		if seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts
}

func stopReceiver(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	if state.receiver == nil {
		status.SetText("Receiver is not running")
		return
	}
	server := state.receiver
	status.SetText("Stopping receiver")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := server.Stop(ctx)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			if err != nil {
				status.SetText("Receiver stop failed")
				dialog.ShowError(err, w)
				return
			}
			state.receiver = nil
			refreshQueryDestination(state)
			refreshQueryResultSummary(state)
			refreshQuerySourceList(state)
			snapshot := server.Snapshot()
			duration := time.Duration(0)
			if !state.receiverStartedAt.IsZero() {
				duration = time.Since(state.receiverStartedAt)
			}
			state.receiverStartedAt = time.Time{}
			recordOperation(state, ops.ReceiverSummary(snapshot, duration))
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			status.SetText(fmt.Sprintf("Receiver stopped: stored %d, duplicates %d, rejected %d, failed %d", snapshot.Stored, snapshot.Duplicates, snapshot.Rejected, snapshot.Failed))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func sendSelectedStudy(w fyne.Window, status *widget.Label, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		status.SetText("Select a study to send")
		return
	}
	if strings.TrimSpace(study.StudyInstanceUID) == "" || study.StudyInstanceUID == "(missing)" {
		status.SetText("Selected study has no Study Instance UID")
		return
	}
	node, ok := selectedSendNode(state)
	if !ok {
		status.SetText("No send-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText(fmt.Sprintf("Sending study %s to %s", study.StudyInstanceUID, node.Name))
	beginSendActivity(state, "Study C-STORE "+node.Name)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		outcome, err := send.SendStudy(ctx, state.catalog, node, study.StudyInstanceUID, callingAE)
		fyne.Do(func() {
			clearActiveSendActivity(state)
			if err != nil {
				status.SetText("C-STORE failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.SendSummary(outcome))
			status.SetText(fmt.Sprintf(
				"C-STORE %s: attempted %d, sent %d, warnings %d, failed %d in %s",
				node.Name,
				outcome.Attempted,
				outcome.Sent,
				outcome.Warnings,
				outcome.Failed,
				outcome.Duration.Round(time.Millisecond),
			))
			if outcome.Failed > 0 {
				dialog.ShowInformation("Send completed with failures", strings.Join(outcome.Failures, "\n"), w)
			}
		})
	}()
}

func sendSelectedSeries(w fyne.Window, status *widget.Label, state *uiState) {
	series, ok := selectedSeries(state)
	if !ok {
		status.SetText("Select a series to send")
		return
	}
	if strings.TrimSpace(series.SeriesInstanceUID) == "" || series.SeriesInstanceUID == "(missing)" {
		status.SetText("Selected series has no Series Instance UID")
		return
	}
	node, ok := selectedSendNode(state)
	if !ok {
		status.SetText("No send-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText(fmt.Sprintf("Sending series %s to %s", series.SeriesInstanceUID, node.Name))
	beginSendActivity(state, "Series C-STORE "+node.Name)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		outcome, err := send.SendSeries(ctx, state.catalog, node, series.SeriesInstanceUID, callingAE)
		fyne.Do(func() {
			clearActiveSendActivity(state)
			if err != nil {
				status.SetText("C-STORE failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.SendSummary(outcome))
			status.SetText(fmt.Sprintf(
				"C-STORE %s: attempted %d, sent %d, warnings %d, failed %d in %s",
				node.Name,
				outcome.Attempted,
				outcome.Sent,
				outcome.Warnings,
				outcome.Failed,
				outcome.Duration.Round(time.Millisecond),
			))
			if outcome.Failed > 0 {
				dialog.ShowInformation("Send completed with failures", strings.Join(outcome.Failures, "\n"), w)
			}
		})
	}()
}

func sendSelectedInstance(w fyne.Window, status *widget.Label, state *uiState) {
	instance, ok := selectedInstance(state)
	if !ok {
		status.SetText("Select an image to send")
		return
	}
	if strings.TrimSpace(instance.SOPInstanceUID) == "" || instance.SOPInstanceUID == "(missing)" {
		status.SetText("Selected image has no SOP Instance UID")
		return
	}
	node, ok := selectedSendNode(state)
	if !ok {
		status.SetText("No send-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText(fmt.Sprintf("Sending image %s to %s", instance.SOPInstanceUID, node.Name))
	beginSendActivity(state, "Image C-STORE "+node.Name)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		outcome, err := send.SendInstance(ctx, state.catalog, node, instance.SOPInstanceUID, callingAE)
		fyne.Do(func() {
			clearActiveSendActivity(state)
			if err != nil {
				status.SetText("C-STORE failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.SendSummary(outcome))
			status.SetText(fmt.Sprintf(
				"C-STORE %s: attempted %d, sent %d, warnings %d, failed %d in %s",
				node.Name,
				outcome.Attempted,
				outcome.Sent,
				outcome.Warnings,
				outcome.Failed,
				outcome.Duration.Round(time.Millisecond),
			))
			if outcome.Failed > 0 {
				dialog.ShowInformation("Send completed with failures", strings.Join(outcome.Failures, "\n"), w)
			}
		})
	}()
}

func retrieveSelectedSeries(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	series, ok := selectedSeries(state)
	if !ok {
		status.SetText("Select a series to retrieve")
		return
	}
	if strings.TrimSpace(series.StudyInstanceUID) == "" || series.StudyInstanceUID == "(missing)" {
		status.SetText("Selected series has no Study Instance UID")
		return
	}
	if strings.TrimSpace(series.SeriesInstanceUID) == "" || series.SeriesInstanceUID == "(missing)" {
		status.SetText("Selected series has no Series Instance UID")
		return
	}
	node, ok := selectedQueryNode(state)
	if !ok {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		status.SetText(issue)
		return
	}
	opts := retrieveOptionsForNode(status, state, node)
	baseCtx, cancel := beginRetrieve(state, node.Name)
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	status.SetText(fmt.Sprintf("Retrieving series %s from %s", series.SeriesInstanceUID, node.Name))
	go func() {
		defer timeoutCancel()
		defer cancel()
		outcome, err := retrieve.RetrieveSeries(ctx, state.catalog, node, series.StudyInstanceUID, series.SeriesInstanceUID, opts)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					status.SetText("Retrieve cancelled for " + node.Name)
					return
				}
				status.SetText("Retrieve failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			recordOperation(state, ops.RetrieveSummary(outcome))
			status.SetText(fmt.Sprintf(
				"%s %s: final=0x%04X completed %d failed %d warnings %d stored %d in %s",
				retrieveMethodName(outcome),
				node.Name,
				outcome.FinalStatus,
				outcome.Completed,
				outcome.Failed,
				outcome.Warnings,
				outcome.Stored,
				outcome.Duration.Round(time.Millisecond),
			))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func retrieveSelectedInstance(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	instance, ok := selectedInstance(state)
	if !ok {
		status.SetText("Select an image to retrieve")
		return
	}
	if strings.TrimSpace(instance.StudyInstanceUID) == "" || instance.StudyInstanceUID == "(missing)" {
		status.SetText("Selected image has no Study Instance UID")
		return
	}
	if strings.TrimSpace(instance.SeriesInstanceUID) == "" || instance.SeriesInstanceUID == "(missing)" {
		status.SetText("Selected image has no Series Instance UID")
		return
	}
	if strings.TrimSpace(instance.SOPInstanceUID) == "" || instance.SOPInstanceUID == "(missing)" {
		status.SetText("Selected image has no SOP Instance UID")
		return
	}
	node, ok := selectedQueryNode(state)
	if !ok {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		status.SetText(issue)
		return
	}
	opts := retrieveOptionsForNode(status, state, node)
	baseCtx, cancel := beginRetrieve(state, node.Name)
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	status.SetText(fmt.Sprintf("Retrieving image %s from %s", instance.SOPInstanceUID, node.Name))
	go func() {
		defer timeoutCancel()
		defer cancel()
		outcome, err := retrieve.RetrieveImage(ctx, state.catalog, node, instance.StudyInstanceUID, instance.SeriesInstanceUID, instance.SOPInstanceUID, opts)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					status.SetText("Retrieve cancelled for " + node.Name)
					return
				}
				status.SetText("Retrieve failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			recordOperation(state, ops.RetrieveSummary(outcome))
			status.SetText(fmt.Sprintf(
				"%s %s: final=0x%04X completed %d failed %d warnings %d stored %d in %s",
				retrieveMethodName(outcome),
				node.Name,
				outcome.FinalStatus,
				outcome.Completed,
				outcome.Failed,
				outcome.Warnings,
				outcome.Stored,
				outcome.Duration.Round(time.Millisecond),
			))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

type archiveRowKind int

const (
	archiveRowPatient archiveRowKind = iota
	archiveRowStudy
	archiveRowSeries
)

type archiveBrowserRow struct {
	kind              archiveRowKind
	studyIndex        int
	seriesIndex       int
	groupKey          string
	collapsed         bool
	studyHasSeries    bool
	studySeriesLoaded bool
	series            archive.Series
	patientName       string
	patientID         string
	patientBirthDate  string
	institutionName   string
	modalities        string
	seriesCount       int
	instanceCount     int
}

type archivePatientGroup struct {
	row          archiveBrowserRow
	studyIndexes []int
}

func archiveBrowserRows(studies []archive.Study) []archiveBrowserRow {
	return archiveBrowserRowsWithCollapse(studies, nil)
}

func archiveBrowserRowsWithCollapse(studies []archive.Study, collapsed map[string]bool) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeries(studies, collapsed, nil)
}

func archiveBrowserRowsWithInlineSeries(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(studies, collapsed, seriesByStudy, nil)
}

func archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series, collapsedStudies map[string]bool) []archiveBrowserRow {
	groupIndex := map[string]int{}
	var groups []archivePatientGroup
	for index, study := range studies {
		key := archivePatientKey(study)
		groupIndexValue, ok := groupIndex[key]
		if !ok {
			groupIndexValue = len(groups)
			groupIndex[key] = groupIndexValue
			groups = append(groups, archivePatientGroup{row: archiveBrowserRow{
				kind:        archiveRowPatient,
				studyIndex:  -1,
				groupKey:    key,
				collapsed:   collapsed[key],
				patientName: emptyDash(study.PatientName),
				patientID:   emptyDash(study.PatientID),
			}})
		}
		group := &groups[groupIndexValue]
		if group.row.patientBirthDate == "" {
			group.row.patientBirthDate = study.PatientBirthDate
		}
		if group.row.institutionName == "" {
			group.row.institutionName = study.InstitutionName
		}
		group.row.seriesCount += study.SeriesCount
		group.row.instanceCount += study.InstanceCount
		group.row.modalities = mergeModalities(group.row.modalities, study.Modalities)
		group.studyIndexes = append(group.studyIndexes, index)
	}

	var rows []archiveBrowserRow
	for _, group := range groups {
		rows = append(rows, group.row)
		if group.row.collapsed {
			continue
		}
		for _, studyIndex := range group.studyIndexes {
			if studyIndex < 0 || studyIndex >= len(studies) {
				rows = append(rows, archiveBrowserRow{kind: archiveRowStudy, studyIndex: studyIndex})
				continue
			}
			studyUID := studies[studyIndex].StudyInstanceUID
			seriesRows := seriesByStudy[studyUID]
			studySeriesCollapsed := collapsedStudies[studyUID]
			rows = append(rows, archiveBrowserRow{
				kind:              archiveRowStudy,
				studyIndex:        studyIndex,
				studyHasSeries:    studies[studyIndex].SeriesCount > 0 || len(seriesRows) > 0,
				studySeriesLoaded: len(seriesRows) > 0 && !studySeriesCollapsed,
			})
			if studySeriesCollapsed {
				continue
			}
			for seriesIndex, series := range seriesRows {
				rows = append(rows, archiveBrowserRow{
					kind:        archiveRowSeries,
					studyIndex:  studyIndex,
					seriesIndex: seriesIndex,
					series:      series,
				})
			}
		}
	}
	return rows
}

func archivePatientKey(study archive.Study) string {
	patientID := strings.TrimSpace(study.PatientID)
	patientName := strings.TrimSpace(study.PatientName)
	if patientID != "" {
		return "id:" + strings.ToUpper(patientID)
	}
	if patientName != "" {
		return "name:" + strings.ToUpper(patientName)
	}
	return "missing:" + strings.TrimSpace(study.StudyInstanceUID)
}

func mergeModalities(existing string, next string) string {
	seen := map[string]bool{}
	var values []string
	for _, source := range []string{existing, next} {
		for _, part := range strings.FieldsFunc(source, func(r rune) bool {
			return r == ',' || r == '\\' || r == '/' || r == ';' || r == ' '
		}) {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			values = append(values, part)
		}
	}
	return strings.Join(values, "\\")
}

func archiveBrowserCell(row archiveBrowserRow, studies []archive.Study, col int) string {
	if row.kind == archiveRowSeries {
		switch col {
		case archiveStudyTableColumnPatient:
			return "      " + archiveInlineSeriesLabel(row.series)
		case archiveStudyTableColumnModality:
			return row.series.Modality
		case archiveStudyTableColumnDescription:
			return row.series.SeriesDescription
		case archiveStudyTableColumnStudyDate:
			return archiveDateTimeCell(row.series.SeriesDate, row.series.SeriesTime)
		case archiveStudyTableColumnTime:
			return dicomTimeCell(row.series.SeriesTime)
		case archiveStudyTableColumnSeries:
			return row.series.SeriesNumber
		case archiveStudyTableColumnInstances:
			return fmt.Sprintf("%d", row.series.InstanceCount)
		case archiveStudyTableColumnStudyUID:
			return row.series.SeriesInstanceUID
		default:
			return ""
		}
	}

	if row.kind == archiveRowStudy {
		if row.studyIndex < 0 || row.studyIndex >= len(studies) {
			return ""
		}
		study := studies[row.studyIndex]
		if col == archiveStudyTableColumnPatient {
			label := strings.TrimSpace(study.StudyDescription)
			if label == "" {
				label = strings.TrimSpace(study.StudyDate)
			}
			if label == "" {
				label = strings.TrimSpace(study.StudyInstanceUID)
			}
			prefix := "   "
			if row.studyHasSeries {
				prefix = "  ▸ "
				if row.studySeriesLoaded {
					prefix = "  ▾ "
				}
			}
			return prefix + emptyDash(label)
		}
		return studyCell(study, col)
	}

	switch col {
	case archiveStudyTableColumnPatient:
		if row.collapsed {
			return "▸ " + emptyDash(row.patientName)
		}
		return "▾ " + emptyDash(row.patientName)
	case archiveStudyTableColumnPatientID:
		return emptyDash(row.patientID)
	case archiveStudyTableColumnDOB:
		return emptyDash(row.patientBirthDate)
	case archiveStudyTableColumnModality:
		return row.modalities
	case archiveStudyTableColumnInstitution:
		return emptyDash(row.institutionName)
	case archiveStudyTableColumnSeries:
		return fmt.Sprintf("%d", row.seriesCount)
	case archiveStudyTableColumnInstances:
		return fmt.Sprintf("%d", row.instanceCount)
	default:
		return ""
	}
}

func archiveInlineSeriesLabel(series archive.Series) string {
	if description := strings.TrimSpace(series.SeriesDescription); description != "" {
		return description
	}
	if number := strings.TrimSpace(series.SeriesNumber); number != "" {
		return "Series " + number
	}
	if modality := strings.TrimSpace(series.Modality); modality != "" {
		return modality + " series"
	}
	return "Series"
}

func toggleArchivePatientGroup(state *uiState, row archiveBrowserRow) bool {
	if state == nil || row.kind != archiveRowPatient || strings.TrimSpace(row.groupKey) == "" {
		return false
	}
	if state.collapsedPatientGroups == nil {
		state.collapsedPatientGroups = map[string]bool{}
	}
	state.collapsedPatientGroups[row.groupKey] = !row.collapsed
	state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(state.studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
	state.selectedStudyRow = -1
	return true
}

func toggleArchiveStudySeries(state *uiState, row archiveBrowserRow) bool {
	if state == nil || row.kind != archiveRowStudy || row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
		return false
	}
	studyUID := strings.TrimSpace(state.studies[row.studyIndex].StudyInstanceUID)
	if studyUID == "" {
		return false
	}
	seriesRows, ok := state.archiveSeriesByStudy[studyUID]
	if !ok || len(seriesRows) == 0 {
		return false
	}
	if state.collapsedArchiveStudies == nil {
		state.collapsedArchiveStudies = map[string]bool{}
	}
	state.collapsedArchiveStudies[studyUID] = !state.collapsedArchiveStudies[studyUID]
	state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(state.studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
	return true
}

func selectedStudy(state *uiState) (archive.Study, bool) {
	if state == nil || len(state.studies) == 0 {
		return archive.Study{}, false
	}
	row := state.selectedStudyRow
	if row < 0 || row >= len(state.studies) {
		return archive.Study{}, false
	}
	return state.studies[row], true
}

func selectedSeries(state *uiState) (archive.Series, bool) {
	if len(state.series) == 0 {
		return archive.Series{}, false
	}
	row := state.selectedSeriesRow
	if row < 0 || row >= len(state.series) {
		return archive.Series{}, false
	}
	return state.series[row], true
}

func selectedInstance(state *uiState) (archive.Instance, bool) {
	if len(state.instances) == 0 {
		return archive.Instance{}, false
	}
	row := state.selectedInstanceRow
	if row < 0 || row >= len(state.instances) {
		return archive.Instance{}, false
	}
	return state.instances[row], true
}

func selectedNode(state *uiState) (nodes.Node, bool) {
	if state == nil || len(state.nodes) == 0 {
		return nodes.Node{}, false
	}
	row := state.selectedNodeRow
	if row < 0 || row >= len(state.nodes) {
		row = 0
	}
	return state.nodes[row], true
}

func selectedEnabledNode(state *uiState) (nodes.Node, bool) {
	return selectedMatchingNode(state, func(node nodes.Node) bool {
		return node.Enabled()
	})
}

func selectedQueryNode(state *uiState) (nodes.Node, bool) {
	return selectedMatchingNode(state, func(node nodes.Node) bool {
		return node.Enabled() && node.QueryEnabled()
	})
}

func querySourceNodes(state *uiState) []nodes.Node {
	if state == nil {
		return nil
	}
	var out []nodes.Node
	for _, node := range state.nodes {
		if node.Enabled() && node.QueryEnabled() {
			out = append(out, node)
		}
	}
	return out
}

func annotateQueryMatches(matches []query.Match, node nodes.Node) []query.Match {
	out := make([]query.Match, len(matches))
	for i, match := range matches {
		match.SourceNodeID = node.ID
		match.SourceNodeName = node.Name
		match.SourceAETitle = node.AETitle
		match.SourceHost = node.Host
		match.SourcePort = node.Port
		out[i] = match
	}
	return out
}

type queryLocalCatalog interface {
	StudyMetadata(context.Context, string) (archive.StudyMetadata, error)
	StudyExists(context.Context, string) (bool, error)
}

func enrichQueryMatchesWithLocalMetadata(ctx context.Context, catalog queryLocalCatalog, matches []query.Match) ([]query.Match, error) {
	out := make([]query.Match, len(matches))
	copy(out, matches)
	if catalog == nil {
		return out, nil
	}
	metadataByStudy := map[string]archive.StudyMetadata{}
	existsByStudy := map[string]bool{}
	for i := range out {
		studyUID := strings.TrimSpace(out[i].StudyInstanceUID)
		if !queryUIDAvailable(studyUID) {
			continue
		}
		exists, ok := existsByStudy[studyUID]
		if !ok {
			var err error
			exists, err = catalog.StudyExists(ctx, studyUID)
			if err != nil {
				return nil, err
			}
			existsByStudy[studyUID] = exists
		}
		if exists {
			out[i].LocalState = queryLocalStatePresent
		}
		metadata, ok := metadataByStudy[studyUID]
		if !ok {
			var err error
			metadata, err = catalog.StudyMetadata(ctx, studyUID)
			if err != nil {
				return nil, err
			}
			metadataByStudy[studyUID] = metadata
		}
		out[i].LocalComments = metadata.Comments
	}
	return out, nil
}

func nodeForQueryMatch(state *uiState, match query.Match) (nodes.Node, bool) {
	if state == nil {
		return nodes.Node{}, false
	}
	if strings.TrimSpace(match.SourceNodeID) != "" {
		for _, node := range state.nodes {
			if node.ID == match.SourceNodeID {
				return node, true
			}
		}
	}
	if strings.TrimSpace(match.SourceNodeName) != "" || strings.TrimSpace(match.SourceHost) != "" || match.SourcePort != 0 {
		for _, node := range state.nodes {
			if strings.EqualFold(node.Name, match.SourceNodeName) && strings.EqualFold(node.Host, match.SourceHost) && node.Port == match.SourcePort {
				return node, true
			}
		}
	}
	return selectedQueryNode(state)
}

func selectedSendNode(state *uiState) (nodes.Node, bool) {
	return selectedMatchingNode(state, func(node nodes.Node) bool {
		return node.Enabled() && node.SendEnabled()
	})
}

func selectedMatchingNode(state *uiState, match func(nodes.Node) bool) (nodes.Node, bool) {
	if state == nil || len(state.nodes) == 0 {
		return nodes.Node{}, false
	}
	row := state.selectedNodeRow
	if row >= 0 && row < len(state.nodes) && match(state.nodes[row]) {
		return state.nodes[row], true
	}
	for _, node := range state.nodes {
		if match(node) {
			return node, true
		}
	}
	return nodes.Node{}, false
}

func newElementTable(state *uiState) *widget.Table {
	headers := []string{"Source", "Tag", "VR", "Keyword", "Length", "Value"}
	table := widget.NewTable(
		func() (int, int) {
			return len(state.elements) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, headers[id.Col], true, false)
				return
			}
			elem := state.elements[id.Row-1]
			applyTextTableCell(cell, id.Row, tableCell(elem, id.Col), false, false)
		},
	)
	table.SetColumnWidth(0, 72)
	table.SetColumnWidth(1, 105)
	table.SetColumnWidth(2, 52)
	table.SetColumnWidth(3, 210)
	table.SetColumnWidth(4, 82)
	table.SetColumnWidth(5, 520)
	applyCompactTableRows(table)
	return table
}

var (
	archiveHeaderRowColor       = color.NRGBA{R: 40, G: 40, B: 40, A: 255}
	archivePatientRowColor      = color.NRGBA{R: 48, G: 48, B: 48, A: 255}
	archiveOddRowColor          = color.NRGBA{R: 28, G: 28, B: 28, A: 255}
	archiveEvenRowColor         = color.NRGBA{R: 34, G: 34, B: 34, A: 255}
	archiveSeriesRowColor       = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
	archiveSelectedRowColor     = color.NRGBA{R: 45, G: 85, B: 128, A: 255}
	tableColumnDividerColor     = color.NRGBA{R: 62, G: 62, B: 62, A: 255}
	queryRetrieveActionRowColor = color.NRGBA{R: 34, G: 58, B: 38, A: 255}
)

const tableColumnDividerWidth float32 = 1
const compactTableRowHeight float32 = 24

const (
	tableCellVerticalPadding   float32 = 1
	tableCellHorizontalPadding float32 = 4
)

func applyCompactTableRows(table *widget.Table) {
	if table == nil {
		return
	}
	table.SetRowHeight(-1, compactTableRowHeight)
}

type rightDividerLayout struct{}

func (rightDividerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	x := size.Width - tableColumnDividerWidth
	if x < 0 {
		x = 0
	}
	objects[0].Move(fyne.NewPos(x, 0))
	objects[0].Resize(fyne.NewSize(tableColumnDividerWidth, size.Height))
}

func (rightDividerLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(tableColumnDividerWidth, 1)
}

func newTableColumnDividerLayer() *fyne.Container {
	return container.New(rightDividerLayout{}, canvas.NewRectangle(tableColumnDividerColor))
}

func newCompactTableCellContent(content fyne.CanvasObject) *fyne.Container {
	return container.New(
		layout.NewCustomPaddedLayout(tableCellVerticalPadding, tableCellVerticalPadding, tableCellHorizontalPadding, tableCellHorizontalPadding),
		content,
	)
}

type archiveTableCell struct {
	*fyne.Container
	background   *canvas.Rectangle
	label        *widget.Label
	statusDot    *canvas.Circle
	statusDotBox *fyne.Container
}

func newArchiveTableCell() *archiveTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	statusDot := newSourceStatusDot()
	statusDotBox := container.NewPadded(sourceStatusDotBox(statusDot))
	statusDotBox.Hide()
	labelRow := container.NewBorder(nil, nil, statusDotBox, nil, label)
	return &archiveTableCell{
		Container:    container.NewStack(background, newCompactTableCellContent(labelRow), newTableColumnDividerLayer()),
		background:   background,
		label:        label,
		statusDot:    statusDot,
		statusDotBox: statusDotBox,
	}
}

type queryTableCell struct {
	*fyne.Container
	background     *canvas.Rectangle
	label          *widget.Label
	retrieveButton *widget.Button
	statusDot      *canvas.Circle
	statusDotBox   *fyne.Container
}

func newQueryTableCell() *queryTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	retrieveButton := newQueryRetrieveButton(nil)
	retrieveButton.Hide()
	statusDot := canvas.NewCircle(queryStatusOKColor)
	statusDot.StrokeColor = color.NRGBA{R: 26, G: 26, B: 26, A: 255}
	statusDot.StrokeWidth = 1
	statusDotBox := container.NewPadded(sourceStatusDotBox(statusDot))
	statusDotBox.Hide()
	labelRow := container.NewBorder(nil, nil, statusDotBox, nil, label)
	return &queryTableCell{
		Container:      container.NewStack(background, newCompactTableCellContent(labelRow), container.NewCenter(retrieveButton), newTableColumnDividerLayer()),
		background:     background,
		label:          label,
		retrieveButton: retrieveButton,
		statusDot:      statusDot,
		statusDotBox:   statusDotBox,
	}
}

func newQueryRetrieveButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", theme.DownloadIcon(), tapped)
	button.Importance = widget.LowImportance
	return button
}

func applyArchiveTableCell(cell *archiveTableCell, tableRow int, text string, row archiveBrowserRow, header bool, selected bool) {
	applyArchiveTableCellWithColumn(cell, tableRow, -1, text, row, header, selected)
}

func applyArchiveTableCellWithColumn(cell *archiveTableCell, tableRow int, tableCol int, text string, row archiveBrowserRow, header bool, selected bool) {
	if cell == nil {
		return
	}
	cell.statusDotBox.Hide()
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{Bold: header || row.kind == archiveRowPatient}
	cell.background.FillColor = archiveTableFillColor(tableRow, row, header, selected)
	if !header && tableCol == archiveStudyTableColumnStatus {
		if fill, ok := studyStatusDotColor(text); ok {
			cell.statusDot.FillColor = fill
			cell.statusDot.Refresh()
			cell.statusDotBox.Show()
		}
	}
	cell.background.Refresh()
	cell.label.Refresh()
}

func applyTextTableCell(cell *archiveTableCell, tableRow int, text string, header bool, selected bool) {
	applyArchiveTableCell(cell, tableRow, text, archiveBrowserRow{kind: archiveRowStudy}, header, selected)
}

func archiveBrowserRowSelected(row archiveBrowserRow, state *uiState) bool {
	if state == nil {
		return false
	}
	switch row.kind {
	case archiveRowStudy:
		return row.studyIndex >= 0 && row.studyIndex == state.selectedStudyRow && state.selectedSeriesRow < 0
	case archiveRowSeries:
		return row.studyIndex >= 0 &&
			row.seriesIndex >= 0 &&
			row.studyIndex == state.selectedStudyRow &&
			row.seriesIndex == state.selectedSeriesRow
	default:
		return false
	}
}

func archiveTableFillColor(tableRow int, row archiveBrowserRow, header bool, selected bool) color.NRGBA {
	if header {
		return archiveHeaderRowColor
	}
	if selected {
		return archiveSelectedRowColor
	}
	if row.kind == archiveRowPatient {
		return archivePatientRowColor
	}
	if row.kind == archiveRowSeries {
		return archiveSeriesRowColor
	}
	if tableRow%2 == 0 {
		return archiveEvenRowColor
	}
	return archiveOddRowColor
}

const (
	archiveStudyTableColumnPatient = iota
	archiveStudyTableColumnModality
	archiveStudyTableColumnInstances
	archiveStudyTableColumnSeries
	archiveStudyTableColumnPatientID
	archiveStudyTableColumnDOB
	archiveStudyTableColumnAccession
	archiveStudyTableColumnStudyDate
	archiveStudyTableColumnTime
	archiveStudyTableColumnAdded
	archiveStudyTableColumnInstitution
	archiveStudyTableColumnStatus
	archiveStudyTableColumnComments
	archiveStudyTableColumnDescription
	archiveStudyTableColumnStudyUID
)

func archiveTableHeaders() []string {
	columns := archiveVisibleStudyColumns()
	headers := make([]string, 0, len(columns))
	for _, col := range columns {
		headers = append(headers, archiveStudyTableHeader(col))
	}
	return headers
}

func archiveVisibleStudyColumns() []int {
	return []int{
		archiveStudyTableColumnPatient,
		archiveStudyTableColumnModality,
		archiveStudyTableColumnInstances,
		archiveStudyTableColumnSeries,
		archiveStudyTableColumnPatientID,
		archiveStudyTableColumnDOB,
		archiveStudyTableColumnAccession,
		archiveStudyTableColumnStudyDate,
		archiveStudyTableColumnAdded,
		archiveStudyTableColumnInstitution,
		archiveStudyTableColumnStatus,
		archiveStudyTableColumnComments,
	}
}

func archiveVisibleStudyColumn(tableCol int) (int, bool) {
	columns := archiveVisibleStudyColumns()
	if tableCol < 0 || tableCol >= len(columns) {
		return 0, false
	}
	return columns[tableCol], true
}

func archiveStudyTableHeader(col int) string {
	switch col {
	case archiveStudyTableColumnPatient:
		return "Patient name"
	case archiveStudyTableColumnModality:
		return "Modality"
	case archiveStudyTableColumnInstances:
		return "# im"
	case archiveStudyTableColumnSeries:
		return "# ser"
	case archiveStudyTableColumnPatientID:
		return "Patient ID"
	case archiveStudyTableColumnDOB:
		return "Date of Birth"
	case archiveStudyTableColumnAccession:
		return "Accession"
	case archiveStudyTableColumnStudyDate:
		return "Date Acquired"
	case archiveStudyTableColumnTime:
		return "Time"
	case archiveStudyTableColumnAdded:
		return "Date Added"
	case archiveStudyTableColumnInstitution:
		return "Institution"
	case archiveStudyTableColumnStatus:
		return "Status"
	case archiveStudyTableColumnComments:
		return "Comments"
	case archiveStudyTableColumnDescription:
		return "Description"
	case archiveStudyTableColumnStudyUID:
		return "Study UID"
	default:
		return ""
	}
}

func newStudyTable(state *uiState) *widget.Table {
	headers := archiveTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.archiveRows) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			col, ok := archiveVisibleStudyColumn(id.Col)
			if !ok {
				applyArchiveTableCell(cell, id.Row, "", archiveBrowserRow{}, id.Row == 0, false)
				return
			}
			if id.Row == 0 {
				applyArchiveTableCell(cell, id.Row, archiveHeaderLabel(state, col, headers[id.Col]), archiveBrowserRow{}, true, false)
				return
			}
			row := state.archiveRows[id.Row-1]
			selected := archiveBrowserRowSelected(row, state)
			applyArchiveTableCellWithColumn(cell, id.Row, col, archiveBrowserCell(row, state.studies, col), row, false, selected)
		},
	)
	state.selectedStudyRow = -1
	widths := []float32{240, 95, 70, 70, 120, 110, 120, 145, 120, 180, 90, 180}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	applyCompactTableRows(table)
	return table
}

func applyArchiveSort(state *uiState, col int) bool {
	if state == nil || !archiveColumnSortable(col) {
		return false
	}
	selectedStudyUID := selectedArchiveStudyUID(state)
	if state.archiveSortActive && state.archiveSortColumn == col {
		state.archiveSortDescending = !state.archiveSortDescending
	} else {
		state.archiveSortActive = true
		state.archiveSortColumn = col
		state.archiveSortDescending = false
	}
	descending := state.archiveSortDescending
	sort.SliceStable(state.studies, func(i, j int) bool {
		left := archiveSortValue(state.studies[i], col)
		right := archiveSortValue(state.studies[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.studies[i].StudyInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.studies[j].StudyInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
	state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(state.studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
	state.selectedStudyRow = archiveStudyIndexByUID(state.studies, selectedStudyUID)
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	state.series = nil
	state.instances = nil
	return true
}

func selectedArchiveStudyUID(state *uiState) string {
	if state == nil || state.selectedStudyRow < 0 || state.selectedStudyRow >= len(state.studies) {
		return ""
	}
	return strings.TrimSpace(state.studies[state.selectedStudyRow].StudyInstanceUID)
}

func archiveStudyIndexByUID(studies []archive.Study, studyUID string) int {
	studyUID = strings.TrimSpace(studyUID)
	if studyUID == "" {
		return -1
	}
	for index, study := range studies {
		if strings.TrimSpace(study.StudyInstanceUID) == studyUID {
			return index
		}
	}
	return -1
}

func archiveColumnSortable(col int) bool {
	switch col {
	case archiveStudyTableColumnTime, archiveStudyTableColumnDescription, archiveStudyTableColumnStudyUID:
		return false
	default:
		for _, visibleCol := range archiveVisibleStudyColumns() {
			if col == visibleCol {
				return true
			}
		}
		return false
	}
}

func archiveSortValue(study archive.Study, col int) string {
	return strings.ToLower(strings.TrimSpace(studyCell(study, col)))
}

func archiveHeaderLabel(state *uiState, col int, label string) string {
	if state == nil || !state.archiveSortActive || state.archiveSortColumn != col {
		return label
	}
	if state.archiveSortDescending {
		return label + " ▼"
	}
	return label + " ▲"
}

func newSeriesTable(state *uiState) *widget.Table {
	headers := seriesTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.series) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, seriesHeaderLabel(state, id.Col, headers[id.Col]), true, false)
				return
			}
			series := state.series[id.Row-1]
			selected := id.Row-1 == state.selectedSeriesRow
			applyTextTableCell(cell, id.Row, seriesCell(series, id.Col), false, selected)
		},
	)
	state.selectedSeriesRow = -1
	table.SetColumnWidth(0, 80)
	table.SetColumnWidth(1, 85)
	table.SetColumnWidth(2, 220)
	table.SetColumnWidth(3, 80)
	table.SetColumnWidth(4, 360)
	applyCompactTableRows(table)
	return table
}

const (
	seriesTableColumnNumber = iota
	seriesTableColumnModality
	seriesTableColumnDescription
	seriesTableColumnInstances
	seriesTableColumnUID
)

func seriesTableHeaders() []string {
	return []string{"Series #", "Modality", "Description", "Instances", "Series UID"}
}

func applySeriesSort(state *uiState, col int) bool {
	if state == nil || !seriesColumnSortable(col) {
		return false
	}
	if state.seriesSortActive && state.seriesSortColumn == col {
		state.seriesSortDescending = !state.seriesSortDescending
	} else {
		state.seriesSortActive = true
		state.seriesSortColumn = col
		state.seriesSortDescending = false
	}
	descending := state.seriesSortDescending
	sort.SliceStable(state.series, func(i, j int) bool {
		left := seriesSortValue(state.series[i], col)
		right := seriesSortValue(state.series[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.series[i].SeriesInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.series[j].SeriesInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	state.instances = nil
	refreshArchiveRowsAfterSeriesSort(state)
	return true
}

func refreshArchiveRowsAfterSeriesSort(state *uiState) {
	if state == nil || len(state.studies) == 0 {
		return
	}
	if state.archiveSeriesByStudy != nil && state.selectedStudyRow >= 0 && state.selectedStudyRow < len(state.studies) {
		studyUID := strings.TrimSpace(state.studies[state.selectedStudyRow].StudyInstanceUID)
		if studyUID != "" {
			state.archiveSeriesByStudy[studyUID] = state.series
		}
	}
	if state.archiveRows != nil {
		state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(state.studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
	}
}

func seriesColumnSortable(col int) bool {
	return col >= 0 && col < len(seriesTableHeaders())
}

func seriesSortValue(series archive.Series, col int) string {
	switch col {
	case seriesTableColumnNumber, seriesTableColumnInstances:
		return numericSortValue(seriesCell(series, col))
	default:
		return strings.ToLower(strings.TrimSpace(seriesCell(series, col)))
	}
}

func seriesHeaderLabel(state *uiState, col int, label string) string {
	if state == nil || !state.seriesSortActive || state.seriesSortColumn != col {
		return label
	}
	if state.seriesSortDescending {
		return label + " ▼"
	}
	return label + " ▲"
}

func newInstanceTable(state *uiState) *widget.Table {
	headers := instanceTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.instances) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, instanceHeaderLabel(state, id.Col, headers[id.Col]), true, false)
				return
			}
			instance := state.instances[id.Row-1]
			selected := id.Row-1 == state.selectedInstanceRow
			applyTextTableCell(cell, id.Row, instanceCell(instance, id.Col), false, selected)
		},
	)
	state.selectedInstanceRow = -1
	table.SetColumnWidth(0, 90)
	table.SetColumnWidth(1, 85)
	table.SetColumnWidth(2, 220)
	table.SetColumnWidth(3, 180)
	table.SetColumnWidth(4, 280)
	table.SetColumnWidth(5, 360)
	applyCompactTableRows(table)
	return table
}

const (
	instanceTableColumnNumber = iota
	instanceTableColumnModality
	instanceTableColumnSOPClass
	instanceTableColumnTransferSyntax
	instanceTableColumnSource
	instanceTableColumnSOPUID
)

func instanceTableHeaders() []string {
	return []string{"Instance #", "Modality", "SOP Class", "Transfer Syntax", "Source", "SOP UID"}
}

func applyInstanceSort(state *uiState, col int) bool {
	if state == nil || !instanceColumnSortable(col) {
		return false
	}
	if state.instanceSortActive && state.instanceSortColumn == col {
		state.instanceSortDescending = !state.instanceSortDescending
	} else {
		state.instanceSortActive = true
		state.instanceSortColumn = col
		state.instanceSortDescending = false
	}
	descending := state.instanceSortDescending
	sort.SliceStable(state.instances, func(i, j int) bool {
		left := instanceSortValue(state.instances[i], col)
		right := instanceSortValue(state.instances[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.instances[i].SOPInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.instances[j].SOPInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
	state.selectedInstanceRow = -1
	return true
}

func instanceColumnSortable(col int) bool {
	return col >= 0 && col < len(instanceTableHeaders())
}

func instanceSortValue(instance archive.Instance, col int) string {
	switch col {
	case instanceTableColumnNumber:
		return numericSortValue(instanceCell(instance, col))
	default:
		return strings.ToLower(strings.TrimSpace(instanceCell(instance, col)))
	}
}

func instanceHeaderLabel(state *uiState, col int, label string) string {
	if state == nil || !state.instanceSortActive || state.instanceSortColumn != col {
		return label
	}
	if state.instanceSortDescending {
		return label + " ▼"
	}
	return label + " ▲"
}

func numericSortValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return "z:" + strings.ToLower(value)
	}
	return fmt.Sprintf("n:%020d", number)
}

func wireArchiveTables(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	tables.studies.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			col, ok := archiveVisibleStudyColumn(id.Col)
			if ok && applyArchiveSort(state, col) {
				tables.studies.Refresh()
				tables.series.Refresh()
				tables.instances.Refresh()
				refreshArchiveChrome(state)
				status.SetText("Sorted Archive by " + archiveTableHeaders()[id.Col])
				return
			}
			state.selectedStudyRow = -1
			clearArchiveDetails(state, tables)
			refreshArchiveChrome(state)
			tables.studies.Refresh()
			return
		}
		rowIndex := id.Row - 1
		if rowIndex < 0 || rowIndex >= len(state.archiveRows) {
			return
		}
		row := state.archiveRows[rowIndex]
		if row.kind == archiveRowPatient {
			if toggleArchivePatientGroup(state, row) {
				clearArchiveDetails(state, tables)
				tables.studies.Refresh()
				refreshArchiveChrome(state)
			}
			return
		}
		if row.kind == archiveRowSeries {
			if row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
				return
			}
			study := state.studies[row.studyIndex]
			seriesRows := state.archiveSeriesByStudy[study.StudyInstanceUID]
			if row.seriesIndex < 0 || row.seriesIndex >= len(seriesRows) {
				return
			}
			state.selectedStudyRow = row.studyIndex
			state.series = seriesRows
			state.selectedSeriesRow = row.seriesIndex
			state.instances = nil
			state.selectedInstanceRow = -1
			tables.studies.Refresh()
			tables.series.Refresh()
			tables.instances.Refresh()
			refreshArchiveChrome(state)
			series := seriesRows[row.seriesIndex]
			status.SetText("Loading instances for " + series.SeriesInstanceUID)
			go func(selectedStudyRow int, selectedSeriesRow int, seriesUID string) {
				instances, err := state.catalog.InstancesForSeries(context.Background(), seriesUID)
				fyne.Do(func() {
					if state.selectedStudyRow != selectedStudyRow ||
						state.selectedSeriesRow != selectedSeriesRow ||
						selectedSeriesRow < 0 ||
						selectedSeriesRow >= len(state.series) ||
						state.series[selectedSeriesRow].SeriesInstanceUID != seriesUID {
						return
					}
					if err != nil {
						status.SetText("Load instances failed")
						dialog.ShowError(err, w)
						return
					}
					setInstances(state, tables, instances)
					status.SetText(fmt.Sprintf("%d instances for series %s", len(instances), seriesUID))
				})
			}(row.studyIndex, row.seriesIndex, series.SeriesInstanceUID)
			return
		}
		if row.kind != archiveRowStudy || row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
			return
		}
		state.selectedStudyRow = row.studyIndex
		study := state.studies[row.studyIndex]
		if toggleArchiveStudySeries(state, row) {
			tables.studies.Refresh()
			refreshArchiveChrome(state)
			if state.collapsedArchiveStudies[study.StudyInstanceUID] {
				status.SetText("Collapsed series for " + study.StudyInstanceUID)
				return
			}
			status.SetText("Expanded series for " + study.StudyInstanceUID)
			return
		}
		clearArchiveDetails(state, tables)
		tables.studies.Refresh()
		refreshArchiveChrome(state)
		status.SetText("Loading series for " + study.StudyInstanceUID)
		filters := state.seriesFilters
		go func(selectedRow int, studyUID string, filters archive.SeriesFilters) {
			series, err := state.catalog.SeriesForStudyWithFilters(context.Background(), studyUID, filters)
			fyne.Do(func() {
				if state.selectedStudyRow != selectedRow ||
					selectedRow < 0 ||
					selectedRow >= len(state.studies) ||
					state.studies[selectedRow].StudyInstanceUID != studyUID {
					return
				}
				if err != nil {
					status.SetText("Load series failed")
					dialog.ShowError(err, w)
					return
				}
				setSeries(state, tables, series)
				status.SetText(fmt.Sprintf("%d series for study %s", len(series), studyUID))
			})
		}(row.studyIndex, study.StudyInstanceUID, filters)
	}

	tables.series.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			if applySeriesSort(state, id.Col) {
				tables.studies.Refresh()
				tables.series.Refresh()
				tables.instances.Refresh()
				refreshArchiveChrome(state)
				status.SetText("Sorted Series by " + seriesTableHeaders()[id.Col])
				return
			}
			state.selectedSeriesRow = -1
			setInstances(state, tables, nil)
			refreshArchiveChrome(state)
			tables.series.Refresh()
			return
		}
		row := id.Row - 1
		if row < 0 || row >= len(state.series) {
			return
		}
		state.selectedSeriesRow = row
		series := state.series[row]
		tables.series.Refresh()
		setInstances(state, tables, nil)
		refreshArchiveChrome(state)
		status.SetText("Loading instances for " + series.SeriesInstanceUID)
		go func(selectedRow int, seriesUID string) {
			instances, err := state.catalog.InstancesForSeries(context.Background(), seriesUID)
			fyne.Do(func() {
				if state.selectedSeriesRow != selectedRow ||
					selectedRow < 0 ||
					selectedRow >= len(state.series) ||
					state.series[selectedRow].SeriesInstanceUID != seriesUID {
					return
				}
				if err != nil {
					status.SetText("Load instances failed")
					dialog.ShowError(err, w)
					return
				}
				setInstances(state, tables, instances)
				status.SetText(fmt.Sprintf("%d instances for series %s", len(instances), seriesUID))
			})
		}(row, series.SeriesInstanceUID)
	}

	tables.instances.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			if applyInstanceSort(state, id.Col) {
				tables.instances.Refresh()
				refreshArchiveChrome(state)
				status.SetText("Sorted Instances by " + instanceTableHeaders()[id.Col])
				return
			}
			state.selectedInstanceRow = -1
			refreshArchiveChrome(state)
			tables.instances.Refresh()
			return
		}
		row := id.Row - 1
		if row >= 0 && row < len(state.instances) {
			state.selectedInstanceRow = row
			refreshArchiveChrome(state)
			tables.instances.Refresh()
		}
	}
}

func newAutoQueryTab(w fyne.Window, status *widget.Label, tables archiveTables, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	savedCriteria := autoQueryCriteriaForState(state)
	profileNames := autoQueryProfileNames(state)
	profileSelect := widget.NewSelect(profileNames, nil)
	profileSelect.SetSelected(selectedAutoQueryProfileName(state))
	titleLabel := workbenchCenteredTitle(autoQueryWindowTitle(profileSelect.Selected))
	previousButton := autoQueryEnabledIconButton(theme.NavigateBackIcon(), func() {
		selectRelativeAutoQueryProfile(profileSelect, -1)
	})
	nextButton := autoQueryEnabledIconButton(theme.NavigateNextIcon(), func() {
		selectRelativeAutoQueryProfile(profileSelect, 1)
	})
	if len(profileNames) <= 1 {
		previousButton.Disable()
		nextButton.Disable()
	}
	var refreshProfileOptions func(string)
	var syncAutoQueryLockedControls func()
	renameButton := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		if state == nil {
			return
		}
		if w == nil {
			newName := nextAutoQueryProfileName(state.autoQueryProfiles)
			if applyAutoQueryProfileRename(nil, status, state, newName) && refreshProfileOptions != nil {
				refreshProfileOptions(selectedAutoQueryProfileName(state))
			}
			return
		}
		entry := widget.NewEntry()
		entry.SetText(selectedAutoQueryProfileName(state))
		form := dialog.NewForm("Rename Auto Q/R Profile", "Rename", "Cancel", []*widget.FormItem{
			widget.NewFormItem("Name", entry),
		}, func(ok bool) {
			if !ok {
				return
			}
			if applyAutoQueryProfileRename(w, status, state, entry.Text) && refreshProfileOptions != nil {
				refreshProfileOptions(selectedAutoQueryProfileName(state))
			}
		}, w)
		form.Resize(fyne.NewSize(420, 0))
		form.Show()
	})
	renameButton.Importance = widget.LowImportance
	addButton := autoQueryEnabledIconButton(theme.ContentAddIcon(), func() {
		if state == nil {
			return
		}
		profile := addAutoQueryProfile(state)
		if refreshProfileOptions != nil {
			refreshProfileOptions(profile.Name)
		}
		if err := saveAutoQueryProfileList(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R profile save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			status.SetText("Auto Q/R profile added")
		}
	})
	removeButton := autoQueryEnabledIconButton(theme.DeleteIcon(), func() {
		if state == nil {
			return
		}
		if !removeSelectedAutoQueryProfile(state) {
			if status != nil {
				status.SetText("Default Auto Q/R profile cannot be removed")
			}
			return
		}
		selected := selectedAutoQueryProfileName(state)
		if refreshProfileOptions != nil {
			refreshProfileOptions(selected)
		}
		if err := saveAutoQueryProfileList(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R profile save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			status.SetText("Auto Q/R profile removed")
		}
	})
	lockButton := autoQueryEnabledIconButton(theme.ConfirmIcon(), func() {
		if state == nil {
			return
		}
		previous := autoQueryProfileLocked(state)
		setAutoQueryProfileLocked(state, !previous)
		if err := saveAutoQuerySelectedProfile(state); err != nil {
			setAutoQueryProfileLocked(state, previous)
			if status != nil {
				status.SetText("Auto Q/R profile lock save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			if autoQueryProfileLocked(state) {
				status.SetText("Auto Q/R profile locked")
			} else {
				status.SetText("Auto Q/R profile unlocked")
			}
		}
		if syncAutoQueryLockedControls != nil {
			syncAutoQueryLockedControls()
		}
	})
	updatingProfile := false

	quickSearchField := widget.NewSelect(queryQuickSearchOptions, func(field string) {
		if state != nil {
			state.autoQuerySearchField = field
		}
	})
	quickSearchField.SetSelected(savedCriteria.SearchField)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder(savedCriteria.SearchField)
	quickSearch.SetText(savedCriteria.SearchText)
	quickSearch.OnChanged = func(search string) {
		if state != nil {
			state.autoQuerySearchText = strings.TrimSpace(search)
		}
	}
	quickSearchField.OnChanged = func(field string) {
		quickSearch.SetPlaceHolder(field)
		if state != nil {
			state.autoQuerySearchField = field
		}
	}

	datePreset := newQueryDatePresetRadioGrid(func(preset string) {
		if state != nil {
			state.autoQueryDatePreset = preset
		}
	})
	datePreset.SetSelected(savedCriteria.DatePreset)
	modalityChecks := newQueryModalityChecks()
	for _, modality := range savedCriteria.Modalities {
		if check := modalityChecks[modality]; check != nil {
			check.SetChecked(true)
		}
	}
	for _, check := range modalityChecks {
		check.OnChanged = func(_ bool) {
			if state != nil {
				state.autoQueryModalities = selectedQueryModalities(modalityChecks)
			}
		}
	}

	sourceList := newAutoQuerySourceList(w, status, state)
	moveAutoSource := func(delta int) {
		if autoQueryProfileLocked(state) {
			if status != nil {
				status.SetText("Auto Q/R profile is locked")
			}
			return
		}
		if !moveAutoQuerySource(state, delta) {
			return
		}
		if err := saveAutoQuerySelectedProfile(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R source order save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			status.SetText("Updated Auto Q/R source priority")
		}
		sourceList.Refresh()
	}
	sourceMoveUp := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		moveAutoSource(-1)
	})
	sourceMoveUp.Importance = widget.LowImportance
	sourceMoveDown := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
		moveAutoSource(1)
	})
	sourceMoveDown.Importance = widget.LowImportance
	sourceHeader := newDicomNodesHeader(container.NewHBox(sourceMoveUp, sourceMoveDown))
	sourcePanel := container.NewBorder(sourceHeader, nil, nil, nil, sourceList)
	sourcePanel.Resize(fyne.NewSize(360, 0))

	destinationSelect := newQueryMoveDestinationEntry(state)
	refreshCadence := widget.NewSelect(autoQueryRefreshModeOptions, nil)
	refreshCadence.SetSelected(savedCriteria.RefreshMode)
	if state != nil {
		state.autoQueryRefreshMode = savedCriteria.RefreshMode
	}
	countdown := widget.NewLabel(autoQueryCountdownText(savedCriteria.RefreshMode, time.Time{}, autoQueryRefreshAvailable(state), time.Now()))
	countdown.Wrapping = fyne.TextTruncate
	if state != nil {
		state.autoQueryCountdownLabel = countdown
		refreshAutoQueryCountdown(state, savedCriteria.RefreshMode, time.Now())
	}
	autoRetrieve := newAutoQueryAutoRetrieveCheck(state)
	settingsButton := widget.NewButtonWithIcon(autoQuerySettingsButtonText, theme.SettingsIcon(), func() {
		showAutoQuerySettingsDialog(w, status, state)
	})
	settingsButton.Importance = widget.LowImportance

	queryTable := newQueryTable(state, func() {
		retrieveSelectedQuery(w, status, tables, state)
	})
	refreshCadence.OnChanged = func(mode string) {
		if state != nil {
			state.autoQueryRefreshMode = mode
		}
		if updatingProfile {
			refreshAutoQueryCountdown(state, mode, time.Now())
			return
		}
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, mode)
		if state == nil {
			countdown.SetText(autoQueryCountdownText(mode, time.Time{}, false, time.Now()))
		}
	}
	queryButton := widget.NewButtonWithIcon(queryActionLabelQuery, theme.MediaPlayIcon(), func() {
		profileCriteria := autoQueryCriteriaFromControls(quickSearchField.Selected, quickSearch.Text, datePreset.Selected(), modalityChecks, refreshCadence.Selected)
		criteria, ok := autoQueryStudyCriteria(quickSearchField.Selected, quickSearch.Text, datePreset.Selected(), modalityChecks, time.Now())
		if !ok {
			if status != nil {
				status.SetText("Auto Q/R query failed")
			}
			if w != nil {
				dialog.ShowError(fmt.Errorf("unsupported Auto Q/R search field %q", quickSearchField.Selected), w)
			}
			return
		}
		if !saveAutoQueryProfileCriteria(w, status, state, profileCriteria) {
			return
		}
		rememberAutoQueryStudy(state, criteria)
		runStudyQueryWithSources(w, status, queryTable, state, criteria, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, refreshCadence.Selected)
	})
	queryButton.Importance = widget.LowImportance
	submitAutoStudyQuery := func() {
		if queryButton != nil && queryButton.OnTapped != nil {
			queryButton.OnTapped()
		}
	}
	autoQuerySearchBar := newQuerySearchBar(quickSearch, submitAutoStudyQuery)
	patientButton := widget.NewButtonWithIcon(queryActionLabelPatient, theme.MediaPlayIcon(), func() {
		profileCriteria := autoQueryCriteriaFromControls(quickSearchField.Selected, quickSearch.Text, datePreset.Selected(), modalityChecks, refreshCadence.Selected)
		criteria, ok := autoQueryPatientCriteria(quickSearchField.Selected, quickSearch.Text)
		if !ok {
			if status != nil {
				status.SetText("Auto Q/R patient query failed")
			}
			if w != nil {
				dialog.ShowError(fmt.Errorf("unsupported Auto Q/R Patient search field %q", quickSearchField.Selected), w)
			}
			return
		}
		if !saveAutoQueryProfileCriteria(w, status, state, profileCriteria) {
			return
		}
		rememberAutoQueryPatient(state, criteria)
		runPatientQueryWithSources(w, status, queryTable, state, criteria, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, refreshCadence.Selected)
	})
	patientButton.Importance = widget.LowImportance
	retrieveButton := disabledAutoQueryAction(queryActionLabelRetrieve, theme.DownloadIcon())
	verifyButton := widget.NewButtonWithIcon(queryActionLabelVerify, theme.ConfirmIcon(), func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	verifyButton.Importance = widget.LowImportance
	refreshButton := newQueryRefreshButton(func() {
		refreshAutoQuery(w, status, tables, queryTable, state)
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, refreshCadence.Selected)
	})
	profileSelect.OnChanged = func(name string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		updatingProfile = true
		defer func() {
			updatingProfile = false
		}()
		if !selectAutoQueryProfile(state, name) {
			return
		}
		criteria := autoQueryCriteriaForState(state)
		quickSearchField.SetSelected(criteria.SearchField)
		quickSearch.SetPlaceHolder(criteria.SearchField)
		quickSearch.SetText(criteria.SearchText)
		datePreset.SetSelected(criteria.DatePreset)
		for _, check := range modalityChecks {
			check.SetChecked(false)
		}
		for _, modality := range criteria.Modalities {
			if check := modalityChecks[modality]; check != nil {
				check.SetChecked(true)
			}
		}
		sourceList.Refresh()
		refreshCadence.SetSelected(criteria.RefreshMode)
		refreshAutoQueryCountdown(state, criteria.RefreshMode, time.Now())
		syncAutoQueryProfileButtons(previousButton, nextButton, removeButton, profileSelect.Options, name)
		titleLabel.SetText(autoQueryWindowTitle(name))
		if syncAutoQueryLockedControls != nil {
			syncAutoQueryLockedControls()
		}
	}
	refreshProfileOptions = func(selected string) {
		profileSelect.Options = autoQueryProfileNames(state)
		profileSelect.Refresh()
		syncAutoQueryProfileButtons(previousButton, nextButton, removeButton, profileSelect.Options, selected)
		if stringInList(selected, profileSelect.Options) && profileSelect.Selected != selected {
			profileSelect.SetSelected(selected)
		}
	}
	refreshProfileOptions(selectedAutoQueryProfileName(state))
	syncAutoQueryLockedControls = func() {
		locked := autoQueryProfileLocked(state)
		setDisableableControl(quickSearchField, locked)
		setDisableableControl(quickSearch, locked)
		datePreset.SetDisabled(locked)
		for _, check := range modalityChecks {
			setDisableableControl(check, locked)
		}
		setDisableableControl(refreshCadence, locked)
		setDisableableControl(autoRetrieve, locked)
		setDisableableControl(settingsButton, locked)
		setDisableableControl(sourceMoveUp, locked)
		setDisableableControl(sourceMoveDown, locked)
		if locked || strings.EqualFold(selectedAutoQueryProfileName(state), autoquery.DefaultProfileName) {
			renameButton.Disable()
		} else {
			renameButton.Enable()
		}
		if locked {
			removeButton.Disable()
		} else {
			syncAutoQueryProfileButtons(previousButton, nextButton, removeButton, profileSelect.Options, profileSelect.Selected)
		}
		sourceList.Refresh()
	}
	syncAutoQueryLockedControls()

	resultSummary := widget.NewLabel(autoQueryResultSummaryText(state))
	resultSummary.Wrapping = fyne.TextTruncate
	state.autoQueryResultSummaryLabel = resultSummary

	profileBar := container.NewBorder(
		nil,
		nil,
		container.NewHBox(previousButton, nextButton),
		container.NewHBox(addButton, renameButton, removeButton, lockButton),
		profileSelect,
	)
	searchBar := container.NewBorder(
		nil,
		nil,
		labeledControl("Search", quickSearchField),
		nil,
		autoQuerySearchBar,
	)
	filters := container.NewGridWithColumns(3,
		sourcePanel,
		container.NewVBox(workbenchSectionTitle("Date"), datePreset.CanvasObject()),
		container.NewVBox(workbenchSectionTitle("Modalities"), queryModalityGrid(modalityChecks)),
	)
	actions := container.NewBorder(
		nil,
		nil,
		container.NewHBox(labeledControl("Retrieve to", destinationSelect), queryButton, patientButton, retrieveButton, verifyButton),
		container.NewHBox(labeledControl("Refresh", refreshCadence), countdown, refreshButton),
		container.NewHBox(autoRetrieve, settingsButton),
	)
	top := container.NewVBox(titleLabel, profileBar, searchBar, filters, actions)
	return container.NewBorder(top, resultSummary, nil, nil, container.NewStack(queryTable))
}

func autoQueryWindowTitle(profile string) string {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = autoquery.DefaultProfileName
	}
	return "DICOM Auto Query/Retrieve : " + profile
}

func newDicomNodesHeader(actions fyne.CanvasObject) fyne.CanvasObject {
	instruction := widget.NewLabel(dicomNodesInstruction)
	instruction.Wrapping = fyne.TextWrapWord
	return container.NewVBox(
		container.NewBorder(nil, nil, workbenchSectionTitle(dicomNodesTitle), actions),
		instruction,
	)
}

func autoQueryIconButton(icon fyne.Resource) *widget.Button {
	button := widget.NewButtonWithIcon("", icon, nil)
	button.Importance = widget.LowImportance
	button.Disable()
	return button
}

func autoQueryEnabledIconButton(icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

func selectRelativeAutoQueryProfile(selectWidget *widget.Select, delta int) {
	if selectWidget == nil || len(selectWidget.Options) == 0 {
		return
	}
	index := 0
	for i, option := range selectWidget.Options {
		if option == selectWidget.Selected {
			index = i
			break
		}
	}
	next := (index + delta + len(selectWidget.Options)) % len(selectWidget.Options)
	selectWidget.SetSelected(selectWidget.Options[next])
}

func syncAutoQueryProfileButtons(previous *widget.Button, next *widget.Button, remove *widget.Button, profiles []string, selected string) {
	if len(profiles) <= 1 {
		if previous != nil {
			previous.Disable()
		}
		if next != nil {
			next.Disable()
		}
	} else {
		if previous != nil {
			previous.Enable()
		}
		if next != nil {
			next.Enable()
		}
	}
	if remove == nil {
		return
	}
	if len(profiles) <= 1 || strings.EqualFold(strings.TrimSpace(selected), autoquery.DefaultProfileName) {
		remove.Disable()
		return
	}
	remove.Enable()
}

func disabledAutoQueryAction(text string, icon fyne.Resource) *widget.Button {
	button := widget.NewButtonWithIcon(text, icon, nil)
	button.Importance = widget.LowImportance
	button.Disable()
	return button
}

func autoQueryResultSummaryText(state *uiState) string {
	count := 0
	if state != nil {
		count = len(state.queries)
	}
	if count == 1 {
		return "1 study found"
	}
	return fmt.Sprintf("%d studies found", count)
}

func refreshAutoQueryResultSummary(state *uiState) {
	if state == nil || state.autoQueryResultSummaryLabel == nil {
		return
	}
	state.autoQueryResultSummaryLabel.SetText(autoQueryResultSummaryText(state))
}

func autoQueryRefreshInterval(mode string) time.Duration {
	switch mode {
	case autoQueryRefreshEvery30Min:
		return 30 * time.Minute
	default:
		return 0
	}
}

func autoQueryCountdownText(mode string, next time.Time, hasQuery bool, now time.Time) string {
	if autoQueryRefreshInterval(mode) <= 0 {
		return autoQueryCountdownDormant
	}
	if !hasQuery {
		return "Next: waiting for Query"
	}
	if next.IsZero() {
		return autoQueryCountdownDormant
	}
	remaining := next.Sub(now)
	if remaining <= 0 {
		return "Next: now"
	}
	remaining = remaining.Round(time.Second)
	if remaining < time.Second {
		remaining = time.Second
	}
	if remaining >= time.Hour {
		hours := int(remaining / time.Hour)
		minutes := int((remaining % time.Hour) / time.Minute)
		return fmt.Sprintf("Next: %dh %02dm", hours, minutes)
	}
	minutes := int(remaining / time.Minute)
	seconds := int((remaining % time.Minute) / time.Second)
	return fmt.Sprintf("Next: %dm %02ds", minutes, seconds)
}

func refreshAutoQueryCountdown(state *uiState, mode string, now time.Time) {
	if state == nil || state.autoQueryCountdownLabel == nil {
		return
	}
	state.autoQueryCountdownLabel.SetText(autoQueryCountdownText(mode, state.autoQueryNextRefresh, autoQueryRefreshAvailable(state), now))
}

func stopAutoQueryRefresh(state *uiState) {
	if state == nil {
		return
	}
	if state.autoQueryRefreshCancel != nil {
		state.autoQueryRefreshCancel()
		state.autoQueryRefreshCancel = nil
	}
	state.autoQueryNextRefresh = time.Time{}
}

func scheduleAutoQueryRefresh(w fyne.Window, status *widget.Label, tables archiveTables, table *widget.Table, state *uiState, mode string) {
	scheduleAutoQueryRefreshAt(w, status, tables, table, state, mode, time.Now())
}

func scheduleAutoQueryRefreshAt(w fyne.Window, status *widget.Label, tables archiveTables, table *widget.Table, state *uiState, mode string, now time.Time) {
	if state == nil {
		return
	}
	stopAutoQueryRefresh(state)
	interval := autoQueryRefreshInterval(mode)
	if interval <= 0 || !autoQueryRefreshAvailable(state) {
		refreshAutoQueryCountdown(state, mode, now)
		return
	}
	state.autoQueryNextRefresh = now.Add(interval)
	refreshAutoQueryCountdown(state, mode, now)
	ctx, cancel := context.WithCancel(context.Background())
	state.autoQueryRefreshCancel = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker.C:
				fyne.Do(func() {
					refreshAutoQueryCountdown(state, mode, tick)
				})
			case <-timer.C:
				fyne.Do(func() {
					refreshAutoQuery(w, status, tables, table, state)
					scheduleAutoQueryRefresh(w, status, tables, table, state, mode)
				})
				return
			}
		}
	}()
}

func rememberAutoQueryStudy(state *uiState, criteria query.Criteria) {
	if state == nil {
		return
	}
	state.autoQueryLast = lastQueryRequest{kind: queryRunStudy, study: criteria}
}

func rememberAutoQueryPatient(state *uiState, criteria query.PatientCriteria) {
	if state == nil {
		return
	}
	state.autoQueryLast = lastQueryRequest{kind: queryRunPatient, patient: criteria}
}

func autoQueryRefreshAvailable(state *uiState) bool {
	return state != nil && state.autoQueryLast.kind != ""
}

func refreshAutoQuery(w fyne.Window, status *widget.Label, tables archiveTables, table *widget.Table, state *uiState) {
	if !autoQueryRefreshAvailable(state) {
		if status != nil {
			status.SetText("No Auto Q/R query to refresh")
		}
		return
	}
	switch state.autoQueryLast.kind {
	case queryRunStudy:
		runStudyQueryWithSources(w, status, table, state, state.autoQueryLast.study, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
	case queryRunPatient:
		runPatientQueryWithSources(w, status, table, state, state.autoQueryLast.patient, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
	default:
		if status != nil {
			status.SetText("Unsupported Auto Q/R refresh")
		}
	}
}

func autoQueryStudyCriteria(field string, search string, datePreset string, modalityChecks map[string]*widget.Check, now time.Time) (query.Criteria, bool) {
	criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{
		Modality: queryModalityCriteriaText("", modalityChecks),
	}, field, search)
	if !ok {
		return query.Criteria{}, false
	}
	dateFrom, dateTo, ok := queryDatePresetRange(datePreset, now)
	if ok {
		criteria.StudyDateFrom = dateFrom
		criteria.StudyDateTo = dateTo
	}
	return criteria, true
}

func autoQueryPatientCriteria(field string, search string) (query.PatientCriteria, bool) {
	return queryPatientCriteriaWithQuickSearch(query.PatientCriteria{}, field, search)
}

func newAutoQuerySourceList(w fyne.Window, status *widget.Label, state *uiState) *widget.List {
	var list *widget.List
	list = widget.NewList(
		func() int {
			return len(autoQuerySourceRows(state))
		},
		func() fyne.CanvasObject {
			return newQuerySourceListCell()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			cell := obj.(*querySourceListCell)
			configureAutoQuerySourceCell(cell, state, id, func() {
				if err := saveAutoQuerySelectedProfile(state); err != nil {
					if status != nil {
						status.SetText("Auto Q/R source save failed")
					}
					if w != nil {
						dialog.ShowError(err, w)
					}
					return
				}
				refreshArchiveChrome(state)
				refreshQueryDestination(state)
				refreshQueryResultSummary(state)
				if list != nil {
					list.Refresh()
				}
			})
		},
	)
	list.HideSeparators = true
	list.OnSelected = func(id widget.ListItemID) {
		entries := autoQuerySourceEntries(state)
		if state == nil || id < 0 || id >= len(entries) {
			list.Unselect(id)
			return
		}
		state.selectedAutoQuerySourceRow = id
		state.selectedNodeRow = entries[id].nodeIndex
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		list.Refresh()
	}
	return list
}

func newQueryTab(w fyne.Window, status *widget.Label, tables archiveTables, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	quickSearchField := widget.NewSelect(queryQuickSearchOptions, nil)
	quickSearchField.SetSelected(queryQuickSearchPatientName)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder("Search")
	quickSearchStrip := newQueryQuickSearchFieldStrip(quickSearchField, quickSearch)
	patientName := widget.NewEntry()
	patientName.SetPlaceHolder("DOE^JOHN")
	patientID := widget.NewEntry()
	patientID.SetPlaceHolder("12345")
	studyDateFrom := widget.NewEntry()
	studyDateFrom.SetPlaceHolder("20260101")
	studyDateTo := widget.NewEntry()
	studyDateTo.SetPlaceHolder("20261231")
	studyTimeFrom := widget.NewEntry()
	studyTimeFrom.SetPlaceHolder("000000")
	studyTimeTo := widget.NewEntry()
	studyTimeTo.SetPlaceHolder("235959")
	lastHours := widget.NewEntry()
	lastHours.SetText("1")
	lastHours.SetPlaceHolder("6")
	onDate := widget.NewEntry()
	onDate.SetText(time.Now().Format("20060102"))
	onDate.SetPlaceHolder("20260604")
	datePreset := newQueryDatePresetRadioGrid(func(preset string) {
		if queryDatePresetPreservesManualRange(preset) {
			return
		}
		from, to, timeFrom, timeTo, ok := queryDateTimePresetRangeWithInputs(preset, onDate.Text, lastHours.Text, time.Now())
		if !ok {
			return
		}
		studyDateFrom.SetText(from)
		studyDateTo.SetText(to)
		studyTimeFrom.SetText(timeFrom)
		studyTimeTo.SetText(timeTo)
	})
	lastHours.OnSubmitted = func(_ string) {
		if datePreset.Selected() == queryDatePresetLastNHours {
			datePreset.SetSelected(queryDatePresetLastNHours)
		}
	}
	onDate.OnSubmitted = func(_ string) {
		if datePreset.Selected() == queryDatePresetOn {
			datePreset.SetSelected(queryDatePresetOn)
		}
	}
	datePreset.SetSelected(queryDatePresetAny)
	studyDescription := widget.NewEntry()
	studyDescription.SetPlaceHolder("Chest CT")
	modality := widget.NewEntry()
	modality.SetPlaceHolder("CT")
	modalityChecks := newQueryModalityChecks()
	accession := widget.NewEntry()
	accession.SetPlaceHolder("ACC123")
	studyUID := widget.NewEntry()
	studyUID.SetPlaceHolder("1.2.840...")
	seriesUID := widget.NewEntry()
	seriesUID.SetPlaceHolder("1.2.840...")
	sopUID := widget.NewEntry()
	sopUID.SetPlaceHolder("1.2.840...")
	sopClassUID := widget.NewEntry()
	sopClassUID.SetPlaceHolder("1.2.840.10008.5.1.4.1.1.2")
	instanceNumber := widget.NewEntry()
	instanceNumber.SetPlaceHolder("1")
	maxResults := widget.NewEntry()
	maxResults.SetPlaceHolder("0")
	refreshMode := widget.NewSelect(queryRefreshModeOptions, nil)
	refreshMode.SetSelected(queryRefreshModeDont)
	autoRetrieve := newQueryAutoRetrieveCheck(state)
	autoRetrieveSettings := disabledAutoQueryAction(autoQuerySettingsButtonText, theme.SettingsIcon())
	keepOnTop := newQueryKeepOnTopCheck(state)

	var retrieveButton *widget.Button
	syncRetrieveButton := func() {
		syncQueryRetrieveButton(retrieveButton, state)
	}
	syncQuerySelection := func() {
		syncRetrieveButton()
		refreshQuerySelectedDetails(state)
	}
	queryTable := newQueryTable(state, func() {
		retrieveSelectedQuery(w, status, tables, state)
	}, syncQuerySelection)
	runButton := widget.NewButtonWithIcon(queryActionLabelQuery, theme.MediaPlayIcon(), func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{
			PatientName:      patientName.Text,
			PatientID:        patientID.Text,
			StudyDateFrom:    studyDateFrom.Text,
			StudyDateTo:      studyDateTo.Text,
			StudyTimeFrom:    studyTimeFrom.Text,
			StudyTimeTo:      studyTimeTo.Text,
			StudyDescription: studyDescription.Text,
			AccessionNumber:  accession.Text,
			Modality:         queryModalityCriteriaText(modality.Text, modalityChecks),
			StudyInstanceUID: studyUID.Text,
			MaxResults:       max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Query failed")
			dialog.ShowError(fmt.Errorf("unsupported search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastStudyQuery(state, criteria)
		runStudyQuery(w, status, queryTable, state, criteria)
	})
	runPatientButton := widget.NewButtonWithIcon(queryActionLabelPatient, theme.MediaPlayIcon(), func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Patient query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := queryPatientCriteriaWithQuickSearch(query.PatientCriteria{
			PatientName: patientName.Text,
			PatientID:   patientID.Text,
			MaxResults:  max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Patient query failed")
			dialog.ShowError(fmt.Errorf("unsupported Patient Root search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastPatientQuery(state, criteria)
		runPatientQuery(w, status, queryTable, state, criteria)
	})
	runSeriesButton := widget.NewButtonWithIcon(queryActionLabelSeries, theme.MediaPlayIcon(), func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := querySeriesCriteriaWithQuickSearch(query.SeriesCriteria{
			PatientName:       patientName.Text,
			PatientID:         patientID.Text,
			StudyDateFrom:     studyDateFrom.Text,
			StudyDateTo:       studyDateTo.Text,
			StudyInstanceUID:  studyUID.Text,
			SeriesInstanceUID: seriesUID.Text,
			Modality:          queryModalityCriteriaText(modality.Text, modalityChecks),
			SeriesDescription: studyDescription.Text,
			MaxResults:        max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Query failed")
			dialog.ShowError(fmt.Errorf("unsupported Series search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastSeriesQuery(state, criteria)
		runSeriesQuery(w, status, queryTable, state, criteria)
	})
	runImageButton := widget.NewButtonWithIcon(queryActionLabelImages, theme.MediaPlayIcon(), func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := queryImageCriteriaWithQuickSearch(query.ImageCriteria{
			PatientName:       patientName.Text,
			PatientID:         patientID.Text,
			StudyDateFrom:     studyDateFrom.Text,
			StudyDateTo:       studyDateTo.Text,
			StudyInstanceUID:  studyUID.Text,
			SeriesInstanceUID: seriesUID.Text,
			SOPInstanceUID:    sopUID.Text,
			SOPClassUID:       sopClassUID.Text,
			Modality:          queryModalityCriteriaText(modality.Text, modalityChecks),
			InstanceNumber:    instanceNumber.Text,
			MaxResults:        max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Query failed")
			dialog.ShowError(fmt.Errorf("unsupported Image search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastImageQuery(state, criteria)
		runImageQuery(w, status, queryTable, state, criteria)
	})
	refreshButton := newQueryRefreshButton(func() {
		refreshLastQuery(w, status, queryTable, state)
	})
	retrieveButton = widget.NewButtonWithIcon(queryActionLabelRetrieve, theme.DownloadIcon(), func() {
		retrieveSelectedQuery(w, status, tables, state)
	})
	syncQueryRetrieveButton(retrieveButton, state)
	verifyButton := widget.NewButtonWithIcon(queryActionLabelVerify, theme.ConfirmIcon(), func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	submitStudyQuery := func() {
		if runButton != nil && runButton.OnTapped != nil {
			runButton.OnTapped()
		}
	}
	querySearchBar := newQuerySearchBar(quickSearch, submitStudyQuery)
	destinationSelect := newQueryMoveDestinationEntry(state)
	state.queryMoveDestinationSelect = destinationSelect
	state.queryDestinationLabel = widget.NewLabel(queryDestinationText(state))
	state.queryDestinationLabel.Wrapping = fyne.TextTruncate
	state.queryResultSummaryLabel = widget.NewLabel(queryResultSummaryText(state))
	state.queryResultSummaryLabel.Wrapping = fyne.TextTruncate
	state.querySelectedDetailsLabel = compactWorkbenchLabel()
	state.querySelectedDetailsLabel.SetText(querySelectedDetailsText(state))
	selectedDetails := widget.NewAccordion(widget.NewAccordionItem("Selected Result Details", state.querySelectedDetailsLabel))
	selectedDetails.CloseAll()
	sourceList := newQuerySourceList(state)
	refreshSources := func() {
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		nodeTable.Refresh()
	}
	moveSource := func(delta int) {
		changed, err := moveQuerySource(state, delta)
		if err != nil {
			status.SetText("Source order update failed")
			dialog.ShowError(err, w)
			return
		}
		if changed {
			refreshSources()
			status.SetText("Updated source priority")
		}
	}
	moveUpButton := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		moveSource(-1)
	})
	moveDownButton := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
		moveSource(1)
	})
	sourceHeader := newDicomNodesHeader(container.NewHBox(moveUpButton, moveDownButton))
	sourcePanel := container.NewBorder(sourceHeader, nil, nil, nil, sourceList)
	sourcePanel.Resize(fyne.NewSize(250, 0))
	advancedCriteria := newQueryAdvancedCriteria(queryAdvancedCriteriaEntries{
		patientName:      patientName,
		patientID:        patientID,
		studyDateFrom:    studyDateFrom,
		studyDateTo:      studyDateTo,
		studyTimeFrom:    studyTimeFrom,
		studyTimeTo:      studyTimeTo,
		accession:        accession,
		studyUID:         studyUID,
		seriesUID:        seriesUID,
		sopUID:           sopUID,
		sopClassUID:      sopClassUID,
		instanceNumber:   instanceNumber,
		studyDescription: studyDescription,
		modality:         modality,
		maxResults:       maxResults,
		seriesButton:     runSeriesButton,
		imagesButton:     runImageButton,
	})

	criteria := container.NewVBox(
		container.NewCenter(workbenchCenteredTitle(queryWorkspaceTitle)),
		quickSearchStrip,
		querySearchBar,
		container.NewGridWithColumns(2,
			labeledEntry("On Date", onDate),
			labeledEntry("Last Hours", lastHours),
		),
		newQueryDateModalityPanel(datePreset.CanvasObject(), queryModalityGrid(modalityChecks)),
		advancedCriteria,
		container.NewBorder(
			nil,
			nil,
			container.NewHBox(labeledControl("Refresh", refreshMode), labeledControl("Retrieve to", destinationSelect), autoRetrieve, autoRetrieveSettings),
			container.NewHBox(refreshButton, runButton, runPatientButton, retrieveButton, verifyButton),
			state.queryDestinationLabel,
		),
	)
	footer := container.NewBorder(
		nil,
		nil,
		keepOnTop,
		nil,
		container.NewHBox(layout.NewSpacer(), state.queryResultSummaryLabel),
	)
	results := container.NewBorder(nil, selectedDetails, nil, nil, container.NewStack(queryTable))
	return container.NewBorder(criteria, footer, sourcePanel, nil, results)
}

func newQueryDateModalityPanel(datePanel fyne.CanvasObject, modalityPanel fyne.CanvasObject) fyne.CanvasObject {
	return container.NewGridWithColumns(2,
		container.NewVBox(workbenchSectionTitle("Date"), datePanel),
		container.NewVBox(workbenchSectionTitle("Modalities"), modalityPanel),
	)
}

const queryAdvancedCriteriaTitle = "Advanced Criteria"

type queryAdvancedCriteriaEntries struct {
	patientName      *widget.Entry
	patientID        *widget.Entry
	studyDateFrom    *widget.Entry
	studyDateTo      *widget.Entry
	studyTimeFrom    *widget.Entry
	studyTimeTo      *widget.Entry
	accession        *widget.Entry
	studyUID         *widget.Entry
	seriesUID        *widget.Entry
	sopUID           *widget.Entry
	sopClassUID      *widget.Entry
	instanceNumber   *widget.Entry
	studyDescription *widget.Entry
	modality         *widget.Entry
	maxResults       *widget.Entry
	seriesButton     *widget.Button
	imagesButton     *widget.Button
}

func newQueryAdvancedCriteria(entries queryAdvancedCriteriaEntries) *widget.Accordion {
	detail := container.NewVBox(
		container.NewGridWithColumns(4,
			labeledEntry("Patient Name", entries.patientName),
			labeledEntry("Patient ID", entries.patientID),
			labeledEntry("Study Date From", entries.studyDateFrom),
			labeledEntry("Study Date To", entries.studyDateTo),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Study Time From", entries.studyTimeFrom),
			labeledEntry("Study Time To", entries.studyTimeTo),
			labeledEntry("Accession", entries.accession),
			labeledEntry("Max Results", entries.maxResults),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Study UID", entries.studyUID),
			labeledEntry("Series UID", entries.seriesUID),
			labeledEntry("SOP UID", entries.sopUID),
			labeledEntry("SOP Class", entries.sopClassUID),
		),
		container.NewGridWithColumns(3,
			labeledEntry("Instance #", entries.instanceNumber),
			labeledEntry("Description", entries.studyDescription),
			labeledEntry("Modality Manual", entries.modality),
		),
		container.NewHBox(entries.seriesButton, entries.imagesButton),
	)
	advanced := widget.NewAccordion(widget.NewAccordionItem(queryAdvancedCriteriaTitle, detail))
	advanced.CloseAll()
	return advanced
}

func newQuerySourceList(state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(querySourceRows(state))
		},
		func() fyne.CanvasObject {
			return newQuerySourceListCell()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			cell := obj.(*querySourceListCell)
			configureQuerySourceCell(cell, state, id, func() {
				refreshArchiveChrome(state)
				refreshQueryDestination(state)
				refreshQueryResultSummary(state)
				refreshQuerySourceList(state)
			})
		},
	)
	list.HideSeparators = true
	list.OnSelected = func(id widget.ListItemID) {
		if state == nil || id < 0 || id >= len(state.nodes) {
			list.Unselect(id)
			return
		}
		state.selectedNodeRow = id
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
	}
	state.querySourceList = list
	return list
}

func parseOptionalMaxResults(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	max, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid max results %q", value)
	}
	if max < 0 {
		return 0, fmt.Errorf("max results must be >= 0")
	}
	return max, nil
}

func rememberLastStudyQuery(state *uiState, criteria query.Criteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunStudy, study: criteria}
}

func rememberLastPatientQuery(state *uiState, criteria query.PatientCriteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunPatient, patient: criteria}
}

func rememberLastSeriesQuery(state *uiState, criteria query.SeriesCriteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunSeries, series: criteria}
}

func rememberLastImageQuery(state *uiState, criteria query.ImageCriteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunImage, image: criteria}
}

func queryRefreshAvailable(state *uiState) bool {
	return state != nil && state.lastQuery.kind != ""
}

func refreshLastQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	if !queryRefreshAvailable(state) {
		status.SetText("No query to refresh")
		return
	}
	switch state.lastQuery.kind {
	case queryRunStudy:
		runStudyQuery(w, status, table, state, state.lastQuery.study)
	case queryRunPatient:
		runPatientQuery(w, status, table, state, state.lastQuery.patient)
	case queryRunSeries:
		runSeriesQuery(w, status, table, state, state.lastQuery.series)
	case queryRunImage:
		runImageQuery(w, status, table, state, state.lastQuery.image)
	default:
		status.SetText("No query to refresh")
	}
}

type queryMatchesHandler func()
type queryAcrossSourceFunc func(context.Context, nodes.Node) (query.Result, error)

type querySourceFailures struct {
	successes  int
	failures   []string
	failedKeys map[string]bool
}

func (err *querySourceFailures) Error() string {
	return strings.Join(err.failures, "; ")
}

func (err *querySourceFailures) failed(node nodes.Node) bool {
	return err != nil && err.failedKeys[nodeVerifyKey(node)]
}

func runQueryAcrossSources(ctx context.Context, sources []nodes.Node, run queryAcrossSourceFunc) (query.Result, error) {
	var merged query.Result
	var failures []string
	failedKeys := map[string]bool{}
	successes := 0
	for _, source := range sources {
		result, err := run(ctx, source)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", emptyDash(source.Name), err.Error()))
			failedKeys[nodeVerifyKey(source)] = true
			continue
		}
		successes++
		merged.Matches = append(merged.Matches, annotateQueryMatches(result.Matches, source)...)
		merged.FinalStatus = result.FinalStatus
		merged.Duration += result.Duration
	}
	if len(failures) > 0 {
		return merged, &querySourceFailures{successes: successes, failures: failures, failedKeys: failedKeys}
	}
	return merged, nil
}

func querySourcesLabel(sources []nodes.Node) string {
	if len(sources) == 1 {
		return sources[0].Name
	}
	return fmt.Sprintf("%d sources", len(sources))
}

func queryFailureWithoutResults(result query.Result, err error) bool {
	if err == nil {
		return false
	}
	var sourceFailures *querySourceFailures
	if errors.As(err, &sourceFailures) && sourceFailures.successes > 0 {
		return false
	}
	return len(result.Matches) == 0
}

func setQueryMatches(ctx context.Context, status *widget.Label, table *widget.Table, state *uiState, matches []query.Match) error {
	var catalog queryLocalCatalog
	if state != nil && state.catalog != nil {
		catalog = state.catalog
	}
	enriched, err := enrichQueryMatchesWithLocalMetadata(ctx, catalog, matches)
	if err != nil {
		if status != nil {
			status.SetText("Query local metadata lookup failed")
		}
		return err
	}
	if state == nil {
		return nil
	}
	state.queries = enriched
	clearSelectedQuery(state)
	state.collapsedQueryGroups = retainCollapsedQueryGroups(state.collapsedQueryGroups, enriched)
	if table != nil {
		table.Refresh()
	}
	refreshQueryResultSummary(state)
	refreshAutoQueryResultSummary(state)
	return nil
}

func retainCollapsedQueryGroups(collapsed map[string]bool, matches []query.Match) map[string]bool {
	if len(collapsed) == 0 || len(matches) == 0 {
		return nil
	}
	valid := queryGroupKeysForMatches(matches)
	retained := map[string]bool{}
	for key, isCollapsed := range collapsed {
		if isCollapsed && valid[key] {
			retained[key] = true
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

func queryGroupKeysForMatches(matches []query.Match) map[string]bool {
	keys := map[string]bool{}
	patientIndices := queryPatientGroupIndices(matches)
	for key, indices := range patientIndices {
		if queryPatientGroupAvailable(matches, indices) {
			keys[key] = true
		}
	}
	studyIndices := map[string][]int{}
	for i, match := range matches {
		key := queryStudyGroupKey(match)
		if key == "" {
			continue
		}
		studyIndices[key] = append(studyIndices[key], i)
	}
	for key, indices := range studyIndices {
		if _, _, ok := queryStudyGroupMembers(matches, indices); ok {
			keys[key] = true
		}
	}
	return keys
}

func queryCompletionStatus(prefix string, sourceLabel string, result query.Result, err error) string {
	text := fmt.Sprintf("%s %s returned %d matches, final=0x%04X in %s", prefix, sourceLabel, len(result.Matches), result.FinalStatus, result.Duration.Round(time.Millisecond))
	if err != nil {
		text += "; partial failure: " + strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ")
	}
	return text
}

func recordQuerySourceStatuses(state *uiState, sources []nodes.Node, err error) {
	if state == nil {
		return
	}
	var sourceFailures *querySourceFailures
	hasSourceFailures := errors.As(err, &sourceFailures)
	if err != nil && !hasSourceFailures {
		return
	}
	for _, source := range sources {
		if hasSourceFailures && sourceFailures.failed(source) {
			recordQuerySourceStatus(state, source, querySourceFail)
			continue
		}
		recordQuerySourceStatus(state, source, querySourceOK)
	}
}

type queryRetrieveRequest struct {
	match query.Match
	node  nodes.Node
	opts  retrieve.Options
	level string
	label string
}

func retrieveSelectedQuery(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	match, ok := selectedQuery(state)
	if !ok {
		status.SetText("Select a query result to retrieve")
		return
	}
	request, ok := prepareQueryRetrieveRequest(status, state, match)
	if !ok {
		return
	}
	startQueryRetrieve(w, status, tables, state, request)
}

func prepareQueryRetrieveRequest(status *widget.Label, state *uiState, match query.Match) (queryRetrieveRequest, bool) {
	level, label, ok := queryRetrieveLevelAndLabel(match)
	if !ok {
		setStatusIfPresent(status, queryRetrieveValidationMessage(match))
		return queryRetrieveRequest{}, false
	}
	node, ok := nodeForQueryMatch(state, match)
	if !ok {
		setStatusIfPresent(status, "No query-enabled remote nodes configured")
		return queryRetrieveRequest{}, false
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		setStatusIfPresent(status, issue)
		return queryRetrieveRequest{}, false
	}
	match.QueryRetrieveLevel = level
	opts := retrieveOptionsForNode(status, state, node)
	return queryRetrieveRequest{match: match, node: node, opts: opts, level: level, label: label}, true
}

func setStatusIfPresent(status *widget.Label, text string) {
	if status != nil {
		status.SetText(text)
	}
}

func queryRetrieveLevelAndLabel(match query.Match) (string, string, bool) {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return "", "", false
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "" {
		level = "STUDY"
	}
	if level == "IMAGE" || queryUIDAvailable(match.SOPInstanceUID) {
		if !queryUIDAvailable(match.SeriesInstanceUID) || !queryUIDAvailable(match.SOPInstanceUID) {
			return "", "", false
		}
		return "IMAGE", "image " + match.SOPInstanceUID, true
	}
	if level == "SERIES" || queryUIDAvailable(match.SeriesInstanceUID) {
		if !queryUIDAvailable(match.SeriesInstanceUID) {
			return "", "", false
		}
		return "SERIES", "series " + match.SeriesInstanceUID, true
	}
	return "STUDY", "study " + match.StudyInstanceUID, true
}

func queryRetrieveValidationMessage(match query.Match) string {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return "Selected query result has no Study Instance UID"
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "IMAGE" || queryUIDAvailable(match.SOPInstanceUID) {
		if !queryUIDAvailable(match.SeriesInstanceUID) {
			return "Selected query result has no Series Instance UID"
		}
		return "Selected query result has no SOP Instance UID"
	}
	if level == "SERIES" || queryUIDAvailable(match.SeriesInstanceUID) {
		return "Selected query result has no Series Instance UID"
	}
	return "Selected query result cannot be retrieved"
}

func startQueryRetrieve(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, request queryRetrieveRequest) {
	baseCtx, cancel := beginRetrieve(state, request.node.Name)
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	setStatusIfPresent(status, fmt.Sprintf("Retrieving %s from %s", request.label, request.node.Name))
	go func() {
		defer timeoutCancel()
		defer cancel()
		outcome, err := runQueryRetrieveRequest(ctx, state, request)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					setStatusIfPresent(status, "Retrieve cancelled for "+request.node.Name)
					return
				}
				recordQueryRetrieveRowStatus(state, request.match, queryRetrieveFailureRowStatus(request))
				refreshQueryResultSummary(state)
				setStatusIfPresent(status, "Retrieve failed for "+request.node.Name)
				dialog.ShowError(err, w)
				return
			}
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			recordOperation(state, ops.RetrieveSummary(outcome))
			recordQueryRetrieveRowStatus(state, request.match, queryRetrieveSuccessRowStatus(request, outcome))
			refreshQueryResultSummary(state)
			setStatusIfPresent(status, fmt.Sprintf(
				"%s %s: final=0x%04X completed %d failed %d warnings %d stored %d in %s",
				retrieveMethodName(outcome),
				request.node.Name,
				outcome.FinalStatus,
				outcome.Completed,
				outcome.Failed,
				outcome.Warnings,
				outcome.Stored,
				outcome.Duration.Round(time.Millisecond),
			))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func queryRetrieveSuccessRowStatus(request queryRetrieveRequest, outcome retrieve.Outcome) string {
	return fmt.Sprintf(
		"retrieved %s from %s (%s final=0x%04X stored %d failed %d)",
		request.label,
		request.node.Name,
		retrieveMethodName(outcome),
		outcome.FinalStatus,
		outcome.Stored,
		outcome.Failed,
	)
}

func queryRetrieveFailureRowStatus(request queryRetrieveRequest) string {
	return fmt.Sprintf("retrieve failed for %s from %s", request.label, request.node.Name)
}

func runQueryRetrieveRequest(ctx context.Context, state *uiState, request queryRetrieveRequest) (retrieve.Outcome, error) {
	switch request.level {
	case "IMAGE":
		return retrieve.RetrieveImage(ctx, state.catalog, request.node, request.match.StudyInstanceUID, request.match.SeriesInstanceUID, request.match.SOPInstanceUID, request.opts)
	case "SERIES":
		return retrieve.RetrieveSeries(ctx, state.catalog, request.node, request.match.StudyInstanceUID, request.match.SeriesInstanceUID, request.opts)
	default:
		return retrieve.RetrieveStudy(ctx, state.catalog, request.node, request.match.StudyInstanceUID, request.opts)
	}
}

func runAutoQueryAutoRetrieve(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, candidates []query.Match) {
	if len(candidates) == 0 {
		setStatusIfPresent(status, "No Auto Q/R retrieve candidates")
		return
	}
	requests := make([]queryRetrieveRequest, 0, len(candidates))
	for _, candidate := range candidates {
		request, ok := prepareQueryRetrieveRequest(status, state, candidate)
		if !ok {
			return
		}
		requests = append(requests, request)
	}
	baseCtx, cancel := beginRetrieve(state, "Auto Q/R")
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	setStatusIfPresent(status, fmt.Sprintf("Auto Q/R retrieving %d matches", len(requests)))
	go func() {
		defer timeoutCancel()
		defer cancel()
		var outcomes []retrieve.Outcome
		for i, request := range requests {
			index := i + 1
			fyne.Do(func() {
				setStatusIfPresent(status, fmt.Sprintf("Auto Q/R retrieving %d/%d %s from %s", index, len(requests), request.label, request.node.Name))
			})
			outcome, err := runQueryRetrieveRequest(ctx, state, request)
			if err != nil {
				fyne.Do(func() {
					clearActiveRetrieve(state)
					if errors.Is(err, context.Canceled) {
						setStatusIfPresent(status, "Auto Q/R retrieve cancelled")
						return
					}
					setStatusIfPresent(status, "Auto Q/R retrieve failed for "+request.node.Name)
					dialog.ShowError(err, w)
				})
				return
			}
			outcomes = append(outcomes, outcome)
		}
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			for _, outcome := range outcomes {
				recordOperation(state, ops.RetrieveSummary(outcome))
			}
			setStatusIfPresent(status, autoQueryRetrieveBatchStatus(outcomes))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func autoQueryRetrieveBatchStatus(outcomes []retrieve.Outcome) string {
	var completed, failed, warnings int
	var stored int64
	var duration time.Duration
	for _, outcome := range outcomes {
		completed += int(outcome.Completed)
		failed += int(outcome.Failed)
		warnings += int(outcome.Warnings)
		stored += outcome.Stored
		duration += outcome.Duration
	}
	return fmt.Sprintf(
		"Auto Q/R retrieve completed %d requests: completed %d failed %d warnings %d stored %d in %s",
		len(outcomes),
		completed,
		failed,
		warnings,
		stored,
		duration.Round(time.Millisecond),
	)
}

func runPatientQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.PatientCriteria) {
	runPatientQueryWithSources(w, status, table, state, criteria, querySourceNodes(state), nil)
}

func runPatientQueryWithSources(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.PatientCriteria, sources []nodes.Node, afterMatches queryMatchesHandler) {
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Patient C-FIND "+sourceLabel)
	status.SetText("Querying patients on " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.PatientRootFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Patient query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Patient C-FIND", sourceLabel, result, err))
			if afterMatches != nil {
				afterMatches()
			}
		})
	}()
}

func runStudyQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.Criteria) {
	runStudyQueryWithSources(w, status, table, state, criteria, querySourceNodes(state), nil)
}

func runStudyQueryWithSources(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.Criteria, sources []nodes.Node, afterMatches queryMatchesHandler) {
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Study C-FIND "+sourceLabel)
	status.SetText("Querying " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("C-FIND", sourceLabel, result, err))
			if afterMatches != nil {
				afterMatches()
			}
		})
	}()
}

func runSeriesQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.SeriesCriteria) {
	sources := querySourceNodes(state)
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Series C-FIND "+sourceLabel)
	status.SetText("Querying series on " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootSeriesFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Series query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Series C-FIND", sourceLabel, result, err))
		})
	}()
}

func runImageQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.ImageCriteria) {
	sources := querySourceNodes(state)
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Image C-FIND "+sourceLabel)
	status.SetText("Querying images on " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootImageFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Image query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Image C-FIND", sourceLabel, result, err))
		})
	}()
}

func selectedQuery(state *uiState) (query.Match, bool) {
	if state == nil {
		return query.Match{}, false
	}
	if state.selectedQueryVirtual {
		return state.selectedQueryVirtualMatch, true
	}
	if len(state.queries) == 0 {
		return query.Match{}, false
	}
	row := state.selectedQueryRow
	if row < 0 || row >= len(state.queries) {
		return query.Match{}, false
	}
	return state.queries[row], true
}

const (
	queryTableColumnPatient = iota
	queryRetrieveColumn
	queryTableColumnLocalState
	queryTableColumnModality
	queryTableColumnImages
	queryTableColumnStudyDate
	queryTableColumnTime
	queryTableColumnDescription
	queryTableColumnPatientID
	queryTableColumnDOB
	queryTableColumnLocalComments
	queryTableColumnServerComments
	queryTableColumnAccession
	queryTableColumnReferrer
	queryTableColumnInstitution
	queryTableColumnStudyStatus
	queryTableColumnSeriesNumber
	queryTableColumnInstanceNumber
	queryTableColumnSource
	queryTableColumnStatus
	queryTableColumnLevel
	queryTableColumnStudyUID
	queryTableColumnSeriesUID
	queryTableColumnSOPClass
	queryTableColumnSOPUID
)

type queryTableRowKind int

const (
	queryTableRowMatch queryTableRowKind = iota
	queryTableRowStudyGroup
	queryTableRowPatientGroup
)

type queryTableRow struct {
	kind       queryTableRowKind
	key        string
	match      query.Match
	queryIndex int
	depth      int
	expanded   bool
	childCount int
}

func newQueryTable(state *uiState, onRetrieve func(), onSelectionChanged ...func()) *widget.Table {
	headers := queryTableHeaders()
	var table *widget.Table
	notifySelectionChanged := func() {
		for _, callback := range onSelectionChanged {
			if callback != nil {
				callback()
			}
		}
	}
	selectQueryCell := func(id widget.TableCellID) {
		if id.Row == 0 {
			if applyQuerySort(state, id.Col) && table != nil {
				table.Refresh()
			}
			return
		}
		rows := queryTableRows(state)
		rowIndex, retrieve, ok := queryTableSelectionAction(id)
		if !ok || rowIndex < 0 || rowIndex >= len(rows) {
			return
		}
		row := rows[rowIndex]
		if retrieve && queryRowCanRetrieve(row) {
			selectQueryRow(state, row)
			if table != nil {
				table.Refresh()
			}
			notifySelectionChanged()
			if onRetrieve != nil {
				onRetrieve()
			}
			return
		}
		if toggleQueryGroupRow(state, row) {
			clearSelectedQuery(state)
			if table != nil {
				table.Refresh()
			}
			notifySelectionChanged()
			return
		}
		if row.queryIndex < 0 || row.queryIndex >= len(state.queries) {
			return
		}
		selectQueryRow(state, row)
		if table != nil {
			table.Refresh()
		}
		notifySelectionChanged()
	}
	table = widget.NewTable(
		func() (int, int) {
			return len(queryTableRows(state)) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newQueryTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*queryTableCell)
			if id.Row == 0 {
				applyQueryTableCell(cell, id.Row, id.Col, queryHeaderLabel(state, id.Col, headers[id.Col]), true, false, false, 0, "")
				return
			}
			rows := queryTableRows(state)
			if id.Row-1 < 0 || id.Row-1 >= len(rows) {
				applyQueryTableCell(cell, id.Row, id.Col, "", false, false, false, 0, "")
				return
			}
			row := rows[id.Row-1]
			match := row.match
			selected := queryRowSelected(state, row)
			retrieveAction := id.Col == queryRetrieveColumn && queryRowCanRetrieve(row)
			text := queryRowCell(row, id.Col)
			localState := match.LocalState
			if id.Col == queryTableColumnLocalState {
				text, localState = queryRowLocalStateCell(state, row)
			}
			applyQueryTableCell(cell, id.Row, id.Col, text, false, selected, retrieveAction, match.Status, localState)
			if retrieveAction {
				cellID := id
				cell.retrieveButton.OnTapped = func() {
					selectQueryCell(cellID)
				}
			}
		},
	)
	clearSelectedQuery(state)
	table.OnSelected = selectQueryCell
	widths := []float32{260, 70, 65, 85, 80, 100, 85, 240, 120, 95, 180, 180, 120, 180, 180, 110, 85, 90, 220, 80}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	applyCompactTableRows(table)
	return table
}

func syncQueryRetrieveButton(button *widget.Button, state *uiState) {
	if button == nil {
		return
	}
	if queryRetrieveActionAvailable(state) {
		button.Enable()
		return
	}
	button.Disable()
}

func queryRetrieveActionAvailable(state *uiState) bool {
	match, ok := selectedQuery(state)
	return ok && queryMatchCanRetrieve(match)
}

func clearSelectedQuery(state *uiState) {
	if state == nil {
		return
	}
	state.selectedQueryRow = -1
	state.selectedQueryVirtual = false
	state.selectedQueryVirtualMatch = query.Match{}
}

func selectQueryRow(state *uiState, row queryTableRow) {
	if state == nil {
		return
	}
	state.selectedQueryRow = row.queryIndex
	if row.kind == queryTableRowStudyGroup {
		state.selectedQueryVirtual = true
		state.selectedQueryVirtualMatch = row.match
		return
	}
	state.selectedQueryVirtual = false
	state.selectedQueryVirtualMatch = query.Match{}
}

func queryRowSelected(state *uiState, row queryTableRow) bool {
	if state == nil {
		return false
	}
	if row.kind == queryTableRowStudyGroup && state.selectedQueryVirtual {
		return autoQueryRetrieveCandidateKey(row.match) == autoQueryRetrieveCandidateKey(state.selectedQueryVirtualMatch)
	}
	return row.kind == queryTableRowMatch && !state.selectedQueryVirtual && row.queryIndex == state.selectedQueryRow
}

func queryRowCanRetrieve(row queryTableRow) bool {
	if row.kind == queryTableRowPatientGroup {
		return false
	}
	return queryMatchCanRetrieve(row.match)
}

func queryTableRows(state *uiState) []queryTableRow {
	if state == nil || len(state.queries) == 0 {
		return nil
	}
	patientIndices := queryPatientGroupIndices(state.queries)
	patientGroupKeys := queryPatientGroupKeys(state.queries, patientIndices)
	processed := map[int]bool{}
	var rows []queryTableRow
	for i, match := range state.queries {
		if processed[i] {
			continue
		}
		patientKey := queryPatientGroupKey(match)
		if patientKey != "" && patientGroupKeys[patientKey] {
			groupIndices := patientIndices[patientKey]
			for _, index := range groupIndices {
				processed[index] = true
			}
			expanded := !state.collapsedQueryGroups[patientKey]
			parent := queryPatientGroupMatch(state.queries[groupIndices[0]])
			parent = queryApplyGroupLocalState(parent, state, groupIndices)
			rows = append(rows, queryTableRow{
				kind:       queryTableRowPatientGroup,
				key:        patientKey,
				match:      parent,
				queryIndex: groupIndices[0],
				expanded:   expanded,
				childCount: len(groupIndices),
			})
			if expanded {
				rows = append(rows, queryTableRowsForIndices(state, groupIndices, 1)...)
			}
			continue
		}
		indices := queryUngroupedStudyIndices(state.queries, i, processed, patientGroupKeys)
		for _, index := range indices {
			processed[index] = true
		}
		rows = append(rows, queryTableRowsForIndices(state, indices, 0)...)
	}
	return rows
}

func queryTableRowsForIndices(state *uiState, indices []int, depth int) []queryTableRow {
	indicesByStudy := map[string][]int{}
	for _, i := range indices {
		match := state.queries[i]
		key := queryStudyGroupKey(match)
		if key == "" {
			continue
		}
		indicesByStudy[key] = append(indicesByStudy[key], i)
	}
	processed := map[int]bool{}
	var rows []queryTableRow
	for _, i := range indices {
		match := state.queries[i]
		if processed[i] {
			continue
		}
		key := queryStudyGroupKey(match)
		groupIndices := indicesByStudy[key]
		parentIndex, childIndices, ok := queryStudyGroupMembers(state.queries, groupIndices)
		if key != "" && ok {
			for _, index := range groupIndices {
				processed[index] = true
			}
			expanded := !state.collapsedQueryGroups[key]
			parent := queryStudyGroupMatch(state.queries[parentIndex])
			parent = queryApplyGroupLocalState(parent, state, groupIndices)
			rows = append(rows, queryTableRow{
				kind:       queryTableRowStudyGroup,
				key:        key,
				match:      parent,
				queryIndex: parentIndex,
				depth:      depth,
				expanded:   expanded,
				childCount: len(childIndices),
			})
			if expanded {
				for _, childIndex := range childIndices {
					rows = append(rows, queryTableRow{
						kind:       queryTableRowMatch,
						key:        key,
						match:      state.queries[childIndex],
						queryIndex: childIndex,
						depth:      depth + 1,
					})
				}
			}
			continue
		}
		rows = append(rows, queryTableRow{kind: queryTableRowMatch, match: match, queryIndex: i, depth: depth})
		processed[i] = true
	}
	return rows
}

func queryApplyGroupLocalState(parent query.Match, state *uiState, indices []int) query.Match {
	if state == nil {
		return parent
	}
	matches := state.queries
	retrieveState := ""
	for _, index := range indices {
		if index < 0 || index >= len(matches) {
			continue
		}
		match := matches[index]
		status := queryRetrieveRowStatus(state, match)
		if strings.HasPrefix(status, "retrieve failed ") {
			retrieveState = queryLocalStateRetrieveFailed
			break
		}
		if retrieveState == "" && strings.HasPrefix(status, "retrieved ") {
			retrieveState = queryLocalStateRetrieved
		}
	}
	if retrieveState != "" {
		parent.LocalState = retrieveState
	} else {
		for _, index := range indices {
			if index < 0 || index >= len(matches) {
				continue
			}
			match := matches[index]
			if queryLocalStateAvailable(parent.LocalState) {
				break
			}
			if queryLocalStateAvailable(match.LocalState) {
				parent.LocalState = match.LocalState
			}
		}
	}
	for _, index := range indices {
		if index < 0 || index >= len(matches) {
			continue
		}
		if strings.TrimSpace(parent.LocalComments) != "" {
			break
		}
		if comments := strings.TrimSpace(matches[index].LocalComments); comments != "" {
			parent.LocalComments = comments
		}
	}
	return parent
}

func queryRowLocalStateText(localState string) string {
	switch strings.TrimSpace(localState) {
	case queryLocalStatePresent:
		return "Duplicate"
	case queryLocalStateRetrieved:
		return "Retrieved"
	case queryLocalStateRetrieveFailed:
		return "Failed"
	default:
		return ""
	}
}

func queryPatientGroupIndices(matches []query.Match) map[string][]int {
	out := map[string][]int{}
	for i, match := range matches {
		key := queryPatientGroupKey(match)
		if key == "" {
			continue
		}
		out[key] = append(out[key], i)
	}
	return out
}

func queryPatientGroupKeys(matches []query.Match, indicesByPatient map[string][]int) map[string]bool {
	out := map[string]bool{}
	for key, indices := range indicesByPatient {
		if queryPatientGroupAvailable(matches, indices) {
			out[key] = true
		}
	}
	return out
}

func queryPatientGroupAvailable(matches []query.Match, indices []int) bool {
	if len(indices) < 2 {
		return false
	}
	studies := map[string]bool{}
	for _, index := range indices {
		studyUID := strings.TrimSpace(matches[index].StudyInstanceUID)
		if queryUIDAvailable(studyUID) {
			studies[studyUID] = true
		}
	}
	return len(studies) > 1
}

func queryPatientGroupKey(match query.Match) string {
	if patientID := strings.TrimSpace(match.PatientID); patientID != "" {
		return "patient-id:" + strings.ToLower(patientID)
	}
	if patientName := strings.TrimSpace(match.PatientName); patientName != "" {
		return "patient-name:" + strings.ToLower(patientName)
	}
	return ""
}

func queryPatientGroupMatch(match query.Match) query.Match {
	match.QueryRetrieveLevel = "PATIENT"
	match.StudyInstanceUID = ""
	match.StudyDate = ""
	match.StudyTime = ""
	match.ImageCount = ""
	match.StudyDescription = ""
	match.AccessionNumber = ""
	match.ReferringPhysicianName = ""
	match.InstitutionName = ""
	match.StudyStatusID = ""
	match.SeriesInstanceUID = ""
	match.Modality = ""
	match.Modalities = ""
	match.SeriesNumber = ""
	match.SeriesDescription = ""
	match.SOPClassUID = ""
	match.SOPInstanceUID = ""
	match.InstanceNumber = ""
	return match
}

func queryUngroupedStudyIndices(matches []query.Match, current int, processed map[int]bool, patientGroupKeys map[string]bool) []int {
	studyKey := queryStudyGroupKey(matches[current])
	if studyKey == "" {
		return []int{current}
	}
	var indices []int
	for i, match := range matches {
		if processed[i] {
			continue
		}
		if patientKey := queryPatientGroupKey(match); patientKey != "" && patientGroupKeys[patientKey] {
			continue
		}
		if queryStudyGroupKey(match) == studyKey {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return []int{current}
	}
	return indices
}

func queryStudyGroupKey(match query.Match) string {
	studyUID := strings.TrimSpace(match.StudyInstanceUID)
	if !queryUIDAvailable(studyUID) {
		return ""
	}
	return "study:" + studyUID
}

func queryStudyGroupMembers(matches []query.Match, indices []int) (int, []int, bool) {
	if len(indices) < 2 {
		return -1, nil, false
	}
	parentIndex := indices[0]
	childIndices := make([]int, 0, len(indices))
	for _, index := range indices {
		match := matches[index]
		if queryMatchIsSeriesOrImage(match) {
			childIndices = append(childIndices, index)
			continue
		}
		if parentIndex == indices[0] || queryMatchIsSeriesOrImage(matches[parentIndex]) {
			parentIndex = index
		}
	}
	if len(childIndices) < 2 && (parentIndex < 0 || queryMatchIsSeriesOrImage(matches[parentIndex])) {
		return -1, nil, false
	}
	if len(childIndices) == 0 {
		return -1, nil, false
	}
	return parentIndex, childIndices, true
}

func queryMatchIsSeriesOrImage(match query.Match) bool {
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	return level == "SERIES" || level == "IMAGE" || queryUIDAvailable(match.SeriesInstanceUID) || queryUIDAvailable(match.SOPInstanceUID)
}

func queryStudyGroupMatch(match query.Match) query.Match {
	match.QueryRetrieveLevel = "STUDY"
	match.SeriesInstanceUID = ""
	match.SeriesNumber = ""
	match.SeriesDescription = ""
	match.SOPClassUID = ""
	match.SOPInstanceUID = ""
	match.InstanceNumber = ""
	return match
}

func toggleQueryGroupRow(state *uiState, row queryTableRow) bool {
	if state == nil || (row.kind != queryTableRowStudyGroup && row.kind != queryTableRowPatientGroup) || row.key == "" {
		return false
	}
	if state.collapsedQueryGroups == nil {
		state.collapsedQueryGroups = map[string]bool{}
	}
	if state.collapsedQueryGroups[row.key] {
		delete(state.collapsedQueryGroups, row.key)
	} else {
		state.collapsedQueryGroups[row.key] = true
	}
	return true
}

func queryRowCell(row queryTableRow, col int) string {
	if row.kind == queryTableRowPatientGroup && col == queryTableColumnPatient {
		label := strings.TrimSpace(row.match.PatientName)
		if label == "" {
			label = strings.TrimSpace(row.match.PatientID)
		}
		if label == "" {
			label = "PATIENT"
		}
		if row.expanded {
			return "▾ " + label
		}
		return "▸ " + label
	}
	if row.kind == queryTableRowStudyGroup && col == queryTableColumnPatient {
		label := strings.TrimSpace(row.match.StudyDescription)
		if label == "" {
			label = strings.TrimSpace(row.match.StudyDate)
		}
		if label == "" {
			label = strings.TrimSpace(row.match.StudyInstanceUID)
		}
		if label == "" {
			label = "STUDY"
		}
		indent := strings.Repeat("    ", row.depth)
		if row.expanded {
			return indent + "▾ " + label
		}
		return indent + "▸ " + label
	}
	if row.kind == queryTableRowPatientGroup && col == queryTableColumnLevel {
		if row.expanded {
			return "▾ PATIENT"
		}
		return "▸ PATIENT"
	}
	if row.kind == queryTableRowStudyGroup && col == queryTableColumnLevel {
		indent := strings.Repeat("  ", row.depth)
		if row.expanded {
			return indent + "▾ STUDY"
		}
		return indent + "▸ STUDY"
	}
	if row.depth > 0 && col == queryTableColumnPatient {
		if value := strings.TrimSpace(row.match.SeriesNumber); value != "" {
			return strings.Repeat("    ", row.depth) + value
		}
		if value := strings.TrimSpace(row.match.SeriesDescription); value != "" {
			return strings.Repeat("    ", row.depth) + value
		}
	}
	return queryCell(row.match, col)
}

func queryRowLocalStateCell(state *uiState, row queryTableRow) (string, string) {
	if status := queryRetrieveRowStatus(state, row.match); status != "" {
		if strings.HasPrefix(status, "retrieved ") {
			return "Retrieved", queryLocalStateRetrieved
		}
		if strings.HasPrefix(status, "retrieve failed ") {
			return "Failed", queryLocalStateRetrieveFailed
		}
	}
	return queryCell(row.match, queryTableColumnLocalState), row.match.LocalState
}

func applyQuerySort(state *uiState, col int) bool {
	if state == nil || !queryColumnSortable(col) {
		return false
	}
	if state.querySortActive && state.querySortColumn == col {
		state.querySortDescending = !state.querySortDescending
	} else {
		state.querySortActive = true
		state.querySortColumn = col
		state.querySortDescending = false
	}
	descending := state.querySortDescending
	sort.SliceStable(state.queries, func(i, j int) bool {
		left := querySortValue(state.queries[i], col)
		right := querySortValue(state.queries[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.queries[i].StudyInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.queries[j].StudyInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
	clearSelectedQuery(state)
	return true
}

func queryColumnSortable(col int) bool {
	switch col {
	case queryRetrieveColumn, queryTableColumnLocalState:
		return false
	default:
		return col >= 0 && col < len(queryTableHeaders())
	}
}

func querySortValue(match query.Match, col int) string {
	return strings.ToLower(strings.TrimSpace(queryCell(match, col)))
}

func queryHeaderLabel(state *uiState, col int, label string) string {
	if state == nil || !state.querySortActive || state.querySortColumn != col {
		return label
	}
	if state.querySortDescending {
		return label + " ▼"
	}
	return label + " ▲"
}

func queryTableSelectionAction(id widget.TableCellID) (int, bool, bool) {
	if id.Row <= 0 {
		return -1, false, false
	}
	return id.Row - 1, id.Col == queryRetrieveColumn, true
}

func queryMatchCanRetrieve(match query.Match) bool {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return false
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "IMAGE" || queryUIDAvailable(match.SOPInstanceUID) {
		return queryUIDAvailable(match.SeriesInstanceUID) && queryUIDAvailable(match.SOPInstanceUID)
	}
	if level == "SERIES" || queryUIDAvailable(match.SeriesInstanceUID) {
		return queryUIDAvailable(match.SeriesInstanceUID)
	}
	return true
}

func queryUIDAvailable(uid string) bool {
	uid = strings.TrimSpace(uid)
	return uid != "" && uid != "(missing)"
}

func queryTableHeaders() []string {
	return []string{"Patient", "Retrieve", "Local", "Modality", "Images", "Study Date", "Time", "Description", "Patient ID", "DOB", "Local Comments", "Server Comments", "Accession", "Referrer", "Institution", "Study Status", "Series #", "Instance #", "Source", "Status"}
}

func applyQueryTableCell(cell *queryTableCell, tableRow int, tableCol int, text string, header bool, selected bool, retrieveAction bool, status uint16, localState string) {
	if cell == nil {
		return
	}
	cell.retrieveButton.OnTapped = nil
	cell.retrieveButton.Hide()
	cell.statusDotBox.Hide()
	cell.label.Show()
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{}
	cell.label.Alignment = fyne.TextAlignLeading
	cell.label.Wrapping = fyne.TextTruncate
	if header {
		cell.label.TextStyle = fyne.TextStyle{Bold: true}
		cell.background.FillColor = archiveHeaderRowColor
		cell.background.Refresh()
		return
	}
	if retrieveAction {
		cell.label.SetText("")
		cell.label.Hide()
		cell.retrieveButton.Show()
	}
	if tableCol == queryTableColumnStatus {
		cell.statusDot.FillColor = queryStatusDotColor(status)
		cell.statusDot.Refresh()
		cell.statusDotBox.Show()
	}
	if tableCol == queryTableColumnLocalState && queryLocalStateAvailable(localState) {
		cell.statusDot.FillColor = queryLocalStateDotColor(localState)
		cell.statusDot.Refresh()
		cell.statusDotBox.Show()
	}
	if selected {
		cell.background.FillColor = archiveSelectedRowColor
		cell.background.Refresh()
		return
	}
	if retrieveAction {
		cell.background.FillColor = queryRetrieveActionRowColor
		cell.background.Refresh()
		return
	}
	if tableRow%2 == 0 {
		cell.background.FillColor = archiveEvenRowColor
	} else {
		cell.background.FillColor = archiveOddRowColor
	}
	cell.background.Refresh()
}

func queryLocalStateAvailable(localState string) bool {
	switch strings.TrimSpace(localState) {
	case queryLocalStatePresent, queryLocalStateRetrieved, queryLocalStateRetrieveFailed:
		return true
	default:
		return false
	}
}

func queryLocalStateDotColor(localState string) color.NRGBA {
	switch strings.TrimSpace(localState) {
	case queryLocalStatePresent, queryLocalStateRetrieved:
		return queryLocalStatePresentColor
	case queryLocalStateRetrieveFailed:
		return queryStatusFailColor
	default:
		return sourceStatusIdleColor
	}
}

func toggleNodeOperationalCell(state *uiState, row int, col int) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	switch col {
	case nodeTableColumnEnabled:
		return setNodeOperationalCheck(state, row, col, !state.nodes[row].Enabled())
	case nodeTableColumnQuery:
		return setNodeOperationalCheck(state, row, col, !state.nodes[row].QueryEnabled())
	case nodeTableColumnSend:
		return setNodeOperationalCheck(state, row, col, !state.nodes[row].SendEnabled())
	}
	next := state.nodes[row]
	switch col {
	case nodeTableColumnRetrieve:
		next.RetrieveMethod = nextRetrieveMethod(next.RetrieveMethod)
	default:
		return false, nil
	}
	return saveNodeAt(state, row, next)
}

func setNodeOperationalCheck(state *uiState, row int, col int, checked bool) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	switch col {
	case nodeTableColumnEnabled:
		next.Disabled = !checked
	case nodeTableColumnQuery:
		next.QueryDisabled = !checked
	case nodeTableColumnSend:
		next.SendDisabled = !checked
	default:
		return false, nil
	}
	return saveNodeAt(state, row, next)
}

func setNodeRetrieveMethod(state *uiState, row int, method string) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	normalized, err := nodes.NormalizeRetrieveMethod(method)
	if err != nil {
		return false, err
	}
	next := state.nodes[row]
	next.RetrieveMethod = normalized
	return saveNodeAt(state, row, next)
}

func saveNodeAt(state *uiState, row int, next nodes.Node) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	if next == state.nodes[row] {
		return false, nil
	}
	original := state.nodes[row]
	state.nodes[row] = next
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes[row] = original
			return true, err
		}
	}
	return true, nil
}

func nextRetrieveMethod(current string) string {
	method, err := nodes.NormalizeRetrieveMethod(current)
	if err != nil {
		return ""
	}
	switch method {
	case "":
		return nodes.RetrieveMethodMove
	case nodes.RetrieveMethodMove:
		return nodes.RetrieveMethodGet
	default:
		return ""
	}
}

type nodeTableCell struct {
	*fyne.Container
	background     *canvas.Rectangle
	label          *widget.Label
	check          *widget.Check
	retrieveSelect *widget.Select
}

func newNodeTableCell() *nodeTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	check := widget.NewCheck("", nil)
	check.Hide()
	retrieveSelect := widget.NewSelect(retrieveMethodOptions(), nil)
	retrieveSelect.Hide()
	return &nodeTableCell{
		Container:      container.NewStack(background, newCompactTableCellContent(label), container.NewCenter(check), newCompactTableCellContent(retrieveSelect), newTableColumnDividerLayer()),
		background:     background,
		label:          label,
		check:          check,
		retrieveSelect: retrieveSelect,
	}
}

func newNodeTable(status *widget.Label, state *uiState) *widget.Table {
	headers := nodeTableHeaders()
	var table *widget.Table
	refreshAfterNodeChange := func(row int, changed bool, err error) {
		if err != nil && status != nil {
			status.SetText("Node update failed")
		} else if changed && status != nil {
			status.SetText("Updated node " + state.nodes[row].Name)
		}
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if table != nil {
			table.Refresh()
		}
	}
	selectNodeCell := func(id widget.TableCellID) {
		if id.Row <= 0 {
			if applyNodeSort(state, id.Col) && table != nil {
				table.Refresh()
			}
			return
		}
		row, ok := nodeTableNodeIndex(state, id.Row-1)
		if !ok {
			return
		}
		state.selectedNodeRow = row
		changed, err := toggleNodeOperationalCell(state, row, id.Col)
		refreshAfterNodeChange(row, changed, err)
	}
	table = widget.NewTable(
		func() (int, int) {
			return len(state.nodes) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newNodeTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*nodeTableCell)
			if id.Row == 0 {
				applyNodeTableCell(cell, id.Row, id.Col, nodeHeaderLabel(state, id.Col, headers[id.Col]), true, false, false, false, false, "")
				return
			}
			nodeIndex, ok := nodeTableNodeIndex(state, id.Row-1)
			if !ok {
				applyNodeTableCell(cell, id.Row, id.Col, "", false, false, false, false, false, "")
				return
			}
			node := state.nodes[nodeIndex]
			selected := nodeIndex == state.selectedNodeRow
			checkbox, checked := nodeOperationalCheckboxState(node, id.Col)
			retrieveDropdown, retrieveValue := nodeRetrieveMethodDropdownState(node, id.Col)
			applyNodeTableCell(cell, id.Row, id.Col, nodeCell(node, id.Col), false, selected, checkbox, checked, retrieveDropdown, retrieveValue)
			if checkbox {
				cellID := id
				cell.check.OnChanged = func(checked bool) {
					row, ok := nodeTableNodeIndex(state, cellID.Row-1)
					if !ok {
						return
					}
					state.selectedNodeRow = row
					changed, err := setNodeOperationalCheck(state, row, cellID.Col, checked)
					refreshAfterNodeChange(row, changed, err)
				}
			}
			if retrieveDropdown {
				cellID := id
				cell.retrieveSelect.OnChanged = func(method string) {
					row, ok := nodeTableNodeIndex(state, cellID.Row-1)
					if !ok {
						return
					}
					state.selectedNodeRow = row
					changed, err := setNodeRetrieveMethod(state, row, method)
					refreshAfterNodeChange(row, changed, err)
				}
			}
		},
	)
	state.selectedNodeRow = -1
	table.OnSelected = selectNodeCell
	widths := []float32{70, 70, 90, 70, 70, 140, 110, 190, 70, 150, 120, 360}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	applyCompactTableRows(table)
	return table
}

const (
	nodeTableColumnEnabled = iota
	nodeTableColumnQuery
	nodeTableColumnRetrieve
	nodeTableColumnSend
	nodeTableColumnTLS
	nodeTableColumnName
	nodeTableColumnAETitle
	nodeTableColumnHost
	nodeTableColumnPort
	nodeTableColumnMoveDestination
	nodeTableColumnSendSyntax
	nodeTableColumnNotes
)

func applyNodeSort(state *uiState, col int) bool {
	if state == nil || !nodeColumnSortable(col) {
		return false
	}
	if state.nodeSortActive && state.nodeSortColumn == col {
		state.nodeSortDescending = !state.nodeSortDescending
	} else {
		state.nodeSortActive = true
		state.nodeSortColumn = col
		state.nodeSortDescending = false
	}
	descending := state.nodeSortDescending
	state.nodeTableRows = makeNodeTableRows(len(state.nodes))
	sortNodeTableRows(state, col, descending)
	state.selectedNodeRow = -1
	return true
}

func refreshNodeTableRows(state *uiState) {
	if state == nil {
		return
	}
	state.nodeTableRows = makeNodeTableRows(len(state.nodes))
	if state.nodeSortActive && nodeColumnSortable(state.nodeSortColumn) {
		sortNodeTableRows(state, state.nodeSortColumn, state.nodeSortDescending)
	}
}

func makeNodeTableRows(count int) []int {
	if count <= 0 {
		return nil
	}
	rows := make([]int, count)
	for i := range rows {
		rows[i] = i
	}
	return rows
}

func ensureNodeTableRows(state *uiState) []int {
	if state == nil {
		return nil
	}
	if !nodeTableRowsValid(state) {
		refreshNodeTableRows(state)
	}
	return state.nodeTableRows
}

func nodeTableRowsValid(state *uiState) bool {
	if state == nil || len(state.nodeTableRows) != len(state.nodes) {
		return false
	}
	seen := make([]bool, len(state.nodes))
	for _, index := range state.nodeTableRows {
		if index < 0 || index >= len(state.nodes) || seen[index] {
			return false
		}
		seen[index] = true
	}
	return true
}

func nodeTableNodeIndex(state *uiState, row int) (int, bool) {
	rows := ensureNodeTableRows(state)
	if row < 0 || row >= len(rows) {
		return -1, false
	}
	index := rows[row]
	return index, index >= 0 && index < len(state.nodes)
}

func sortNodeTableRows(state *uiState, col int, descending bool) {
	if state == nil || len(state.nodes) == 0 {
		return
	}
	if !nodeTableRowsValid(state) {
		state.nodeTableRows = makeNodeTableRows(len(state.nodes))
	}
	sort.SliceStable(state.nodeTableRows, func(i, j int) bool {
		leftIndex := state.nodeTableRows[i]
		rightIndex := state.nodeTableRows[j]
		left := nodeSortValue(state.nodes[leftIndex], col)
		right := nodeSortValue(state.nodes[rightIndex], col)
		if left == right {
			left = nodeSortTieValue(state.nodes[leftIndex])
			right = nodeSortTieValue(state.nodes[rightIndex])
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func nodeColumnSortable(col int) bool {
	switch col {
	case nodeTableColumnEnabled, nodeTableColumnQuery, nodeTableColumnRetrieve, nodeTableColumnSend:
		return false
	default:
		return col >= 0 && col < len(nodeTableHeaders())
	}
}

func nodeSortValue(node nodes.Node, col int) string {
	if col == nodeTableColumnPort {
		return fmt.Sprintf("%05d", node.Port)
	}
	return strings.ToLower(strings.TrimSpace(nodeCell(node, col)))
}

func nodeSortTieValue(node nodes.Node) string {
	return strings.ToLower(strings.TrimSpace(node.Name + "|" + node.AETitle))
}

func nodeHeaderLabel(state *uiState, col int, label string) string {
	if state == nil || !state.nodeSortActive || state.nodeSortColumn != col {
		return label
	}
	if state.nodeSortDescending {
		return label + " ▼"
	}
	return label + " ▲"
}

func nodeOperationalCheckboxState(node nodes.Node, col int) (bool, bool) {
	switch col {
	case nodeTableColumnEnabled:
		return true, node.Enabled()
	case nodeTableColumnQuery:
		return true, node.QueryEnabled()
	case nodeTableColumnSend:
		return true, node.SendEnabled()
	default:
		return false, false
	}
}

func nodeRetrieveMethodDropdownState(node nodes.Node, col int) (bool, string) {
	if col != nodeTableColumnRetrieve {
		return false, ""
	}
	return true, node.RetrieveMethodOrDefault()
}

func applyNodeTableCell(cell *nodeTableCell, tableRow int, tableCol int, text string, header bool, selected bool, checkbox bool, checked bool, retrieveDropdown bool, retrieveValue string) {
	if cell == nil {
		return
	}
	cell.check.OnChanged = nil
	cell.check.Hide()
	cell.retrieveSelect.OnChanged = nil
	cell.retrieveSelect.Hide()
	cell.label.Show()
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{}
	cell.label.Alignment = fyne.TextAlignLeading
	cell.label.Wrapping = fyne.TextTruncate
	if header {
		cell.label.TextStyle = fyne.TextStyle{Bold: true}
		cell.background.FillColor = archiveHeaderRowColor
		cell.background.Refresh()
		return
	}
	if checkbox {
		cell.label.SetText("")
		cell.label.Hide()
		cell.check.SetChecked(checked)
		cell.check.Show()
	}
	if retrieveDropdown {
		cell.label.SetText("")
		cell.label.Hide()
		cell.retrieveSelect.SetSelected(retrieveValue)
		cell.retrieveSelect.Show()
	}
	if selected {
		cell.background.FillColor = archiveSelectedRowColor
		cell.background.Refresh()
		return
	}
	if tableRow%2 == 0 {
		cell.background.FillColor = archiveEvenRowColor
	} else {
		cell.background.FillColor = archiveOddRowColor
	}
	cell.background.Refresh()
}

func nodeTableHeaders() []string {
	return []string{"Enabled", "Query", "Retrieve", "Send", "TLS", "Name", "Called AE", "Host", "Port", "Move Destination", "Send Syntax", "Notes"}
}

func nodeCheckCell(enabled bool) string {
	if enabled {
		return "☑"
	}
	return "☐"
}

func nodeMenuCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Auto"
	}
	return "▾ " + value
}

func nodeCell(node nodes.Node, col int) string {
	switch col {
	case nodeTableColumnEnabled:
		return nodeCheckCell(node.Enabled())
	case nodeTableColumnQuery:
		return nodeCheckCell(node.QueryEnabled())
	case nodeTableColumnRetrieve:
		return nodeMenuCell(node.RetrieveMethodOrDefault())
	case nodeTableColumnSend:
		return nodeCheckCell(node.SendEnabled())
	case nodeTableColumnTLS:
		return nodeCheckCell(false)
	case nodeTableColumnName:
		return node.Name
	case nodeTableColumnAETitle:
		return node.AETitle
	case nodeTableColumnHost:
		return node.Host
	case nodeTableColumnPort:
		return fmt.Sprintf("%d", node.Port)
	case nodeTableColumnMoveDestination:
		return node.PreferredMoveDestination
	case nodeTableColumnSendSyntax:
		return nodeMenuCell(sendSyntaxLabel(node.SendTransferSyntaxOrDefault()))
	case nodeTableColumnNotes:
		return node.Notes
	default:
		return ""
	}
}

func queryCell(match query.Match, col int) string {
	switch col {
	case queryRetrieveColumn:
		return ""
	case queryTableColumnLocalState:
		return queryRowLocalStateText(match.LocalState)
	case queryTableColumnLevel:
		return queryLevelCell(match.QueryRetrieveLevel)
	case queryTableColumnPatient:
		return match.PatientName
	case queryTableColumnPatientID:
		return match.PatientID
	case queryTableColumnDOB:
		return match.PatientBirthDate
	case queryTableColumnStudyDate:
		return match.StudyDate
	case queryTableColumnTime:
		return dicomTimeCell(match.StudyTime)
	case queryTableColumnModality:
		if match.Modality != "" {
			return match.Modality
		}
		return match.Modalities
	case queryTableColumnImages:
		return match.ImageCount
	case queryTableColumnDescription:
		if match.SeriesDescription != "" {
			return match.SeriesDescription
		}
		return match.StudyDescription
	case queryTableColumnAccession:
		return match.AccessionNumber
	case queryTableColumnReferrer:
		return match.ReferringPhysicianName
	case queryTableColumnInstitution:
		return match.InstitutionName
	case queryTableColumnLocalComments:
		return match.LocalComments
	case queryTableColumnServerComments:
		return match.PatientComments
	case queryTableColumnStudyStatus:
		return match.StudyStatusID
	case queryTableColumnSeriesNumber:
		return match.SeriesNumber
	case queryTableColumnInstanceNumber:
		return match.InstanceNumber
	case queryTableColumnStudyUID:
		return match.StudyInstanceUID
	case queryTableColumnSeriesUID:
		return match.SeriesInstanceUID
	case queryTableColumnSOPClass:
		return match.SOPClassUID
	case queryTableColumnSOPUID:
		return match.SOPInstanceUID
	case queryTableColumnSource:
		return querySourceCell(match)
	case queryTableColumnStatus:
		return queryStatusCell(match.Status)
	default:
		return ""
	}
}

func queryLevelCell(level string) string {
	level = strings.ToUpper(strings.TrimSpace(level))
	switch level {
	case "PATIENT":
		return "▾ PATIENT"
	case "STUDY":
		return "  ▸ STUDY"
	case "SERIES":
		return "    ▸ SERIES"
	case "IMAGE":
		return "      IMAGE"
	default:
		return emptyDash(level)
	}
}

func querySourceCell(match query.Match) string {
	name := strings.TrimSpace(match.SourceNodeName)
	host := strings.TrimSpace(match.SourceHost)
	if host == "" || match.SourcePort == 0 {
		return name
	}
	endpoint := fmt.Sprintf("%s:%d", host, match.SourcePort)
	if name == "" {
		return endpoint
	}
	return fmt.Sprintf("%s / %s", name, endpoint)
}

func queryStatusCell(status uint16) string {
	return fmt.Sprintf("0x%04X", status)
}

func queryStatusDotColor(status uint16) color.NRGBA {
	switch status {
	case 0x0000:
		return queryStatusOKColor
	case 0xFF00, 0xFF01:
		return queryStatusPendingColor
	default:
		return queryStatusFailColor
	}
}

func studyStatusCell(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	switch {
	case strings.EqualFold(status, studyStatusPresetReviewedLabel):
		return "✓ " + studyStatusPresetReviewedLabel
	case strings.EqualFold(status, studyStatusPresetInterestingLabel):
		return "★ " + studyStatusPresetInterestingLabel
	case strings.EqualFold(status, studyStatusPresetFollowUpLabel):
		return "↪ " + studyStatusPresetFollowUpLabel
	case strings.EqualFold(status, studyStatusPresetTeachingLabel):
		return "▣ " + studyStatusPresetTeachingLabel
	case strings.EqualFold(status, studyStatusPresetProblemLabel):
		return "⚠ " + studyStatusPresetProblemLabel
	default:
		return "• " + status
	}
}

func studyStatusDotColor(status string) (color.NRGBA, bool) {
	status = strings.TrimSpace(status)
	if status == "" {
		return color.NRGBA{}, false
	}
	normalized := strings.ToLower(status)
	switch {
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetReviewedLabel)):
		return studyStatusReviewedColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetInterestingLabel)):
		return studyStatusInterestingColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetFollowUpLabel)):
		return studyStatusFollowUpColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetTeachingLabel)):
		return studyStatusTeachingColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetProblemLabel)):
		return studyStatusProblemColor, true
	default:
		return studyStatusCustomColor, true
	}
}

func studyCell(study archive.Study, col int) string {
	switch col {
	case archiveStudyTableColumnPatient:
		return study.PatientName
	case archiveStudyTableColumnPatientID:
		return study.PatientID
	case archiveStudyTableColumnDOB:
		return study.PatientBirthDate
	case archiveStudyTableColumnStudyDate:
		return archiveDateTimeCell(study.StudyDate, study.StudyTime)
	case archiveStudyTableColumnTime:
		return dicomTimeCell(study.StudyTime)
	case archiveStudyTableColumnAdded:
		return archiveTimestampCell(study.ImportedAt)
	case archiveStudyTableColumnModality:
		return study.Modalities
	case archiveStudyTableColumnDescription:
		return study.StudyDescription
	case archiveStudyTableColumnAccession:
		return study.AccessionNumber
	case archiveStudyTableColumnInstitution:
		return study.InstitutionName
	case archiveStudyTableColumnStatus:
		return studyStatusCell(study.Status)
	case archiveStudyTableColumnComments:
		return study.Comments
	case archiveStudyTableColumnSeries:
		return fmt.Sprintf("%d", study.SeriesCount)
	case archiveStudyTableColumnInstances:
		return fmt.Sprintf("%d", study.InstanceCount)
	case archiveStudyTableColumnStudyUID:
		return study.StudyInstanceUID
	default:
		return ""
	}
}

func dicomTimeCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.SplitN(value, ".", 2)[0]
	if len(value) >= 6 {
		return value[0:2] + ":" + value[2:4] + ":" + value[4:6]
	}
	if len(value) >= 4 {
		return value[0:2] + ":" + value[2:4]
	}
	return value
}

func archiveDateTimeCell(date string, dicomTime string) string {
	date = strings.TrimSpace(date)
	timeText := dicomTimeCell(dicomTime)
	if date == "" {
		return timeText
	}
	if timeText == "" {
		return date
	}
	return date + " " + timeText
}

func archiveTimestampCell(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04")
}

func seriesCell(series archive.Series, col int) string {
	switch col {
	case 0:
		return series.SeriesNumber
	case 1:
		return series.Modality
	case 2:
		return series.SeriesDescription
	case 3:
		return fmt.Sprintf("%d", series.InstanceCount)
	case 4:
		return series.SeriesInstanceUID
	default:
		return ""
	}
}

func instanceCell(instance archive.Instance, col int) string {
	switch col {
	case 0:
		return instance.InstanceNumber
	case 1:
		return instance.Modality
	case 2:
		return instance.SOPClassUID
	case 3:
		if instance.TransferSyntax != "" {
			return instance.TransferSyntax
		}
		return instance.TransferSyntaxUID
	case 4:
		return instance.SourcePath
	case 5:
		return instance.SOPInstanceUID
	default:
		return ""
	}
}

func tableCell(elem dicominspect.ElementSummary, col int) string {
	switch col {
	case 0:
		return elem.Source
	case 1:
		return elem.Tag
	case 2:
		return elem.VR
	case 3:
		if elem.Keyword != "" {
			return elem.Keyword
		}
		return elem.Name
	case 4:
		return elem.Length
	case 5:
		return elem.Value
	default:
		return ""
	}
}

func defaultArchiveDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".", ".go-pacs")
	}
	return filepath.Join(dir, "go-pacs")
}
