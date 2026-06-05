package main

import (
	"context"
	"errors"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
)

func TestDefaultWindowSizeIsWorkstationOriented(t *testing.T) {
	size := defaultWindowSize()

	if size.Width < 1600 || size.Height < 900 {
		t.Fatalf("default window size = %v, want at least 1600x900", size)
	}
}

func TestConfigureAppAppearanceUsesDarkTheme(t *testing.T) {
	a := fynetest.NewApp()
	configureAppAppearance(a)

	background := a.Settings().Theme().Color(theme.ColorNameBackground, theme.VariantDark)
	rgba := color.NRGBAModel.Convert(background).(color.NRGBA)
	if rgba.R > 80 || rgba.G > 80 || rgba.B > 80 {
		t.Fatalf("dark background = %#v, want dark workstation colors", rgba)
	}
}

func TestRecordOperationPrependsAndCapsHistory(t *testing.T) {
	state := &uiState{}
	for i := 0; i < maxTaskHistory+2; i++ {
		recordOperation(state, operations.Summary{
			Kind:       operations.KindImport,
			Status:     operations.StatusSuccess,
			DurationMS: uint64(i),
		})
	}

	if len(state.operations) != maxTaskHistory {
		t.Fatalf("len(operations) = %d, want %d", len(state.operations), maxTaskHistory)
	}
	if state.operations[0].DurationMS != uint64(maxTaskHistory+1) {
		t.Fatalf("newest DurationMS = %d, want %d", state.operations[0].DurationMS, maxTaskHistory+1)
	}
	if state.operations[len(state.operations)-1].DurationMS != 2 {
		t.Fatalf("oldest DurationMS = %d, want 2", state.operations[len(state.operations)-1].DurationMS)
	}
}

func TestTaskCellRendersImportCounts(t *testing.T) {
	requested := uint64(3)
	stored := uint64(1)
	failed := uint64(1)
	duplicates := uint64(1)
	summary := operations.Summary{
		Kind:   operations.KindImport,
		Status: operations.StatusWarning,
		Counts: operations.Counts{
			Requested:  &requested,
			Stored:     &stored,
			Failed:     &failed,
			Duplicates: &duplicates,
		},
		DurationMS: 1500,
	}

	if got := taskCell(summary, 0); got != "import" {
		t.Fatalf("kind cell = %q, want import", got)
	}
	if got := taskCell(summary, 1); got != "warning" {
		t.Fatalf("status cell = %q, want warning", got)
	}
	if got := taskCell(summary, 2); got != "requested 3, stored 1, duplicates 1, failed 1" {
		t.Fatalf("counts cell = %q", got)
	}
	if got := taskCell(summary, 3); got != "1.5s" {
		t.Fatalf("duration cell = %q, want 1.5s", got)
	}
}

func TestTaskDetailTextRendersFormattedSummaryJSON(t *testing.T) {
	text := taskDetailText(operations.Summary{
		Version:    operations.SummaryVersion,
		Kind:       operations.KindImport,
		Status:     operations.StatusSuccess,
		DurationMS: 25,
	})

	if !strings.Contains(text, "\"kind\": \"import\"") || !strings.Contains(text, "\"duration_ms\": 25") {
		t.Fatalf("detail text = %s", text)
	}
}

func TestCancelActiveRetrieveCancelsContextAndClearsState(t *testing.T) {
	state := &uiState{}
	status := widget.NewLabel("")
	ctx, cancel := beginRetrieve(state, "remote-a")
	defer cancel()

	if state.activeRetrieveCancel == nil {
		t.Fatal("activeRetrieveCancel was not set")
	}
	if state.retrieveActivityNode != "remote-a" {
		t.Fatalf("retrieveActivityNode = %q, want remote-a", state.retrieveActivityNode)
	}

	cancelActiveRetrieve(status, state)

	if state.activeRetrieveCancel != nil {
		t.Fatal("activeRetrieveCancel was not cleared")
	}
	if status.Text != "Cancelling active retrieve" {
		t.Fatalf("status = %q", status.Text)
	}
	if err := ctx.Err(); err == nil {
		t.Fatal("retrieve context was not cancelled")
	}
}

func TestCancelActiveRetrieveReportsNoActiveRetrieve(t *testing.T) {
	state := &uiState{}
	status := widget.NewLabel("")

	cancelActiveRetrieve(status, state)

	if status.Text != "No active retrieve" {
		t.Fatalf("status = %q", status.Text)
	}
}

func TestRefreshArchiveChromeShowsActivityCancelButtonDuringRetrieve(t *testing.T) {
	state := &uiState{archiveCancelRetrieveButton: widget.NewButton("Cancel", nil)}

	refreshArchiveChrome(state)

	if state.archiveCancelRetrieveButton.Visible() {
		t.Fatal("cancel button should be hidden without an active retrieve")
	}

	state.activeRetrieveCancel = func() {}
	refreshArchiveChrome(state)

	if !state.archiveCancelRetrieveButton.Visible() {
		t.Fatal("cancel button should be visible during an active retrieve")
	}
}

func TestRetrieveProgressCallbackUpdatesStatusText(t *testing.T) {
	fynetest.NewApp()
	status := widget.NewLabel("")
	state := &uiState{}

	callback := retrieveProgressCallback(status, state, "remote-a")
	callback(retrieve.Progress{
		FinalStatus: 0xFF00,
		Remaining:   2,
		Completed:   3,
		Failed:      1,
		Warnings:    4,
	})

	want := "Retrieve remote-a progress: status=0xFF00 remaining 2 completed 3 failed 1 warnings 4"
	if status.Text != want {
		t.Fatalf("status = %q, want %q", status.Text, want)
	}
	if state.retrieveActivityNode != "remote-a" || state.retrieveActivityProgress.Completed != 3 {
		t.Fatalf("retrieve activity state = %#v node %q", state.retrieveActivityProgress, state.retrieveActivityNode)
	}
}

func TestRetrieveProgressFractionAndText(t *testing.T) {
	progress := retrieve.Progress{
		Remaining: 2,
		Completed: 3,
		Failed:    1,
		Warnings:  4,
	}

	if got := retrieveProgressFraction(progress); got != 0.8 {
		t.Fatalf("fraction = %v, want 0.8", got)
	}
	text := retrieveProgressText("remote-a", progress)
	for _, want := range []string{"remote-a", "8/10", "failed 1", "warnings 4"} {
		if !strings.Contains(text, want) {
			t.Fatalf("progress text missing %q in %q", want, text)
		}
	}
}

func TestRetrieveOptionsUseConfiguredReceiverAddress(t *testing.T) {
	status := widget.NewLabel("")
	state := &uiState{appConfig: appconfig.Config{
		LocalAETitle:    "GOPACS",
		ReceiverAddress: "0.0.0.0:11113",
	}}
	node := nodes.Node{Name: "radiant", AETitle: "RADIANT", PreferredMoveDestination: "GOPACS"}

	opts := retrieveOptionsForNode(status, state, node)

	if opts.ReceiveAddress != "0.0.0.0:11113" {
		t.Fatalf("ReceiveAddress = %q, want 0.0.0.0:11113", opts.ReceiveAddress)
	}
	if opts.CallingAETitle != "GOPACS" {
		t.Fatalf("CallingAETitle = %q, want GOPACS", opts.CallingAETitle)
	}
	if opts.MoveDestination != "GOPACS" {
		t.Fatalf("MoveDestination = %q, want GOPACS", opts.MoveDestination)
	}
	if opts.OnProgress == nil {
		t.Fatal("OnProgress was nil")
	}
}

func TestRetrieveOptionsUseQueryMoveDestinationOverride(t *testing.T) {
	status := widget.NewLabel("")
	state := &uiState{
		appConfig: appconfig.Config{
			LocalAETitle:    "GOPACS",
			ReceiverAddress: "0.0.0.0:11113",
		},
		queryMoveDestination: "GOPACS_ALT",
	}
	node := nodes.Node{Name: "radiant", PreferredMoveDestination: "GOPACS_STORE"}

	opts := retrieveOptionsForNode(status, state, node)

	if opts.MoveDestination != "GOPACS_ALT" {
		t.Fatalf("MoveDestination = %q, want GOPACS_ALT", opts.MoveDestination)
	}
}

func TestRetrieveOptionsUseNodeRetrieveMethodPreference(t *testing.T) {
	status := widget.NewLabel("")
	state := &uiState{
		appConfig: appconfig.Config{
			LocalAETitle:    "GOPACS",
			ReceiverAddress: "0.0.0.0:11113",
		},
	}
	node := nodes.Node{Name: "radiant", RetrieveMethod: nodes.RetrieveMethodGet}

	opts := retrieveOptionsForNode(status, state, node)

	if opts.Method != retrieve.MethodGet {
		t.Fatalf("Method = %q, want %q", opts.Method, retrieve.MethodGet)
	}
}

func TestQueryDestinationTextShowsMoveDestinationAndReceiver(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			LocalAETitle:    "GOPACS",
			ReceiverAddress: "0.0.0.0:11113",
		},
		nodes: []nodes.Node{{
			Name:                     "RADIANT",
			PreferredMoveDestination: "GOPACS_STORE",
		}},
	}

	text := queryDestinationText(state)

	for _, want := range []string{"Retrieve to: GOPACS_STORE", "0.0.0.0:11113", "Auto C-MOVE/C-GET"} {
		if !strings.Contains(text, want) {
			t.Fatalf("destination text missing %q in %q", want, text)
		}
	}
}

func TestQueryDestinationTextShowsNodeRetrieveMethodPreference(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			LocalAETitle:    "GOPACS",
			ReceiverAddress: "0.0.0.0:11113",
		},
		nodes: []nodes.Node{{Name: "RADIANT", RetrieveMethod: nodes.RetrieveMethodGet}},
	}

	text := queryDestinationText(state)

	if !strings.Contains(text, "C-GET") {
		t.Fatalf("destination text = %q", text)
	}
}

func TestQueryDestinationTextUsesMoveDestinationOverride(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			LocalAETitle:    "GOPACS",
			ReceiverAddress: "0.0.0.0:11113",
		},
		queryMoveDestination: "GOPACS_ALT",
		nodes: []nodes.Node{{
			Name:                     "RADIANT",
			PreferredMoveDestination: "GOPACS_STORE",
		}},
	}

	text := queryDestinationText(state)

	if !strings.Contains(text, "Retrieve to: GOPACS_ALT") {
		t.Fatalf("destination text = %q", text)
	}
}

