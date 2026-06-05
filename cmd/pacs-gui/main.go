package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
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
	queryDatePresetToday              = "Today"
	queryDatePresetYesterday          = "Yesterday"
	queryDatePresetDayBeforeYesterday = "Day Before Yesterday"
	queryDatePresetLast2Days          = "Last 2 days"
	queryDatePresetLast7Days          = "Last 7 days"
	queryDatePresetLastMonth          = "Last month"
	queryDatePresetLast3Months        = "Last 3 months"
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
	queryActionLabelQuery    = "Query"
	queryActionLabelPatient  = "Patient"
	queryActionLabelSeries   = "Series"
	queryActionLabelImages   = "Images"
	queryActionLabelRetrieve = "Retrieve"
	queryActionLabelVerify   = "Verify"
)

const (
	toolbarLabelOpen           = "Open"
	toolbarLabelInspect        = "Inspect"
	toolbarLabelImport         = "Import"
	toolbarLabelFolder         = "Folder"
	toolbarLabelRefresh        = "Refresh"
	toolbarLabelSendStudy      = "Send Study"
	toolbarLabelSendSeries     = "Send Series"
	toolbarLabelSendImage      = "Send Image"
	toolbarLabelRetrieveSeries = "Get Series"
	toolbarLabelRetrieveImage  = "Get Image"
	toolbarLabelCancel         = "Cancel"
	toolbarLabelAdd            = "Add"
	toolbarLabelEdit           = "Edit"
	toolbarLabelDelete         = "Delete"
	toolbarLabelVerify         = "Verify"
	toolbarLabelListen         = "Listen"
	toolbarLabelStop           = "Stop"
	toolbarLabelSettings       = "Settings"
)

