package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

// TestRenderUIScreens renders each major screen to a PNG so the Fyne UI can be
// compared, pixel-for-pixel, against the Horos reference screenshots. It is
// skipped unless RENDER_UI is set, and writes to RENDER_UI_DIR (default
// /tmp/ui-render). It uses only synthetic, non-PHI data.
func TestRenderUIScreens(t *testing.T) {
	if os.Getenv("RENDER_UI") == "" {
		t.Skip("set RENDER_UI=1 to render UI screenshots")
	}
	outDir := os.Getenv("RENDER_UI_DIR")
	if outDir == "" {
		outDir = "/tmp/ui-render"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := fynetest.NewApp()
	configureAppAppearance(a)
	w := a.NewWindow("go-pacs")

	tmp := t.TempDir()
	catalog, err := archive.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	state := &uiState{
		catalog:                catalog,
		nodeStore:              nodes.NewStore(filepath.Join(tmp, "nodes.json")),
		nodes:                  syntheticNodes(),
		openedArchiveStudyUIDs: map[string]bool{},
	}
	state.nodeTableRows = makeNodeTableRows(len(state.nodes))

	status := widget.NewLabel("Ready")
	status.Wrapping = fyne.TextTruncate

	studyTable := newStudyTable(state)
	seriesTable := newSeriesTable(state)
	instanceTable := newInstanceTable(state)
	elementTable := newElementTable(state)
	taskTable := newTaskTable(state)
	taskDetail := newTaskDetail()
	state.operationTable = taskTable
	state.operationDetail = taskDetail
	summary := newSummaryPanel()

	tables := archiveTables{studies: studyTable, series: seriesTable, instances: instanceTable}
	wireArchiveTables(w, status, tables, state)
	nodeTable := newNodeTable(status, state)
	queryTab := newQueryTab(w, status, tables, nodeTable, state)
	autoQueryTab := newAutoQueryTab(w, status, tables, nodeTable, state)
	archiveControlSet := newArchiveControlSet(w, status, tables, state)
	archiveControls := archiveControlSet.archiveControls

	// Populate synthetic archive data AFTER the control set is built so the
	// builders do not clear it through an internal refresh.
	state.studies = syntheticStudies()
	state.archiveRows = archiveBrowserRowsForState(state)

	state.archiveSeriesSummary = compactWorkbenchLabel()
	state.archiveInstancesSummary = compactWorkbenchLabel()
	archiveBrowser := newArchiveBrowser(studyTable, seriesTable, instanceTable, state)
	archiveTab := newArchiveWorkbench(w, status, tables, archiveControls, archiveBrowser, state)
	networkTab := newNetworkTab(w, status, nodeTable, state)
	tasksBrowser := container.NewVSplit(labeledTable("Tasks", taskTable), container.NewStack(taskDetail))
	tasksBrowser.SetOffset(0.55)
	tasksTab := container.NewBorder(nil, nil, nil, nil, tasksBrowser)
	inspectorTab := container.NewBorder(summary.container, nil, nil, nil, container.NewStack(elementTable))

	actions := renderToolbarActions()
	actionsScroll := container.NewHScroll(actions)
	actionsScroll.SetMinSize(fyne.NewSize(0, actions.MinSize().Height))
	toolbar := newMainToolbar(status, actionsScroll, archiveControlSet.toolbarSearch)

	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Archive", nil, archiveTab),
		container.NewTabItemWithIcon(networkTabTitle, nil, networkTab),
		container.NewTabItemWithIcon("Query", nil, queryTab),
		container.NewTabItemWithIcon(autoQueryTabTitle, nil, autoQueryTab),
		container.NewTabItemWithIcon("Tasks", nil, tasksTab),
		container.NewTabItemWithIcon("Inspector", nil, inspectorTab),
	)

	content := container.NewBorder(toolbar, nil, nil, nil, tabs)
	w.SetContent(content)
	w.Resize(fyne.NewSize(1600, 900))

	// Apply the real load path so the collapse-by-default behaviour is reflected
	// in the render (patients collapsed, one row per exam).
	setStudies(state, tables, syntheticStudies())
	studyTable.Refresh()
	nodeTable.Refresh()

	shots := []struct {
		name string
		idx  int
		size fyne.Size
	}{
		{"01_archive_main", 0, fyne.NewSize(1600, 900)},
		{"05_nodes", 1, fyne.NewSize(1600, 900)},
		{"02_query", 2, fyne.NewSize(1600, 900)},
		{"03_auto_query", 3, fyne.NewSize(1600, 900)},
	}
	for _, s := range shots {
		tabs.SelectIndex(s.idx)
		w.Resize(s.size)
		studyTable.Refresh()
		nodeTable.Refresh()
		img := w.Canvas().Capture()
		path := filepath.Join(outDir, s.name+".png")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		t.Logf("wrote %s (%dx%d)", path, img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func renderToolbarActions() *fyne.Container {
	disabled := map[string]bool{}
	for _, l := range mainToolbarDisabledLabels() {
		disabled[l] = true
	}
	actionsMap := map[string]fyne.CanvasObject{}
	for _, label := range mainToolbarButtonLabels() {
		icon := mainToolbarIconResource(label)
		if disabled[label] {
			actionsMap[label] = disabledMainToolbarAction(label, icon)
		} else {
			actionsMap[label] = mainToolbarAction(label, icon, func() {})
		}
	}
	return groupedToolbarActions(actionsMap)
}

func syntheticStudies() []archive.Study {
	now := time.Now()
	mk := func(uid, name, id, dob, modality, desc, date, inst string, series, instances int, status, comments string) archive.Study {
		return archive.Study{
			StudyInstanceUID: uid,
			PatientName:      name,
			PatientID:        id,
			PatientBirthDate: dob,
			InstitutionName:  inst,
			StudyDate:        date,
			StudyDescription: desc,
			Modalities:       modality,
			Status:           status,
			Comments:         comments,
			SeriesCount:      series,
			InstanceCount:    instances,
			ImportedAt:       now,
		}
	}
	return []archive.Study{
		mk("1.1", "TEST PATIENT ALPHA", "1167410", "1968/02/05", "CT", "Abdomen Superior", "20250305", "Region Center", 8, 588, "", "reside one age"),
		mk("1.2", "TEST PATIENT BRAVO", "3714827", "1944/02/24", "CT", "Head Routine", "20250220", "Santa Casa", 7, 702, "", ""),
		mk("1.3", "TEST PATIENT CHARLIE", "3030305", "2022/01/02", "CT", "Localizers", "20250115", "Santa Casa", 7, 760, "", ""),
		mk("1.4", "TEST PATIENT DELTA", "0500409", "1955/06/30", "CT", "Vol Pulmao", "20250110", "Region Center", 4, 244, "", ""),
		mk("1.5", "TEST PATIENT ECHO", "7102211", "1971/11/03", "MR", "Examination Report", "20240630", "Gamma Med", 12, 11, "", "Automatic Result"),
		mk("1.6", "TEST PATIENT FOXTROT", "4410233", "1980/09/12", "CR", "Dose Report", "20240522", "Region Center", 1, 1, "", ""),
		mk("1.7", "TEST PATIENT GOLF", "9920114", "1990/04/18", "US", "Abd Sc", "20240410", "Gamma Med", 3, 120, "", ""),
		mk("1.8", "TEST PATIENT HOTEL", "2031885", "1962/12/01", "CT", "Vol Arterial", "20240228", "Region Center", 5, 410, "", ""),
		mk("1.9", "TEST PATIENT INDIA", "5519007", "1958/07/22", "MR", "Vol T2W", "20240115", "Santa Casa", 9, 540, "", ""),
		mk("1.10", "TEST PATIENT JULIET", "6610402", "2001/03/14", "CT", "Vol Pulmao", "20231220", "Region Center", 6, 333, "", ""),
		mk("1.11", "TEST PATIENT KILO", "7720518", "1948/10/09", "PT", "Whole Body", "20231101", "Gamma Med", 4, 280, "", ""),
		mk("1.12", "TEST PATIENT LIMA", "8830629", "1975/05/27", "CT", "Vol Arterial", "20231015", "Region Center", 5, 410, "", ""),
	}
}

func syntheticNodes() []nodes.Node {
	mk := func(id, name, ae, host string, port uint16, method, syntax string, tls bool) nodes.Node {
		return nodes.Node{ID: id, Name: name, AETitle: ae, Host: host, Port: port, RetrieveMethod: method, SendTransferSyntax: syntax, UseTLS: tls}
	}
	return []nodes.Node{
		mk("n1", "CT_SCANNER_1", "AN_CT63801", "10.5.7.240", 104, "C-GET", "Explicit Little Endian", false),
		mk("n2", "REMOTE_ARCHIVE", "DSVR6", "172.16.0.162", 114, "C-GET", "Explicit Little Endian", false),
		mk("n3", "MINI_PACS", "MINIPACS", "10.5.13.4007", 4007, "C-GET", "Explicit Little Endian", false),
		mk("n4", "VIEWER_REGIONAL", "VIEWER1", "10.5.7.252", 104, "C-MOVE", "Explicit Little Endian", false),
		mk("n5", "ARCHIVE_LOCAL", "ARCHIVE1", "10.5.7.229", 104, "C-MOVE", "Explicit Little Endian", false),
		mk("n6", "CT135461", "CT135461", "10.5.7.229", 104, "C-GET", "Explicit Little Endian", false),
		mk("n7", "REMOTE_ARCHIVE_2", "DSVR6", "172.16.0.162", 114, "C-GET", "Explicit Little Endian", false),
		mk("n8", "SANTA_CASA", "SANTA_CASA", "192.168.103.65", 104, "C-MOVE", "Explicit Little Endian", true),
		mk("n9", "SANTA_CASA_2", "SANTA_CASA_2", "177.43.116.243", 104, "C-MOVE", "Explicit Little Endian", true),
	}
}

func TestMeasureFloors(t *testing.T) {
	if os.Getenv("RENDER_UI") == "" {
		t.Skip("set RENDER_UI=1")
	}
	a := fynetest.NewApp()
	configureAppAppearance(a)
	th := a.Settings().Theme()
	t.Logf("THEME sizes: Text=%.2f InnerPadding=%.2f Padding=%.2f InlineIcon=%.2f",
		th.Size("text"), th.Size("innerPadding"), th.Size("padding"), th.Size("iconInline"))
	cell := newArchiveTableCell()
	t.Logf("archiveTableCell.MinSize = %v", cell.MinSize())
	lbl := widget.NewLabel("Patient")
	t.Logf("plain Label.MinSize = %v", lbl.MinSize())
	chk := widget.NewCheck("", func(bool) {})
	t.Logf("Check.MinSize = %v", chk.MinSize())
	sel := widget.NewSelect([]string{"C-GET", "C-MOVE"}, func(string) {})
	t.Logf("Select.MinSize = %v", sel.MinSize())
	t.Logf("CONSTS archiveRow=%.0f queryRow=%.0f networkRow=%.0f compactRow=%.0f",
		archiveTableRowHeight, queryTableRowHeight, networkTableRowHeight, compactTableRowHeight)
}

func TestReproQuerySourceMarkup(t *testing.T) {
	if os.Getenv("RENDER_UI") == "" {
		t.Skip("set RENDER_UI=1")
	}
	a := fynetest.NewApp()
	configureAppAppearance(a)
	state := &uiState{nodes: syntheticNodes()}
	list := newQuerySourceList(state)
	panel := newDicomNodesSourcePanel(
		newDicomNodesHeader(widget.NewLabel("")),
		container.NewBorder(newQuerySourceColumnHeader(), nil, nil, nil, list),
	)
	w := a.NewWindow("x")
	w.SetContent(panel)
	w.Resize(fyne.NewSize(560, 320))
	w.Canvas().Capture()
	markup := fynetest.RenderObjectToMarkup(panel)
	for _, name := range []string{"CT_SCANNER_1", "REMOTE_ARCHIVE", "AN_CT63801"} {
		if strings.Contains(markup, name) {
			t.Logf("markup CONTAINS %q", name)
		} else {
			t.Logf("markup MISSING  %q", name)
		}
	}
	t.Logf("markup length=%d", len(markup))
}