func TestQueryMoveDestinationOptionsIncludeLocalPreferredAndOverride(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{LocalAETitle: "GOPACS"},
		nodes: []nodes.Node{{
			Name:                     "RADIANT",
			PreferredMoveDestination: "GOPACS_STORE",
		}},
		queryMoveDestination: "GOPACS_ALT",
	}

	options := queryMoveDestinationOptions(state)
	want := []string{"GOPACS", "GOPACS_STORE", "GOPACS_ALT"}

	if strings.Join(options, "|") != strings.Join(want, "|") {
		t.Fatalf("destination options = %#v, want %#v", options, want)
	}
}

func TestQueryMoveDestinationEntryAllowsCustomDestination(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{LocalAETitle: "GOPACS", ReceiverAddress: "0.0.0.0:11113"},
		nodes: []nodes.Node{{
			Name:                     "RADIANT",
			PreferredMoveDestination: "GOPACS_STORE",
		}},
	}

	entry := newQueryMoveDestinationEntry(state)

	if entry.Text != "GOPACS" {
		t.Fatalf("entry text = %q, want GOPACS", entry.Text)
	}
	entry.SetText(" GOPACS_ALT ")

	if state.queryMoveDestination != "GOPACS_ALT" {
		t.Fatalf("queryMoveDestination = %q, want GOPACS_ALT", state.queryMoveDestination)
	}
	if text := queryDestinationText(state); !strings.Contains(text, "Retrieve to: GOPACS_ALT") {
		t.Fatalf("destination text = %q", text)
	}
}

func TestQueryDestinationTextFallsBackToLocalAEWithoutNode(t *testing.T) {
	state := &uiState{appConfig: appconfig.Config{
		LocalAETitle:    "GOPACS",
		ReceiverAddress: "0.0.0.0:11113",
	}}

	text := queryDestinationText(state)

	if !strings.Contains(text, "Retrieve to: GOPACS") || !strings.Contains(text, "no source selected") {
		t.Fatalf("destination text = %q", text)
	}
}

func TestQueryResultSummaryTextShowsCountAndSelectedSource(t *testing.T) {
	state := &uiState{
		queries: []query.Match{{}, {}},
		nodes: []nodes.Node{{
			Name: "RADIANT",
			Host: "192.168.100.26",
			Port: 11112,
		}},
	}

	text := queryResultSummaryText(state)

	if text != "2 matches found / RADIANT / 192.168.100.26:11112" {
		t.Fatalf("summary text = %q", text)
	}
}

func TestRefreshQueryResultSummaryUpdatesLabel(t *testing.T) {
	label := widget.NewLabel("")
	state := &uiState{
		queryResultSummaryLabel: label,
		queries:                 []query.Match{{}},
	}

	refreshQueryResultSummary(state)

	if label.Text != "1 match found / no source selected" {
		t.Fatalf("summary label = %q", label.Text)
	}
}

func TestQueryModalityCriteriaTextUsesCheckedModalities(t *testing.T) {
	checks := map[string]*widget.Check{
		"CT": {Checked: true},
		"MR": {Checked: true},
	}

	if got := queryModalityCriteriaText("OT", checks); got != "CT\\MR" {
		t.Fatalf("modality criteria = %q, want CT\\MR", got)
	}
}

func TestQueryModalityCriteriaTextFallsBackToManualEntry(t *testing.T) {
	if got := queryModalityCriteriaText("  pt  ", nil); got != "pt" {
		t.Fatalf("modality criteria = %q, want pt", got)
	}
}

func TestQueryDatePresetRange(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name     string
		preset   string
		wantFrom string
		wantTo   string
		wantOK   bool
	}{
		{name: "any", preset: queryDatePresetAny, wantOK: true},
		{name: "today", preset: queryDatePresetToday, wantFrom: "20260604", wantTo: "20260604", wantOK: true},
		{name: "yesterday", preset: queryDatePresetYesterday, wantFrom: "20260603", wantTo: "20260603", wantOK: true},
		{name: "day before yesterday", preset: queryDatePresetDayBeforeYesterday, wantFrom: "20260602", wantTo: "20260602", wantOK: true},
		{name: "last two", preset: queryDatePresetLast2Days, wantFrom: "20260603", wantTo: "20260604", wantOK: true},
		{name: "last seven", preset: queryDatePresetLast7Days, wantFrom: "20260529", wantTo: "20260604", wantOK: true},
		{name: "last month", preset: queryDatePresetLastMonth, wantFrom: "20260505", wantTo: "20260604", wantOK: true},
		{name: "last three months", preset: queryDatePresetLast3Months, wantFrom: "20260305", wantTo: "20260604", wantOK: true},
		{name: "unknown", preset: "unknown", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, ok := queryDatePresetRange(tt.preset, now)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if from != tt.wantFrom || to != tt.wantTo {
				t.Fatalf("range = %q/%q, want %q/%q", from, to, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestQueryActionButtonLabelsAreCompact(t *testing.T) {
	labels := queryActionButtonLabels()
	want := []string{"Query", "Patient", "Series", "Images", "Retrieve", "Verify"}

	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("query action labels = %q, want %q", strings.Join(labels, "|"), strings.Join(want, "|"))
	}
}

func TestMainToolbarButtonLabelsAreCompact(t *testing.T) {
	labels := mainToolbarButtonLabels()
	want := []string{
		"Open", "Inspect", "Import", "Folder", "Refresh",
		"Send Study", "Send Series", "Send Image", "Get Series", "Get Image",
		"Cancel", "Add", "Edit", "Delete", "Verify", "Listen", "Stop", "Settings",
	}

	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("main toolbar labels = %q, want %q", strings.Join(labels, "|"), strings.Join(want, "|"))
	}
	for _, label := range labels {
		if strings.Contains(label, "DICOM") || strings.Contains(label, "Receiver") || len(label) > 11 {
			t.Fatalf("toolbar label %q is not compact", label)
		}
	}
}

func TestCompactToolbarButtonUsesLowImportance(t *testing.T) {
	button := compactToolbarButton("Open", theme.FolderOpenIcon(), nil)

	if button.Text != "Open" {
		t.Fatalf("button text = %q, want Open", button.Text)
	}
	if button.Importance != widget.LowImportance {
		t.Fatalf("button importance = %v, want LowImportance", button.Importance)
	}
	if button.Icon == nil {
		t.Fatal("button icon is nil")
	}
}

func TestQueryRefreshControlsMatchManualMode(t *testing.T) {
	if queryRefreshButtonLabel != "Refresh" {
		t.Fatalf("refresh button label = %q, want Refresh", queryRefreshButtonLabel)
	}
	if strings.Join(queryRefreshModeOptions, "|") != "Don't refresh" {
		t.Fatalf("refresh mode options = %q", strings.Join(queryRefreshModeOptions, "|"))
	}
}

func TestQueryAutoRetrieveControlDefaultsOffAndUpdatesState(t *testing.T) {
	state := &uiState{}

	check := newQueryAutoRetrieveCheck(state)

	if check.Text != queryAutoRetrieveLabel {
		t.Fatalf("auto-retrieve label = %q, want %q", check.Text, queryAutoRetrieveLabel)
	}
	if check.Checked {
		t.Fatal("auto-retrieve should default off")
	}
	if state.queryAutoRetrieve {
		t.Fatal("state auto-retrieve should default off")
	}

	check.SetChecked(true)
	if !state.queryAutoRetrieve {
		t.Fatal("state auto-retrieve did not update when checked")
	}
	check.SetChecked(false)
	if state.queryAutoRetrieve {
		t.Fatal("state auto-retrieve did not update when unchecked")
	}
}

func TestRememberLastStudyQueryMakesRefreshAvailable(t *testing.T) {
	state := &uiState{}
	if queryRefreshAvailable(state) {
		t.Fatal("refresh should not be available before a query is remembered")
	}

	criteria := query.Criteria{PatientName: "DOE^JANE", StudyDateFrom: "20260601", MaxResults: 12}
	rememberLastStudyQuery(state, criteria)

	if !queryRefreshAvailable(state) {
		t.Fatal("refresh should be available after a study query is remembered")
	}
	if state.lastQuery.kind != queryRunStudy {
		t.Fatalf("last query kind = %q, want %q", state.lastQuery.kind, queryRunStudy)
	}
	if state.lastQuery.study != criteria {
		t.Fatalf("last study criteria = %#v, want %#v", state.lastQuery.study, criteria)
	}
}

func TestRememberLastPatientQueryMakesRefreshAvailable(t *testing.T) {
	state := &uiState{}
	criteria := query.PatientCriteria{PatientName: "DOE^*", PatientID: "P123", MaxResults: 12}

	rememberLastPatientQuery(state, criteria)

	if !queryRefreshAvailable(state) {
		t.Fatal("refresh should be available after a patient query is remembered")
	}
	if state.lastQuery.kind != queryRunPatient {
		t.Fatalf("last query kind = %q, want %q", state.lastQuery.kind, queryRunPatient)
	}
	if state.lastQuery.patient != criteria {
		t.Fatalf("last patient criteria = %#v, want %#v", state.lastQuery.patient, criteria)
	}
}

func TestQueryCriteriaWithQuickSearch(t *testing.T) {
	base := query.Criteria{
		PatientID:        "P123",
		StudyDateFrom:    "20260601",
		StudyDateTo:      "20260630",
		StudyDescription: "advanced description",
		AccessionNumber:  "A100",
		Modality:         "CT",
		StudyInstanceUID: "1.2.3",
		MaxResults:       25,
	}

	criteria, ok := queryCriteriaWithQuickSearch(base, queryQuickSearchPatientName, "  DOE  ")

	if !ok {
		t.Fatal("patient-name quick search should be supported")
	}
	if criteria.PatientName != "DOE" {
		t.Fatalf("PatientName = %q, want DOE", criteria.PatientName)
	}
	if criteria.PatientID != "P123" ||
		criteria.StudyDateFrom != "20260601" ||
		criteria.StudyDateTo != "20260630" ||
		criteria.StudyDescription != "advanced description" ||
		criteria.AccessionNumber != "A100" ||
		criteria.Modality != "CT" ||
		criteria.StudyInstanceUID != "1.2.3" ||
		criteria.MaxResults != 25 {
		t.Fatalf("advanced criteria were not preserved: %#v", criteria)
	}
}

func TestQueryCriteriaWithQuickSearchSupportsStudyFields(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		wantID    string
		wantAcc   string
		wantDesc  string
		wantName  string
		wantBirth string
		wantRef   string
		wantInst  string
		wantComm  string
		wantStat  string
		wantOK    bool
	}{
		{name: "patient id", field: queryQuickSearchPatientID, wantID: "value", wantOK: true},
		{name: "accession", field: queryQuickSearchAccession, wantAcc: "value", wantOK: true},
		{name: "description", field: queryQuickSearchDescription, wantDesc: "value", wantOK: true},
		{name: "patient name", field: queryQuickSearchPatientName, wantName: "value", wantOK: true},
		{name: "birthdate", field: queryQuickSearchBirthdate, wantBirth: "value", wantOK: true},
		{name: "referring physician", field: queryQuickSearchReferringPhysician, wantRef: "value", wantOK: true},
		{name: "institution", field: queryQuickSearchInstitution, wantInst: "value", wantOK: true},
		{name: "comments", field: queryQuickSearchComments, wantComm: "value", wantOK: true},
		{name: "status", field: queryQuickSearchStatus, wantStat: "value", wantOK: true},
		{name: "unknown", field: "Unknown", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{}, tt.field, " value ")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if criteria.PatientID != tt.wantID ||
				criteria.AccessionNumber != tt.wantAcc ||
				criteria.StudyDescription != tt.wantDesc ||
				criteria.PatientName != tt.wantName ||
				criteria.PatientBirthDate != tt.wantBirth ||
				criteria.ReferringPhysicianName != tt.wantRef ||
				criteria.InstitutionName != tt.wantInst ||
				criteria.PatientComments != tt.wantComm ||
				criteria.StudyStatusID != tt.wantStat {
				t.Fatalf("criteria = %#v", criteria)
			}
		})
	}
}

