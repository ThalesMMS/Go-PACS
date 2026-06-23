// Studies (main) tab — Horos-like: icon toolbar (Import/Export/Query/Send/
// Anonymize/Meta-Data/Delete + Patient Name search) over a 3-pane body: sidebar
// (Albums / Sources / Activity) | wide expandable study/series table | right
// per-patient study cards. archive.* JSON uses Go PascalCase field names.
window.TABS.archive = (function () {
  let panel;
  let allStudies = [];
  let displayed = [];
  let activeAlbum = "all";
  let selectedStudy = null;
  let sendNodes = [];
  let nodes = [];
  let receiver = { running: false };
  let recentOps = [];
  let openedUIDs = new Set();
  let search = { text: "" };
  let activeSearchField = "patientName";
  let page = { limit: 100, offset: 0, total: 0 };
  const expanded = new Set();
  const seriesCache = {};

  // Column model for the study table. Each column renders a study-row cell via
  // study(st) and (when expanded) a series-row cell via series(se); the returned
  // content may be a string or an array of nodes/strings. Columns are resizable
  // and reorderable via the shared helper (persisted under tableKey "studies").
  const COLUMNS = [
    { key: "name", label: "Patient name", width: 210, cls: "name",
      study: (st) => [discloseFor(st), modiconFor(mods(st).split(/[\\,]/)[0] || "?"), st.PatientName || "(unnamed)"],
      series: (se) => [modiconFor(se.Modality || "?"), se.SeriesDescription || "unnamed"] },
    { key: "modality", label: "Modality", width: 90, study: (st) => st.Modalities || "", series: (se) => se.Modality || "" },
    { key: "im", label: "# im", width: 60, num: true, study: (st) => String(st.InstanceCount ?? 0), series: (se) => String(se.InstanceCount ?? 0) },
    { key: "ser", label: "# ser", width: 60, num: true, study: (st) => String(st.SeriesCount ?? 0), series: () => "" },
    { key: "patientID", label: "Patient ID", width: 130, study: (st) => st.PatientID || "", series: () => "" },
    { key: "dob", label: "Date of Birth", width: 110, study: (st) => st.PatientBirthDate || "", series: () => "" },
    { key: "accession", label: "Accession", width: 120, study: (st) => st.AccessionNumber || "", series: () => "" },
    { key: "acquired", label: "Date Acquired", width: 120, study: (st) => st.StudyDate || "", series: (se) => se.SeriesDate || "" },
    { key: "added", label: "Date Added", width: 110, study: (st) => st.ImportedAt ? new Date(st.ImportedAt).toLocaleDateString() : "", series: () => "" },
    { key: "institution", label: "Institution", width: 150, study: (st) => st.InstitutionName || "", series: () => "" },
    { key: "status", label: "Status", width: 100, study: (st) => st.Status || "", series: () => "" },
    { key: "comments", label: "Comments", width: 180, study: (st) => st.Comments || "", series: () => "" },
  ];
  function modiconFor(text) { return el("span", { class: "modicon" }, text); }
  function discloseFor(st) {
    const open = expanded.has(st.StudyInstanceUID);
    return el("span", { class: "disclose" + (open ? " open" : ""), onclick: (e) => { e.stopPropagation(); toggleSeries(st); } }, "▶");
  }
  function studyColumns() { return orderedColumns(COLUMNS, "studies"); }
  function fillCell(td, content) {
    for (const c of (Array.isArray(content) ? content : [content])) {
      if (c === null || c === undefined || c === false) continue;
      td.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
    }
    return td;
  }
  // buildStudyTable creates the <table> (colgroup + header + empty body) and wires
  // resize/reorder. rebuildStudyTable swaps in a fresh table after a column drag.
  function buildStudyTable() {
    const oc = studyColumns();
    const table = el("table", { class: "studytable coltable" },
      el("colgroup", null, ...oc.map((c) => el("col", { style: "width:" + c.width + "px" }))),
      el("thead", null, el("tr", null, ...oc.map((c) => el("th", { class: c.num ? "num" : "" }, c.label)))),
      el("tbody", { id: "arc-rows" }));
    enhanceTableColumns(table, oc, { tableKey: "studies", onReorder: rebuildStudyTable });
    return table;
  }
  function rebuildStudyTable() {
    const mt = panel && panel.querySelector(".maintable");
    if (!mt) return;
    const old = mt.querySelector("table.studytable");
    const fresh = buildStudyTable();
    if (old) mt.replaceChild(fresh, old); else mt.appendChild(fresh);
    renderRows();
  }

  // Magnifier dropdown: DICOM search fields the backend supports (label -> query param key).
  const SEARCH_FIELDS = [
    { label: "All fields", key: "q" },
    { label: "Patient Name", key: "patientName" },
    { label: "Patient ID", key: "patientID" },
    { label: "Patient Birthdate (dd/MM/yyyy)", key: "patientBirthDate" },
    { label: "Study ID", key: "studyID" },
    { label: "Study Instance UID", key: "studyInstanceUID" },
    { label: "Comments", key: "comments" },
    { label: "Study Description", key: "description" },
    { label: "Body Part", key: "bodyPart" },
    { label: "Modality", key: "modality" },
    { label: "Accession Number", key: "accession" },
    { label: "Referring Physician", key: "referringPhysician" },
    { label: "Performing Physician", key: "performingPhysician" },
  ];
  function searchFieldLabel() { const f = SEARCH_FIELDS.find((x) => x.key === activeSearchField); return f ? f.label : "Patient Name"; }

  function todayYMD() { const d = new Date(); return `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`; }
  function startOfToday() { const d = new Date(); d.setHours(0, 0, 0, 0); return d; }
  function importedToday(s) { return s.ImportedAt && new Date(s.ImportedAt) >= startOfToday(); }
  function importedLastHour(s) { return s.ImportedAt && (Date.now() - new Date(s.ImportedAt).getTime()) < 3600e3; }
  function mods(s) { return (s.Modalities || "").toUpperCase(); }
  const ALBUMS = [
    { id: "all", label: "Database", cls: "blue", match: () => true },
    { id: "comments", label: "Cases with comments", cls: "album", match: (s) => !!s.Comments },
    { id: "interesting", label: "Interesting Cases", cls: "star", match: (s) => s.Status === "Interesting" },
    { id: "acq1h", label: "Just Acquired (last hour)", cls: "album", match: (s) => s.StudyDate === todayYMD() },
    { id: "add1h", label: "Just Added (last hour)", cls: "album", match: importedLastHour },
    { id: "opened", label: "Just Opened", cls: "album", match: (s) => openedUIDs.has(s.StudyInstanceUID) },
    { id: "todayCR", label: "Today CR", cls: "album", match: (s) => importedToday(s) && mods(s).includes("CR") },
    { id: "todayCT", label: "Today CT", cls: "album", match: (s) => importedToday(s) && mods(s).includes("CT") },
  ];

  /**
   * Renders the archive tab UI with toolbar, pane layout, study table, and sidebar.
   */
  function render() {
    panel.innerHTML = "";
    panel.appendChild(iconToolbar());
    panel.appendChild(el("div", { class: "three-pane grow-fill" },
      el("div", { class: "sidebar", id: "arc-sidebar" }),
      el("div", { class: "maintable" }, buildStudyTable(), el("div", { class: "pager", id: "arc-pager" })),
      el("div", { class: "detailpane", id: "arc-detail" }, el("div", { class: "empty" }, "No patient selected"))));
    panel.appendChild(el("input", { type: "file", id: "arc-file", multiple: true, accept: ".dcm,.zip,application/zip,application/dicom", style: "display:none", onchange: uploadImport }));
    reloadAll();
  }

  // ---- Icon toolbar ----
  const ICONS = (() => {
    const doc = (arrow) => `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>${arrow}</svg>`;
    return {
      import: `<span style="color:#3cba54">${doc('<path d="M12 11v6"/><path d="M9.5 14.5 12 17l2.5-2.5"/>')}</span>`,
      export: `<span style="color:#4a90d9">${doc('<path d="M12 17v-6"/><path d="M9.5 13.5 12 11l2.5 2.5"/>')}</span>`,
      query: `<span style="color:#3cba54"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v11"/><path d="M7.5 10.5 12 15l4.5-4.5"/><path d="M5 20h14"/></svg></span>`,
      send: `<span style="color:#4a90d9"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20V9"/><path d="M7.5 13.5 12 9l4.5 4.5"/><path d="M5 4h14"/></svg></span>`,
      anon: `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3l18 18"/><path d="M10.6 5.1A9.6 9.6 0 0 1 21 12a9.8 9.8 0 0 1-2.3 3.2"/><path d="M6.4 6.4A9.7 9.7 0 0 0 3 12a9.6 9.6 0 0 0 13 4.6"/></svg>`,
      meta: `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="3" width="14" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>`,
      storage: `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="7" ry="3"/><path d="M5 5v6c0 1.7 3.1 3 7 3s7-1.3 7-3V5"/><path d="M5 11v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"/></svg>`,
      del: `<span style="color:#d05a52"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16"/><path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/><path d="M6 7l1 13a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-13"/></svg></span>`,
      report: `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/><path d="M8 13h8M8 17h5"/></svg>`,
      detail: `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/></svg>`,
    };
  })();
  /**
   * Creates the archive tab icon toolbar with action buttons and search functionality.
   * @return {Element} The toolbar DOM element containing action buttons and a search box.
   */
  function iconToolbar() {
    const tb = (icon, l, fn, id) => el("button", { class: "tbtn", id, onclick: fn },
      el("span", { class: "g", html: ICONS[icon] }), el("span", { class: "l" }, l));
    const searchInput = el("input", { placeholder: searchFieldLabel(), onkeydown: (e) => { if (e.key === "Enter") doSearch(); } });
    searchInput.id = "arc-search";
    const mag = el("button", { class: "mag", onclick: (e) => { e.stopPropagation(); toggleFieldMenu(); } }, "⌕");
    const box = el("div", { class: "box" }, mag, searchInput);
    return el("div", { class: "icontoolbar" },
      tb("import", "Import", importStudies),
      tb("export", "Export", exportStudies),
      el("span", { class: "sep" }),
      tb("query", "Query", () => activateTab("query")),
      tb("send", "Send", openSend, "tb-send"),
      el("span", { class: "sep" }),
      tb("anon", "Anonymize", anonymizeStudy, "tb-anon"),
      tb("meta", "Meta-Data", metaData, "tb-meta"),
      tb("del", "Delete", deleteStudy, "tb-delete"),
      tb("report", "Report", openReport, "tb-report"),
      tb("storage", "Storage", openStorage),
      tb("detail", "Detail", toggleDetailPane, "tb-detail"),
      el("span", { class: "grow" }),
      el("div", { class: "search" },
        box));
  }
  function toggleDetailPane() {
    const tp = document.querySelector(".three-pane");
    if (tp) tp.classList.toggle("detail-collapsed");
  }

  // ---- Magnifier dropdown (DICOM search fields) ----
  let fieldMenu = null;
  function closeFieldMenu() {
    if (fieldMenu) { fieldMenu.remove(); fieldMenu = null; }
    document.removeEventListener("click", onFieldMenuOutside, true);
    document.removeEventListener("keydown", onFieldMenuKey, true);
  }
  function onFieldMenuOutside(e) { if (fieldMenu && !fieldMenu.contains(e.target) && !(e.target.classList && e.target.classList.contains("mag"))) closeFieldMenu(); }
  function onFieldMenuKey(e) { if (e.key === "Escape") closeFieldMenu(); }
  function toggleFieldMenu() {
    if (fieldMenu) { closeFieldMenu(); return; }
    const box = document.querySelector(".icontoolbar .search .box");
    if (!box) return;
    fieldMenu = el("div", { class: "fieldmenu" });
    for (const f of SEARCH_FIELDS) {
      fieldMenu.appendChild(el("div", { class: "fitem" + (f.key === activeSearchField ? " active" : ""), onclick: () => selectSearchField(f) }, f.label));
    }
    box.appendChild(fieldMenu);
    document.addEventListener("click", onFieldMenuOutside, true);
    document.addEventListener("keydown", onFieldMenuKey, true);
  }
  function selectSearchField(f) {
    activeSearchField = f.key;
    const input = document.getElementById("arc-search");
    if (input) input.placeholder = f.label;
    closeFieldMenu();
    doSearch();
  }

  /**
   * Executes a search using the current input value and reloads the study results.
   */
  function doSearch() {
    search.text = (document.getElementById("arc-search").value || "").trim();
    page.offset = 0;
    reloadAll();
  }

  /**
   * Loads archive studies and configuration from the backend and updates the archive view.
   */
  async function reloadAll() {
    const params = studyQueryParams();
    const [st, nd, rx, ops, cfg] = await Promise.all([
      apiGet("/api/archive/studies?" + params.toString()), apiGet("/api/nodes"), apiGet("/api/receiver/status"), apiGet("/api/tasks"), apiGet("/api/config"),
    ]);
    const data = (st.ok && st.data) || {};
    allStudies = Array.isArray(data) ? data : (data.items || []);
    page.total = Array.isArray(data) ? allStudies.length : (data.total || 0);
    page.limit = Array.isArray(data) ? 100 : (data.limit || 100);
    page.offset = Array.isArray(data) ? 0 : (data.offset || 0);
    nodes = (nd.ok && nd.data) || [];
    sendNodes = nodes;
    receiver = (rx.ok && rx.data) || { running: false };
    recentOps = ((ops.ok && ops.data) || []).slice(0, 3);
    openedUIDs = new Set(((cfg.ok && cfg.data && cfg.data.openedArchiveStudyUIDs) || []));
    renderSidebar();
    applyFilters();
  }

  /**
   * Builds query parameters for fetching studies from the archive API.
   * @returns {URLSearchParams} Query parameters with pagination, search, and album filters.
   */
  function studyQueryParams() {
    const params = new URLSearchParams({ limit: String(page.limit || 100), offset: String(page.offset || 0) });
    if (search.text) {
      let value = search.text;
      if (activeSearchField === "patientBirthDate") {
        const mdate = value.match(/^(\d{2})\/(\d{2})\/(\d{4})$/);
        if (mdate) value = mdate[3] + mdate[2] + mdate[1];
      }
      params.set(activeSearchField, value);
    }
    const today = todayYMD();
    const oneHourAgo = new Date(Date.now() - 3600e3).toISOString();
    const todayStart = startOfToday().toISOString();
    if (activeAlbum === "comments") params.set("hasComments", "true");
    if (activeAlbum === "interesting") params.set("status", "Interesting");
    if (activeAlbum === "acq1h") { params.set("dateFrom", today); params.set("dateTo", today); }
    if (activeAlbum === "add1h") params.set("importedFrom", oneHourAgo);
    if (activeAlbum === "todayCR" || activeAlbum === "todayCT") {
      params.set("importedFrom", todayStart);
      params.set("modality", activeAlbum === "todayCR" ? "CR" : "CT");
    }
    return params;
  }

  /**
   * Populates the sidebar with album filters, data sources, and recent activity.
   */
  function renderSidebar() {
    const sb = document.getElementById("arc-sidebar");
    if (!sb) return;
    sb.innerHTML = "";
    const albums = el("div", { class: "group" }, el("div", { class: "hdr" }, "Albums"));
    for (const a of ALBUMS) {
      const count = a.id === "all" ? page.total : allStudies.filter(a.match).length;
      albums.appendChild(el("button", {
        class: "side-item" + (a.id === activeAlbum ? " active" : ""),
        onclick: () => { activeAlbum = a.id; page.offset = 0; select(null); renderSidebar(); reloadAll(); },
      }, el("span", { class: "ico " + a.cls }), a.label, el("span", { class: "count" }, String(count))));
    }
    sb.appendChild(albums);

    const sources = el("div", { class: "group" }, el("div", { class: "hdr" }, "Sources"));
    sources.appendChild(el("button", { class: "side-item active" }, el("span", { class: "ico blue" }), "Documents DB"));
    sources.appendChild(el("button", { class: "side-item", onclick: () => activateTab("network") },
      el("span", { class: "ico" }), receiver.running ? `Receiver ${receiver.aeTitle || ""}` : "Receiver",
      el("span", { class: "nodedot" + (receiver.running ? " online" : "") })));
    for (const n of nodes) {
      sources.appendChild(el("button", { class: "side-item", onclick: () => activateTab("query") },
        el("span", { class: "ico" }), n.name || n.aeTitle, el("span", { class: "nodedot" })));
    }
    sb.appendChild(sources);

    const act = el("div", { class: "group fill" }, el("div", { class: "hdr" }, "Activity"));
    if (!recentOps.length) act.appendChild(el("div", { class: "version" }, "No Activity"));
    for (const op of recentOps) {
      act.appendChild(el("div", { class: "activity-row" },
        el("div", { class: "at" }, `${op.kind || ""} · ${op.status || ""}`)));
    }
    sb.appendChild(act);
  }

  /**
   * Filters studies by the active album and updates the display.
   */
  async function applyFilters() {
    const m = ALBUMS.find((a) => a.id === activeAlbum).match;
    displayed = allStudies.filter(m);
    renderRows();
  }

  /**
   * Renders the study and series rows in the archive table.
   * 
   * Populates the table with rows from the currently displayed studies, expanding series for any studies in the expanded set. Displays a "No studies" message if no studies are available, and updates pagination controls.
   */
  function renderRows() {
    const body = document.getElementById("arc-rows");
    if (!body) return;
    body.innerHTML = "";
    const oc = studyColumns();
    if (!displayed.length) {
      body.appendChild(el("tr", null, el("td", { colspan: String(oc.length), class: "empty" }, "No studies")));
      renderPager();
      return;
    }
    for (const st of displayed) {
      body.appendChild(studyRow(st, oc));
      if (expanded.has(st.StudyInstanceUID)) appendSeriesRows(body, st, oc);
    }
    renderPager();
  }

  /**
   * Updates the pagination controls with Previous/Next buttons and a record count display.
   */
  function renderPager() {
    const wrap = document.getElementById("arc-pager");
    if (!wrap) return;
    const from = page.total === 0 ? 0 : Math.min(page.offset + 1, page.total);
    const to = Math.min(page.offset + displayed.length, page.total);
    wrap.innerHTML = "";
    wrap.append(
      el("button", { class: "btn secondary", disabled: page.offset <= 0, onclick: () => { page.offset = Math.max(0, page.offset - page.limit); reloadAll(); } }, "Previous"),
      el("span", { class: "muted" }, `${from}-${to} of ${page.total}`),
      el("button", { class: "btn secondary", disabled: page.offset + page.limit >= page.total, onclick: () => { page.offset += page.limit; reloadAll(); } }, "Next"));
  }

  /**
   * Builds a table row element for a study with selection and context menu.
   * @param {Object} st - The study object to display.
   * @param {Array} oc - Ordered columns defining the row's cells.
   * @return {HTMLTableRowElement} The table row element.
   */
  function studyRow(st, oc) {
    const tr = el("tr", { class: "study" + (selectedStudy && selectedStudy.StudyInstanceUID === st.StudyInstanceUID ? " selected" : "") });
    for (const c of oc) {
      const td = el("td", { class: ((c.num ? "num " : "") + (c.cls || "")).trim() });
      fillCell(td, c.study(st));
      tr.appendChild(td);
    }
    tr.addEventListener("click", () => select(st));
    tr.addEventListener("contextmenu", (e) => { e.preventDefault(); select(st); showContextMenu(e.clientX, e.clientY, st); });
    return tr;
  }

  // ---- Right-click context menu on study rows ----
  let ctxMenu = null;
  function closeContextMenu() {
    if (ctxMenu) { ctxMenu.remove(); ctxMenu = null; }
    document.removeEventListener("click", onCtxOutside, true);
    document.removeEventListener("keydown", onCtxKey, true);
    document.removeEventListener("scroll", closeContextMenu, true);
  }
  function onCtxOutside(e) { if (ctxMenu && !ctxMenu.contains(e.target)) closeContextMenu(); }
  function onCtxKey(e) { if (e.key === "Escape") closeContextMenu(); }
  /**
   * Displays a context menu with study operations at the specified position.
   * @param {number} x - The left position in pixels.
   * @param {number} y - The top position in pixels.
   * @param {Object} st - The study object.
   */
  function showContextMenu(x, y, st) {
    closeContextMenu();
    const open = expanded.has(st.StudyInstanceUID);
    const item = (label, action, disabled) => {
      const d = el("div", { class: "citem" + (disabled ? " disabled" : "") }, label);
      if (!disabled && action) d.addEventListener("click", () => { closeContextMenu(); action(); });
      return d;
    };
    const sep = () => el("div", { class: "csep" });
    const items = [
      item("Query Selected Patient from Q&R Window…", () => activateTab("query")),
      item("Copy '" + (st.PatientName || "") + "' (Patient name) to clipboard", () => { navigator.clipboard.writeText(st.PatientName || ""); setStatus("Copied patient name to clipboard", "ok"); }),
      sep(),
      item("Export", () => exportStudies()),
      item("Import image / movie / PDF file to this study…", () => document.getElementById("arc-file").click()),
      item("Export ROI and Key Images", null, true),
      sep(),
      item("Expand Study", () => { if (!open) toggleSeries(st); }, open),
      item("Collapse Study", () => { if (open) toggleSeries(st); }, !open),
      sep(),
      item("Lock Study", null, true),
      item("Unlock Study", null, true),
      item("Merge Selected Studies", null, true),
      item("Unify Patient Identity", null, true),
      item("Generate new UIDs", null, true),
      item("Convert multi-frame file(s) to single files", null, true),
      sep(),
      item("Anonymize", () => anonymizeStudy()),
      item("Reindex Files", null, true),
      item("Compress DICOM files", null, true),
      item("Decompress DICOM files", () => decompressStudy(st)),
      sep(),
      item("Delete Selected Line(s)", () => deleteStudy()),
      item("Report", () => openReport()),
      sep(),
      item("Calculate total used disk space…", () => setStatus(`${allStudies.length} studies, ${displayed.length} shown`, "ok")),
    ];
    ctxMenu = el("div", { class: "ctxmenu" }, ...items);
    ctxMenu.style.position = "fixed";
    ctxMenu.style.left = x + "px";
    ctxMenu.style.top = y + "px";
    document.body.appendChild(ctxMenu);
    // Clamp so the menu stays on screen.
    const r = ctxMenu.getBoundingClientRect();
    if (r.right > window.innerWidth) ctxMenu.style.left = Math.max(0, window.innerWidth - r.width - 4) + "px";
    if (r.bottom > window.innerHeight) ctxMenu.style.top = Math.max(0, window.innerHeight - r.height - 4) + "px";
    document.addEventListener("click", onCtxOutside, true);
    document.addEventListener("keydown", onCtxKey, true);
    document.addEventListener("scroll", closeContextMenu, true);
  }

  async function toggleSeries(st) {
    const uid = st.StudyInstanceUID;
    if (expanded.has(uid)) { expanded.delete(uid); renderRows(); return; }
    expanded.add(uid);
    if (!seriesCache[uid]) { const r = await apiGet(`/api/archive/studies/${encodeURIComponent(uid)}/series`); seriesCache[uid] = (r.ok && r.data) || []; }
    renderRows();
  }
  function appendSeriesRows(body, st, oc) {
    for (const se of (seriesCache[st.StudyInstanceUID] || [])) {
      const tr = el("tr", { class: "series" });
      for (const c of oc) {
        const td = el("td", { class: ((c.num ? "num " : "") + (c.cls || "")).trim() });
        fillCell(td, c.series(se));
        tr.appendChild(td);
      }
      tr.addEventListener("click", () => { select(st); window.inspectFirstOfSeries && window.inspectFirstOfSeries(se); });
      body.appendChild(tr);
    }
  }

  /**
   * Selects a study and updates the UI to display its details, related studies, and preview.
   * @param {Object|null} st - The study object to select, or `null` to deselect.
   */
  function select(st) {
    selectedStudy = st;
    window.gopacsSelectedStudy = st || null;
    window.gopacsSelectedStudyUID = st ? st.StudyInstanceUID : "";
    renderRows();
    for (const id of ["tb-send", "tb-anon", "tb-meta", "tb-delete", "tb-report"]) { const b = document.getElementById(id); if (b) b.disabled = !st; }
    const d = document.getElementById("arc-detail");
    if (!st) { d.innerHTML = '<div class="empty">No patient selected</div>'; return; }
    d.innerHTML = "";
    d.appendChild(el("div", { class: "dhdr" }, el("div", { class: "t" }, st.PatientName || "(unnamed)")));
    const preview = el("div", { class: "previewbox" }, el("div", { class: "empty" }, "Preview"));
    d.appendChild(preview);
    loadPreview(st, preview);
    const studies = allStudies.filter((s) => (s.PatientID && s.PatientID === st.PatientID) || s.StudyInstanceUID === st.StudyInstanceUID);
    for (const s of studies) {
      d.appendChild(el("div", { class: "pcard" + (s.StudyInstanceUID === st.StudyInstanceUID ? " selected" : ""), onclick: () => select(s) },
        el("div", { class: "body" },
          el("div", { class: "ttl" }, s.StudyDescription || "unnamed"),
          el("div", { class: "sub" }, `${s.StudyDate || ""} · ${s.InstanceCount ?? 0} images`)),
        el("div", { class: "mod" }, mods(s).split(/[\\,]/)[0] || "")));
    }
  }

  /**
   * Loads and displays a preview thumbnail for a study's first instance in the provided element.
   * 
   * If no instances are available or the preview image fails to load, displays an appropriate message.
   * 
   * @param {Object} st - The study object with StudyInstanceUID property.
   * @param {HTMLElement} wrap - The DOM element in which to display the preview.
   */
  async function loadPreview(st, wrap) {
    const r = await apiGet(`/api/archive/studies/${encodeURIComponent(st.StudyInstanceUID)}/instances`);
    const inst = r.ok && (r.data || []).find((x) => x.SOPInstanceUID);
    if (!inst) { wrap.innerHTML = '<div class="empty">No preview</div>'; return; }
    wrap.innerHTML = "";
    const img = el("img", { src: `/api/archive/instances/${encodeURIComponent(inst.SOPInstanceUID)}/preview?size=thumb`, alt: "DICOM preview" });
    img.onerror = () => { wrap.innerHTML = '<div class="empty">Preview unavailable</div>'; };
    wrap.appendChild(img);
  }

  // ---- Toolbar actions ----
  // Import via native open panel (files + folders) when bridged into the webview;
  // otherwise fall back to the hidden <input type=file> (plain browser).
  async function importStudies() {
    if (typeof window.__gopacsPickPaths === "function") {
      const paths = await window.__gopacsPickPaths();
      if (!paths || !paths.length) return;
      setStatus(`Importing ${paths.length} item(s)…`);
      const r = await apiSend("/api/archive/import-path", "POST", { paths });
      const rep = (r.data) || {};
      setStatus(r.ok ? `Imported: stored ${rep.StoredFiles ?? 0}, duplicates ${rep.Duplicates ?? 0}, invalid ${rep.InvalidFiles ?? 0}` : `Import failed: ${r.error}`, r.ok ? "ok" : "error");
      Object.keys(seriesCache).forEach((k) => delete seriesCache[k]);
      reloadAll();
    } else {
      document.getElementById("arc-file").click();
    }
  }

  async function uploadImport() {
    const input = document.getElementById("arc-file");
    if (!input.files.length) return;
    const fd = new FormData();
    for (const f of input.files) fd.append("files", f, f.name);
    setStatus(`Importing ${input.files.length} file(s)…`);
    const res = await fetch("/api/archive/import", { method: "POST", body: fd });
    const r = await res.json().catch(() => ({ ok: false, error: `HTTP ${res.status}` }));
    input.value = "";
    if (r.ok || r.data) {
      const rep = r.data || {};
      setStatus(r.ok ? `Imported: stored ${rep.StoredFiles ?? 0}, duplicates ${rep.Duplicates ?? 0}, invalid ${rep.InvalidFiles ?? 0}` : `Import error: ${r.error}`, r.ok ? "ok" : "error");
      Object.keys(seriesCache).forEach((k) => delete seriesCache[k]);
      reloadAll();
    } else setStatus(`Import failed: ${r.error}`, "error");
  }
  // Export via native save panel (writes a ZIP server-side) when bridged into the
  // webview; otherwise fall back to a browser download.
  async function exportStudies() {
    if (!selectedStudy) { setStatus("Select a study to export as DICOM", "error"); return; }
    const uid = selectedStudy.StudyInstanceUID;
    if (typeof window.__gopacsPickSave === "function") {
      const safe = (selectedStudy.PatientName || uid).replace(/[^A-Za-z0-9._-]/g, "_");
      const dest = await window.__gopacsPickSave(safe + ".dcm.zip");
      if (!dest) return;
      setStatus("Exporting DICOM…");
      const r = await apiSend("/api/archive/export-path", "POST", { studyUID: uid, seriesUID: "", dest });
      setStatus(r.ok ? `Exported ${r.data && r.data.count} file(s) to ${r.data && r.data.path}` : `Export failed: ${r.error}`, r.ok ? "ok" : "error");
    } else {
      window.open("/api/archive/export/dicom?studyUID=" + encodeURIComponent(uid), "_blank");
    }
  }

  /**
   * Opens a modal to select the send destination for the selected study.
   */
  function openSend() {
    if (!selectedStudy) return;
    const enabled = sendNodes.filter((n) => !n.sendDisabled && !n.disabled);
    const sendLabel = (n) => n.protocol === "dicomweb" ? `${n.name} (STOW-RS)` : `${n.name} (C-STORE ${n.aeTitle || ""})`;
    const sel = el("select", null, ...(enabled.length ? enabled.map((n) => el("option", { value: n.id }, sendLabel(n))) : [el("option", { value: "" }, "no send nodes")]));
    openModal(el("div", null,
      el("h2", null, "Send study"),
      modalRow("Destination", sel),
      el("div", { class: "mactions" },
        el("button", { class: "btn secondary", onclick: closeModal }, "Cancel"),
        el("button", { class: "btn", onclick: () => { closeModal(); sendStudy(sel.value); } }, "Send"))));
  }
  /**
   * Sends the selected study to a remote DICOM node.
   * @param {string} nodeID - The destination node identifier.
   */
  async function sendStudy(nodeID) {
    if (!nodeID) { setStatus("No send-enabled node", "error"); return; }
    setStatus("Sending study…");
    const r = await apiSend("/api/archive/send", "POST", { nodeID, level: "STUDY", studyUID: selectedStudy.StudyInstanceUID });
    if (!r.ok || !r.data || !r.data.jobID) { setStatus(`Send failed to start: ${r.error || "no job"}`, "error"); return; }
    streamJob(r.data.jobID, {
      onProgress: (p) => setStatus(`Sending… ${(p.Sent ?? 0)}/${(p.Total ?? 0)}`),
      onDone: (o) => setStatus(`${(o && o.Method) || "Send"} complete: sent ${(o && o.Sent) ?? 0}, failed ${(o && o.Failed) ?? 0}`, "ok"),
      onError: (msg) => setStatus(`Send failed: ${msg}`, "error"),
    });
  }

  /**
   * Creates an anonymized copy of the selected study.
   */
  async function anonymizeStudy() {
    if (!selectedStudy) return;
    const uid = selectedStudy.StudyInstanceUID;
    if (!confirm(`Create an anonymized copy of study ${uid}?`)) return;
    setStatus("Anonymizing study…");
    const r = await apiSend(`/api/archive/studies/${encodeURIComponent(uid)}/anonymize`, "POST");
    if (!r.ok || !r.data || !r.data.jobID) { setStatus(`Anonymize failed to start: ${r.error || "no job"}`, "error"); return; }
    streamJob(r.data.jobID, {
      onDone: (o) => {
        setStatus(`Anonymized study to ${(o && (o.newStudyUID || o.NewStudyUID)) || "new UID"}: stored ${(o && (o.storedFiles ?? o.StoredFiles)) || 0}`, "ok");
        Object.keys(seriesCache).forEach((k) => delete seriesCache[k]);
        reloadAll();
      },
      onError: (msg) => setStatus(`Anonymize failed: ${msg}`, "error"),
    });
  }

  /**
   * Updates the selected study's status and comments metadata through user prompts.
   */
  async function metaData() {
    if (!selectedStudy) return;
    const status = prompt("Status:", selectedStudy.Status || "");
    if (status === null) return;
    const comments = prompt("Comments:", selectedStudy.Comments || "");
    if (comments === null) return;
    const uid = selectedStudy.StudyInstanceUID;
    // Fetch current metadata first so the Report field is preserved (backend replaces the row).
    const cur = await apiGet(`/api/archive/studies/${encodeURIComponent(uid)}/metadata`);
    const md = (cur.ok && cur.data) || {};
    const r = await apiSend(`/api/archive/studies/${encodeURIComponent(uid)}/metadata`, "PUT", { Status: status, Comments: comments, Report: md.Report || "" });
    if (r.ok) { setStatus("Metadata saved", "ok"); reloadAll(); } else setStatus(`Save failed: ${r.error}`, "error");
  }

  async function openReport() {
    if (!selectedStudy) return;
    const uid = selectedStudy.StudyInstanceUID;
    const cur = await apiGet(`/api/archive/studies/${encodeURIComponent(uid)}/metadata`);
    const md = (cur.ok && cur.data) || {};
    const ta = el("textarea", { class: "report-text" });
    ta.value = md.Report || "";
    openModal(el("div", null,
      el("h2", null, "Report — " + (selectedStudy.PatientName || "(unnamed)")),
      ta,
      el("div", { class: "mactions" },
        el("button", { class: "btn secondary", onclick: closeModal }, "Cancel"),
        el("button", { class: "btn", onclick: async () => {
          const r = await apiSend(`/api/archive/studies/${encodeURIComponent(uid)}/metadata`, "PUT", { Status: md.Status || "", Comments: md.Comments || "", Report: ta.value });
          if (r.ok) { closeModal(); setStatus("Report saved", "ok"); reloadAll(); } else setStatus(`Save failed: ${r.error}`, "error");
        } }, "Save"))));
  }

  /**
   * Moves the selected study to local trash after confirming with the user.
   */
  async function deleteStudy() {
    if (!selectedStudy) return;
    if (!confirm(`Move study for ${selectedStudy.PatientName || selectedStudy.StudyInstanceUID} to local trash?`)) return;
    const r = await apiSend(`/api/archive/studies/${encodeURIComponent(selectedStudy.StudyInstanceUID)}`, "DELETE");
    if (r.ok) {
      const count = r.data.trashedObjects ?? r.data.deletedObjects ?? 0;
      setStatus(`Moved ${count} objects to local trash`, "ok");
      select(null);
      reloadAll();
    }
    else setStatus(`Delete failed: ${r.error}`, "error");
  }

  /**
   * Opens the storage management modal.
   */
  async function openStorage() {
    const body = el("div", { class: "storage-modal" },
      el("h2", null, "Storage"),
      el("div", { id: "storage-body" }, el("div", { class: "empty" }, "Loading…")),
      el("div", { class: "mactions" },
        el("button", { class: "btn secondary", onclick: closeModal }, "Close")));
    openModal(body);
    refreshStorageModal();
  }

  /**
   * Loads storage statistics and populates the storage modal with instance counts, disk usage, and trash management controls.
   */
  async function refreshStorageModal() {
    const wrap = document.getElementById("storage-body");
    if (!wrap) return;
    const r = await apiGet("/api/archive/storage");
    if (!r.ok) { wrap.innerHTML = `<div class="empty">Storage error: ${escapeHTML(r.error)}</div>`; return; }
    const data = r.data || {};
    const policy = data.policy || {};
    const stats = data.stats || {};
    const trash = data.trash || [];
    const days = el("input", { type: "number", min: "0", value: String(policy.trashAutoPurgeDays ?? 90) });
    wrap.innerHTML = "";
    wrap.append(
      el("div", { class: "storage-grid" },
        el("span", { class: "muted" }, "Instances"), el("span", null, String(stats.InstanceCount ?? stats.instanceCount ?? 0)),
        el("span", { class: "muted" }, "Bytes"), el("span", null, formatBytes(stats.TotalBytes ?? stats.totalBytes ?? 0)),
        el("span", { class: "muted" }, "Trash auto-purge days"), days),
      el("div", { class: "row", style: "margin:10px 0" },
        el("button", { class: "btn", onclick: async () => saveStoragePolicy(days.value) }, "Save policy"),
        el("button", { class: "btn secondary", onclick: verifyArchiveNow }, "Verify"),
        el("button", { class: "btn secondary", onclick: purgeExpiredTrash }, "Purge expired"),
        el("button", { class: "btn secondary", onclick: backupArchivePath }, "Backup path"),
        el("button", { class: "btn secondary", onclick: restoreArchivePath }, "Restore path")),
      trashTable(trash));
  }

  /**
   * Displays a table of trashed studies with restore and purge actions, or an empty state if no items exist.
   * @param {Array} trash - An array of trashed study items, each containing patient name, study date, deleted object count, and timestamp.
   * @returns {Element} A DOM element displaying the trash table or empty-state message.
   */
  function trashTable(trash) {
    if (!trash.length) return el("div", { class: "empty" }, "Trash is empty");
    const body = el("tbody", null);
    for (const item of trash) {
      body.appendChild(el("tr", null,
        el("td", null, item.patientName || item.PatientName || "(unnamed)"),
        el("td", null, item.studyDate || item.StudyDate || ""),
        el("td", null, String(item.deletedCount ?? item.DeletedCount ?? 0)),
        el("td", null, item.trashedAt || item.TrashedAt || ""),
        el("td", null, el("div", { class: "row", style: "gap:4px" },
          el("button", { class: "btn secondary", onclick: () => restoreTrash(item.studyInstanceUID || item.StudyInstanceUID) }, "Restore"),
          el("button", { class: "btn danger", onclick: () => purgeTrash(item.studyInstanceUID || item.StudyInstanceUID) }, "Purge")))));
    }
    return el("table", { class: "edit storage-trash" },
      el("thead", null, el("tr", null, ...["Patient", "Date", "Objects", "Trashed", ""].map((h) => el("th", null, h)))),
      body);
  }

  /**
   * Saves the trash auto-purge retention policy.
   * @param {string|number} value - The number of days to retain trash before auto-purging.
   */
  async function saveStoragePolicy(value) {
    const days = parseInt(value, 10);
    const r = await apiSend("/api/archive/storage/policy", "PUT", { trashAutoPurgeDays: Number.isFinite(days) ? days : 90 });
    setStatus(r.ok ? "Storage policy saved" : `Policy failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshStorageModal();
  }

  /**
   * Removes expired items from the archive trash.
   */
  async function purgeExpiredTrash() {
    const r = await apiSend("/api/archive/trash/purge-expired", "POST");
    const rep = r.data || {};
    setStatus(r.ok ? `Purged ${rep.purged || 0} expired trash entr${(rep.purged || 0) === 1 ? "y" : "ies"}` : `Purge failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshStorageModal();
  }

  /**
   * Restores a trashed study to the main archive.
   * @param {string} uid - The study UID to restore from trash.
   */
  async function restoreTrash(uid) {
    if (!uid) return;
    const r = await apiSend(`/api/archive/trash/${encodeURIComponent(uid)}/restore`, "POST");
    setStatus(r.ok ? "Trash entry restored" : `Restore failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshStorageModal();
    reloadAll();
  }

  /**
   * Permanently removes a trash entry after user confirmation.
   */
  async function purgeTrash(uid) {
    if (!uid || !confirm("Permanently purge this trash entry?")) return;
    const r = await apiSend(`/api/archive/trash/${encodeURIComponent(uid)}`, "DELETE");
    setStatus(r.ok ? "Trash entry purged" : `Purge failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshStorageModal();
  }

  /**
   * Initiates an archive verification check.
   */
  async function verifyArchiveNow() {
    const r = await apiSend("/api/archive/verify", "POST");
    setStatus(r.ok && r.data && r.data.ok ? "Archive verification OK" : `Archive verification failed: ${r.error || "see result"}`, r.ok && r.data && r.data.ok ? "ok" : "error");
  }

  /**
   * Backs up the archive to a user-specified destination path.
   */
  async function backupArchivePath() {
    const destPath = prompt("Backup destination path:");
    if (!destPath) return;
    const r = await apiSend("/api/archive/backup-path", "POST", { destPath });
    setStatus(r.ok ? `Backup written to ${r.data.destinationDir || destPath}` : `Backup failed: ${r.error}`, r.ok ? "ok" : "error");
  }

  /**
   * Restores an archive from a backup to a user-specified destination path.
   */
  async function restoreArchivePath() {
    const backupPath = prompt("Backup path:");
    if (!backupPath) return;
    const destPath = prompt("Restore destination path:");
    if (!destPath) return;
    const r = await apiSend("/api/archive/restore-path", "POST", { backupPath, destPath, allowOverwrite: false });
    setStatus(r.ok ? `Restored to ${destPath}` : `Restore failed: ${r.error}`, r.ok ? "ok" : "error");
  }

  /**
   * Formats a byte count as a human-readable string.
   * @param {number} n - The number of bytes to format.
   * @return {string} The formatted byte count with appropriate unit (B, KB, MB, GB, or TB).
   */
  function formatBytes(n) {
    n = Number(n || 0);
    const units = ["B", "KB", "MB", "GB", "TB"];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    return `${n.toFixed(i ? 1 : 0)} ${units[i]}`;
  }

  /**
   * Decompresses DICOM files for the given study.
   * @param {object} st - The study object with a StudyInstanceUID property.
   */
  async function decompressStudy(st) {
    if (!st) return;
    setStatus("Decompressing DICOM files…");
    const r = await apiSend(`/api/archive/studies/${encodeURIComponent(st.StudyInstanceUID)}/decompress`, "POST");
    if (!r.ok) { setStatus(`Decompress failed: ${r.error}`, "error"); return; }
    const rep = r.data || {};
    setStatus(`Decompressed ${rep.DecompressedFiles ?? 0}, skipped ${rep.SkippedFiles ?? 0}, failed ${rep.FailedFiles ?? 0}`, rep.FailedFiles ? "error" : "ok");
    Object.keys(seriesCache).forEach((k) => delete seriesCache[k]);
    reloadAll();
  }

  window.inspectFirstOfSeries = async function (se) {
    const r = await apiGet(`/api/archive/series/${encodeURIComponent(se.SeriesInstanceUID)}/instances`);
    const list = (r.ok && r.data) || [];
    if (list.length) window.inspectInstance(list[0].SOPInstanceUID);
  };

  // ---- Keyboard navigation over the study list ----
  // Up/Down walk the selection through the displayed studies; Right expands the
  // selected study's series, Left collapses it. Inactive while a text field has
  // focus, a modal is open, or this tab is not active.
  function scrollSelectedIntoView() {
    const row = document.querySelector("#arc-rows tr.study.selected");
    if (row) row.scrollIntoView({ block: "nearest" });
  }
  function keyNav(e) {
    if (!panel || !panel.classList.contains("active")) return;
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (!["ArrowDown", "ArrowUp", "ArrowRight", "ArrowLeft"].includes(e.key)) return;
    const ae = document.activeElement;
    if (ae && /^(INPUT|TEXTAREA|SELECT)$/.test(ae.tagName)) return;
    if (document.getElementById("modal-backdrop")) return;
    if (!displayed.length) return;
    e.preventDefault();
    const i = selectedStudy ? displayed.findIndex((s) => s.StudyInstanceUID === selectedStudy.StudyInstanceUID) : -1;
    if (e.key === "ArrowDown") {
      select(displayed[i < 0 ? 0 : Math.min(i + 1, displayed.length - 1)]);
      scrollSelectedIntoView();
    } else if (e.key === "ArrowUp") {
      select(displayed[i <= 0 ? 0 : i - 1]);
      scrollSelectedIntoView();
    } else if (e.key === "ArrowRight") {
      if (selectedStudy && !expanded.has(selectedStudy.StudyInstanceUID)) toggleSeries(selectedStudy);
    } else if (e.key === "ArrowLeft") {
      if (selectedStudy && expanded.has(selectedStudy.StudyInstanceUID)) toggleSeries(selectedStudy);
    }
  }

  return {
    init(p) { panel = p; render(); document.addEventListener("keydown", keyNav); },
    refresh() { if (panel) reloadAll(); },
    search(text) { search.text = (text || "").trim(); if (panel) applyFilters(); },
  };
})();
