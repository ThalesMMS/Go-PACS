package main

import (
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
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

func TestApplyCompactTableRowsSetsDenseDefaultHeight(t *testing.T) {
	fynetest.NewApp()
	table := widget.NewTable(
		func() (int, int) { return 3, 2 },
		func() fyne.CanvasObject { return newArchiveTableCell() },
		func(widget.TableCellID, fyne.CanvasObject) {},
	)

	applyCompactTableRows(table)

	rowHeights := reflect.ValueOf(table).Elem().FieldByName("rowHeights")
	defaultHeight := rowHeights.MapIndex(reflect.ValueOf(-1))
	if !defaultHeight.IsValid() {
		t.Fatal("compact table rows should set the default row height")
	}
	if got := defaultHeight.Float(); got != float64(compactTableRowHeight) {
		t.Fatalf("default row height = %v, want %v", got, compactTableRowHeight)
	}
	if compactTableRowHeight >= newArchiveTableCell().MinSize().Height {
		t.Fatalf("compact row height = %v, want less than template cell height %v", compactTableRowHeight, newArchiveTableCell().MinSize().Height)
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

func TestClearRecentOperationsClearsStateAndPersistsHistory(t *testing.T) {
	fynetest.NewApp()
	historyPath := filepath.Join(t.TempDir(), "tasks.json")
	status := widget.NewLabel("")
	detail := newTaskDetail()
	state := &uiState{
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusSuccess},
			{Kind: operations.KindRetrieveMove, Status: operations.StatusWarning},
		},
		operationHistoryPath: historyPath,
		operationDetail:      detail,
		selectedOperationRow: 1,
		archiveActivity:      widget.NewLabel("old activity"),
	}
	state.operationTable = newTaskTable(state)
	if err := operations.SaveHistory(historyPath, state.operations); err != nil {
		t.Fatal(err)
	}

	clearRecentOperations(status, state)

	if len(state.operations) != 0 {
		t.Fatalf("len(operations) = %d, want 0", len(state.operations))
	}
	if state.selectedOperationRow != -1 {
		t.Fatalf("selectedOperationRow = %d, want -1", state.selectedOperationRow)
	}
	if detail.Text != "" {
		t.Fatalf("operation detail = %q, want empty", detail.Text)
	}
	if status.Text != "Activity history cleared" {
		t.Fatalf("status = %q", status.Text)
	}
	if state.archiveActivity.Text != "No recent activity" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}
	history, err := operations.LoadHistory(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("persisted history length = %d, want 0", len(history))
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

func TestRefreshArchiveChromeShowsClearActivityButtonWhenHistoryExists(t *testing.T) {
	state := &uiState{archiveClearActivityButton: widget.NewButton("Clear", nil)}

	refreshArchiveChrome(state)

	if state.archiveClearActivityButton.Visible() {
		t.Fatal("clear activity button should be hidden without recent operations")
	}

	state.operations = []operations.Summary{{Kind: operations.KindImport, Status: operations.StatusSuccess}}
	refreshArchiveChrome(state)

	if !state.archiveClearActivityButton.Visible() {
		t.Fatal("clear activity button should be visible with recent operations")
	}
}

func TestNewActivityDismissButtonIsCompactIconOnly(t *testing.T) {
	button := newActivityDismissButton(nil)

	if button.Text != "" {
		t.Fatalf("button text = %q, want empty icon-only button", button.Text)
	}
	if button.Icon == nil || button.Icon.Name() != theme.ContentClearIcon().Name() {
		t.Fatalf("button icon = %#v, want content clear icon", button.Icon)
	}
	if button.Importance != widget.LowImportance {
		t.Fatalf("button importance = %v, want LowImportance", button.Importance)
	}
}

func TestNewQueryRetrieveButtonIsNativeIconOnly(t *testing.T) {
	tapped := 0
	button := newQueryRetrieveButton(func() {
		tapped++
	})

	if button.Text != "" {
		t.Fatalf("button text = %q, want empty icon-only button", button.Text)
	}
	if button.Icon == nil || button.Icon.Name() != theme.DownloadIcon().Name() {
		t.Fatalf("button icon = %#v, want download icon", button.Icon)
	}
	if button.Importance != widget.LowImportance {
		t.Fatalf("button importance = %v, want LowImportance", button.Importance)
	}

	button.OnTapped()
	if tapped != 1 {
		t.Fatalf("button tap count = %d, want 1", tapped)
	}
}

func TestArchiveActivityRowsMarkRecentOperationsDismissible(t *testing.T) {
	state := &uiState{
		activeRetrieveCancel: func() {},
		retrieveActivityNode: "remote-a",
		retrieveActivityProgress: retrieve.Progress{
			Remaining: 2,
			Completed: 3,
		},
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusSuccess},
			{Kind: operations.KindSendStore, Status: operations.StatusWarning},
		},
	}

	rows := archiveActivityRows(state)

	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].Dismissible || rows[0].OperationIndex != -1 || !strings.Contains(rows[0].Text, "Retrieve remote-a") {
		t.Fatalf("active row = %#v, want non-dismissible retrieve row", rows[0])
	}
	if !rows[1].Dismissible || rows[1].OperationIndex != 0 || !strings.Contains(rows[1].Text, "import success") {
		t.Fatalf("first operation row = %#v", rows[1])
	}
	if !rows[2].Dismissible || rows[2].OperationIndex != 1 || !strings.Contains(rows[2].Text, "send_store warning") {
		t.Fatalf("second operation row = %#v", rows[2])
	}
}

func TestArchiveActivityRowsShowsActiveQueryBeforeHistory(t *testing.T) {
	state := &uiState{
		activeQueryActivityLabel: "Study C-FIND 2 sources",
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusSuccess},
		},
	}

	rows := archiveActivityRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Text != "Query Study C-FIND 2 sources" || rows[0].Dismissible || rows[0].OperationIndex != -1 {
		t.Fatalf("active query row = %#v, want non-dismissible query row", rows[0])
	}
	if !rows[1].Dismissible || rows[1].OperationIndex != 0 || !strings.Contains(rows[1].Text, "import success") {
		t.Fatalf("history row = %#v", rows[1])
	}
}

func TestArchiveActivityRowsShowsActiveQueryProgressCounts(t *testing.T) {
	state := &uiState{}

	beginQueryActivity(state, "Study C-FIND 3 sources")
	recordQueryActivityProgress(state, queryActivityProgress{
		Attempted: 2,
		Total:     3,
		Matches:   14,
		Failures:  1,
	})
	rows := archiveActivityRows(state)

	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want active query only", rows)
	}
	for _, want := range []string{"Query Study C-FIND 3 sources", "2/3 src", "14 match", "1 fail"} {
		if !strings.Contains(rows[0].Text, want) {
			t.Fatalf("active query progress row missing %q in %q", want, rows[0].Text)
		}
	}
}

func TestArchiveActivityRowsShowsActiveSendBeforeHistory(t *testing.T) {
	state := &uiState{
		activeSendActivityLabel: "Study C-STORE remote-a",
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusSuccess},
		},
	}

	rows := archiveActivityRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Text != "Send Study C-STORE remote-a" || rows[0].Dismissible || rows[0].OperationIndex != -1 {
		t.Fatalf("active send row = %#v, want non-dismissible send row", rows[0])
	}
	if !rows[1].Dismissible || rows[1].OperationIndex != 0 || !strings.Contains(rows[1].Text, "import success") {
		t.Fatalf("history row = %#v", rows[1])
	}
}

func TestArchiveActivityRowsShowsActiveSendProgressCounts(t *testing.T) {
	state := &uiState{}

	beginSendActivity(state, "Study C-STORE remote-a")
	recordSendActivityProgress(state, send.Progress{
		Attempted: 3,
		Sent:      2,
		Failed:    1,
		Total:     5,
	})
	rows := archiveActivityRows(state)

	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want active send only", rows)
	}
	for _, want := range []string{"Send Study C-STORE remote-a", "3/5", "sent 2", "fail 1"} {
		if !strings.Contains(rows[0].Text, want) {
			t.Fatalf("active send progress row missing %q in %q", want, rows[0].Text)
		}
	}
}

func TestArchiveActivityRowsShowsActiveImportBeforeHistory(t *testing.T) {
	state := &uiState{
		activeImportActivityLabel: "folder-a",
		operations: []operations.Summary{
			{Kind: operations.KindSendStore, Status: operations.StatusSuccess},
		},
	}

	rows := archiveActivityRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Text != "Import folder-a" || rows[0].Dismissible || rows[0].OperationIndex != -1 {
		t.Fatalf("active import row = %#v, want non-dismissible import row", rows[0])
	}
	if !rows[1].Dismissible || rows[1].OperationIndex != 0 || !strings.Contains(rows[1].Text, "send_store success") {
		t.Fatalf("history row = %#v", rows[1])
	}
}

func TestArchiveActivityRowsShowsActiveImportProgressCounts(t *testing.T) {
	state := &uiState{}

	beginImportActivity(state, "folder-a")
	recordImportActivityProgress(state, archive.ImportProgress{
		ScannedFiles: 4,
		StoredFiles:  2,
		Duplicates:   1,
		InvalidFiles: 1,
	})
	rows := archiveActivityRows(state)

	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want active import only", rows)
	}
	for _, want := range []string{"Import folder-a", "scan 4", "store 2", "dup 1", "invalid 1"} {
		if !strings.Contains(rows[0].Text, want) {
			t.Fatalf("active import progress row missing %q in %q", want, rows[0].Text)
		}
	}
}

func TestArchiveActivityRowsUseCompactProgressWording(t *testing.T) {
	state := &uiState{}

	beginQueryActivity(state, "Study C-FIND 3 sources")
	recordQueryActivityProgress(state, queryActivityProgress{Attempted: 2, Total: 3, Matches: 14, Failures: 1})
	queryRow := archiveActivityRows(state)[0].Text
	for _, want := range []string{"2/3 src", "14 match", "1 fail"} {
		if !strings.Contains(queryRow, want) {
			t.Fatalf("compact query activity row missing %q in %q", want, queryRow)
		}
	}
	for _, reject := range []string{"sources,", "matches", "failed"} {
		if strings.Contains(queryRow, reject) {
			t.Fatalf("compact query activity row should not contain %q in %q", reject, queryRow)
		}
	}

	clearActiveQueryActivity(state)
	beginSendActivity(state, "Study C-STORE remote-a")
	recordSendActivityProgress(state, send.Progress{Attempted: 3, Sent: 2, Failed: 1, Warnings: 1, Total: 5})
	sendRow := archiveActivityRows(state)[0].Text
	for _, want := range []string{"3/5 sent 2", "fail 1", "warn 1"} {
		if !strings.Contains(sendRow, want) {
			t.Fatalf("compact send activity row missing %q in %q", want, sendRow)
		}
	}

	clearActiveSendActivity(state)
	beginImportActivity(state, "folder-a")
	recordImportActivityProgress(state, archive.ImportProgress{ScannedFiles: 4, StoredFiles: 2, Duplicates: 1, InvalidFiles: 1})
	importRow := archiveActivityRows(state)[0].Text
	for _, want := range []string{"scan 4", "store 2", "dup 1", "invalid 1"} {
		if !strings.Contains(importRow, want) {
			t.Fatalf("compact import activity row missing %q in %q", want, importRow)
		}
	}
	for _, reject := range []string{"scanned", "stored", "duplicates"} {
		if strings.Contains(importRow, reject) {
			t.Fatalf("compact import activity row should not contain %q in %q", reject, importRow)
		}
	}
}

func TestArchiveActivityListShowsDeterminateProgressForActiveSend(t *testing.T) {
	state := &uiState{}

	beginSendActivity(state, "Study C-STORE remote-a")
	recordSendActivityProgress(state, send.Progress{
		Attempted: 3,
		Sent:      2,
		Failed:    1,
		Total:     5,
	})
	list := newArchiveActivityList(widget.NewLabel(""), state)
	item := list.CreateItem()

	list.UpdateItem(0, item)

	progress := findProgressBar(item)
	if progress == nil {
		t.Fatal("active send Activity row should render a determinate progress bar")
	}
	if !progress.Visible() {
		t.Fatal("active send Activity progress bar should be visible")
	}
	if progress.Value != 0.6 {
		t.Fatalf("active send Activity progress = %v, want 0.6", progress.Value)
	}
	if infinite := findProgressBarInfinite(item); infinite != nil && infinite.Visible() {
		t.Fatal("active send Activity row should not show indeterminate progress")
	}
	if !containsCanvasRectangleColor(item, archiveOddRowColor) {
		t.Fatal("active send Activity row should use dark row background")
	}
	if got := countCanvasRectanglesWithColor(item, tableColumnDividerColor); got < 2 {
		t.Fatalf("active send Activity row dividers = %d, want right and bottom divider", got)
	}
}

func TestArchiveActivityListShowsIndeterminateProgressForActiveImport(t *testing.T) {
	state := &uiState{}

	beginImportActivity(state, "folder-a")
	recordImportActivityProgress(state, archive.ImportProgress{ScannedFiles: 4, StoredFiles: 2})
	list := newArchiveActivityList(widget.NewLabel(""), state)
	item := list.CreateItem()

	list.UpdateItem(0, item)

	if progress := findProgressBar(item); progress != nil && progress.Visible() {
		t.Fatal("active import Activity row should not show determinate progress without a known total")
	}
	infinite := findProgressBarInfinite(item)
	if infinite == nil {
		t.Fatal("active import Activity row should render an indeterminate progress bar")
	}
	if !infinite.Visible() {
		t.Fatal("active import Activity progress bar should be visible")
	}
}

func TestArchiveActivityListUsesStableProgressSlots(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	beginSendActivity(state, "Study C-STORE remote-a")
	recordSendActivityProgress(state, send.Progress{Attempted: 1, Total: 2})
	list := newArchiveActivityList(widget.NewLabel(""), state)
	item := list.CreateItem()

	list.UpdateItem(0, item)

	stack := item.(*fyne.Container)
	box := stack.Objects[1].(*fyne.Container).Objects[0].(*fyne.Container)
	if _, ok := box.Objects[1].(*fyne.Container); !ok {
		t.Fatalf("activity determinate progress object = %T, want stable slot container", box.Objects[1])
	}
	if _, ok := box.Objects[2].(*fyne.Container); !ok {
		t.Fatalf("activity indeterminate progress object = %T, want stable slot container", box.Objects[2])
	}
	if got := box.Objects[1].MinSize().Width; got < compactArchiveActivityProgressWidth {
		t.Fatalf("activity progress slot width = %.1f, want at least %.1f", got, compactArchiveActivityProgressWidth)
	}
}

func TestArchiveActivityRowDismissControlUsesTrailingSpacer(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusSuccess},
		},
	}
	list := newArchiveActivityList(widget.NewLabel(""), state)
	item := list.CreateItem()

	list.UpdateItem(0, item)

	stack := item.(*fyne.Container)
	box := stack.Objects[1].(*fyne.Container).Objects[0].(*fyne.Container)
	header := box.Objects[0].(*fyne.Container)
	if len(header.Objects) != 3 {
		t.Fatalf("activity row header objects = %d, want label, spacer, dismiss button", len(header.Objects))
	}
	if _, ok := header.Objects[1].(*layout.Spacer); !ok {
		t.Fatalf("activity row middle object = %T, want spacer before dismiss button", header.Objects[1])
	}
	dismiss, ok := header.Objects[2].(*widget.Button)
	if !ok {
		t.Fatalf("activity row trailing object = %T, want dismiss button", header.Objects[2])
	}
	if !dismiss.Visible() || dismiss.OnTapped == nil {
		t.Fatal("activity row trailing dismiss button should remain visible and actionable for history rows")
	}
}

func TestArchiveRailListsUseCompactRowHeights(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "HOROS", Host: "127.0.0.1", Port: 4007},
		},
	}

	albumList := newArchiveAlbumList(nil, widget.NewLabel(""), archiveTables{}, state)
	sourceList := newArchiveSourceList(state)

	tests := []struct {
		name string
		list *widget.List
		rows int
		want float32
	}{
		{name: "albums", list: albumList, rows: len(archiveAlbumRowsForState(state, time.Now())), want: compactArchiveRailListRowHeight},
		{name: "sources", list: sourceList, rows: len(archiveSourceRows(state)), want: compactArchiveRailListRowHeight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for id := 0; id < tt.rows; id++ {
				height, ok := listItemHeight(tt.list, id)
				if !ok {
					t.Fatalf("%s row %d should have an explicit compact height", tt.name, id)
				}
				if height != tt.want {
					t.Fatalf("%s row %d height = %.0f, want %.0f", tt.name, id, height, tt.want)
				}
			}
		})
	}
}

func TestArchiveRailItemsUseCompactIconSlots(t *testing.T) {
	album := newArchiveAlbumListItem()
	source := newArchiveSourceListItem()

	tests := []struct {
		name string
		slot *fyne.Container
	}{
		{name: "album selection", slot: album.selectionIconSlot},
		{name: "album icon", slot: album.albumIconSlot},
		{name: "source selection", slot: source.selectionIconSlot},
		{name: "source icon", slot: source.sourceIconSlot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.slot == nil {
				t.Fatal("rail icon slot should be wired")
			}
			size := tt.slot.MinSize()
			if size.Width != compactArchiveRailIconSlotSize || size.Height != compactArchiveRailIconSlotSize {
				t.Fatalf("rail icon slot size = %.0fx%.0f, want %.0fx%.0f", size.Width, size.Height, compactArchiveRailIconSlotSize, compactArchiveRailIconSlotSize)
			}
		})
	}
}

func TestArchiveActivityListUsesCompactRowHeights(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	beginSendActivity(state, "Study C-STORE remote-a")
	recordSendActivityProgress(state, send.Progress{Attempted: 1, Total: 2})

	list := newArchiveActivityList(widget.NewLabel(""), state)

	for id := range archiveActivityRows(state) {
		height, ok := listItemHeight(list, id)
		if !ok {
			t.Fatalf("activity row %d should have an explicit compact height", id)
		}
		if height != compactArchiveActivityRowHeight {
			t.Fatalf("activity row %d height = %.0f, want %.0f", id, height, compactArchiveActivityRowHeight)
		}
	}
}

func TestBeginAndClearQueryActivityRefreshArchiveChrome(t *testing.T) {
	state := &uiState{archiveActivity: widget.NewLabel("")}

	beginQueryActivity(state, "Series C-FIND remote-a")

	if state.activeQueryActivityLabel != "Series C-FIND remote-a" {
		t.Fatalf("activeQueryActivityLabel = %q", state.activeQueryActivityLabel)
	}
	if state.archiveActivity.Text != "Query Series C-FIND remote-a" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}

	clearActiveQueryActivity(state)

	if state.activeQueryActivityLabel != "" {
		t.Fatalf("activeQueryActivityLabel = %q, want empty", state.activeQueryActivityLabel)
	}
	if state.archiveActivity.Text != "No recent activity" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}
}

func TestBeginAndClearSendActivityRefreshArchiveChrome(t *testing.T) {
	state := &uiState{archiveActivity: widget.NewLabel("")}

	beginSendActivity(state, "Series C-STORE remote-a")

	if state.activeSendActivityLabel != "Series C-STORE remote-a" {
		t.Fatalf("activeSendActivityLabel = %q", state.activeSendActivityLabel)
	}
	if state.archiveActivity.Text != "Send Series C-STORE remote-a" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}

	clearActiveSendActivity(state)

	if state.activeSendActivityLabel != "" {
		t.Fatalf("activeSendActivityLabel = %q, want empty", state.activeSendActivityLabel)
	}
	if state.archiveActivity.Text != "No recent activity" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}
}

func TestBeginAndClearImportActivityRefreshArchiveChrome(t *testing.T) {
	state := &uiState{archiveActivity: widget.NewLabel("")}

	beginImportActivity(state, "folder-a")

	if state.activeImportActivityLabel != "folder-a" {
		t.Fatalf("activeImportActivityLabel = %q", state.activeImportActivityLabel)
	}
	if state.archiveActivity.Text != "Import folder-a" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}

	clearActiveImportActivity(state)

	if state.activeImportActivityLabel != "" {
		t.Fatalf("activeImportActivityLabel = %q, want empty", state.activeImportActivityLabel)
	}
	if state.archiveActivity.Text != "No recent activity" {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}
}

func TestDismissArchiveActivityRowRemovesSingleOperationAndPersistsHistory(t *testing.T) {
	fynetest.NewApp()
	historyPath := filepath.Join(t.TempDir(), "tasks.json")
	status := widget.NewLabel("")
	state := &uiState{
		activeRetrieveCancel: func() {},
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusSuccess},
			{Kind: operations.KindSendStore, Status: operations.StatusWarning},
		},
		operationHistoryPath: historyPath,
		operationDetail:      newTaskDetail(),
		selectedOperationRow: 1,
		archiveActivity:      widget.NewLabel("old activity"),
	}
	state.operationTable = newTaskTable(state)
	if err := operations.SaveHistory(historyPath, state.operations); err != nil {
		t.Fatal(err)
	}

	dismissArchiveActivityRow(status, state, 1)

	if len(state.operations) != 1 {
		t.Fatalf("len(operations) = %d, want 1", len(state.operations))
	}
	if state.operations[0].Kind != operations.KindSendStore {
		t.Fatalf("remaining operation kind = %q, want %q", state.operations[0].Kind, operations.KindSendStore)
	}
	if status.Text != "Activity row dismissed" {
		t.Fatalf("status = %q", status.Text)
	}
	if !strings.Contains(state.archiveActivity.Text, "send_store warning") || strings.Contains(state.archiveActivity.Text, "import success") {
		t.Fatalf("archive activity = %q", state.archiveActivity.Text)
	}
	history, err := operations.LoadHistory(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Kind != operations.KindSendStore {
		t.Fatalf("persisted history = %#v, want one send_store", history)
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
	for _, want := range []string{"remote-a", "8/10", "fail 1", "warn 4"} {
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

func TestQueryDestinationTextSummarizesActiveSourcePriority(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			LocalAETitle:    "GOPACS",
			ReceiverAddress: "0.0.0.0:11113",
		},
		nodes: []nodes.Node{
			{Name: "RADIANT"},
			{Name: "HOROSMINI", RetrieveMethod: nodes.RetrieveMethodGet},
			{Name: "OFFLINE", QueryDisabled: true},
		},
	}

	text := queryDestinationText(state)

	for _, want := range []string{
		"Retrieve to: GOPACS",
		"2 sources priority: RADIANT -> HOROSMINI",
		"mixed retrieve methods",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("destination text missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "OFFLINE") {
		t.Fatalf("destination text included disabled source: %q", text)
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

func TestQueryMoveDestinationOptionsIncludeActiveSourcePreferredDestinations(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{LocalAETitle: "GOPACS"},
		nodes: []nodes.Node{
			{Name: "RADIANT", PreferredMoveDestination: "GOPACS_STORE"},
			{Name: "HOROSMINI", PreferredMoveDestination: "HOROS_STORE"},
			{Name: "OFFLINE", PreferredMoveDestination: "OFF_STORE", QueryDisabled: true},
		},
		queryMoveDestination: "GOPACS_ALT",
	}

	options := queryMoveDestinationOptions(state)
	want := []string{"GOPACS", "GOPACS_STORE", "HOROS_STORE", "GOPACS_ALT"}

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

	if text != "2 studies found\nRADIANT / 192.168.100.26:11112" {
		t.Fatalf("summary text = %q", text)
	}
}

func TestQueryResultSummaryTextUsesActiveQueryLevel(t *testing.T) {
	tests := []struct {
		name string
		kind queryRunKind
		want string
	}{
		{name: "patient", kind: queryRunPatient, want: "2 patients found\nno source selected"},
		{name: "series", kind: queryRunSeries, want: "2 series found\nno source selected"},
		{name: "image", kind: queryRunImage, want: "2 images found\nno source selected"},
		{name: "study singular", kind: queryRunStudy, want: "1 study found\nno source selected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := []query.Match{{}, {}}
			if strings.Contains(tt.name, "singular") {
				queries = queries[:1]
			}
			state := &uiState{
				queries:   queries,
				lastQuery: lastQueryRequest{kind: tt.kind},
			}

			text := queryResultSummaryText(state)

			if text != tt.want {
				t.Fatalf("summary text = %q, want %q", text, tt.want)
			}
		})
	}
}

func TestQueryResultSummaryTextShowsSelectedRetrieveStatus(t *testing.T) {
	state := &uiState{
		queries: []query.Match{{
			QueryRetrieveLevel: "SERIES",
			StudyInstanceUID:   "1.2.3.study",
			SeriesInstanceUID:  "1.2.3.series",
			SourceNodeID:       "radiant",
		}},
		selectedQueryRow: 0,
		lastQuery:        lastQueryRequest{kind: queryRunSeries},
		nodes: []nodes.Node{{
			ID:   "radiant",
			Name: "RADIANT",
			Host: "192.168.100.26",
			Port: 11112,
		}},
	}

	text := queryResultSummaryText(state)

	want := "1 series found\nRADIANT / 192.168.100.26:11112\nSelected: retrieve series 1.2.3.series from RADIANT"
	if text != want {
		t.Fatalf("summary text = %q, want %q", text, want)
	}
}

func TestQueryResultSummaryTextShowsSelectedRetrieveOutcome(t *testing.T) {
	match := query.Match{
		QueryRetrieveLevel: "SERIES",
		StudyInstanceUID:   "1.2.3.study",
		SeriesInstanceUID:  "1.2.3.series",
		SourceNodeID:       "radiant",
	}
	state := &uiState{
		queries:           []query.Match{match},
		selectedQueryRow:  0,
		lastQuery:         lastQueryRequest{kind: queryRunSeries},
		queryRetrieveRows: map[string]string{queryRetrieveStatusKey(match): "retrieved series 1.2.3.series from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)"},
		nodes: []nodes.Node{{
			ID:   "radiant",
			Name: "RADIANT",
			Host: "192.168.100.26",
			Port: 11112,
		}},
	}

	text := queryResultSummaryText(state)

	want := "1 series found\nRADIANT / 192.168.100.26:11112\nSelected: retrieved series 1.2.3.series from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)"
	if text != want {
		t.Fatalf("summary text = %q, want %q", text, want)
	}
}

func TestQueryRetrieveRowStatusTextFormatsSuccessAndFailure(t *testing.T) {
	request := queryRetrieveRequest{
		label: "series 1.2.3.series",
		node:  nodes.Node{Name: "RADIANT"},
	}
	outcome := retrieve.Outcome{
		FinalStatus: 0x0000,
		Stored:      42,
		Failed:      0,
		Method:      retrieve.MethodMove,
	}

	if got := queryRetrieveSuccessRowStatus(request, outcome); got != "retrieved series 1.2.3.series from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)" {
		t.Fatalf("success status = %q", got)
	}
	if got := queryRetrieveFailureRowStatus(request); got != "retrieve failed for series 1.2.3.series from RADIANT" {
		t.Fatalf("failure status = %q", got)
	}
}

func TestQueryRowLocalStateShowsPostRetrieveOutcome(t *testing.T) {
	successMatch := query.Match{QueryRetrieveLevel: "STUDY", StudyInstanceUID: "1.2.3.study"}
	failedMatch := query.Match{QueryRetrieveLevel: "SERIES", StudyInstanceUID: "1.2.4.study", SeriesInstanceUID: "1.2.4.series"}
	state := &uiState{
		queryRetrieveRows: map[string]string{
			queryRetrieveStatusKey(successMatch): "retrieved study 1.2.3.study from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)",
			queryRetrieveStatusKey(failedMatch):  "retrieve failed for series 1.2.4.series from RADIANT",
		},
	}

	successText, successState := queryRowLocalStateCell(state, queryTableRow{match: successMatch})
	if successText != "Retrieved" || successState != queryLocalStateRetrieved {
		t.Fatalf("success local state = %q/%q", successText, successState)
	}

	failedText, failedState := queryRowLocalStateCell(state, queryTableRow{match: failedMatch})
	if failedText != "Failed" || failedState != queryLocalStateRetrieveFailed {
		t.Fatalf("failed local state = %q/%q", failedText, failedState)
	}
}

func TestQueryResultSummaryTextShowsMultiSourceMatches(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{SourceNodeName: "RADIANT", SourceHost: "192.168.100.26", SourcePort: 11112},
			{SourceNodeName: "HOROSMINI", SourceHost: "192.168.100.50", SourcePort: 4007},
			{SourceNodeName: "RADIANT", SourceHost: "192.168.100.26", SourcePort: 11112},
		},
	}

	text := queryResultSummaryText(state)

	want := "3 studies found\n2 sources: RADIANT, HOROSMINI"
	if text != want {
		t.Fatalf("summary text = %q, want %q", text, want)
	}
}

func TestQuerySelectedDetailsTextShowsTechnicalIdentifiers(t *testing.T) {
	state := &uiState{
		queries: []query.Match{{
			QueryRetrieveLevel: "IMAGE",
			StudyInstanceUID:   "1.2.study",
			SeriesInstanceUID:  "1.2.series",
			SOPClassUID:        "1.2.840.10008.5.1.4.1.1.2",
			SOPInstanceUID:     "1.2.sop",
			SourceNodeName:     "RADIANT",
			SourceHost:         "192.168.100.26",
			SourcePort:         11112,
		}},
		selectedQueryRow: 0,
	}

	text := querySelectedDetailsText(state)

	for _, want := range []string{
		"Level: IMAGE",
		"Study UID: 1.2.study",
		"Series UID: 1.2.series",
		"SOP Class UID: 1.2.840.10008.5.1.4.1.1.2",
		"SOP Instance UID: 1.2.sop",
		"Source: RADIANT / 192.168.100.26:11112",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("selected details missing %q in:\n%s", want, text)
		}
	}
}

func TestQuerySelectedDetailsTextShowsLocalAndRetrieveState(t *testing.T) {
	match := query.Match{
		QueryRetrieveLevel: "SERIES",
		StudyInstanceUID:   "1.2.study",
		SeriesInstanceUID:  "1.2.series",
		LocalState:         queryLocalStatePresent,
	}
	state := &uiState{
		queries:          []query.Match{match},
		selectedQueryRow: 0,
		queryRetrieveRows: map[string]string{
			queryRetrieveStatusKey(match): "retrieved series 1.2.series from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)",
		},
	}

	text := querySelectedDetailsText(state)

	for _, want := range []string{
		"Local State: Duplicate",
		"Retrieve Status: retrieved series 1.2.series from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("selected details missing %q in:\n%s", want, text)
		}
	}
}

func TestArchiveSelectedDetailsTextShowsTechnicalIdentifiers(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{
			StudyInstanceUID: "1.2.study",
			PatientName:      "DOE^JANE",
		}},
		selectedStudyRow: 0,
		series: []archive.Series{{
			SeriesInstanceUID: "1.2.series",
			SeriesNumber:      "4",
		}},
		selectedSeriesRow: 0,
		instances: []archive.Instance{{
			SOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
			SOPInstanceUID: "1.2.sop",
			StoredPath:     "/archive/study/image.dcm",
		}},
		selectedInstanceRow: 0,
	}

	text := archiveSelectedDetailsText(state)

	for _, want := range []string{
		"Study UID: 1.2.study",
		"Series UID: 1.2.series",
		"SOP Class UID: 1.2.840.10008.5.1.4.1.1.2",
		"SOP Instance UID: 1.2.sop",
		"Stored Path: /archive/study/image.dcm",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("archive selected details missing %q in:\n%s", want, text)
		}
	}
}

func TestArchiveSelectedDetailsTextHandlesNilState(t *testing.T) {
	if got := archiveSelectedDetailsText(nil); got != "No archive row selected" {
		t.Fatalf("nil selected details = %q, want empty-selection placeholder", got)
	}
}

func TestRefreshQueryResultSummaryUpdatesLabel(t *testing.T) {
	label := widget.NewLabel("")
	state := &uiState{
		queryResultSummaryLabel: label,
		queries:                 []query.Match{{}},
	}

	refreshQueryResultSummary(state)

	if label.Text != "1 study found\nno source selected" {
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

func TestQueryModalityColumnsMatchReferenceTwoColumnGrid(t *testing.T) {
	columns := queryModalityColumns()
	if len(columns) != 2 {
		t.Fatalf("modality columns = %d, want 2", len(columns))
	}
	wantLeft := []string{"CR", "CT", "MG", "XA", "RF", "NM", "DX", "ES", "PT", "SR"}
	wantRight := []string{"SC", "MR", "AU", "OT", "RG", "DR", "XC", "VL", "US"}
	if strings.Join(columns[0], "|") != strings.Join(wantLeft, "|") {
		t.Fatalf("left modality column = %q, want %q", strings.Join(columns[0], "|"), strings.Join(wantLeft, "|"))
	}
	if strings.Join(columns[1], "|") != strings.Join(wantRight, "|") {
		t.Fatalf("right modality column = %q, want %q", strings.Join(columns[1], "|"), strings.Join(wantRight, "|"))
	}

	var flattened []string
	for _, column := range columns {
		flattened = append(flattened, column...)
	}
	if strings.Join(flattened, "|") != strings.Join(queryModalityCodes, "|") {
		t.Fatalf("flattened modality columns = %q, want %q", strings.Join(flattened, "|"), strings.Join(queryModalityCodes, "|"))
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

func TestQueryDatePresetOptionsIncludeTodayAMPM(t *testing.T) {
	joined := strings.Join(queryDatePresetOptions, "|")
	if !strings.Contains(joined, queryDatePresetTodayAM) || !strings.Contains(joined, queryDatePresetTodayPM) {
		t.Fatalf("queryDatePresetOptions = %q, want Today AM and Today PM", joined)
	}
}

func TestQueryDatePresetOptionsIncludeLastNHours(t *testing.T) {
	joined := strings.Join(queryDatePresetOptions, "|")
	if !strings.Contains(joined, queryDatePresetLastNHours) {
		t.Fatalf("queryDatePresetOptions = %q, want Last N hours", joined)
	}
}

func TestQueryDatePresetOptionsIncludeFixedLastHourShortcuts(t *testing.T) {
	joined := strings.Join(queryDatePresetOptions, "|")
	for _, preset := range []string{
		queryDatePresetLast30Min,
		queryDatePresetLast1Hour,
		queryDatePresetLast2Hours,
		queryDatePresetLast3Hours,
		queryDatePresetLast6Hours,
		queryDatePresetLast8Hours,
		queryDatePresetLast12Hours,
		queryDatePresetLast24Hours,
	} {
		if !strings.Contains(joined, preset) {
			t.Fatalf("queryDatePresetOptions = %q, want %s", joined, preset)
		}
	}
}

func TestQueryDatePresetColumnsMatchReferenceRadioGrid(t *testing.T) {
	columns := queryDatePresetColumns()
	if len(columns) != 3 {
		t.Fatalf("preset columns = %d, want 3", len(columns))
	}
	wantLeft := []string{
		queryDatePresetAny,
		queryDatePresetTodayAM,
		queryDatePresetTodayPM,
		queryDatePresetToday,
		queryDatePresetYesterday,
		queryDatePresetDayBeforeYesterday,
		queryDatePresetLast2Days,
		queryDatePresetLast7Days,
	}
	wantMiddle := []string{
		queryDatePresetLastMonth,
		queryDatePresetLast3Months,
		queryDatePresetOn,
		queryDatePresetBetween,
	}
	wantRight := []string{
		queryDatePresetLast30Min,
		queryDatePresetLast1Hour,
		queryDatePresetLast2Hours,
		queryDatePresetLast3Hours,
		queryDatePresetLast6Hours,
		queryDatePresetLast8Hours,
		queryDatePresetLast12Hours,
		queryDatePresetLast24Hours,
		queryDatePresetLastNHours,
	}
	for index, want := range [][]string{wantLeft, wantMiddle, wantRight} {
		if strings.Join(columns[index], "|") != strings.Join(want, "|") {
			t.Fatalf("column %d = %q, want %q", index, strings.Join(columns[index], "|"), strings.Join(want, "|"))
		}
	}
}

func TestQueryDatePresetRadioGridKeepsSingleSelectionAcrossColumns(t *testing.T) {
	var changed []string
	grid := newQueryDatePresetRadioGrid(func(preset string) {
		changed = append(changed, preset)
	})

	grid.SetSelected(queryDatePresetToday)
	if grid.Selected() != queryDatePresetToday {
		t.Fatalf("selected preset = %q, want %q", grid.Selected(), queryDatePresetToday)
	}
	if grid.groups[0].Selected != queryDatePresetToday {
		t.Fatalf("left group selected = %q, want %q", grid.groups[0].Selected, queryDatePresetToday)
	}

	grid.groups[2].SetSelected(queryDatePresetLast2Hours)
	if grid.Selected() != queryDatePresetLast2Hours {
		t.Fatalf("selected preset after right column selection = %q, want %q", grid.Selected(), queryDatePresetLast2Hours)
	}
	if grid.groups[0].Selected != "" {
		t.Fatalf("left group selected after right column selection = %q, want cleared", grid.groups[0].Selected)
	}
	if grid.groups[2].Selected != queryDatePresetLast2Hours {
		t.Fatalf("right group selected = %q, want %q", grid.groups[2].Selected, queryDatePresetLast2Hours)
	}
	if got := changed[len(changed)-1]; got != queryDatePresetLast2Hours {
		t.Fatalf("last changed preset = %q, want %q", got, queryDatePresetLast2Hours)
	}
}

func TestQueryDatePresetOptionsIncludeOn(t *testing.T) {
	joined := strings.Join(queryDatePresetOptions, "|")
	if !strings.Contains(joined, queryDatePresetOn) {
		t.Fatalf("queryDatePresetOptions = %q, want On", joined)
	}
}

func TestQueryDatePresetOptionsIncludeBetween(t *testing.T) {
	joined := strings.Join(queryDatePresetOptions, "|")
	if !strings.Contains(joined, queryDatePresetBetween) {
		t.Fatalf("queryDatePresetOptions = %q, want Between", joined)
	}
}

func TestQueryDatePresetBetweenPreservesManualRange(t *testing.T) {
	if !queryDatePresetPreservesManualRange(queryDatePresetBetween) {
		t.Fatal("Between preset should preserve manual date/time range fields")
	}
	if queryDatePresetPreservesManualRange(queryDatePresetToday) {
		t.Fatal("fixed presets should not preserve manual date/time range fields")
	}
}

func TestQueryDateTimePresetRangeSupportsTodayAMPM(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name         string
		preset       string
		wantDateFrom string
		wantDateTo   string
		wantTimeFrom string
		wantTimeTo   string
	}{
		{
			name:         "today am",
			preset:       queryDatePresetTodayAM,
			wantDateFrom: "20260604",
			wantDateTo:   "20260604",
			wantTimeFrom: "000000",
			wantTimeTo:   "115959",
		},
		{
			name:         "today pm",
			preset:       queryDatePresetTodayPM,
			wantDateFrom: "20260604",
			wantDateTo:   "20260604",
			wantTimeFrom: "120000",
			wantTimeTo:   "235959",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dateFrom, dateTo, timeFrom, timeTo, ok := queryDateTimePresetRange(tt.preset, now)
			if !ok {
				t.Fatal("preset should be supported")
			}
			if dateFrom != tt.wantDateFrom || dateTo != tt.wantDateTo ||
				timeFrom != tt.wantTimeFrom || timeTo != tt.wantTimeTo {
				t.Fatalf("range = %q/%q %q/%q, want %q/%q %q/%q", dateFrom, dateTo, timeFrom, timeTo, tt.wantDateFrom, tt.wantDateTo, tt.wantTimeFrom, tt.wantTimeTo)
			}
		})
	}
}

func TestQueryDateTimePresetRangeWithOnDate(t *testing.T) {
	dateFrom, dateTo, timeFrom, timeTo, ok := queryDateTimePresetRangeWithInputs(queryDatePresetOn, "20260604", "", time.Now())
	if !ok {
		t.Fatal("On preset should be supported with an On Date")
	}
	if dateFrom != "20260604" || dateTo != "20260604" || timeFrom != "" || timeTo != "" {
		t.Fatalf("range = %q/%q %q/%q, want 20260604/20260604 empty time", dateFrom, dateTo, timeFrom, timeTo)
	}
}

func TestQueryDateTimePresetRangeWithOnDateRejectsEmptyDate(t *testing.T) {
	_, _, _, _, ok := queryDateTimePresetRangeWithInputs(queryDatePresetOn, " ", "", time.Now())
	if ok {
		t.Fatal("On preset should reject empty On Date")
	}
}

func TestStepDICOMDate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		delta int
		want  string
	}{
		{name: "next day", value: "20260604", delta: 1, want: "20260605"},
		{name: "previous day", value: "20260604", delta: -1, want: "20260603"},
		{name: "month rollover", value: "20260228", delta: 1, want: "20260301"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := stepDICOMDate(tt.value, tt.delta)
			if !ok {
				t.Fatal("stepDICOMDate should accept a valid DICOM DA")
			}
			if got != tt.want {
				t.Fatalf("stepDICOMDate(%q, %d) = %q, want %q", tt.value, tt.delta, got, tt.want)
			}
		})
	}
}

func TestStepDICOMDateRejectsInvalidDate(t *testing.T) {
	if got, ok := stepDICOMDate("2026-06-04", 1); ok || got != "" {
		t.Fatalf("invalid DICOM DA step = %q/%v, want rejected", got, ok)
	}
}

func TestStepPositiveInteger(t *testing.T) {
	tests := []struct {
		name  string
		value string
		delta int
		want  string
	}{
		{name: "increment", value: "6", delta: 1, want: "7"},
		{name: "decrement", value: "6", delta: -1, want: "5"},
		{name: "minimum", value: "1", delta: -1, want: "1"},
		{name: "invalid starts at minimum", value: "abc", delta: 1, want: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stepPositiveInteger(tt.value, tt.delta, 1); got != tt.want {
				t.Fatalf("stepPositiveInteger(%q, %d) = %q, want %q", tt.value, tt.delta, got, tt.want)
			}
		})
	}
}

func TestQueryManualDateInputsExposeStepperButtons(t *testing.T) {
	onDate := widget.NewEntry()
	onDate.SetText("20260604")
	lastHours := widget.NewEntry()
	lastHours.SetText("6")
	applied := 0
	controls := newQueryManualDateInputs(onDate, lastHours, func() {
		applied++
	})
	removeButtons := findButtonsWithIcon(controls, theme.ContentRemoveIcon())
	addButtons := findButtonsWithIcon(controls, theme.ContentAddIcon())
	if len(removeButtons) != 2 || len(addButtons) != 2 {
		t.Fatalf("manual date controls should expose two decrement and two increment buttons, got %d/%d", len(removeButtons), len(addButtons))
	}

	addButtons[0].OnTapped()
	if onDate.Text != "20260605" {
		t.Fatalf("On Date after increment = %q, want 20260605", onDate.Text)
	}
	removeButtons[1].OnTapped()
	if lastHours.Text != "5" {
		t.Fatalf("Last Hours after decrement = %q, want 5", lastHours.Text)
	}
	if applied != 2 {
		t.Fatalf("apply callback count = %d, want 2", applied)
	}
}

func TestQueryDateTimePresetRangeWithLastHours(t *testing.T) {
	tests := []struct {
		name         string
		now          time.Time
		hours        string
		wantDateFrom string
		wantDateTo   string
		wantTimeFrom string
		wantTimeTo   string
	}{
		{
			name:         "same day",
			now:          time.Date(2026, 6, 4, 12, 30, 15, 0, time.Local),
			hours:        "6",
			wantDateFrom: "20260604",
			wantDateTo:   "20260604",
			wantTimeFrom: "063015",
			wantTimeTo:   "123015",
		},
		{
			name:         "crosses midnight",
			now:          time.Date(2026, 6, 4, 1, 15, 0, 0, time.Local),
			hours:        "3",
			wantDateFrom: "20260603",
			wantDateTo:   "20260604",
			wantTimeFrom: "221500",
			wantTimeTo:   "011500",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dateFrom, dateTo, timeFrom, timeTo, ok := queryDateTimePresetRangeWithLastHours(queryDatePresetLastNHours, tt.hours, tt.now)
			if !ok {
				t.Fatal("Last N hours preset should be supported")
			}
			if dateFrom != tt.wantDateFrom || dateTo != tt.wantDateTo ||
				timeFrom != tt.wantTimeFrom || timeTo != tt.wantTimeTo {
				t.Fatalf("range = %q/%q %q/%q, want %q/%q %q/%q", dateFrom, dateTo, timeFrom, timeTo, tt.wantDateFrom, tt.wantDateTo, tt.wantTimeFrom, tt.wantTimeTo)
			}
		})
	}
}

func TestQueryDateTimePresetRangeSupportsFixedLastHourShortcuts(t *testing.T) {
	tests := []struct {
		name         string
		preset       string
		now          time.Time
		wantDateFrom string
		wantDateTo   string
		wantTimeFrom string
		wantTimeTo   string
	}{
		{
			name:         "last thirty minutes",
			preset:       queryDatePresetLast30Min,
			now:          time.Date(2026, 6, 4, 12, 30, 15, 0, time.Local),
			wantDateFrom: "20260604",
			wantDateTo:   "20260604",
			wantTimeFrom: "120015",
			wantTimeTo:   "123015",
		},
		{
			name:         "last two hours crosses midnight",
			preset:       queryDatePresetLast2Hours,
			now:          time.Date(2026, 6, 4, 1, 15, 0, 0, time.Local),
			wantDateFrom: "20260603",
			wantDateTo:   "20260604",
			wantTimeFrom: "231500",
			wantTimeTo:   "011500",
		},
		{
			name:         "last twenty four hours",
			preset:       queryDatePresetLast24Hours,
			now:          time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local),
			wantDateFrom: "20260603",
			wantDateTo:   "20260604",
			wantTimeFrom: "120000",
			wantTimeTo:   "120000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dateFrom, dateTo, timeFrom, timeTo, ok := queryDateTimePresetRange(tt.preset, tt.now)
			if !ok {
				t.Fatal("fixed last-hours preset should be supported")
			}
			if dateFrom != tt.wantDateFrom || dateTo != tt.wantDateTo ||
				timeFrom != tt.wantTimeFrom || timeTo != tt.wantTimeTo {
				t.Fatalf("range = %q/%q %q/%q, want %q/%q %q/%q", dateFrom, dateTo, timeFrom, timeTo, tt.wantDateFrom, tt.wantDateTo, tt.wantTimeFrom, tt.wantTimeTo)
			}
		})
	}
}

func TestQueryDateTimePresetRangeWithLastHoursRejectsInvalidHours(t *testing.T) {
	_, _, _, _, ok := queryDateTimePresetRangeWithLastHours(queryDatePresetLastNHours, "0", time.Now())
	if ok {
		t.Fatal("Last N hours should reject non-positive hours")
	}
}

func TestQueryActionButtonLabelsMatchWorkbenchActionBar(t *testing.T) {
	labels := queryActionButtonLabels()
	want := []string{"Query", "Query Patient", "Retrieve", "Verify"}

	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("query action labels = %q, want %q", strings.Join(labels, "|"), strings.Join(want, "|"))
	}
}

func TestQueryAdvancedActionButtonLabelsPreserveSeriesAndImages(t *testing.T) {
	labels := queryAdvancedActionButtonLabels()
	want := []string{"Series", "Images"}

	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("query advanced action labels = %q, want %q", strings.Join(labels, "|"), strings.Join(want, "|"))
	}
}

func TestQueryTabExposesDedicatedWorkspaceTitle(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if findLabelContaining(tab, "DICOM Query/Retrieve") == nil {
		t.Fatal("Query tab should expose dedicated workspace title")
	}
}

func TestQueryWorkspaceTitleUsesWindowHeaderChrome(t *testing.T) {
	title := workbenchWindowTitle(queryWorkspaceTitle)

	label := findLabelContaining(title, queryWorkspaceTitle)
	if label == nil {
		t.Fatal("Query workspace title chrome should include the title label")
	}
	if !label.TextStyle.Bold || label.Alignment != fyne.TextAlignCenter {
		t.Fatalf("Query workspace title style = %#v alignment = %v, want bold centered", label.TextStyle, label.Alignment)
	}
	if !containsCanvasRectangleColor(title, archiveHeaderRowColor) {
		t.Fatal("Query workspace title should use the workstation header background")
	}
	if got := countCanvasRectanglesWithColor(title, tableColumnDividerColor); got < 1 {
		t.Fatalf("Query workspace title divider rectangles = %d, want bottom divider", got)
	}
}

func TestWorkbenchWindowTitleLabelPreservesMutableTitleInHeaderChrome(t *testing.T) {
	label := workbenchCenteredTitle("DICOM Auto Query/Retrieve : Default Instance")

	title := workbenchWindowTitleLabel(label)
	label.SetText("DICOM Auto Query/Retrieve : Night CT")

	if findLabelContaining(title, "Night CT") != label {
		t.Fatal("window title chrome should preserve the mutable title label")
	}
	if !label.TextStyle.Bold || label.Alignment != fyne.TextAlignCenter {
		t.Fatalf("window title label style = %#v alignment = %v, want bold centered", label.TextStyle, label.Alignment)
	}
	if !containsCanvasRectangleColor(title, archiveHeaderRowColor) {
		t.Fatal("window title label chrome should use the workstation header background")
	}
	if got := countCanvasRectanglesWithColor(title, tableColumnDividerColor); got < 1 {
		t.Fatalf("window title label divider rectangles = %d, want bottom divider", got)
	}
}

func TestAutoQueryTitleAndProfileControlsShareHeaderChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectHeaderChromeWithTextAndSelect(
		tab,
		"DICOM Auto Query/Retrieve : Default Instance",
		autoQueryProfileDefault,
	) {
		t.Fatal("Auto Q/R title and profile controls should share one dark header bar")
	}
}

func TestQueryTabExposesReferenceQuickSearchFieldStrip(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	for _, field := range queryQuickSearchOptions {
		if findButtonWithText(tab, field) == nil {
			t.Fatalf("Query quick-search strip should expose field %q", field)
		}
	}
	customField := findButtonWithText(tab, queryQuickSearchCustomDICOMField)
	if customField == nil || customField.OnTapped == nil {
		t.Fatalf("Query quick-search strip should expose actionable %q field", queryQuickSearchCustomDICOMField)
	}
	customField.OnTapped()
	if findEntryWithPlaceholder(tab, "StudyID=ABC123") == nil {
		t.Fatal("tapping Custom DICOM field should update the search placeholder")
	}
}

func TestQueryQuickSearchFieldStripUsesHorizontalScroll(t *testing.T) {
	fynetest.NewApp()
	selectWidget := widget.NewSelect(queryQuickSearchOptions, nil)
	selectWidget.SetSelected(queryQuickSearchPatientName)

	strip := newQueryQuickSearchFieldStrip(selectWidget, widget.NewEntry())

	if findHorizontalScroll(strip) == nil {
		t.Fatal("Query quick-search field strip should be horizontally scrollable to preserve all reference segments")
	}
	if findButtonWithText(strip, queryQuickSearchCustomDICOMField) == nil {
		t.Fatalf("scrollable Query quick-search strip should still expose %q", queryQuickSearchCustomDICOMField)
	}
}

func TestQueryQuickSearchFieldStripUsesSegmentedWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	selectWidget := widget.NewSelect(queryQuickSearchOptions, nil)
	selectWidget.SetSelected(queryQuickSearchPatientName)

	strip := newQueryQuickSearchFieldStrip(selectWidget, widget.NewEntry())
	scroll := findHorizontalScroll(strip)
	if scroll == nil {
		t.Fatal("Query quick-search field strip should be horizontally scrollable")
	}
	if !containsCanvasRectangleColor(scroll.Content, archiveHeaderRowColor) {
		t.Fatal("Query quick-search field strip should use the workstation header background")
	}
	if got := countCanvasRectanglesWithColor(scroll.Content, tableColumnDividerColor); got < len(queryQuickSearchOptions) {
		t.Fatalf("Query quick-search strip divider rectangles = %d, want segment dividers plus row divider", got)
	}
}

func TestQueryQuickSearchFieldStripUsesStableSegmentSlots(t *testing.T) {
	fynetest.NewApp()
	selectWidget := widget.NewSelect(queryQuickSearchOptions, nil)
	selectWidget.SetSelected(queryQuickSearchPatientName)

	strip := newQueryQuickSearchFieldStrip(selectWidget, widget.NewEntry())
	scroll := findHorizontalScroll(strip)
	if scroll == nil {
		t.Fatal("Query quick-search field strip should be horizontally scrollable")
	}
	stack, ok := scroll.Content.(*fyne.Container)
	if !ok || len(stack.Objects) < 2 {
		t.Fatal("Query quick-search strip should wrap segments in workstation chrome")
	}
	row, ok := stack.Objects[1].(*fyne.Container)
	if !ok || len(row.Objects) != len(queryQuickSearchOptions) {
		t.Fatalf("Query quick-search strip segment count = %d, want %d", len(row.Objects), len(queryQuickSearchOptions))
	}
	if got := row.Objects[0].MinSize().Width; got < 112 {
		t.Fatalf("short query quick-search segment width = %.1f, want at least 112", got)
	}
}

func TestQueryQuickSearchFieldStripUpdatesPlaceholderFromSelectedSegment(t *testing.T) {
	fynetest.NewApp()
	selectWidget := widget.NewSelect(queryQuickSearchOptions, nil)
	selectWidget.SetSelected(queryQuickSearchPatientName)
	entry := widget.NewEntry()

	strip := newQueryQuickSearchFieldStrip(selectWidget, entry)

	if entry.PlaceHolder != "Patient Name" {
		t.Fatalf("initial query search placeholder = %q, want Patient Name", entry.PlaceHolder)
	}
	patientIDButton := findButtonWithText(strip, queryQuickSearchPatientID)
	if patientIDButton == nil || patientIDButton.OnTapped == nil {
		t.Fatalf("Query quick-search strip should expose actionable %q field", queryQuickSearchPatientID)
	}
	patientIDButton.OnTapped()
	if selectWidget.Selected != queryQuickSearchPatientID {
		t.Fatalf("selected query search field = %q, want %q", selectWidget.Selected, queryQuickSearchPatientID)
	}
	if entry.PlaceHolder != queryQuickSearchPatientID {
		t.Fatalf("updated query search placeholder = %q, want %q", entry.PlaceHolder, queryQuickSearchPatientID)
	}
}

func TestQueryTabExposesComposedSearchBarSubmitButton(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	searchEntry := findEntryWithPlaceholder(tab, "Patient Name")
	if searchEntry == nil {
		t.Fatal("Query tab should expose the main workstation search entry")
	}
	searchButton := findButtonWithIcon(tab, theme.SearchIcon())
	if searchButton == nil {
		t.Fatal("Query search bar should expose a trailing search button with a search icon")
	}
	if searchButton.Text != "" {
		t.Fatalf("Query search bar button text = %q, want icon-only", searchButton.Text)
	}
	if searchButton.OnTapped == nil {
		t.Fatal("Query search bar button should submit the current Study query")
	}
}

func TestQuerySearchBarUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findEntryPlaceholderInWorkbenchChrome(tab, "Patient Name") {
		t.Fatal("Query search bar should sit in a dark workbench strip")
	}
}

func TestQueryTabMovesTechnicalCriteriaIntoCollapsedAdvancedCriteria(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	item := findAccordionItem(tab, "Advanced Criteria")
	if item == nil {
		t.Fatal("Query tab should keep raw DICOM fields in an Advanced Criteria accordion")
	}
	if item.Open {
		t.Fatal("Query Advanced Criteria should default collapsed to keep the workstation Q/R surface dense")
	}
	for _, placeholder := range []string{"1.2.840...", "1.2.840.10008.5.1.4.1.1.2", "ACC123"} {
		if findEntryWithPlaceholder(item.Detail, placeholder) == nil {
			t.Fatalf("Advanced Criteria should preserve entry placeholder %q", placeholder)
		}
	}
	for _, action := range queryAdvancedActionButtonLabels() {
		button := findButtonWithText(item.Detail, action)
		if button == nil || button.OnTapped == nil {
			t.Fatalf("Advanced Criteria should preserve actionable %q button", action)
		}
	}
}

func TestQueryTabExposesCollapsedSelectedResultDetails(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	item := findAccordionItem(tab, "Selected Result Details")
	if item == nil {
		t.Fatal("Query tab should expose a collapsed selected-result technical details affordance")
	}
	if item.Open {
		t.Fatal("Selected Result Details should default collapsed to keep technical identifiers out of the default table")
	}
	if findLabelContaining(item.Detail, "No query result selected") == nil {
		t.Fatal("Selected Result Details should show an empty-selection placeholder")
	}
}

func TestQuerySelectedDetailsPanelCopiesSelectedStudyUID(t *testing.T) {
	app := fynetest.NewApp()
	state := &uiState{
		queries: []query.Match{{
			StudyInstanceUID:  "1.2.study",
			SeriesInstanceUID: "1.2.series",
			SOPClassUID:       "1.2.class",
			SOPInstanceUID:    "1.2.sop",
		}},
		selectedQueryRow: 0,
	}
	status := widget.NewLabel("")
	panel := newQuerySelectedDetailsPanel(state, status)

	button := findButtonWithText(panel, "Copy Study UID")
	if button == nil || button.OnTapped == nil {
		t.Fatal("Selected Result Details should expose Copy Study UID")
	}
	button.OnTapped()

	if got := app.Clipboard().Content(); got != "1.2.study" {
		t.Fatalf("clipboard = %q, want study UID", got)
	}
	if status.Text != "Copied Study UID" {
		t.Fatalf("status = %q, want copy confirmation", status.Text)
	}
}

func TestArchiveSelectedDetailsPanelCopiesSelectedStudyUID(t *testing.T) {
	app := fynetest.NewApp()
	state := &uiState{
		studies:          []archive.Study{{StudyInstanceUID: "1.2.study"}},
		selectedStudyRow: 0,
	}
	status := widget.NewLabel("")
	panel := newArchiveSelectedDetailsPanel(state, status)

	button := findButtonWithText(panel, "Copy Study UID")
	if button == nil || button.OnTapped == nil {
		t.Fatal("Selected Archive Details should expose Copy Study UID")
	}
	button.OnTapped()

	if got := app.Clipboard().Content(); got != "1.2.study" {
		t.Fatalf("clipboard = %q, want study UID", got)
	}
	if status.Text != "Copied Study UID" {
		t.Fatalf("status = %q, want copy confirmation", status.Text)
	}
}

func TestArchiveWorkbenchExposesCollapsedSelectedArchiveDetails(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	workbench := newArchiveWorkbench(nil, status, archiveTables{}, canvas.NewRectangle(color.Transparent), canvas.NewRectangle(color.Transparent), state)

	item := findAccordionItem(workbench, "Selected Archive Details")
	if item == nil {
		t.Fatal("Archive workbench should expose a collapsed selected-archive technical details affordance")
	}
	if item.Open {
		t.Fatal("Selected Archive Details should default collapsed to keep technical identifiers out of the default table")
	}
	if findLabelContaining(item.Detail, "No archive row selected") == nil {
		t.Fatal("Selected Archive Details should show an empty-selection placeholder")
	}
}

func TestQueryTabPlacesDateAndModalityPanelsSideBySide(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findSideBySideSectionTitles(tab, "Date", "Modalities") {
		t.Fatal("Query tab should place Date and Modalities panels side by side in the reference Q/R layout")
	}
}

func TestQueryDateModalityPanelsUseWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()

	panel := newQueryDateModalityPanel(widget.NewLabel("date body"), widget.NewLabel("modality body"))

	if !findSideBySideSectionTitles(panel, "Date", "Modalities") {
		t.Fatal("Query Date and Modalities chrome should preserve side-by-side section titles")
	}
	if got := countCanvasRectanglesWithColor(panel, archiveHeaderRowColor); got < 2 {
		t.Fatalf("Query Date/Modalities header backgrounds = %d, want one per panel", got)
	}
	if got := countCanvasRectanglesWithColor(panel, tableColumnDividerColor); got < 4 {
		t.Fatalf("Query Date/Modalities divider rectangles = %d, want panel borders and header dividers", got)
	}
}

func TestQueryDateModalityPanelsUseStableFilterWidths(t *testing.T) {
	fynetest.NewApp()

	panel := newQueryDateModalityPanel(widget.NewLabel("date body"), widget.NewLabel("modality body"))
	row, ok := panel.(*fyne.Container)
	if !ok || len(row.Objects) != 2 {
		t.Fatal("Query Date/Modalities panel should keep two stable filter slots")
	}
	if got := row.Objects[0].MinSize().Width; got < queryDateFilterPanelMinWidth {
		t.Fatalf("Query Date panel width = %.1f, want at least %.1f", got, queryDateFilterPanelMinWidth)
	}
	if got := row.Objects[1].MinSize().Width; got < queryModalityFilterPanelMinWidth {
		t.Fatalf("Query Modalities panel width = %.1f, want at least %.1f", got, queryModalityFilterPanelMinWidth)
	}
}

func TestQueryTabExposesKeepOnTopFooterCheckbox(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")
	label := "Keep this window on top of all other windows"

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	keepOnTop := findCheckWithText(tab, label)
	if keepOnTop == nil {
		t.Fatalf("Query tab should expose %q checkbox", label)
	}
	if keepOnTop.Checked {
		t.Fatal("Query keep-on-top checkbox should default off")
	}
	keepOnTop.SetChecked(true)
	if !state.queryKeepOnTop {
		t.Fatal("Query keep-on-top state did not update when checked")
	}
	keepOnTop.SetChecked(false)
	if state.queryKeepOnTop {
		t.Fatal("Query keep-on-top state did not update when unchecked")
	}
}

func TestQueryFooterUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsAndWorkbenchChromeExcluding(
		tab,
		[]string{queryKeepOnTopLabel, "0 studies found"},
		[]string{queryActionLabelQuery, "DICOM Nodes:"},
	) {
		t.Fatal("Query footer should place keep-on-top and result summary in a dark workbench strip")
	}
}

func TestQueryFooterSummaryUsesTrailingAlignment(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	_ = newQueryTab(nil, status, archiveTables{}, nil, state)

	if state.queryResultSummaryLabel == nil {
		t.Fatal("Query tab should retain its result summary label")
	}
	if state.queryResultSummaryLabel.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("Query footer summary alignment = %v, want trailing", state.queryResultSummaryLabel.Alignment)
	}
}

func TestQueryTabExposesDisabledAutoRetrieveSettingsButton(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if findCheckWithText(tab, queryAutoRetrieveLabel) == nil {
		t.Fatalf("Query tab should expose %q checkbox", queryAutoRetrieveLabel)
	}
	settingsButton := findButtonWithText(tab, autoQuerySettingsButtonText)
	if settingsButton == nil {
		t.Fatalf("Query tab should expose %q beside Auto-Retrieve", autoQuerySettingsButtonText)
	}
	if !settingsButton.Disabled() {
		t.Fatal("manual Query Auto-Retrieve Settings should stay disabled until guarded execution is implemented")
	}
}

func TestQueryTabExposesDicomNodesPriorityInstruction(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if findLabelContaining(tab, "DICOM Nodes:") == nil {
		t.Fatal("Query tab should label its source list as DICOM Nodes in the source panel header")
	}
	if findLabelContaining(tab, "Drag sources into the priority order for retrieving") == nil {
		t.Fatal("Query tab should expose the source-priority instruction")
	}
}

func TestQueryTabExposesSourceColumnHeader(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTexts(tab, "Name", "Address", "AETitle") {
		t.Fatal("Query source panel should expose Name, Address, and AETitle column headers")
	}
}

func TestQueryTabUsesLowImportanceSourcePriorityButtons(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	for _, tt := range []struct {
		name string
		icon fyne.Resource
	}{
		{name: "up", icon: theme.MoveUpIcon()},
		{name: "down", icon: theme.MoveDownIcon()},
	} {
		button := findButtonWithIconAndText(tab, tt.icon, "")
		if button == nil {
			t.Fatalf("Query source %s priority button should be icon-only", tt.name)
		}
		if button.Importance != widget.LowImportance {
			t.Fatalf("Query source %s priority importance = %v, want LowImportance", tt.name, button.Importance)
		}
	}
}

func TestQuerySourceColumnHeaderUsesWorkbenchHeaderChrome(t *testing.T) {
	header := newQuerySourceColumnHeader()

	if !containsCanvasRectangleColor(header, archiveHeaderRowColor) {
		t.Fatal("Query source column header should use the workstation table header background")
	}
	if countCanvasRectangles(header) < 3 {
		t.Fatal("Query source column header should include background plus row and column dividers")
	}
}

func TestQuerySourceColumnHeaderUsesInternalColumnDividers(t *testing.T) {
	header := newQuerySourceColumnHeader()

	if got := countCanvasRectanglesWithColor(header, tableColumnDividerColor); got < 4 {
		t.Fatalf("source header divider rectangles = %d, want outer row/column plus internal Name/Address/AETitle dividers", got)
	}
}

func TestDicomNodesSourcePanelUsesWorkstationWidth(t *testing.T) {
	fynetest.NewApp()

	panel := newDicomNodesSourcePanel(widget.NewLabel("DICOM Nodes:"), widget.NewLabel("body"))

	if panel.Size().Width < 360 {
		t.Fatalf("DICOM Nodes source panel width = %.0f, want at least 360 for Name/Address/AETitle columns", panel.Size().Width)
	}
}

func TestQueryTabDisablesRetrieveUntilResultSelected(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	retrieveButton := findButtonWithText(tab, queryActionLabelRetrieve)
	if retrieveButton == nil {
		t.Fatal("Query tab should expose the Retrieve action")
	}
	if !retrieveButton.Disabled() {
		t.Fatal("Query Retrieve action should start disabled until a retrievable result is selected")
	}
}

func TestQueryTabEnablesRetrieveAfterRetrievableResultSelected(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		queries: []query.Match{{PatientName: "DOE^JANE", StudyInstanceUID: "1.2.3"}},
	}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)
	retrieveButton := findButtonWithText(tab, queryActionLabelRetrieve)
	if retrieveButton == nil {
		t.Fatal("Query tab should expose the Retrieve action")
	}
	if !retrieveButton.Disabled() {
		t.Fatal("Query Retrieve action should start disabled before a row is selected")
	}
	table := findTable(tab)
	if table == nil {
		t.Fatal("Query tab should expose a results table")
	}

	table.OnSelected(widget.TableCellID{Row: 1, Col: queryTableColumnPatient})

	if retrieveButton.Disabled() {
		t.Fatal("Query Retrieve action should enable after selecting a retrievable result row")
	}
}

func TestQueryTabUsesIconOnlyRefreshButton(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	refreshButton := findButtonWithIconAndText(tab, theme.ViewRefreshIcon(), "")
	if refreshButton == nil {
		t.Fatal("Query refresh action should be a reference-style icon-only refresh button")
	}
	if refreshButton.OnTapped == nil {
		t.Fatal("Query refresh action should remain actionable")
	}
}

func TestQueryActionStripGroupsDestinationWithPrimaryActions(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsExcluding(
		tab,
		[]string{"Retrieve to", queryActionLabelQuery, queryActionLabelPatient, queryActionLabelRetrieve, queryActionLabelVerify},
		[]string{"Refresh"},
	) {
		t.Fatal("Query action strip should group Retrieve to with Query, Query Patient, Retrieve, and Verify in the primary Q/R action cluster")
	}
}

func TestQueryActionStripUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsAndWorkbenchChromeExcluding(
		tab,
		[]string{"Retrieve to", queryActionLabelQuery, queryActionLabelPatient, queryActionLabelRetrieve, queryActionLabelVerify},
		[]string{"Refresh"},
	) {
		t.Fatal("Query action strip should use dark workbench chrome without merging the Refresh controls into the primary action cluster")
	}
}

func TestQueryPrimaryActionStripUsesStableButtonSlots(t *testing.T) {
	fynetest.NewApp()
	queryButton := widget.NewButtonWithIcon(queryActionLabelQuery, theme.MediaPlayIcon(), nil)

	strip := newQueryPrimaryActionStrip(queryButton)
	stack, ok := strip.(*fyne.Container)
	if !ok || len(stack.Objects) < 2 {
		t.Fatal("Query primary action strip should use workstation stack chrome")
	}
	content, ok := stack.Objects[1].(*fyne.Container)
	if !ok || len(content.Objects) == 0 {
		t.Fatal("Query primary action strip should wrap row content")
	}
	row, ok := content.Objects[0].(*fyne.Container)
	if !ok || len(row.Objects) == 0 {
		t.Fatal("Query primary action strip should keep action buttons in a row")
	}
	if got := row.Objects[0].MinSize().Width; got < 116 {
		t.Fatalf("query primary action button slot width = %.1f, want at least 116", got)
	}
}

func TestQueryRefreshClusterUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsAndWorkbenchChromeExcluding(
		tab,
		[]string{"Refresh", "Auto-Retrieve", autoQuerySettingsButtonText},
		[]string{queryActionLabelQuery, queryActionLabelPatient},
	) {
		t.Fatal("Query refresh and Auto-Retrieve controls should sit in a dark workbench strip separate from primary actions")
	}
}

func TestQueryRefreshClusterUsesStableCadenceSlot(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findSelectSlotWithMinWidth(tab, queryRefreshModeDont, queryRefreshCadenceSlotWidth) {
		t.Fatal("Query refresh cadence selector should sit in a stable-width slot")
	}
}

func TestQueryRefreshClusterUsesStableCountdownSlot(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findLabelSlotWithPrefixAndMinWidth(tab, "Next:", queryRefreshCountdownSlotWidth) {
		t.Fatal("Query refresh countdown label should sit in a stable-width slot")
	}
}

func TestQueryRefreshClusterUsesStableAutoRetrieveSlots(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	if !findCheckSlotWithTextAndMinWidth(tab, queryAutoRetrieveLabel, queryAutoRetrieveSlotWidth) {
		t.Fatal("Query Auto-Retrieve checkbox should sit in a stable-width slot")
	}
	if !findButtonSlotWithTextAndMinWidth(tab, autoQuerySettingsButtonText, queryAutoRetrieveSettingsSlotWidth) {
		t.Fatal("Query Auto-Retrieve Settings button should sit in a stable-width slot")
	}
}

func TestQueryPrimaryActionButtonsUseCompactImportance(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newQueryTab(nil, status, archiveTables{}, nil, state)

	for _, label := range queryActionButtonLabels() {
		button := findButtonWithText(tab, label)
		if button == nil {
			t.Fatalf("Query tab should expose %q action", label)
		}
		if button.Importance != widget.LowImportance {
			t.Fatalf("Query action %q importance = %v, want LowImportance", label, button.Importance)
		}
	}
}

func TestMainToolbarButtonLabelsAreCompact(t *testing.T) {
	labels := mainToolbarButtonLabels()
	want := []string{
		"Open", "Inspect", "Import", "Export", "Folder", "Refresh",
		"Query", "Send Study", "Send Series", "Send Image", "Get Series", "Get Image",
		"Cancel", "Anonymize", "Meta-Data", "Add", "Edit", "Delete", "Verify", "Listen", "Stop", "Settings",
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

func TestMainToolbarButtonGroupsSeparateOperationalClusters(t *testing.T) {
	groups := mainToolbarButtonGroups()
	want := [][]string{
		{"Open", "Inspect", "Import", "Export", "Folder", "Refresh"},
		{"Query", "Send Study", "Send Series", "Send Image"},
		{"Get Series", "Get Image", "Cancel"},
		{"Anonymize", "Meta-Data", "Add", "Edit", "Delete", "Verify"},
		{"Listen", "Stop", "Settings"},
	}

	if len(groups) != len(want) {
		t.Fatalf("toolbar group count = %d, want %d: %#v", len(groups), len(want), groups)
	}
	for i := range want {
		if strings.Join(groups[i], "|") != strings.Join(want[i], "|") {
			t.Fatalf("toolbar group %d = %q, want %q", i, strings.Join(groups[i], "|"), strings.Join(want[i], "|"))
		}
	}
	if strings.Join(mainToolbarButtonLabels(), "|") != strings.Join(flattenStringGroups(want), "|") {
		t.Fatal("toolbar labels should preserve grouped ordering")
	}
}

func TestMainToolbarDisabledLabelsExposeUnavailableParityActions(t *testing.T) {
	labels := mainToolbarDisabledLabels()
	want := []string{"Anonymize"}

	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("disabled toolbar labels = %q, want %q", strings.Join(labels, "|"), strings.Join(want, "|"))
	}
}

func TestGroupedToolbarActionsUseWorkbenchChrome(t *testing.T) {
	buttons := map[string]fyne.CanvasObject{}
	for _, label := range mainToolbarButtonLabels() {
		buttons[label] = mainToolbarAction(label, theme.ContentAddIcon(), nil)
	}

	actions := groupedToolbarActions(buttons)

	if findLabelContaining(actions, toolbarLabelQuery) == nil || findLabelContaining(actions, toolbarLabelSettings) == nil {
		t.Fatal("grouped toolbar actions should preserve toolbar labels")
	}
	if !containsCanvasRectangleColor(actions, archiveHeaderRowColor) {
		t.Fatal("grouped toolbar actions should use dark workbench chrome")
	}
	if got := countCanvasRectanglesWithColor(actions, tableColumnDividerColor); got < 2 {
		t.Fatalf("toolbar action divider rectangles = %d, want chrome column and row dividers", got)
	}
}

func TestGroupedToolbarActionsUseStableGroupSeparatorSlots(t *testing.T) {
	buttons := map[string]fyne.CanvasObject{}
	for _, label := range mainToolbarButtonLabels() {
		buttons[label] = mainToolbarAction(label, theme.ContentAddIcon(), nil)
	}

	actions := groupedToolbarActions(buttons)

	want := len(mainToolbarButtonGroups()) - 1
	if got := countToolbarGroupSeparatorSlots(actions); got != want {
		t.Fatalf("toolbar group separator slots = %d, want %d", got, want)
	}
}

func countToolbarGroupSeparatorSlots(obj fyne.CanvasObject) int {
	const expectedToolbarGroupSeparatorSlotWidth float32 = 12
	containerObj, ok := obj.(*fyne.Container)
	if !ok {
		return 0
	}
	count := 0
	if len(containerObj.Objects) == 1 {
		if _, ok := containerObj.Objects[0].(*widget.Separator); ok && containerObj.MinSize().Width >= expectedToolbarGroupSeparatorSlotWidth {
			count++
		}
	}
	for _, child := range containerObj.Objects {
		count += countToolbarGroupSeparatorSlots(child)
	}
	return count
}

func TestMainToolbarActionUsesVerticalIconLabel(t *testing.T) {
	tapped := false
	action := mainToolbarAction("Open", theme.FolderOpenIcon(), func() {
		tapped = true
	})
	if len(action.Objects) != 2 {
		t.Fatalf("toolbar action object count = %d, want button plus label", len(action.Objects))
	}
	iconSlot, ok := action.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("toolbar action first object = %T, want icon slot container", action.Objects[0])
	}
	captionSlot, ok := action.Objects[1].(*fyne.Container)
	if !ok {
		t.Fatalf("toolbar action second object = %T, want caption slot container", action.Objects[1])
	}
	button := findButtonWithIconAndText(iconSlot, theme.FolderOpenIcon(), "")
	if button == nil {
		t.Fatal("toolbar action icon slot should contain the icon-only button")
	}
	label := findLabelContaining(captionSlot, "Open")
	if label == nil {
		t.Fatal("toolbar action caption slot should contain the action label")
	}

	if label.Text != "Open" {
		t.Fatalf("toolbar action label = %q, want Open", label.Text)
	}
	if label.Alignment != fyne.TextAlignCenter {
		t.Fatalf("toolbar action label alignment = %v, want center", label.Alignment)
	}
	if button.Text != "" {
		t.Fatalf("toolbar action button text = %q, want icon-only", button.Text)
	}
	if button.Icon == nil {
		t.Fatal("toolbar action button icon is nil")
	}
	button.OnTapped()
	if !tapped {
		t.Fatal("toolbar action should preserve the tapped callback")
	}
}

func TestMainToolbarActionUsesStableIconSlot(t *testing.T) {
	action := mainToolbarAction("Open", theme.FolderOpenIcon(), nil)

	slot, ok := action.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("toolbar action icon object = %T, want stable icon slot container", action.Objects[0])
	}
	size := slot.MinSize()
	if size.Width != mainToolbarActionIconSlotSize || size.Height != mainToolbarActionIconSlotSize {
		t.Fatalf("toolbar action icon slot size = %.0fx%.0f, want %.0fx%.0f", size.Width, size.Height, mainToolbarActionIconSlotSize, mainToolbarActionIconSlotSize)
	}
	if findButtonWithIconAndText(slot, theme.FolderOpenIcon(), "") == nil {
		t.Fatal("toolbar action icon slot should preserve the icon-only button")
	}
}

func TestMainToolbarActionUsesStableCaptionSlot(t *testing.T) {
	action := mainToolbarAction("Send Series", theme.UploadIcon(), nil)

	slot, ok := action.Objects[1].(*fyne.Container)
	if !ok {
		t.Fatalf("toolbar action caption object = %T, want stable caption slot container", action.Objects[1])
	}
	size := slot.MinSize()
	if size.Width != mainToolbarActionCaptionSlotWidth {
		t.Fatalf("toolbar action caption slot width = %.0f, want %.0f", size.Width, mainToolbarActionCaptionSlotWidth)
	}
	label := findLabelContaining(slot, "Send Series")
	if label == nil {
		t.Fatal("toolbar action caption slot should preserve the action label")
	}
	if label.Alignment != fyne.TextAlignCenter {
		t.Fatalf("toolbar action caption alignment = %v, want center", label.Alignment)
	}
}

func TestMainToolbarActionUsesStableActionWidth(t *testing.T) {
	action := mainToolbarAction("Meta-Data", theme.SearchIcon(), nil)

	if got := action.MinSize().Width; got < mainToolbarActionSlotWidth {
		t.Fatalf("toolbar action width = %.0f, want at least %.0f", got, mainToolbarActionSlotWidth)
	}
}

func TestSelectAppTabByTextSelectsMatchingTab(t *testing.T) {
	tabs := container.NewAppTabs(
		container.NewTabItem("Archive", widget.NewLabel("archive")),
		container.NewTabItem("Query", widget.NewLabel("query")),
	)

	if !selectAppTabByText(tabs, "Query") {
		t.Fatal("selectAppTabByText should find Query tab")
	}
	if tabs.Selected() == nil || tabs.Selected().Text != "Query" {
		t.Fatalf("selected tab = %#v, want Query", tabs.Selected())
	}
	if selectAppTabByText(tabs, "Missing") {
		t.Fatal("selectAppTabByText should reject unknown tab")
	}
}

func TestOpenMetadataInspectorInspectsSelectedArchiveImage(t *testing.T) {
	tabs := container.NewAppTabs(
		container.NewTabItem("Archive", widget.NewLabel("archive")),
		container.NewTabItem("Inspector", widget.NewLabel("inspector")),
	)
	state := &uiState{
		instances:           []archive.Instance{{StoredPath: "/archive/image.dcm", SOPInstanceUID: "1.2.sop"}},
		selectedInstanceRow: 0,
	}
	status := widget.NewLabel("")
	inspected := false

	openMetadataInspector(tabs, status, state, func() {
		inspected = true
	})

	if tabs.Selected() == nil || tabs.Selected().Text != "Inspector" {
		t.Fatalf("selected tab = %#v, want Inspector", tabs.Selected())
	}
	if !inspected {
		t.Fatal("Meta-Data should inspect the selected archive image when one is selected")
	}
}

func TestOpenMetadataInspectorWithoutSelectedImageOnlyShowsInspector(t *testing.T) {
	tabs := container.NewAppTabs(
		container.NewTabItem("Archive", widget.NewLabel("archive")),
		container.NewTabItem("Inspector", widget.NewLabel("inspector")),
	)
	state := &uiState{selectedInstanceRow: -1}
	status := widget.NewLabel("")
	inspected := false

	openMetadataInspector(tabs, status, state, func() {
		inspected = true
	})

	if tabs.Selected() == nil || tabs.Selected().Text != "Inspector" {
		t.Fatalf("selected tab = %#v, want Inspector", tabs.Selected())
	}
	if inspected {
		t.Fatal("Meta-Data should not inspect when no archive image is selected")
	}
	if status.Text != "Showing metadata inspector" {
		t.Fatalf("status = %q, want Showing metadata inspector", status.Text)
	}
}

func flattenStringGroups(groups [][]string) []string {
	var flat []string
	for _, group := range groups {
		flat = append(flat, group...)
	}
	return flat
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
	if strings.Join(queryRefreshModeOptions, "|") != "Don't refresh|Refresh every 30 min" {
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

func TestAutoQueryTabExposesProfileCadenceAndResultShell(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		nodes: []nodes.Node{{
			Name:    "RADIANT",
			Host:    "192.168.100.26",
			Port:    11112,
			AETitle: "RADIANT",
		}},
	}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if findSelectWithSelected(tab, autoQueryProfileDefault) == nil {
		t.Fatalf("auto Q/R tab should expose profile %q", autoQueryProfileDefault)
	}
	if findSelectWithSelected(tab, autoQueryRefreshEvery30Min) == nil {
		t.Fatalf("auto Q/R tab should expose cadence %q", autoQueryRefreshEvery30Min)
	}
	autoRetrieve := findCheckWithText(tab, queryAutoRetrieveLabel)
	if autoRetrieve == nil {
		t.Fatalf("auto Q/R tab should expose %q checkbox", queryAutoRetrieveLabel)
	}
	autoRetrieve.SetChecked(true)
	if !state.autoQueryAutoRetrieve {
		t.Fatal("auto Q/R auto-retrieve checkbox did not update auto-query state")
	}
	if findButtonWithText(tab, "Settings") == nil {
		t.Fatal("auto Q/R tab should expose Settings button")
	}
	settingsButton := findButtonWithText(tab, "Settings")
	if settingsButton.OnTapped == nil {
		t.Fatal("auto Q/R Settings button should open settings instead of being decorative")
	}
	if findLabelContaining(tab, "0 studies found") == nil {
		t.Fatal("auto Q/R tab should expose initial studies-found result count")
	}
	if got := strings.Join(querySourceRows(state), "|"); !strings.Contains(got, "RADIANT") {
		t.Fatalf("auto Q/R sources should reuse query source rows, got %q", got)
	}
}

func TestAutoQueryFooterUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsAndWorkbenchChromeExcluding(
		tab,
		[]string{"0 studies found"},
		[]string{"DICOM Nodes:", queryActionLabelQuery},
	) {
		t.Fatal("Auto Q/R footer should place the result summary in a dark workbench strip")
	}
}

func TestAutoQueryRefreshClusterUsesStableCountdownSlot(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findLabelSlotWithPrefixAndMinWidth(tab, "Next:", queryRefreshCountdownSlotWidth) {
		t.Fatal("Auto Q/R refresh countdown label should sit in a stable-width slot")
	}
}

func TestAutoQueryRefreshClusterUsesStableAutoRetrieveSlots(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findCheckSlotWithTextAndMinWidth(tab, queryAutoRetrieveLabel, queryAutoRetrieveSlotWidth) {
		t.Fatal("Auto Q/R Auto-Retrieve checkbox should sit in a stable-width slot")
	}
	if !findButtonSlotWithTextAndMinWidth(tab, autoQuerySettingsButtonText, queryAutoRetrieveSettingsSlotWidth) {
		t.Fatal("Auto Q/R Auto-Retrieve Settings button should sit in a stable-width slot")
	}
}

func TestAutoQueryFooterSummaryUsesTrailingAlignment(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	_ = newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if state.autoQueryResultSummaryLabel == nil {
		t.Fatal("Auto Q/R tab should retain its result summary label")
	}
	if state.autoQueryResultSummaryLabel.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("Auto Q/R footer summary alignment = %v, want trailing", state.autoQueryResultSummaryLabel.Alignment)
	}
}

func TestAutoQueryTabExposesProfileWindowTitle(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if findLabelContaining(tab, "DICOM Auto Query/Retrieve : Default Instance") == nil {
		t.Fatal("Auto Q/R tab should expose the profile window-style title with the selected profile")
	}
}

func TestAutoQueryTabExposesProfileSearchBarSubmitButton(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	searchEntry := findEntryWithPlaceholder(tab, "Patient Name")
	if searchEntry == nil {
		t.Fatal("Auto Q/R tab should expose the profile search entry")
	}
	searchButton := findButtonWithIcon(tab, theme.SearchIcon())
	if searchButton == nil {
		t.Fatal("Auto Q/R search bar should expose a trailing search button with a search icon")
	}
	if searchButton.Text != "" {
		t.Fatalf("Auto Q/R search bar button text = %q, want icon-only", searchButton.Text)
	}
	if searchButton.OnTapped == nil {
		t.Fatal("Auto Q/R search bar button should submit the current profile Study query")
	}
}

func TestAutoQuerySearchBarUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findEntryPlaceholderInWorkbenchChrome(tab, "Patient Name") {
		t.Fatal("Auto Q/R search bar should sit in a dark workbench strip")
	}
}

func TestAutoQueryTabExposesReferenceQuickSearchFieldStrip(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if findHorizontalScroll(tab) == nil {
		t.Fatal("Auto Q/R quick-search field strip should be horizontally scrollable as a reference-style strip")
	}
	for _, field := range queryQuickSearchOptions {
		if findButtonWithText(tab, field) == nil {
			t.Fatalf("Auto Q/R quick-search strip should expose field %q", field)
		}
	}
	customField := findButtonWithText(tab, queryQuickSearchCustomDICOMField)
	if customField == nil || customField.OnTapped == nil {
		t.Fatalf("Auto Q/R quick-search strip should expose actionable %q field", queryQuickSearchCustomDICOMField)
	}
	customField.OnTapped()
	if findEntryWithPlaceholder(tab, "StudyID=ABC123") == nil {
		t.Fatal("tapping Auto Q/R Custom DICOM field should update the search placeholder")
	}
	if state.autoQuerySearchField != queryQuickSearchCustomDICOMField {
		t.Fatalf("auto Q/R search field state = %q, want %q", state.autoQuerySearchField, queryQuickSearchCustomDICOMField)
	}
}

func TestAutoQueryProfileBarUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findSelectWithSelectedInWorkbenchChrome(tab, autoQueryProfileDefault) {
		t.Fatal("Auto Q/R profile selector should sit in a dark workbench strip")
	}
}

func TestAutoQueryTabExposesDicomNodesPriorityInstruction(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if findLabelContaining(tab, "DICOM Nodes:") == nil {
		t.Fatal("Auto Q/R tab should label its source list as DICOM Nodes in the source panel header")
	}
	if findLabelContaining(tab, "Drag sources into the priority order for retrieving") == nil {
		t.Fatal("Auto Q/R tab should expose the source-priority instruction")
	}
}

func TestAutoQueryTabExposesSourceColumnHeader(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTexts(tab, "Name", "Address", "AETitle") {
		t.Fatal("Auto Q/R source panel should expose Name, Address, and AETitle column headers")
	}
}

func TestAutoQueryFilterPanelsUseWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findThreeColumnFilterPanelsWithWorkbenchChrome(tab, "DICOM Nodes:", "Date", "Modalities") {
		t.Fatal("Auto Q/R source, Date, and Modalities filters should share dark workbench panel chrome")
	}
}

func TestAutoQueryActionStripUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsAndWorkbenchChromeExcluding(
		tab,
		[]string{"Retrieve to", queryActionLabelQuery, queryActionLabelPatient, queryActionLabelRetrieve, queryActionLabelVerify},
		[]string{"Refresh"},
	) {
		t.Fatal("Auto Q/R action strip should use dark workbench chrome without merging the Refresh controls")
	}
}

func TestAutoQueryRefreshClusterUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if !findDirectContainerWithTextsAndWorkbenchChromeExcluding(
		tab,
		[]string{"Next:", queryAutoRetrieveLabel, autoQuerySettingsButtonText},
		[]string{"Retrieve to", queryActionLabelPatient},
	) {
		t.Fatal("Auto Q/R refresh and Auto-Retrieve controls should sit in a dark workbench strip separate from primary actions")
	}
}

func TestAutoQueryTabUsesIconOnlyRefreshButton(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	refreshButton := findButtonWithIconAndText(tab, theme.ViewRefreshIcon(), "")
	if refreshButton == nil {
		t.Fatal("Auto Q/R refresh action should be a reference-style icon-only refresh button")
	}
	if refreshButton.OnTapped == nil {
		t.Fatal("Auto Q/R refresh action should remain actionable")
	}
}

func TestAutoQuerySettingsDefaultsAreGuarded(t *testing.T) {
	settings := autoQuerySettingsForState(&uiState{})

	if settings.RetrieveLevel != autoQueryRetrieveLevelStudy {
		t.Fatalf("RetrieveLevel = %q, want %q", settings.RetrieveLevel, autoQueryRetrieveLevelStudy)
	}
	if settings.MaxMatches != autoQueryDefaultMaxMatches {
		t.Fatalf("MaxMatches = %q, want %q", settings.MaxMatches, autoQueryDefaultMaxMatches)
	}
	if settings.DuplicatePolicy != autoQueryDuplicatePolicySkipExisting {
		t.Fatalf("DuplicatePolicy = %q, want %q", settings.DuplicatePolicy, autoQueryDuplicatePolicySkipExisting)
	}
	if !settings.RequireConfirmation {
		t.Fatal("Auto Q/R settings should require confirmation by default")
	}
}

func TestApplyAutoQuerySettingsUpdatesState(t *testing.T) {
	state := &uiState{}
	applyAutoQuerySettings(state, autoQuerySettings{
		RetrieveLevel:       autoQueryRetrieveLevelSeries,
		MaxMatches:          "10",
		DuplicatePolicy:     autoQueryDuplicatePolicyKeepDuplicate,
		RequireConfirmation: false,
	})

	if state.autoQueryRetrieveLevel != autoQueryRetrieveLevelSeries {
		t.Fatalf("retrieve level = %q, want %q", state.autoQueryRetrieveLevel, autoQueryRetrieveLevelSeries)
	}
	if state.autoQueryMaxMatches != "10" {
		t.Fatalf("max matches = %q, want 10", state.autoQueryMaxMatches)
	}
	if state.autoQueryDuplicatePolicy != autoQueryDuplicatePolicyKeepDuplicate {
		t.Fatalf("duplicate policy = %q, want %q", state.autoQueryDuplicatePolicy, autoQueryDuplicatePolicyKeepDuplicate)
	}
	if state.autoQueryRequireConfirmation {
		t.Fatal("require confirmation should be false after applying unchecked setting")
	}
}

func TestPlanAutoQueryAutoRetrieveDisabled(t *testing.T) {
	state := &uiState{autoQueryAutoRetrieve: false}

	plan := planAutoQueryAutoRetrieve(state, []query.Match{{StudyInstanceUID: "1.2.3"}})

	if plan.Enabled {
		t.Fatal("disabled Auto Q/R auto-retrieve should not enable plan")
	}
	if len(plan.Candidates) != 0 {
		t.Fatalf("disabled Auto Q/R candidates = %d, want 0", len(plan.Candidates))
	}
	if plan.Message == "" {
		t.Fatal("disabled Auto Q/R plan should explain that retrieval is off")
	}
}

func TestPlanAutoQueryAutoRetrieveRequiresConfirmationByDefault(t *testing.T) {
	state := &uiState{autoQueryAutoRetrieve: true}

	plan := planAutoQueryAutoRetrieve(state, []query.Match{{StudyInstanceUID: "1.2.3"}})

	if !plan.Enabled {
		t.Fatal("enabled Auto Q/R should enable plan")
	}
	if !plan.RequiresConfirmation {
		t.Fatal("Auto Q/R auto-retrieve should require confirmation by default")
	}
	if len(plan.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(plan.Candidates))
	}
	if plan.Candidates[0].QueryRetrieveLevel != "STUDY" {
		t.Fatalf("candidate retrieve level = %q, want STUDY", plan.Candidates[0].QueryRetrieveLevel)
	}
}

func TestPlanAutoQueryAutoRetrieveAppliesMaxAndSkipsLocalStudies(t *testing.T) {
	state := &uiState{autoQueryAutoRetrieve: true}
	applyAutoQuerySettings(state, autoQuerySettings{
		RetrieveLevel:       autoQueryRetrieveLevelStudy,
		MaxMatches:          "2",
		DuplicatePolicy:     autoQueryDuplicatePolicySkipExisting,
		RequireConfirmation: false,
	})

	plan := planAutoQueryAutoRetrieve(state, []query.Match{
		{StudyInstanceUID: "1.2.local", LocalState: queryLocalStatePresent},
		{StudyInstanceUID: "1.2.remote.1"},
		{StudyInstanceUID: "1.2.remote.1"},
		{StudyInstanceUID: "1.2.remote.2"},
		{StudyInstanceUID: "1.2.remote.3"},
	})

	if plan.RequiresConfirmation {
		t.Fatal("plan should honor disabled confirmation")
	}
	if plan.SkippedLocal != 1 {
		t.Fatalf("SkippedLocal = %d, want 1", plan.SkippedLocal)
	}
	if !plan.Limited {
		t.Fatal("plan should report that max matches limited candidates")
	}
	if len(plan.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(plan.Candidates))
	}
	if plan.Candidates[0].StudyInstanceUID != "1.2.remote.1" || plan.Candidates[1].StudyInstanceUID != "1.2.remote.2" {
		t.Fatalf("candidates = %#v, want first two unique remote studies", plan.Candidates)
	}
}

func TestPlanAutoQueryAutoRetrieveRejectsDuplicates(t *testing.T) {
	state := &uiState{autoQueryAutoRetrieve: true}
	applyAutoQuerySettings(state, autoQuerySettings{
		RetrieveLevel:       autoQueryRetrieveLevelStudy,
		MaxMatches:          "25",
		DuplicatePolicy:     autoQueryDuplicatePolicyReject,
		RequireConfirmation: true,
	})

	plan := planAutoQueryAutoRetrieve(state, []query.Match{
		{StudyInstanceUID: "1.2.local", LocalState: queryLocalStatePresent},
		{StudyInstanceUID: "1.2.remote"},
	})

	if plan.Err == nil {
		t.Fatal("reject duplicates policy should block plan when a local duplicate is present")
	}
	if plan.RejectedLocal != 1 {
		t.Fatalf("RejectedLocal = %d, want 1", plan.RejectedLocal)
	}
	if len(plan.Candidates) != 0 {
		t.Fatalf("candidates = %d, want 0 when duplicates are rejected", len(plan.Candidates))
	}
}

func TestPlanAutoQueryAutoRetrieveRequiresUIDsForLevel(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		match    query.Match
		wantUIDs []string
	}{
		{
			name:     "study",
			level:    autoQueryRetrieveLevelStudy,
			match:    query.Match{StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series", SOPInstanceUID: "1.2.sop"},
			wantUIDs: []string{"1.2.study", "", ""},
		},
		{
			name:     "series",
			level:    autoQueryRetrieveLevelSeries,
			match:    query.Match{StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series"},
			wantUIDs: []string{"1.2.study", "1.2.series", ""},
		},
		{
			name:     "image",
			level:    autoQueryRetrieveLevelImage,
			match:    query.Match{StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series", SOPInstanceUID: "1.2.sop"},
			wantUIDs: []string{"1.2.study", "1.2.series", "1.2.sop"},
		},
		{
			name:  "series rejects missing series UID",
			level: autoQueryRetrieveLevelSeries,
			match: query.Match{StudyInstanceUID: "1.2.study"},
		},
		{
			name:  "image rejects missing SOP UID",
			level: autoQueryRetrieveLevelImage,
			match: query.Match{StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &uiState{autoQueryAutoRetrieve: true}
			applyAutoQuerySettings(state, autoQuerySettings{
				RetrieveLevel:       tt.level,
				MaxMatches:          "25",
				DuplicatePolicy:     autoQueryDuplicatePolicyKeepDuplicate,
				RequireConfirmation: true,
			})

			plan := planAutoQueryAutoRetrieve(state, []query.Match{tt.match})

			if len(tt.wantUIDs) == 0 {
				if len(plan.Candidates) != 0 {
					t.Fatalf("candidates = %d, want 0 for incomplete %s match", len(plan.Candidates), tt.level)
				}
				return
			}
			if len(plan.Candidates) != 1 {
				t.Fatalf("candidates = %d, want 1", len(plan.Candidates))
			}
			candidate := plan.Candidates[0]
			if candidate.QueryRetrieveLevel != strings.ToUpper(tt.level) ||
				candidate.StudyInstanceUID != tt.wantUIDs[0] ||
				candidate.SeriesInstanceUID != tt.wantUIDs[1] ||
				candidate.SOPInstanceUID != tt.wantUIDs[2] {
				t.Fatalf("candidate = %#v, want level/UIDs %#v", candidate, tt.wantUIDs)
			}
		})
	}
}

func TestAutoQueryMatchesHandlerRequiresConfirmationBeforeRetrieve(t *testing.T) {
	state := &uiState{
		autoQueryAutoRetrieve: true,
		queries:               []query.Match{{StudyInstanceUID: "1.2.3"}},
	}
	status := widget.NewLabel("")

	autoQueryMatchesHandler(nil, status, archiveTables{}, state)()

	if state.activeRetrieveCancel != nil {
		t.Fatal("Auto Q/R should not start retrieve before confirmation")
	}
	if !strings.Contains(status.Text, "awaiting confirmation") {
		t.Fatalf("status = %q, want awaiting confirmation", status.Text)
	}
}

func TestAutoQueryMatchesHandlerReportsDuplicatePolicyBlock(t *testing.T) {
	state := &uiState{
		autoQueryAutoRetrieve: true,
		queries:               []query.Match{{StudyInstanceUID: "1.2.local", LocalState: queryLocalStatePresent}},
	}
	applyAutoQuerySettings(state, autoQuerySettings{
		RetrieveLevel:       autoQueryRetrieveLevelStudy,
		MaxMatches:          "25",
		DuplicatePolicy:     autoQueryDuplicatePolicyReject,
		RequireConfirmation: true,
	})
	status := widget.NewLabel("")

	autoQueryMatchesHandler(nil, status, archiveTables{}, state)()

	if !strings.Contains(status.Text, "stopped by duplicate policy") {
		t.Fatalf("status = %q, want duplicate policy block", status.Text)
	}
}

func TestLoadAutoQueryProfilesAppliesDefaultProfileSettings(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Settings: autoquery.Settings{
			RetrieveLevel:       autoQueryRetrieveLevelImage,
			MaxMatches:          "7",
			DuplicatePolicy:     autoQueryDuplicatePolicyReject,
			RequireConfirmation: false,
		},
	}})

	if state.autoQueryRetrieveLevel != autoQueryRetrieveLevelImage {
		t.Fatalf("retrieve level = %q, want %q", state.autoQueryRetrieveLevel, autoQueryRetrieveLevelImage)
	}
	if state.autoQueryMaxMatches != "7" {
		t.Fatalf("max matches = %q, want 7", state.autoQueryMaxMatches)
	}
	if state.autoQueryDuplicatePolicy != autoQueryDuplicatePolicyReject {
		t.Fatalf("duplicate policy = %q, want %q", state.autoQueryDuplicatePolicy, autoQueryDuplicatePolicyReject)
	}
	if state.autoQueryRequireConfirmation {
		t.Fatal("require confirmation should reflect saved profile")
	}
}

func TestSaveAutoQueryDefaultProfilePersistsCurrentSettings(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{autoQueryProfileStore: store}
	applyAutoQuerySettings(state, autoQuerySettings{
		RetrieveLevel:       autoQueryRetrieveLevelSeries,
		MaxMatches:          "10",
		DuplicatePolicy:     autoQueryDuplicatePolicyKeepDuplicate,
		RequireConfirmation: false,
	})

	if err := saveAutoQueryDefaultProfile(state); err != nil {
		t.Fatalf("saveAutoQueryDefaultProfile: %v", err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].Name != autoquery.DefaultProfileName {
		t.Fatalf("profile name = %q, want %q", profiles[0].Name, autoquery.DefaultProfileName)
	}
	if profiles[0].Settings.RetrieveLevel != autoQueryRetrieveLevelSeries {
		t.Fatalf("stored retrieve level = %q, want %q", profiles[0].Settings.RetrieveLevel, autoQueryRetrieveLevelSeries)
	}
	if profiles[0].Settings.MaxMatches != "10" {
		t.Fatalf("stored max matches = %q, want 10", profiles[0].Settings.MaxMatches)
	}
	if profiles[0].Settings.DuplicatePolicy != autoQueryDuplicatePolicyKeepDuplicate {
		t.Fatalf("stored duplicate policy = %q, want %q", profiles[0].Settings.DuplicatePolicy, autoQueryDuplicatePolicyKeepDuplicate)
	}
	if profiles[0].Settings.RequireConfirmation {
		t.Fatal("stored confirmation should be false")
	}
}

func TestLoadAutoQueryProfilesAppliesDefaultProfileCriteria(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Criteria: autoquery.Criteria{
			SearchField: queryQuickSearchPatientID,
			SearchText:  "P123",
			DatePreset:  queryDatePresetToday,
			Modalities:  []string{"CT", "MR"},
			RefreshMode: autoQueryRefreshEvery30Min,
		},
	}})

	if state.autoQuerySearchField != queryQuickSearchPatientID {
		t.Fatalf("search field = %q, want %q", state.autoQuerySearchField, queryQuickSearchPatientID)
	}
	if state.autoQuerySearchText != "P123" {
		t.Fatalf("search text = %q, want P123", state.autoQuerySearchText)
	}
	if state.autoQueryDatePreset != queryDatePresetToday {
		t.Fatalf("date preset = %q, want %q", state.autoQueryDatePreset, queryDatePresetToday)
	}
	if strings.Join(state.autoQueryModalities, "\\") != "CT\\MR" {
		t.Fatalf("modalities = %q, want CT\\MR", strings.Join(state.autoQueryModalities, "\\"))
	}
	if state.autoQueryRefreshMode != autoQueryRefreshEvery30Min {
		t.Fatalf("refresh mode = %q, want %q", state.autoQueryRefreshMode, autoQueryRefreshEvery30Min)
	}
}

func TestLoadAutoQueryProfilesKeepsNamedProfileOptions(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{
			Name: autoquery.DefaultProfileName,
		},
		{
			Name: "Night CT",
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientID,
				SearchText:  "NIGHT",
				DatePreset:  queryDatePresetToday,
				Modalities:  []string{"CT"},
				RefreshMode: autoQueryRefreshEvery30Min,
			},
		},
	})

	if got := strings.Join(autoQueryProfileNames(state), "|"); got != "Default Instance|Night CT" {
		t.Fatalf("profile names = %q, want Default Instance|Night CT", got)
	}
	if state.autoQueryProfileName != autoquery.DefaultProfileName {
		t.Fatalf("selected profile = %q, want %q", state.autoQueryProfileName, autoquery.DefaultProfileName)
	}
}

func TestSelectAutoQueryProfileAppliesNamedSettingsAndCriteria(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{
			Name: autoquery.DefaultProfileName,
		},
		{
			Name: "Night CT",
			Settings: autoquery.Settings{
				RetrieveLevel:       autoQueryRetrieveLevelSeries,
				MaxMatches:          "12",
				DuplicatePolicy:     autoQueryDuplicatePolicyKeepDuplicate,
				RequireConfirmation: false,
			},
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientID,
				SearchText:  "NIGHT",
				DatePreset:  queryDatePresetToday,
				Modalities:  []string{"CT"},
				RefreshMode: autoQueryRefreshEvery30Min,
			},
		},
	})

	if !selectAutoQueryProfile(state, "Night CT") {
		t.Fatal("selectAutoQueryProfile should select an existing named profile")
	}
	if state.autoQueryProfileName != "Night CT" {
		t.Fatalf("selected profile = %q, want Night CT", state.autoQueryProfileName)
	}
	if state.autoQueryRetrieveLevel != autoQueryRetrieveLevelSeries {
		t.Fatalf("retrieve level = %q, want %q", state.autoQueryRetrieveLevel, autoQueryRetrieveLevelSeries)
	}
	if state.autoQuerySearchField != queryQuickSearchPatientID {
		t.Fatalf("search field = %q, want %q", state.autoQuerySearchField, queryQuickSearchPatientID)
	}
	if state.autoQuerySearchText != "NIGHT" {
		t.Fatalf("search text = %q, want NIGHT", state.autoQuerySearchText)
	}
	if strings.Join(state.autoQueryModalities, "\\") != "CT" {
		t.Fatalf("modalities = %q, want CT", strings.Join(state.autoQueryModalities, "\\"))
	}
}

func TestSelectAutoQueryProfileAppliesLockedState(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{Name: autoquery.DefaultProfileName},
		{Name: "Locked Night CT", Locked: true},
	})

	if !selectAutoQueryProfile(state, "Locked Night CT") {
		t.Fatal("selectAutoQueryProfile should select locked profile")
	}
	if !state.autoQueryProfileLocked {
		t.Fatal("selected profile should apply locked state")
	}
	if !autoQueryProfileLocked(state) {
		t.Fatal("autoQueryProfileLocked should report selected profile lock")
	}
}

func TestSaveAutoQueryDefaultProfilePersistsCurrentCriteria(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{
		autoQueryProfileStore: store,
		autoQuerySearchField:  queryQuickSearchPatientID,
		autoQuerySearchText:   "P123",
		autoQueryDatePreset:   queryDatePresetToday,
		autoQueryModalities:   []string{"CT", "MR"},
		autoQueryRefreshMode:  autoQueryRefreshEvery30Min,
	}

	if err := saveAutoQueryDefaultProfile(state); err != nil {
		t.Fatalf("saveAutoQueryDefaultProfile: %v", err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := profiles[0].Criteria.SearchField; got != queryQuickSearchPatientID {
		t.Fatalf("stored search field = %q, want %q", got, queryQuickSearchPatientID)
	}
	if got := profiles[0].Criteria.SearchText; got != "P123" {
		t.Fatalf("stored search text = %q, want P123", got)
	}
	if got := profiles[0].Criteria.DatePreset; got != queryDatePresetToday {
		t.Fatalf("stored date preset = %q, want %q", got, queryDatePresetToday)
	}
	if got := strings.Join(profiles[0].Criteria.Modalities, "\\"); got != "CT\\MR" {
		t.Fatalf("stored modalities = %q, want CT\\MR", got)
	}
	if got := profiles[0].Criteria.RefreshMode; got != autoQueryRefreshEvery30Min {
		t.Fatalf("stored refresh mode = %q, want %q", got, autoQueryRefreshEvery30Min)
	}
}

func TestSaveAutoQuerySelectedProfilePersistsLockedState(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{autoQueryProfileStore: store}
	loadAutoQueryProfiles(state, []autoquery.Profile{{Name: autoquery.DefaultProfileName}})
	setAutoQueryProfileLocked(state, true)

	if err := saveAutoQuerySelectedProfile(state); err != nil {
		t.Fatalf("saveAutoQuerySelectedProfile: %v", err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if !profiles[0].Locked {
		t.Fatal("saved profile should persist locked state")
	}
}

func TestSaveAutoQueryProfileCriteriaRejectsLockedProfile(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	original := []autoquery.Profile{{
		Name:   autoquery.DefaultProfileName,
		Locked: true,
		Criteria: autoquery.Criteria{
			SearchField: queryQuickSearchPatientName,
			SearchText:  "OLD",
		},
	}}
	if err := store.Save(original); err != nil {
		t.Fatalf("Save original: %v", err)
	}
	state := &uiState{autoQueryProfileStore: store}
	loadAutoQueryProfiles(state, original)
	status := widget.NewLabel("")

	if saveAutoQueryProfileCriteria(nil, status, state, autoquery.Criteria{SearchField: queryQuickSearchPatientName, SearchText: "NEW"}) {
		t.Fatal("saveAutoQueryProfileCriteria should reject locked profile")
	}
	if status.Text != "Auto Q/R profile is locked" {
		t.Fatalf("status = %q, want locked message", status.Text)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if profiles[0].Criteria.SearchText != "OLD" {
		t.Fatalf("locked profile criteria changed to %q, want OLD", profiles[0].Criteria.SearchText)
	}
}

func TestSaveAutoQuerySelectedProfilePreservesOtherProfiles(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{autoQueryProfileStore: store}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{
			Name: autoquery.DefaultProfileName,
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientName,
				SearchText:  "DEFAULT",
			},
		},
		{
			Name: "Night CT",
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientID,
				SearchText:  "OLD",
			},
		},
	})
	if !selectAutoQueryProfile(state, "Night CT") {
		t.Fatal("selectAutoQueryProfile should select Night CT")
	}
	state.autoQuerySearchText = "NEW"
	state.autoQueryModalities = []string{"CT", "MR"}

	if err := saveAutoQuerySelectedProfile(state); err != nil {
		t.Fatalf("saveAutoQuerySelectedProfile: %v", err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("len(profiles) = %d, want 2", len(profiles))
	}
	if profiles[0].Name != autoquery.DefaultProfileName || profiles[0].Criteria.SearchText != "DEFAULT" {
		t.Fatalf("default profile changed unexpectedly: %#v", profiles[0])
	}
	if profiles[1].Name != "Night CT" {
		t.Fatalf("second profile name = %q, want Night CT", profiles[1].Name)
	}
	if profiles[1].Criteria.SearchText != "NEW" {
		t.Fatalf("night profile search = %q, want NEW", profiles[1].Criteria.SearchText)
	}
	if got := strings.Join(profiles[1].Criteria.Modalities, "\\"); got != "CT\\MR" {
		t.Fatalf("night profile modalities = %q, want CT\\MR", got)
	}
}

func TestLoadAutoQueryProfileAppliesSourcePriority(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{ID: "node-1", Name: "RADIANT", Host: "10.0.0.1", Port: 104},
			{ID: "node-2", Name: "HOROS", Host: "10.0.0.2", Port: 105},
		},
	}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Sources: []autoquery.Source{
			{NodeID: "node-2", Enabled: true},
			{NodeID: "node-1", Enabled: false},
		},
	}})

	rows := autoQuerySourceRows(state)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0] != "  [x] HOROS 10.0.0.2:105" {
		t.Fatalf("first Auto Q/R source row = %q", rows[0])
	}
	if rows[1] != "  [ ] RADIANT 10.0.0.1:104" {
		t.Fatalf("second Auto Q/R source row = %q", rows[1])
	}
	got := autoQuerySourceNodes(state)
	if len(got) != 1 || got[0].ID != "node-2" {
		t.Fatalf("Auto Q/R source nodes = %+v, want node-2 only", got)
	}
}

func TestAutoQuerySourceCellUsesSeparateNodeColumns(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{ID: "node-1", Name: "RADIANT", AETitle: "RADIANT_AE", Host: "10.0.0.1", Port: 104},
		},
	}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Sources: []autoquery.Source{
			{NodeID: "node-1", Enabled: true},
		},
	}})
	state.selectedAutoQuerySourceRow = 0
	cell := newQuerySourceListCell()

	configureAutoQuerySourceCell(cell, state, 0, nil)

	if cell.check.Text != "" {
		t.Fatalf("Auto Q/R source checkbox text = %q, want empty text with separate labels", cell.check.Text)
	}
	if cell.nameLabel.Text != "▶ RADIANT" {
		t.Fatalf("name label = %q", cell.nameLabel.Text)
	}
	if cell.addressLabel.Text != "10.0.0.1:104" {
		t.Fatalf("address label = %q", cell.addressLabel.Text)
	}
	if cell.aeTitleLabel.Text != "RADIANT_AE" {
		t.Fatalf("AE title label = %q", cell.aeTitleLabel.Text)
	}
}

func TestSetAutoQuerySourceEnabledDoesNotMutateGlobalNode(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{ID: "node-1", Name: "RADIANT", Host: "10.0.0.1", Port: 104},
			{ID: "node-2", Name: "HOROS", Host: "10.0.0.2", Port: 105},
		},
	}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Sources: []autoquery.Source{
			{NodeID: "node-1", Enabled: true},
			{NodeID: "node-2", Enabled: true},
		},
	}})

	changed := setAutoQuerySourceEnabled(state, 0, false)

	if !changed {
		t.Fatal("setAutoQuerySourceEnabled should report profile source change")
	}
	if state.nodes[0].QueryDisabled {
		t.Fatalf("Auto Q/R source toggle should not mutate global node: %+v", state.nodes[0])
	}
	got := autoQuerySourceNodes(state)
	if len(got) != 1 || got[0].ID != "node-2" {
		t.Fatalf("Auto Q/R source nodes = %+v, want node-2 after disabling node-1 in profile", got)
	}
}

func TestMoveAutoQuerySourceReordersProfileOnly(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{ID: "node-1", Name: "RADIANT", Host: "10.0.0.1", Port: 104},
			{ID: "node-2", Name: "HOROS", Host: "10.0.0.2", Port: 105},
			{ID: "node-3", Name: "ORTHANC", Host: "10.0.0.3", Port: 106},
		},
		selectedAutoQuerySourceRow: 2,
	}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Sources: []autoquery.Source{
			{NodeID: "node-1", Enabled: true},
			{NodeID: "node-2", Enabled: true},
			{NodeID: "node-3", Enabled: true},
		},
	}})
	state.selectedAutoQuerySourceRow = 2

	changed := moveAutoQuerySource(state, -1)

	if !changed {
		t.Fatal("moveAutoQuerySource should report change")
	}
	if state.selectedAutoQuerySourceRow != 1 {
		t.Fatalf("selected auto source row = %d, want 1", state.selectedAutoQuerySourceRow)
	}
	got := nodeNames(autoQuerySourceNodes(state))
	if strings.Join(got, "|") != "RADIANT|ORTHANC|HOROS" {
		t.Fatalf("Auto Q/R node order = %q, want RADIANT|ORTHANC|HOROS", strings.Join(got, "|"))
	}
	if strings.Join(nodeNames(state.nodes), "|") != "RADIANT|HOROS|ORTHANC" {
		t.Fatalf("global node order changed: %q", strings.Join(nodeNames(state.nodes), "|"))
	}
}

func TestSaveAutoQuerySelectedProfilePersistsSourcePriority(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{
		autoQueryProfileStore: store,
		nodes: []nodes.Node{
			{ID: "node-1", Name: "RADIANT", Host: "10.0.0.1", Port: 104},
			{ID: "node-2", Name: "HOROS", Host: "10.0.0.2", Port: 105},
		},
	}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name: autoquery.DefaultProfileName,
		Sources: []autoquery.Source{
			{NodeID: "node-2", Enabled: true},
			{NodeID: "node-1", Enabled: false},
		},
	}})

	if err := saveAutoQuerySelectedProfile(state); err != nil {
		t.Fatalf("saveAutoQuerySelectedProfile: %v", err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if len(profiles[0].Sources) != 2 {
		t.Fatalf("saved sources = %#v, want 2 entries", profiles[0].Sources)
	}
	if profiles[0].Sources[0].NodeID != "node-2" || !profiles[0].Sources[0].Enabled {
		t.Fatalf("first saved source = %#v, want enabled node-2", profiles[0].Sources[0])
	}
	if profiles[0].Sources[1].NodeID != "node-1" || profiles[0].Sources[1].Enabled {
		t.Fatalf("second saved source = %#v, want disabled node-1", profiles[0].Sources[1])
	}
}

func TestAddAutoQueryProfileCreatesUniqueNamedInstance(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{{Name: autoquery.DefaultProfileName}})
	state.autoQuerySearchField = queryQuickSearchPatientID
	state.autoQuerySearchText = "P123"
	state.autoQueryDatePreset = queryDatePresetToday
	state.autoQueryModalities = []string{"CT"}
	state.autoQueryRefreshMode = autoQueryRefreshEvery30Min

	profile := addAutoQueryProfile(state)

	if profile.Name != "Instance 2" {
		t.Fatalf("new profile name = %q, want Instance 2", profile.Name)
	}
	if state.autoQueryProfileName != "Instance 2" {
		t.Fatalf("selected profile = %q, want Instance 2", state.autoQueryProfileName)
	}
	if got := strings.Join(autoQueryProfileNames(state), "|"); got != "Default Instance|Instance 2" {
		t.Fatalf("profile names = %q, want Default Instance|Instance 2", got)
	}
	if profile.Criteria.SearchText != "P123" || strings.Join(profile.Criteria.Modalities, "\\") != "CT" {
		t.Fatalf("new profile criteria = %#v, want current state criteria", profile.Criteria)
	}
}

func TestRemoveSelectedAutoQueryProfileKeepsDefaultProfile(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{Name: autoquery.DefaultProfileName},
		{Name: "Night CT"},
	})
	if !selectAutoQueryProfile(state, "Night CT") {
		t.Fatal("selectAutoQueryProfile should select Night CT")
	}

	if !removeSelectedAutoQueryProfile(state) {
		t.Fatal("removeSelectedAutoQueryProfile should remove non-default profile")
	}
	if got := strings.Join(autoQueryProfileNames(state), "|"); got != "Default Instance" {
		t.Fatalf("profile names = %q, want Default Instance", got)
	}
	if state.autoQueryProfileName != autoquery.DefaultProfileName {
		t.Fatalf("selected profile = %q, want %q", state.autoQueryProfileName, autoquery.DefaultProfileName)
	}
	if removeSelectedAutoQueryProfile(state) {
		t.Fatal("removeSelectedAutoQueryProfile should not remove Default Instance")
	}
}

func TestRenameSelectedAutoQueryProfileUpdatesSelectionAndPreservesData(t *testing.T) {
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{Name: autoquery.DefaultProfileName},
		{
			Name: "Night CT",
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientID,
				SearchText:  "NIGHT",
			},
		},
	})
	if !selectAutoQueryProfile(state, "Night CT") {
		t.Fatal("selectAutoQueryProfile should select Night CT")
	}

	if err := renameSelectedAutoQueryProfile(state, "Emergency CT"); err != nil {
		t.Fatalf("renameSelectedAutoQueryProfile: %v", err)
	}

	if state.autoQueryProfileName != "Emergency CT" {
		t.Fatalf("selected profile = %q, want Emergency CT", state.autoQueryProfileName)
	}
	if got := strings.Join(autoQueryProfileNames(state), "|"); got != "Default Instance|Emergency CT" {
		t.Fatalf("profile names = %q, want Default Instance|Emergency CT", got)
	}
	if state.autoQuerySearchText != "NIGHT" {
		t.Fatalf("renamed profile search text = %q, want NIGHT", state.autoQuerySearchText)
	}
}

func TestRenameSelectedAutoQueryProfileRejectsDefaultDuplicateAndLocked(t *testing.T) {
	tests := []struct {
		name     string
		selected string
		renameTo string
		profiles []autoquery.Profile
	}{
		{
			name:     "default",
			selected: autoquery.DefaultProfileName,
			renameTo: "Main",
			profiles: []autoquery.Profile{{Name: autoquery.DefaultProfileName}},
		},
		{
			name:     "duplicate",
			selected: "Night CT",
			renameTo: "Default Instance",
			profiles: []autoquery.Profile{{Name: autoquery.DefaultProfileName}, {Name: "Night CT"}},
		},
		{
			name:     "locked",
			selected: "Night CT",
			renameTo: "Emergency CT",
			profiles: []autoquery.Profile{{Name: autoquery.DefaultProfileName}, {Name: "Night CT", Locked: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &uiState{}
			loadAutoQueryProfiles(state, tt.profiles)
			if !selectAutoQueryProfile(state, tt.selected) {
				t.Fatalf("selectAutoQueryProfile(%q) failed", tt.selected)
			}
			if err := renameSelectedAutoQueryProfile(state, tt.renameTo); err == nil {
				t.Fatal("renameSelectedAutoQueryProfile should reject invalid rename")
			}
			if state.autoQueryProfileName != tt.selected {
				t.Fatalf("selected profile changed to %q, want %q", state.autoQueryProfileName, tt.selected)
			}
		})
	}
}

func TestApplyAutoQueryProfileRenamePersistsToStore(t *testing.T) {
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{autoQueryProfileStore: store}
	loadAutoQueryProfiles(state, []autoquery.Profile{{Name: autoquery.DefaultProfileName}, {Name: "Night CT"}})
	if !selectAutoQueryProfile(state, "Night CT") {
		t.Fatal("selectAutoQueryProfile should select Night CT")
	}
	status := widget.NewLabel("")

	if ok := applyAutoQueryProfileRename(nil, status, state, "Emergency CT"); !ok {
		t.Fatal("applyAutoQueryProfileRename should accept valid rename")
	}

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 || profiles[1].Name != "Emergency CT" {
		t.Fatalf("profiles = %#v, want renamed second profile", profiles)
	}
	if status.Text != "Auto Q/R profile renamed" {
		t.Fatalf("status = %q, want rename confirmation", status.Text)
	}
}

func TestAutoQueryTabRenameButtonPersistsProfileName(t *testing.T) {
	fynetest.NewApp()
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{autoQueryProfileStore: store}
	loadAutoQueryProfiles(state, []autoquery.Profile{{Name: autoquery.DefaultProfileName}, {Name: "Night CT"}})
	if !selectAutoQueryProfile(state, "Night CT") {
		t.Fatal("selectAutoQueryProfile should select Night CT")
	}
	if err := saveAutoQueryProfileList(state); err != nil {
		t.Fatalf("saveAutoQueryProfileList: %v", err)
	}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)
	renameButton := findButtonWithIcon(tab, theme.DocumentCreateIcon())
	if renameButton == nil {
		t.Fatal("Auto Q/R tab should expose rename button")
	}
	if renameButton.Disabled() {
		t.Fatal("rename button should be enabled for unlocked non-default profile")
	}
	renameButton.OnTapped()

	if state.autoQueryProfileName != "Instance 2" {
		t.Fatalf("selected profile = %q, want Instance 2 after headless rename", state.autoQueryProfileName)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 || profiles[1].Name != "Instance 2" {
		t.Fatalf("profiles = %#v, want renamed second profile", profiles)
	}
}

func TestAutoQueryTabInitializesSavedProfileCriteria(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		autoQuerySearchField: queryQuickSearchPatientID,
		autoQuerySearchText:  "P123",
		autoQueryDatePreset:  queryDatePresetToday,
		autoQueryModalities:  []string{"CT", "MR"},
		autoQueryRefreshMode: autoQueryRefreshEvery30Min,
	}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	if state.autoQuerySearchField != queryQuickSearchPatientID {
		t.Fatalf("auto Q/R search field state = %q, want %q", state.autoQuerySearchField, queryQuickSearchPatientID)
	}
	if findEntryWithPlaceholder(tab, queryQuickSearchPatientID) == nil {
		t.Fatalf("auto Q/R search placeholder should initialize to %q", queryQuickSearchPatientID)
	}
	if findEntryWithText(tab, "P123") == nil {
		t.Fatal("auto Q/R search text should initialize from saved profile criteria")
	}
	for _, modality := range []string{"CT", "MR"} {
		check := findCheckWithText(tab, modality)
		if check == nil {
			t.Fatalf("auto Q/R should expose modality %q", modality)
		}
		if !check.Checked {
			t.Fatalf("auto Q/R modality %q should initialize checked", modality)
		}
	}
}

func TestAutoQueryTabSwitchesNamedProfiles(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{
		{
			Name: autoquery.DefaultProfileName,
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientName,
				SearchText:  "DEFAULT",
				DatePreset:  queryDatePresetAny,
				RefreshMode: queryRefreshModeDont,
			},
		},
		{
			Name: "Night CT",
			Criteria: autoquery.Criteria{
				SearchField: queryQuickSearchPatientID,
				SearchText:  "NIGHT",
				DatePreset:  queryDatePresetToday,
				Modalities:  []string{"CT"},
				RefreshMode: autoQueryRefreshEvery30Min,
			},
		},
	})
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)
	profileSelect := findSelectWithOption(tab, "Night CT")
	if profileSelect == nil {
		t.Fatal("Auto Q/R profile selector should list named profiles")
	}
	profileSelect.SetSelected("Night CT")

	if state.autoQueryProfileName != "Night CT" {
		t.Fatalf("selected profile = %q, want Night CT", state.autoQueryProfileName)
	}
	if state.autoQuerySearchField != queryQuickSearchPatientID {
		t.Fatalf("Auto Q/R search field state = %q, want %q", state.autoQuerySearchField, queryQuickSearchPatientID)
	}
	if findEntryWithPlaceholder(tab, queryQuickSearchPatientID) == nil {
		t.Fatalf("Auto Q/R search placeholder should switch to %q", queryQuickSearchPatientID)
	}
	if findEntryWithText(tab, "NIGHT") == nil {
		t.Fatal("Auto Q/R search text should switch to named profile criteria")
	}
	check := findCheckWithText(tab, "CT")
	if check == nil || !check.Checked {
		t.Fatal("Auto Q/R modality CT should switch to checked")
	}
}

func TestAutoQueryTabLockButtonPersistsProfileLock(t *testing.T) {
	fynetest.NewApp()
	store := autoquery.NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	state := &uiState{autoQueryProfileStore: store}
	loadAutoQueryProfiles(state, []autoquery.Profile{{Name: autoquery.DefaultProfileName}})
	if err := saveAutoQuerySelectedProfile(state); err != nil {
		t.Fatalf("saveAutoQuerySelectedProfile: %v", err)
	}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)
	lockButton := findButtonWithIcon(tab, theme.ConfirmIcon())
	if lockButton == nil {
		t.Fatal("Auto Q/R tab should expose lock button")
	}
	if lockButton.Disabled() {
		t.Fatal("Auto Q/R lock button should be enabled")
	}
	lockButton.OnTapped()

	if !state.autoQueryProfileLocked {
		t.Fatal("lock button should lock selected profile")
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 || !profiles[0].Locked {
		t.Fatalf("persisted profiles = %#v, want locked default profile", profiles)
	}
	if status.Text != "Auto Q/R profile locked" {
		t.Fatalf("status = %q, want lock confirmation", status.Text)
	}
}

func TestAutoQueryTabDisablesMutatingControlsWhenProfileLocked(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	loadAutoQueryProfiles(state, []autoquery.Profile{{
		Name:   autoquery.DefaultProfileName,
		Locked: true,
	}})
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)

	settingsButton := findButtonWithText(tab, "Settings")
	if settingsButton == nil {
		t.Fatal("Auto Q/R tab should expose Settings button")
	}
	if !settingsButton.Disabled() {
		t.Fatal("Settings button should be disabled for locked profile")
	}
	autoRetrieve := findCheckWithText(tab, queryAutoRetrieveLabel)
	if autoRetrieve == nil {
		t.Fatalf("Auto Q/R tab should expose %q checkbox", queryAutoRetrieveLabel)
	}
	if !autoRetrieve.Disabled() {
		t.Fatal("Auto-Retrieve checkbox should be disabled for locked profile")
	}
	queryButton := findButtonWithText(tab, queryActionLabelQuery)
	if queryButton == nil || queryButton.Disabled() {
		t.Fatal("locked profile should still allow Query execution")
	}
}

func TestAutoQuerySettingsFormItemsExposeSafetyControls(t *testing.T) {
	items := autoQuerySettingsFormItems(&uiState{})
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Text)
	}
	joined := strings.Join(labels, "|")
	for _, want := range []string{"Retrieve Level", "Max Matches", "Duplicate Policy", "Require Confirmation"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("settings form labels = %q, want %q", joined, want)
		}
	}
}

func TestAutoQueryTabEnablesManualProfileQuery(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	tab := newAutoQueryTab(nil, status, archiveTables{}, nil, state)
	queryButton := findButtonWithText(tab, queryActionLabelQuery)

	if queryButton == nil {
		t.Fatalf("auto Q/R tab should expose %q button", queryActionLabelQuery)
	}
	if queryButton.Disabled() {
		t.Fatalf("auto Q/R %q button should be enabled for manual profile runs", queryActionLabelQuery)
	}
	patientButton := findButtonWithText(tab, queryActionLabelPatient)
	if patientButton == nil {
		t.Fatalf("auto Q/R tab should expose %q button", queryActionLabelPatient)
	}
	if patientButton.Disabled() {
		t.Fatalf("auto Q/R %q button should be enabled for Patient Root profile runs", queryActionLabelPatient)
	}
}

func TestAutoQueryStudyCriteriaUsesSearchDateAndModality(t *testing.T) {
	checks := newQueryModalityChecks()
	checks["CT"].SetChecked(true)
	now := time.Date(2026, 6, 5, 14, 0, 0, 0, time.Local)

	criteria, ok := autoQueryStudyCriteria(queryQuickSearchPatientName, " DOE^JANE ", queryDatePresetToday, checks, now)

	if !ok {
		t.Fatal("auto Q/R study criteria should support patient-name search")
	}
	if criteria.PatientName != "DOE^JANE" {
		t.Fatalf("PatientName = %q, want DOE^JANE", criteria.PatientName)
	}
	if criteria.StudyDateFrom != "20260605" || criteria.StudyDateTo != "20260605" {
		t.Fatalf("study date range = %q/%q, want 20260605/20260605", criteria.StudyDateFrom, criteria.StudyDateTo)
	}
	if criteria.Modality != "CT" {
		t.Fatalf("Modality = %q, want CT", criteria.Modality)
	}
}

func TestAutoQueryPatientCriteriaUsesSupportedSearchFields(t *testing.T) {
	criteria, ok := autoQueryPatientCriteria(queryQuickSearchPatientID, " P123 ")

	if !ok {
		t.Fatal("auto Q/R Patient criteria should support Patient ID search")
	}
	if criteria.PatientID != "P123" {
		t.Fatalf("PatientID = %q, want P123", criteria.PatientID)
	}

	_, ok = autoQueryPatientCriteria(queryQuickSearchAccession, " A100 ")
	if ok {
		t.Fatal("auto Q/R Patient criteria should reject unsupported Accession search")
	}
}

func TestRefreshAutoQueryResultSummaryUpdatesLabel(t *testing.T) {
	state := &uiState{
		autoQueryResultSummaryLabel: widget.NewLabel("stale"),
		queries: []query.Match{
			{PatientName: "DOE^JANE"},
			{PatientName: "SMITH^JOHN"},
		},
		nodes: []nodes.Node{{
			Name:    "RADIANT",
			Host:    "192.168.100.26",
			Port:    11112,
			AETitle: "RADIANT",
		}},
	}

	refreshAutoQueryResultSummary(state)

	if state.autoQueryResultSummaryLabel.Text != "2 studies found\nRADIANT / 192.168.100.26:11112" {
		t.Fatalf("auto Q/R summary = %q, want count plus selected source", state.autoQueryResultSummaryLabel.Text)
	}
}

func TestAutoQueryResultSummaryTextShowsCheckedSources(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "10.5.4.13", Port: 4007},
		},
	}

	text := autoQueryResultSummaryText(state)

	if text != "0 studies found\n2 sources: RADIANT, HOROSMINI" {
		t.Fatalf("auto Q/R summary = %q, want count plus checked sources", text)
	}
}

func TestAutoQueryResultSummaryTextUsesResultSources(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{SourceNodeName: "RADIANT", SourceHost: "192.168.100.26", SourcePort: 11112},
			{SourceNodeName: "RADIANT", SourceHost: "192.168.100.26", SourcePort: 11112},
		},
		nodes: []nodes.Node{{
			Name: "HOROSMINI",
			Host: "10.5.4.13",
			Port: 4007,
		}},
	}

	text := autoQueryResultSummaryText(state)

	if text != "2 studies found\nRADIANT" {
		t.Fatalf("auto Q/R summary = %q, want result source before fallback source", text)
	}
}

func TestAutoQueryRefreshIntervalRecognizesThirtyMinuteCadence(t *testing.T) {
	if got := autoQueryRefreshInterval(autoQueryRefreshEvery30Min); got != 30*time.Minute {
		t.Fatalf("Refresh every 30 min interval = %s, want 30m", got)
	}
	if got := autoQueryRefreshInterval(queryRefreshModeDont); got != 0 {
		t.Fatalf("Don't refresh interval = %s, want 0", got)
	}
}

func TestAutoQueryCountdownTextDescribesRefreshState(t *testing.T) {
	now := time.Date(2026, 6, 5, 15, 0, 0, 0, time.Local)

	cases := []struct {
		name     string
		mode     string
		next     time.Time
		hasQuery bool
		want     string
	}{
		{name: "dormant", mode: queryRefreshModeDont, want: autoQueryCountdownDormant},
		{name: "waiting", mode: autoQueryRefreshEvery30Min, want: "Next: waiting for Query"},
		{name: "remaining", mode: autoQueryRefreshEvery30Min, next: now.Add(90 * time.Second), hasQuery: true, want: "Next: 1m 30s"},
		{name: "due", mode: autoQueryRefreshEvery30Min, next: now.Add(-time.Second), hasQuery: true, want: "Next: now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoQueryCountdownText(tc.mode, tc.next, tc.hasQuery, now); got != tc.want {
				t.Fatalf("countdown text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScheduleAutoQueryRefreshSetsNextRunAfterRememberedQuery(t *testing.T) {
	state := &uiState{autoQueryCountdownLabel: widget.NewLabel("")}
	rememberAutoQueryStudy(state, query.Criteria{PatientName: "DOE^JANE"})
	now := time.Date(2026, 6, 5, 15, 0, 0, 0, time.Local)

	scheduleAutoQueryRefreshAt(nil, widget.NewLabel(""), archiveTables{}, nil, state, autoQueryRefreshEvery30Min, now)
	defer stopAutoQueryRefresh(state)

	want := now.Add(30 * time.Minute)
	if !state.autoQueryNextRefresh.Equal(want) {
		t.Fatalf("next refresh = %s, want %s", state.autoQueryNextRefresh, want)
	}
	if state.autoQueryCountdownLabel.Text != "Next: 30m 00s" {
		t.Fatalf("countdown label = %q, want Next: 30m 00s", state.autoQueryCountdownLabel.Text)
	}
}

func TestScheduleAutoQueryRefreshWaitsForProfileRun(t *testing.T) {
	state := &uiState{autoQueryCountdownLabel: widget.NewLabel("")}
	now := time.Date(2026, 6, 5, 15, 0, 0, 0, time.Local)

	scheduleAutoQueryRefreshAt(nil, widget.NewLabel(""), archiveTables{}, nil, state, autoQueryRefreshEvery30Min, now)

	if !state.autoQueryNextRefresh.IsZero() {
		t.Fatalf("next refresh = %s, want zero before a profile run", state.autoQueryNextRefresh)
	}
	if state.autoQueryCountdownLabel.Text != "Next: waiting for Query" {
		t.Fatalf("countdown label = %q, want waiting message", state.autoQueryCountdownLabel.Text)
	}
}

func TestRememberAutoQueryStudyMakesAutoRefreshAvailable(t *testing.T) {
	state := &uiState{}
	if autoQueryRefreshAvailable(state) {
		t.Fatal("auto Q/R refresh should not be available before an Auto Q/R query is remembered")
	}

	criteria := query.Criteria{PatientName: "DOE^JANE", StudyDateFrom: "20260605", Modality: "CT"}
	rememberAutoQueryStudy(state, criteria)

	if !autoQueryRefreshAvailable(state) {
		t.Fatal("auto Q/R refresh should be available after an Auto Q/R query is remembered")
	}
	if state.autoQueryLast.kind != queryRunStudy {
		t.Fatalf("auto Q/R last kind = %q, want %q", state.autoQueryLast.kind, queryRunStudy)
	}
	if state.autoQueryLast.study != criteria {
		t.Fatalf("auto Q/R last criteria = %#v, want %#v", state.autoQueryLast.study, criteria)
	}
}

func TestRememberAutoQueryPatientMakesAutoRefreshAvailable(t *testing.T) {
	state := &uiState{}
	criteria := query.PatientCriteria{PatientName: "DOE^JANE", PatientID: "P123"}

	rememberAutoQueryPatient(state, criteria)

	if !autoQueryRefreshAvailable(state) {
		t.Fatal("auto Q/R refresh should be available after an Auto Q/R Patient query is remembered")
	}
	if state.autoQueryLast.kind != queryRunPatient {
		t.Fatalf("auto Q/R last kind = %q, want %q", state.autoQueryLast.kind, queryRunPatient)
	}
	if state.autoQueryLast.patient != criteria {
		t.Fatalf("auto Q/R patient criteria = %#v, want %#v", state.autoQueryLast.patient, criteria)
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

func TestQueryRefreshIntervalRecognizesThirtyMinuteCadence(t *testing.T) {
	if got := queryRefreshInterval(autoQueryRefreshEvery30Min); got != 30*time.Minute {
		t.Fatalf("Refresh every 30 min interval = %s, want 30m", got)
	}
	if got := queryRefreshInterval(queryRefreshModeDont); got != 0 {
		t.Fatalf("Don't refresh interval = %s, want 0", got)
	}
}

func TestQueryCountdownTextDescribesRefreshState(t *testing.T) {
	now := time.Date(2026, 6, 5, 15, 0, 0, 0, time.Local)

	cases := []struct {
		name     string
		mode     string
		next     time.Time
		hasQuery bool
		want     string
	}{
		{name: "dormant", mode: queryRefreshModeDont, want: autoQueryCountdownDormant},
		{name: "waiting", mode: autoQueryRefreshEvery30Min, want: "Next: waiting for Query"},
		{name: "remaining", mode: autoQueryRefreshEvery30Min, next: now.Add(90 * time.Second), hasQuery: true, want: "Next: 1m 30s"},
		{name: "due", mode: autoQueryRefreshEvery30Min, next: now.Add(-time.Second), hasQuery: true, want: "Next: now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := queryCountdownText(tc.mode, tc.next, tc.hasQuery, now); got != tc.want {
				t.Fatalf("countdown text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScheduleQueryRefreshSetsNextRunAfterRememberedQuery(t *testing.T) {
	state := &uiState{queryCountdownLabel: widget.NewLabel("")}
	rememberLastStudyQuery(state, query.Criteria{PatientName: "DOE^JANE"})
	now := time.Date(2026, 6, 5, 15, 0, 0, 0, time.Local)

	scheduleQueryRefreshAt(nil, widget.NewLabel(""), nil, state, autoQueryRefreshEvery30Min, now)
	defer stopQueryRefresh(state)

	want := now.Add(30 * time.Minute)
	if !state.queryNextRefresh.Equal(want) {
		t.Fatalf("next refresh = %s, want %s", state.queryNextRefresh, want)
	}
	if state.queryCountdownLabel.Text != "Next: 30m 00s" {
		t.Fatalf("countdown label = %q, want Next: 30m 00s", state.queryCountdownLabel.Text)
	}
}

func TestScheduleQueryRefreshWaitsForManualQueryRun(t *testing.T) {
	state := &uiState{queryCountdownLabel: widget.NewLabel("")}
	now := time.Date(2026, 6, 5, 15, 0, 0, 0, time.Local)

	scheduleQueryRefreshAt(nil, widget.NewLabel(""), nil, state, autoQueryRefreshEvery30Min, now)

	if !state.queryNextRefresh.IsZero() {
		t.Fatalf("next refresh = %s, want zero before a query run", state.queryNextRefresh)
	}
	if state.queryCountdownLabel.Text != "Next: waiting for Query" {
		t.Fatalf("countdown label = %q, want waiting message", state.queryCountdownLabel.Text)
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

func TestQuerySeriesCriteriaWithQuickSearchSupportsAvailableFields(t *testing.T) {
	base := query.SeriesCriteria{
		PatientName:       "advanced name",
		PatientID:         "P123",
		StudyDateFrom:     "20260601",
		StudyDateTo:       "20260630",
		StudyInstanceUID:  "1.2.study",
		SeriesInstanceUID: "1.2.series",
		Modality:          "CT",
		SeriesDescription: "advanced description",
		MaxResults:        25,
	}

	tests := []struct {
		name     string
		field    string
		wantName string
		wantID   string
		wantDesc string
		wantOK   bool
	}{
		{name: "patient name", field: queryQuickSearchPatientName, wantName: "value", wantID: "P123", wantDesc: "advanced description", wantOK: true},
		{name: "patient id", field: queryQuickSearchPatientID, wantName: "advanced name", wantID: "value", wantDesc: "advanced description", wantOK: true},
		{name: "description", field: queryQuickSearchDescription, wantName: "advanced name", wantID: "P123", wantDesc: "value", wantOK: true},
		{name: "unsupported accession", field: queryQuickSearchAccession, wantOK: false},
		{name: "empty unsupported value preserves base", field: queryQuickSearchAccession, wantName: "advanced name", wantID: "P123", wantDesc: "advanced description", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := " value "
			if strings.HasPrefix(tt.name, "empty") {
				value = " "
			}
			criteria, ok := querySeriesCriteriaWithQuickSearch(base, tt.field, value)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if criteria.PatientName != tt.wantName ||
				criteria.PatientID != tt.wantID ||
				criteria.SeriesDescription != tt.wantDesc ||
				criteria.StudyDateFrom != "20260601" ||
				criteria.StudyDateTo != "20260630" ||
				criteria.StudyInstanceUID != "1.2.study" ||
				criteria.SeriesInstanceUID != "1.2.series" ||
				criteria.Modality != "CT" ||
				criteria.MaxResults != 25 {
				t.Fatalf("series criteria = %#v", criteria)
			}
		})
	}
}

func TestQueryImageCriteriaWithQuickSearchSupportsAvailableFields(t *testing.T) {
	base := query.ImageCriteria{
		PatientName:       "advanced name",
		PatientID:         "P123",
		StudyDateFrom:     "20260601",
		StudyDateTo:       "20260630",
		StudyInstanceUID:  "1.2.study",
		SeriesInstanceUID: "1.2.series",
		SOPInstanceUID:    "1.2.image",
		SOPClassUID:       "1.2.class",
		Modality:          "CT",
		InstanceNumber:    "7",
		MaxResults:        25,
	}

	tests := []struct {
		name     string
		field    string
		wantName string
		wantID   string
		wantOK   bool
	}{
		{name: "patient name", field: queryQuickSearchPatientName, wantName: "value", wantID: "P123", wantOK: true},
		{name: "patient id", field: queryQuickSearchPatientID, wantName: "advanced name", wantID: "value", wantOK: true},
		{name: "unsupported description", field: queryQuickSearchDescription, wantOK: false},
		{name: "empty unsupported value preserves base", field: queryQuickSearchDescription, wantName: "advanced name", wantID: "P123", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := " value "
			if strings.HasPrefix(tt.name, "empty") {
				value = " "
			}
			criteria, ok := queryImageCriteriaWithQuickSearch(base, tt.field, value)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if criteria.PatientName != tt.wantName ||
				criteria.PatientID != tt.wantID ||
				criteria.StudyDateFrom != "20260601" ||
				criteria.StudyDateTo != "20260630" ||
				criteria.StudyInstanceUID != "1.2.study" ||
				criteria.SeriesInstanceUID != "1.2.series" ||
				criteria.SOPInstanceUID != "1.2.image" ||
				criteria.SOPClassUID != "1.2.class" ||
				criteria.Modality != "CT" ||
				criteria.InstanceNumber != "7" ||
				criteria.MaxResults != 25 {
				t.Fatalf("image criteria = %#v", criteria)
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

	if len(rows) != 1 || !strings.HasPrefix(rows[0], "  [x] ✓ ") || !strings.HasSuffix(rows[0], " RADIANT 192.168.100.26:11112") {
		t.Fatalf("verified source row = %#v", rows)
	}
}

func TestQuerySourceRowsShowVerifiedNodeStatusTimestamp(t *testing.T) {
	node := nodes.Node{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}
	state := &uiState{
		nodes:                 []nodes.Node{node},
		selectedNodeRow:       -1,
		nodeVerifyStatuses:    map[string]nodeVerifyStatus{nodeVerifyKey(node): nodeVerifyOK},
		nodeVerifyStatusTimes: map[string]time.Time{nodeVerifyKey(node): time.Date(2026, 6, 5, 9, 42, 0, 0, time.Local)},
	}

	rows := querySourceRows(state)

	if len(rows) != 1 || rows[0] != "  [x] ✓ 09:42 RADIANT 192.168.100.26:11112" {
		t.Fatalf("verified source row with timestamp = %#v", rows)
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
	if !strings.HasPrefix(rows[0], "  [x] Q! ") || !strings.HasSuffix(rows[0], " offline 10.0.0.1:104") {
		t.Fatalf("failed source row = %q", rows[0])
	}
	if !strings.HasPrefix(rows[1], "  [x] Q✓ ") || !strings.HasSuffix(rows[1], " online 10.0.0.2:105") {
		t.Fatalf("successful source row = %q", rows[1])
	}
}

func TestQuerySourceRowsShowQuerySourceStatusTimestamp(t *testing.T) {
	node := nodes.Node{ID: "node-1", Name: "online", Host: "10.0.0.2", Port: 105}
	state := &uiState{
		nodes:                  []nodes.Node{node},
		selectedNodeRow:        -1,
		querySourceStatuses:    map[string]querySourceStatus{nodeVerifyKey(node): querySourceOK},
		querySourceStatusTimes: map[string]time.Time{nodeVerifyKey(node): time.Date(2026, 6, 5, 9, 43, 0, 0, time.Local)},
	}

	rows := querySourceRows(state)

	if len(rows) != 1 || rows[0] != "  [x] Q✓ 09:43 online 10.0.0.2:105" {
		t.Fatalf("query source row with timestamp = %#v", rows)
	}
}

func TestSourceStatusHistoryLinesShowRecentQueryAndVerifyEvents(t *testing.T) {
	node := nodes.Node{ID: "node-1", Name: "RADIANT", Host: "10.0.0.2", Port: 105}
	state := &uiState{}

	appendSourceStatusHistoryAt(state, node, sourceStatusHistoryKindVerify, sourceStatusHistoryOK, time.Date(2026, 6, 5, 9, 42, 0, 0, time.Local))
	appendSourceStatusHistoryAt(state, node, sourceStatusHistoryKindQuery, sourceStatusHistoryFail, time.Date(2026, 6, 5, 9, 43, 0, 0, time.Local))

	lines := sourceStatusHistoryLines(state)
	want := []string{
		"09:43 RADIANT Query failed",
		"09:42 RADIANT C-ECHO OK",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("source history lines = %#v, want %#v", lines, want)
	}
}

func TestRecordQuerySourceStatusRefreshesHistoryLabel(t *testing.T) {
	node := nodes.Node{ID: "node-1", Name: "RADIANT", Host: "10.0.0.2", Port: 105}
	state := &uiState{querySourceHistoryLabel: widget.NewLabel("")}

	recordQuerySourceStatus(state, node, querySourceOK)

	if !strings.Contains(state.querySourceHistoryLabel.Text, "RADIANT Query OK") {
		t.Fatalf("source history label = %q, want query OK event", state.querySourceHistoryLabel.Text)
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

	if label != "▶ RADIANT 192.168.100.26:11112" {
		t.Fatalf("query source check label = %q", label)
	}
	for _, marker := range []string{"[x]", "[ ]", "✓", "!", "Q"} {
		if strings.Contains(label, marker) {
			t.Fatalf("native checkbox label should not include text marker %q: %q", marker, label)
		}
	}
}

func TestQuerySourceCheckLabelIncludesAETitle(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{{
			Name:    "RADIANT",
			AETitle: "RADIANT_AE",
			Host:    "192.168.100.26",
			Port:    11112,
		}},
		selectedNodeRow: 0,
	}

	if got := querySourceCheckLabel(state, 0); got != "▶ RADIANT 192.168.100.26:11112 RADIANT_AE" {
		t.Fatalf("query source check label = %q", got)
	}
}

func TestQuerySourceCheckLabelShowsDisabledReason(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "OFFLINE", Host: "10.0.0.1", Port: 104, Disabled: true},
			{Name: "NOQUERY", Host: "10.0.0.2", Port: 105, QueryDisabled: true},
		},
		selectedNodeRow: -1,
	}

	if got := querySourceCheckLabel(state, 0); got != "  OFFLINE 10.0.0.1:104 (disabled)" {
		t.Fatalf("disabled label = %q", got)
	}
	if got := querySourceCheckLabel(state, 1); got != "  NOQUERY 10.0.0.2:105 (query off)" {
		t.Fatalf("query-disabled label = %q", got)
	}
}

func TestQuerySourceListCellUsesNativeStatusDots(t *testing.T) {
	node := nodes.Node{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}
	state := &uiState{nodes: []nodes.Node{node}, selectedNodeRow: 0}
	recordNodeVerifyStatus(state, node, nodeVerifyOK)
	recordQuerySourceStatus(state, node, querySourceFail)
	cell := newQuerySourceListCell()

	configureQuerySourceCell(cell, state, 0, nil)

	if cell.check.Text != "" {
		t.Fatalf("check text = %q", cell.check.Text)
	}
	if cell.nameLabel.Text != "▶ RADIANT" {
		t.Fatalf("name label = %q", cell.nameLabel.Text)
	}
	if cell.addressLabel.Text != "192.168.100.26:11112" {
		t.Fatalf("address label = %q", cell.addressLabel.Text)
	}
	if got := color.NRGBAModel.Convert(cell.verifyDot.FillColor).(color.NRGBA); got != sourceStatusOKColor {
		t.Fatalf("verify dot color = %#v, want %#v", got, sourceStatusOKColor)
	}
	if got := color.NRGBAModel.Convert(cell.queryDot.FillColor).(color.NRGBA); got != sourceStatusFailColor {
		t.Fatalf("query dot color = %#v, want %#v", got, sourceStatusFailColor)
	}
}

func TestQuerySourceListCellUsesSeparateNodeColumns(t *testing.T) {
	node := nodes.Node{Name: "RADIANT", AETitle: "RADIANT_AE", Host: "192.168.100.26", Port: 11112}
	state := &uiState{nodes: []nodes.Node{node}, selectedNodeRow: 0}
	cell := newQuerySourceListCell()

	configureQuerySourceCell(cell, state, 0, nil)

	if cell.check.Text != "" {
		t.Fatalf("source checkbox text = %q, want empty text with separate labels", cell.check.Text)
	}
	if cell.nameLabel.Text != "▶ RADIANT" {
		t.Fatalf("name label = %q", cell.nameLabel.Text)
	}
	if cell.addressLabel.Text != "192.168.100.26:11112" {
		t.Fatalf("address label = %q", cell.addressLabel.Text)
	}
	if cell.aeTitleLabel.Text != "RADIANT_AE" {
		t.Fatalf("AE title label = %q", cell.aeTitleLabel.Text)
	}
}

func TestQuerySourceListCellUsesWorkbenchRowBackground(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "HOROS", Host: "127.0.0.1", Port: 4007},
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
		},
		selectedNodeRow: 1,
	}
	unselected := newQuerySourceListCell()
	selected := newQuerySourceListCell()

	configureQuerySourceCell(unselected, state, 0, nil)
	configureQuerySourceCell(selected, state, 1, nil)

	if got := color.NRGBAModel.Convert(unselected.background.FillColor).(color.NRGBA); got != archiveEvenRowColor {
		t.Fatalf("unselected source row background = %#v, want %#v", got, archiveEvenRowColor)
	}
	if got := color.NRGBAModel.Convert(selected.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected source row background = %#v, want %#v", got, archiveSelectedRowColor)
	}
}

func TestQuerySourceListCellUsesInternalColumnDividers(t *testing.T) {
	cell := newQuerySourceListCell()

	if got := countCanvasRectanglesWithColor(cell.Container, tableColumnDividerColor); got < 4 {
		t.Fatalf("source row divider rectangles = %d, want outer row/column plus internal Name/Address/AETitle dividers", got)
	}
}

func TestQuerySourceListCellShowsEmptySourcesPlaceholder(t *testing.T) {
	state := &uiState{nodes: nil, selectedNodeRow: -1}
	cell := newQuerySourceListCell()

	configureQuerySourceCell(cell, state, 0, nil)

	if cell.nameLabel.Text != querySourceEmptyLabel {
		t.Fatalf("empty source name label = %q, want %q", cell.nameLabel.Text, querySourceEmptyLabel)
	}
	if cell.addressLabel.Text != "" || cell.aeTitleLabel.Text != "" {
		t.Fatalf("empty source secondary labels = %q/%q, want empty", cell.addressLabel.Text, cell.aeTitleLabel.Text)
	}
	if !cell.check.Disabled() {
		t.Fatal("empty source placeholder checkbox should be disabled")
	}
}

func TestAutoQuerySourceListCellShowsEmptySourcesPlaceholder(t *testing.T) {
	state := &uiState{nodes: nil, selectedAutoQuerySourceRow: -1}
	cell := newQuerySourceListCell()

	configureAutoQuerySourceCell(cell, state, 0, nil)

	if cell.nameLabel.Text != querySourceEmptyLabel {
		t.Fatalf("empty auto source name label = %q, want %q", cell.nameLabel.Text, querySourceEmptyLabel)
	}
	if cell.addressLabel.Text != "" || cell.aeTitleLabel.Text != "" {
		t.Fatalf("empty auto source secondary labels = %q/%q, want empty", cell.addressLabel.Text, cell.aeTitleLabel.Text)
	}
	if !cell.check.Disabled() {
		t.Fatal("empty auto source placeholder checkbox should be disabled")
	}
}

func TestQuerySourceListUsesCompactRowHeights(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "HOROS", Host: "127.0.0.1", Port: 4007},
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
		},
	}

	list := newQuerySourceList(state)

	for id := range state.nodes {
		height, ok := listItemHeight(list, id)
		if !ok {
			t.Fatalf("source row %d should have an explicit compact height", id)
		}
		if height > compactTableRowHeight+6 {
			t.Fatalf("source row %d height = %.0f, want compact workstation height", id, height)
		}
	}
}

func TestAutoQuerySourceListUsesCompactRowHeights(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "HOROS", Host: "127.0.0.1", Port: 4007},
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
		},
	}

	list := newAutoQuerySourceList(nil, nil, state)

	for id := range autoQuerySourceRows(state) {
		height, ok := listItemHeight(list, id)
		if !ok {
			t.Fatalf("auto source row %d should have an explicit compact height", id)
		}
		if height > compactTableRowHeight+6 {
			t.Fatalf("auto source row %d height = %.0f, want compact workstation height", id, height)
		}
	}
}

func TestQuerySourceListCellStylesDisabledSources(t *testing.T) {
	node := nodes.Node{Name: "OFFLINE", Host: "10.0.0.1", Port: 104, Disabled: true}
	state := &uiState{nodes: []nodes.Node{node}, selectedNodeRow: 0}
	cell := newQuerySourceListCell()

	configureQuerySourceCell(cell, state, 0, nil)

	if cell.check.Disabled() {
		t.Fatal("disabled source checkbox should remain enabled so it can re-enable the source")
	}
	if cell.nameLabel.Text != "▶ OFFLINE (disabled)" {
		t.Fatalf("name label = %q", cell.nameLabel.Text)
	}
	for _, label := range []*widget.Label{cell.nameLabel, cell.addressLabel, cell.aeTitleLabel} {
		if !label.TextStyle.Italic {
			t.Fatalf("disabled source label %q should be italic", label.Text)
		}
	}
}

func TestQuerySourceListCellUsesIdleDotsForUnknownStatus(t *testing.T) {
	node := nodes.Node{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}
	state := &uiState{nodes: []nodes.Node{node}, selectedNodeRow: -1}
	cell := newQuerySourceListCell()

	configureQuerySourceCell(cell, state, 0, nil)

	if got := color.NRGBAModel.Convert(cell.verifyDot.FillColor).(color.NRGBA); got != sourceStatusIdleColor {
		t.Fatalf("verify idle dot color = %#v, want %#v", got, sourceStatusIdleColor)
	}
	if got := color.NRGBAModel.Convert(cell.queryDot.FillColor).(color.NRGBA); got != sourceStatusIdleColor {
		t.Fatalf("query idle dot color = %#v, want %#v", got, sourceStatusIdleColor)
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

func TestSetAllNodesEnabledPersistsWithoutChangingQueryOrSendFlags(t *testing.T) {
	store := nodes.NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	state := &uiState{
		nodeStore: store,
		nodes: []nodes.Node{
			{ID: "node-1", Name: "first", Disabled: false, QueryDisabled: true, SendDisabled: false},
			{ID: "node-2", Name: "second", Disabled: true, QueryDisabled: false, SendDisabled: true},
		},
	}
	if err := store.Save(state.nodes); err != nil {
		t.Fatal(err)
	}

	changed, err := setAllNodesEnabled(state, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("disable all did not report change")
	}
	for _, node := range state.nodes {
		if !node.Disabled {
			t.Fatalf("node should be disabled after None: %+v", node)
		}
	}
	if !state.nodes[0].QueryDisabled || state.nodes[0].SendDisabled ||
		state.nodes[1].QueryDisabled || !state.nodes[1].SendDisabled {
		t.Fatalf("query/send flags changed after None: %+v", state.nodes)
	}
	persisted, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if !persisted[0].Disabled || !persisted[1].Disabled {
		t.Fatalf("persisted nodes not disabled: %+v", persisted)
	}

	changed, err = setAllNodesEnabled(state, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("enable all did not report change")
	}
	for _, node := range state.nodes {
		if node.Disabled {
			t.Fatalf("node should be enabled after All: %+v", node)
		}
	}
	if !state.nodes[0].QueryDisabled || state.nodes[0].SendDisabled ||
		state.nodes[1].QueryDisabled || !state.nodes[1].SendDisabled {
		t.Fatalf("query/send flags changed after All: %+v", state.nodes)
	}
}

func TestNetworkNodeActionLabelsMatchNodeManagerFooter(t *testing.T) {
	labels := networkNodeActionLabels()
	want := []string{"All", "None", "Save...", "Load...", "Verify", "Add new node", "Edit", "Delete"}

	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("network action labels = %q, want %q", strings.Join(labels, "|"), strings.Join(want, "|"))
	}
}

func TestNetworkSecondaryNodeActionButtonUsesIconOnlyLowImportance(t *testing.T) {
	button := networkSecondaryNodeActionButton(theme.DocumentCreateIcon(), nil)

	if button.Text != "" {
		t.Fatalf("secondary node action text = %q, want icon-only", button.Text)
	}
	if button.Icon == nil {
		t.Fatal("secondary node action icon is nil")
	}
	if button.Importance != widget.LowImportance {
		t.Fatalf("secondary node action importance = %v, want LowImportance", button.Importance)
	}
}

func TestExportNodesToPathWritesCurrentNodesAsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.json")
	state := &uiState{
		nodes: []nodes.Node{
			{ID: "node-1", Name: "RADIANT", AETitle: "RADIANT", Host: "192.168.100.26", Port: 11112, RetrieveMethod: nodes.RetrieveMethodGet},
			{ID: "node-2", Name: "ORTHANC", AETitle: "ORTHANC", Host: "127.0.0.1", Port: 4242, Disabled: true},
		},
	}

	if err := exportNodesToPath(state, path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("exported JSON should end with newline: %q", data)
	}
	var exported []nodes.Node
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if strings.Join(nodeNames(exported), "|") != "RADIANT|ORTHANC" {
		t.Fatalf("exported nodes = %+v", exported)
	}
	if exported[0].RetrieveMethod != nodes.RetrieveMethodGet || !exported[1].Disabled {
		t.Fatalf("exported node fields = %+v", exported)
	}
}

func TestExportNodesToPathRejectsNilState(t *testing.T) {
	err := exportNodesToPath(nil, filepath.Join(t.TempDir(), "nodes.json"))
	if err == nil || !strings.Contains(err.Error(), "state is unavailable") {
		t.Fatalf("exportNodesToPath error = %v", err)
	}
}

func TestImportNodesFromPathReplacesStateAndPersists(t *testing.T) {
	tempDir := t.TempDir()
	store := nodes.NewStore(filepath.Join(tempDir, "stored-nodes.json"))
	importPath := filepath.Join(tempDir, "import-nodes.json")
	imported := []nodes.Node{
		{ID: "node-3", Name: "REMOTE", AETitle: "REMOTE", Host: "10.0.0.3", Port: 104, SendDisabled: true},
		{ID: "node-4", Name: "BACKUP", AETitle: "BACKUP", Host: "10.0.0.4", Port: 11112, QueryDisabled: true},
	}
	data, err := json.MarshalIndent(imported, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(importPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &uiState{
		nodeStore:       store,
		selectedNodeRow: 1,
		nodeSortActive:  true,
		nodeSortColumn:  nodeTableColumnName,
		nodeTableRows:   []int{1, 0},
		nodes:           []nodes.Node{{ID: "old", Name: "OLD", AETitle: "OLD", Host: "127.0.0.1", Port: 104}},
	}

	if err := importNodesFromPath(state, importPath); err != nil {
		t.Fatal(err)
	}

	if strings.Join(nodeNames(state.nodes), "|") != "REMOTE|BACKUP" {
		t.Fatalf("state nodes = %+v", state.nodes)
	}
	if state.selectedNodeRow != -1 {
		t.Fatalf("selectedNodeRow = %d, want reset", state.selectedNodeRow)
	}
	if strings.Join(nodeTableNames(state), "|") != "BACKUP|REMOTE" {
		t.Fatalf("sorted node table rows = %+v", nodeTableNames(state))
	}
	persisted, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(nodeNames(persisted), "|") != "REMOTE|BACKUP" {
		t.Fatalf("persisted nodes = %+v", persisted)
	}
	if !persisted[0].SendDisabled || !persisted[1].QueryDisabled {
		t.Fatalf("persisted node flags = %+v", persisted)
	}
}

func TestNetworkDeleteShortcutHintMatchesReference(t *testing.T) {
	if got := networkDeleteShortcutHint(); got != "Press Delete key to remove a node" {
		t.Fatalf("delete shortcut hint = %q", got)
	}
}

func TestNetworkDeleteShortcutAppliesOnlyToNetworkTab(t *testing.T) {
	if !networkDeleteShortcutApplies("Network") {
		t.Fatal("Delete shortcut should apply on Network tab")
	}
	for _, tab := range []string{"Archive", "Query", "Tasks", "Inspector", ""} {
		if networkDeleteShortcutApplies(tab) {
			t.Fatalf("Delete shortcut should not apply on %q tab", tab)
		}
	}
}

func TestNetworkVerifyShortcutAppliesOnlyToNetworkTab(t *testing.T) {
	if !networkVerifyShortcutApplies("Network") {
		t.Fatal("Verify shortcut should apply on Network tab")
	}
	for _, tab := range []string{"Archive", "Query", "Tasks", "Inspector", ""} {
		if networkVerifyShortcutApplies(tab) {
			t.Fatalf("Verify shortcut should not apply on %q tab", tab)
		}
	}
}

func TestNetworkVerifyShortcutUsesDefaultModifierK(t *testing.T) {
	shortcut := networkVerifyShortcut()

	if shortcut.KeyName != fyne.KeyK {
		t.Fatalf("verify shortcut key = %q, want K", shortcut.KeyName)
	}
	if shortcut.Modifier != fyne.KeyModifierShortcutDefault {
		t.Fatalf("verify shortcut modifier = %v, want platform default", shortcut.Modifier)
	}
}

func TestQueryShortcutsApplyOnlyToQueryTab(t *testing.T) {
	if !queryShortcutApplies("Query") {
		t.Fatal("Query shortcuts should apply on Query tab")
	}
	for _, tab := range []string{"Archive", "Network", "Auto Q/R", "Tasks", "Inspector", ""} {
		if queryShortcutApplies(tab) {
			t.Fatalf("Query shortcuts should not apply on %q tab", tab)
		}
	}
}

func TestQueryRunShortcutUsesDefaultModifierReturn(t *testing.T) {
	shortcut := queryRunShortcut()

	if shortcut.KeyName != fyne.KeyReturn {
		t.Fatalf("query shortcut key = %q, want Return", shortcut.KeyName)
	}
	if shortcut.Modifier != fyne.KeyModifierShortcutDefault {
		t.Fatalf("query shortcut modifier = %v, want platform default", shortcut.Modifier)
	}
}

func TestQueryRetrieveShortcutUsesDefaultModifierR(t *testing.T) {
	shortcut := queryRetrieveShortcut()

	if shortcut.KeyName != fyne.KeyR {
		t.Fatalf("retrieve shortcut key = %q, want R", shortcut.KeyName)
	}
	if shortcut.Modifier != fyne.KeyModifierShortcutDefault {
		t.Fatalf("retrieve shortcut modifier = %v, want platform default", shortcut.Modifier)
	}
}

func TestQueryCancelShortcutUsesEscape(t *testing.T) {
	shortcut := queryCancelShortcut()

	if shortcut.KeyName != fyne.KeyEscape {
		t.Fatalf("cancel shortcut key = %q, want Escape", shortcut.KeyName)
	}
	if shortcut.Modifier != 0 {
		t.Fatalf("cancel shortcut modifier = %v, want none", shortcut.Modifier)
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
	want := []string{"⊙", "Address", "AETitle", "Port", "Q...", "Retrieve", "Send", "TLS", "Name", "Send Transfer Syntax", "Move Destination", "Notes"}

	if !slices.Equal(headers, want) {
		t.Fatalf("node headers = %v, want operational order %v", headers, want)
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
		{nodeTableColumnEnabled, "☑"},
		{nodeTableColumnHost, "192.168.100.26"},
		{nodeTableColumnAETitle, "RADIANTAE"},
		{nodeTableColumnPort, "11112"},
		{nodeTableColumnQuery, "☑"},
		{nodeTableColumnRetrieve, "▾ Auto"},
		{nodeTableColumnSend, "☑"},
		{nodeTableColumnTLS, "▾ No"},
		{nodeTableColumnName, "RADIANT"},
		{nodeTableColumnSendSyntax, "▾ Auto"},
		{nodeTableColumnMoveDestination, "GOPACS"},
		{nodeTableColumnNotes, "lab node"},
	}

	for _, tt := range tests {
		if got := nodeCell(node, tt.col); got != tt.want {
			t.Fatalf("nodeCell col %d = %q, want %q", tt.col, got, tt.want)
		}
	}
}

func TestNodeCellShowsDisabledState(t *testing.T) {
	node := nodes.Node{Disabled: true}

	if got := nodeCell(node, nodeTableColumnEnabled); got != "☐" {
		t.Fatalf("disabled Enabled cell = %q, want unchecked marker", got)
	}
}

func TestNodeCellShowsQueryAndSendDisabledState(t *testing.T) {
	node := nodes.Node{QueryDisabled: true, SendDisabled: true}

	if got := nodeCell(node, nodeTableColumnQuery); got != "☐" {
		t.Fatalf("query cell = %q, want unchecked marker", got)
	}
	if got := nodeCell(node, nodeTableColumnSend); got != "☐" {
		t.Fatalf("send cell = %q, want unchecked marker", got)
	}
}

func TestNodeCellShowsRetrieveMethodPreference(t *testing.T) {
	node := nodes.Node{RetrieveMethod: nodes.RetrieveMethodGet}

	if got := nodeCell(node, nodeTableColumnRetrieve); got != "▾ C-GET" {
		t.Fatalf("retrieve cell = %q, want dropdown marker", got)
	}
}

func TestApplyNodeSortSortsAndTogglesDirection(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "beta", AETitle: "BETA", Host: "10.0.0.2", Port: 11112},
			{Name: "alpha", AETitle: "ALPHA", Host: "10.0.0.1", Port: 104},
			{Name: "gamma", AETitle: "GAMMA", Host: "10.0.0.3", Port: 4242},
		},
		selectedNodeRow: 2,
	}

	if !applyNodeSort(state, nodeTableColumnName) {
		t.Fatal("applyNodeSort returned false for Name column")
	}
	if got := nodeTableNames(state); strings.Join(got, "|") != "alpha|beta|gamma" {
		t.Fatalf("ascending node order = %v", got)
	}
	if got := nodeNames(state.nodes); strings.Join(got, "|") != "beta|alpha|gamma" {
		t.Fatalf("operational node order changed to %v", got)
	}
	if !state.nodeSortActive || state.nodeSortColumn != nodeTableColumnName || state.nodeSortDescending {
		t.Fatalf("node sort state = active %v col %d desc %v", state.nodeSortActive, state.nodeSortColumn, state.nodeSortDescending)
	}
	if state.selectedNodeRow != -1 {
		t.Fatalf("selected node row = %d, want reset", state.selectedNodeRow)
	}

	if !applyNodeSort(state, nodeTableColumnName) {
		t.Fatal("second applyNodeSort returned false for Name column")
	}
	if got := nodeTableNames(state); strings.Join(got, "|") != "gamma|beta|alpha" {
		t.Fatalf("descending node order = %v", got)
	}
	if !state.nodeSortDescending {
		t.Fatal("second sort should toggle to descending")
	}

	if !applyNodeSort(state, nodeTableColumnPort) {
		t.Fatal("applyNodeSort returned false for Port column")
	}
	if got := nodeTableNames(state); strings.Join(got, "|") != "alpha|gamma|beta" {
		t.Fatalf("ascending port order = %v", got)
	}
	if state.nodeSortColumn != nodeTableColumnPort || state.nodeSortDescending {
		t.Fatalf("new node sort state = col %d desc %v", state.nodeSortColumn, state.nodeSortDescending)
	}
}

func TestApplyNodeSortPreservesOperationalNodePriority(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "beta", AETitle: "BETA", Host: "10.0.0.2", Port: 11112},
			{Name: "alpha", AETitle: "ALPHA", Host: "10.0.0.1", Port: 104},
			{Name: "gamma", AETitle: "GAMMA", Host: "10.0.0.3", Port: 4242},
		},
		selectedNodeRow: 2,
	}

	if !applyNodeSort(state, nodeTableColumnName) {
		t.Fatal("applyNodeSort returned false for Name column")
	}

	if got := nodeNames(state.nodes); strings.Join(got, "|") != "beta|alpha|gamma" {
		t.Fatalf("operational node order changed to %v", got)
	}
	if state.selectedNodeRow != -1 {
		t.Fatalf("selected node row = %d, want reset", state.selectedNodeRow)
	}
	if got := nodeTableNames(state); strings.Join(got, "|") != "alpha|beta|gamma" {
		t.Fatalf("visual node order = %v", got)
	}
}

func TestApplyNodeSortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		nodes: []nodes.Node{
			{Name: "beta", AETitle: "BETA", Host: "10.0.0.2", Port: 11112},
			{Name: "alpha", AETitle: "ALPHA", Host: "10.0.0.1", Port: 104},
		},
	}

	if !applyNodeSort(state, nodeTableColumnName) {
		t.Fatal("applyNodeSort returned false for Name column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[nodeSortPreferenceKey]
	if !ok {
		t.Fatalf("missing node sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != nodeTableColumnName || pref.Descending {
		t.Fatalf("persisted node sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedNodeSortPreferenceSortsVisualRows(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				nodeSortPreferenceKey: {Column: nodeTableColumnName, Descending: true},
			},
		},
		nodes: []nodes.Node{
			{Name: "alpha", AETitle: "ALPHA", Host: "10.0.0.1", Port: 104},
			{Name: "gamma", AETitle: "GAMMA", Host: "10.0.0.3", Port: 4242},
			{Name: "beta", AETitle: "BETA", Host: "10.0.0.2", Port: 11112},
		},
	}

	applySavedNodeSortPreference(state)

	if got := nodeTableNames(state); strings.Join(got, "|") != "gamma|beta|alpha" {
		t.Fatalf("saved visual node order = %q", strings.Join(got, "|"))
	}
	if got := nodeNames(state.nodes); strings.Join(got, "|") != "alpha|gamma|beta" {
		t.Fatalf("operational node order changed to %v", got)
	}
	if !state.nodeSortActive || state.nodeSortColumn != nodeTableColumnName || !state.nodeSortDescending {
		t.Fatalf("saved node sort state = active %v col %d desc %v", state.nodeSortActive, state.nodeSortColumn, state.nodeSortDescending)
	}
}

func TestNodeSortRejectsEditableOperationalColumns(t *testing.T) {
	state := &uiState{nodes: []nodes.Node{{Name: "beta"}, {Name: "alpha"}}}

	for _, col := range []int{nodeTableColumnEnabled, nodeTableColumnQuery, nodeTableColumnRetrieve, nodeTableColumnSend, nodeTableColumnTLS} {
		if applyNodeSort(state, col) {
			t.Fatalf("operational column %d should not be sortable", col)
		}
	}
	if state.nodeSortActive {
		t.Fatal("node sort should remain inactive after unsortable columns")
	}
	if state.nodes[0].Name != "beta" {
		t.Fatalf("nodes reordered after unsortable sort: %#v", state.nodes)
	}
}

func TestNodeCellShowsConfiguredSendSyntax(t *testing.T) {
	node := nodes.Node{SendTransferSyntax: nodes.SendTransferSyntaxExplicitVRLittleEndian}

	if got := nodeCell(node, nodeTableColumnSendSyntax); got != "▾ Explicit VR Little Endian" {
		t.Fatalf("send syntax cell = %q", got)
	}
}

func TestSendSyntaxOptionsExposeSupportedNativeChoices(t *testing.T) {
	labels := sendSyntaxOptions()
	joined := strings.Join(labels, "|")

	if joined != "Auto|Explicit VR Little Endian|Implicit VR Little Endian" {
		t.Fatalf("send syntax options = %q", joined)
	}
	if strings.Contains(joined, "JPEG") {
		t.Fatalf("unsupported compressed syntax should not appear in send choices: %q", joined)
	}
}

func TestSendSyntaxLabelRoundTripsTransferSyntax(t *testing.T) {
	label := sendSyntaxLabel(nodes.SendTransferSyntaxImplicitVRLittleEndian)
	if label != "Implicit VR Little Endian" {
		t.Fatalf("label = %q", label)
	}

	value := sendSyntaxValue(label)
	if value != nodes.SendTransferSyntaxImplicitVRLittleEndian {
		t.Fatalf("value = %q, want %q", value, nodes.SendTransferSyntaxImplicitVRLittleEndian)
	}

	if sendSyntaxValue("unknown") != nodes.SendTransferSyntaxAuto {
		t.Fatalf("unknown label should map to Auto")
	}
}

func nodeTableNames(state *uiState) []string {
	names := make([]string, 0, len(state.nodes))
	for row := range state.nodes {
		index, ok := nodeTableNodeIndex(state, row)
		if !ok {
			names = append(names, "<missing>")
			continue
		}
		names = append(names, state.nodes[index].Name)
	}
	return names
}

func TestNodeHeaderLabelShowsSortDirection(t *testing.T) {
	state := &uiState{nodeSortActive: true, nodeSortColumn: nodeTableColumnName}

	if got := nodeHeaderLabel(state, nodeTableColumnName, "Name"); got != "Name ▲" {
		t.Fatalf("ascending header = %q, want Name ▲", got)
	}
	state.nodeSortDescending = true
	if got := nodeHeaderLabel(state, nodeTableColumnName, "Name"); got != "Name ▼" {
		t.Fatalf("descending header = %q, want Name ▼", got)
	}
	if got := nodeHeaderLabel(state, nodeTableColumnPort, "Port"); got != "Port" {
		t.Fatalf("inactive header = %q, want Port", got)
	}
}

func TestNodeDraftFromFormStateMapsOperationalFields(t *testing.T) {
	draft := nodeDraftFromFormState("pacs", "REMOTE", "localhost", 11112, false, true, "C-MOVE", false, nodes.SendTransferSyntaxExplicitVRLittleEndian, "LOCAL", "notes")

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
	if draft.SendTransferSyntax != nodes.SendTransferSyntaxExplicitVRLittleEndian {
		t.Fatalf("SendTransferSyntax = %q, want %q", draft.SendTransferSyntax, nodes.SendTransferSyntaxExplicitVRLittleEndian)
	}
	if draft.Name != "pacs" || draft.AETitle != "REMOTE" || draft.Host != "localhost" || draft.Port != 11112 || draft.PreferredMoveDestination != "LOCAL" || draft.Notes != "notes" {
		t.Fatalf("draft fields = %+v", draft)
	}
}

func TestNodeTLSControlsExposeDisabledTLSAffordance(t *testing.T) {
	fynetest.NewApp()

	controls := newNodeTLSControls()
	texts := collectWidgetTexts(controls)
	if !slices.Contains(texts, "Use TLS") || !slices.Contains(texts, "TLS Settings") {
		t.Fatalf("TLS controls texts = %#v", texts)
	}
	tlsCheck := findCheckWithText(controls, "Use TLS")
	if tlsCheck == nil {
		t.Fatal("node TLS controls should expose a Use TLS checkbox")
	}
	if !tlsCheck.Disabled() {
		t.Fatal("node TLS checkbox should stay disabled until TLS support exists")
	}
	settings := findButtonWithText(controls, "TLS Settings")
	if settings == nil {
		t.Fatal("node TLS controls should expose TLS Settings")
	}
	if !settings.Disabled() {
		t.Fatal("node TLS Settings should stay disabled until TLS support exists")
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
		{"enabled", nodeTableColumnEnabled, func(node nodes.Node) bool { return node.Disabled }},
		{"query", nodeTableColumnQuery, func(node nodes.Node) bool { return node.QueryDisabled }},
		{"send", nodeTableColumnSend, func(node nodes.Node) bool { return node.SendDisabled }},
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
		changed, err := toggleNodeOperationalCell(state, 0, nodeTableColumnRetrieve)
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

	changed, err := toggleNodeOperationalCell(state, 0, nodeTableColumnTLS)

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

func TestEnrichQueryMatchesWithLocalMetadataAddsLocalComments(t *testing.T) {
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	if err := catalog.SetStudyMetadata(ctx, "1.2.3.study", archive.StudyMetadata{Status: "Reviewed", Comments: "Discuss with surgeon"}); err != nil {
		t.Fatal(err)
	}

	matches, err := enrichQueryMatchesWithLocalMetadata(ctx, catalog, []query.Match{
		{StudyInstanceUID: "1.2.3.study", PatientName: "DOE^JANE", StudyStatusID: "REMOTE"},
		{StudyInstanceUID: "9.9.9.study", PatientName: "DOE^JOHN"},
		{PatientName: "NO^UID"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if matches[0].LocalComments != "Discuss with surgeon" {
		t.Fatalf("local comments = %q, want Discuss with surgeon", matches[0].LocalComments)
	}
	if matches[0].StudyStatusID != "REMOTE" {
		t.Fatalf("remote StudyStatusID = %q, want REMOTE", matches[0].StudyStatusID)
	}
	if matches[1].LocalComments != "" || matches[2].LocalComments != "" {
		t.Fatalf("unexpected local comments for unmatched rows: %#v", matches)
	}
}

type fakeQueryLocalCatalog struct {
	metadata map[string]archive.StudyMetadata
	exists   map[string]bool
}

func (f fakeQueryLocalCatalog) StudyMetadata(_ context.Context, studyUID string) (archive.StudyMetadata, error) {
	return f.metadata[studyUID], nil
}

func (f fakeQueryLocalCatalog) StudyExists(_ context.Context, studyUID string) (bool, error) {
	return f.exists[studyUID], nil
}

func TestEnrichQueryMatchesWithLocalMetadataMarksLocalStudies(t *testing.T) {
	matches, err := enrichQueryMatchesWithLocalMetadata(context.Background(), fakeQueryLocalCatalog{
		metadata: map[string]archive.StudyMetadata{
			"1.2.3.local": {Comments: "local note"},
		},
		exists: map[string]bool{
			"1.2.3.local": true,
		},
	}, []query.Match{
		{StudyInstanceUID: "1.2.3.local"},
		{StudyInstanceUID: "9.9.9.remote"},
		{StudyInstanceUID: "(missing)"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if matches[0].LocalState != queryLocalStatePresent {
		t.Fatalf("local state = %q, want %q", matches[0].LocalState, queryLocalStatePresent)
	}
	if matches[0].LocalComments != "local note" {
		t.Fatalf("local comments = %q, want local note", matches[0].LocalComments)
	}
	if matches[1].LocalState != "" || matches[2].LocalState != "" {
		t.Fatalf("unexpected local state for remote/missing matches: %#v", matches)
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

func TestRunQueryAcrossSourcesWithProgressReportsSourceCounts(t *testing.T) {
	sources := []nodes.Node{
		{ID: "node-1", Name: "offline"},
		{ID: "node-2", Name: "online"},
	}
	var updates []queryActivityProgress

	result, err := runQueryAcrossSourcesWithProgress(context.Background(), sources, func(_ context.Context, node nodes.Node) (query.Result, error) {
		if node.Name == "offline" {
			return query.Result{}, errors.New("association failed")
		}
		return query.Result{Matches: []query.Match{{PatientName: "A"}, {PatientName: "B"}}, FinalStatus: 0x0000}, nil
	}, func(update queryActivityProgress) {
		updates = append(updates, update)
	})

	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("error = %v, want partial source failure", err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(result.Matches))
	}
	if len(updates) != 2 {
		t.Fatalf("progress updates = %#v, want two source updates", updates)
	}
	if updates[0].Attempted != 1 || updates[0].Total != 2 || updates[0].Matches != 0 || updates[0].Failures != 1 {
		t.Fatalf("first progress update = %#v", updates[0])
	}
	if updates[1].Attempted != 2 || updates[1].Total != 2 || updates[1].Matches != 2 || updates[1].Failures != 1 {
		t.Fatalf("second progress update = %#v", updates[1])
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

	if marker := querySourceStatusMarker(state, nodesList[0]); !strings.HasPrefix(marker, "Q! ") || len(marker) <= len("Q! ") {
		t.Fatalf("failed status marker = %q", querySourceStatusMarker(state, nodesList[0]))
	}
	if marker := querySourceStatusMarker(state, nodesList[1]); !strings.HasPrefix(marker, "Q✓ ") || len(marker) <= len("Q✓ ") {
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

func TestListenerAddressSummaryTextShowsCommaSeparatedAdvertisedAddresses(t *testing.T) {
	summary := listenerAddressSummaryText([]string{"192.168.100.10", "10.0.0.5"}, "11113")

	if summary != "192.168.100.10:11113, 10.0.0.5:11113" {
		t.Fatalf("summary = %q", summary)
	}
	if strings.Contains(summary, "Advertised Address(es):") || strings.Contains(summary, "Host Name:") {
		t.Fatalf("summary should be field text only, got %q", summary)
	}
}

func TestListenerAddressSummaryTextShowsFallback(t *testing.T) {
	summary := listenerAddressSummaryText(nil, "11113")

	if summary != "-" {
		t.Fatalf("summary = %q, want -", summary)
	}
}

func TestNewListenerAddressSummaryEntryIsReadOnly(t *testing.T) {
	fynetest.NewApp()

	entry := newListenerAddressSummaryEntry("192.168.100.10:11113")

	if entry.Text != "192.168.100.10:11113" {
		t.Fatalf("entry text = %q", entry.Text)
	}
	if !entry.Disabled() {
		t.Fatal("address summary entry should be read-only until address override editing is implemented")
	}
}

func TestNewListenerHostNameControlsExposeReadOnlyHostName(t *testing.T) {
	fynetest.NewApp()

	hostName, editButton := newListenerHostNameControls(" mac-mini.local ")

	if hostName.Text != "mac-mini.local" {
		t.Fatalf("host name text = %q, want trimmed host name", hostName.Text)
	}
	if !hostName.Disabled() {
		t.Fatal("host name entry should be read-only until editing is implemented")
	}
	if editButton.Text != "Edit" {
		t.Fatalf("edit button text = %q, want Edit", editButton.Text)
	}
	if !editButton.Disabled() {
		t.Fatal("host name edit button should be disabled until editing is implemented")
	}
}

func TestNewListenerAddressEditButtonIsDisabledAffordance(t *testing.T) {
	fynetest.NewApp()

	editButton := newListenerAddressEditButton()

	if editButton.Text != "Edit" {
		t.Fatalf("edit button text = %q, want Edit", editButton.Text)
	}
	if !editButton.Disabled() {
		t.Fatal("address edit button should stay disabled until address override editing is implemented")
	}
}

func TestListenerReachableEndpointsTrimsAndDefaultsPort(t *testing.T) {
	endpoints := listenerReachableEndpoints([]string{" 192.168.100.10 ", "", "10.0.0.5"}, "")

	if strings.Join(endpoints, "|") != "192.168.100.10:11112|10.0.0.5:11112" {
		t.Fatalf("endpoints = %#v", endpoints)
	}
}

func TestListenerHostSelectOptionsPreferCurrentHostAndDetectedAddresses(t *testing.T) {
	options := listenerHostSelectOptions(" 0.0.0.0 ", []string{"192.168.100.10", "0.0.0.0", "", "10.0.0.5"})

	if strings.Join(options, "|") != "0.0.0.0|192.168.100.10|10.0.0.5" {
		t.Fatalf("host options = %#v", options)
	}
}

func TestNewListenerHostSelectUpdatesReceiverHost(t *testing.T) {
	fynetest.NewApp()
	receiverHost := widget.NewEntry()
	receiverHost.SetText("0.0.0.0")
	updated := false

	selectWidget := newListenerHostSelect(receiverHost, []string{"192.168.100.10"}, func() {
		updated = true
	})
	selectWidget.SetSelected("192.168.100.10")

	if receiverHost.Text != "192.168.100.10" {
		t.Fatalf("receiver host = %q, want selected address", receiverHost.Text)
	}
	if !updated {
		t.Fatal("listener host selection should trigger UI refresh callback")
	}
}

func TestAETitleFromHostNameSanitizesAndTruncatesForDICOM(t *testing.T) {
	got := aeTitleFromHostName("Thaless-Mac-mini.local")

	if got != "THALESS MAC MINI" {
		t.Fatalf("AE title from host = %q, want THALESS MAC MINI", got)
	}
	if err := nodes.ValidateAETitle(got); err != nil {
		t.Fatalf("derived AE title should be valid: %v", err)
	}
}

func TestNewUseHostNameForAETitleCheckFillsAndRestoresManualTitle(t *testing.T) {
	fynetest.NewApp()
	localAE := widget.NewEntry()
	localAE.SetText("MANUAL")
	refreshed := false

	check := newUseHostNameForAETitleCheck(localAE, "Thaless-Mac-mini.local", func() {
		refreshed = true
	})

	check.SetChecked(true)
	if localAE.Text != "THALESS MAC MINI" {
		t.Fatalf("checked AE title = %q, want hostname-derived title", localAE.Text)
	}
	if !localAE.Disabled() {
		t.Fatal("local AE entry should be disabled while using host name")
	}
	if !refreshed {
		t.Fatal("checking host-name AE should refresh listener status")
	}

	refreshed = false
	check.SetChecked(false)
	if localAE.Text != "MANUAL" {
		t.Fatalf("unchecked AE title = %q, want restored manual title", localAE.Text)
	}
	if localAE.Disabled() {
		t.Fatal("local AE entry should be editable after disabling host-name AE")
	}
	if !refreshed {
		t.Fatal("unchecking host-name AE should refresh listener status")
	}
}

func TestListenerSettingsStatusTextShowsStoppedAndPendingStates(t *testing.T) {
	stopped := listenerSettingsStatusText("GOPACS", "0.0.0.0", "11113", false, nil)
	if stopped != "Stopped: GOPACS binding 0.0.0.0:11113" {
		t.Fatalf("stopped status = %q", stopped)
	}

	pending := listenerSettingsStatusText("GOPACS", "0.0.0.0", "11113", true, nil)
	if pending != "Will start on Save: GOPACS binding 0.0.0.0:11113" {
		t.Fatalf("pending status = %q", pending)
	}
}

func TestListenerSettingsStatusTextShowsRunningSnapshot(t *testing.T) {
	snapshot := receive.Snapshot{AETitle: "GOPACS", Address: "127.0.0.1:11113", Stored: 7}

	status := listenerSettingsStatusText("IGNORED", "0.0.0.0", "11112", false, &snapshot)

	if status != "Running: GOPACS binding 127.0.0.1:11113; stored 7 objects" {
		t.Fatalf("running status = %q", status)
	}
}

func TestNewActivateListenerCheckUsesAutoStartWording(t *testing.T) {
	fynetest.NewApp()

	autoStart := newActivateListenerCheck(true, false)
	running := newActivateListenerCheck(false, true)
	stopped := newActivateListenerCheck(false, false)

	if autoStart.Text != "Activate DICOM listener when Go PACS is running" {
		t.Fatalf("activate listener text = %q", autoStart.Text)
	}
	if !autoStart.Checked {
		t.Fatal("auto-start checkbox should be checked when the persisted preference is enabled")
	}
	if !running.Checked {
		t.Fatal("auto-start checkbox should be checked while the receiver is already running")
	}
	if stopped.Checked {
		t.Fatal("auto-start checkbox should be unchecked when stopped and not persisted")
	}
}

func TestListenerPortFormLabelMatchesReference(t *testing.T) {
	if settingsLabelReceiverPort != "Port Number" {
		t.Fatalf("receiver port form label = %q, want Port Number", settingsLabelReceiverPort)
	}
}

func TestNewReceiverPortControlsShowValidRangeHint(t *testing.T) {
	fynetest.NewApp()

	entry, controls := newReceiverPortControls("11112")
	texts := collectWidgetTexts(controls)

	if entry.Text != "11112" {
		t.Fatalf("port entry text = %q", entry.Text)
	}
	if !slices.Contains(texts, "11112") {
		t.Fatalf("port controls missing entry text in %#v", texts)
	}
	if !slices.Contains(texts, "(between 1 and 65535)") {
		t.Fatalf("port controls missing valid range hint in %#v", texts)
	}
}

func TestListenerIncomingPolicyControlsExposeDisabledIncomingPolicyChoices(t *testing.T) {
	fynetest.NewApp()

	panel := newListenerIncomingPolicyControls()
	texts := collectWidgetTexts(panel)

	for _, want := range []string{
		"Check for new files every",
		"10",
		"seconds",
		"Incoming files",
		"If Go PACS is not able to open a file received by the DICOM listener:",
		"Replace existing files with newly received files",
	} {
		if !slices.Contains(texts, want) {
			t.Fatalf("incoming policy controls missing %q in %#v", want, texts)
		}
	}
	incomingFiles := findRadioGroupWithOption(panel, "Compress non-compressed images with JPEG (See General Preferences)")
	if incomingFiles == nil {
		t.Fatal("incoming file processing choices should be a disabled radio group")
	}
	if incomingFiles.Selected != "Don't modify" {
		t.Fatalf("incoming file selected choice = %q, want Don't modify", incomingFiles.Selected)
	}
	if !incomingFiles.Disabled() {
		t.Fatal("incoming file radio group should be disabled until policy support is implemented")
	}
	unreadableObjects := findRadioGroupWithOption(panel, "Move it to the NOT READABLE folder")
	if unreadableObjects == nil {
		t.Fatal("unreadable object choices should be a disabled radio group")
	}
	if unreadableObjects.Selected != "Move it to the NOT READABLE folder" {
		t.Fatalf("unreadable selected choice = %q, want move policy", unreadableObjects.Selected)
	}
	if !unreadableObjects.Disabled() {
		t.Fatal("unreadable object radio group should be disabled until raw-file policy support is implemented")
	}
}

func TestListenerMetadataPolicyControlsExposeDisabledMetadataPolicyChoices(t *testing.T) {
	fynetest.NewApp()

	panel := newListenerMetadataPolicyControls()
	texts := collectWidgetTexts(panel)

	for _, want := range []string{
		"Store Destination AETitle in PrivateInformationCreatorUID (0002,0100)",
		"Store Source AETitle in SourceApplicationEntityTitle (0002,0016)",
		"Activate the DICOM Listener only if this user session is active (user switching)",
	} {
		if !slices.Contains(texts, want) {
			t.Fatalf("metadata policy controls missing %q in %#v", want, texts)
		}
	}
}

func TestDICOMTimeoutDurationsUseConfiguredSecondsAndDefaults(t *testing.T) {
	cfg := appconfig.Config{
		DICOMCommunicationTimeoutSeconds: 55,
		DICOMConnectionTimeoutSeconds:    12,
	}

	if got := dicomCommunicationTimeoutDuration(cfg); got != 55*time.Second {
		t.Fatalf("communication timeout = %s, want 55s", got)
	}
	if got := dicomConnectionTimeoutDuration(cfg); got != 12*time.Second {
		t.Fatalf("connection timeout = %s, want 12s", got)
	}

	if got := dicomCommunicationTimeoutDuration(appconfig.Config{}); got != time.Duration(appconfig.DefaultDICOMCommunicationTimeoutSeconds)*time.Second {
		t.Fatalf("default communication timeout = %s", got)
	}
	if got := dicomConnectionTimeoutDuration(appconfig.Config{}); got != time.Duration(appconfig.DefaultDICOMConnectionTimeoutSeconds)*time.Second {
		t.Fatalf("default connection timeout = %s", got)
	}
}

func TestNewDICOMTimeoutControlsShowSecondsSuffix(t *testing.T) {
	fynetest.NewApp()

	entry, controls := newDICOMTimeoutControls("40")
	texts := collectWidgetTexts(controls)

	if entry.Text != "40" {
		t.Fatalf("timeout entry text = %q, want 40", entry.Text)
	}
	if !slices.Contains(texts, "40") {
		t.Fatalf("timeout controls missing entry text in %#v", texts)
	}
	if !slices.Contains(texts, "seconds") {
		t.Fatalf("timeout controls missing seconds suffix in %#v", texts)
	}
}

func TestReceivePreferredSyntaxOptionsExposeSupportedNativeChoices(t *testing.T) {
	labels := receivePreferredSyntaxLabels()
	joined := strings.Join(labels, "|")

	if joined != "Auto|Explicit VR Little Endian|Implicit VR Little Endian" {
		t.Fatalf("receive preferred syntax labels = %q", joined)
	}
	if strings.Contains(joined, "JPEG") {
		t.Fatalf("unsupported compressed syntax should not appear in supported choices: %q", joined)
	}
}

func TestReceivePreferredSyntaxLabelRoundTripsTransferSyntax(t *testing.T) {
	label := receivePreferredSyntaxLabel(receive.PreferredTransferSyntaxExplicitVRLittleEndian)
	if label != "Explicit VR Little Endian" {
		t.Fatalf("label = %q", label)
	}

	value := receivePreferredSyntaxValue(label)
	if value != receive.PreferredTransferSyntaxExplicitVRLittleEndian {
		t.Fatalf("value = %q, want %q", value, receive.PreferredTransferSyntaxExplicitVRLittleEndian)
	}

	if receivePreferredSyntaxValue("unknown") != receive.PreferredTransferSyntaxAuto {
		t.Fatalf("unknown label should map to Auto")
	}
}

func TestNewPreferredReceiveSyntaxControlsShowRetrieveUsageHint(t *testing.T) {
	fynetest.NewApp()

	selectWidget, hint, controls := newPreferredReceiveSyntaxControls(receive.PreferredTransferSyntaxImplicitVRLittleEndian)

	if selectWidget.Selected != receivePreferredSyntaxImplicitLabel {
		t.Fatalf("selected preferred syntax = %q, want %q", selectWidget.Selected, receivePreferredSyntaxImplicitLabel)
	}
	if hint.Text != "(used during Q/R Retrieve)" {
		t.Fatalf("hint = %q, want retrieve usage text", hint.Text)
	}
	if controls == nil {
		t.Fatal("preferred syntax controls should return a composed UI object")
	}
}

func TestDICOMTimeoutContextAppliesConfiguredDeadline(t *testing.T) {
	state := &uiState{appConfig: appconfig.Config{DICOMCommunicationTimeoutSeconds: 55}}

	ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 54*time.Second || remaining > 55*time.Second {
		t.Fatalf("deadline remaining = %s, want about 55s", remaining)
	}
}

func TestParsePositiveSecondsRequiresPositiveInteger(t *testing.T) {
	value, err := parsePositiveSeconds(" 40 ", "DICOM communications timeout")
	if err != nil {
		t.Fatal(err)
	}
	if value != 40 {
		t.Fatalf("seconds = %d, want 40", value)
	}

	for _, input := range []string{"", "0", "-1", "1.5"} {
		if _, err := parsePositiveSeconds(input, "DICOM communications timeout"); err == nil {
			t.Fatalf("parsePositiveSeconds(%q) succeeded, want error", input)
		}
	}
}

func TestArchiveAlbumLinesSummarizeStudyCounts(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	studies := []archive.Study{
		{Modalities: "CT", StudyDate: "20260604", StudyTime: "113000", ImportedAt: now.Add(-2 * time.Hour)},
		{Modalities: "CR", StudyDate: "20260604", StudyTime: "090000", ImportedAt: now.Add(-30 * time.Minute)},
		{Modalities: "MR", StudyDate: "20260604", StudyTime: "113500", ImportedAt: now.Add(-30 * time.Minute)},
		{Modalities: "CT\\SR", StudyDate: "20260602", StudyTime: "120000", ImportedAt: now.AddDate(0, 0, -2)},
	}

	lines := archiveAlbumLines(studies, now)
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		"Database                         4",
		"Just Acquired (last hour)        2",
		"Just Added (last hour)           2",
		"Today                            3",
		"Today CR                         1",
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
		{Modalities: "CT", ImportedAt: now, Status: "Interesting", Comments: "Discuss with surgeon"},
		{Modalities: "MR", ImportedAt: now.Add(-2 * time.Hour)},
	}

	rows := archiveAlbumRows(studies, now, archiveAlbumToday)

	if rows[0].Text != "  Database                         2" {
		t.Fatalf("database row text = %q", rows[0].Text)
	}
	if rows[6].Text != "▶ Today                            2" {
		t.Fatalf("selected today row text = %q", rows[6].Text)
	}
	if !rows[6].Filterable {
		t.Fatalf("today row should be filterable")
	}
	if rows[1].Text != "  Cases with comments              1" {
		t.Fatalf("comments row text = %q", rows[1].Text)
	}
	if !rows[1].Filterable {
		t.Fatalf("cases with comments row should be filterable")
	}
	if rows[2].Text != "  Interesting Cases                1" {
		t.Fatalf("interesting row text = %q", rows[2].Text)
	}
	if !rows[2].Filterable {
		t.Fatalf("interesting cases row should be filterable")
	}
}

func TestArchiveAlbumRowsCountAndFilterJustOpenedStudies(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	studies := []archive.Study{
		{StudyInstanceUID: "study-opened", PatientName: "OPENED"},
		{StudyInstanceUID: "study-other", PatientName: "OTHER"},
	}
	opened := map[string]bool{"study-opened": true}

	rows := archiveAlbumRowsWithOpened(studies, now, archiveAlbumOpened, opened)

	if rows[5].Text != "▶ Just Opened                      1" {
		t.Fatalf("opened row text = %q", rows[5].Text)
	}
	if !rows[5].Filterable {
		t.Fatal("Just Opened should be filterable once opened study tracking exists")
	}

	filtered := filterStudiesByOpenedUIDs(studies, opened)
	if len(filtered) != 1 || filtered[0].StudyInstanceUID != "study-opened" {
		t.Fatalf("opened filtered studies = %#v", filtered)
	}
}

func TestArchiveAlbumListUsesNativeIconsAndTrailingCounts(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	state := &uiState{
		studies: []archive.Study{
			{Modalities: "CT", ImportedAt: now, Comments: "Needs review"},
			{Modalities: "MR", ImportedAt: now.Add(-2 * time.Hour)},
		},
		selectedArchiveAlbum: archiveAlbumComments,
	}
	list := newArchiveAlbumList(nil, widget.NewLabel(""), archiveTables{}, state)
	item := list.CreateItem()

	albumItem, ok := item.(*archiveAlbumListItem)
	if !ok {
		t.Fatalf("album list item = %T, want *archiveAlbumListItem", item)
	}
	list.UpdateItem(widget.ListItemID(0), albumItem)

	if albumItem.albumIcon.Resource == nil || albumItem.albumIcon.Resource.Name() != theme.StorageIcon().Name() {
		t.Fatalf("database icon = %#v, want storage icon", albumItem.albumIcon.Resource)
	}
	if albumItem.label.Text != "Database" {
		t.Fatalf("database label = %q", albumItem.label.Text)
	}
	if albumItem.count.Text != "2" {
		t.Fatalf("database count = %q, want 2", albumItem.count.Text)
	}
	if albumItem.countSlot == nil {
		t.Fatal("album count should be wrapped in a fixed trailing slot")
	}
	if size := albumItem.countSlot.MinSize(); size.Width != compactArchiveAlbumCountSlotWidth {
		t.Fatalf("album count slot width = %.0f, want %.0f", size.Width, compactArchiveAlbumCountSlotWidth)
	}
	if albumItem.selectionIcon.Visible() {
		t.Fatal("unselected album should hide selection marker")
	}

	list.UpdateItem(widget.ListItemID(1), albumItem)
	if albumItem.albumIcon.Resource == nil || albumItem.albumIcon.Resource.Name() != theme.FolderIcon().Name() {
		t.Fatalf("album icon = %#v, want folder icon", albumItem.albumIcon.Resource)
	}
	if albumItem.label.Text != "Cases with comments" {
		t.Fatalf("comments label = %q", albumItem.label.Text)
	}
	if albumItem.count.Text != "1" {
		t.Fatalf("comments count = %q, want 1", albumItem.count.Text)
	}
	if !albumItem.selectionIcon.Visible() || albumItem.selectionIcon.Resource.Name() != theme.NavigateNextIcon().Name() {
		t.Fatalf("selected marker = visible %v resource %#v", albumItem.selectionIcon.Visible(), albumItem.selectionIcon.Resource)
	}
	if got := color.NRGBAModel.Convert(albumItem.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected album fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
	if got := countCanvasRectanglesWithColor(albumItem.Container, tableColumnDividerColor); got < 2 {
		t.Fatalf("album row dividers = %d, want right and bottom divider", got)
	}
}

func TestRecordOpenedArchiveStudyTracksSelectedStudyUID(t *testing.T) {
	state := &uiState{}

	recordOpenedArchiveStudy(state, archive.Study{StudyInstanceUID: " study-1 "})

	if !state.openedArchiveStudyUIDs["study-1"] {
		t.Fatalf("opened study UIDs = %#v, want study-1", state.openedArchiveStudyUIDs)
	}
}

func TestRecordOpenedArchiveStudyPersistsRecentUIDs(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig: appconfig.Config{
			LocalAETitle:           "GOPACS",
			ReceiverAddress:        "127.0.0.1:11113",
			OpenedArchiveStudyUIDs: []string{"study-old"},
		},
	}

	recordOpenedArchiveStudy(state, archive.Study{StudyInstanceUID: " study-new "})
	recordOpenedArchiveStudy(state, archive.Study{StudyInstanceUID: " study-old "})

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	if got, want := loaded.OpenedArchiveStudyUIDs, []string{"study-old", "study-new"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("persisted opened studies = %#v, want %#v", got, want)
	}
	if !state.openedArchiveStudyUIDs["study-new"] || !state.openedArchiveStudyUIDs["study-old"] {
		t.Fatalf("opened study map = %#v, want both studies", state.openedArchiveStudyUIDs)
	}
}

func TestArchiveAlbumFilterForComments(t *testing.T) {
	filters, ok := archiveAlbumFilters(archiveAlbumComments, time.Now())

	if !ok {
		t.Fatalf("comments album should be filterable")
	}
	if !filters.HasComments {
		t.Fatalf("HasComments = false, want true")
	}
}

func TestArchiveAlbumFilterForInteresting(t *testing.T) {
	filters, ok := archiveAlbumFilters(archiveAlbumInteresting, time.Now())

	if !ok {
		t.Fatalf("interesting album should be filterable")
	}
	if filters.Status != "Interesting" {
		t.Fatalf("Status = %q, want Interesting", filters.Status)
	}
}

func TestArchiveAlbumFilterForJustAddedLastHour(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)

	filters, ok := archiveAlbumFilters(archiveAlbumAddedLastHour, now)

	if !ok {
		t.Fatal("Just Added album should produce filters")
	}
	if filters.ImportedAtFrom == "" || filters.StudyDateTimeFrom != "" {
		t.Fatalf("Just Added filters = %#v, want imported-at only", filters)
	}
}

func TestArchiveAlbumFilterForJustAcquiredLastHour(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)

	filters, ok := archiveAlbumFilters(archiveAlbumLastHour, now)

	if !ok {
		t.Fatal("Just Acquired album should produce filters")
	}
	if filters.StudyDateTimeFrom != "20260604110000" || filters.StudyDateTimeTo != "20260604120000" {
		t.Fatalf("Just Acquired filters = %#v, want study datetime bounds", filters)
	}
	if filters.ImportedAtFrom != "" || filters.ImportedAtTo != "" {
		t.Fatalf("Just Acquired should not use imported-at filters: %#v", filters)
	}
}

func TestArchiveAlbumFilterForTodayCR(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)

	filters, ok := archiveAlbumFilters(archiveAlbumTodayCR, now)

	if !ok {
		t.Fatalf("Today CR album should produce filters")
	}
	if filters.ImportedAtFrom == "" || filters.ImportedAtTo == "" {
		t.Fatalf("today filter did not set imported-at bounds: %#v", filters)
	}
	if strings.Join(filters.Modalities, ",") != "CR" {
		t.Fatalf("Modalities = %#v, want CR", filters.Modalities)
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
		filters.SourcePath != "" ||
		filters.Status != "" ||
		filters.HasComments {
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

func TestArchiveFiltersWithCommentsAlbumPreservesUserSearchFields(t *testing.T) {
	base := archive.StudyFilters{
		PatientName:      "DOE",
		AccessionNumber:  "A100",
		StudyDescription: "abdomen",
	}

	filters, ok := archiveFiltersWithAlbum(base, archiveAlbumComments, time.Now())

	if !ok {
		t.Fatalf("comments album should be filterable")
	}
	if !filters.HasComments {
		t.Fatalf("HasComments = false, want true")
	}
	if filters.PatientName != "DOE" || filters.AccessionNumber != "A100" || filters.StudyDescription != "abdomen" {
		t.Fatalf("user search fields were not preserved: %#v", filters)
	}
}

func TestArchiveFiltersWithInterestingAlbumPreservesUserSearchFields(t *testing.T) {
	base := archive.StudyFilters{
		PatientName:      "DOE",
		AccessionNumber:  "A100",
		StudyDescription: "abdomen",
	}

	filters, ok := archiveFiltersWithAlbum(base, archiveAlbumInteresting, time.Now())

	if !ok {
		t.Fatalf("interesting album should be filterable")
	}
	if filters.Status != "Interesting" {
		t.Fatalf("Status = %q, want Interesting", filters.Status)
	}
	if filters.PatientName != "DOE" || filters.AccessionNumber != "A100" || filters.StudyDescription != "abdomen" {
		t.Fatalf("user search fields were not preserved: %#v", filters)
	}
}

func TestStudyStatusPresetOptionsIncludeInteresting(t *testing.T) {
	options := studyStatusPresetOptions()
	joined := strings.Join(options, "|")

	if !strings.Contains(joined, "Interesting") {
		t.Fatalf("status preset options = %q, want Interesting", joined)
	}
	if studyStatusPresetValue("Interesting") != "Interesting" {
		t.Fatalf("Interesting preset should map to Interesting status")
	}
	if studyStatusPresetValue("Custom") != "" {
		t.Fatalf("Custom preset should not overwrite the manual status")
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

func TestArchiveSourceRowsUseNativeIconsAndSelection(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: 1,
		appConfig:       appconfig.Config{LocalAETitle: "GOPACS", ReceiverAddress: "0.0.0.0:11113"},
	}

	rows := archiveSourceRows(state)

	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want local receiver plus 2 nodes", len(rows))
	}
	if rows[0].Icon == nil || rows[0].Icon.Name() != theme.StorageIcon().Name() {
		t.Fatalf("local icon = %#v, want storage icon", rows[0].Icon)
	}
	if rows[1].Icon == nil || rows[1].Icon.Name() != theme.ComputerIcon().Name() {
		t.Fatalf("receiver icon = %#v, want computer icon", rows[1].Icon)
	}
	if rows[2].Icon == nil || rows[2].Icon.Name() != theme.DesktopIcon().Name() {
		t.Fatalf("node icon = %#v, want desktop icon", rows[2].Icon)
	}
	if rows[2].Selected {
		t.Fatalf("first node should not be selected: %#v", rows[2])
	}
	if rows[2].NodeIndex != 0 {
		t.Fatalf("first node index = %d, want 0", rows[2].NodeIndex)
	}
	if !rows[3].Selected || rows[3].Text != "HOROSMINI 192.168.100.50:4007" || rows[3].NodeIndex != 1 {
		t.Fatalf("selected node row = %#v", rows[3])
	}
}

func TestArchiveSourceListUsesNativeIconWidgets(t *testing.T) {
	state := &uiState{
		nodes:           []nodes.Node{{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}},
		selectedNodeRow: -1,
	}
	list := newArchiveSourceList(state)
	item := list.CreateItem()

	sourceItem, ok := item.(*archiveSourceListItem)
	if !ok {
		t.Fatalf("source list item = %T, want *archiveSourceListItem", item)
	}
	list.UpdateItem(widget.ListItemID(2), sourceItem)

	if sourceItem.sourceIcon.Resource == nil || sourceItem.sourceIcon.Resource.Name() != theme.DesktopIcon().Name() {
		t.Fatalf("source icon = %#v, want desktop icon", sourceItem.sourceIcon.Resource)
	}
	if sourceItem.selectionIcon.Visible() {
		t.Fatal("unselected source should hide selection icon")
	}
	state.selectedNodeRow = 0
	list.UpdateItem(widget.ListItemID(2), sourceItem)
	if !sourceItem.selectionIcon.Visible() || sourceItem.selectionIcon.Resource.Name() != theme.NavigateNextIcon().Name() {
		t.Fatalf("selected marker = visible %v resource %#v", sourceItem.selectionIcon.Visible(), sourceItem.selectionIcon.Resource)
	}
	if sourceItem.label.Text != "RADIANT 192.168.100.26:11112" {
		t.Fatalf("source label = %q", sourceItem.label.Text)
	}
	if got := color.NRGBAModel.Convert(sourceItem.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected source fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
	if got := countCanvasRectanglesWithColor(sourceItem.Container, tableColumnDividerColor); got < 2 {
		t.Fatalf("source row dividers = %d, want right and bottom divider", got)
	}
}

func TestArchiveSourceListSelectsRemoteNode(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: -1,
	}
	list := newArchiveSourceList(state)
	if list.OnSelected == nil {
		t.Fatal("Archive source list should handle selection")
	}

	list.OnSelected(widget.ListItemID(0))
	if state.selectedNodeRow != -1 {
		t.Fatalf("local DB selection changed node row to %d", state.selectedNodeRow)
	}
	list.OnSelected(widget.ListItemID(1))
	if state.selectedNodeRow != -1 {
		t.Fatalf("receiver selection changed node row to %d", state.selectedNodeRow)
	}
	list.OnSelected(widget.ListItemID(3))

	if state.selectedNodeRow != 1 {
		t.Fatalf("selectedNodeRow = %d, want 1", state.selectedNodeRow)
	}
}

func TestArchiveSourcePriorityButtonsMoveSelectedNode(t *testing.T) {
	state := &uiState{
		nodes: []nodes.Node{
			{Name: "RADIANT", Host: "192.168.100.26", Port: 11112},
			{Name: "HOROSMINI", Host: "192.168.100.50", Port: 4007},
		},
		selectedNodeRow: 1,
	}
	status := widget.NewLabel("")

	newArchiveSidebar(nil, status, archiveTables{}, state)

	if state.archiveSourceMoveUpButton == nil || state.archiveSourceMoveUpButton.Icon.Name() != theme.MoveUpIcon().Name() {
		t.Fatalf("move up button = %#v", state.archiveSourceMoveUpButton)
	}
	if state.archiveSourceMoveDownButton == nil || state.archiveSourceMoveDownButton.Icon.Name() != theme.MoveDownIcon().Name() {
		t.Fatalf("move down button = %#v", state.archiveSourceMoveDownButton)
	}

	state.archiveSourceMoveUpButton.OnTapped()

	if state.selectedNodeRow != 0 {
		t.Fatalf("selectedNodeRow = %d, want 0", state.selectedNodeRow)
	}
	if got := strings.Join(nodeNames(state.nodes), "|"); got != "HOROSMINI|RADIANT" {
		t.Fatalf("node order = %q", got)
	}
	if status.Text != "Updated source priority" {
		t.Fatalf("status = %q, want Updated source priority", status.Text)
	}
}

func TestArchiveSidebarSectionsUseWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}
	status := widget.NewLabel("")

	sidebar := newArchiveSidebar(nil, status, archiveTables{}, state)
	content := sidebar
	if scroll, ok := sidebar.(*container.Scroll); ok {
		content = scroll.Content
	}

	for _, title := range []string{"Albums", "Sources", "Activity"} {
		if findLabelContaining(content, title) == nil {
			t.Fatalf("archive sidebar should expose %q section title", title)
		}
	}
	if got := countCanvasRectanglesWithColor(content, archiveHeaderRowColor); got < 3 {
		t.Fatalf("archive sidebar header backgrounds = %d, want one per section", got)
	}
	if got := countCanvasRectanglesWithColor(content, archiveOddRowColor); got < 3 {
		t.Fatalf("archive sidebar body backgrounds = %d, want one per section", got)
	}
	if got := countCanvasRectanglesWithColor(content, tableColumnDividerColor); got < 6 {
		t.Fatalf("archive sidebar divider rectangles = %d, want panel borders and header dividers", got)
	}
}

func TestArchiveSummaryTextShowsSelectedStudySeriesAndInstances(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{
			PatientName:      "DOE^JANE",
			PatientID:        "P123",
			PatientBirthDate: "19700102",
			InstitutionName:  "General Hospital",
			StudyDate:        "20260531",
			StudyTime:        "134501",
			StudyDescription: "CT Abdomen",
			Modalities:       "CT",
			AccessionNumber:  "A100",
			Status:           "Reviewed",
			Comments:         "Discuss with surgeon",
			SeriesCount:      2,
			InstanceCount:    42,
		}},
		series: []archive.Series{{
			Modality:          "CT",
			SeriesNumber:      "4",
			SeriesDescription: "Portal Venous",
			InstanceCount:     40,
		}},
		instances: []archive.Instance{{SOPInstanceUID: "1.2.3", SourcePath: "incoming/ct1.dcm"}},
	}
	state.selectedStudyRow = 0
	state.selectedSeriesRow = 0

	text := archiveSummaryText(state)

	for _, want := range []string{
		"DOE^JANE",
		"Patient ID: P123",
		"DOB: 19700102",
		"Study: 20260531 13:45:01 CT",
		"Institution: General Hospital",
		"Accession: A100",
		"Source: incoming/ct1.dcm",
		"Status: ✓ Reviewed",
		"Comments: Discuss with surgeon",
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

func TestArchiveSummaryPaneTitleUsesSelectedPatientName(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		studies: []archive.Study{{
			PatientName: "MARIA^OLIVEIRA",
			PatientID:   "3020035",
		}},
		selectedStudyRow: 0,
	}

	_ = newArchiveSummaryPane(nil, widget.NewLabel(""), archiveTables{}, state)
	refreshArchiveChrome(state)

	if state.archiveSummaryTitle == nil {
		t.Fatal("archive summary title was not wired")
	}
	if got := state.archiveSummaryTitle.Text; got != "MARIA^OLIVEIRA" {
		t.Fatalf("archive summary title = %q, want selected patient name", got)
	}

	state.selectedStudyRow = -1
	refreshArchiveChrome(state)
	if got := state.archiveSummaryTitle.Text; got != "Selected Study" {
		t.Fatalf("archive summary title after clearing selection = %q, want Selected Study", got)
	}
}

func TestArchiveSummaryPaneUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}

	pane := newArchiveSummaryPane(nil, widget.NewLabel(""), archiveTables{}, state)

	if state.archiveSummaryTitle == nil || state.archiveEditStudyButton == nil || state.archiveSummary == nil {
		t.Fatal("archive summary pane should wire title, edit button, and summary label")
	}
	if !containsCanvasRectangleColor(pane, archiveHeaderRowColor) {
		t.Fatal("archive summary pane should use dark workbench header background")
	}
	if !containsCanvasRectangleColor(pane, archiveOddRowColor) {
		t.Fatal("archive summary pane should use dark workbench body background")
	}
	if got := countCanvasRectanglesWithColor(pane, tableColumnDividerColor); got < 2 {
		t.Fatalf("archive summary pane divider rectangles = %d, want panel borders and header divider", got)
	}
}

func TestArchiveSummaryPaneUsesIconOnlyMetadataEditControl(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{}

	_ = newArchiveSummaryPane(nil, widget.NewLabel(""), archiveTables{}, state)

	if state.archiveEditStudyButton == nil {
		t.Fatal("archive summary edit button was not wired")
	}
	if state.archiveEditStudyButton.Text != "" {
		t.Fatalf("archive summary edit button text = %q, want icon-only", state.archiveEditStudyButton.Text)
	}
	if state.archiveEditStudyButton.Icon == nil || state.archiveEditStudyButton.Icon.Name() != theme.DocumentCreateIcon().Name() {
		t.Fatalf("archive summary edit button icon = %#v, want document-create icon", state.archiveEditStudyButton.Icon)
	}
}

func TestSaveSelectedStudyMetadataPersistsAndRefreshesStudy(t *testing.T) {
	fynetest.NewApp()
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	status := widget.NewLabel("")
	state := &uiState{
		catalog:          catalog,
		studies:          []archive.Study{{StudyInstanceUID: "1.2.3.study", StudyDescription: "CT Abdomen"}},
		selectedStudyRow: 0,
		archiveSummary:   widget.NewLabel("old summary"),
	}
	tables := archiveTables{
		studies:   widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
		series:    widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
		instances: widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
	}

	if err := saveSelectedStudyMetadata(ctx, status, tables, state, archive.StudyMetadata{Status: "Reviewed", Comments: "Discuss with surgeon"}); err != nil {
		t.Fatal(err)
	}

	if state.studies[0].Status != "Reviewed" || state.studies[0].Comments != "Discuss with surgeon" {
		t.Fatalf("state study metadata = %q/%q", state.studies[0].Status, state.studies[0].Comments)
	}
	if !strings.Contains(state.archiveSummary.Text, "Status: ✓ Reviewed") || !strings.Contains(state.archiveSummary.Text, "Comments: Discuss with surgeon") {
		t.Fatalf("archive summary = %q", state.archiveSummary.Text)
	}
	if status.Text != "Updated study status/comments" {
		t.Fatalf("status = %q", status.Text)
	}
	stored, err := catalog.StudyMetadata(ctx, "1.2.3.study")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "Reviewed" || stored.Comments != "Discuss with surgeon" {
		t.Fatalf("stored metadata = %#v", stored)
	}
}

func TestDeleteSelectedStudyRemovesStudyAndRefreshesArchive(t *testing.T) {
	fynetest.NewApp()
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	deleteStudyUID := "1.2.3.delete"
	keepStudyUID := "1.2.3.keep"
	importTestArchiveObject(t, catalog, "network://delete", "DELETE^TARGET", "D001", "CT", deleteStudyUID, deleteStudyUID+".series", deleteStudyUID+".instance")
	importTestArchiveObject(t, catalog, "network://keep", "KEEP^OTHER", "K001", "MR", keepStudyUID, keepStudyUID+".series", keepStudyUID+".instance")

	status := widget.NewLabel("")
	state := &uiState{
		catalog:             catalog,
		studies:             []archive.Study{{StudyInstanceUID: deleteStudyUID, StudyDescription: "CT Delete"}, {StudyInstanceUID: keepStudyUID, StudyDescription: "MR Keep"}},
		selectedStudyRow:    0,
		series:              []archive.Series{{StudyInstanceUID: deleteStudyUID, SeriesInstanceUID: deleteStudyUID + ".series"}},
		selectedSeriesRow:   0,
		instances:           []archive.Instance{{StudyInstanceUID: deleteStudyUID, SOPInstanceUID: deleteStudyUID + ".instance"}},
		selectedInstanceRow: 0,
		archiveSummary:      widget.NewLabel("old summary"),
	}
	tables := archiveTables{
		studies:   widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
		series:    widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
		instances: widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
	}

	deleted, err := deleteSelectedStudy(ctx, status, tables, state)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if len(state.studies) != 1 || state.studies[0].StudyInstanceUID != keepStudyUID {
		t.Fatalf("studies after delete = %#v", state.studies)
	}
	if state.selectedStudyRow != -1 || state.selectedSeriesRow != -1 || state.selectedInstanceRow != -1 {
		t.Fatalf("selection after delete = study %d series %d instance %d", state.selectedStudyRow, state.selectedSeriesRow, state.selectedInstanceRow)
	}
	if len(state.series) != 0 || len(state.instances) != 0 {
		t.Fatalf("details after delete = series %#v instances %#v", state.series, state.instances)
	}
	if status.Text != "Deleted study 1.2.3.delete (1 object)" {
		t.Fatalf("status = %q", status.Text)
	}
	if !strings.Contains(state.archiveSummary.Text, "No study selected") || !strings.Contains(state.archiveSummary.Text, "1 studies in archive") {
		t.Fatalf("archive summary = %q", state.archiveSummary.Text)
	}
	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != keepStudyUID {
		t.Fatalf("catalog studies after delete = %#v", studies)
	}
}

func importTestArchiveObject(t *testing.T, catalog *archive.Catalog, sourcePath, patientName, patientID, modality, studyUID, seriesUID, sopUID string) {
	t.Helper()
	dataset := object.FromElements([]core.Element{
		testStringElement(core.NewTag(0x0008, 0x0016), core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
		testStringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
		testStringElement(core.NewTag(0x0010, 0x0010), core.VRPN, patientName),
		testStringElement(core.NewTag(0x0010, 0x0020), core.VRLO, patientID),
		testStringElement(core.NewTag(0x0008, 0x0060), core.VRCS, modality),
		testStringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
		testStringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
	}, std.Dictionary)
	report, err := catalog.ImportObject(context.Background(), sourcePath, dataset, transfer.ExplicitVRLittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}
}

func testStringElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: vr},
		Value:  core.StringValue{value},
	}
}

func TestPatientStudySummaryRowsListSamePatientStudies(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260501", StudyDescription: "CT Chest", Modalities: "CT", InstanceCount: 42},
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260531", StudyDescription: "MR Brain", Modalities: "MR", InstanceCount: 8},
			{PatientName: "SMITH^JOHN", PatientID: "P999", StudyDate: "20260530", StudyDescription: "US Abdomen", Modalities: "US", InstanceCount: 3},
		},
	}
	state.selectedStudyRow = 1

	rows := patientStudySummaryRows(state, state.selectedStudyRow)

	if len(rows) != 2 {
		t.Fatalf("patient study rows = %#v, want selected plus same-patient prior study", rows)
	}
	if !rows[0].Selected || rows[0].StudyIndex != 1 || rows[0].Primary != "MR Brain" || rows[0].Modality != "MR" || rows[0].Secondary != "20260531" || rows[0].Images != "8 images" {
		t.Fatalf("selected patient study row = %#v", rows[0])
	}
	if rows[1].Selected || rows[1].StudyIndex != 0 || rows[1].Primary != "CT Chest" || rows[1].Modality != "CT" || rows[1].Secondary != "20260501" || rows[1].Images != "42 images" {
		t.Fatalf("prior patient study row = %#v", rows[1])
	}
}

func TestArchiveSummaryPaneUsesNativePatientStudyList(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260501", StudyDescription: "CT Chest", Modalities: "CT", InstanceCount: 42},
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260531", StudyDescription: "MR Brain", Modalities: "MR", InstanceCount: 8},
		},
		selectedStudyRow: 1,
	}

	_ = newArchiveSummaryPane(nil, widget.NewLabel(""), archiveTables{}, state)
	if state.archivePatientStudyList == nil {
		t.Fatal("archive summary pane should wire native patient study list")
	}
	item := state.archivePatientStudyList.CreateItem()
	studyItem, ok := item.(*archivePatientStudyListItem)
	if !ok {
		t.Fatalf("patient study list item = %T, want *archivePatientStudyListItem", item)
	}

	state.archivePatientStudyList.UpdateItem(widget.ListItemID(0), studyItem)

	if !studyItem.selectionIcon.Visible() || studyItem.selectionIcon.Resource.Name() != theme.NavigateNextIcon().Name() {
		t.Fatalf("selected patient study marker = visible %v resource %#v", studyItem.selectionIcon.Visible(), studyItem.selectionIcon.Resource)
	}
	if studyItem.primary.Text != "MR Brain" {
		t.Fatalf("primary text = %q", studyItem.primary.Text)
	}
	if !studyItem.primary.TextStyle.Bold {
		t.Fatal("primary patient study line should use bold compact typography")
	}
	if studyItem.modality.Text != "MR" {
		t.Fatalf("modality text = %q", studyItem.modality.Text)
	}
	if studyItem.modality.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("modality alignment = %v, want trailing", studyItem.modality.Alignment)
	}
	if studyItem.secondary.Text != "20260531" {
		t.Fatalf("secondary text = %q", studyItem.secondary.Text)
	}
	if !studyItem.secondary.TextStyle.Italic {
		t.Fatal("secondary patient study line should remain visually subdued")
	}
	if studyItem.images.Text != "8 images" {
		t.Fatalf("images text = %q", studyItem.images.Text)
	}
	if studyItem.images.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("images alignment = %v, want trailing", studyItem.images.Alignment)
	}
	if studyItem.metricsSlot.MinSize().Width < archivePatientStudyMetricsSlotWidth {
		t.Fatalf("patient study metrics slot width = %.0f, want at least %.0f", studyItem.metricsSlot.MinSize().Width, archivePatientStudyMetricsSlotWidth)
	}
	if got := color.NRGBAModel.Convert(studyItem.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected patient study fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
	if got := countCanvasRectanglesWithColor(studyItem.Container, tableColumnDividerColor); got < 2 {
		t.Fatalf("patient study row dividers = %d, want right and bottom divider", got)
	}
}

func TestArchivePatientStudyListUsesCompactRowHeights(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260501", StudyDescription: "CT Chest", Modalities: "CT", InstanceCount: 42},
			{PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260531", StudyDescription: "MR Brain", Modalities: "MR", InstanceCount: 8},
		},
		selectedStudyRow: 1,
	}

	list := newArchivePatientStudyList(state)

	for id := range patientStudySummaryRows(state, state.selectedStudyRow) {
		height, ok := listItemHeight(list, id)
		if !ok {
			t.Fatalf("patient study summary row %d should have an explicit compact height", id)
		}
		if height != compactArchivePatientStudyRowHeight {
			t.Fatalf("patient study summary row %d height = %.0f, want %.0f", id, height, compactArchivePatientStudyRowHeight)
		}
	}
}

func TestArchivePatientStudyListSelectionDrivesArchiveStudySelection(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		studies: []archive.Study{
			{StudyInstanceUID: "study-ct", PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260501", StudyDescription: "CT Chest", Modalities: "CT", InstanceCount: 42},
			{StudyInstanceUID: "study-mr", PatientName: "DOE^JANE", PatientID: "P123", StudyDate: "20260531", StudyDescription: "MR Brain", Modalities: "MR", InstanceCount: 8},
		},
		selectedStudyRow:    1,
		selectedSeriesRow:   0,
		selectedInstanceRow: 0,
		series:              []archive.Series{{SeriesInstanceUID: "series-old"}},
		instances:           []archive.Instance{{SOPInstanceUID: "instance-old"}},
	}
	state.archiveRows = archiveBrowserRows(state.studies)
	state.archiveSummary = compactWorkbenchLabel()

	list := newArchivePatientStudyList(state)
	if list.OnSelected == nil {
		t.Fatal("patient study list should handle row selection")
	}

	list.OnSelected(widget.ListItemID(1))

	if state.selectedStudyRow != 0 || state.studies[state.selectedStudyRow].StudyInstanceUID != "study-ct" {
		t.Fatalf("selected study row = %d, want study-ct", state.selectedStudyRow)
	}
	if state.selectedSeriesRow != -1 || state.selectedInstanceRow != -1 || len(state.series) != 0 || len(state.instances) != 0 {
		t.Fatalf("dependent selections not cleared: series row %d instance row %d series %#v instances %#v", state.selectedSeriesRow, state.selectedInstanceRow, state.series, state.instances)
	}
	if !archiveBrowserRowSelected(state.archiveRows[1], state) {
		t.Fatalf("archive rows should highlight the newly selected study: %#v", state.archiveRows)
	}
	if !state.openedArchiveStudyUIDs["study-ct"] {
		t.Fatalf("opened studies = %#v, want study-ct recorded", state.openedArchiveStudyUIDs)
	}
	if !strings.Contains(state.archiveSummary.Text, "CT Chest") {
		t.Fatalf("archive summary = %q, want newly selected study details", state.archiveSummary.Text)
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

func TestArchiveResultSummaryTextIncludesActiveAlbumContext(t *testing.T) {
	state := &uiState{
		selectedArchiveAlbum: archiveAlbumTodayCT,
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", SeriesCount: 2, InstanceCount: 42},
			{PatientName: "SMITH^JOHN", PatientID: "P999", SeriesCount: 1, InstanceCount: 3},
		},
	}

	got := archiveResultSummaryText(state)

	want := "2 patients, 2 studies, 3 series, 45 images - Album: Today CT - Scope: imported today, modality CT"
	if got != want {
		t.Fatalf("archive result summary = %q, want %q", got, want)
	}
}

func TestArchiveResultSummaryTextIncludesSelectedSourceContext(t *testing.T) {
	state := &uiState{
		nodes:           []nodes.Node{{Name: "RADIANT", Host: "192.168.100.26", Port: 11112}},
		selectedNodeRow: 0,
		studies: []archive.Study{
			{PatientName: "DOE^JANE", PatientID: "P123", SeriesCount: 2, InstanceCount: 42},
		},
	}

	got := archiveResultSummaryText(state)

	want := "1 patients, 1 studies, 2 series, 42 images - Source: RADIANT 192.168.100.26:11112"
	if got != want {
		t.Fatalf("archive result summary = %q, want %q", got, want)
	}
}

func TestArchiveSeriesSummaryTextCountsLoadedSeries(t *testing.T) {
	state := &uiState{
		series: []archive.Series{
			{SeriesNumber: "2", SeriesDescription: "Abdomen", Modality: "CT", InstanceCount: 42},
			{Modality: "SR", InstanceCount: 1},
		},
		selectedSeriesRow: 0,
	}

	got := archiveSeriesSummaryText(state)

	want := "2 series, 43 images - Selected: #2 Abdomen CT"
	if got != want {
		t.Fatalf("archive series summary = %q, want %q", got, want)
	}
}

func TestArchiveInstancesSummaryTextCountsLoadedImages(t *testing.T) {
	state := &uiState{
		instances: []archive.Instance{
			{InstanceNumber: "1", SOPInstanceUID: "1.2.3"},
			{InstanceNumber: "2", SOPInstanceUID: "1.2.4"},
			{SOPInstanceUID: "1.2.5"},
		},
		selectedInstanceRow: 1,
	}

	got := archiveInstancesSummaryText(state)

	want := "3 images - Selected: #2 1.2.4"
	if got != want {
		t.Fatalf("archive instances summary = %q, want %q", got, want)
	}
}

func TestRefreshArchiveChromeUpdatesDetailSummaryFooters(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		series: []archive.Series{
			{InstanceCount: 2},
			{InstanceCount: 3},
		},
		instances:               []archive.Instance{{SOPInstanceUID: "1"}, {SOPInstanceUID: "2"}},
		archiveSeriesSummary:    widget.NewLabel("old series"),
		archiveInstancesSummary: widget.NewLabel("old images"),
		selectedSeriesRow:       -1,
		selectedInstanceRow:     -1,
	}

	refreshArchiveChrome(state)

	if state.archiveSeriesSummary.Text != "2 series, 5 images" {
		t.Fatalf("series footer = %q", state.archiveSeriesSummary.Text)
	}
	if state.archiveInstancesSummary.Text != "2 images" {
		t.Fatalf("instances footer = %q", state.archiveInstancesSummary.Text)
	}
}

func TestLabeledTableWithFooterUsesWorkbenchFooterChrome(t *testing.T) {
	fynetest.NewApp()
	footer := widget.NewLabel("2 series, 5 images")
	table := widget.NewTable(
		func() (int, int) { return 1, 1 },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(widget.TableCellID, fyne.CanvasObject) {},
	)

	panel := labeledTableWithFooter("Series", table, footer)

	if findLabelContaining(panel, "2 series, 5 images") == nil {
		t.Fatal("labeled table footer should preserve the summary label")
	}
	if !containsCanvasRectangleColor(panel, archiveHeaderRowColor) {
		t.Fatal("labeled table footer should use dark workbench footer background")
	}
	if got := countCanvasRectanglesWithColor(panel, tableColumnDividerColor); got < 2 {
		t.Fatalf("labeled table footer divider rectangles = %d, want footer chrome dividers", got)
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

func TestSetStudiesPreservesSelectedStudyWhenStillVisible(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{
			{StudyInstanceUID: "study-old", PatientName: "OLDER^PATIENT", PatientID: "P0"},
			{StudyInstanceUID: "study-keep", PatientName: "DOE^JANE", PatientID: "P123"},
			{StudyInstanceUID: "study-drop", PatientName: "SMITH^JOHN", PatientID: "P999"},
		},
		selectedStudyRow:    1,
		selectedSeriesRow:   0,
		selectedInstanceRow: 0,
		series:              []archive.Series{{SeriesInstanceUID: "series-old"}},
		instances:           []archive.Instance{{SOPInstanceUID: "instance-old"}},
	}

	setStudies(state, archiveTables{}, []archive.Study{
		{StudyInstanceUID: "study-new", PatientName: "ALPHA^ANN", PatientID: "P111"},
		{StudyInstanceUID: "study-keep", PatientName: "DOE^JANE", PatientID: "P123"},
	})

	if state.selectedStudyRow != 1 || state.studies[state.selectedStudyRow].StudyInstanceUID != "study-keep" {
		t.Fatalf("selected study row = %d, studies = %#v", state.selectedStudyRow, state.studies)
	}
	if state.selectedSeriesRow != -1 || state.selectedInstanceRow != -1 || len(state.series) != 0 || len(state.instances) != 0 {
		t.Fatalf("details after setStudies = series row %d instance row %d series %#v instances %#v", state.selectedSeriesRow, state.selectedInstanceRow, state.series, state.instances)
	}
	if !archiveBrowserRowSelected(state.archiveRows[3], state) {
		t.Fatalf("visible selected study row should remain highlighted: %#v", state.archiveRows)
	}
}

func TestSetStudiesPrunesObsoleteCollapsedArchiveState(t *testing.T) {
	oldVisible := archive.Study{StudyInstanceUID: "study-keep", PatientName: "DOE^JANE", PatientID: "P123"}
	oldHidden := archive.Study{StudyInstanceUID: "study-drop", PatientName: "SMITH^JOHN", PatientID: "P999"}
	state := &uiState{
		studies: []archive.Study{oldVisible, oldHidden},
		collapsedPatientGroups: map[string]bool{
			archivePatientKey(oldVisible): true,
			archivePatientKey(oldHidden):  true,
			"id:false-value":              false,
		},
		collapsedArchiveStudies: map[string]bool{
			"study-keep":  true,
			"study-drop":  true,
			"study-false": false,
		},
	}

	setStudies(state, archiveTables{}, []archive.Study{oldVisible})

	if len(state.collapsedPatientGroups) != 1 || !state.collapsedPatientGroups[archivePatientKey(oldVisible)] {
		t.Fatalf("collapsed patient groups = %#v, want only visible patient", state.collapsedPatientGroups)
	}
	if len(state.collapsedArchiveStudies) != 1 || !state.collapsedArchiveStudies["study-keep"] {
		t.Fatalf("collapsed archive studies = %#v, want only visible study", state.collapsedArchiveStudies)
	}
	if len(state.archiveRows) != 1 || !state.archiveRows[0].collapsed {
		t.Fatalf("archive rows = %#v, want collapsed visible patient only", state.archiveRows)
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

func TestArchiveTableHeadersIncludeWorkstationDisplayFields(t *testing.T) {
	headers := strings.Join(archiveTableHeaders(), "|")

	for _, want := range []string{"Patient name", "Date of Birth", "Date Acquired", "Date Added", "# im", "# ser", "Institution", "Status", "Comments"} {
		if !strings.Contains(headers, want) {
			t.Fatalf("headers missing %q in %q", want, headers)
		}
	}
	if strings.Contains(headers, "Study UID") {
		t.Fatalf("Study UID should not be a visible Archive column: %q", headers)
	}
}

func TestArchiveTableHeadersHideSeparateTimeAndDescriptionByDefault(t *testing.T) {
	headers := archiveTableHeaders()
	joined := strings.Join(headers, "|")

	for _, hidden := range []string{"Time", "Description"} {
		if strings.Contains(joined, hidden) {
			t.Fatalf("default Archive headers should not include separate %q column: %q", hidden, joined)
		}
	}

	want := []string{"Patient name", "Modality", "# im", "# ser", "Patient ID", "Date of Birth", "Accession", "Date Acquired", "Date Added", "Institution", "Status", "Comments"}
	if strings.Join(headers, "|") != strings.Join(want, "|") {
		t.Fatalf("archive headers = %q, want %q", strings.Join(headers, "|"), strings.Join(want, "|"))
	}
}

func TestArchiveTableHeadersFollowWorkstationPrimaryOrder(t *testing.T) {
	want := []string{"Patient name", "Modality", "# im", "# ser", "Patient ID", "Date of Birth", "Accession", "Date Acquired", "Date Added", "Institution", "Status", "Comments"}

	if got := archiveTableHeaders(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("archive headers = %q, want %q", strings.Join(got, "|"), strings.Join(want, "|"))
	}
}

func TestApplyArchiveSortSortsStudiesAndRebuildsRows(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{
			{StudyInstanceUID: "study-smith", PatientName: "SMITH^JOHN", PatientID: "P2", StudyDescription: "US", ImportedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)},
			{StudyInstanceUID: "study-doe", PatientName: "DOE^JANE", PatientID: "P1", StudyDescription: "CT", ImportedAt: time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)},
			{StudyInstanceUID: "study-alpha", PatientName: "ALPHA^ANN", PatientID: "P0", StudyDescription: "MR", ImportedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
		},
		selectedStudyRow:  1,
		selectedSeriesRow: 0,
	}
	state.archiveRows = archiveBrowserRows(state.studies)

	if !applyArchiveSort(state, archiveStudyTableColumnPatient) {
		t.Fatal("applyArchiveSort returned false for Patient column")
	}
	if got := []string{state.studies[0].PatientName, state.studies[1].PatientName, state.studies[2].PatientName}; strings.Join(got, "|") != "ALPHA^ANN|DOE^JANE|SMITH^JOHN" {
		t.Fatalf("ascending patient order = %v", got)
	}
	if !state.archiveSortActive || state.archiveSortColumn != archiveStudyTableColumnPatient || state.archiveSortDescending {
		t.Fatalf("archive sort state = active %v col %d desc %v", state.archiveSortActive, state.archiveSortColumn, state.archiveSortDescending)
	}
	if state.selectedStudyRow != 1 || state.studies[state.selectedStudyRow].StudyInstanceUID != "study-doe" || state.selectedSeriesRow != -1 {
		t.Fatalf("selected rows = study %d series %d, want selected study-doe and reset series", state.selectedStudyRow, state.selectedSeriesRow)
	}
	if state.archiveRows[0].kind != archiveRowPatient || state.archiveRows[1].studyIndex != 0 {
		t.Fatalf("archive rows not rebuilt after sort: %#v", state.archiveRows[:2])
	}

	if !applyArchiveSort(state, archiveStudyTableColumnPatient) {
		t.Fatal("second applyArchiveSort returned false for Patient column")
	}
	if got := []string{state.studies[0].PatientName, state.studies[1].PatientName, state.studies[2].PatientName}; strings.Join(got, "|") != "SMITH^JOHN|DOE^JANE|ALPHA^ANN" {
		t.Fatalf("descending patient order = %v", got)
	}
	if !state.archiveSortDescending {
		t.Fatal("second sort should toggle to descending")
	}

	if !applyArchiveSort(state, archiveStudyTableColumnAdded) {
		t.Fatal("applyArchiveSort returned false for Added column")
	}
	if got := []string{state.studies[0].PatientName, state.studies[1].PatientName, state.studies[2].PatientName}; strings.Join(got, "|") != "ALPHA^ANN|SMITH^JOHN|DOE^JANE" {
		t.Fatalf("ascending added order = %v", got)
	}
	if state.archiveSortColumn != archiveStudyTableColumnAdded || state.archiveSortDescending {
		t.Fatalf("new archive sort state = col %d desc %v", state.archiveSortColumn, state.archiveSortDescending)
	}
}

func TestApplyArchiveSortPreservesSelectedStudyWhenItMoves(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{
			{StudyInstanceUID: "study-smith", PatientName: "SMITH^JOHN", PatientID: "P2"},
			{StudyInstanceUID: "study-doe", PatientName: "DOE^JANE", PatientID: "P1"},
			{StudyInstanceUID: "study-alpha", PatientName: "ALPHA^ANN", PatientID: "P0"},
		},
		selectedStudyRow:    0,
		selectedSeriesRow:   1,
		selectedInstanceRow: 2,
		series:              []archive.Series{{SeriesInstanceUID: "series-old"}},
		instances:           []archive.Instance{{SOPInstanceUID: "instance-old"}},
	}
	state.archiveRows = archiveBrowserRows(state.studies)

	if !applyArchiveSort(state, archiveStudyTableColumnPatient) {
		t.Fatal("applyArchiveSort returned false for Patient column")
	}

	if state.selectedStudyRow != 2 {
		t.Fatalf("selected study row = %d, want 2 after sorting", state.selectedStudyRow)
	}
	if state.studies[state.selectedStudyRow].StudyInstanceUID != "study-smith" {
		t.Fatalf("selected study UID = %q, want study-smith", state.studies[state.selectedStudyRow].StudyInstanceUID)
	}
	if state.selectedSeriesRow != -1 || state.selectedInstanceRow != -1 || len(state.series) != 0 || len(state.instances) != 0 {
		t.Fatalf("details after sort = series row %d instance row %d series %#v instances %#v", state.selectedSeriesRow, state.selectedInstanceRow, state.series, state.instances)
	}
	if !archiveBrowserRowSelected(state.archiveRows[5], state) {
		t.Fatalf("moved study row should remain selected in archive rows: %#v", state.archiveRows)
	}
}

func TestApplyArchiveSortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		studies: []archive.Study{
			{StudyInstanceUID: "study-smith", PatientName: "SMITH^JOHN", PatientID: "P2"},
			{StudyInstanceUID: "study-doe", PatientName: "DOE^JANE", PatientID: "P1"},
		},
	}
	state.archiveRows = archiveBrowserRows(state.studies)

	if !applyArchiveSort(state, archiveStudyTableColumnPatient) {
		t.Fatal("applyArchiveSort returned false for Patient column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[archiveSortPreferenceKey]
	if !ok {
		t.Fatalf("missing archive sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != archiveStudyTableColumnPatient || pref.Descending {
		t.Fatalf("persisted archive sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedArchiveSortPreferenceSortsLoadedStudiesAndPreservesSelection(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				archiveSortPreferenceKey: {Column: archiveStudyTableColumnPatient, Descending: true},
			},
		},
		studies: []archive.Study{
			{StudyInstanceUID: "study-alpha", PatientName: "ALPHA^ANN", PatientID: "P0"},
			{StudyInstanceUID: "study-smith", PatientName: "SMITH^JOHN", PatientID: "P2"},
			{StudyInstanceUID: "study-doe", PatientName: "DOE^JANE", PatientID: "P1"},
		},
		selectedStudyRow: 2,
	}
	state.archiveRows = archiveBrowserRows(state.studies)

	applySavedArchiveSortPreference(state)

	if got := archivePatientOrder(state.studies); strings.Join(got, "|") != "SMITH^JOHN|DOE^JANE|ALPHA^ANN" {
		t.Fatalf("saved archive sort order = %q", strings.Join(got, "|"))
	}
	if !state.archiveSortActive || state.archiveSortColumn != archiveStudyTableColumnPatient || !state.archiveSortDescending {
		t.Fatalf("saved archive sort state = active %v col %d desc %v", state.archiveSortActive, state.archiveSortColumn, state.archiveSortDescending)
	}
	if state.selectedStudyRow != 1 || state.studies[state.selectedStudyRow].StudyInstanceUID != "study-doe" {
		t.Fatalf("selected study after saved sort = row %d uid %q", state.selectedStudyRow, state.studies[state.selectedStudyRow].StudyInstanceUID)
	}
	if len(state.archiveRows) == 0 || state.archiveRows[0].kind != archiveRowPatient {
		t.Fatalf("archive rows not rebuilt after saved sort: %#v", state.archiveRows)
	}
}

func TestArchiveSortRejectsUnsortableColumns(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{PatientName: "DOE^JANE"}, {PatientName: "ALPHA^ANN"}},
	}

	if applyArchiveSort(state, archiveStudyTableColumnStudyUID) {
		t.Fatal("Study UID column should not be sorted from the main Archive view")
	}
	if state.archiveSortActive {
		t.Fatal("archive sort should remain inactive after unsortable column")
	}
	if state.studies[0].PatientName != "DOE^JANE" {
		t.Fatalf("studies reordered after unsortable sort: %#v", state.studies)
	}
}

func TestArchiveHeaderLabelShowsSortDirection(t *testing.T) {
	state := &uiState{archiveSortActive: true, archiveSortColumn: archiveStudyTableColumnAdded}

	if got := archiveHeaderLabel(state, archiveStudyTableColumnAdded, "Added"); got != "Added ▲" {
		t.Fatalf("ascending header = %q, want Added ▲", got)
	}
	state.archiveSortDescending = true
	if got := archiveHeaderLabel(state, archiveStudyTableColumnAdded, "Added"); got != "Added ▼" {
		t.Fatalf("descending header = %q, want Added ▼", got)
	}
	if got := archiveHeaderLabel(state, archiveStudyTableColumnPatient, "Patient"); got != "Patient" {
		t.Fatalf("inactive header = %q, want Patient", got)
	}
}

func archivePatientOrder(studies []archive.Study) []string {
	out := make([]string, 0, len(studies))
	for _, study := range studies {
		out = append(out, study.PatientName)
	}
	return out
}

func TestArchiveHeaderSelectionMapsVisibleColumnToDataColumn(t *testing.T) {
	fynetest.NewApp()
	state := &uiState{
		studies: []archive.Study{
			{PatientName: "OLDER", ImportedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
			{PatientName: "NEWER", ImportedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)},
		},
	}
	state.archiveRows = archiveBrowserRows(state.studies)
	status := widget.NewLabel("")
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
		instances: widget.NewTable(func() (int, int) { return 1, 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.TableCellID, fyne.CanvasObject) {}),
	}
	wireArchiveTables(nil, status, tables, state)

	dateAddedVisibleColumn := -1
	for index, header := range archiveTableHeaders() {
		if header == "Date Added" {
			dateAddedVisibleColumn = index
			break
		}
	}
	if dateAddedVisibleColumn < 0 {
		t.Fatal("Date Added header not found")
	}

	tables.studies.OnSelected(widget.TableCellID{Row: 0, Col: dateAddedVisibleColumn})

	if state.archiveSortColumn != archiveStudyTableColumnAdded {
		t.Fatalf("archive sort column = %d, want Date Added data column %d", state.archiveSortColumn, archiveStudyTableColumnAdded)
	}
	if status.Text != "Sorted Archive by Date Added" {
		t.Fatalf("status = %q, want Sorted Archive by Date Added", status.Text)
	}
}

func TestApplySeriesSortSortsDetailRowsAndResetsSelection(t *testing.T) {
	state := &uiState{
		series: []archive.Series{
			{SeriesNumber: "10", SeriesDescription: "Late", InstanceCount: 4},
			{SeriesNumber: "2", SeriesDescription: "Early", InstanceCount: 12},
			{SeriesNumber: "7", SeriesDescription: "Middle", InstanceCount: 1},
		},
		instances:           []archive.Instance{{InstanceNumber: "1"}},
		selectedSeriesRow:   2,
		selectedInstanceRow: 0,
	}

	if !applySeriesSort(state, seriesTableColumnNumber) {
		t.Fatal("applySeriesSort returned false for Series # column")
	}
	if got := []string{state.series[0].SeriesNumber, state.series[1].SeriesNumber, state.series[2].SeriesNumber}; strings.Join(got, "|") != "2|7|10" {
		t.Fatalf("ascending series order = %v", got)
	}
	if !state.seriesSortActive || state.seriesSortColumn != seriesTableColumnNumber || state.seriesSortDescending {
		t.Fatalf("series sort state = active %v col %d desc %v", state.seriesSortActive, state.seriesSortColumn, state.seriesSortDescending)
	}
	if state.selectedSeriesRow != -1 || state.selectedInstanceRow != -1 || state.instances != nil {
		t.Fatalf("detail selection after series sort = series %d instance %d instances %#v, want reset", state.selectedSeriesRow, state.selectedInstanceRow, state.instances)
	}

	if !applySeriesSort(state, seriesTableColumnNumber) {
		t.Fatal("second applySeriesSort returned false")
	}
	if got := []string{state.series[0].SeriesNumber, state.series[1].SeriesNumber, state.series[2].SeriesNumber}; strings.Join(got, "|") != "10|7|2" {
		t.Fatalf("descending series order = %v", got)
	}
	if !state.seriesSortDescending {
		t.Fatal("second series sort should toggle to descending")
	}

	if !applySeriesSort(state, seriesTableColumnDescription) {
		t.Fatal("applySeriesSort returned false for Description column")
	}
	if got := []string{state.series[0].SeriesDescription, state.series[1].SeriesDescription, state.series[2].SeriesDescription}; strings.Join(got, "|") != "Early|Late|Middle" {
		t.Fatalf("description series order = %v", got)
	}
	if state.seriesSortColumn != seriesTableColumnDescription || state.seriesSortDescending {
		t.Fatalf("new series sort state = col %d desc %v", state.seriesSortColumn, state.seriesSortDescending)
	}
}

func TestApplySeriesSortRebuildsInlineArchiveRows(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{StudyInstanceUID: "1.2.study", PatientName: "DOE^JANE", SeriesCount: 2}},
		series: []archive.Series{
			{SeriesNumber: "10", SeriesDescription: "Late"},
			{SeriesNumber: "2", SeriesDescription: "Early"},
		},
		archiveSeriesByStudy: map[string][]archive.Series{
			"1.2.study": {
				{SeriesNumber: "10", SeriesDescription: "Late"},
				{SeriesNumber: "2", SeriesDescription: "Early"},
			},
		},
		selectedStudyRow: 0,
	}
	state.archiveRows = archiveBrowserRowsWithInlineSeries(state.studies, nil, state.archiveSeriesByStudy)

	if !applySeriesSort(state, seriesTableColumnNumber) {
		t.Fatal("applySeriesSort returned false")
	}
	if len(state.archiveRows) < 4 || state.archiveRows[2].series.SeriesNumber != "2" || state.archiveRows[3].series.SeriesNumber != "10" {
		t.Fatalf("inline series rows not rebuilt in sorted order: %#v", state.archiveRows)
	}
}

func TestApplySeriesSortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		series: []archive.Series{
			{SeriesNumber: "10", SeriesDescription: "Late"},
			{SeriesNumber: "2", SeriesDescription: "Early"},
		},
	}

	if !applySeriesSort(state, seriesTableColumnNumber) {
		t.Fatal("applySeriesSort returned false for Series # column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[seriesSortPreferenceKey]
	if !ok {
		t.Fatalf("missing series sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != seriesTableColumnNumber || pref.Descending {
		t.Fatalf("persisted series sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedSeriesSortPreferenceSortsLoadedSeries(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				seriesSortPreferenceKey: {Column: seriesTableColumnNumber, Descending: true},
			},
		},
		series: []archive.Series{
			{SeriesNumber: "2", SeriesDescription: "Early"},
			{SeriesNumber: "10", SeriesDescription: "Late"},
			{SeriesNumber: "7", SeriesDescription: "Middle"},
		},
	}

	applySavedSeriesSortPreference(state)

	if got := seriesNumberOrder(state.series); strings.Join(got, "|") != "10|7|2" {
		t.Fatalf("saved series sort order = %q", strings.Join(got, "|"))
	}
	if !state.seriesSortActive || state.seriesSortColumn != seriesTableColumnNumber || !state.seriesSortDescending {
		t.Fatalf("saved series sort state = active %v col %d desc %v", state.seriesSortActive, state.seriesSortColumn, state.seriesSortDescending)
	}
}

func TestApplyInstanceSortSortsDetailRowsAndResetsSelection(t *testing.T) {
	state := &uiState{
		instances: []archive.Instance{
			{InstanceNumber: "10", SOPInstanceUID: "1.2.10"},
			{InstanceNumber: "2", SOPInstanceUID: "1.2.2"},
			{InstanceNumber: "7", SOPInstanceUID: "1.2.7"},
		},
		selectedInstanceRow: 2,
	}

	if !applyInstanceSort(state, instanceTableColumnNumber) {
		t.Fatal("applyInstanceSort returned false for Instance # column")
	}
	if got := []string{state.instances[0].InstanceNumber, state.instances[1].InstanceNumber, state.instances[2].InstanceNumber}; strings.Join(got, "|") != "2|7|10" {
		t.Fatalf("ascending instance order = %v", got)
	}
	if !state.instanceSortActive || state.instanceSortColumn != instanceTableColumnNumber || state.instanceSortDescending {
		t.Fatalf("instance sort state = active %v col %d desc %v", state.instanceSortActive, state.instanceSortColumn, state.instanceSortDescending)
	}
	if state.selectedInstanceRow != -1 {
		t.Fatalf("selected instance row after sort = %d, want reset", state.selectedInstanceRow)
	}

	if !applyInstanceSort(state, instanceTableColumnNumber) {
		t.Fatal("second applyInstanceSort returned false")
	}
	if got := []string{state.instances[0].InstanceNumber, state.instances[1].InstanceNumber, state.instances[2].InstanceNumber}; strings.Join(got, "|") != "10|7|2" {
		t.Fatalf("descending instance order = %v", got)
	}
	if !state.instanceSortDescending {
		t.Fatal("second instance sort should toggle to descending")
	}
}

func TestApplyInstanceSortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		instances: []archive.Instance{
			{InstanceNumber: "10", SOPInstanceUID: "1.2.10"},
			{InstanceNumber: "2", SOPInstanceUID: "1.2.2"},
		},
	}

	if !applyInstanceSort(state, instanceTableColumnNumber) {
		t.Fatal("applyInstanceSort returned false for Instance # column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[instanceSortPreferenceKey]
	if !ok {
		t.Fatalf("missing instance sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != instanceTableColumnNumber || pref.Descending {
		t.Fatalf("persisted instance sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedInstanceSortPreferenceSortsLoadedInstances(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				instanceSortPreferenceKey: {Column: instanceTableColumnNumber, Descending: true},
			},
		},
		instances: []archive.Instance{
			{InstanceNumber: "2", SOPInstanceUID: "1.2.2"},
			{InstanceNumber: "10", SOPInstanceUID: "1.2.10"},
			{InstanceNumber: "7", SOPInstanceUID: "1.2.7"},
		},
	}

	applySavedInstanceSortPreference(state)

	if got := instanceNumberOrder(state.instances); strings.Join(got, "|") != "10|7|2" {
		t.Fatalf("saved instance sort order = %q", strings.Join(got, "|"))
	}
	if !state.instanceSortActive || state.instanceSortColumn != instanceTableColumnNumber || !state.instanceSortDescending {
		t.Fatalf("saved instance sort state = active %v col %d desc %v", state.instanceSortActive, state.instanceSortColumn, state.instanceSortDescending)
	}
}

func TestArchiveDetailHeaderLabelsShowSortDirection(t *testing.T) {
	state := &uiState{
		seriesSortActive:   true,
		seriesSortColumn:   seriesTableColumnDescription,
		instanceSortActive: true,
		instanceSortColumn: instanceTableColumnTransferSyntax,
	}

	if got := seriesHeaderLabel(state, seriesTableColumnDescription, "Description"); got != "Description ▲" {
		t.Fatalf("series ascending header = %q, want Description ▲", got)
	}
	state.seriesSortDescending = true
	if got := seriesHeaderLabel(state, seriesTableColumnDescription, "Description"); got != "Description ▼" {
		t.Fatalf("series descending header = %q, want Description ▼", got)
	}
	if got := seriesHeaderLabel(state, seriesTableColumnNumber, "Series #"); got != "Series #" {
		t.Fatalf("inactive series header = %q, want Series #", got)
	}

	if got := instanceHeaderLabel(state, instanceTableColumnTransferSyntax, "Transfer Syntax"); got != "Transfer Syntax ▲" {
		t.Fatalf("instance ascending header = %q, want Transfer Syntax ▲", got)
	}
	state.instanceSortDescending = true
	if got := instanceHeaderLabel(state, instanceTableColumnTransferSyntax, "Transfer Syntax"); got != "Transfer Syntax ▼" {
		t.Fatalf("instance descending header = %q, want Transfer Syntax ▼", got)
	}
	if got := instanceHeaderLabel(state, instanceTableColumnNumber, "Instance #"); got != "Instance #" {
		t.Fatalf("inactive instance header = %q, want Instance #", got)
	}
}

func seriesNumberOrder(seriesList []archive.Series) []string {
	out := make([]string, 0, len(seriesList))
	for _, series := range seriesList {
		out = append(out, series.SeriesNumber)
	}
	return out
}

func instanceNumberOrder(instances []archive.Instance) []string {
	out := make([]string, 0, len(instances))
	for _, instance := range instances {
		out = append(out, instance.InstanceNumber)
	}
	return out
}

func TestStudyCellShowsWorkstationDisplayFields(t *testing.T) {
	study := archive.Study{
		PatientBirthDate: "19700102",
		InstitutionName:  "General Hospital",
		StudyDate:        "20260605",
		StudyTime:        "134501",
		Status:           "Reviewed",
		Comments:         "Discuss with surgeon",
		ImportedAt:       time.Date(2026, 6, 4, 13, 45, 0, 0, time.UTC),
	}

	if got := studyCell(study, archiveStudyTableColumnDOB); got != "19700102" {
		t.Fatalf("DOB cell = %q, want 19700102", got)
	}
	if got := studyCell(study, archiveStudyTableColumnStudyDate); got != "20260605 13:45:01" {
		t.Fatalf("date acquired cell = %q, want composed date/time", got)
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
	if got := studyCell(study, archiveStudyTableColumnStatus); got != "✓ Reviewed" {
		t.Fatalf("status cell = %q, want ✓ Reviewed", got)
	}
	if got := studyCell(study, archiveStudyTableColumnComments); got != "Discuss with surgeon" {
		t.Fatalf("comments cell = %q, want Discuss with surgeon", got)
	}
}

func TestStudyStatusCellShowsCompactVisualMarkers(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{name: "blank", status: "  ", want: ""},
		{name: "reviewed", status: "Reviewed", want: "✓ Reviewed"},
		{name: "interesting", status: "Interesting", want: "★ Interesting"},
		{name: "follow up", status: "Follow-up", want: "↪ Follow-up"},
		{name: "teaching", status: "Teaching", want: "▣ Teaching"},
		{name: "problem", status: "Problem", want: "⚠ Problem"},
		{name: "custom", status: "Urgent review", want: "• Urgent review"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := studyCell(archive.Study{Status: tt.status}, archiveStudyTableColumnStatus)
			if got != tt.want {
				t.Fatalf("status cell = %q, want %q", got, tt.want)
			}
		})
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

func TestArchiveBrowserRowsIncludeLoadedInstancesBelowSeries(t *testing.T) {
	studies := []archive.Study{{
		StudyInstanceUID: "1.2.3",
		StudyDescription: "CT Abdomen",
		SeriesCount:      1,
		InstanceCount:    2,
	}}
	series := archive.Series{SeriesInstanceUID: "9.8.7", SeriesNumber: "4", Modality: "CT", InstanceCount: 2}
	instances := []archive.Instance{
		{SeriesInstanceUID: "9.8.7", InstanceNumber: "1", Modality: "CT", SOPInstanceUID: "1.2.image.1"},
		{SeriesInstanceUID: "9.8.7", InstanceNumber: "2", Modality: "CT", SOPInstanceUID: "1.2.image.2"},
	}

	rows := archiveBrowserRowsWithInlineSeriesAndInstances(studies, nil, map[string][]archive.Series{"1.2.3": {series}}, map[string][]archive.Instance{"9.8.7": instances})

	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want patient, study, series, and 2 image rows", len(rows))
	}
	if rows[3].kind != archiveRowInstance || rows[3].studyIndex != 0 || rows[3].seriesIndex != 0 || rows[3].instanceIndex != 0 {
		t.Fatalf("first image row = %#v", rows[3])
	}
	if rows[4].kind != archiveRowInstance || rows[4].instanceIndex != 1 {
		t.Fatalf("second image row = %#v", rows[4])
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnPatient); got != "      ▾ Series 4" {
		t.Fatalf("loaded series label = %q, want expanded disclosure affordance", got)
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

func TestToggleArchiveSeriesImagesCollapsesAndExpandsLoadedRows(t *testing.T) {
	state := &uiState{
		studies: []archive.Study{{
			StudyInstanceUID: "1.2.3",
			StudyDescription: "CT Abdomen",
			SeriesCount:      1,
			InstanceCount:    2,
		}},
		archiveSeriesByStudy: map[string][]archive.Series{
			"1.2.3": {{
				SeriesInstanceUID: "9.8.7",
				SeriesNumber:      "4",
				Modality:          "CT",
				InstanceCount:     2,
			}},
		},
		archiveInstancesBySeries: map[string][]archive.Instance{
			"9.8.7": {
				{SeriesInstanceUID: "9.8.7", InstanceNumber: "1", SOPInstanceUID: "1.2.image.1"},
				{SeriesInstanceUID: "9.8.7", InstanceNumber: "2", SOPInstanceUID: "1.2.image.2"},
			},
		},
	}
	state.archiveRows = archiveBrowserRowsForState(state)

	if !toggleArchiveSeriesImages(state, state.archiveRows[2]) {
		t.Fatal("first toggle returned false, want true")
	}
	if !state.collapsedArchiveSeries["9.8.7"] || len(state.archiveRows) != 3 {
		t.Fatalf("collapsed state = %#v rows=%#v", state.collapsedArchiveSeries, state.archiveRows)
	}
	if got := archiveBrowserCell(state.archiveRows[2], state.studies, archiveStudyTableColumnPatient); got != "      ▸ Series 4" {
		t.Fatalf("collapsed series cell = %q, want collapsed disclosure", got)
	}
	if !toggleArchiveSeriesImages(state, state.archiveRows[2]) {
		t.Fatal("second toggle returned false, want true")
	}
	if state.collapsedArchiveSeries["9.8.7"] || len(state.archiveRows) != 5 || state.archiveRows[3].kind != archiveRowInstance {
		t.Fatalf("expanded state = %#v rows=%#v", state.collapsedArchiveSeries, state.archiveRows)
	}
}

func TestArchiveBrowserCellRendersInlineSeriesRows(t *testing.T) {
	studies := []archive.Study{{StudyInstanceUID: "1.2.3", StudyDescription: "CT Abdomen", SeriesCount: 1}}
	series := archive.Series{
		SeriesNumber:      "4",
		Modality:          "CT",
		SeriesDescription: "Portal Venous",
		SeriesDate:        "20260605",
		SeriesTime:        "140102",
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
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnStudyDate); got != "20260605 14:01:02" {
		t.Fatalf("series date cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnTime); got != "14:01:02" {
		t.Fatalf("series time cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnInstances); got != "40" {
		t.Fatalf("series instance count cell = %q", got)
	}
	if got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnStudyUID); got != "9.8.7" {
		t.Fatalf("series UID cell = %q", got)
	}
}

func TestArchiveBrowserCellRendersInlineInstanceRows(t *testing.T) {
	studies := []archive.Study{{StudyInstanceUID: "1.2.3", StudyDescription: "CT Abdomen", SeriesCount: 1, InstanceCount: 1}}
	series := archive.Series{SeriesInstanceUID: "9.8.7", SeriesNumber: "4", Modality: "CT", InstanceCount: 1}
	instance := archive.Instance{
		SeriesInstanceUID: "9.8.7",
		InstanceNumber:    "12",
		Modality:          "CT",
		SOPInstanceUID:    "1.2.840.technical.image.uid",
	}
	rows := archiveBrowserRowsWithInlineSeriesAndInstances(studies, nil, map[string][]archive.Series{"1.2.3": {series}}, map[string][]archive.Instance{"9.8.7": {instance}})

	got := archiveBrowserCell(rows[3], studies, archiveStudyTableColumnPatient)
	if got != "          • Image 12" {
		t.Fatalf("inline image label = %q, want leaf marker and image number", got)
	}
	if strings.Contains(got, instance.SOPInstanceUID) {
		t.Fatalf("visible image label leaked SOP Instance UID: %q", got)
	}
	if got := archiveBrowserCell(rows[3], studies, archiveStudyTableColumnModality); got != "CT" {
		t.Fatalf("inline image modality = %q, want CT", got)
	}
	if got := archiveBrowserCell(rows[3], studies, archiveStudyTableColumnInstances); got != "1" {
		t.Fatalf("inline image count = %q, want 1", got)
	}
	if got := archiveBrowserCell(rows[3], studies, archiveStudyTableColumnStudyUID); got != instance.SOPInstanceUID {
		t.Fatalf("inline image technical UID cell = %q, want SOP Instance UID", got)
	}
}

func TestArchiveBrowserCellDoesNotUseSeriesUIDAsVisibleInlineLabel(t *testing.T) {
	studies := []archive.Study{{StudyInstanceUID: "1.2.3", StudyDescription: "CT Abdomen", SeriesCount: 1}}
	series := archive.Series{
		SeriesNumber:      "7",
		Modality:          "MR",
		SeriesInstanceUID: "1.2.840.technical.series.uid",
	}
	rows := archiveBrowserRowsWithInlineSeries(studies, nil, map[string][]archive.Series{"1.2.3": {series}})

	got := archiveBrowserCell(rows[2], studies, archiveStudyTableColumnPatient)
	if got != "      Series 7" {
		t.Fatalf("series fallback label = %q, want compact non-UID label", got)
	}
	if strings.Contains(got, series.SeriesInstanceUID) {
		t.Fatalf("visible series label leaked UID: %q", got)
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

	applyArchiveTableCell(cell, 4, "          Image 12", archiveBrowserRow{kind: archiveRowInstance}, false, false)
	instanceFill := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA)
	if instanceFill != archiveInstanceRowColor {
		t.Fatalf("image fill = %#v, want %#v", instanceFill, archiveInstanceRowColor)
	}
	if instanceFill == archiveSeriesRowColor {
		t.Fatalf("image fill should be distinct from series fill %#v", archiveSeriesRowColor)
	}
	if !cell.label.TextStyle.Italic {
		t.Fatal("inline image label should use subdued italic typography")
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

func TestArchiveStatusTableCellUsesNativeStatusDot(t *testing.T) {
	cell := newArchiveTableCell()

	applyArchiveTableCellWithColumn(cell, 2, archiveStudyTableColumnStatus, "★ Interesting", archiveBrowserRow{kind: archiveRowStudy}, false, false)

	if !cell.statusDotBox.Visible() {
		t.Fatal("archive status cell should show native status dot")
	}
	if got := color.NRGBAModel.Convert(cell.statusDot.FillColor).(color.NRGBA); got != studyStatusInterestingColor {
		t.Fatalf("archive status dot color = %#v, want %#v", got, studyStatusInterestingColor)
	}

	applyArchiveTableCellWithColumn(cell, 2, archiveStudyTableColumnComments, "Discuss with surgeon", archiveBrowserRow{kind: archiveRowStudy}, false, false)
	if cell.statusDotBox.Visible() {
		t.Fatal("archive non-status cells should hide native status dot")
	}
}

func TestArchiveStatusTableCellUsesSubtleStatusChipFill(t *testing.T) {
	cell := newArchiveTableCell()

	applyArchiveTableCellWithColumn(cell, 2, archiveStudyTableColumnStatus, "★ Interesting", archiveBrowserRow{kind: archiveRowStudy}, false, false)

	want := color.NRGBA{R: 60, G: 49, B: 28, A: 255}
	if got := countCanvasRectanglesWithColor(cell.Container, want); got == 0 {
		t.Fatalf("archive status cell should include subtle status chip fill %#v", want)
	}

	applyArchiveTableCellWithColumn(cell, 2, archiveStudyTableColumnComments, "Discuss with surgeon", archiveBrowserRow{kind: archiveRowStudy}, false, false)
	if got := countCanvasRectanglesWithColor(cell.Container, want); got != 0 {
		t.Fatalf("archive comments cell kept status chip fill count %d", got)
	}
}

func TestArchiveSelectedStatusTableCellPreservesSelectionFill(t *testing.T) {
	cell := newArchiveTableCell()

	applyArchiveTableCellWithColumn(cell, 2, archiveStudyTableColumnStatus, "★ Interesting", archiveBrowserRow{kind: archiveRowStudy}, false, true)

	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected archive status fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
	if got := countCanvasRectanglesWithColor(cell.Container, studyStatusInterestingChipColor); got != 0 {
		t.Fatalf("selected archive status cell should not cover selection with status chip, got %d chip fills", got)
	}
}

func TestWorkstationTableCellsIncludeColumnDividers(t *testing.T) {
	tests := []struct {
		name string
		cell fyne.CanvasObject
	}{
		{name: "archive", cell: newArchiveTableCell().Container},
		{name: "query", cell: newQueryTableCell().Container},
		{name: "network", cell: newNodeTableCell().Container},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countCanvasRectangles(tt.cell); got < 2 {
				t.Fatalf("%s cell has %d rectangles, want background plus explicit divider", tt.name, got)
			}
		})
	}
}

func TestWorkstationTableCellsIncludeRowAndColumnDividers(t *testing.T) {
	tests := []struct {
		name string
		cell fyne.CanvasObject
	}{
		{name: "archive", cell: newArchiveTableCell().Container},
		{name: "query", cell: newQueryTableCell().Container},
		{name: "network", cell: newNodeTableCell().Container},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countCanvasRectangles(tt.cell); got < 3 {
				t.Fatalf("%s cell has %d rectangles, want background plus row and column dividers", tt.name, got)
			}
		})
	}
}

func TestArchiveDetailTablesUseWorkstationCellsAndSelectionStyling(t *testing.T) {
	state := &uiState{
		series: []archive.Series{{
			SeriesNumber:      "4",
			Modality:          "CT",
			SeriesDescription: "Portal Venous",
			InstanceCount:     40,
			SeriesInstanceUID: "9.8.7",
		}},
		instances: []archive.Instance{{
			InstanceNumber: "7",
			Modality:       "CT",
			SOPInstanceUID: "1.2.3",
		}},
	}

	seriesTable := newSeriesTable(state)
	seriesObj := seriesTable.CreateCell()
	seriesCell, ok := seriesObj.(*archiveTableCell)
	if !ok {
		t.Fatalf("series table cell = %T, want *archiveTableCell", seriesObj)
	}
	seriesTable.UpdateCell(widget.TableCellID{Row: 0, Col: 0}, seriesCell)
	if !seriesCell.label.TextStyle.Bold {
		t.Fatal("series header should use bold workstation styling")
	}
	state.selectedSeriesRow = 0
	seriesTable.UpdateCell(widget.TableCellID{Row: 1, Col: 0}, seriesCell)
	if seriesCell.label.Text != "4" {
		t.Fatalf("series cell text = %q, want 4", seriesCell.label.Text)
	}
	if got := color.NRGBAModel.Convert(seriesCell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected series fill = %#v, want %#v", got, archiveSelectedRowColor)
	}

	instanceTable := newInstanceTable(state)
	instanceObj := instanceTable.CreateCell()
	instanceCell, ok := instanceObj.(*archiveTableCell)
	if !ok {
		t.Fatalf("instance table cell = %T, want *archiveTableCell", instanceObj)
	}
	instanceTable.UpdateCell(widget.TableCellID{Row: 0, Col: 0}, instanceCell)
	if !instanceCell.label.TextStyle.Bold {
		t.Fatal("instance header should use bold workstation styling")
	}
	state.selectedInstanceRow = 0
	instanceTable.UpdateCell(widget.TableCellID{Row: 1, Col: 0}, instanceCell)
	if instanceCell.label.Text != "7" {
		t.Fatalf("instance cell text = %q, want 7", instanceCell.label.Text)
	}
	if got := color.NRGBAModel.Convert(instanceCell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected instance fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
}

func TestUtilityTablesUseWorkstationCells(t *testing.T) {
	state := &uiState{
		selectedOperationRow: -1,
		elements:             []dicominspect.ElementSummary{{Tag: "(0010,0010)", Keyword: "PatientName"}},
		operations: []operations.Summary{{
			Kind:   operations.KindImport,
			Status: operations.StatusSuccess,
		}},
	}

	for _, tt := range []struct {
		name  string
		table *widget.Table
	}{
		{name: "element", table: newElementTable(state)},
		{name: "task", table: newTaskTable(state)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := tt.table.CreateCell()
			cell, ok := obj.(*archiveTableCell)
			if !ok {
				t.Fatalf("%s table cell = %T, want *archiveTableCell", tt.name, obj)
			}
			tt.table.UpdateCell(widget.TableCellID{Row: 0, Col: 0}, cell)
			if !cell.label.TextStyle.Bold {
				t.Fatalf("%s header should use bold workstation styling", tt.name)
			}
			tt.table.UpdateCell(widget.TableCellID{Row: 1, Col: 0}, cell)
			if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveOddRowColor {
				t.Fatalf("%s data fill = %#v, want %#v", tt.name, got, archiveOddRowColor)
			}
			if countCanvasRectangles(cell.Container) < 2 {
				t.Fatalf("%s table cell should include explicit divider", tt.name)
			}
		})
	}
}

func TestApplyElementSortSortsAndTogglesDirection(t *testing.T) {
	state := &uiState{
		elements: []dicominspect.ElementSummary{
			{Tag: "(0020,000D)", Keyword: "StudyInstanceUID"},
			{Tag: "(0010,0010)", Keyword: "PatientName"},
			{Tag: "(0008,0060)", Keyword: "Modality"},
		},
	}

	if !applyElementSort(state, elementTableColumnKeyword) {
		t.Fatal("applyElementSort returned false for Keyword column")
	}
	if got := elementKeywordOrder(state.elements); strings.Join(got, "|") != "Modality|PatientName|StudyInstanceUID" {
		t.Fatalf("ascending keyword order = %q", strings.Join(got, "|"))
	}
	if !state.elementSortActive || state.elementSortColumn != elementTableColumnKeyword || state.elementSortDescending {
		t.Fatalf("sort state after ascending = active %v col %d desc %v", state.elementSortActive, state.elementSortColumn, state.elementSortDescending)
	}

	if !applyElementSort(state, elementTableColumnKeyword) {
		t.Fatal("second applyElementSort returned false for Keyword column")
	}
	if got := elementKeywordOrder(state.elements); strings.Join(got, "|") != "StudyInstanceUID|PatientName|Modality" {
		t.Fatalf("descending keyword order = %q", strings.Join(got, "|"))
	}
	if !state.elementSortDescending {
		t.Fatal("second sort should toggle descending")
	}
}

func TestElementHeaderLabelShowsSortDirection(t *testing.T) {
	state := &uiState{elementSortActive: true, elementSortColumn: elementTableColumnTag}

	if got := elementHeaderLabel(state, elementTableColumnTag, "Tag"); got != "Tag ▲" {
		t.Fatalf("ascending header = %q, want Tag ▲", got)
	}
	state.elementSortDescending = true
	if got := elementHeaderLabel(state, elementTableColumnTag, "Tag"); got != "Tag ▼" {
		t.Fatalf("descending header = %q, want Tag ▼", got)
	}
	if got := elementHeaderLabel(state, elementTableColumnKeyword, "Keyword"); got != "Keyword" {
		t.Fatalf("inactive header = %q, want Keyword", got)
	}
}

func TestApplyElementSortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		elements: []dicominspect.ElementSummary{
			{Tag: "(0020,000D)", Keyword: "StudyInstanceUID"},
			{Tag: "(0010,0010)", Keyword: "PatientName"},
		},
	}

	if !applyElementSort(state, elementTableColumnKeyword) {
		t.Fatal("applyElementSort returned false for Keyword column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[elementSortPreferenceKey]
	if !ok {
		t.Fatalf("missing element sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != elementTableColumnKeyword || pref.Descending {
		t.Fatalf("persisted element sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedElementSortPreferenceSortsElementsAfterInspection(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				elementSortPreferenceKey: {Column: elementTableColumnKeyword, Descending: true},
			},
		},
		elements: []dicominspect.ElementSummary{
			{Tag: "(0008,0060)", Keyword: "Modality"},
			{Tag: "(0010,0010)", Keyword: "PatientName"},
			{Tag: "(0020,000D)", Keyword: "StudyInstanceUID"},
		},
	}

	applySavedElementSortPreference(state)

	if got := elementKeywordOrder(state.elements); strings.Join(got, "|") != "StudyInstanceUID|PatientName|Modality" {
		t.Fatalf("saved element sort order = %q", strings.Join(got, "|"))
	}
	if !state.elementSortActive || state.elementSortColumn != elementTableColumnKeyword || !state.elementSortDescending {
		t.Fatalf("saved element sort state = active %v col %d desc %v", state.elementSortActive, state.elementSortColumn, state.elementSortDescending)
	}
}

func elementKeywordOrder(elements []dicominspect.ElementSummary) []string {
	out := make([]string, 0, len(elements))
	for _, element := range elements {
		out = append(out, element.Keyword)
	}
	return out
}

func TestApplyTaskSortSortsAndTogglesDirection(t *testing.T) {
	state := &uiState{
		selectedOperationRow: 1,
		operations: []operations.Summary{
			{Kind: operations.KindSendStore, Status: operations.StatusSuccess, DurationMS: 3000},
			{Kind: operations.KindImport, Status: operations.StatusWarning, DurationMS: 1000},
			{Kind: operations.KindQueryFind, Status: operations.StatusFailure, DurationMS: 2000},
		},
	}

	if !applyTaskSort(state, taskTableColumnDuration) {
		t.Fatal("applyTaskSort returned false for Duration column")
	}
	if got := taskDurationOrder(state.operations); strings.Join(got, "|") != "1s|2s|3s" {
		t.Fatalf("ascending duration order = %q", strings.Join(got, "|"))
	}
	if state.selectedOperationRow != 0 {
		t.Fatalf("selected operation row after ascending sort = %d, want 0", state.selectedOperationRow)
	}
	if !state.taskSortActive || state.taskSortColumn != taskTableColumnDuration || state.taskSortDescending {
		t.Fatalf("sort state after ascending = active %v col %d desc %v", state.taskSortActive, state.taskSortColumn, state.taskSortDescending)
	}

	if !applyTaskSort(state, taskTableColumnDuration) {
		t.Fatal("second applyTaskSort returned false for Duration column")
	}
	if got := taskDurationOrder(state.operations); strings.Join(got, "|") != "3s|2s|1s" {
		t.Fatalf("descending duration order = %q", strings.Join(got, "|"))
	}
	if state.selectedOperationRow != 2 {
		t.Fatalf("selected operation row after descending sort = %d, want 2", state.selectedOperationRow)
	}
	if !state.taskSortDescending {
		t.Fatal("second sort should toggle descending")
	}
}

func TestTaskHeaderLabelShowsSortDirection(t *testing.T) {
	state := &uiState{taskSortActive: true, taskSortColumn: taskTableColumnStatus}

	if got := taskHeaderLabel(state, taskTableColumnStatus, "Status"); got != "Status ▲" {
		t.Fatalf("ascending header = %q, want Status ▲", got)
	}
	state.taskSortDescending = true
	if got := taskHeaderLabel(state, taskTableColumnStatus, "Status"); got != "Status ▼" {
		t.Fatalf("descending header = %q, want Status ▼", got)
	}
	if got := taskHeaderLabel(state, taskTableColumnKind, "Kind"); got != "Kind" {
		t.Fatalf("inactive header = %q, want Kind", got)
	}
}

func TestApplyTaskSortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		operations: []operations.Summary{
			{Kind: operations.KindSendStore, Status: operations.StatusSuccess, DurationMS: 3000},
			{Kind: operations.KindImport, Status: operations.StatusWarning, DurationMS: 1000},
		},
	}

	if !applyTaskSort(state, taskTableColumnDuration) {
		t.Fatal("applyTaskSort returned false for Duration column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[taskSortPreferenceKey]
	if !ok {
		t.Fatalf("missing task sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != taskTableColumnDuration || pref.Descending {
		t.Fatalf("persisted task sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedTaskSortPreferenceSortsTasksOnStartup(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				taskSortPreferenceKey: {Column: taskTableColumnDuration, Descending: true},
			},
		},
		operations: []operations.Summary{
			{Kind: operations.KindImport, Status: operations.StatusWarning, DurationMS: 1000},
			{Kind: operations.KindQueryFind, Status: operations.StatusFailure, DurationMS: 2000},
			{Kind: operations.KindSendStore, Status: operations.StatusSuccess, DurationMS: 3000},
		},
	}

	applySavedTaskSortPreference(state)

	if got := taskDurationOrder(state.operations); strings.Join(got, "|") != "3s|2s|1s" {
		t.Fatalf("saved task sort order = %q", strings.Join(got, "|"))
	}
	if !state.taskSortActive || state.taskSortColumn != taskTableColumnDuration || !state.taskSortDescending {
		t.Fatalf("saved task sort state = active %v col %d desc %v", state.taskSortActive, state.taskSortColumn, state.taskSortDescending)
	}
}

func taskDurationOrder(summaries []operations.Summary) []string {
	out := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, (time.Duration(summary.DurationMS) * time.Millisecond).String())
	}
	return out
}

func countCanvasRectangles(obj fyne.CanvasObject) int {
	count := 0
	if _, ok := obj.(*canvas.Rectangle); ok {
		count++
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			count += countCanvasRectangles(child)
		}
	}
	return count
}

func containsCanvasRectangleColor(obj fyne.CanvasObject, want color.NRGBA) bool {
	if rect, ok := obj.(*canvas.Rectangle); ok {
		if got := color.NRGBAModel.Convert(rect.FillColor).(color.NRGBA); got == want {
			return true
		}
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if containsCanvasRectangleColor(child, want) {
				return true
			}
		}
	}
	return false
}

func countCanvasRectanglesWithColor(obj fyne.CanvasObject, want color.NRGBA) int {
	count := 0
	if rect, ok := obj.(*canvas.Rectangle); ok {
		if got := color.NRGBAModel.Convert(rect.FillColor).(color.NRGBA); got == want {
			count++
		}
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			count += countCanvasRectanglesWithColor(child, want)
		}
	}
	return count
}

func listItemHeight(list *widget.List, id int) (float32, bool) {
	if list == nil {
		return 0, false
	}
	itemHeights := reflect.ValueOf(list).Elem().FieldByName("itemHeights")
	if itemHeights.IsNil() {
		return 0, false
	}
	height := itemHeights.MapIndex(reflect.ValueOf(id))
	if !height.IsValid() {
		return 0, false
	}
	return float32(height.Float()), true
}

func TestWorkstationTableCellsUseCompactPadding(t *testing.T) {
	tests := []struct {
		name string
		cell fyne.CanvasObject
	}{
		{name: "archive", cell: newArchiveTableCell().Container},
		{name: "query", cell: newQueryTableCell().Container},
		{name: "network", cell: newNodeTableCell().Container},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !hasCompactPaddedLayout(tt.cell) {
				t.Fatalf("%s cell should use compact custom padding", tt.name)
			}
		})
	}
}

func hasCompactPaddedLayout(obj fyne.CanvasObject) bool {
	c, ok := obj.(*fyne.Container)
	if !ok {
		return false
	}
	if padding, ok := c.Layout.(layout.CustomPaddedLayout); ok {
		if padding.TopPadding <= 1 && padding.BottomPadding <= 1 && padding.LeftPadding <= 4 && padding.RightPadding <= 4 {
			return true
		}
	}
	for _, child := range c.Objects {
		if hasCompactPaddedLayout(child) {
			return true
		}
	}
	return false
}

func TestQueryTableCellUsesHeaderAndStripedStyling(t *testing.T) {
	cell := newQueryTableCell()

	applyQueryTableCell(cell, 0, 0, "Patient", true, false, false, 0, "")

	if cell.label.Text != "Patient" {
		t.Fatalf("header text = %q", cell.label.Text)
	}
	if !cell.label.TextStyle.Bold {
		t.Fatal("query header should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveHeaderRowColor {
		t.Fatalf("header fill = %#v, want %#v", got, archiveHeaderRowColor)
	}

	applyQueryTableCell(cell, 1, 2, "DOE^JANE", false, false, false, 0, "")
	if cell.label.TextStyle.Bold {
		t.Fatal("query row should not be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveOddRowColor {
		t.Fatalf("query odd row fill = %#v, want %#v", got, archiveOddRowColor)
	}
}

func TestQueryRetrieveTableCellUsesIconAction(t *testing.T) {
	cell := newQueryTableCell()

	applyQueryTableCell(cell, 1, queryRetrieveColumn, "", false, false, true, 0, "")

	if cell.label.Visible() {
		t.Fatal("retrieve action cell should hide text label")
	}
	if !cell.retrieveButton.Visible() {
		t.Fatal("retrieve action cell should show native icon button")
	}
	if cell.retrieveButton.Icon == nil || cell.retrieveButton.Icon.Name() != theme.DownloadIcon().Name() {
		t.Fatalf("retrieve button icon = %#v, want download icon", cell.retrieveButton.Icon)
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != queryRetrieveActionRowColor {
		t.Fatalf("retrieve fill = %#v, want %#v", got, queryRetrieveActionRowColor)
	}
}

func TestQuerySelectedTableCellUsesSelectionStyling(t *testing.T) {
	cell := newQueryTableCell()

	applyQueryTableCell(cell, 2, 2, "DOE^JANE", false, true, false, 0, "")

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

func TestQueryGroupDisclosureCellUsesBoldText(t *testing.T) {
	cell := newQueryTableCell()

	applyQueryTableCell(cell, 1, queryTableColumnPatient, "▾ DOE^JANE (3 matches)", false, false, false, 0, "")
	if !cell.label.TextStyle.Bold {
		t.Fatal("expanded patient group disclosure should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archivePatientRowColor {
		t.Fatalf("expanded patient group fill = %#v, want %#v", got, archivePatientRowColor)
	}

	applyQueryTableCell(cell, 2, queryTableColumnPatient, "    ▸ CT Abdomen (2 items)", false, false, false, 0, "")
	if !cell.label.TextStyle.Bold {
		t.Fatal("nested collapsed study group disclosure should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archivePatientRowColor {
		t.Fatalf("nested study group fill = %#v, want %#v", got, archivePatientRowColor)
	}

	applyQueryTableCell(cell, 3, queryTableColumnPatient, "    2", false, false, false, 0, "")
	if cell.label.TextStyle.Bold {
		t.Fatal("ordinary indented child rows should not be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveOddRowColor {
		t.Fatalf("ordinary child fill = %#v, want %#v", got, archiveOddRowColor)
	}
}

func TestQueryTableHeadersIncludeRetrieveIndicator(t *testing.T) {
	headers := queryTableHeaders()

	if len(headers) < 2 || headers[1] != "" {
		t.Fatalf("query headers = %q, want blank retrieve action column as second column", strings.Join(headers, "|"))
	}
}

func TestQueryTableHeadersIncludeLocalStateIndicator(t *testing.T) {
	headers := queryTableHeaders()

	if len(headers) < 12 || headers[11] != "Local" {
		t.Fatalf("query headers = %q, want Local after Server Comments", strings.Join(headers, "|"))
	}
}

func TestQueryTableHeadersFollowWorkstationPrimaryOrderAndHideTechnicalUIDs(t *testing.T) {
	want := []string{"Patient Name", "", "Modality", "# im", "Date", "Time", "Description", "Patient ID", "Date of Birth", "Local Comments", "Server Comments", "Local", "Accession", "Referrer", "Institution", "Study Status", "Series #", "Instance #", "Source", "Status"}

	if got := queryTableHeaders(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("query headers = %q, want %q", strings.Join(got, "|"), strings.Join(want, "|"))
	}
	for _, hidden := range []string{"Level", "Study UID", "Series UID", "SOP Class", "SOP UID"} {
		if slices.Contains(queryTableHeaders(), hidden) {
			t.Fatalf("query headers should hide technical column %q by default: %q", hidden, strings.Join(queryTableHeaders(), "|"))
		}
	}
}

func TestQueryTableHeadersIncludeWorkstationDisplayFields(t *testing.T) {
	headers := strings.Join(queryTableHeaders(), "|")

	for _, want := range []string{"Date of Birth", "Time", "# im", "Referrer", "Institution", "Source"} {
		if !strings.Contains(headers, want) {
			t.Fatalf("headers missing %q in %q", want, headers)
		}
	}
}

func TestQueryRowCellMovesHierarchyIntoPatientColumn(t *testing.T) {
	patient := queryTableRow{
		kind:     queryTableRowPatientGroup,
		match:    query.Match{PatientName: "DOE^JANE"},
		expanded: true,
	}
	collapsedStudy := queryTableRow{
		kind:     queryTableRowStudyGroup,
		match:    query.Match{StudyDescription: "CT Abdomen", StudyDate: "20260605", StudyInstanceUID: "1.2.3"},
		depth:    1,
		expanded: false,
	}

	if got := queryRowCell(patient, queryTableColumnPatient); got != "▾ DOE^JANE" {
		t.Fatalf("patient group cell = %q, want hierarchy in Patient column", got)
	}
	if got := queryRowCell(collapsedStudy, queryTableColumnPatient); got != "    ▸ CT Abdomen" {
		t.Fatalf("study group cell = %q, want hierarchy in Patient column", got)
	}
}

func TestQueryCellLeavesRetrieveColumnForNativeButton(t *testing.T) {
	match := query.Match{StudyInstanceUID: "1.2.3"}

	if got := queryCell(match, 0); got != "" {
		t.Fatalf("retrieve indicator text = %q, want empty native-button cell", got)
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

func TestQueryCellShowsLocalState(t *testing.T) {
	match := query.Match{LocalState: queryLocalStatePresent}

	if got := queryCell(match, queryTableColumnLocalState); got != "Duplicate" {
		t.Fatalf("local state cell = %q, want Duplicate", got)
	}
	if got := queryCell(query.Match{}, queryTableColumnLocalState); got != "" {
		t.Fatalf("empty local state cell = %q, want empty", got)
	}
}

func TestQueryCellShowsWorkstationDisplayFields(t *testing.T) {
	match := query.Match{
		PatientBirthDate:       "19700102",
		StudyTime:              "134501",
		ImageCount:             "42",
		ReferringPhysicianName: "REFER^DOC",
		InstitutionName:        "General Hospital",
		LocalComments:          "local note",
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
	if got := queryCell(match, queryTableColumnLocalComments); got != "local note" {
		t.Fatalf("local comments cell = %q, want local note", got)
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

func TestApplyQuerySortSortsAndTogglesDirection(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{PatientName: "SMITH^JOHN", StudyDate: "20260602"},
			{PatientName: "DOE^JANE", StudyDate: "20260603"},
			{PatientName: "ALPHA^ANN", StudyDate: "20260601"},
		},
		selectedQueryRow: 2,
	}

	if !applyQuerySort(state, queryTableColumnPatient) {
		t.Fatal("applyQuerySort returned false for Patient column")
	}
	if got := []string{state.queries[0].PatientName, state.queries[1].PatientName, state.queries[2].PatientName}; strings.Join(got, "|") != "ALPHA^ANN|DOE^JANE|SMITH^JOHN" {
		t.Fatalf("ascending patient order = %v", got)
	}
	if !state.querySortActive || state.querySortColumn != queryTableColumnPatient || state.querySortDescending {
		t.Fatalf("sort state = active %v col %d desc %v", state.querySortActive, state.querySortColumn, state.querySortDescending)
	}
	if state.selectedQueryRow != -1 {
		t.Fatalf("selected row = %d, want reset", state.selectedQueryRow)
	}

	if !applyQuerySort(state, queryTableColumnPatient) {
		t.Fatal("second applyQuerySort returned false for Patient column")
	}
	if got := []string{state.queries[0].PatientName, state.queries[1].PatientName, state.queries[2].PatientName}; strings.Join(got, "|") != "SMITH^JOHN|DOE^JANE|ALPHA^ANN" {
		t.Fatalf("descending patient order = %v", got)
	}
	if !state.querySortDescending {
		t.Fatal("second sort should toggle to descending")
	}

	if !applyQuerySort(state, queryTableColumnStudyDate) {
		t.Fatal("applyQuerySort returned false for Study Date column")
	}
	if got := []string{state.queries[0].StudyDate, state.queries[1].StudyDate, state.queries[2].StudyDate}; strings.Join(got, "|") != "20260601|20260602|20260603" {
		t.Fatalf("ascending study date order = %v", got)
	}
	if state.querySortColumn != queryTableColumnStudyDate || state.querySortDescending {
		t.Fatalf("new sort state = col %d desc %v", state.querySortColumn, state.querySortDescending)
	}
}

func TestApplyQuerySortPersistsSortPreference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	state := &uiState{
		appConfigPath: configPath,
		appConfig:     appconfig.Defaults(),
		queries: []query.Match{
			{PatientName: "SMITH^JOHN", StudyDate: "20260602"},
			{PatientName: "DOE^JANE", StudyDate: "20260603"},
		},
	}

	if !applyQuerySort(state, queryTableColumnPatient) {
		t.Fatal("applyQuerySort returned false for Patient column")
	}

	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("Load persisted config: %v", err)
	}
	pref, ok := loaded.UISortPreferences[querySortPreferenceKey]
	if !ok {
		t.Fatalf("missing query sort preference in %#v", loaded.UISortPreferences)
	}
	if pref.Column != queryTableColumnPatient || pref.Descending {
		t.Fatalf("persisted query sort = col %d desc %v", pref.Column, pref.Descending)
	}
}

func TestApplySavedQuerySortPreferenceSortsLoadedQueries(t *testing.T) {
	state := &uiState{
		appConfig: appconfig.Config{
			UISortPreferences: map[string]appconfig.SortPreference{
				querySortPreferenceKey: {Column: queryTableColumnPatient, Descending: true},
			},
		},
		queries: []query.Match{
			{PatientName: "ALPHA^ANN", StudyDate: "20260601"},
			{PatientName: "SMITH^JOHN", StudyDate: "20260602"},
			{PatientName: "DOE^JANE", StudyDate: "20260603"},
		},
		selectedQueryRow: 1,
	}

	applySavedQuerySortPreference(state)

	if got := []string{state.queries[0].PatientName, state.queries[1].PatientName, state.queries[2].PatientName}; strings.Join(got, "|") != "SMITH^JOHN|DOE^JANE|ALPHA^ANN" {
		t.Fatalf("saved query sort order = %v", got)
	}
	if !state.querySortActive || state.querySortColumn != queryTableColumnPatient || !state.querySortDescending {
		t.Fatalf("saved query sort state = active %v col %d desc %v", state.querySortActive, state.querySortColumn, state.querySortDescending)
	}
	if state.selectedQueryRow != -1 {
		t.Fatalf("selected query row = %d, want reset", state.selectedQueryRow)
	}
}

func TestQuerySortRejectsUnsortableColumns(t *testing.T) {
	state := &uiState{
		queries: []query.Match{{PatientName: "DOE^JANE"}, {PatientName: "ALPHA^ANN"}},
	}

	if applyQuerySort(state, queryRetrieveColumn) {
		t.Fatal("Retrieve column should not be sortable")
	}
	if applyQuerySort(state, queryTableColumnLocalState) {
		t.Fatal("Local state indicator column should not be sortable")
	}
	if state.querySortActive {
		t.Fatal("query sort should remain inactive after unsortable columns")
	}
	if state.queries[0].PatientName != "DOE^JANE" {
		t.Fatalf("queries reordered after unsortable sort: %#v", state.queries)
	}
}

func TestQueryHeaderLabelShowsSortDirection(t *testing.T) {
	state := &uiState{querySortActive: true, querySortColumn: queryTableColumnPatient}

	if got := queryHeaderLabel(state, queryTableColumnPatient, "Patient"); got != "Patient ▲" {
		t.Fatalf("ascending header = %q, want Patient ▲", got)
	}
	state.querySortDescending = true
	if got := queryHeaderLabel(state, queryTableColumnPatient, "Patient"); got != "Patient ▼" {
		t.Fatalf("descending header = %q, want Patient ▼", got)
	}
	if got := queryHeaderLabel(state, queryTableColumnStudyDate, "Study Date"); got != "Study Date" {
		t.Fatalf("inactive header = %q, want Study Date", got)
	}
}

func TestQueryTableRowsGroupSeriesUnderExpandedStudy(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1", SeriesNumber: "2", SeriesDescription: "ABD SC"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.2", SeriesNumber: "4", SeriesDescription: "ABD CC"},
			{QueryRetrieveLevel: "STUDY", PatientName: "SMITH^JOHN", StudyInstanceUID: "9.9.study", StudyDescription: "HEAD CT"},
		},
	}

	rows := queryTableRows(state)

	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	if rows[0].kind != queryTableRowStudyGroup || rows[0].childCount != 2 || !rows[0].expanded {
		t.Fatalf("study group row = %#v, want expanded group with two children", rows[0])
	}
	if rows[1].kind != queryTableRowMatch || rows[1].queryIndex != 0 || rows[1].depth != 1 {
		t.Fatalf("first child row = %#v, want query index 0 depth 1", rows[1])
	}
	if rows[2].kind != queryTableRowMatch || rows[2].queryIndex != 1 || rows[2].depth != 1 {
		t.Fatalf("second child row = %#v, want query index 1 depth 1", rows[2])
	}
	if rows[3].kind != queryTableRowMatch || rows[3].queryIndex != 2 || rows[3].depth != 0 {
		t.Fatalf("standalone study row = %#v, want query index 2 depth 0", rows[3])
	}
	if got := queryRowCell(rows[0], queryTableColumnLevel); got != "▾ STUDY" {
		t.Fatalf("group level cell = %q, want expanded STUDY", got)
	}
	if got := queryRowCell(rows[0], queryTableColumnPatient); got != "▾ 1.2.study (2 items)" {
		t.Fatalf("study group patient cell = %q, want expanded study with item count", got)
	}
	if got := queryRowCell(rows[1], queryTableColumnPatient); got != "    2" {
		t.Fatalf("series child patient cell = %q, want indented series number", got)
	}
}

func TestQueryTableRowsGroupStudiesUnderExpandedPatient(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1", StudyDescription: "ABD CT"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1", SeriesInstanceUID: "1.2.series.1", SeriesNumber: "2", SeriesDescription: "ABD SC"},
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.2", StudyDescription: "HEAD CT"},
			{QueryRetrieveLevel: "STUDY", PatientName: "SMITH^JOHN", PatientID: "P2", StudyInstanceUID: "9.9.study", StudyDescription: "CHEST CT"},
		},
	}

	rows := queryTableRows(state)

	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want patient group, nested study group, child series, second study, and other patient", len(rows))
	}
	if rows[0].kind != queryTableRowPatientGroup || rows[0].childCount != 3 || !rows[0].expanded {
		t.Fatalf("patient group row = %#v, want expanded group with three underlying rows", rows[0])
	}
	if got := queryRowCell(rows[0], queryTableColumnLevel); got != "▾ PATIENT" {
		t.Fatalf("patient group level cell = %q, want expanded PATIENT", got)
	}
	if got := queryRowCell(rows[0], queryTableColumnPatient); got != "▾ DOE^JANE (3 matches)" {
		t.Fatalf("patient group patient cell = %q, want expanded DOE^JANE with match count", got)
	}
	if rows[1].kind != queryTableRowStudyGroup || rows[1].depth != 1 {
		t.Fatalf("nested study group row = %#v, want depth 1 study group", rows[1])
	}
	if got := queryRowCell(rows[1], queryTableColumnLevel); got != "  ▾ STUDY" {
		t.Fatalf("nested study level cell = %q, want indented expanded STUDY", got)
	}
	if rows[2].kind != queryTableRowMatch || rows[2].queryIndex != 1 || rows[2].depth != 2 {
		t.Fatalf("nested series child row = %#v, want query index 1 depth 2", rows[2])
	}
	if got := queryRowCell(rows[2], queryTableColumnPatient); got != "        2" {
		t.Fatalf("nested series patient cell = %q, want double-indented series number", got)
	}
	if rows[3].kind != queryTableRowMatch || rows[3].queryIndex != 2 || rows[3].depth != 1 {
		t.Fatalf("second study row = %#v, want query index 2 depth 1", rows[3])
	}
}

func TestToggleQueryGroupRowCollapsesAndExpandsPatient(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1"},
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.2"},
		},
	}

	rows := queryTableRows(state)
	if !toggleQueryGroupRow(state, rows[0]) {
		t.Fatal("toggleQueryGroupRow should collapse a patient group row")
	}
	collapsed := queryTableRows(state)
	if len(collapsed) != 1 || collapsed[0].expanded {
		t.Fatalf("collapsed rows = %#v, want one collapsed patient row", collapsed)
	}
	if got := queryRowCell(collapsed[0], queryTableColumnLevel); got != "▸ PATIENT" {
		t.Fatalf("collapsed patient level cell = %q, want collapsed PATIENT", got)
	}

	if !toggleQueryGroupRow(state, collapsed[0]) {
		t.Fatal("toggleQueryGroupRow should expand a patient group row")
	}
	expanded := queryTableRows(state)
	if len(expanded) != 3 || !expanded[0].expanded {
		t.Fatalf("expanded rows = %#v, want patient plus two study rows", expanded)
	}
}

func TestSetQueryMatchesPreservesCollapsedQueryGroupsForSameKeys(t *testing.T) {
	state := &uiState{
		collapsedQueryGroups: map[string]bool{
			"patient-id:p1":   true,
			"study:1.2.study": true,
			"study:old":       true,
		},
	}
	matches := []query.Match{
		{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study"},
		{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1"},
		{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.other"},
	}

	if err := setQueryMatches(context.Background(), widget.NewLabel(""), nil, state, matches); err != nil {
		t.Fatalf("setQueryMatches returned error: %v", err)
	}

	if !state.collapsedQueryGroups["patient-id:p1"] {
		t.Fatal("patient group collapse state was not retained")
	}
	if !state.collapsedQueryGroups["study:1.2.study"] {
		t.Fatal("study group collapse state was not retained")
	}
	if state.collapsedQueryGroups["study:old"] {
		t.Fatal("stale study collapse key should be discarded")
	}
	rows := queryTableRows(state)
	if len(rows) != 1 || rows[0].kind != queryTableRowPatientGroup || rows[0].expanded {
		t.Fatalf("rows after refresh = %#v, want collapsed patient group", rows)
	}
}

func TestSetQueryMatchesDropsCollapsedQueryGroupsThatNoLongerGroup(t *testing.T) {
	state := &uiState{
		collapsedQueryGroups: map[string]bool{
			"patient-id:p1":   true,
			"study:1.2.study": true,
		},
	}
	matches := []query.Match{
		{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study"},
	}

	if err := setQueryMatches(context.Background(), widget.NewLabel(""), nil, state, matches); err != nil {
		t.Fatalf("setQueryMatches returned error: %v", err)
	}

	if len(state.collapsedQueryGroups) != 0 {
		t.Fatalf("collapsedQueryGroups = %#v, want stale non-group keys discarded", state.collapsedQueryGroups)
	}
	rows := queryTableRows(state)
	if len(rows) != 1 || rows[0].kind != queryTableRowMatch {
		t.Fatalf("rows after refresh = %#v, want standalone match row", rows)
	}
}

func TestQueryTableSelectionUsesNestedPatientChildQueryIndex(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1", SeriesInstanceUID: "1.2.series.1"},
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.2"},
		},
	}
	retrieveCount := 0
	table := newQueryTable(state, func() {
		retrieveCount++
	})

	table.OnSelected(widget.TableCellID{Row: 3, Col: queryRetrieveColumn})

	if state.selectedQueryRow != 1 {
		t.Fatalf("selectedQueryRow after nested child retrieve = %d, want query index 1", state.selectedQueryRow)
	}
	if retrieveCount != 1 {
		t.Fatalf("retrieve callback count = %d, want 1", retrieveCount)
	}
}

func TestQueryTableStudyGroupRetrieveColumnInvokesStudyRetrieve(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.2"},
		},
	}
	retrieveCount := 0
	table := newQueryTable(state, func() {
		retrieveCount++
	})

	table.OnSelected(widget.TableCellID{Row: 1, Col: queryRetrieveColumn})

	if retrieveCount != 1 {
		t.Fatalf("retrieve callback count = %d, want 1 for study parent retrieve", retrieveCount)
	}
	if len(queryTableRows(state)) != 3 {
		t.Fatal("study parent retrieve should not toggle the group")
	}
	match, ok := selectedQuery(state)
	if !ok {
		t.Fatal("study parent retrieve should select a retrievable study match")
	}
	if match.QueryRetrieveLevel != "STUDY" || match.StudyInstanceUID != "1.2.study" || match.SeriesInstanceUID != "" {
		t.Fatalf("selected query = %#v, want virtual STUDY match without series UID", match)
	}
}

func TestQueryTableStudyGroupPropagatesLocalStateFromChildren(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.2", LocalState: queryLocalStatePresent, LocalComments: "already imported"},
		},
	}

	rows := queryTableRows(state)

	if rows[0].kind != queryTableRowStudyGroup {
		t.Fatalf("first row = %#v, want study group", rows[0])
	}
	if rows[0].match.LocalState != queryLocalStatePresent {
		t.Fatalf("study group local state = %q, want %q", rows[0].match.LocalState, queryLocalStatePresent)
	}
	if rows[0].match.LocalComments != "already imported" {
		t.Fatalf("study group local comments = %q, want child comments", rows[0].match.LocalComments)
	}
	if got := queryRowCell(rows[0], queryTableColumnLocalState); got != "Duplicate" {
		t.Fatalf("study group local cell = %q, want Duplicate", got)
	}
}

func TestQueryTableStudyGroupPropagatesPostRetrieveStateFromChildren(t *testing.T) {
	retrieved := query.Match{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.2"}
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1"},
			retrieved,
		},
		queryRetrieveRows: map[string]string{
			queryRetrieveStatusKey(retrieved): "retrieved series 1.2.series.2 from RADIANT (C-MOVE final=0x0000 stored 42 failed 0)",
		},
	}

	rows := queryTableRows(state)

	if rows[0].kind != queryTableRowStudyGroup {
		t.Fatalf("first row = %#v, want study group", rows[0])
	}
	text, localState := queryRowLocalStateCell(state, rows[0])
	if text != "Retrieved" || localState != queryLocalStateRetrieved {
		t.Fatalf("study group post-retrieve state = %q/%q, want Retrieved/%q", text, localState, queryLocalStateRetrieved)
	}
}

func TestQueryTablePatientGroupPropagatesLocalStateFromDescendants(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1"},
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.2", LocalState: queryLocalStatePresent, LocalComments: "prior study"},
		},
	}

	rows := queryTableRows(state)

	if rows[0].kind != queryTableRowPatientGroup {
		t.Fatalf("first row = %#v, want patient group", rows[0])
	}
	if rows[0].match.LocalState != queryLocalStatePresent {
		t.Fatalf("patient group local state = %q, want %q", rows[0].match.LocalState, queryLocalStatePresent)
	}
	if rows[0].match.LocalComments != "prior study" {
		t.Fatalf("patient group local comments = %q, want descendant comments", rows[0].match.LocalComments)
	}
	if got := queryRowCell(rows[0], queryTableColumnLocalState); got != "Duplicate" {
		t.Fatalf("patient group local cell = %q, want Duplicate", got)
	}
}

func TestQueryTablePatientGroupRetrieveColumnDoesNotInvokeRetrieve(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.1"},
			{QueryRetrieveLevel: "STUDY", PatientName: "DOE^JANE", PatientID: "P1", StudyInstanceUID: "1.2.study.2"},
		},
	}
	retrieveCount := 0
	table := newQueryTable(state, func() {
		retrieveCount++
	})

	table.OnSelected(widget.TableCellID{Row: 1, Col: queryRetrieveColumn})

	if retrieveCount != 0 {
		t.Fatalf("retrieve callback count = %d, want 0 for patient parent row", retrieveCount)
	}
	if len(queryTableRows(state)) != 1 {
		t.Fatal("patient parent retrieve column should still toggle the patient group")
	}
}

func TestToggleQueryGroupRowCollapsesAndExpandsStudy(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.2"},
		},
	}

	rows := queryTableRows(state)
	if !toggleQueryGroupRow(state, rows[0]) {
		t.Fatal("toggleQueryGroupRow should collapse a group row")
	}
	collapsed := queryTableRows(state)
	if len(collapsed) != 1 || collapsed[0].expanded {
		t.Fatalf("collapsed rows = %#v, want one collapsed group row", collapsed)
	}
	if got := queryRowCell(collapsed[0], queryTableColumnLevel); got != "▸ STUDY" {
		t.Fatalf("collapsed level cell = %q, want collapsed STUDY", got)
	}

	if !toggleQueryGroupRow(state, collapsed[0]) {
		t.Fatal("toggleQueryGroupRow should expand a collapsed group row")
	}
	expanded := queryTableRows(state)
	if len(expanded) != 3 || !expanded[0].expanded {
		t.Fatalf("expanded rows = %#v, want group plus two children", expanded)
	}
}

func TestQueryTableSelectionUsesVisibleChildQueryIndex(t *testing.T) {
	state := &uiState{
		queries: []query.Match{
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.1"},
			{QueryRetrieveLevel: "SERIES", PatientName: "DOE^JANE", StudyInstanceUID: "1.2.study", SeriesInstanceUID: "1.2.series.2"},
		},
	}
	retrieveCount := 0
	table := newQueryTable(state, func() {
		retrieveCount++
	})

	table.OnSelected(widget.TableCellID{Row: 1, Col: queryTableColumnPatient})
	if state.selectedQueryRow != -1 {
		t.Fatalf("selectedQueryRow after group row = %d, want unchanged", state.selectedQueryRow)
	}
	if len(queryTableRows(state)) != 1 {
		t.Fatal("selecting the group row should collapse its children")
	}

	table.OnSelected(widget.TableCellID{Row: 1, Col: queryTableColumnPatient})
	table.OnSelected(widget.TableCellID{Row: 3, Col: queryRetrieveColumn})
	if state.selectedQueryRow != 1 {
		t.Fatalf("selectedQueryRow after second child retrieve = %d, want query index 1", state.selectedQueryRow)
	}
	if retrieveCount != 1 {
		t.Fatalf("retrieve callback count = %d, want 1", retrieveCount)
	}
}

func TestQueryCellFormatsStatusAsCodeOnly(t *testing.T) {
	tests := []struct {
		name   string
		status uint16
		want   string
	}{
		{"success", 0x0000, "0x0000"},
		{"pending", 0xFF00, "0xFF00"},
		{"pending warning", 0xFF01, "0xFF01"},
		{"failure", 0xA700, "0xA700"},
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

func TestQueryStatusTableCellUsesNativeStatusDot(t *testing.T) {
	tests := []struct {
		name   string
		status uint16
		want   color.NRGBA
	}{
		{"success", 0x0000, queryStatusOKColor},
		{"pending", 0xFF00, queryStatusPendingColor},
		{"pending warning", 0xFF01, queryStatusPendingColor},
		{"failure", 0xA700, queryStatusFailColor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cell := newQueryTableCell()

			applyQueryTableCell(cell, 1, queryTableColumnStatus, queryStatusCell(tt.status), false, false, false, tt.status, "")

			if !cell.statusDotBox.Visible() {
				t.Fatal("query status cell should show a native status dot")
			}
			if got := color.NRGBAModel.Convert(cell.statusDot.FillColor).(color.NRGBA); got != tt.want {
				t.Fatalf("status dot color = %#v, want %#v", got, tt.want)
			}
			if strings.HasPrefix(cell.label.Text, "●") || strings.HasPrefix(cell.label.Text, "!") {
				t.Fatalf("status label should not include text marker: %q", cell.label.Text)
			}
		})
	}
}

func TestQueryLocalStateTableCellUsesNativeStatusDot(t *testing.T) {
	cell := newQueryTableCell()

	applyQueryTableCell(cell, 1, queryTableColumnLocalState, "Duplicate", false, false, false, 0, queryLocalStatePresent)

	if !cell.statusDotBox.Visible() {
		t.Fatal("local state cell should show a native status dot")
	}
	if got := color.NRGBAModel.Convert(cell.statusDot.FillColor).(color.NRGBA); got != queryLocalStatePresentColor {
		t.Fatalf("local state dot color = %#v, want %#v", got, queryLocalStatePresentColor)
	}
	if cell.label.Text != "Duplicate" {
		t.Fatalf("local state label = %q, want Duplicate", cell.label.Text)
	}
}

func TestQueryRetrieveFailedLocalStateUsesFailureDot(t *testing.T) {
	cell := newQueryTableCell()

	applyQueryTableCell(cell, 1, queryTableColumnLocalState, "Failed", false, false, false, 0, queryLocalStateRetrieveFailed)

	if !cell.statusDotBox.Visible() {
		t.Fatal("retrieve failure local state should show a native status dot")
	}
	if got := color.NRGBAModel.Convert(cell.statusDot.FillColor).(color.NRGBA); got != queryStatusFailColor {
		t.Fatalf("retrieve failure dot color = %#v, want %#v", got, queryStatusFailColor)
	}
	if cell.label.Text != "Failed" {
		t.Fatalf("retrieve failure label = %q", cell.label.Text)
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
		{name: "retrieve cell", id: widget.TableCellID{Row: 2, Col: queryRetrieveColumn}, wantRow: 1, wantRetrieve: true, wantOK: true},
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

func TestQueryMatchCanRetrieveRequiresUIDsForLevel(t *testing.T) {
	tests := []struct {
		name  string
		match query.Match
		want  bool
	}{
		{
			name:  "study defaults to Study UID",
			match: query.Match{StudyInstanceUID: "1.2.3"},
			want:  true,
		},
		{
			name:  "missing study UID",
			match: query.Match{},
		},
		{
			name:  "missing placeholder study UID",
			match: query.Match{StudyInstanceUID: "(missing)"},
		},
		{
			name:  "series requires study and series UID",
			match: query.Match{QueryRetrieveLevel: "SERIES", StudyInstanceUID: "1.2.3", SeriesInstanceUID: "4.5.6"},
			want:  true,
		},
		{
			name:  "series hides without series UID",
			match: query.Match{QueryRetrieveLevel: "SERIES", StudyInstanceUID: "1.2.3"},
		},
		{
			name:  "image requires study series and SOP UID",
			match: query.Match{QueryRetrieveLevel: "image", StudyInstanceUID: "1.2.3", SeriesInstanceUID: "4.5.6", SOPInstanceUID: "7.8.9"},
			want:  true,
		},
		{
			name:  "image hides without SOP UID",
			match: query.Match{QueryRetrieveLevel: "IMAGE", StudyInstanceUID: "1.2.3", SeriesInstanceUID: "4.5.6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryMatchCanRetrieve(tt.match); got != tt.want {
				t.Fatalf("queryMatchCanRetrieve() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNodeTableCellUsesHeaderAndStripedStyling(t *testing.T) {
	cell := newNodeTableCell()

	applyNodeTableCell(cell, 0, 0, "Enabled", true, false, false, false, false, false, "")

	if cell.label.Text != "Enabled" {
		t.Fatalf("header text = %q", cell.label.Text)
	}
	if cell.check.Visible() {
		t.Fatal("node header should hide native checkbox")
	}
	if cell.retrieveSelect.Visible() {
		t.Fatal("node header should hide retrieve dropdown")
	}
	if !cell.label.TextStyle.Bold {
		t.Fatal("node header should be bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveHeaderRowColor {
		t.Fatalf("header fill = %#v, want %#v", got, archiveHeaderRowColor)
	}

	applyNodeTableCell(cell, 2, 5, "RADIANT", false, false, false, false, false, false, "")
	if cell.label.TextStyle.Bold {
		t.Fatal("node row should not be bold")
	}
	if cell.check.Visible() {
		t.Fatal("plain node cell should hide native checkbox")
	}
	if cell.retrieveSelect.Visible() {
		t.Fatal("plain node cell should hide retrieve dropdown")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveEvenRowColor {
		t.Fatalf("node even row fill = %#v, want %#v", got, archiveEvenRowColor)
	}
}

func TestNodeOperationalTableCellUsesNativeCheckbox(t *testing.T) {
	cell := newNodeTableCell()

	applyNodeTableCell(cell, 1, 0, "", false, false, false, true, true, false, "")

	if !cell.check.Visible() {
		t.Fatal("operational node cell should show native checkbox")
	}
	if !cell.check.Checked {
		t.Fatal("operational node checkbox should be checked")
	}
	if cell.label.Visible() && strings.ContainsAny(cell.label.Text, "☑☐") {
		t.Fatalf("operational node cell should not use text marker: %q", cell.label.Text)
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveOddRowColor {
		t.Fatalf("node operational fill = %#v, want %#v", got, archiveOddRowColor)
	}
}

func TestNodeRetrieveTableCellUsesNativeDropdown(t *testing.T) {
	cell := newNodeTableCell()

	applyNodeTableCell(cell, 1, 2, nodeMenuCell(nodes.RetrieveMethodGet), false, false, false, false, false, true, nodes.RetrieveMethodGet)

	if !cell.retrieveSelect.Visible() {
		t.Fatal("retrieve node cell should show native dropdown")
	}
	if cell.retrieveSelect.Selected != nodes.RetrieveMethodGet {
		t.Fatalf("retrieve dropdown selected = %q, want %q", cell.retrieveSelect.Selected, nodes.RetrieveMethodGet)
	}
	if cell.label.Visible() && strings.Contains(cell.label.Text, "▾") {
		t.Fatalf("retrieve node cell should not use text dropdown marker: %q", cell.label.Text)
	}
}

func TestNodeSendSyntaxTableCellUsesNativeDropdown(t *testing.T) {
	cell := newNodeTableCell()

	applyNodeTableCell(cell, 1, nodeTableColumnSendSyntax, nodeMenuCell(sendSyntaxExplicitLabel), false, false, false, false, false, true, sendSyntaxExplicitLabel)

	if !cell.retrieveSelect.Visible() {
		t.Fatal("send syntax node cell should show native dropdown")
	}
	if !slices.Equal(cell.retrieveSelect.Options, sendSyntaxOptions()) {
		t.Fatalf("send syntax dropdown options = %v, want %v", cell.retrieveSelect.Options, sendSyntaxOptions())
	}
	if cell.retrieveSelect.Selected != sendSyntaxExplicitLabel {
		t.Fatalf("send syntax dropdown selected = %q, want %q", cell.retrieveSelect.Selected, sendSyntaxExplicitLabel)
	}
	if cell.label.Visible() && strings.Contains(cell.label.Text, "▾") {
		t.Fatalf("send syntax node cell should not use text dropdown marker: %q", cell.label.Text)
	}
}

func TestNodeTLSTableCellUsesDisabledNativeDropdown(t *testing.T) {
	cell := newNodeTableCell()
	dropdown, value := nodeDropdownState(nodes.Node{}, nodeTableColumnTLS)

	if !dropdown {
		t.Fatal("TLS node cell should use the native dropdown affordance from the reference")
	}
	applyNodeTableCell(cell, 1, nodeTableColumnTLS, nodeMenuCell(value), false, false, false, false, false, dropdown, value)

	if !cell.retrieveSelect.Visible() {
		t.Fatal("TLS node cell should show native dropdown")
	}
	if !slices.Equal(cell.retrieveSelect.Options, []string{"No"}) {
		t.Fatalf("TLS dropdown options = %v, want [No]", cell.retrieveSelect.Options)
	}
	if cell.retrieveSelect.Selected != "No" {
		t.Fatalf("TLS dropdown selected = %q, want No", cell.retrieveSelect.Selected)
	}
	if !cell.retrieveSelect.Disabled() {
		t.Fatal("TLS dropdown should stay disabled until DIMSE TLS is implemented")
	}
}

func TestNodeSelectedTableCellUsesSelectionStyling(t *testing.T) {
	cell := newNodeTableCell()

	applyNodeTableCell(cell, 2, 5, "RADIANT", false, true, false, false, false, false, "")

	if cell.label.TextStyle.Bold {
		t.Fatal("selected node row text should not become bold")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != archiveSelectedRowColor {
		t.Fatalf("selected node fill = %#v, want %#v", got, archiveSelectedRowColor)
	}
}

func TestDisabledNodeTableCellUsesSubduedStyling(t *testing.T) {
	cell := newNodeTableCell()

	applyNodeTableCell(cell, 1, 5, "OFFLINE", false, false, true, false, false, false, "")

	if !cell.label.TextStyle.Italic {
		t.Fatal("disabled node row text should be italic")
	}
	if got := color.NRGBAModel.Convert(cell.background.FillColor).(color.NRGBA); got != nodeDisabledRowColor {
		t.Fatalf("disabled node fill = %#v, want %#v", got, nodeDisabledRowColor)
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

func TestArchiveFiltersWithQuickSearchFieldClearsSelectedField(t *testing.T) {
	base := archive.StudyFilters{
		PatientName:     "DOE",
		PatientID:       "P123",
		AccessionNumber: "A100",
		SourcePath:      "incoming",
	}

	filters, ok := archiveFiltersWithQuickSearchField(base, archiveQuickSearchPatientName, " ")

	if !ok {
		t.Fatal("patient-name quick search should support empty text")
	}
	if filters.PatientName != "" {
		t.Fatalf("PatientName = %q, want cleared", filters.PatientName)
	}
	if filters.PatientID != "P123" || filters.AccessionNumber != "A100" || filters.SourcePath != "incoming" {
		t.Fatalf("non-selected filters changed: %#v", filters)
	}
}

func TestArchiveFiltersWithQuickSearchFieldAppliesSoundexOnlyToPatientName(t *testing.T) {
	filters, ok := archiveFiltersWithQuickSearchFieldAndSoundex(archive.StudyFilters{}, archiveQuickSearchPatientName, " Alyce ", true)
	if !ok {
		t.Fatal("patient-name quick search should support soundex")
	}
	if filters.PatientName != "Alyce" || !filters.PatientNameSoundex {
		t.Fatalf("patient-name soundex filters = %#v", filters)
	}

	filters, ok = archiveFiltersWithQuickSearchFieldAndSoundex(archive.StudyFilters{PatientNameSoundex: true}, archiveQuickSearchPatientID, " P123 ", true)
	if !ok {
		t.Fatal("patient-id quick search should be supported")
	}
	if filters.PatientID != "P123" || filters.PatientNameSoundex {
		t.Fatalf("patient-id search should clear patient-name soundex flag: %#v", filters)
	}

	filters, ok = archiveFiltersWithQuickSearchFieldAndSoundex(archive.StudyFilters{PatientNameSoundex: true}, archiveQuickSearchPatientName, " ", true)
	if !ok {
		t.Fatal("empty patient-name quick search should be supported")
	}
	if filters.PatientName != "" || filters.PatientNameSoundex {
		t.Fatalf("empty patient-name search should clear soundex filter: %#v", filters)
	}
}

func TestArchiveControlsOmitQuickSearchForToolbarPlacement(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}

	controls := newArchiveControls(nil, widget.NewLabel(""), tables, state)

	if findEntryWithPlaceholder(controls, "Search") != nil {
		t.Fatal("archive tab controls should not contain quick search once it lives in the top toolbar")
	}
	if findCheckWithText(controls, "Soundex") != nil {
		t.Fatal("archive tab controls should not contain the toolbar Soundex checkbox")
	}
	if !findAccordionItemWithTitle(controls, "Advanced Filters") {
		t.Fatal("archive tab controls should keep Advanced Filters")
	}
}

func TestArchiveControlsShowActiveAlbumScopeInAdvancedFilters(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{
		catalog:                catalog,
		selectedArchiveAlbum:   archiveAlbumTodayCT,
		openedArchiveStudyUIDs: map[string]bool{},
	}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}

	controls := newArchiveControls(nil, widget.NewLabel(""), tables, state)

	item := findAccordionItem(controls, "Advanced Filters")
	if item == nil {
		t.Fatal("archive tab controls should keep Advanced Filters")
	}
	if findLabelContaining(item.Detail, "Album Scope: imported today, modality CT") == nil {
		t.Fatal("archive Advanced Filters should mirror the active smart album scope")
	}
}

func TestRefreshArchiveChromeUpdatesAdvancedAlbumScope(t *testing.T) {
	state := &uiState{
		selectedArchiveAlbum:   archiveAlbumTodayCT,
		archiveAlbumScopeLabel: compactWorkbenchLabel(),
	}

	refreshArchiveChrome(state)
	if state.archiveAlbumScopeLabel.Text != "Album Scope: imported today, modality CT" {
		t.Fatalf("scope label = %q, want Today CT scope", state.archiveAlbumScopeLabel.Text)
	}

	state.selectedArchiveAlbum = archiveAlbumDatabase
	refreshArchiveChrome(state)
	if state.archiveAlbumScopeLabel.Text != "Album Scope: all studies" {
		t.Fatalf("scope label after database = %q, want all studies", state.archiveAlbumScopeLabel.Text)
	}
}

func TestArchiveAdvancedFiltersMirrorActiveAlbumFields(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.Local)
	filters, ok := archiveAlbumFilters(archiveAlbumTodayCT, now)
	if !ok {
		t.Fatal("Today CT album should produce filters")
	}
	state := &uiState{
		catalog:              catalog,
		selectedArchiveAlbum: archiveAlbumTodayCT,
		studyFilters:         filters,
	}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}

	controls := newArchiveControls(nil, widget.NewLabel(""), tables, state)
	item := findAccordionItem(controls, "Advanced Filters")
	if item == nil {
		t.Fatal("archive tab controls should keep Advanced Filters")
	}

	if entry := findEntryWithPlaceholder(item.Detail, "2026-06-04T00:00:00Z"); entry == nil || entry.Text == "" {
		t.Fatalf("Imported From entry did not mirror active album filter: %#v", entry)
	}
	if entry := findEntryWithPlaceholder(item.Detail, "2026-06-04T23:59:59Z"); entry == nil || entry.Text == "" {
		t.Fatalf("Imported To entry did not mirror active album filter: %#v", entry)
	}
	if entry := findEntryWithPlaceholder(item.Detail, "CT,MR"); entry == nil || entry.Text != "CT" {
		t.Fatalf("Modality entry = %#v, want CT", entry)
	}
}

func TestArchiveAdvancedFilterClearResetsSelectedAlbum(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	filters, ok := archiveAlbumFilters(archiveAlbumTodayCT, time.Now())
	if !ok {
		t.Fatal("Today CT album should produce filters")
	}
	state := &uiState{
		catalog:              catalog,
		selectedArchiveAlbum: archiveAlbumTodayCT,
		studyFilters:         filters,
	}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}

	controls := newArchiveControls(nil, widget.NewLabel(""), tables, state)
	item := findAccordionItem(controls, "Advanced Filters")
	if item == nil {
		t.Fatal("archive tab controls should keep Advanced Filters")
	}
	clearButton := findButtonWithText(item.Detail, "Clear")
	if clearButton == nil {
		t.Fatal("advanced filters should expose Clear")
	}

	clearButton.OnTapped()

	if state.selectedArchiveAlbum != archiveAlbumDatabase {
		t.Fatalf("selectedArchiveAlbum = %q, want %q", state.selectedArchiveAlbum, archiveAlbumDatabase)
	}
}

func TestArchiveQuickSearchFiltersWhileTypingAndShowsSoundex(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}
	status := widget.NewLabel("")

	controls := newArchiveControlSet(nil, status, tables, state).toolbarSearch
	quickSearch := findEntryWithPlaceholder(controls, archiveQuickSearchPatientName)
	if quickSearch == nil {
		t.Fatal("archive toolbar should expose a visible quick search entry with the selected field placeholder")
	}
	soundex := findCheckWithText(controls, "Soundex")
	if soundex == nil {
		t.Fatal("archive toolbar should expose a workstation Soundex checkbox")
	}

	soundex.SetChecked(true)
	quickSearch.SetText("  DOE  ")

	if state.studyFilters.PatientName != "DOE" {
		t.Fatalf("PatientName filter = %q, want DOE", state.studyFilters.PatientName)
	}
	if !state.studyFilters.PatientNameSoundex {
		t.Fatal("Soundex toolbar checkbox should enable patient-name soundex filter")
	}
	if status.Text != "0 studies in local archive" {
		t.Fatalf("status = %q, want refreshed archive count", status.Text)
	}

	quickSearch.SetText("")

	if state.studyFilters.PatientName != "" {
		t.Fatalf("PatientName filter after clearing = %q, want empty", state.studyFilters.PatientName)
	}
	if state.studyFilters.PatientNameSoundex {
		t.Fatal("Soundex filter should clear when search text is empty")
	}
}

func TestArchiveQuickSearchHeaderShowsSelectedSearchMode(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}
	status := widget.NewLabel("")

	controls := newArchiveControlSet(nil, status, tables, state).toolbarSearch

	if findLabelContaining(controls, "Search by Patient Name") == nil {
		t.Fatal("archive toolbar quick search should show selected search mode")
	}
	if findEntryWithPlaceholder(controls, archiveQuickSearchPatientName) == nil {
		t.Fatal("archive toolbar quick search should use Patient Name as the initial placeholder")
	}
	field := findSelectWithOption(controls, archiveQuickSearchPatientID)
	if field == nil {
		t.Fatal("archive quick search field selector not found")
	}
	field.SetSelected(archiveQuickSearchPatientID)
	if findLabelContaining(controls, "Search by Patient ID") == nil {
		t.Fatal("archive quick search mode label should update with selected field")
	}
	if findEntryWithPlaceholder(controls, archiveQuickSearchPatientID) == nil {
		t.Fatal("archive quick search placeholder should update with selected field")
	}
}

func TestArchiveQuickSearchUsesIconOnlySearchButton(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}
	status := widget.NewLabel("")

	controls := newArchiveControlSet(nil, status, tables, state).toolbarSearch

	searchButton := findButtonWithIconAndText(controls, theme.SearchIcon(), "")
	if searchButton == nil {
		t.Fatal("archive toolbar search action should be an icon-only button")
	}
	if searchButton.OnTapped == nil {
		t.Fatal("archive toolbar search action should remain actionable")
	}
}

func TestArchiveToolbarQuickSearchUsesWorkbenchChrome(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}
	status := widget.NewLabel("")

	controls := newArchiveControlSet(nil, status, tables, state).toolbarSearch

	if !findEntryPlaceholderInWorkbenchChrome(controls, archiveQuickSearchPatientName) {
		t.Fatal("archive toolbar quick search should sit inside dark workbench chrome")
	}
	if !findSelectWithSelectedInWorkbenchChrome(controls, archiveQuickSearchPatientName) {
		t.Fatal("archive toolbar search selector should share the quick-search workbench chrome")
	}
	if findCheckWithText(controls, "Soundex") == nil {
		t.Fatal("archive toolbar quick search should preserve the Soundex checkbox")
	}
}

func TestArchiveToolbarQuickSearchBoxUsesStableWidth(t *testing.T) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder(archiveQuickSearchPatientName)
	soundex := widget.NewCheck("Soundex", nil)
	mode := compactWorkbenchLabel()
	mode.SetText(archiveQuickSearchModeText(archiveQuickSearchPatientName))

	box := newArchiveToolbarQuickSearchBox(entry, soundex, mode)

	if box.MinSize().Width < archiveToolbarQuickSearchWidth {
		t.Fatalf("archive toolbar quick search width = %.1f, want at least %.1f", box.MinSize().Width, archiveToolbarQuickSearchWidth)
	}
	if findEntryWithPlaceholder(box, archiveQuickSearchPatientName) == nil {
		t.Fatal("archive toolbar quick search box should preserve the search entry")
	}
	if findCheckWithText(box, "Soundex") == nil {
		t.Fatal("archive toolbar quick search box should preserve Soundex")
	}
	if findLabelContaining(box, "Search by Patient Name") == nil {
		t.Fatal("archive toolbar quick search box should preserve search mode label")
	}
	if mode.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("archive toolbar quick search mode alignment = %v, want trailing", mode.Alignment)
	}
	if !findLabelSlotWithPrefixAndMinWidth(box, "Search by", archiveToolbarQuickSearchWidth) {
		t.Fatal("archive toolbar quick search mode label should sit in the same stable-width slot as the entry")
	}
}

func TestArchiveToolbarQuickSearchOmitsSeparateSearchLabel(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}
	status := widget.NewLabel("")

	controls := newArchiveControlSet(nil, status, tables, state).toolbarSearch

	for _, text := range collectWidgetTexts(controls) {
		if text == "Search" {
			t.Fatal("archive toolbar quick search should not render a separate Search label beside the field selector")
		}
	}
	if findSelectWithOption(controls, archiveQuickSearchPatientID) == nil {
		t.Fatal("archive toolbar quick search should keep the field selector")
	}
	if findLabelContaining(controls, "Search by Patient Name") == nil {
		t.Fatal("archive toolbar quick search should keep the selected mode label")
	}
}

func TestArchiveSoundexToggleReappliesExistingQuickSearch(t *testing.T) {
	fynetest.NewApp()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer catalog.Close()
	state := &uiState{catalog: catalog}
	tables := archiveTables{
		studies:   newStudyTable(state),
		series:    newSeriesTable(state),
		instances: newInstanceTable(state),
	}
	status := widget.NewLabel("")

	controls := newArchiveControlSet(nil, status, tables, state).toolbarSearch
	quickSearch := findEntryWithPlaceholder(controls, archiveQuickSearchPatientName)
	soundex := findCheckWithText(controls, "Soundex")
	if quickSearch == nil || soundex == nil {
		t.Fatal("archive toolbar should expose quick search and Soundex")
	}

	quickSearch.SetText("Alyce")
	if state.studyFilters.PatientNameSoundex {
		t.Fatal("Soundex should default off before the checkbox is enabled")
	}
	soundex.SetChecked(true)
	if !state.studyFilters.PatientNameSoundex {
		t.Fatal("turning Soundex on should reapply the existing patient-name search")
	}
	soundex.SetChecked(false)
	if state.studyFilters.PatientNameSoundex {
		t.Fatal("turning Soundex off should reapply the existing patient-name search")
	}
}

func findEntryWithPlaceholder(obj fyne.CanvasObject, placeholder string) *widget.Entry {
	if entry, ok := obj.(*widget.Entry); ok && entry.PlaceHolder == placeholder {
		return entry
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findEntryWithPlaceholder(scroll.Content, placeholder)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findEntryWithPlaceholder(child, placeholder); found != nil {
				return found
			}
		}
	}
	return nil
}

func findEntryWithText(obj fyne.CanvasObject, text string) *widget.Entry {
	if entry, ok := obj.(*widget.Entry); ok && entry.Text == text {
		return entry
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findEntryWithText(scroll.Content, text)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findEntryWithText(child, text); found != nil {
				return found
			}
		}
	}
	return nil
}

func findCheckWithText(obj fyne.CanvasObject, text string) *widget.Check {
	if check, ok := obj.(*widget.Check); ok && check.Text == text {
		return check
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findCheckWithText(scroll.Content, text)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findCheckWithText(child, text); found != nil {
				return found
			}
		}
	}
	return nil
}

func findRadioGroupWithOption(obj fyne.CanvasObject, option string) *widget.RadioGroup {
	if radio, ok := obj.(*widget.RadioGroup); ok && slices.Contains(radio.Options, option) {
		return radio
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findRadioGroupWithOption(scroll.Content, option)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findRadioGroupWithOption(child, option); found != nil {
				return found
			}
		}
	}
	return nil
}

func collectWidgetTexts(obj fyne.CanvasObject) []string {
	var texts []string
	switch value := obj.(type) {
	case *widget.Label:
		if value.Text != "" {
			texts = append(texts, value.Text)
		}
	case *widget.Check:
		if value.Text != "" {
			texts = append(texts, value.Text)
		}
	case *widget.Entry:
		if value.Text != "" {
			texts = append(texts, value.Text)
		}
	case *widget.Button:
		if value.Text != "" {
			texts = append(texts, value.Text)
		}
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		texts = append(texts, collectWidgetTexts(scroll.Content)...)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			texts = append(texts, collectWidgetTexts(child)...)
		}
	}
	return texts
}

func findSelectWithSelected(obj fyne.CanvasObject, selected string) *widget.Select {
	if selectWidget, ok := obj.(*widget.Select); ok && selectWidget.Selected == selected {
		return selectWidget
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findSelectWithSelected(scroll.Content, selected)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findSelectWithSelected(child, selected); found != nil {
				return found
			}
		}
	}
	return nil
}

func findSelectSlotWithMinWidth(obj fyne.CanvasObject, selected string, minWidth float32) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if len(c.Objects) == 1 && c.MinSize().Width >= minWidth {
			if selectWidget, ok := c.Objects[0].(*widget.Select); ok && selectWidget.Selected == selected {
				return true
			}
		}
		for _, child := range c.Objects {
			if findSelectSlotWithMinWidth(child, selected, minWidth) {
				return true
			}
		}
	}
	return false
}

func findLabelSlotWithPrefixAndMinWidth(obj fyne.CanvasObject, prefix string, minWidth float32) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if len(c.Objects) == 1 && c.MinSize().Width >= minWidth {
			if label, ok := c.Objects[0].(*widget.Label); ok && strings.HasPrefix(label.Text, prefix) {
				return true
			}
		}
		for _, child := range c.Objects {
			if findLabelSlotWithPrefixAndMinWidth(child, prefix, minWidth) {
				return true
			}
		}
	}
	return false
}

func findCheckSlotWithTextAndMinWidth(obj fyne.CanvasObject, text string, minWidth float32) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if len(c.Objects) == 1 && c.MinSize().Width >= minWidth {
			if check, ok := c.Objects[0].(*widget.Check); ok && check.Text == text {
				return true
			}
		}
		for _, child := range c.Objects {
			if findCheckSlotWithTextAndMinWidth(child, text, minWidth) {
				return true
			}
		}
	}
	return false
}

func findButtonSlotWithTextAndMinWidth(obj fyne.CanvasObject, text string, minWidth float32) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if len(c.Objects) == 1 && c.MinSize().Width >= minWidth {
			if button, ok := c.Objects[0].(*widget.Button); ok && button.Text == text {
				return true
			}
		}
		for _, child := range c.Objects {
			if findButtonSlotWithTextAndMinWidth(child, text, minWidth) {
				return true
			}
		}
	}
	return false
}

func findSelectWithOption(obj fyne.CanvasObject, option string) *widget.Select {
	if selectWidget, ok := obj.(*widget.Select); ok && stringInList(option, selectWidget.Options) {
		return selectWidget
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findSelectWithOption(scroll.Content, option)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findSelectWithOption(child, option); found != nil {
				return found
			}
		}
	}
	return nil
}

func findButtonWithText(obj fyne.CanvasObject, text string) *widget.Button {
	if button, ok := obj.(*widget.Button); ok && button.Text == text {
		return button
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findButtonWithText(scroll.Content, text)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findButtonWithText(child, text); found != nil {
				return found
			}
		}
	}
	return nil
}

func findButtonWithIcon(obj fyne.CanvasObject, icon fyne.Resource) *widget.Button {
	if button, ok := obj.(*widget.Button); ok && button.Icon != nil && icon != nil && button.Icon.Name() == icon.Name() {
		return button
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findButtonWithIcon(scroll.Content, icon)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findButtonWithIcon(child, icon); found != nil {
				return found
			}
		}
	}
	return nil
}

func findButtonsWithIcon(obj fyne.CanvasObject, icon fyne.Resource) []*widget.Button {
	var buttons []*widget.Button
	if button, ok := obj.(*widget.Button); ok && button.Icon != nil && icon != nil && button.Icon.Name() == icon.Name() {
		buttons = append(buttons, button)
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		buttons = append(buttons, findButtonsWithIcon(scroll.Content, icon)...)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			buttons = append(buttons, findButtonsWithIcon(child, icon)...)
		}
	}
	return buttons
}

func findButtonWithIconAndText(obj fyne.CanvasObject, icon fyne.Resource, text string) *widget.Button {
	if button, ok := obj.(*widget.Button); ok && button.Text == text && button.Icon != nil && icon != nil && button.Icon.Name() == icon.Name() {
		return button
	}
	if scroll, ok := obj.(*container.Scroll); ok {
		return findButtonWithIconAndText(scroll.Content, icon, text)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findButtonWithIconAndText(child, icon, text); found != nil {
				return found
			}
		}
	}
	return nil
}

func findProgressBar(obj fyne.CanvasObject) *widget.ProgressBar {
	if progress, ok := obj.(*widget.ProgressBar); ok {
		return progress
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findProgressBar(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func findProgressBarInfinite(obj fyne.CanvasObject) *widget.ProgressBarInfinite {
	if progress, ok := obj.(*widget.ProgressBarInfinite); ok {
		return progress
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findProgressBarInfinite(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func findHorizontalScroll(obj fyne.CanvasObject) *container.Scroll {
	if scroll, ok := obj.(*container.Scroll); ok && scroll.Direction == container.ScrollHorizontalOnly {
		return scroll
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findHorizontalScroll(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func findTable(obj fyne.CanvasObject) *widget.Table {
	if table, ok := obj.(*widget.Table); ok {
		return table
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findTable(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func findAccordionItemWithTitle(obj fyne.CanvasObject, title string) bool {
	return findAccordionItem(obj, title) != nil
}

func findAccordionItem(obj fyne.CanvasObject, title string) *widget.AccordionItem {
	if accordion, ok := obj.(*widget.Accordion); ok {
		for _, item := range accordion.Items {
			if item.Title == title {
				return item
			}
		}
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findAccordionItem(child, title); found != nil {
				return found
			}
		}
	}
	if split, ok := obj.(*container.Split); ok {
		if found := findAccordionItem(split.Leading, title); found != nil {
			return found
		}
		if found := findAccordionItem(split.Trailing, title); found != nil {
			return found
		}
	}
	return nil
}

func findSideBySideSectionTitles(obj fyne.CanvasObject, leftTitle string, rightTitle string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if len(c.Objects) == 2 &&
			findLabelContaining(c.Objects[0], leftTitle) != nil &&
			findLabelContaining(c.Objects[1], rightTitle) != nil {
			return true
		}
		for _, child := range c.Objects {
			if findSideBySideSectionTitles(child, leftTitle, rightTitle) {
				return true
			}
		}
	}
	return false
}

func findDirectContainerWithTexts(obj fyne.CanvasObject, texts ...string) bool {
	return findDirectContainerWithTextsExcluding(obj, texts, nil)
}

func findDirectContainerWithTextsExcluding(obj fyne.CanvasObject, texts []string, excluded []string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		directTexts := collectDirectChildTexts(c)
		if allTextsPresent(directTexts, texts...) && !anyTextsPresent(directTexts, excluded...) {
			return true
		}
		for _, child := range c.Objects {
			if findDirectContainerWithTextsExcluding(child, texts, excluded) {
				return true
			}
		}
	}
	return false
}

func findDirectContainerWithTextsAndWorkbenchChromeExcluding(obj fyne.CanvasObject, texts []string, excluded []string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		directTexts := collectDirectChildTexts(c)
		if allTextsPresent(directTexts, texts...) &&
			!anyTextsPresent(directTexts, excluded...) &&
			containsCanvasRectangleColor(c, archiveHeaderRowColor) &&
			countCanvasRectanglesWithColor(c, tableColumnDividerColor) >= 2 {
			return true
		}
		for _, child := range c.Objects {
			if findDirectContainerWithTextsAndWorkbenchChromeExcluding(child, texts, excluded) {
				return true
			}
		}
	}
	return false
}

func findDirectHeaderChromeWithTextAndSelect(obj fyne.CanvasObject, text string, selected string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if findLabelContaining(c, text) != nil &&
			findSelectWithSelected(c, selected) != nil &&
			directCanvasRectanglesWithColor(c, archiveHeaderRowColor) >= 1 &&
			countCanvasRectanglesWithColor(c, tableColumnDividerColor) >= 1 {
			return true
		}
		for _, child := range c.Objects {
			if findDirectHeaderChromeWithTextAndSelect(child, text, selected) {
				return true
			}
		}
	}
	return false
}

func findSelectWithSelectedInWorkbenchChrome(obj fyne.CanvasObject, selected string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if findSelectWithSelected(c, selected) != nil &&
			directCanvasRectanglesWithColor(c, archiveHeaderRowColor) >= 1 &&
			countCanvasRectanglesWithColor(c, tableColumnDividerColor) >= 2 {
			return true
		}
		for _, child := range c.Objects {
			if findSelectWithSelectedInWorkbenchChrome(child, selected) {
				return true
			}
		}
	}
	return false
}

func findEntryPlaceholderInWorkbenchChrome(obj fyne.CanvasObject, placeholder string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if findEntryWithPlaceholder(c, placeholder) != nil &&
			directCanvasRectanglesWithColor(c, archiveHeaderRowColor) >= 1 &&
			countCanvasRectanglesWithColor(c, tableColumnDividerColor) >= 2 {
			return true
		}
		for _, child := range c.Objects {
			if findEntryPlaceholderInWorkbenchChrome(child, placeholder) {
				return true
			}
		}
	}
	return false
}

func directCanvasRectanglesWithColor(c *fyne.Container, want color.NRGBA) int {
	count := 0
	for _, child := range c.Objects {
		rect, ok := child.(*canvas.Rectangle)
		if !ok {
			continue
		}
		if got := color.NRGBAModel.Convert(rect.FillColor).(color.NRGBA); got == want {
			count++
		}
	}
	return count
}

func findThreeColumnFilterPanelsWithWorkbenchChrome(obj fyne.CanvasObject, sourceTitle string, dateTitle string, modalityTitle string) bool {
	if c, ok := obj.(*fyne.Container); ok {
		if len(c.Objects) == 3 &&
			findLabelContaining(c.Objects[0], sourceTitle) != nil &&
			findLabelContaining(c.Objects[1], dateTitle) != nil &&
			findLabelContaining(c.Objects[2], modalityTitle) != nil &&
			containsCanvasRectangleColor(c.Objects[1], archiveHeaderRowColor) &&
			containsCanvasRectangleColor(c.Objects[2], archiveHeaderRowColor) &&
			countCanvasRectanglesWithColor(c.Objects[1], tableColumnDividerColor) >= 2 &&
			countCanvasRectanglesWithColor(c.Objects[2], tableColumnDividerColor) >= 2 {
			return true
		}
		for _, child := range c.Objects {
			if findThreeColumnFilterPanelsWithWorkbenchChrome(child, sourceTitle, dateTitle, modalityTitle) {
				return true
			}
		}
	}
	return false
}

func collectDirectChildTexts(c *fyne.Container) []string {
	var texts []string
	for _, child := range c.Objects {
		texts = append(texts, collectWidgetTexts(child)...)
	}
	return texts
}

func allTextsPresent(haystack []string, needles ...string) bool {
	joined := strings.Join(haystack, "\n")
	for _, needle := range needles {
		if !strings.Contains(joined, needle) {
			return false
		}
	}
	return true
}

func anyTextsPresent(haystack []string, needles ...string) bool {
	joined := strings.Join(haystack, "\n")
	for _, needle := range needles {
		if needle != "" && strings.Contains(joined, needle) {
			return true
		}
	}
	return false
}

func findLabelContaining(obj fyne.CanvasObject, text string) *widget.Label {
	if label, ok := obj.(*widget.Label); ok && strings.Contains(label.Text, text) {
		return label
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if found := findLabelContaining(child, text); found != nil {
				return found
			}
		}
	}
	return nil
}