func TestQueryQuickSearchOptionsIncludeCustomDICOMField(t *testing.T) {
	for _, option := range queryQuickSearchOptions {
		if option == queryQuickSearchCustomDICOMField {
			return
		}
	}
	t.Fatalf("queryQuickSearchOptions missing %q: %#v", queryQuickSearchCustomDICOMField, queryQuickSearchOptions)
}

func TestQueryCriteriaWithQuickSearchSupportsCustomDICOMField(t *testing.T) {
	criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{PatientName: "DOE"}, queryQuickSearchCustomDICOMField, " StudyID = ABC123 ")

	if !ok {
		t.Fatal("custom DICOM quick search should be supported")
	}
	if criteria.PatientName != "DOE" {
		t.Fatalf("advanced PatientName = %q, want DOE", criteria.PatientName)
	}
	if criteria.CustomFieldKeyword != "StudyID" || criteria.CustomFieldValue != "ABC123" {
		t.Fatalf("custom field = %q/%q, want StudyID/ABC123", criteria.CustomFieldKeyword, criteria.CustomFieldValue)
	}
}

func TestQueryCriteriaWithQuickSearchRejectsMalformedCustomDICOMField(t *testing.T) {
	criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{PatientName: "DOE"}, queryQuickSearchCustomDICOMField, "StudyID")

	if ok {
		t.Fatalf("malformed custom DICOM quick search returned ok with criteria %#v", criteria)
	}
}

func TestQueryPatientCriteriaWithQuickSearchSupportsPatientFields(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		wantName  string
		wantID    string
		wantBirth string
		wantComm  string
		wantOK    bool
	}{
		{name: "patient name", field: queryQuickSearchPatientName, wantName: "value", wantOK: true},
		{name: "patient id", field: queryQuickSearchPatientID, wantID: "value", wantOK: true},
		{name: "birthdate", field: queryQuickSearchBirthdate, wantBirth: "value", wantOK: true},
		{name: "comments", field: queryQuickSearchComments, wantComm: "value", wantOK: true},
		{name: "accession unsupported", field: queryQuickSearchAccession, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			criteria, ok := queryPatientCriteriaWithQuickSearch(query.PatientCriteria{}, tt.field, " value ")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if criteria.PatientName != tt.wantName ||
				criteria.PatientID != tt.wantID ||
				criteria.PatientBirthDate != tt.wantBirth ||
				criteria.PatientComments != tt.wantComm {
				t.Fatalf("criteria = %#v", criteria)
			}
		})
	}
}

func TestQuerySourceRowsMarkSelectedNode(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: 1,
	}

	rows := querySourceRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0] != "  [x] RADIANT 192.168.100.26:11112" {
		t.Fatalf("first source = %q", rows[0])
	}
	if rows[1] != "▶ [x] HOROSMINI 192.168.100.50:4007" {
		t.Fatalf("selected source = %q", rows[1])
	}
}

func TestQuerySourceRowsShowVerifiedNodeStatus(t *testing.T) {
	node := nodes.Node{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}
	state := &uiState{nodes: []nodes.Node{node}, selectedNodeRow: -1}

	recordNodeVerifyStatus(state, node, nodeVerifyOK)
	rows := querySourceRows(state)

	if len(rows) != 1 || rows[0] != "  [x] ✓ RADIANT 192.168.100.26:11112" {
		t.Fatalf("verified source row = %#v", rows)
	}
}

func TestQuerySourceRowsShowQuerySourceStatus(t *testing.T) {
	nodesList := []nodes.Node{
		{ID: "node-1", Name: "offline", Host: "10.0.0.1", Port: 104},
		{ID: "node-2", Name: "online", Host: "10.0.0.2", Port: 105},
	}
	state := &uiState{nodes: nodesList, selectedNodeRow: -1}

	recordQuerySourceStatus(state, nodesList[0], querySourceFail)
	recordQuerySourceStatus(state, nodesList[1], querySourceOK)
	rows := querySourceRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0] != "  [x] Q! offline 10.0.0.1:104" {
		t.Fatalf("failed source row = %q", rows[0])
	}
	if rows[1] != "  [x] Q✓ online 10.0.0.2:105" {
		t.Fatalf("successful source row = %q", rows[1])
	}
}

func TestQuerySourceRowsShowDisabledNodesUnchecked(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112, Disabled: true},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: 1,
	}

	rows := querySourceRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0] != "  [ ] RADIANT 192.168.100.26:11112" {
		t.Fatalf("disabled source = %q", rows[0])
	}
	if rows[1] != "▶ [x] HOROSMINI 192.168.100.50:4007" {
		t.Fatalf("enabled source = %q", rows[1])
	}
}

func TestQuerySourceRowsShowQueryDisabledNodesUnchecked(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112, QueryDisabled: true},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: 1,
	}

	rows := querySourceRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0] != "  [ ] RADIANT 192.168.100.26:11112" {
		t.Fatalf("query disabled source = %q", rows[0])
	}
	if rows[1] != "▶ [x] HOROSMINI 192.168.100.50:4007" {
		t.Fatalf("enabled source = %q", rows[1])
	}
}

func TestQuerySourceCheckedRequiresEnabledAndQueryEnabled(t *testing.T) {
	tests := []struct {
		name string
		node nodes.Node
		want bool
	}{
		{"enabled", nodes.Node{}, true},
		{"disabled", nodes.Node{Disabled: true}, false},
		{"query_disabled", nodes.Node{QueryDisabled: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := querySourceChecked(tt.node); got != tt.want {
				t.Fatalf("querySourceChecked(%+v) = %t, want %t", tt.node, got, tt.want)
			}
		})
	}
}

func TestQuerySourceCheckLabelOmitsTextCheckboxMarker(t *testing.T) {
	node := nodes.Node{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}
	state := &uiState{nodes: []nodes.Node{node}, selectedNodeRow: 0}
	recordNodeVerifyStatus(state, node, nodeVerifyOK)
	recordQuerySourceStatus(state, node, querySourceFail)

	label := querySourceCheckLabel(state, 0)

	if label != "▶ ✓ Q! RADIANT 192.168.100.26:11112" {
		t.Fatalf("query source check label = %q", label)
	}
	if strings.Contains(label, "[x]") || strings.Contains(label, "[ ]") {
		t.Fatalf("native checkbox label should not include text marker: %q", label)
	}
}

func TestSetQuerySourceEnabledPersistsAndReenablesSource(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore: store,
		nodes: []nodes.Node{{
			ID:            "node-1",
			Name:          "radiant",
			AETitle:       "RADIANT",
			Host:          "192.168.100.26",
			Port:          11112,
			Disabled:      true,
			QueryDisabled: true,
			SendDisabled:  true,
		}},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}

	changed, err := setQuerySourceEnabled(state, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected source enable to report change")
	}
	if state.nodes[0].Disabled || state.nodes[0].QueryDisabled {
		t.Fatalf("source was not enabled for query: %+v", state.nodes[0])
	}
	if !state.nodes[0].SendDisabled {
		t.Fatalf("source enable should not alter Send flag: %+v", state.nodes[0])
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Disabled || list[0].QueryDisabled || !list[0].SendDisabled {
		t.Fatalf("persisted nodes = %+v", list)
	}
}

func TestSetQuerySourceDisabledPersistsQueryFlagOnly(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore: store,
		nodes: []nodes.Node{{
			ID:      "node-1",
			Name:    "radiant",
			AETitle: "RADIANT",
			Host:    "192.168.100.26",
			Port:    11112,
		}},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}

	changed, err := setQuerySourceEnabled(state, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected source disable to report change")
	}
	if state.nodes[0].Disabled || !state.nodes[0].QueryDisabled {
		t.Fatalf("source should remain node-enabled but query-disabled: %+v", state.nodes[0])
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Disabled || !list[0].QueryDisabled {
		t.Fatalf("persisted nodes = %+v", list)
	}
}

func TestConfigureQuerySourceCheckTogglesSource(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore: store,
		nodes: []nodes.Node{{
			ID:            "node-1",
			Name:          "RADIANT",
			AETitle:       "RADIANT",
			Host:          "192.168.100.26",
			Port:          11112,
			QueryDisabled: true,
		}},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}
	check := widget.NewCheck("", nil)
	calls := 0

	configureQuerySourceCheck(check, state, 0, func() { calls++ })

	if check.Checked {
		t.Fatal("rendered query-disabled source as checked")
	}
	if calls != 0 {
		t.Fatalf("render should not trigger onChanged, calls=%d", calls)
	}
	check.SetChecked(true)
	if state.nodes[0].QueryDisabled {
		t.Fatalf("check toggle did not enable query source: %+v", state.nodes[0])
	}
	if calls != 1 {
		t.Fatalf("onChanged calls = %d, want 1", calls)
	}
}

