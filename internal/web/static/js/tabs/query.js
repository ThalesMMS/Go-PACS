// Query/Retrieve tab — dense layout: search-field tabs + node selection table +
// date-preset / modality filter grids + Retrieve-to destination + Query / Query
// Patient / Retrieve / Verify, then an expandable results table. This tab also
// folds in the auto-query (scheduler) controls: a profile ("instance") selector,
// refresh interval, Auto-Retrieve toggle and Start / Stop / Save, reusing the
// same field tabs / node selection / date / modality controls to build profiles.
window.TABS.query = (function () {
  let panel;
  let sources = [];
  let matches = [];
  let selected = null;
  let activeStream = null;
  // Per-row expand state for the results table: which study UIDs are expanded,
  // plus a cache of each study's series subquery results so re-expanding is free.
  const expanded = new Set();
  const seriesCache = new Map();
  // Auto-query state.
  let profiles = [];
  let idx = -1;

  const FIELDS = [
    ["Name", "patientName"], ["Patient ID", "patientID"], ["Accession Number", "accessionNumber"],
    ["Birthdate", "patientBirthDate"], ["Description", "studyDescription"],
    ["Referring Physician", "referringPhysicianName"], ["Institution", "institutionName"],
    ["Custom DICOM field", "customFieldValue"], ["Status", "studyStatusID"],
  ];
  const DATE_PRESETS = [
    ["Any date", "any"], ["Today", "today"], ["Today AM", "todayAM"], ["Today PM", "todayPM"],
    ["Yesterday", "yesterday"], ["Day Before Yesterday", "dby"],
    ["Last 2 days", "2"], ["Last 7 days", "7"], ["Last month", "30"], ["Last 3 months", "90"],
  ];
  const MODALITIES = ["CR", "CT", "MR", "US", "MG", "NM", "PT", "XA", "DX", "RF", "SC", "XC", "OT", "SR", "ES", "RG"];
  let activeField = "patientName";
  let activeDate = "any";

  function cur() { return idx >= 0 ? profiles[idx] : null; }

  // Column model for the results table. _exp (expand chevron) and _dl (download)
  // are fixed control columns — never resized or moved; the rest are data columns
  // that the user can resize and reorder (persisted under tableKey "query").
  const QCOLS = [
    { key: "_exp", label: "", width: 30, fixed: true },
    { key: "patientName", label: "Patient Name", width: 170, get: (m) => m.PatientName || "" },
    { key: "modality", label: "Modality", width: 90, get: (m) => m.Modality || m.Modalities || "" },
    { key: "im", label: "# im", width: 60, num: true, get: (m) => m.ImageCount || "" },
    { key: "date", label: "Date", width: 95, get: (m) => m.StudyDate || "" },
    { key: "time", label: "Time", width: 85, get: (m) => m.StudyTime || "" },
    { key: "description", label: "Description", width: 200, get: (m) => m.StudyDescription || "" },
    { key: "patientID", label: "Patient ID", width: 130, get: (m) => m.PatientID || "" },
    { key: "dob", label: "Date of Birth", width: 110, get: (m) => m.PatientBirthDate || "" },
    { key: "source", label: "Source", width: 130, get: (m) => m.SourceNodeName || "" },
    { key: "accession", label: "Accession", width: 120, get: (m) => m.AccessionNumber || "" },
    { key: "_dl", label: "", width: 40, fixed: true },
  ];
  function qColumns() { return orderedColumns(QCOLS, "query"); }
  function buildResultsTable() {
    const oc = qColumns();
    const table = el("table", { class: "grid coltable" },
      el("colgroup", null, ...oc.map((c) => el("col", { style: "width:" + c.width + "px" }))),
      el("thead", null, el("tr", null, ...oc.map((c) => el("th", { class: c.num ? "num" : "" }, c.label)))),
      el("tbody", { id: "q-results" }));
    enhanceTableColumns(table, oc, { tableKey: "query", onReorder: rebuildResultsTable });
    return table;
  }
  function rebuildResultsTable() {
    const old = panel && panel.querySelector("table.grid.coltable");
    if (!old || !old.parentNode) return;
    old.parentNode.replaceChild(buildResultsTable(), old);
    renderResults();
  }

  function render() {
    panel.innerHTML = "";
    panel.appendChild(controlsCard());
    panel.appendChild(autoQueryCard());
    panel.appendChild(el("section", { class: "card" },
      el("div", { class: "row" },
        el("h2", { class: "grow", id: "q-summary" }, "Results"),
        el("span", { id: "q-progress", class: "muted" })),
      buildResultsTable()));
    load();
  }

  function controlsCard() {
    const p = cur();
    return el("section", { class: "card" },
      el("div", { class: "fieldtabs", id: "q-fieldtabs" },
        ...FIELDS.map(([t, v]) => el("button", { class: v === activeField ? "active" : "", onclick: () => { activeField = v; render(); } }, t))),
      el("div", { class: "row", style: "margin:10px 0" },
        el("input", { id: "q-text", placeholder: FIELDS.find((f) => f[1] === activeField)[0], value: (p && p.criteria && p.criteria.searchText) || "", style: "min-width:300px",
          onkeydown: (e) => { if (e.key === "Enter") runQuery("study"); } })),
      el("div", { class: "qr-controls" },
        el("div", null, el("h2", null, "DICOM Nodes"),
          el("table", { class: "grid" },
            el("thead", null, el("tr", null, el("th", null, ""), el("th", null, "Name"), el("th", null, "Address"), el("th", null, "AE Title"))),
            el("tbody", { id: "q-sources" }))),
        el("div", null, el("h2", null, "Date"),
          el("div", { class: "dateopts", id: "q-dates" },
            ...DATE_PRESETS.map(([t, v]) => el("label", null, radio("q-date", v, v === activeDate, () => { activeDate = v; }), t)))),
        el("div", null, el("h2", null, "Modality"),
          el("div", { class: "checkgrid", id: "q-mods" },
            ...MODALITIES.map((m) => {
              const checked = p && p.criteria && (p.criteria.modalities || []).includes(m);
              return el("label", null, chk(m, checked), m);
            })))),
      el("div", { class: "row", style: "margin-top:12px" },
        el("label", { class: "field", style: "flex-direction:row;align-items:center;gap:6px" }, "Retrieve to",
          el("select", { id: "q-dest" }, el("option", null, "This Computer (local archive)"))),
        el("span", { class: "grow" }),
        el("button", { class: "btn", onclick: () => runQuery(document.getElementById("q-level") ? document.getElementById("q-level").value : "study") }, "Query"),
        el("button", { class: "btn secondary", onclick: () => runQuery("study") }, "Query Patient"),
        el("button", { class: "btn", id: "q-retrieve", disabled: true, onclick: retrieveSelected }, "Retrieve"),
        el("button", { class: "btn danger", id: "q-cancel", style: "display:none", onclick: cancelRetrieve }, "Cancel"),
        el("button", { class: "btn secondary", onclick: verifySources }, "Verify"),
        el("select", { id: "q-level", title: "Query level" }, el("option", { value: "study" }, "Study"), el("option", { value: "series" }, "Series"), el("option", { value: "image" }, "Image"))));
  }

  // autoQueryCard folds the former Auto Query tab into this screen: a profile
  // ("instance") selector, add/remove, refresh interval, Auto-Retrieve toggle,
  // Start / Stop / Save, and a scheduler status line. It reuses the main field
  // tabs / DICOM Nodes / Date / Modality controls to build the saved profile.
  function autoQueryCard() {
    return el("section", { class: "card" },
      el("div", { class: "row" },
        el("h2", { class: "grow" }, "Auto Query"),
        el("span", { id: "q-sched", class: "muted" })),
      el("div", { class: "row", style: "margin-top:8px" },
        el("label", { class: "field", style: "flex-direction:row;align-items:center;gap:6px" }, "Instance",
          el("select", { id: "q-profile", onchange: (e) => { idx = parseInt(e.target.value, 10); render(); } })),
        el("button", { class: "btn secondary", onclick: addProfile }, "+"),
        el("button", { class: "btn secondary", onclick: removeProfile }, "−"),
        el("span", { class: "grow" }),
        el("label", { class: "field", style: "flex-direction:row;align-items:center;gap:6px" }, "Refresh every (s)",
          el("input", { id: "q-interval", type: "number", value: "300", style: "width:90px" })),
        autoRetrieveToggle(),
        el("button", { class: "btn", onclick: startScheduler }, "Start"),
        el("button", { class: "btn danger", onclick: stopScheduler }, "Stop"),
        el("button", { class: "btn secondary", onclick: saveProfile }, "Save")));
  }

  function autoRetrieveToggle() {
    const p = cur();
    const tg = toggle("Auto-Retrieve", p && p.settings && p.settings.autoRetrieve);
    tg.input.addEventListener("change", () => { if (cur()) { cur().settings = cur().settings || {}; cur().settings.autoRetrieve = tg.input.checked; } });
    tg.input.id = "q-autoretrieve";
    return tg.wrap;
  }

  function radio(name, value, checked, onchange) { const r = el("input", { type: "radio", name, value, onchange }); r.checked = checked; return r; }
  function chk(value, checked) { const c = el("input", { type: "checkbox", value }); c.checked = !!checked; return c; }

  // load fetches both the query source nodes and the auto-query profiles, then
  // wires the instance selector, source checkboxes and scheduler status.
  async function load() {
    const [nd, pr] = await Promise.all([apiGet("/api/query/sources"), apiGet("/api/autoquery/profiles")]);
    sources = (nd.ok && nd.data) || [];
    profiles = (pr.ok && pr.data) || [];
    if (idx < 0 && profiles.length) idx = 0;
    const sel = document.getElementById("q-profile");
    if (sel) {
      sel.innerHTML = "";
      profiles.forEach((p, i) => sel.appendChild(el("option", { value: i, selected: i === idx }, p.name || `Instance ${i + 1}`)));
      if (!profiles.length) sel.appendChild(el("option", { value: -1 }, "(no instances)"));
    }
    renderSources();
    refreshSched();
  }

  function renderSources() {
    const body = document.getElementById("q-sources");
    if (!body) return;
    body.innerHTML = "";
    if (!sources.length) { body.appendChild(el("tr", null, el("td", { colspan: "4", class: "faint" }, "no query nodes — add in Network"))); return; }
    const p = cur();
    for (const n of sources) {
      // When a profile is selected, default the checkboxes to that profile's
      // enabled sources; otherwise default everything checked (manual query).
      const inProfile = p && (p.sources || []).some((s) => (s.nodeID === n.id || s.name === n.name) && s.enabled);
      const cb = el("input", { type: "checkbox", value: n.id }); cb.checked = p ? !!inProfile : true;
      body.appendChild(el("tr", null, el("td", { class: "c" }, cb), el("td", null, n.name || ""), el("td", null, `${n.host}:${n.port}`), el("td", null, n.aeTitle || "")));
    }
  }

  function selectedSourceIDs() { return [...document.querySelectorAll("#q-sources input:checked")].map((c) => c.value); }
  function selectedModalities() { return [...document.querySelectorAll("#q-mods input:checked")].map((c) => c.value); }

  function dateRange() {
    const fmt = (d) => `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`;
    const today = new Date();
    const daysAgo = (n) => { const d = new Date(); d.setDate(d.getDate() - n); return d; };
    switch (activeDate) {
      case "any": return {};
      case "today": return { studyDateFrom: fmt(today), studyDateTo: fmt(today) };
      case "todayAM": return { studyDateFrom: fmt(today), studyDateTo: fmt(today), studyTimeFrom: "000000", studyTimeTo: "115959" };
      case "todayPM": return { studyDateFrom: fmt(today), studyDateTo: fmt(today), studyTimeFrom: "120000", studyTimeTo: "235959" };
      case "yesterday": return { studyDateFrom: fmt(daysAgo(1)), studyDateTo: fmt(daysAgo(1)) };
      case "dby": return { studyDateFrom: fmt(daysAgo(2)), studyDateTo: fmt(daysAgo(2)) };
      default: return { studyDateFrom: fmt(daysAgo(parseInt(activeDate, 10) || 0)), studyDateTo: fmt(today) };
    }
  }

  function criteria() {
    const c = Object.assign({ [activeField]: (document.getElementById("q-text").value || "").trim() }, dateRange());
    const mods = selectedModalities();
    if (mods.length) c.modality = mods[0];
    return c;
  }

  async function runQuery(level) {
    setStatus(`Querying (${level})…`);
    const r = await apiSend(`/api/query/${level}`, "POST", { criteria: criteria(), sourceIDs: selectedSourceIDs() });
    if (!r.ok) { setStatus(`Query failed: ${r.error}`, "error"); return; }
    matches = (r.data && r.data.matches) || [];
    // A fresh result set invalidates any prior per-row expansion / series cache.
    expanded.clear();
    seriesCache.clear();
    renderResults();
    const failures = (r.data && r.data.failures) || [];
    setStatus(failures.length ? `${matches.length} matches; ${failures.length} source failure(s)` : `${matches.length} matches`, failures.length ? "error" : "ok");
  }

  function renderResults() {
    selected = null;
    const rbtn = document.getElementById("q-retrieve"); if (rbtn) rbtn.disabled = true;
    document.getElementById("q-summary").textContent = `Results (${matches.length})`;
    const body = document.getElementById("q-results");
    if (!body) return;
    body.innerHTML = "";
    const oc = qColumns();
    if (!matches.length) { body.appendChild(el("tr", null, el("td", { colspan: String(oc.length), class: "empty" }, "no matches"))); return; }
    matches.forEach((m, i) => {
      const isOpen = expanded.has(m.StudyInstanceUID);
      const row = el("tr", { onclick: () => selectMatch(i, row) });
      for (const c of oc) {
        if (c.key === "_exp") {
          row.appendChild(el("td", { class: "c" }, el("span", { class: isOpen ? "disclose open" : "disclose",
            onclick: (e) => { e.stopPropagation(); toggleExpand(m); } }, "▶")));
        } else if (c.key === "_dl") {
          row.appendChild(el("td", { class: "c" }, el("button", { class: "q-dl", title: "Retrieve this study",
            onclick: (e) => { e.stopPropagation(); retrieveMatch(m, "study"); } }, "↓")));
        } else {
          row.appendChild(el("td", { class: c.num ? "num" : "" }, c.get(m)));
        }
      }
      body.appendChild(row);
      if (isOpen) renderSeriesRows(m, row);
    });
  }

  // toggleExpand flips a study row's expansion. On first expand it runs a series
  // subquery (cached thereafter) and renders indented series sub-rows.
  async function toggleExpand(m) {
    const uid = m.StudyInstanceUID;
    if (expanded.has(uid)) { expanded.delete(uid); removeSeriesRows(uid); return; }
    expanded.add(uid);
    const row = studyRowFor(uid);
    if (seriesCache.has(uid)) { if (row) renderSeriesRows(m, row); return; }
    // Show a transient placeholder while the subquery runs.
    if (row) {
      const loading = el("tr", { class: "subrow", "data-study": uid },
        el("td", null, ""), el("td", { colspan: String(qColumns().length - 1), class: "faint" }, "querying series…"));
      row.after(loading);
    }
    setStatus("Querying series…");
    const resp = await apiSend("/api/query/series", "POST", {
      criteria: { StudyInstanceUID: uid, PatientID: m.PatientID },
      sourceIDs: [m.SourceNodeID].filter(Boolean),
    });
    removeSeriesRows(uid);
    if (!expanded.has(uid)) return; // user collapsed while it was loading
    const cur2 = studyRowFor(uid);
    if (!resp.ok) {
      seriesCache.set(uid, []);
      if (cur2) cur2.after(el("tr", { class: "subrow", "data-study": uid },
        el("td", null, ""), el("td", { colspan: String(qColumns().length - 1), class: "faint" }, `series query failed: ${resp.error || "error"}`)));
      setStatus(`Series query failed: ${resp.error || "error"}`, "error");
      return;
    }
    const series = (resp.data && resp.data.matches) || [];
    seriesCache.set(uid, series);
    if (cur2) renderSeriesRows(m, cur2);
    setStatus(`${series.length} series`, "ok");
  }

  function studyRowFor(uid) {
    const i = matches.findIndex((x) => x.StudyInstanceUID === uid);
    if (i < 0) return null;
    return document.querySelectorAll("#q-results > tr:not(.subrow)")[i] || null;
  }

  function removeSeriesRows(uid) {
    for (const tr of document.querySelectorAll(`#q-results tr.subrow[data-study="${cssEscape(uid)}"]`)) tr.remove();
  }

  // renderSeriesRows inserts cached series sub-rows immediately after a study row.
  function renderSeriesRows(m, afterRow) {
    const uid = m.StudyInstanceUID;
    const series = seriesCache.get(uid) || [];
    const mid = Math.max(1, qColumns().length - 2); // span the data columns; control cols stay pinned
    if (!series.length) {
      afterRow.after(el("tr", { class: "subrow", "data-study": uid },
        el("td", null, ""), el("td", { colspan: String(mid), class: "faint" }, "no series"), el("td", null, "")));
      return;
    }
    // Insert in order so the visual sequence matches the series array.
    let anchor = afterRow;
    for (const sm of series) {
      const dl = el("button", { class: "q-dl", title: "Retrieve this series",
        onclick: (e) => { e.stopPropagation(); retrieveMatch(sm, "series"); } }, "↓");
      const label = [sm.SeriesDescription || "(no description)", sm.Modality || "",
        sm.SeriesNumber ? `#${sm.SeriesNumber}` : "", sm.ImageCount ? `${sm.ImageCount} im` : "",
        sm.SeriesInstanceUID || ""].filter(Boolean).join(" · ");
      const sub = el("tr", { class: "subrow", "data-study": uid },
        el("td", null, ""),
        el("td", { colspan: String(mid) }, label),
        el("td", { class: "c" }, dl));
      anchor.after(sub);
      anchor = sub;
    }
  }

  // cssEscape quotes a UID for use inside an attribute selector (CSS.escape
  // fallback for older webviews).
  function cssEscape(v) {
    return window.CSS && CSS.escape ? CSS.escape(v) : String(v).replace(/["\\]/g, "\\$&");
  }

  function selectMatch(i, row) {
    selected = { match: matches[i], level: (document.getElementById("q-level") || {}).value || "study" };
    for (const tr of document.querySelectorAll("#q-results tr")) tr.classList.remove("selected");
    if (row) row.classList.add("selected");
    document.getElementById("q-retrieve").disabled = false;
  }

  async function verifySources() {
    for (const id of selectedSourceIDs()) await apiSend("/api/echo", "POST", { nodeID: id });
    setStatus("Verified selected sources", "ok");
  }

  function retrieveSelected() {
    if (!selected) return;
    retrieveMatch(selected.match, selected.level);
  }

  // retrieveMatch posts a C-MOVE for one match at the given level and streams the
  // job. Shared by the toolbar "Retrieve" button and the per-row green ↓ buttons
  // (study and series sub-rows).
  async function retrieveMatch(m, level) {
    if (!m) return;
    const lvl = (level || "study").toUpperCase();
    const btn = document.getElementById("q-retrieve");
    const cancel = document.getElementById("q-cancel");
    const progress = document.getElementById("q-progress");
    if (btn) btn.disabled = true;
    setStatus(`Retrieving from ${m.SourceNodeName || ""}…`);
    const r = await apiSend("/api/query/retrieve", "POST", {
      nodeID: m.SourceNodeID || m.SourceNodeName, level: lvl,
      studyUID: m.StudyInstanceUID, seriesUID: m.SeriesInstanceUID, sopUID: m.SOPInstanceUID,
    });
    if (!r.ok || !r.data || !r.data.jobID) { if (btn) btn.disabled = false; setStatus(`Retrieve failed to start: ${r.error || "no job"}`, "error"); return; }
    if (cancel) cancel.style.display = "";
    const finish = (msg, kind) => { if (btn) btn.disabled = false; if (cancel) cancel.style.display = "none"; if (progress) progress.textContent = ""; activeStream = null; setStatus(msg, kind); };
    activeStream = streamJob(r.data.jobID, {
      onProgress: (p) => { if (!progress) return; const done = (p.Completed ?? 0) + (p.Failed ?? 0) + (p.Warnings ?? 0); const total = done + (p.Remaining ?? 0); progress.textContent = total ? `${done}/${total}` : "working…"; },
      onDone: (o) => finish(`Retrieve complete: stored ${(o && o.Stored) ?? 0}, failed ${(o && o.Failed) ?? 0}`, "ok"),
      onError: (msg) => finish(`Retrieve failed: ${msg}`, "error"),
    });
  }
  async function cancelRetrieve() { if (activeStream) { await activeStream.cancel(); setStatus("Cancelling retrieve…"); } }

  // --- Auto-query (scheduler) ---------------------------------------------

  // gather builds an auto-query profile from the shared controls, preserving the
  // JSON shape the backend expects: name, settings.{autoRetrieve,retrieveLevel},
  // criteria {searchField, searchText, onDate, modalities}, sources [{...}].
  function gather() {
    const p = cur() || { name: "Default Instance", settings: { retrieveLevel: "STUDY" }, criteria: {}, sources: [] };
    p.criteria = p.criteria || {};
    p.criteria.searchField = activeField;
    p.criteria.searchText = (document.getElementById("q-text").value || "").trim();
    if (activeDate === "today") { const d = new Date(); p.criteria.onDate = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`; }
    else p.criteria.onDate = "";
    p.criteria.modalities = selectedModalities();
    p.sources = [...document.querySelectorAll("#q-sources input:checked")].map((c) => {
      const n = sources.find((s) => s.id === c.value) || {};
      return { nodeID: n.id, name: n.name, host: n.host, port: n.port, enabled: true };
    });
    const ar = document.getElementById("q-autoretrieve");
    p.settings = p.settings || {}; if (ar) p.settings.autoRetrieve = ar.checked;
    return p;
  }

  function addProfile() { profiles.push({ name: "New Instance", settings: { retrieveLevel: "STUDY" }, criteria: {}, sources: [] }); idx = profiles.length - 1; render(); }
  function removeProfile() { if (idx < 0) return; profiles.splice(idx, 1); idx = profiles.length ? 0 : -1; render(); }
  async function saveProfile() {
    if (idx >= 0) profiles[idx] = gather();
    const r = await apiSend("/api/autoquery/profiles", "PUT", profiles);
    if (r.ok) { setStatus("Instances saved", "ok"); profiles = r.data || profiles; load(); } else setStatus(`Save failed: ${r.error}`, "error");
  }

  async function startScheduler() {
    const interval = parseInt(document.getElementById("q-interval").value, 10) || 300;
    const r = await apiSend("/api/autoquery/start", "POST", { profile: gather(), intervalSeconds: interval });
    setStatus(r.ok ? `Auto-query started (${interval}s)` : `Start failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshSched();
  }
  async function stopScheduler() {
    const r = await apiSend("/api/autoquery/stop", "POST");
    setStatus(r.ok ? "Auto-query stopped" : `Stop failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshSched();
  }
  async function refreshSched() {
    const r = await apiGet("/api/autoquery/status");
    const box = document.getElementById("q-sched");
    if (!box) return;
    const st = (r.ok && r.data) || {};
    box.innerHTML = st.running
      ? `<span class="pill ok">Auto</span> every ${st.intervalSeconds}s · last ${st.lastMatches ?? 0} matches, retrieved ${st.lastRetrieved ?? 0}`
      : '<span class="pill idle">Idle</span>';
  }

  return {
    init(p) { panel = p; render(); },
    refresh() { if (panel) load(); },
  };
})();
