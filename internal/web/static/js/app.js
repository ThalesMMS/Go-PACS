// Shared helpers and tab shell for the go-pacs web frontend.
// Each tab module registers itself on window.TABS[name] = { init(panel), refresh() }.
// app.js wires the top nav to panels and lazily initializes each tab on first view.

window.TABS = window.TABS || {};

// --- API helpers ---------------------------------------------------------

async function api(path, opts) {
  const res = await fetch(path, opts);
  let body;
  try {
    body = await res.json();
  } catch {
    return { ok: false, error: `HTTP ${res.status}` };
  }
  return body;
}

async function apiGet(path) {
  return api(path);
}

async function apiSend(path, method, data) {
  return api(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: data === undefined ? undefined : JSON.stringify(data),
  });
}

function escapeHTML(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => (
    { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]
  ));
}

function el(tag, attrs, ...children) {
  const node = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") node.className = v;
      else if (k === "html") node.innerHTML = v;
      else if (k.startsWith("on") && typeof v === "function") node.addEventListener(k.slice(2), v);
      else if (v !== null && v !== undefined && v !== false) node.setAttribute(k, v === true ? "" : v);
    }
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return node;
}

// streamJob subscribes to an async job's SSE events. handlers: onProgress(data),
// onDone(data), onError(message, data). Returns { cancel, close }.
function streamJob(jobID, handlers) {
  const url = `/api/jobs/${encodeURIComponent(jobID)}/events`;
  const es = new EventSource(url);
  es.addEventListener("progress", (e) => {
    try { handlers.onProgress && handlers.onProgress(JSON.parse(e.data).data); } catch { /* ignore */ }
  });
  es.addEventListener("done", (e) => {
    es.close();
    try { handlers.onDone && handlers.onDone(JSON.parse(e.data).data); } catch { /* ignore */ }
  });
  es.addEventListener("error", (e) => {
    // A named "error" event from the server carries data; a transport error does not.
    if (e && e.data) {
      es.close();
      try { const ev = JSON.parse(e.data); handlers.onError && handlers.onError(ev.error, ev.data); } catch { /* ignore */ }
    }
  });
  return {
    cancel: async () => { await apiSend(`/api/jobs/${encodeURIComponent(jobID)}/cancel`, "POST"); },
    close: () => es.close(),
  };
}

// --- Modal + macOS toggle helpers ---------------------------------------

function openModal(node) {
  closeModal();
  const backdrop = el("div", {
    class: "modal-backdrop", id: "modal-backdrop",
    onclick: (e) => { if (e.target.id === "modal-backdrop") closeModal(); },
  }, el("div", { class: "modal" }, node));
  document.body.appendChild(backdrop);
  document.addEventListener("keydown", escClose);
}
function closeModal() {
  const b = document.getElementById("modal-backdrop");
  if (b) b.remove();
  document.removeEventListener("keydown", escClose);
}
function escClose(e) { if (e.key === "Escape") closeModal(); }

// modalRow builds a label-left / control-right row for modal forms.
function modalRow(label, control) {
  return el("div", { class: "mrow" }, el("label", { class: "lbl" }, label), el("div", { class: "ctl" }, control));
}

// toggle builds a macOS switch. Returns { wrap, input }.
function toggle(label, checked) {
  const input = el("input", { type: "checkbox" });
  input.checked = !!checked;
  const wrap = el("label", { class: "toggle" }, input, el("span", { class: "track" }), label || "");
  return { wrap, input };
}

let statusTimer = null;
function setStatus(msg, kind) {
  const line = document.getElementById("statusline");
  if (!line) return;
  line.textContent = msg || "";
  line.className = kind || "";
  if (statusTimer) clearTimeout(statusTimer);
  if (kind === "ok") {
    statusTimer = setTimeout(() => { line.textContent = "Ready"; line.className = ""; }, 4000);
  }
}

// --- Connection indicator ------------------------------------------------

async function pollConnection() {
  const conn = document.getElementById("conn");
  const r = await apiGet("/api/config");
  if (conn) conn.className = "conn " + (r && r.ok ? "online" : "offline");
}

// --- Tab navigation ------------------------------------------------------

const initialized = new Set();

function activateTab(name) {
  for (const btn of document.querySelectorAll("#nav button")) {
    btn.classList.toggle("active", btn.dataset.tab === name);
  }
  for (const panel of document.querySelectorAll(".panel")) {
    panel.classList.toggle("active", panel.id === `panel-${name}`);
  }
  const mod = window.TABS[name];
  const panel = document.getElementById(`panel-${name}`);
  if (mod && panel && !initialized.has(name)) {
    initialized.add(name);
    try { mod.init(panel); } catch (e) { setStatus(`Failed to init ${name}: ${e}`, "error"); }
  } else if (mod && mod.refresh) {
    try { mod.refresh(); } catch { /* ignore */ }
  }
  location.hash = name;
}