func TestMoveQuerySourceReordersSelectionAndPersists(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore:       store,
		selectedNodeRow: 1,
		nodes: []nodes.Node{
			{ID: "node-1", Name: "first", AETitle: "FIRST", Host: "10.0.0.1", Port: 104},
			{ID: "node-2", Name: "second", AETitle: "SECOND", Host: "10.0.0.2", Port: 105},
			{ID: "node-3", Name: "third", AETitle: "THIRD", Host: "10.0.0.3", Port: 106},
		},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}

	changed, err := moveQuerySource(state, -1)
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("move up did not report change")
	}
	if state.selectedNodeRow != 0 {
		t.Fatalf("selectedNodeRow = %d, want 0", state.selectedNodeRow)
	}
	if got := nodeNames(state.nodes); strings.Join(got, "|") != "second|first|third" {
		t.Fatalf("node order = %#v", got)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeNames(list); strings.Join(got, "|") != "second|first|third" {
		t.Fatalf("persisted order = %#v", got)
	}
}

func TestMoveQuerySourceIgnoresBoundaryRows(t *testing.T) {
	state := &uiState{
		selectedNodeRow: 0,
		nodes:           []nodes.Node{{Name: "first"}, {Name: "second"}},
	}

	changed, err := moveQuerySource(state, -1)

	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("move up from first row should not change")
	}
	if state.selectedNodeRow != 0 || strings.Join(nodeNames(state.nodes), "|") != "first|second" {
		t.Fatalf("state after boundary move = row %d nodes %#v", state.selectedNodeRow, nodeNames(state.nodes))
	}
}

func nodeNames(nodeList []nodes.Node) []string {
	names := make([]string, 0, len(nodeList))
	for _, node := range nodeList {
		names = append(names, node.Name)
	}
	return names
}

func TestQuerySourceRowsShowsEmptyState(t *testing.T) {
	rows := querySourceRows(&uiState{})

	if len(rows) != 1 || rows[0] != "No remote sources configured" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestNodeTableHeadersIncludeOperationalColumns(t *testing.T) {
	headers := nodeTableHeaders()
	joined := strings.Join(headers, "|")

	for _, want := range []string{"Enabled", "Query", "Retrieve", "Send", "TLS", "Name", "Called AE", "Host", "Port", "Move Destination", "Send Syntax", "Notes"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("headers missing %q in %q", want, joined)
		}
	}
}

func TestNodeCellShowsOperationalDefaults(t *testing.T) {
	node := nodes.Node{
		Name:                     "RADIANT",
		AETitle:                  "RADIANTAE",
		Host:                     "192.168.100.26",
		Port:                     11112,
		PreferredMoveDestination: "GOPACS",
		Notes:                    "lab node",
	}

	tests := []struct {
		col  int
		want string
	}{
		{0, "☑"},
		{1, "☑"},
		{2, "▾ Auto"},
		{3, "☑"},
		{4, "☐"},
		{5, "RADIANT"},
		{6, "RADIANTAE"},
		{7, "192.168.100.26"},
		{8, "11112"},
		{9, "GOPACS"},
		{10, "▾ Auto"},
		{11, "lab node"},
	}

	for _, tt := range tests {
		if got := nodeCell(node, tt.col); got != tt.want {
			t.Fatalf("nodeCell col %d = %q, want %q", tt.col, got, tt.want)
		}
	}
}

func TestNodeCellShowsDisabledState(t *testing.T) {
	node := nodes.Node{Disabled: true}

	if got := nodeCell(node, 0); got != "☐" {
		t.Fatalf("disabled Enabled cell = %q, want unchecked marker", got)
	}
}

func TestNodeCellShowsQueryAndSendDisabledState(t *testing.T) {
	node := nodes.Node{QueryDisabled: true, SendDisabled: true}

	if got := nodeCell(node, 1); got != "☐" {
		t.Fatalf("query cell = %q, want unchecked marker", got)
	}
	if got := nodeCell(node, 3); got != "☐" {
		t.Fatalf("send cell = %q, want unchecked marker", got)
	}
}

func TestNodeCellShowsRetrieveMethodPreference(t *testing.T) {
	node := nodes.Node{RetrieveMethod: nodes.RetrieveMethodGet}

	if got := nodeCell(node, 2); got != "▾ C-GET" {
		t.Fatalf("retrieve cell = %q, want dropdown marker", got)
	}
}

func TestNodeDraftFromFormStateMapsEnabledCheckbox(t *testing.T) {
	draft := nodeDraftFromFormState("pacs", "REMOTE", "localhost", 11112, false, true, "C-MOVE", false, "LOCAL", "notes")

	if !draft.Disabled {
		t.Fatalf("disabled checkbox state produced draft %+v", draft)
	}
	if draft.QueryDisabled {
		t.Fatalf("enabled query checkbox produced draft %+v", draft)
	}
	if !draft.SendDisabled {
		t.Fatalf("disabled send checkbox produced draft %+v", draft)
	}
	if draft.RetrieveMethod != "C-MOVE" {
		t.Fatalf("RetrieveMethod = %q, want C-MOVE", draft.RetrieveMethod)
	}
	if draft.Name != "pacs" || draft.AETitle != "REMOTE" || draft.Host != "localhost" || draft.Port != 11112 || draft.PreferredMoveDestination != "LOCAL" || draft.Notes != "notes" {
		t.Fatalf("draft fields = %+v", draft)
	}
}

func TestToggleNodeOperationalCellPersistsEnabledQueryAndSend(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore: store,
		nodes: []nodes.Node{{
			ID:      "node-1",
			Name:    "radiant",
			AETitle: "RADIANT",
			Host:    "192.168.100.26",
			Port:    11112,
		}},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		col  int
		want func(nodes.Node) bool
	}{
		{"enabled", 0, func(node nodes.Node) bool { return node.Disabled }},
		{"query", 1, func(node nodes.Node) bool { return node.QueryDisabled }},
		{"send", 3, func(node nodes.Node) bool { return node.SendDisabled }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state.nodes[0] = nodes.Node{ID: "node-1", Name: "radiant", AETitle: "RADIANT", Host: "192.168.100.26", Port: 11112}
			if err := store.Save(state.nodes); err != nil {
				t.Fatal(err)
			}

			changed, err := toggleNodeOperationalCell(state, 0, tt.col)
			if err != nil {
				t.Fatal(err)
			}
			if !changed {
				t.Fatalf("toggle col %d did not report change", tt.col)
			}
			if !tt.want(state.nodes[0]) {
				t.Fatalf("state node after toggle = %+v", state.nodes[0])
			}
			list, err := store.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 || !tt.want(list[0]) {
				t.Fatalf("persisted nodes = %+v", list)
			}
		})
	}
}

func TestToggleNodeOperationalCellCyclesAndPersistsRetrieveMethod(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore: store,
		nodes: []nodes.Node{{
			ID:      "node-1",
			Name:    "radiant",
			AETitle: "RADIANT",
			Host:    "192.168.100.26",
			Port:    11112,
		}},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{nodes.RetrieveMethodMove, nodes.RetrieveMethodGet, ""} {
		changed, err := toggleNodeOperationalCell(state, 0, 2)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Fatal("retrieve method toggle did not report change")
		}
		if state.nodes[0].RetrieveMethod != want {
			t.Fatalf("RetrieveMethod = %q, want %q", state.nodes[0].RetrieveMethod, want)
		}
		list, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].RetrieveMethod != want {
			t.Fatalf("persisted nodes = %+v, want retrieve %q", list, want)
		}
	}
}

func TestToggleNodeOperationalCellIgnoresNonEditableColumns(t *testing.T) {
	state := &uiState{nodes: []nodes.Node{{Name: "radiant"}}}

	changed, err := toggleNodeOperationalCell(state, 0, 4)

	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("TLS column should not toggle without TLS support")
	}
	if state.nodes[0].Disabled || state.nodes[0].QueryDisabled || state.nodes[0].SendDisabled || state.nodes[0].RetrieveMethod != "" {
		t.Fatalf("node changed = %+v", state.nodes[0])
	}
}

func TestSelectedEnabledNodeSkipsDisabledSelection(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "disabled", Disabled: true},
			{Name: "enabled"},
		},
		selectedNodeRow: 0,
	}

	node, ok := selectedEnabledNode(state)

	if !ok || node.Name != "enabled" {
		t.Fatalf("selected enabled node = %+v/%t, want enabled/true", node, ok)
	}
}

func TestSelectedEnabledNodeRejectsAllDisabledNodes(t *testing.T) {
	state := &uiState{nodes: []nodes.Node{{Name: "disabled", Disabled: true}}, selectedNodeRow: 0}

	if node, ok := selectedEnabledNode(state); ok {
		t.Fatalf("selected enabled node = %+v/true, want false", node)
	}
}

func TestSelectedQueryNodeSkipsQueryDisabledSelection(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "disabled", QueryDisabled: true},
			{Name: "enabled"},
		},
		selectedNodeRow: 0,
	}

	node, ok := selectedQueryNode(state)

	if !ok || node.Name != "enabled" {
		t.Fatalf("selected query node = %+v/%t, want enabled/true", node, ok)
	}
}

func TestQuerySourceNodesReturnCheckedNodesInPriorityOrder(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "disabled", Disabled: true},
			{Name: "first"},
			{Name: "query-disabled", QueryDisabled: true},
			{Name: "second"},
		},
	}

	got := querySourceNodes(state)

	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Fatalf("query source nodes = %+v, want first and second", got)
	}
}

func TestAnnotateQueryMatchesAddsSourceNode(t *testing.T) {
	node := nodes.Node{ID: "node-1", Name: "RADIANT", AETitle: "RADIANTAE", Host: "192.168.100.26", Port: 11112}
	matches := annotateQueryMatches([]query.Match{{PatientName: "DOE^JANE"}}, node)

	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	match := matches[0]
	if match.SourceNodeID != "node-1" || match.SourceNodeName != "RADIANT" || match.SourceAETitle != "RADIANTAE" || match.SourceHost != "192.168.100.26" || match.SourcePort != 11112 {
		t.Fatalf("annotated match = %+v", match)
	}
}

func TestNodeForQueryMatchUsesMatchSourceBeforeCurrentSelection(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{ID: "node-1", Name: "selected"},
			{ID: "node-2", Name: "match-source"},
		},
		selectedNodeRow: 0,
	}

	node, ok := nodeForQueryMatch(state, query.Match{SourceNodeID: "node-2"})

	if !ok || node.Name != "match-source" {
		t.Fatalf("nodeForQueryMatch = %+v/%t, want match-source/true", node, ok)
	}
}

func TestNodeForQueryMatchFallsBackToSelectedQueryNode(t *testing.T) {
	state := &uiState{
		nodes:           []nodes.Node{{Name: "selected"}, {Name: "query-disabled", QueryDisabled: true}},
		selectedNodeRow: 0,
	}

	node, ok := nodeForQueryMatch(state, query.Match{})

	if !ok || node.Name != "selected" {
		t.Fatalf("nodeForQueryMatch fallback = %+v/%t, want selected/true", node, ok)
	}
}