const (
	queryRefreshModeDont    = "Don't refresh"
	queryRefreshButtonLabel = "Refresh"
	queryAutoRetrieveLabel  = "Auto-Retrieve"
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

const (
	archiveQuickSearchPatientName = "Patient Name"
	archiveQuickSearchPatientID   = "Patient ID"
	archiveQuickSearchAccession   = "Accession"
)

var queryDatePresetOptions = []string{
	queryDatePresetAny,
	queryDatePresetToday,
	queryDatePresetYesterday,
	queryDatePresetDayBeforeYesterday,
	queryDatePresetLast2Days,
	queryDatePresetLast7Days,
	queryDatePresetLastMonth,
	queryDatePresetLast3Months,
}

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

func queryActionButtonLabels() []string {
	return []string{
		queryActionLabelQuery,
		queryActionLabelPatient,
		queryActionLabelSeries,
		queryActionLabelImages,
		queryActionLabelRetrieve,
		queryActionLabelVerify,
	}
}

func mainToolbarButtonLabels() []string {
	return []string{
		toolbarLabelOpen,
		toolbarLabelInspect,
		toolbarLabelImport,
		toolbarLabelFolder,
		toolbarLabelRefresh,
		toolbarLabelSendStudy,
		toolbarLabelSendSeries,
		toolbarLabelSendImage,
		toolbarLabelRetrieveSeries,
		toolbarLabelRetrieveImage,
		toolbarLabelCancel,
		toolbarLabelAdd,
		toolbarLabelEdit,
		toolbarLabelDelete,
		toolbarLabelVerify,
		toolbarLabelListen,
		toolbarLabelStop,
		toolbarLabelSettings,
	}
}

func compactToolbarButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon(label, icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

var archiveQuickSearchOptions = []string{
	archiveQuickSearchPatientName,
	archiveQuickSearchPatientID,
	archiveQuickSearchAccession,
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
	w := a.NewWindow("Go-PACS")
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

	state := &uiState{
		catalog:              catalog,
		nodeStore:            nodeStore,
		nodes:                nodeList,
		appConfig:            appCfg,
		appConfigPath:        configPath,
		operations:           operationHistory,
		operationHistoryPath: operationHistoryPath,
	}
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
	archiveControls := newArchiveControls(w, status, tables, state)

	openButton := compactToolbarButton(toolbarLabelOpen, theme.FolderOpenIcon(), func() {
		openFileDialog(w, status, summary, elementTable, state)
	})
	inspectArchiveButton := compactToolbarButton(toolbarLabelInspect, theme.SearchIcon(), func() {
		inspectSelectedArchiveInstance(w, status, summary, elementTable, state)
	})
	importFileButton := compactToolbarButton(toolbarLabelImport, theme.ContentAddIcon(), func() {
		importFileDialog(w, status, tables, state)
	})
	importFolderButton := compactToolbarButton(toolbarLabelFolder, theme.FolderIcon(), func() {
		importFolderDialog(w, status, tables, state)
	})
	refreshButton := compactToolbarButton(toolbarLabelRefresh, theme.ViewRefreshIcon(), func() {
		refreshStudies(w, status, tables, state)
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
	addNodeButton := compactToolbarButton(toolbarLabelAdd, theme.ContentAddIcon(), func() {
		showAddNodeDialog(w, status, nodeTable, state)
	})
	editNodeButton := compactToolbarButton(toolbarLabelEdit, theme.DocumentCreateIcon(), func() {
		showEditNodeDialog(w, status, nodeTable, state)
	})
	deleteNodeButton := compactToolbarButton(toolbarLabelDelete, theme.DeleteIcon(), func() {
		deleteSelectedNode(w, status, nodeTable, state)
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

	title := widget.NewLabelWithStyle("Go-PACS", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.TextStyle.Monospace = false
	actions := container.NewHBox(openButton, inspectArchiveButton, importFileButton, importFolderButton, refreshButton, sendStudyButton, sendSeriesButton, sendImageButton, retrieveSeriesButton, retrieveImageButton, cancelRetrieveButton, addNodeButton, editNodeButton, deleteNodeButton, echoButton, startReceiverButton, stopReceiverButton, settingsButton)
	actionsScroll := container.NewHScroll(actions)
	actionsScroll.SetMinSize(fyne.NewSize(0, actions.MinSize().Height))
	toolbar := container.NewVBox(container.NewBorder(nil, nil, title, nil, actionsScroll), status)

	seriesAndInstances := container.NewVSplit(
		labeledTable("Series", seriesTable),
		labeledTable("Instances", instanceTable),
	)
	seriesAndInstances.SetOffset(0.42)
	archiveBrowser := container.NewVSplit(
		labeledTable("Studies", studyTable),
		seriesAndInstances,
	)
	archiveBrowser.SetOffset(0.46)
	archiveTab := newArchiveWorkbench(w, status, tables, archiveControls, archiveBrowser, state)
	networkTab := container.NewBorder(nil, nil, nil, nil, container.NewStack(nodeTable))
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
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Archive", theme.StorageIcon(), archiveTab),
		container.NewTabItemWithIcon("Network", theme.ComputerIcon(), networkTab),
		container.NewTabItemWithIcon("Query", theme.SearchReplaceIcon(), queryTab),
		container.NewTabItemWithIcon("Tasks", theme.HistoryIcon(), tasksTab),
		container.NewTabItemWithIcon("Inspector", theme.SearchIcon(), inspectorTab),
	)

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
	elements                    []dicominspect.ElementSummary
	studies                     []archive.Study
	archiveRows                 []archiveBrowserRow
	collapsedPatientGroups      map[string]bool
	collapsedArchiveStudies     map[string]bool
	archiveSeriesByStudy        map[string][]archive.Series
	series                      []archive.Series
	instances                   []archive.Instance
	nodes                       []nodes.Node
	queries                     []query.Match
	catalog                     *archive.Catalog
	nodeStore                   *nodes.Store
	receiver                    *receive.Server
	appConfig                   appconfig.Config
	appConfigPath               string
	operations                  []ops.Summary
	operationTable              *widget.Table
	operationDetail             *widget.Entry
	operationHistoryPath        string
	archiveAlbumList            *widget.List
	selectedArchiveAlbum        archiveAlbumID
	archiveSources              *widget.Label
	archiveActivity             *widget.Label
	archiveActivityProgress     *widget.ProgressBar
	archiveCancelRetrieveButton *widget.Button
	archiveSummary              *widget.Label
	archiveResultSummary        *widget.Label
	queryDestinationLabel       *widget.Label
	queryResultSummaryLabel     *widget.Label
	querySourceList             *widget.List
	queryMoveDestinationSelect  *widget.SelectEntry
	lastQuery                   lastQueryRequest
	queryMoveDestination        string
	queryAutoRetrieve           bool
	nodeVerifyStatuses          map[string]nodeVerifyStatus
	querySourceStatuses         map[string]querySourceStatus
	receiverStartedAt           time.Time
	activeRetrieveCancel        context.CancelFunc
	retrieveActivityNode        string
	retrieveActivityProgress    retrieve.Progress
	selectedOperationRow        int
	studyFilters                archive.StudyFilters
	seriesFilters               archive.SeriesFilters
	selectedStudyRow            int
	selectedSeriesRow           int
	selectedInstanceRow         int
	selectedNodeRow             int
	selectedQueryRow            int
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

func newTaskDetail() *widget.Entry {
	detail := widget.NewMultiLineEntry()
	detail.Wrapping = fyne.TextWrapWord
	detail.Disable()
	return detail
}

func newTaskTable(state *uiState) *widget.Table {
	headers := []string{"Kind", "Status", "Counts", "Duration", "Failures"}
	table := widget.NewTable(
		func() (int, int) {
			return len(state.operations) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("wide table cell value")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(headers[id.Col])
				return
			}
			label.TextStyle = fyne.TextStyle{}
			summary := state.operations[id.Row-1]
			label.SetText(taskCell(summary, id.Col))
		},
	)
	table.SetColumnWidth(0, 120)
	table.SetColumnWidth(1, 100)
	table.SetColumnWidth(2, 420)
	table.SetColumnWidth(3, 100)
	table.SetColumnWidth(4, 500)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			return
		}
		state.selectedOperationRow = id.Row - 1
		updateTaskDetail(state)
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
	summary := newArchiveSummaryPane(state)
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
	state.archiveSources = compactWorkbenchLabel()
	state.archiveActivity = compactWorkbenchLabel()
	state.archiveActivityProgress = widget.NewProgressBar()
	state.archiveActivityProgress.Hide()
	state.archiveCancelRetrieveButton = widget.NewButtonWithIcon("Cancel Retrieve", theme.MediaStopIcon(), func() {
		cancelActiveRetrieve(status, state)
	})
	state.archiveCancelRetrieveButton.Hide()
	content := container.NewVBox(
		workbenchSectionTitle("Albums"),
		state.archiveAlbumList,
		widget.NewSeparator(),
		workbenchSectionTitle("Sources"),
		state.archiveSources,
		widget.NewSeparator(),
		workbenchSectionTitle("Activity"),
		state.archiveActivity,
		state.archiveActivityProgress,
		state.archiveCancelRetrieveButton,
	)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(220, 0))
	return scroll
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

func newArchiveSummaryPane(state *uiState) fyne.CanvasObject {
	state.archiveSummary = compactWorkbenchLabel()
	state.archiveSummary.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(state.archiveSummary)
	scroll.SetMinSize(fyne.NewSize(260, 0))
	return container.NewBorder(workbenchSectionTitle("Selected Study"), nil, nil, nil, scroll)
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

func refreshArchiveChrome(state *uiState) {
	if state == nil {
		return
	}
	if state.archiveAlbumList != nil {
		state.archiveAlbumList.Refresh()
	}
	if state.archiveSources != nil {
		state.archiveSources.SetText(strings.Join(archiveSourceLines(state), "\n"))
	}
	if state.archiveActivity != nil {
		state.archiveActivity.SetText(strings.Join(archiveActivityLines(state), "\n"))
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
	if state.archiveSummary != nil {
		state.archiveSummary.SetText(archiveSummaryText(state))
	}
	if state.archiveResultSummary != nil {
		state.archiveResultSummary.SetText(archiveResultSummaryText(state))
	}
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
	return fmt.Sprintf(
		"%d patients, %d studies, %d series, %d images",
		len(patientKeys),
		len(state.studies),
		seriesCount,
		imageCount,
	)
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

func archiveSourceLines(state *uiState) []string {
	lines := []string{"▣ Documents DB"}
	if state == nil {
		return lines
	}
	if state.receiver != nil {
		snapshot := state.receiver.Snapshot()
		lines = append(lines, fmt.Sprintf("● Receiver %s %s", snapshot.AETitle, snapshot.Address))
	} else {
		lines = append(lines, fmt.Sprintf("● Receiver %s stopped", localAETitle(state)))
	}
	for index, node := range state.nodes {
		prefix := "  "
		if index == state.selectedNodeRow {
			prefix = "▶ "
		}
		lines = append(lines, fmt.Sprintf("%s◉ %s %s:%d", prefix, node.Name, node.Host, node.Port))
	}
	return lines
}

func archiveActivityLines(state *uiState) []string {
	if state == nil {
		return []string{"No recent activity"}
	}
	var lines []string
	if state.activeRetrieveCancel != nil {
		lines = append(lines, retrieveProgressText(state.retrieveActivityNode, state.retrieveActivityProgress))
	}
	for i, summary := range state.operations {
		if i >= 4 {
			break
		}
		counts := shortTaskCounts(summary.Counts)
		if counts != "" {
			lines = append(lines, fmt.Sprintf("%s %s %s", summary.Kind, summary.Status, counts))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", summary.Kind, summary.Status))
	}
	if len(lines) == 0 {
		lines = append(lines, "No recent activity")
	}
	return lines
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
	fmt.Fprintf(&b, "Study: %s %s\n", emptyDash(study.StudyDate), emptyDash(study.Modalities))
	if strings.TrimSpace(study.StudyDescription) != "" {
		fmt.Fprintf(&b, "%s\n", study.StudyDescription)
	}
	fmt.Fprintf(&b, "Accession: %s\n", emptyDash(study.AccessionNumber))
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

func patientStudySummaryLines(state *uiState, selectedStudyIndex int) []string {
	if state == nil || selectedStudyIndex < 0 || selectedStudyIndex >= len(state.studies) {
		return nil
	}
	selected := state.studies[selectedStudyIndex]
	selectedKey := archivePatientKey(selected)
	var lines []string
	appendStudy := func(study archive.Study) {
		description := strings.TrimSpace(study.StudyDescription)
		if description == "" {
			description = strings.TrimSpace(study.StudyInstanceUID)
		}
		lines = append(lines, fmt.Sprintf(
			"%s %s %s %d images",
			emptyDash(study.StudyDate),
			emptyDash(study.Modalities),
			emptyDash(description),
			study.InstanceCount,
		))
	}
	appendStudy(selected)
	for index, study := range state.studies {
		if index == selectedStudyIndex {
			continue
		}
		if archivePatientKey(study) == selectedKey {
			appendStudy(study)
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
		{ID: archiveAlbumDatabase, Label: "Database", Count: len(studies), Filterable: true},
		{ID: archiveAlbumComments, Label: "Cases with comments", Count: 0},
		{ID: archiveAlbumInteresting, Label: "Interesting Cases", Count: 0},
		{ID: archiveAlbumLastHour, Label: "Just Acquired (last hour)", Count: countStudiesImportedSince(studies, now.Add(-time.Hour)), Filterable: true},
		{ID: archiveAlbumOpened, Label: "Just Opened", Count: 0},
		{ID: archiveAlbumToday, Label: "Today", Count: countStudiesImportedToday(studies, now, ""), Filterable: true},
		{ID: archiveAlbumTodayCT, Label: "Today CT", Count: countStudiesImportedToday(studies, now, "CT"), Filterable: true},
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
	status.SetText("Importing " + filepath.Base(path))
	started := time.Now()
	go func() {
		report, err := state.catalog.ImportPathWithOptions(context.Background(), path, importOptionsFromConfig(state.appConfig))
		summary := ops.ImportSummary(report, time.Since(started))
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
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

func newArchiveControls(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	quickSearchField := widget.NewSelect(archiveQuickSearchOptions, nil)
	quickSearchField.SetSelected(archiveQuickSearchPatientName)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder("Search")
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
		switch quickSearchField.Selected {
		case archiveQuickSearchPatientID:
			quickSearch.SetText(strings.TrimSpace(patientID.Text))
		case archiveQuickSearchAccession:
			quickSearch.SetText(strings.TrimSpace(accession.Text))
		default:
			quickSearch.SetText(strings.TrimSpace(patientName.Text))
		}
		refreshStudies(w, status, tables, state)
	}
	applyButton := widget.NewButtonWithIcon("Apply Filters", theme.SearchIcon(), applyAdvancedFilters)
	quickSearch.OnSubmitted = func(_ string) {
		filters, ok := archiveFiltersWithQuickSearchField(state.studyFilters, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Search failed")
			dialog.ShowError(fmt.Errorf("unsupported archive search field %q", quickSearchField.Selected), w)
			return
		}
		state.studyFilters = filters
		patientName.SetText(state.studyFilters.PatientName)
		patientID.SetText(state.studyFilters.PatientID)
		accession.SetText(state.studyFilters.AccessionNumber)
		refreshStudies(w, status, tables, state)
	}
	searchButton := widget.NewButtonWithIcon("Search", theme.SearchIcon(), func() {
		quickSearch.OnSubmitted(quickSearch.Text)
	})
	clearButton := widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		quickSearch.SetText("")
		quickSearchField.SetSelected(archiveQuickSearchPatientName)
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

	quickRow := container.NewBorder(
		nil,
		nil,
		labeledControl("Search", quickSearchField),
		container.NewHBox(searchButton),
		quickSearch,
	)
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
	return container.NewVBox(quickRow, advancedFilters, exportActions)
}

func labeledEntry(label string, entry *widget.Entry) fyne.CanvasObject {
	return labeledControl(label, entry)
}

func labeledControl(label string, control fyne.CanvasObject) fyne.CanvasObject {
	text := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	text.Wrapping = fyne.TextTruncate
	return container.NewBorder(nil, nil, text, nil, control)
}

func archiveFiltersWithQuickSearch(filters archive.StudyFilters, query string) archive.StudyFilters {
	filters, _ = archiveFiltersWithQuickSearchField(filters, archiveQuickSearchPatientName, query)
	return filters
}

func archiveFiltersWithQuickSearchField(filters archive.StudyFilters, field string, query string) (archive.StudyFilters, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return filters, true
	}
	switch field {
	case archiveQuickSearchPatientName:
		filters.PatientName = query
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
	formatDay := func(value time.Time) string {
		return value.Local().Format("20060102")
	}
	switch preset {
	case queryDatePresetAny:
		return "", "", true
	case queryDatePresetToday:
		day := formatDay(now)
		return day, day, true
	case queryDatePresetYesterday:
		day := formatDay(now.AddDate(0, 0, -1))
		return day, day, true
	case queryDatePresetDayBeforeYesterday:
		day := formatDay(now.AddDate(0, 0, -2))
		return day, day, true
	case queryDatePresetLast2Days:
		return formatDay(now.AddDate(0, 0, -1)), formatDay(now), true
	case queryDatePresetLast7Days:
		return formatDay(now.AddDate(0, 0, -6)), formatDay(now), true
	case queryDatePresetLastMonth:
		return formatDay(now.AddDate(0, -1, 1)), formatDay(now), true
	case queryDatePresetLast3Months:
		return formatDay(now.AddDate(0, -3, 1)), formatDay(now), true
	default:
		return "", "", false
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

func queryModalityCriteriaText(manual string, checks map[string]*widget.Check) string {
	var selected []string
	for _, code := range queryModalityCodes {
		check := checks[code]
		if check != nil && check.Checked {
			selected = append(selected, code)
		}
	}
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

func queryModalityGrid(checks map[string]*widget.Check) fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, 0, len(queryModalityCodes))
	for _, code := range queryModalityCodes {
		if check := checks[code]; check != nil {
			objects = append(objects, check)
		}
	}
	return container.NewGridWithColumns(10, objects...)
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
	picker.SetFileName("Go-PACS-studies.csv")
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
	picker.SetFileName("Go-PACS-studies.json")
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
	picker.SetFileName("Go-PACS-series.csv")
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
	picker.SetFileName("Go-PACS-series.json")
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
	picker.SetFileName("Go-PACS-images.csv")
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
	picker.SetFileName("Go-PACS-images.json")
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
	state.studies = studies
	state.archiveRows = archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.collapsedArchiveStudies)
	state.selectedStudyRow = -1
	clearArchiveDetails(state, tables)
	tables.studies.Refresh()
	refreshArchiveChrome(state)
}

func clearArchiveDetails(state *uiState, tables archiveTables) {
	state.series = nil
	state.instances = nil
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	tables.series.Refresh()
	tables.instances.Refresh()
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
	noun := "matches"
	if count == 1 {
		noun = "match"
	}
	source := "no source selected"
	if node, ok := selectedQueryNode(state); ok {
		source = fmt.Sprintf("%s / %s:%d", emptyDash(node.Name), emptyDash(node.Host), node.Port)
	}
	return fmt.Sprintf("%d %s found / %s", count, noun, source)
}

func refreshQueryResultSummary(state *uiState) {
	if state == nil || state.queryResultSummaryLabel == nil {
		return
	}
	state.queryResultSummaryLabel.SetText(queryResultSummaryText(state))
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
	return fmt.Sprintf("%s%s%s %s:%d", prefix, querySourceMarkers(state, node), node.Name, node.Host, node.Port)
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
		updateListenerAddressSummary()
		updateListenerStatus()
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
		widget.NewFormItem("Receiver Port", receiverPort),
		widget.NewFormItem("Local Addresses", container.NewBorder(nil, nil, nil, copyAddressesButton, listenerAddressSummary)),
		widget.NewFormItem("AE Aliases", additionalAEs),
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
			LocalAETitle:            localAE.Text,
			ReceiverAddress:         receiverAddress,
			ReceiverAutoStart:       activateListener.Checked,
			AdditionalAETitles:      parseAETitleList(additionalAEs.Text),
			MaxFileImportBytes:      fileLimit,
			MaxZipEntryBytes:        zipEntryLimit,
			MaxZipTotalBytes:        zipTotalLimit,
			MaxZipEntryCount:        zipEntryCount,
			MaxStoreObjectBytes:     storeObjectLimit,
			MaxImportTotalFiles:     importTotalFiles,
			MaxImportPathLength:     importPathLength,
			MaxImportDirectoryDepth: importDirectoryDepth,
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
	form.Resize(fyne.NewSize(660, 540))
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

func nodeDraftFromFormState(name string, aeTitle string, host string, port uint16, enabled bool, queryEnabled bool, retrieveMethod string, sendEnabled bool, moveDestination string, notes string) nodes.Draft {
	return nodes.Draft{
		Name:                     name,
		AETitle:                  aeTitle,
		Host:                     host,
		Port:                     port,
		Disabled:                 !enabled,
		QueryDisabled:            !queryEnabled,
		SendDisabled:             !sendEnabled,
		RetrieveMethod:           retrieveMethod,
		PreferredMoveDestination: moveDestination,
		Notes:                    notes,
	}
}

func retrieveMethodOptions() []string {
	return []string{nodes.RetrieveMethodAuto, nodes.RetrieveMethodMove, nodes.RetrieveMethodGet}
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
		node, err := state.nodeStore.Add(nodeDraftFromFormState(name.Text, aeTitle.Text, host.Text, portValue, enabled.Checked, queryEnabled.Checked, retrieveMethod.Selected, sendEnabled.Checked, moveDestination.Text, notes.Text))
		if err != nil {
			status.SetText("Add node failed")
			dialog.ShowError(err, w)
			return
		}
		state.nodes = append(state.nodes, node)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Added node " + node.Name)
	}, w)
	form.Resize(fyne.NewSize(520, 420))
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
		updated, err := state.nodeStore.Update(node.ID, nodeDraftFromFormState(name.Text, aeTitle.Text, host.Text, portValue, enabled.Checked, queryEnabled.Checked, retrieveMethod.Selected, sendEnabled.Checked, moveDestination.Text, notes.Text))
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
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Edited node " + updated.Name)
	}, w)
	form.Resize(fyne.NewSize(520, 420))
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
		result, err := netverify.Echo(context.Background(), node, callingAE)
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
		Catalog:                state.catalog,
		Address:                state.appConfig.ReceiverAddress,
		AETitle:                localAETitle(state),
		AllowedCalledAETitles:  state.appConfig.AdditionalAETitles,
		AllowedCallingAETitles: allowedCallingAEs,
		AllowedRemoteHosts:     allowedRemoteHosts,
		MaxStoreObjectBytes:    optionalInt64Value(state.appConfig.MaxStoreObjectBytes),
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
	go func() {
		outcome, err := send.SendStudy(context.Background(), state.catalog, node, study.StudyInstanceUID, callingAE)
		fyne.Do(func() {
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
	go func() {
		outcome, err := send.SendSeries(context.Background(), state.catalog, node, series.SeriesInstanceUID, callingAE)
		fyne.Do(func() {
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
	go func() {
		outcome, err := send.SendInstance(context.Background(), state.catalog, node, instance.SOPInstanceUID, callingAE)
		fyne.Do(func() {
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
	ctx, cancel := beginRetrieve(state, node.Name)
	status.SetText(fmt.Sprintf("Retrieving series %s from %s", series.SeriesInstanceUID, node.Name))
	go func() {
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
	ctx, cancel := beginRetrieve(state, node.Name)
	status.SetText(fmt.Sprintf("Retrieving image %s from %s", instance.SOPInstanceUID, node.Name))
	go func() {
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
			label := strings.TrimSpace(row.series.SeriesDescription)
			if label == "" {
				label = strings.TrimSpace(row.series.SeriesNumber)
			}
			if label == "" {
				label = strings.TrimSpace(row.series.SeriesInstanceUID)
			}
			return "      " + emptyDash(label)
		case archiveStudyTableColumnModality:
			return row.series.Modality
		case archiveStudyTableColumnDescription:
			return row.series.SeriesDescription
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
	if len(state.studies) == 0 {
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
			label := widget.NewLabel("wide table cell value")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(headers[id.Col])
				return
			}
			label.TextStyle = fyne.TextStyle{}
			elem := state.elements[id.Row-1]
			label.SetText(tableCell(elem, id.Col))
		},
	)
	table.SetColumnWidth(0, 72)
	table.SetColumnWidth(1, 105)
	table.SetColumnWidth(2, 52)
	table.SetColumnWidth(3, 210)
	table.SetColumnWidth(4, 82)
	table.SetColumnWidth(5, 520)
	return table
}

var (
	archiveHeaderRowColor       = color.NRGBA{R: 40, G: 40, B: 40, A: 255}
	archivePatientRowColor      = color.NRGBA{R: 48, G: 48, B: 48, A: 255}
	archiveOddRowColor          = color.NRGBA{R: 28, G: 28, B: 28, A: 255}
	archiveEvenRowColor         = color.NRGBA{R: 34, G: 34, B: 34, A: 255}
	archiveSeriesRowColor       = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
	archiveSelectedRowColor     = color.NRGBA{R: 45, G: 85, B: 128, A: 255}
	queryRetrieveActionRowColor = color.NRGBA{R: 34, G: 58, B: 38, A: 255}
)

type archiveTableCell struct {
	*fyne.Container
	background *canvas.Rectangle
	label      *widget.Label
}

func newArchiveTableCell() *archiveTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	return &archiveTableCell{
		Container:  container.NewStack(background, container.NewPadded(label)),
		background: background,
		label:      label,
	}
}

func applyArchiveTableCell(cell *archiveTableCell, tableRow int, text string, row archiveBrowserRow, header bool, selected bool) {
	if cell == nil {
		return
	}
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{Bold: header || row.kind == archiveRowPatient}
	cell.background.FillColor = archiveTableFillColor(tableRow, row, header, selected)
	cell.background.Refresh()
	cell.label.Refresh()
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
	archiveStudyTableColumnPatientID
	archiveStudyTableColumnDOB
	archiveStudyTableColumnStudyDate
	archiveStudyTableColumnTime
	archiveStudyTableColumnAdded
	archiveStudyTableColumnModality
	archiveStudyTableColumnDescription
	archiveStudyTableColumnAccession
	archiveStudyTableColumnInstitution
	archiveStudyTableColumnStatus
	archiveStudyTableColumnComments
	archiveStudyTableColumnSeries
	archiveStudyTableColumnInstances
	archiveStudyTableColumnStudyUID
)

func archiveTableHeaders() []string {
	return []string{"Patient", "Patient ID", "DOB", "Study Date", "Time", "Added", "Modality", "Description", "Accession", "Institution", "Status", "Comments", "Series", "Instances", "Study UID"}
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
			if id.Row == 0 {
				applyArchiveTableCell(cell, id.Row, headers[id.Col], archiveBrowserRow{}, true, false)
				return
			}
			row := state.archiveRows[id.Row-1]
			selected := archiveBrowserRowSelected(row, state)
			applyArchiveTableCell(cell, id.Row, archiveBrowserCell(row, state.studies, id.Col), row, false, selected)
		},
	)
	state.selectedStudyRow = -1
	widths := []float32{180, 120, 95, 95, 80, 120, 95, 220, 120, 180, 90, 180, 80, 80, 360}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	return table
}

func newSeriesTable(state *uiState) *widget.Table {
	headers := []string{"Series #", "Modality", "Description", "Instances", "Series UID"}
	table := widget.NewTable(
		func() (int, int) {
			return len(state.series) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("wide table cell value")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(headers[id.Col])
				return
			}
			label.TextStyle = fyne.TextStyle{}
			series := state.series[id.Row-1]
			label.SetText(seriesCell(series, id.Col))
		},
	)
	state.selectedSeriesRow = -1
	table.SetColumnWidth(0, 80)
	table.SetColumnWidth(1, 85)
	table.SetColumnWidth(2, 220)
	table.SetColumnWidth(3, 80)
	table.SetColumnWidth(4, 360)
	return table
}

func newInstanceTable(state *uiState) *widget.Table {
	headers := []string{"Instance #", "Modality", "SOP Class", "Transfer Syntax", "Source", "SOP UID"}
	table := widget.NewTable(
		func() (int, int) {
			return len(state.instances) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("wide table cell value")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(headers[id.Col])
				return
			}
			label.TextStyle = fyne.TextStyle{}
			instance := state.instances[id.Row-1]
			label.SetText(instanceCell(instance, id.Col))
		},
	)
	state.selectedInstanceRow = -1
	table.SetColumnWidth(0, 90)
	table.SetColumnWidth(1, 85)
	table.SetColumnWidth(2, 220)
	table.SetColumnWidth(3, 180)
	table.SetColumnWidth(4, 280)
	table.SetColumnWidth(5, 360)
	return table
}

func wireArchiveTables(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	tables.studies.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
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
			state.selectedSeriesRow = -1
			setInstances(state, tables, nil)
			refreshArchiveChrome(state)
			return
		}
		row := id.Row - 1
		if row < 0 || row >= len(state.series) {
			return
		}
		state.selectedSeriesRow = row
		series := state.series[row]
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
			state.selectedInstanceRow = -1
			refreshArchiveChrome(state)
			return
		}
		row := id.Row - 1
		if row >= 0 && row < len(state.instances) {
			state.selectedInstanceRow = row
			refreshArchiveChrome(state)
		}
	}
}

func newQueryTab(w fyne.Window, status *widget.Label, tables archiveTables, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	quickSearchField := widget.NewSelect(queryQuickSearchOptions, nil)
	quickSearchField.SetSelected(queryQuickSearchPatientName)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder("Search")
	quickSearchField.OnChanged = func(field string) {
		if field == queryQuickSearchCustomDICOMField {
			quickSearch.SetPlaceHolder("StudyID=ABC123")
			return
		}
		quickSearch.SetPlaceHolder("Search")
	}
	patientName := widget.NewEntry()
	patientName.SetPlaceHolder("DOE^JOHN")
	patientID := widget.NewEntry()
	patientID.SetPlaceHolder("12345")
	studyDateFrom := widget.NewEntry()
	studyDateFrom.SetPlaceHolder("20260101")
	studyDateTo := widget.NewEntry()
	studyDateTo.SetPlaceHolder("20261231")
	datePreset := widget.NewSelect(queryDatePresetOptions, func(preset string) {
		from, to, ok := queryDatePresetRange(preset, time.Now())
		if !ok {
			return
		}
		studyDateFrom.SetText(from)
		studyDateTo.SetText(to)
	})
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

	queryTable := newQueryTable(state, func() {
		retrieveSelectedQuery(w, status, tables, state)
	})
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
		criteria := query.SeriesCriteria{
			PatientName:       patientName.Text,
			PatientID:         patientID.Text,
			StudyDateFrom:     studyDateFrom.Text,
			StudyDateTo:       studyDateTo.Text,
			StudyInstanceUID:  studyUID.Text,
			SeriesInstanceUID: seriesUID.Text,
			Modality:          queryModalityCriteriaText(modality.Text, modalityChecks),
			SeriesDescription: studyDescription.Text,
			MaxResults:        max,
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
		criteria := query.ImageCriteria{
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
		}
		rememberLastImageQuery(state, criteria)
		runImageQuery(w, status, queryTable, state, criteria)
	})
	refreshButton := widget.NewButtonWithIcon(queryRefreshButtonLabel, theme.ViewRefreshIcon(), func() {
		refreshLastQuery(w, status, queryTable, state)
	})
	retrieveButton := widget.NewButtonWithIcon(queryActionLabelRetrieve, theme.DownloadIcon(), func() {
		retrieveSelectedQuery(w, status, tables, state)
	})
	verifyButton := widget.NewButtonWithIcon(queryActionLabelVerify, theme.ConfirmIcon(), func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	destinationSelect := newQueryMoveDestinationEntry(state)
	state.queryMoveDestinationSelect = destinationSelect
	state.queryDestinationLabel = widget.NewLabel(queryDestinationText(state))
	state.queryDestinationLabel.Wrapping = fyne.TextTruncate
	state.queryResultSummaryLabel = widget.NewLabel(queryResultSummaryText(state))
	state.queryResultSummaryLabel.Wrapping = fyne.TextTruncate
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
	sourceHeader := container.NewBorder(nil, nil, workbenchSectionTitle("Sources"), container.NewHBox(moveUpButton, moveDownButton))
	sourcePanel := container.NewBorder(sourceHeader, nil, nil, nil, sourceList)
	sourcePanel.Resize(fyne.NewSize(250, 0))

	criteria := container.NewVBox(
		container.NewBorder(
			nil,
			nil,
			labeledControl("Search", quickSearchField),
			nil,
			quickSearch,
		),
		container.NewGridWithColumns(5,
			labeledEntry("Patient Name", patientName),
			labeledEntry("Patient ID", patientID),
			labeledControl("Date", datePreset),
			labeledEntry("Study Date From", studyDateFrom),
			labeledEntry("Study Date To", studyDateTo),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Accession", accession),
			labeledEntry("Study UID", studyUID),
			labeledEntry("Series UID", seriesUID),
			labeledEntry("SOP UID", sopUID),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Description", studyDescription),
			labeledEntry("Modality Manual", modality),
			labeledEntry("SOP Class", sopClassUID),
			labeledEntry("Instance #", instanceNumber),
		),
		container.NewVBox(workbenchSectionTitle("Modalities"), queryModalityGrid(modalityChecks)),
		container.NewBorder(
			nil,
			nil,
			container.NewHBox(labeledEntry("Max Results", maxResults), labeledControl("Refresh", refreshMode), labeledControl("Retrieve to", destinationSelect), autoRetrieve),
			container.NewHBox(refreshButton, runButton, runPatientButton, runSeriesButton, runImageButton, retrieveButton, verifyButton),
			state.queryDestinationLabel,
		),
	)
	return container.NewBorder(criteria, state.queryResultSummaryLabel, sourcePanel, nil, container.NewStack(queryTable))
}

func newQuerySourceList(state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(querySourceRows(state))
		},
		func() fyne.CanvasObject {
			return widget.NewCheck("", nil)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			check := obj.(*widget.Check)
			configureQuerySourceCheck(check, state, id, func() {
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

func retrieveSelectedQuery(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	match, ok := selectedQuery(state)
	if !ok {
		status.SetText("Select a query result to retrieve")
		return
	}
	if strings.TrimSpace(match.StudyInstanceUID) == "" {
		status.SetText("Selected query result has no Study Instance UID")
		return
	}
	node, ok := nodeForQueryMatch(state, match)
	if !ok {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		status.SetText(issue)
		return
	}
	opts := retrieveOptionsForNode(status, state, node)
	retrieveLevel := match.QueryRetrieveLevel
	if retrieveLevel == "" {
		retrieveLevel = "STUDY"
	}
	retrieveLabel := "study " + match.StudyInstanceUID
	if retrieveLevel == "IMAGE" || strings.TrimSpace(match.SOPInstanceUID) != "" {
		if strings.TrimSpace(match.SeriesInstanceUID) == "" {
			status.SetText("Selected query result has no Series Instance UID")
			return
		}
		if strings.TrimSpace(match.SOPInstanceUID) == "" {
			status.SetText("Selected query result has no SOP Instance UID")
			return
		}
		retrieveLevel = "IMAGE"
		retrieveLabel = "image " + match.SOPInstanceUID
	} else if retrieveLevel == "SERIES" || strings.TrimSpace(match.SeriesInstanceUID) != "" {
		if strings.TrimSpace(match.SeriesInstanceUID) == "" {
			status.SetText("Selected query result has no Series Instance UID")
			return
		}
		retrieveLevel = "SERIES"
		retrieveLabel = "series " + match.SeriesInstanceUID
	}
	ctx, cancel := beginRetrieve(state, node.Name)
	status.SetText(fmt.Sprintf("Retrieving %s from %s", retrieveLabel, node.Name))
	go func() {
		defer cancel()
		var outcome retrieve.Outcome
		var err error
		if retrieveLevel == "IMAGE" {
			outcome, err = retrieve.RetrieveImage(ctx, state.catalog, node, match.StudyInstanceUID, match.SeriesInstanceUID, match.SOPInstanceUID, opts)
		} else if retrieveLevel == "SERIES" {
			outcome, err = retrieve.RetrieveSeries(ctx, state.catalog, node, match.StudyInstanceUID, match.SeriesInstanceUID, opts)
		} else {
			outcome, err = retrieve.RetrieveStudy(ctx, state.catalog, node, match.StudyInstanceUID, opts)
		}
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

func runPatientQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.PatientCriteria) {
	sources := querySourceNodes(state)
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	status.SetText("Querying patients on " + sourceLabel)
	go func() {
		result, err := runQueryAcrossSources(context.Background(), sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.PatientRootFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Patient query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			state.queries = result.Matches
			state.selectedQueryRow = -1
			table.Refresh()
			refreshQueryResultSummary(state)
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Patient C-FIND", sourceLabel, result, err))
		})
	}()
}

func runStudyQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.Criteria) {
	sources := querySourceNodes(state)
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	status.SetText("Querying " + sourceLabel)
	go func() {
		result, err := runQueryAcrossSources(context.Background(), sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			state.queries = result.Matches
			state.selectedQueryRow = -1
			table.Refresh()
			refreshQueryResultSummary(state)
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("C-FIND", sourceLabel, result, err))
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
	status.SetText("Querying series on " + sourceLabel)
	go func() {
		result, err := runQueryAcrossSources(context.Background(), sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootSeriesFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Series query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			state.queries = result.Matches
			state.selectedQueryRow = -1
			table.Refresh()
			refreshQueryResultSummary(state)
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
	status.SetText("Querying images on " + sourceLabel)
	go func() {
		result, err := runQueryAcrossSources(context.Background(), sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootImageFind(ctx, node, criteria, callingAE)
		})
		fyne.Do(func() {
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Image query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			state.queries = result.Matches
			state.selectedQueryRow = -1
			table.Refresh()
			refreshQueryResultSummary(state)
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Image C-FIND", sourceLabel, result, err))
		})
	}()
}

func selectedQuery(state *uiState) (query.Match, bool) {
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
	queryRetrieveColumn = iota
	queryTableColumnLevel
	queryTableColumnPatient
	queryTableColumnPatientID
	queryTableColumnDOB
	queryTableColumnStudyDate
	queryTableColumnTime
	queryTableColumnModality
	queryTableColumnImages
	queryTableColumnDescription
	queryTableColumnAccession
	queryTableColumnReferrer
	queryTableColumnInstitution
	queryTableColumnLocalComments
	queryTableColumnServerComments
	queryTableColumnStudyStatus
	queryTableColumnSeriesNumber
	queryTableColumnInstanceNumber
	queryTableColumnStudyUID
	queryTableColumnSeriesUID
	queryTableColumnSOPClass
	queryTableColumnSOPUID
	queryTableColumnSource
	queryTableColumnStatus
)

func newQueryTable(state *uiState, onRetrieve func()) *widget.Table {
	headers := queryTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.queries) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyQueryTableCell(cell, id.Row, id.Col, headers[id.Col], true, false)
				return
			}
			match := state.queries[id.Row-1]
			selected := id.Row-1 == state.selectedQueryRow
			applyQueryTableCell(cell, id.Row, id.Col, queryCell(match, id.Col), false, selected)
		},
	)
	state.selectedQueryRow = -1
	table.OnSelected = func(id widget.TableCellID) {
		row, retrieve, ok := queryTableSelectionAction(id)
		if !ok || row < 0 || row >= len(state.queries) {
			return
		}
		state.selectedQueryRow = row
		table.Refresh()
		if retrieve && onRetrieve != nil && strings.TrimSpace(state.queries[row].StudyInstanceUID) != "" {
			onRetrieve()
		}
	}
	widths := []float32{75, 75, 180, 120, 95, 95, 80, 95, 80, 220, 120, 180, 190, 160, 180, 110, 85, 90, 300, 300, 280, 300, 220, 80}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	return table
}

func queryTableSelectionAction(id widget.TableCellID) (int, bool, bool) {
	if id.Row <= 0 {
		return -1, false, false
	}
	return id.Row - 1, id.Col == queryRetrieveColumn, true
}

func queryTableHeaders() []string {
	return []string{"Retrieve", "Level", "Patient", "Patient ID", "DOB", "Study Date", "Time", "Modality", "Images", "Description", "Accession", "Referrer", "Institution", "Local Comments", "Server Comments", "Study Status", "Series #", "Instance #", "Study UID", "Series UID", "SOP Class", "SOP UID", "Source", "Status"}
}

func applyQueryTableCell(cell *archiveTableCell, tableRow int, tableCol int, text string, header bool, selected bool) {
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
	retrieveAction := tableCol == queryRetrieveColumn && strings.TrimSpace(text) != ""
	if retrieveAction {
		cell.label.TextStyle = fyne.TextStyle{Bold: true}
		cell.label.Alignment = fyne.TextAlignCenter
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

func toggleNodeOperationalCell(state *uiState, row int, col int) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	switch col {
	case 0:
		next.Disabled = !next.Disabled
	case 1:
		next.QueryDisabled = !next.QueryDisabled
	case 2:
		next.RetrieveMethod = nextRetrieveMethod(next.RetrieveMethod)
	case 3:
		next.SendDisabled = !next.SendDisabled
	default:
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

func newNodeTable(status *widget.Label, state *uiState) *widget.Table {
	headers := nodeTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.nodes) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyNodeTableCell(cell, id.Row, headers[id.Col], true, false)
				return
			}
			node := state.nodes[id.Row-1]
			selected := id.Row-1 == state.selectedNodeRow
			applyNodeTableCell(cell, id.Row, nodeCell(node, id.Col), false, selected)
		},
	)
	state.selectedNodeRow = -1
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row > 0 {
			row := id.Row - 1
			state.selectedNodeRow = row
			changed, err := toggleNodeOperationalCell(state, row, id.Col)
			if err != nil && status != nil {
				status.SetText("Node update failed")
			} else if changed && status != nil {
				status.SetText("Updated node " + state.nodes[row].Name)
			}
			refreshArchiveChrome(state)
			refreshQueryDestination(state)
			refreshQueryResultSummary(state)
			refreshQuerySourceList(state)
			table.Refresh()
		}
	}
	widths := []float32{70, 70, 90, 70, 70, 140, 110, 190, 70, 150, 120, 360}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	return table
}

func applyNodeTableCell(cell *archiveTableCell, tableRow int, text string, header bool, selected bool) {
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
	case 0:
		return nodeCheckCell(node.Enabled())
	case 1:
		return nodeCheckCell(node.QueryEnabled())
	case 2:
		return nodeMenuCell(node.RetrieveMethodOrDefault())
	case 3:
		return nodeCheckCell(node.SendEnabled())
	case 4:
		return nodeCheckCell(false)
	case 5:
		return node.Name
	case 6:
		return node.AETitle
	case 7:
		return node.Host
	case 8:
		return fmt.Sprintf("%d", node.Port)
	case 9:
		return node.PreferredMoveDestination
	case 10:
		return nodeMenuCell("Auto")
	case 11:
		return node.Notes
	default:
		return ""
	}
}

func queryCell(match query.Match, col int) string {
	switch col {
	case queryRetrieveColumn:
		if strings.TrimSpace(match.StudyInstanceUID) != "" {
			return "↓"
		}
		return ""
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
		return ""
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
	marker := "!"
	if status == 0x0000 || status == 0xFF00 || status == 0xFF01 {
		marker = "●"
	}
	return fmt.Sprintf("%s 0x%04X", marker, status)
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
		return study.StudyDate
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
		return ""
	case archiveStudyTableColumnComments:
		return ""
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
		return filepath.Join(".", ".Go-PACS")
	}
	return filepath.Join(dir, "Go-PACS")
}