function bootShell() {
  for (const btn of document.querySelectorAll("#nav button")) {
    btn.addEventListener("click", () => activateTab(btn.dataset.tab));
  }
  const search = document.getElementById("global-search");
  if (search) {
    search.addEventListener("keydown", (e) => {
      if (e.key !== "Enter") return;
      activateTab("archive");
      const mod = window.TABS.archive;
      if (mod && mod.search) mod.search(search.value);
    });
  }
  const start = (location.hash || "#archive").slice(1);
  const known = [...document.querySelectorAll("#nav button")].map((b) => b.dataset.tab);
  activateTab(known.includes(start) ? start : known[0]);
  pollConnection();
  setInterval(pollConnection, 15000);
}

document.addEventListener("DOMContentLoaded", bootShell);

// --- Resizable + reorderable table columns -------------------------------
// A tab renders its table from an ordered list of column descriptors plus a
// <colgroup> (one <col> per column, same order) and then calls
// enhanceTableColumns(table, columns, {tableKey, onReorder}). This applies any
// persisted column widths to the <col>s and wires per-header resize handles and
// drag-to-reorder. Order + widths persist per tableKey in localStorage so they
// survive re-renders and reloads. Columns flagged {fixed:true} (e.g. control
// columns) are neither resizable nor movable. Tables must use table-layout:fixed
// (see app.css) so the <col> widths are authoritative.

function colState(tableKey) {
  try { return JSON.parse(localStorage.getItem("cols:" + tableKey)) || {}; } catch { return {}; }
}
function saveColState(tableKey, state) {
  try { localStorage.setItem("cols:" + tableKey, JSON.stringify(state)); } catch { /* ignore */ }
}

// orderedColumns returns `columns` reordered to match the persisted order for
// tableKey. Fixed columns are pinned at their original index; movable columns
// follow the saved order, with any new (unsaved) movable columns kept at the end.
function orderedColumns(columns, tableKey) {
  const saved = (colState(tableKey).order) || [];
  if (!saved.length) return columns.slice();
  const movable = columns.filter((c) => !c.fixed);
  const byKey = new Map(movable.map((c) => [c.key, c]));
  const seq = [];
  for (const k of saved) if (byKey.has(k)) { seq.push(byKey.get(k)); byKey.delete(k); }
  for (const c of movable) if (byKey.has(c.key)) seq.push(c);
  const out = seq.slice();
  columns.forEach((c, i) => { if (c.fixed) out.splice(i, 0, c); });
  return out;
}

function enhanceTableColumns(table, columns, opts) {
  const tableKey = opts.tableKey;
  const st = colState(tableKey);
  const cols = [...table.querySelectorAll("colgroup > col")];
  const ths = [...table.querySelectorAll("thead > tr > th")];
  columns.forEach((c, i) => {
    const w = st.widths && st.widths[c.key];
    if (w && cols[i]) cols[i].style.width = w + "px";
    const th = ths[i];
    if (!th || c.fixed) return;
    th.classList.add("col-h");
    const handle = el("span", { class: "col-resize", title: "Drag to resize" });
    handle.addEventListener("mousedown", (e) => beginColResize(e, cols[i], c.key, tableKey));
    handle.addEventListener("click", (e) => e.stopPropagation());
    th.appendChild(handle);
    th.setAttribute("draggable", "true");
    th.addEventListener("dragstart", (e) => { e.dataTransfer.setData("text/col", c.key); e.dataTransfer.effectAllowed = "move"; });
    th.addEventListener("dragover", (e) => { e.preventDefault(); th.classList.add("col-drop"); });
    th.addEventListener("dragleave", () => th.classList.remove("col-drop"));
    th.addEventListener("drop", (e) => {
      e.preventDefault();
      th.classList.remove("col-drop");
      const from = e.dataTransfer.getData("text/col");
      const to = c.key;
      if (!from || from === to) return;
      const order = columns.filter((x) => !x.fixed).map((x) => x.key);
      const fi = order.indexOf(from), ti = order.indexOf(to);
      if (fi < 0 || ti < 0) return;
      order.splice(ti, 0, order.splice(fi, 1)[0]);
      const s = colState(tableKey); s.order = order; saveColState(tableKey, s);
      if (opts.onReorder) opts.onReorder();
    });
  });
}

function beginColResize(e, col, key, tableKey) {
  if (!col) return;
  e.preventDefault();
  e.stopPropagation();
  const th = e.target.closest("th");
  if (th) th.draggable = false; // don't start a header drag while resizing
  const startX = e.clientX;
  const startW = col.getBoundingClientRect().width;
  document.body.classList.add("col-resizing");
  const onMove = (ev) => { col.style.width = Math.max(44, startW + (ev.clientX - startX)) + "px"; };
  const onUp = () => {
    document.removeEventListener("mousemove", onMove);
    document.removeEventListener("mouseup", onUp);
    document.body.classList.remove("col-resizing");
    if (th) th.draggable = true;
    const s = colState(tableKey);
    s.widths = s.widths || {};
    s.widths[key] = Math.round(col.getBoundingClientRect().width);
    saveColState(tableKey, s);
  };
  document.addEventListener("mousemove", onMove);
  document.addEventListener("mouseup", onUp);
}