func TestRunQueryAcrossSourcesAnnotatesAndPreservesSourceOrder(t *testing.T) {
	sources := []nodes.Node{
		{ID: "node-1", Name: "first", Host: "10.0.0.1", Port: 104},
		{ID: "node-2", Name: "second", Host: "10.0.0.2", Port: 105},
	}
	var calls []string

	result, err := runQueryAcrossSources(context.Background(), sources, func(_ context.Context, node nodes.Node) (query.Result, error) {
		calls = append(calls, node.Name)
		return query.Result{Matches: []query.Match{{PatientName: node.Name}}, FinalStatus: 0x0000}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "|") != "first|second" {
		t.Fatalf("calls = %#v, want first then second", calls)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(result.Matches))
	}
	if result.Matches[0].PatientName != "first" || result.Matches[0].SourceNodeID != "node-1" {
		t.Fatalf("first match = %+v", result.Matches[0])
	}
	if result.Matches[1].PatientName != "second" || result.Matches[1].SourceNodeID != "node-2" {
		t.Fatalf("second match = %+v", result.Matches[1])
	}
}

func TestRunQueryAcrossSourcesReportsSourceErrors(t *testing.T) {
	_, err := runQueryAcrossSources(context.Background(), []nodes.Node{{Name: "RADIANT"}}, func(context.Context, nodes.Node) (query.Result, error) {
		return query.Result{}, errors.New("association failed")
	})

	if err == nil || !strings.Contains(err.Error(), "RADIANT") || !strings.Contains(err.Error(), "association failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunQueryAcrossSourcesContinuesAfterSourceError(t *testing.T) {
	sources := []nodes.Node{
		{ID: "node-1", Name: "offline"},
		{ID: "node-2", Name: "online"},
	}
	var calls []string

	result, err := runQueryAcrossSources(context.Background(), sources, func(_ context.Context, node nodes.Node) (query.Result, error) {
		calls = append(calls, node.Name)
		if node.Name == "offline" {
			return query.Result{}, errors.New("association failed")
		}
		return query.Result{Matches: []query.Match{{PatientName: "DOE^JANE"}}, FinalStatus: 0x0000}, nil
	})

	if strings.Join(calls, "|") != "offline|online" {
		t.Fatalf("calls = %#v, want both sources attempted", calls)
	}
	if err == nil || !strings.Contains(err.Error(), "offline") || !strings.Contains(err.Error(), "association failed") {
		t.Fatalf("error = %v", err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1 partial match", len(result.Matches))
	}
	match := result.Matches[0]
	if match.PatientName != "DOE^JANE" || match.SourceNodeID != "node-2" || match.SourceNodeName != "online" {
		t.Fatalf("partial match = %+v", match)
	}
}

func TestRunQueryAcrossSourcesReportsAllSourceErrors(t *testing.T) {
	sources := []nodes.Node{{Name: "first"}, {Name: "second"}}
	var calls []string

	result, err := runQueryAcrossSources(context.Background(), sources, func(_ context.Context, node nodes.Node) (query.Result, error) {
		calls = append(calls, node.Name)
		return query.Result{}, errors.New(node.Name + " unavailable")
	})

	if strings.Join(calls, "|") != "first|second" {
		t.Fatalf("calls = %#v, want both sources attempted", calls)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("len(matches) = %d, want 0", len(result.Matches))
	}
	if err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueryFailureWithoutResultsRequiresErrorAndNoMatches(t *testing.T) {
	if !queryFailureWithoutResults(query.Result{}, errors.New("all sources failed")) {
		t.Fatal("empty errored query should be a hard failure")
	}
	if queryFailureWithoutResults(query.Result{Matches: []query.Match{{PatientName: "DOE^JANE"}}}, errors.New("one source failed")) {
		t.Fatal("errored query with partial matches should not be a hard failure")
	}
	if queryFailureWithoutResults(query.Result{}, &querySourceFailures{successes: 1, failures: []string{"offline: association failed"}}) {
		t.Fatal("errored query with a successful zero-match source should not be a hard failure")
	}
	if queryFailureWithoutResults(query.Result{}, nil) {
		t.Fatal("empty successful query should not be a hard failure")
	}
}

func TestQueryCompletionStatusReportsPartialFailures(t *testing.T) {
	result := query.Result{
		Matches:     []query.Match{{PatientName: "DOE^JANE"}},
		FinalStatus: 0x0000,
		Duration:    25 * time.Millisecond,
	}

	text := queryCompletionStatus("C-FIND", "2 sources", result, errors.New("offline: association failed"))

	for _, want := range []string{"C-FIND 2 sources returned 1 matches", "final=0x0000", "partial failure", "offline"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status %q missing %q", text, want)
		}
	}
}

func TestRecordQuerySourceStatusesMarksPartialFailure(t *testing.T) {
	nodesList := []nodes.Node{
		{ID: "node-1", Name: "offline", Host: "10.0.0.1", Port: 104},
		{ID: "node-2", Name: "online", Host: "10.0.0.2", Port: 105},
	}
	state := &uiState{nodes: nodesList}
	err := &querySourceFailures{
		successes: 1,
		failures:  []string{"offline: association failed"},
		failedKeys: map[string]bool{
			nodeVerifyKey(nodesList[0]): true,
		},
	}

	recordQuerySourceStatuses(state, nodesList, err)

	if querySourceStatusMarker(state, nodesList[0]) != "Q! " {
		t.Fatalf("failed status marker = %q", querySourceStatusMarker(state, nodesList[0]))
	}
	if querySourceStatusMarker(state, nodesList[1]) != "Q✓ " {
		t.Fatalf("successful status marker = %q", querySourceStatusMarker(state, nodesList[1]))
	}
}

func TestSelectedSendNodeSkipsSendDisabledSelection(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "disabled", SendDisabled: true},
			{Name: "enabled"},
		},
		selectedNodeRow: 0,
	}

	node, ok := selectedSendNode(state)

	if !ok || node.Name != "enabled" {
		t.Fatalf("selected send node = %+v/%t, want enabled/true", node, ok)
	}
}

func TestRetrieveReceiverAddressIssueFlagsLoopbackForRemoteNode(t *testing.T) {
	state := &uiState{appConfig: appconfig.Config{ReceiverAddress: "127.0.0.1:11113"}}
	node := nodes.Node{Name: "radiant", Host: "192.168.100.26"}

	issue := retrieveReceiverAddressIssue(state, node)

	if !strings.Contains(issue, "127.0.0.1:11113") || !strings.Contains(issue, "remote node radiant") {
		t.Fatalf("issue = %q", issue)
	}
}

func TestRetrieveReceiverAddressIssueAllowsWildcardForRemoteNode(t *testing.T) {
	state := &uiState{appConfig: appconfig.Config{ReceiverAddress: "0.0.0.0:11113"}}
	node := nodes.Node{Name: "radiant", Host: "192.168.100.26"}

	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		t.Fatalf("issue = %q, want empty", issue)
	}
}

func TestReceiverAddressPartsSplitsHostAndPort(t *testing.T) {
	host, port := receiverAddressParts("0.0.0.0:11113")

	if host != "0.0.0.0" || port != "11113" {
		t.Fatalf("parts = %q/%q, want 0.0.0.0/11113", host, port)
	}
}

func TestReceiverAddressPartsUsesDefaultsForHostOnly(t *testing.T) {
	host, port := receiverAddressParts("192.168.100.10")

	if host != "192.168.100.10" || port != "11112" {
		t.Fatalf("parts = %q/%q, want host/default port", host, port)
	}
}

func TestReceiverAddressFromPartsValidatesAndJoins(t *testing.T) {
	address, err := receiverAddressFromParts("0.0.0.0", "11113")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if address != "0.0.0.0:11113" {
		t.Fatalf("address = %q, want 0.0.0.0:11113", address)
	}

	if _, err := receiverAddressFromParts("0.0.0.0", "70000"); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestListenerAddressSummaryIncludesHostnameAndAddresses(t *testing.T) {
	lines := listenerAddressSummaryLines("mac-mini", []string{"192.168.100.10", "10.0.0.5"}, "11113")
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"Host Name: mac-mini",
		"192.168.100.10:11113",
		"10.0.0.5:11113",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("summary missing %q in:\n%s", want, joined)
		}
	}
}

func TestListenerAddressSummaryShowsFallback(t *testing.T) {
	lines := listenerAddressSummaryLines("", nil, "11113")

	if strings.Join(lines, "\n") != "Host Name: -\nReachable Addresses: -" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestListenerReachableEndpointsTrimsAndDefaultsPort(t *testing.T) {
	endpoints := listenerReachableEndpoints([]string{" 192.168.100.10 ", "", "10.0.0.5"}, "")

	if strings.Join(endpoints, "|") != "192.168.100.10:11112|10.0.0.5:11112" {
		t.Fatalf("endpoints = %#v", endpoints)
	}
}

func TestListenerSettingsStatusTextShowsStoppedAndPendingStates(t *testing.T) {
	stopped := listenerSettingsStatusText("GOPACS", "0.0.0.0", "11113", false, nil)
	if stopped != "Stopped: GOPACS on 0.0.0.0:11113" {
		t.Fatalf("stopped status = %q", stopped)
	}

	pending := listenerSettingsStatusText("GOPACS", "0.0.0.0", "11113", true, nil)
	if pending != "Will start on Save: GOPACS on 0.0.0.0:11113" {
		t.Fatalf("pending status = %q", pending)
	}
}

func TestListenerSettingsStatusTextShowsRunningSnapshot(t *testing.T) {
	snapshot := receive.Snapshot{AETitle: "GOPACS", Address: "127.0.0.1:11113", Stored: 7}

	status := listenerSettingsStatusText("IGNORED", "0.0.0.0", "11112", false, &snapshot)

	if status != "Running: GOPACS on 127.0.0.1:11113; stored 7 objects" {
		t.Fatalf("running status = %q", status)
	}
}

func TestArchiveAlbumLinesSummarizeStudyCounts(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	studies := []archive.Study{
		{Modalities: "CT", ImportedAt: now},
		{Modalities: "MR", ImportedAt: now.Add(-2 * time.Hour)},
		{Modalities: "CT\\SR", ImportedAt: now.AddDate(0, 0, -2)},
	}

	lines := archiveAlbumLines(studies, now)
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"Database                         3",
		"Just Acquired (last hour)        1",
		"Today                            2",
		"Today CT                         1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("album lines missing %q in:\n%s", want, joined)
		}
	}
}

