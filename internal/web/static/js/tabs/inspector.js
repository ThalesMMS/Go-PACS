// Inspector tab: DICOM element inspection of an archived instance by SOP
// Instance UID. Backed by GET /api/archive/instances/{sopUID}/inspect.
window.TABS.inspector = (function () {
  let panel;
  let pending = null;
  let elements = [];
  let sortKey = "tag";
  let sortAsc = true;

  function render() {
    panel.innerHTML = "";
    panel.appendChild(el("section", { class: "card" },
      el("div", { class: "row" },
        el("input", { id: "insp-sop", placeholder: "SOP Instance UID", style: "min-width:320px",
          onkeydown: (e) => { if (e.key === "Enter") loadFromInput(); } }),
        el("button", { class: "btn", onclick: loadFromInput }, "Inspect"),
        el("input", { id: "insp-filter", placeholder: "Filter elements…", style: "min-width:200px",
          oninput: renderElements }),
      ),
      el("pre", { class: "detail", id: "insp-header", style: "margin-top:12px" }, "Enter a SOP Instance UID or use Inspect from the Archive tab."),
    ));
    panel.appendChild(el("section", { class: "card" },
      el("h2", null, "Elements"),
      el("table", { class: "grid" },
        el("thead", null, el("tr", null,
          th("Tag", "tag"), th("VR", "vr"), th("Keyword", "keyword"), th("Length", "length"), el("th", null, "Value"))),
        el("tbody", { id: "insp-elements" })),
    ));
    if (pending) { document.getElementById("insp-sop").value = pending; doLoad(pending); pending = null; }
  }

  function th(label, key) {
    return el("th", { onclick: () => { if (sortKey === key) sortAsc = !sortAsc; else { sortKey = key; sortAsc = true; } renderElements(); } },
      label + (sortKey === key ? (sortAsc ? " ▲" : " ▼") : ""));
  }

  function loadFromInput() {
    const sop = document.getElementById("insp-sop").value.trim();
    if (sop) doLoad(sop);
  }

  async function doLoad(sop) {
    setStatus(`Inspecting ${sop}…`);
    const r = await apiGet(`/api/archive/instances/${encodeURIComponent(sop)}/inspect`);
    const header = document.getElementById("insp-header");
    if (!r.ok) { header.textContent = "error: " + r.error; elements = []; renderElements(); setStatus(r.error, "error"); return; }
    const s = r.data || {};
    elements = s.elements || [];
    header.textContent =
      `Patient: ${s.patientName || "-"}  (${s.patientID || "-"})\n` +
      `Study: ${s.studyDescription || "-"}  ${s.studyDate || ""}\n` +
      `Modality: ${s.modality || "-"}   SOP Class: ${s.sopClassUID || "-"}\n` +
      `Transfer Syntax: ${s.transferSyntax || s.transferSyntaxUID || "-"}\n` +
      `Elements: ${s.elementCount ?? elements.length}`;
    renderElements();
    setStatus(`Inspected ${elements.length} elements`, "ok");
  }

  function renderElements() {
    const body = document.getElementById("insp-elements");
    if (!body) return;
    const filter = (document.getElementById("insp-filter").value || "").toLowerCase();
    let rows = elements.slice();
    if (filter) {
      rows = rows.filter((e) =>
        [e.tag, e.vr, e.keyword, e.name, e.value].some((v) => String(v || "").toLowerCase().includes(filter)));
    }
    rows.sort((a, b) => {
      const av = String(a[sortKey] || ""), bv = String(b[sortKey] || "");
      return sortAsc ? av.localeCompare(bv) : bv.localeCompare(av);
    });
    body.innerHTML = "";
    if (!rows.length) { body.appendChild(el("tr", null, el("td", { colspan: "5", class: "empty" }, "no elements"))); return; }
    for (const e of rows) {
      body.appendChild(el("tr", null,
        el("td", { class: "muted" }, e.tag || ""), el("td", null, e.vr || ""),
        el("td", null, e.keyword || e.name || ""), el("td", null, e.length || ""),
        el("td", null, e.value || "")));
    }
  }

  return {
    init(p) { panel = p; render(); },
    refresh() {},
    load(sop) { pending = sop; if (panel) { document.getElementById("insp-sop").value = sop; doLoad(sop); } },
  };
})();

// Cross-tab helper: jump to the Inspector tab and load a SOP Instance UID.
window.inspectInstance = function (sop) {
  activateTab("inspector");
  window.TABS.inspector.load(sop);
};