func TestArchiveAlbumRowsMarkSelectedFilterableAlbum(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	studies := []archive.Study{
		{Modalities: "CT", ImportedAt: now},
		{Modalities: "MR", ImportedAt: now.Add(-2 * time.Hour)},
	}

	rows := archiveAlbumRows(studies, now, archiveAlbumToday)

	if rows[0].Text != "  Database                         2" {
		t.Fatalf("database row text = %q", rows[0].Text)
	}
	if rows[5].Text != "▶ Today                            2" {
		t.Fatalf("selected today row text = %q", rows[5].Text)
	}
	if !rows[5].Filterable {
		t.Fatalf("today row should be filterable")
	}
	if rows[1].Filterable {
		t.Fatalf("cases with comments row should not be filterable before comments are implemented")
	}
}

func TestArchiveAlbumFilterForTodayCT(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)

	filters, ok := archiveAlbumFilters(archiveAlbumTodayCT, now)

	if !ok {
		t.Fatalf("Today CT album should produce filters")
	}
	if filters.ImportedAtFrom == "" || filters.ImportedAtTo == "" {
		t.Fatalf("today filter did not set imported-at bounds: %#v", filters)
	}
	if strings.Join(filters.Modalities, ",") != "CT" {
		t.Fatalf("Modalities = %#v, want CT", filters.Modalities)
	}
}

func TestArchiveAlbumFilterForDatabaseClearsFilters(t *testing.T) {
	filters, ok := archiveAlbumFilters(archiveAlbumDatabase, time.Now())

	if !ok {
		t.Fatalf("database album should be filterable")
	}
	if filters.PatientName != "" ||
		filters.PatientID != "" ||
		filters.AccessionNumber != "" ||
		filters.StudyDescription != "" ||
		filters.StudyDateFrom != "" ||
		filters.StudyDateTo != "" ||
		filters.ImportedAtFrom != "" ||
		filters.ImportedAtTo != "" ||
		len(filters.Modalities) != 0 ||
		filters.SourcePath != "" {
		t.Fatalf("database filters = %#v, want empty filters", filters)
	}
}

func TestArchiveFiltersWithAlbumPreservesUserSearchFields(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	base := archive.StudyFilters{
		PatientName:      "DOE",
		PatientID:        "P123",
		AccessionNumber:  "A100",
		StudyDescription: "abdomen",
		StudyDateFrom:    "20260601",
		StudyDateTo:      "20260630",
		ImportedAtFrom:   "old-from",
		ImportedAtTo:     "old-to",
		Modalities:       []string{"MR"},
		SourcePath:       "incoming",
	}

	filters, ok := archiveFiltersWithAlbum(base, archiveAlbumTodayCT, now)

	if !ok {
		t.Fatalf("Today CT album should be filterable")
	}
	if filters.PatientName != "DOE" ||
		filters.PatientID != "P123" ||
		filters.AccessionNumber != "A100" ||
		filters.StudyDescription != "abdomen" ||
		filters.StudyDateFrom != "20260601" ||
		filters.StudyDateTo != "20260630" ||
		filters.SourcePath != "incoming" {
		t.Fatalf("user search fields were not preserved: %#v", filters)
	}
	if filters.ImportedAtFrom == "old-from" || filters.ImportedAtTo == "old-to" {
		t.Fatalf("album imported-at bounds were not applied: %#v", filters)
	}
	if strings.Join(filters.Modalities, ",") != "CT" {
		t.Fatalf("Modalities = %#v, want CT", filters.Modalities)
	}
}

func TestArchiveSourceLinesIncludeLocalAndRemoteNodes(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		appConfig: appconfig.Config{LocalAETitle: "GOPACS", ReceiverAddress: "0.0.0.0:11113"},
	}

	lines := archiveSourceLines(state)
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"▣ Documents DB",
		"● Receiver GOPACS stopped",
		"◉ RADIANT 192.168.100.26:11112",
		"◉ HOROSMINI 192.168.100.50:4007",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("source lines missing %q in:\n%s", want, joined)
		}
	}
}

func TestArchiveSourceLinesMarksSelectedRemoteNode(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: 1,
		appConfig:       appconfig.Config{LocalAETitle: "GOPACS", ReceiverAddress: "0.0.0.0:11113"},
	}

	lines := archiveSourceLines(state)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "  ◉ RADIANT 192.168.100.26:11112") {
		t.Fatalf("unselected node line missing in:\n%s", joined)
	}
	if !strings.Contains(joined, "▶ ◉ HOROSMINI 192.168.100.50:4007") {
		t.Fatalf("selected node marker missing in:\n%s", joined)
	}
}

func TestArchiveSummaryTextShowsSelectedStudySeriesAndInstances(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{
			PatientName:      "DOE^JANE",
			PatientID:        "P123",
			StudyDate:        "20260531",
			StudyDescription: "CT Abdomen",
			Modalities:       "CT",
			AccessionNumber:  "A100",
			SeriesCount:      2,
			InstanceCount:    42,
		}},
		series: []archive.Series{{
			Modality:          "CT",
			SeriesNumber:      "4",
			SeriesDescription: "Portal Venous",
			InstanceCount:     40,
		}},
		instances: []archive.Instance{{SOPInstanceUID: "1.2.3"}},
	}
	state.selectedStudyRow = 0
	state.selectedSeriesRow = 0

	text := archiveSummaryText(state)

	for _, want := range []string{
		"DOE^JANE",
		"Patient ID: P123",
		"Study: 20260531 CT",
		"Accession: A100",
		"Series: 2",
		"Images: 42",
		"Selected series",
		"4 CT Portal Venous",
		"Loaded images: 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary text missing %q in:\n%s", want, text)
		}
	}
}

func TestArchiveSummaryTextListsSamePatientStudies(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260501", StudyDescription: "CT Chest", Modalities: "CT", InstanceCount: 42},
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260531", StudyDescription: "MR Brain", Modalities: "MR", InstanceCount: 8},
			{PatientName: "SMITH^JOHN", PatientID: "P999", StudyDate: "20260530", StudyDescription: "US Abdomen", Modalities: "US", InstanceCount: 3},
		},
	}
	state.selectedStudyRow = 1

	text := archiveSummaryText(state)

	for _, want := range []string{
		"Patient studies",
		"20260531 MR MR Brain 8 images",
		"20260501 CT CT Chest 42 images",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary text missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "SMITH") || strings.Contains(text, "US Abdomen") {
		t.Fatalf("summary text included another patient:\n%s", text)
	}
}

func TestArchiveResultSummaryTextCountsVisibleArchive(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", SeriesCount: 2, InstanceCount: 42},
			{PatientName: "DOE^JANE", PatientID: "P123", SeriesCount: 1, InstanceCount: 8},
			{PatientName: "SMITH^JOHN", PatientID: "P999", SeriesCount: 1, InstanceCount: 3},
		},
	}

	got := archiveResultSummaryText(state)

	want := "2 patients, 3 studies, 4 series, 53 images"
	if got != want {
		t.Fatalf("archive result summary = %q, want %q", got, want)
	}
}

func TestArchiveBrowserRowsGroupStudiesByPatient(t *testing.T) {
	studies := []archive.Study{
		{PatientName: "DOE^JANE", PatientID: "P123", Modalities: "CT", SeriesCount: 2, InstanceCount: 42},
		{PatientName: "DOE^JANE", PatientID: "P123", Modalities: "MR", SeriesCount: 1, InstanceCount: 8},
		{PatientName: "SMITH^JOHN", PatientID: "P999", Modalities: "US", SeriesCount: 1, InstanceCount: 3},
	}

	rows := archiveBrowserRows(studies)

	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want 5", len(rows))
	}
	if rows[0].kind != archiveRowPatient || rows[0].studyIndex != -1 {
		t.Fatalf("first row = %#v, want patient header", rows[0])
	}
	if rows[1].kind != archiveRowStudy || rows[1].studyIndex != 0 {
		t.Fatalf("second row = %#v, want first study", rows[1])
	}
	if rows[2].kind != archiveRowStudy || rows[2].studyIndex != 1 {
		t.Fatalf("third row = %#v, want second study", rows[2])
	}
	if rows[3].kind != archiveRowPatient || rows[4].studyIndex != 2 {
		t.Fatalf("last patient group rows = %#v %#v", rows[3], rows[4])
	}
	if rows[0].seriesCount != 3 || rows[0].instanceCount != 50 {
		t.Fatalf("patient aggregate = series %d instances %d, want 3/50", rows[0].seriesCount, rows[0].instanceCount)
	}
}

func TestArchiveBrowserRowsKeepPatientStudiesContiguousWhenInputIsInterleaved(t *testing.T) {
	studies := []archive.Study{
		{PatientName: "DOE^JANE", PatientID: "P123", StudyDescription: "CT"},
		{PatientName: "SMITH^JOHN", PatientID: "P999", StudyDescription: "US"},
		{PatientName: "DOE^JANE", PatientID: "P123", StudyDescription: "MR"},
	}

	rows := archiveBrowserRows(studies)

	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want 5", len(rows))
	}
	if rows[0].kind != archiveRowPatient || rows[1].studyIndex != 0 || rows[2].studyIndex != 2 {
		t.Fatalf("first patient group rows = %#v %#v %#v", rows[0], rows[1], rows[2])
	}
	if rows[3].kind != archiveRowPatient || rows[4].studyIndex != 1 {
		t.Fatalf("second patient group rows = %#v %#v", rows[3], rows[4])
	}
}

func TestArchiveBrowserRowsHideCollapsedPatientStudies(t *testing.T) {
	studies := []archive.Study{
		{PatientName: "DOE^JANE", PatientID: "P123", StudyDescription: "CT"},
		{PatientName: "DOE^JANE", PatientID: "P123", StudyDescription: "MR"},
		{PatientName: "SMITH^JOHN", PatientID: "P999", StudyDescription: "US"},
	}
	collapsed := map[string]bool{archivePatientKey(studies[0]): true}

	rows := archiveBrowserRowsWithCollapse(studies, collapsed)

	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].kind != archiveRowPatient || !rows[0].collapsed {
		t.Fatalf("first row = %#v, want collapsed patient", rows[0])
	}
	if rows[1].kind != archiveRowPatient || rows[2].studyIndex != 2 {
		t.Fatalf("remaining rows = %#v %#v, want second patient and study", rows[1], rows[2])
	}
	if got := archiveBrowserCell(rows[0], studies, 0); got != "▸ DOE^JANE" {
		t.Fatalf("collapsed patient cell = %q", got)
	}
}

func TestToggleArchivePatientGroupRebuildsRows(t *testing.T) {
	state := &uiState{studies: []archive.Study{
		{PatientName: "DOE^JANE", PatientID: "P123", StudyDescription: "CT"},
		{PatientName: "DOE^JANE", PatientID: "P123", StudyDescription: "MR"},
	}}
	state.archiveRows = archiveBrowserRows(state.studies)

	if !toggleArchivePatientGroup(state, state.archiveRows[0]) {
		t.Fatal("toggle returned false, want true")
	}
	if len(state.archiveRows) != 1 || !state.archiveRows[0].collapsed {
		t.Fatalf("collapsed rows = %#v", state.archiveRows)
	}
	if !toggleArchivePatientGroup(state, state.archiveRows[0]) {
		t.Fatal("second toggle returned false, want true")
	}
	if len(state.archiveRows) != 3 || state.archiveRows[0].collapsed {
		t.Fatalf("expanded rows = %#v", state.archiveRows)
	}
}

func TestArchiveBrowserCellRendersPatientAndIndentedStudyRows(t *testing.T) {
	studies := []archive.Study{{
		PatientName:      "DOE^JANE",
		PatientID:        "P123",
		PatientBirthDate: "19700102",
		StudyDate:        "20260531",
		StudyDescription: "CT Abdomen",
		Modalities:       "CT",
		InstitutionName:  "General Hospital",
		SeriesCount:      2,
		InstanceCount:    42,
	}}
	rows := archiveBrowserRows(studies)

	if got := archiveBrowserCell(rows[0], studies, 0); got != "▾ DOE^JANE" {
		t.Fatalf("patient cell = %q", got)
	}
	if got := archiveBrowserCell(rows[0], studies, archiveStudyTableColumnSeries); got != "2" {
		t.Fatalf("patient series cell = %q, want 2", got)
	}
	if got := archiveBrowserCell(rows[1], studies, 0); got != "  ▸ CT Abdomen" {
		t.Fatalf("study cell = %q", got)
	}
	if got := archiveBrowserCell(rows[1], studies, archiveStudyTableColumnStudyDate); got != "20260531" {
		t.Fatalf("study date cell = %q", got)
	}
	if got := archiveBrowserCell(rows[1], studies, archiveStudyTableColumnDOB); got != "19700102" {
		t.Fatalf("study DOB cell = %q, want 19700102", got)
	}
}

func TestArchiveTableHeadersIncludeHorosDisplayFields(t *testing.T) {
	headers := strings.Join(archiveTableHeaders(), "|")

	for _, want := range []string{"DOB", "Time", "Added", "Institution", "Status", "Comments"} {
		if !strings.Contains(headers, want) {
			t.Fatalf("headers missing %q in %q", want, headers)
		}
	}
}

func TestStudyCellShowsHorosDisplayFields(t *testing.T) {
	study := archive.Study{
		PatientBirthDate: "19700102",
		InstitutionName:  "General Hospital",
		StudyTime:        "134501",
		ImportedAt:       time.Date(2026, 6, 4, 13, 45, 0, 0, time.UTC),
	}

	if got := studyCell(study, archiveStudyTableColumnDOB); got != "19700102" {
		t.Fatalf("DOB cell = %q, want 19700102", got)
	}
	if got := studyCell(study, archiveStudyTableColumnTime); got != "13:45:01" {
		t.Fatalf("time cell = %q, want 13:45:01", got)
	}
	if got := studyCell(study, archiveStudyTableColumnAdded); got != "2026-06-04 13:45" {
		t.Fatalf("added cell = %q, want 2026-06-04 13:45", got)
	}
	if got := studyCell(study, archiveStudyTableColumnInstitution); got != "General Hospital" {
		t.Fatalf("institution cell = %q, want General Hospital", got)
	}
	if got := studyCell(study, archiveStudyTableColumnStatus); got != "" {
		t.Fatalf("status cell = %q, want empty placeholder", got)
	}
	if got := studyCell(study, archiveStudyTableColumnComments); got != "" {
		t.Fatalf("comments cell = %q, want empty placeholder", got)
	}
}

func TestArchiveBrowserRowsIncludeLoadedSeriesBelowStudy(t *testing.T) {
	studies := []archive.Study{{
		PatientName:      "DOE^JANE",
		PatientID:        "P123",
		StudyInstanceUID: "1.2.3",
		StudyDescription: "CT Abdomen",
		Modalities:       "CT",
		SeriesCount:      2,
		InstanceCount:    42,
	}}
	loadedSeries := map[string][]archive.Series{
		"1.2.3": {
			{SeriesNumber: "4", Modality: "CT", SeriesDescription: "Portal Venous", InstanceCount: 40},
			{SeriesNumber: "5", Modality: "SR", SeriesDescription: "Dose Report", InstanceCount: 1},
		},
	}

	rows := archiveBrowserRowsWithInlineSeries(studies, nil, loadedSeries)

	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want patient, study, and 2 series rows", len(rows))
	}
	if rows[2].kind != archiveRowSeries || rows[2].studyIndex != 0 || rows[2].seriesIndex != 0 {
		t.Fatalf("first series row = %#v", rows[2])
	}
	if rows[3].kind != archiveRowSeries || rows[3].seriesIndex != 1 {
		t.Fatalf("second series row = %#v", rows[3])
	}
}

func TestArchiveBrowserRowsHideCollapsedLoadedStudySeries(t *testing.T) {
	studies := []archive.Study{{
		StudyInstanceUID: "1.2.3",
		StudyDescription: "CT Abdomen",
		SeriesCount:      1,
	}}
	loadedSeries := map[string][]archive.Series{
		"1.2.3": {{SeriesDescription: "Portal Venous"}},
	}
	collapsedStudies := map[string]bool{"1.2.3": true}

	rows := archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(studies, nil, loadedSeries, collapsedStudies)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want patient and collapsed study only", len(rows))
	}
	if rows[1].kind != archiveRowStudy || !rows[1].studyHasSeries || rows[1].studySeriesLoaded {
		t.Fatalf("study row = %#v, want collapsed loaded study", rows[1])
	}
	if got := archiveBrowserCell(rows[1], studies, 0); got != "  ▸ CT Abdomen" {
		t.Fatalf("collapsed study cell = %q", got)
	}
}

func TestToggleArchiveStudySeriesCollapsesAndExpandsLoadedRows(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{
			StudyInstanceUID: "1.2.3",
			StudyDescription: "CT Abdomen",
			SeriesCount:      1,
		}},
		archiveSeriesByStudy: map[string][]archive.Series{
			"1.2.3": {{SeriesDescription: "Portal Venous"}},
		},
	}
	state.archiveRows = archiveBrowserRowsWithInlineSeries(state.studies, nil, state.archiveSeriesByStudy)

	if !toggleArchiveStudySeries(state, state.archiveRows[1]) {
		t.Fatal("first toggle returned false, want true")
	}
	if !state.collapsedArchiveStudies["1.2.3"] || len(state.archiveRows) != 2 {
		t.Fatalf("collapsed state = %#v rows=%#v", state.collapsedArchiveStudies, state.archiveRows)
	}
	if !toggleArchiveStudySeries(state, state.archiveRows[1]) {
		t.Fatal("second toggle returned false, want true")
	}
	if state.collapsedArchiveStudies["1.2.3"] || len(state.archiveRows) != 3 || state.archiveRows[2].kind != archiveRowSeries {
		t.Fatalf("expanded state = %#v rows=%#v", state.collapsedArchiveStudies, state.archiveRows)
	}
}

func TestArchiveBrowserCellRendersInlineSeriesRows(t *testing.T) {
	studies := []archive.Study{{StudyInstanceUID: "1.2.3", StudyDescription: "CT Abdomen", SeriesCount: 1}}
	series := archive.Series{
		SeriesNumber:      "4",
		Modality:          "CT",
		SeriesDescription: "Portal Venous",
		InstanceCount:     40,
		SeriesInstanceUID: "9.8.7",
	}
	rows := archiveBrowserRowsWithInlineSeries(studies, nil, map[string][]archive.Series{"1.2.3": {series}})

	if got := archiveBrowserCell(rows[1], studies, 0); got != "  ▾ CT Abdomen" {
		t.Fatalf("loaded study cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, 0); got != "      Portal Venous" {
		t.Fatalf("series description cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnModality); got != "CT" {
		t.Fatalf("series modality cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnInstances); got != "40" {
		t.Fatalf("series instance count cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnStudyUID); got != "9.8.7" {
		t.Fatalf("series UID cell = %q", got)
	}
}

func TestArchiveTableCellAppliesHeaderAndPatientStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyArchiveTableCell(cell, 0, "Patient", archiveBrowserRow{}, true, false)

	if cell.label.Text != "Patient" {
		t.Fatalf("header text = %q", cell.label.Text)
	}
	if !cell.label.TextStyle.Bold {
		t.Fatal("header label was not bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveHeaderRowColor {
		t.Fatalf("header fill = %#v, want %#v", got, archiveHeaderRowColor)
	}

	applyArchiveTableCell(cell, 1, "▾ DOE^JANE", archiveBrowserRow{kind: archiveRowPatient}, false, false)

	if !cell.label.TextStyle.Bold {
		t.Fatal("patient label was not bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archivePatientRowColor {
		t.Fatalf("patient fill = %#v, want %#v", got, archivePatientRowColor)
	}
}

func TestArchiveTableCellUsesStripedStudyAndSeriesStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyArchiveTableCell(cell, 2, "   CT Abdomen", archiveBrowserRow{kind: archiveRowStudy}, false, false)
	studyFill := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA)
	if cell.label.TextStyle.Bold {
		t.Fatal("study label should not be bold")
	}
	if studyFill != archiveEvenRowColor {
		t.Fatalf("study fill = %#v, want %#v", studyFill, archiveEvenRowColor)
	}

	applyArchiveTableCell(cell, 3, "      Portal Venous", archiveBrowserRow{kind: archiveRowSeries}, false, false)
	seriesFill := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA)
	if seriesFill != archiveSeriesRowColor {
		t.Fatalf("series fill = %#v, want %#v", seriesFill, archiveSeriesRowColor)
	}
}

func TestArchiveSelectedTableCellUsesSelectionStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyArchiveTableCell(cell, 2, "   CT Abdomen", archiveBrowserRow{kind: archiveRowStudy}, false, true)

	if cell.label.TextStyle.Bold {
		t.Fatal("selected archive study text should not become bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected archive fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
}

func TestQueryTableCellUsesHeaderAndStripedStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyQueryTableCell(cell, 0, 0, "Patient", true, false)

	if cell.label.Text != "Patient" {
		t.Fatalf("header text = %q", cell.label.Text)
	}
	if !cell.label.TextStyle.Bold {
		t.Fatal("query header should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveHeaderRowColor {
		t.Fatalf("header fill = %#v, want %#v", got, archiveHeaderRowColor)
	}

	applyQueryTableCell(cell, 1, 2, "DOE^JANE", false, false)
	if cell.label.TextStyle.Bold {
		t.Fatal("query row should not be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveOddRowColor {
		t.Fatalf("query odd row fill = %#v, want %#v", got, archiveOddRowColor)
	}
}

func TestQueryRetrieveTableCellUsesActionStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyQueryTableCell(cell, 1, queryRetrieveColumn, "↓", false, false)

	if cell.label.Alignment != fyne.TextAlignCenter {
		t.Fatalf("retrieve alignment = %v, want center", cell.label.Alignment)
	}
	if !cell.label.TextStyle.Bold {
		t.Fatal("retrieve indicator should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != queryRetrieveActionRowColor {
		t.Fatalf("retrieve fill = %#v, want %#v", got, queryRetrieveActionRowColor)
	}
}

func TestQuerySelectedTableCellUsesSelectionStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyQueryTableCell(cell, 2, 2, "DOE^JANE", false, true)

	if cell.label.TextStyle.Bold {
		t.Fatal("selected query row text should not become bold")
	}
	if cell.label.Alignment != fyne.TextAlignLeading {
		t.Fatalf("selected alignment = %v, want leading", cell.label.Alignment)
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
}

func TestQueryTableHeadersIncludeRetrieveIndicator(t *testing.T) {
	headers := queryTableHeaders()

	if len(headers) == 0 || headers[0] != "Retrieve" {
		t.Fatalf("first query header = %q, want Retrieve", strings.Join(headers, "|"))
	}
}

func TestQueryTableHeadersIncludeHorosDisplayFields(t *testing.T) {
	headers := strings.Join(queryTableHeaders(), "|")

	for _, want := range []string{"DOB", "Time", "Images", "Referrer", "Institution", "Source"} {
		if !strings.Contains(headers, want) {
			t.Fatalf("headers missing %q in %q", want, headers)
		}
	}
}

func TestQueryCellShowsRetrieveIndicatorWhenStudyUIDIsAvailable(t *testing.T) {
	match := query.Match{StudyInstanceUID: "1.2.3"}

	if got := queryCell(match, 0); got != "↓" {
		t.Fatalf("retrieve indicator = %q, want down arrow", got)
	}

	if got := queryCell(query.Match{}, 0); got != "" {
		t.Fatalf("empty retrieve indicator = %q, want empty", got)
	}
}

func TestQueryCellFormatsRetrieveLevelAsHierarchy(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"PATIENT", "▾ PATIENT"},
		{"STUDY", "  ▸ STUDY"},
		{"SERIES", "    ▸ SERIES"},
		{"IMAGE", "      IMAGE"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			match := query.Match{QueryRetrieveLevel: tt.level}
			if got := queryCell(match, queryTableColumnLevel); got != tt.want {
				t.Fatalf("level cell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryCellShowsHorosDisplayFields(t *testing.T) {
	match := query.Match{
		PatientBirthDate:       "19700102",
		StudyTime:              "134501",
		ImageCount:             "42",
		ReferringPhysicianName: "REFER^DOC",
		InstitutionName:        "General Hospital",
		PatientComments:        "remote comment",
		StudyStatusID:          "VERIFIED",
	}

	if got := queryCell(match, queryTableColumnDOB); got != "19700102" {
		t.Fatalf("DOB cell = %q, want 19700102", got)
	}
	if got := queryCell(match, queryTableColumnTime); got != "13:45:01" {
		t.Fatalf("time cell = %q, want 13:45:01", got)
	}
	if got := queryCell(match, queryTableColumnImages); got != "42" {
		t.Fatalf("images cell = %q, want 42", got)
	}
	if got := queryCell(match, queryTableColumnReferrer); got != "REFER^DOC" {
		t.Fatalf("referrer cell = %q, want REFER^DOC", got)
	}
	if got := queryCell(match, queryTableColumnInstitution); got != "General Hospital" {
		t.Fatalf("institution cell = %q, want General Hospital", got)
	}
	if got := queryCell(match, queryTableColumnLocalComments); got != "" {
		t.Fatalf("local comments cell = %q, want empty placeholder", got)
	}
	if got := queryCell(match, queryTableColumnServerComments); got != "remote comment" {
		t.Fatalf("server comments cell = %q, want remote comment", got)
	}
	if got := queryCell(match, queryTableColumnStudyStatus); got != "VERIFIED" {
		t.Fatalf("study status cell = %q, want VERIFIED", got)
	}
}

func TestQueryTableHeadersIncludeLocalAndServerComments(t *testing.T) {
	headers := strings.Join(queryTableHeaders(), "|")

	for _, want := range []string{"Local Comments", "Server Comments"} {
		if !strings.Contains(headers, want) {
			t.Fatalf("headers missing %q in %q", want, headers)
		}
	}
}

func TestQueryCellFormatsStatusWithAvailabilityMarker(t *testing.T) {
	tests := []struct {
		name   string
		status uint16
		want   string
	}{
		{"success", 0x0000, "● 0x0000"},
		{"pending", 0xFF00, "● 0xFF00"},
		{"pending warning", 0xFF01, "● 0xFF01"},
		{"failure", 0xA700, "! 0xA700"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := query.Match{Status: tt.status}
			if got := queryCell(match, queryTableColumnStatus); got != tt.want {
				t.Fatalf("status cell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryCellShowsSourceNode(t *testing.T) {
	match := query.Match{SourceNodeName: "RADIANT", SourceHost: "192.168.100.26", SourcePort: 11112}

	if got := queryCell(match, queryTableColumnSource); got != "RADIANT / 192.168.100.26:11112" {
		t.Fatalf("source cell = %q", got)
	}
}

func TestQueryTableSelectionActionMarksRetrieveColumn(t *testing.T) {
	tests := []struct {
		name         string
		id           widget.TableCellID
		wantRow      int
		wantRetrieve bool
		wantOK       bool
	}{
		{name: "header", id: widget.TableCellID{Row: 0, Col: 0}, wantRow: -1, wantOK: false},
		{name: "normal cell", id: widget.TableCellID{Row: 3, Col: 2}, wantRow: 2, wantOK: true},
		{name: "retrieve cell", id: widget.TableCellID{Row: 2, Col: 0}, wantRow: 1, wantRetrieve: true, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, retrieve, ok := queryTableSelectionAction(tt.id)

			if row != tt.wantRow || retrieve != tt.wantRetrieve || ok != tt.wantOK {
				t.Fatalf("selection action = row %d retrieve %v ok %v, want row %d retrieve %v ok %v", row, retrieve, ok, tt.wantRow, tt.wantRetrieve, tt.wantOK)
			}
		})
	}
}

func TestQueryTableRetrieveColumnInvokesRetrieveAction(t *testing.T) {
	state := &uiState{
		queries: []query.Match{{PatientName: "DOE^JANE", StudyInstanceUID: "1.2.3"}},
	}
	retrieveCount := 0
	table := newQueryTable(state, func() {
		retrieveCount++
	})

	table.OnSelected(widget.TableCellID{Row: 1, Col: 2})
	if state.selectedQueryRow != 0 {
		t.Fatalf("selectedQueryRow = %d, want 0", state.selectedQueryRow)
	}
	if retrieveCount != 0 {
		t.Fatalf("retrieve callback count after normal column = %d, want 0", retrieveCount)
	}

	table.OnSelected(widget.TableCellID{Row: 1, Col: queryRetrieveColumn})
	if state.selectedQueryRow != 0 {
		t.Fatalf("selectedQueryRow after retrieve = %d, want 0", state.selectedQueryRow)
	}
	if retrieveCount != 1 {
		t.Fatalf("retrieve callback count after retrieve column = %d, want 1", retrieveCount)
	}
}

func TestNodeTableCellUsesHeaderAndStripedStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyNodeTableCell(cell, 0, "Enabled", true, false)

	if cell.label.Text != "Enabled" {
		t.Fatalf("header text = %q", cell.label.Text)
	}
	if !cell.label.TextStyle.Bold {
		t.Fatal("node header should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveHeaderRowColor {
		t.Fatalf("header fill = %#v, want %#v", got, archiveHeaderRowColor)
	}

	applyNodeTableCell(cell, 2, "RADIANT", false, false)
	if cell.label.TextStyle.Bold {
		t.Fatal("node row should not be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveEvenRowColor {
		t.Fatalf("node even row fill = %#v, want %#v", got, archiveEvenRowColor)
	}
}

func TestNodeSelectedTableCellUsesSelectionStyling(t *testing.T) {
	cell := newArchiveTableCell()

	applyNodeTableCell(cell, 2, "RADIANT", false, true)

	if cell.label.TextStyle.Bold {
		t.Fatal("selected node row text should not become bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected node fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
}

func TestArchiveFiltersWithQuickSearchPreservesAdvancedFilters(t *testing.T) {
	base := archive.StudyFilters{
		PatientID:       "P123",
		AccessionNumber: "A100",
		Modalities:      []string{"CT", "MR"},
		SourcePath:      "incoming",
	}

	filters, ok := archiveFiltersWithQuickSearchField(base, archiveQuickSearchPatientName, "  DOE  ")

	if !ok {
		t.Fatal("patient-name quick search should be supported")
	}
	if filters.PatientName != "DOE" {
		t.Fatalf("PatientName = %q, want DOE", filters.PatientName)
	}
	if filters.PatientID != "P123" || filters.AccessionNumber != "A100" || filters.SourcePath != "incoming" {
		t.Fatalf("advanced fields not preserved: %#v", filters)
	}
	if strings.Join(filters.Modalities, ",") != "CT,MR" {
		t.Fatalf("Modalities = %#v, want CT,MR", filters.Modalities)
	}
}

func TestArchiveFiltersWithQuickSearchFieldSupportsPatientIDAndAccession(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantID  string
		wantAcc string
		wantOK  bool
	}{
		{name: "patient id", field: archiveQuickSearchPatientID, value: "P123", wantID: "P123", wantOK: true},
		{name: "accession", field: archiveQuickSearchAccession, value: "A100", wantAcc: "A100", wantOK: true},
		{name: "unknown", field: "Unknown", value: "value", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters, ok := archiveFiltersWithQuickSearchField(archive.StudyFilters{}, tt.field, " "+tt.value+" ")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if filters.PatientID != tt.wantID || filters.AccessionNumber != tt.wantAcc {
				t.Fatalf("filters = %#v", filters)
			}
		})
	}
}
