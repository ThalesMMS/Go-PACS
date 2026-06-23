// Network tab — dense Horos-like layout: DICOM listener control + status,
// an inline-editable DICOM nodes table (nodes_add_edit_delete.png), and a dense
// listener configuration form (listener_config.png).
window.TABS.network = (function () {
  let panel;
  let statusTimer = null;
  let nodes = [];
  let cfg = null;

  const RETRIEVE_METHODS = ["Auto", "C-MOVE", "C-GET"];
  const SEND_SYNTAXES = [
    ["Auto", "Auto"],
    ["Explicit Little Endian", "1.2.840.10008.1.2.1"],
    ["Implicit Little Endian", "1.2.840.10008.1.2"],
  ];

  function render() {
    panel.innerHTML = "";
    panel.appendChild(receiverCard());
    panel.appendChild(nodesCard());
    panel.appendChild(settingsCard());
    refreshStatus();
    refreshNodes();
    loadSettings();
    if (!statusTimer) statusTimer = setInterval(refreshStatus, 5000);
  }

  // ---- Listener control ----
  function receiverCard() {
    return el("section", { class: "card" },
      el("h2", null, "DICOM listener"),
      el("div", { class: "row", id: "rx-statusrow" }, el("span", { class: "muted" }, "loading…")),
      el("div", { class: "row", style: "margin-top:10px" },
        el("button", { class: "btn", id: "rx-start", onclick: () => receiverAction("start") }, "Start"),
        el("button", { class: "btn secondary", id: "rx-stop", onclick: () => receiverAction("stop") }, "Stop"),
        el("button", { class: "btn secondary", id: "rx-restart", onclick: () => receiverAction("restart") }, "Restart")),
      el("div", { class: "row", id: "rx-counters", style: "margin-top:10px" }),
      el("div", { id: "rx-warnings", class: "faint", style: "margin-top:8px" }));
  }
  async function refreshStatus() {
    const r = await apiGet("/api/receiver/status");
    if (!r.ok) return;
    const st = r.data || {};
    const row = document.getElementById("rx-statusrow");
    if (!row) return;
    row.innerHTML = "";
    row.appendChild(el("span", { class: "pill " + (st.running ? "ok" : "idle") }, st.running ? "Running" : "Stopped"));
    row.appendChild(el("span", { class: "muted" }, `${st.aeTitle || "-"} @ ${st.address || "-"}`));
    document.getElementById("rx-start").disabled = st.running;
    document.getElementById("rx-stop").disabled = !st.running;
    document.getElementById("rx-restart").disabled = !st.running;
    const counters = document.getElementById("rx-counters");
    counters.innerHTML = "";
    if (st.running && st.snapshot) {
      const s = st.snapshot;
      for (const [k, v] of [["Associations", s.Associations], ["Stored", s.Stored], ["Duplicates", s.Duplicates], ["Rejected", s.Rejected], ["Failed", s.Failed]])
        counters.appendChild(el("span", { class: "muted" }, `${k}: ${v ?? 0}`));
    }
    document.getElementById("rx-warnings").textContent = (st.warnings || []).join(" · ");
  }
  async function receiverAction(action) {
    setStatus(`Receiver ${action}…`);
    const r = await apiSend(`/api/receiver/${action}`, "POST");
    setStatus(r.ok ? `Receiver ${action} OK` : `Receiver ${action} failed: ${r.error}`, r.ok ? "ok" : "error");
    refreshStatus();
  }

  // ---- Inline node table ----
  function nodesCard() {
    return el("section", { class: "card" },
      el("h2", null, "DICOM nodes for Query/Retrieve and Send"),
      el("table", { class: "edit" },
        el("thead", null, el("tr", null,
          ...["", "Address", "AE Title", "Port", "Query", "Retrieve", "Send", "TLS", "Name", "Send Transfer Syntax", ""].map((h, i) =>
            el("th", { class: (i === 0 || (i >= 4 && i <= 7)) ? "c" : "" }, h)))),
        el("tbody", { id: "net-nodes" })),
      el("div", { class: "row", style: "margin-top:10px" },
        el("button", { class: "btn", onclick: addNode }, "Add new node"),
        el("button", { class: "btn secondary", onclick: () => setAllQuery(true) }, "All"),
        el("button", { class: "btn secondary", onclick: () => setAllQuery(false) }, "None"),
        el("button", { class: "btn secondary", onclick: verifyAll }, "Verify all")));
  }

  async function refreshNodes() {
    const r = await apiGet("/api/nodes");
    nodes = (r.ok && r.data) || [];
    const body = document.getElementById("net-nodes");
    if (!body) return;
    body.innerHTML = "";
    if (!nodes.length) { body.appendChild(el("tr", null, el("td", { colspan: "11", class: "empty" }, "no nodes — Add new node"))); return; }
    for (const n of nodes) body.appendChild(nodeRow(n));
  }

  function nodeRow(n) {
    const draft = () => ({
      name: cells.name.value, aeTitle: cells.aeTitle.value, host: cells.host.value,
      port: parseInt(cells.port.value, 10) || 0,
      retrieveMethod: cells.retrieve.value, sendTransferSyntax: cells.syntax.value,
      disabled: !cells.enabled.checked, queryDisabled: !cells.query.checked, sendDisabled: !cells.send.checked,
      useTLS: cells.tls.checked, preferredMoveDestination: n.preferredMoveDestination || "", notes: n.notes || "",
    });
    const save = async () => {
      const r = await apiSend(`/api/nodes/${encodeURIComponent(n.id)}`, "PUT", draft());
      if (!r.ok) setStatus(`Save failed: ${r.error}`, "error"); else setStatus("Node saved", "ok");
    };
    const txt = (v) => el("input", { type: "text", value: v || "", onchange: save });
    const cells = {
      enabled: chk(!n.disabled, save),
      host: txt(n.host), aeTitle: txt(n.aeTitle),
      port: el("input", { type: "number", value: String(n.port ?? ""), onchange: save }),
      query: chk(!n.queryDisabled, save), send: chk(!n.sendDisabled, save), tls: chk(!!n.useTLS, save),
      retrieve: sel(RETRIEVE_METHODS.map((m) => [m, m]), n.retrieveMethod || "Auto", save),
      syntax: sel(SEND_SYNTAXES, n.sendTransferSyntax || "Auto", save),
      name: txt(n.name),
    };
    const verifyPill = el("span");
    const tr = el("tr", null,
      el("td", { class: "c" }, cells.enabled), el("td", null, cells.host), el("td", null, cells.aeTitle), el("td", null, cells.port),
      el("td", { class: "c" }, cells.query), el("td", null, cells.retrieve), el("td", { class: "c" }, cells.send),
      el("td", { class: "c" }, cells.tls), el("td", null, cells.name), el("td", null, cells.syntax),
      el("td", null, el("div", { class: "row", style: "gap:4px;flex-wrap:nowrap" },
        el("button", { class: "btn secondary", onclick: (e) => verify(n, e.target, verifyPill) }, "Echo"),
        el("button", { class: "btn danger", onclick: () => deleteNode(n) }, "✕"), verifyPill)));
    return tr;
  }
  function chk(checked, onchange) { const i = el("input", { type: "checkbox", onchange }); i.checked = checked; return i; }
  function sel(opts, value, onchange) { return el("select", { onchange }, ...opts.map(([t, v]) => el("option", { value: v, selected: v === value }, t))); }

  async function addNode() {
    const r = await apiSend("/api/nodes", "POST", { name: "new-node", aeTitle: "AETITLE", host: "127.0.0.1", port: 104 });
    if (r.ok) { setStatus("Node added", "ok"); refreshNodes(); } else setStatus(`Add failed: ${r.error}`, "error");
  }
  async function deleteNode(n) {
    if (!confirm(`Delete node "${n.name}"?`)) return;
    const r = await apiSend(`/api/nodes/${encodeURIComponent(n.id)}`, "DELETE");
    if (r.ok) { setStatus(`Deleted ${n.name}`, "ok"); refreshNodes(); } else setStatus(`Delete failed: ${r.error}`, "error");
  }
  async function setAllQuery(on) {
    for (const n of nodes) await apiSend(`/api/nodes/${encodeURIComponent(n.id)}`, "PUT", Object.assign({}, draftOf(n), { queryDisabled: !on }));
    setStatus(on ? "Query enabled for all" : "Query disabled for all", "ok");
    refreshNodes();
  }
  function draftOf(n) {
    return { name: n.name, aeTitle: n.aeTitle, host: n.host, port: n.port, retrieveMethod: n.retrieveMethod || "Auto",
      sendTransferSyntax: n.sendTransferSyntax || "Auto", disabled: !!n.disabled, queryDisabled: !!n.queryDisabled,
      sendDisabled: !!n.sendDisabled, useTLS: !!n.useTLS, preferredMoveDestination: n.preferredMoveDestination || "", notes: n.notes || "" };
  }
  async function verify(node, btn, pill) {
    btn.disabled = true;
    setStatus(`Verifying ${node.name}…`);
    const r = await apiSend("/api/echo", "POST", { nodeID: node.id || node.name });
    btn.disabled = false;
    pill.innerHTML = "";
    pill.appendChild(el("span", { class: "pill " + (r.ok ? "ok" : "fail") }, r.ok ? "OK" : "fail"));
    setStatus(r.ok ? `${node.name}: C-ECHO OK` : `${node.name}: ${r.error}`, r.ok ? "ok" : "error");
  }
  async function verifyAll() { for (const n of nodes) await apiSend("/api/echo", "POST", { nodeID: n.id }); setStatus("Verified all nodes", "ok"); }

  // ---- Listener config (dense form) ----
  function settingsCard() {
    return el("section", { class: "card" },
      el("h2", null, "DICOM listener configuration"),
      el("div", { class: "formgrid", id: "net-settings" }, el("span", { class: "muted" }, "loading…")),
      el("div", { class: "row", style: "margin-top:12px" },
        el("button", { class: "btn", onclick: saveSettings }, "Save settings")));
  }
  let fields = {};
  async function loadSettings() {
    const r = await apiGet("/api/config");
    const wrap = document.getElementById("net-settings");
    if (!wrap) return;
    if (!r.ok) { wrap.innerHTML = `<span class="muted">error: ${escapeHTML(r.error)}</span>`; return; }
    cfg = r.data || {};
    wrap.innerHTML = "";
    fields = {};
    const text = (key, val) => { const i = el("input", { type: "text", value: val == null ? "" : String(val) }); fields[key] = i; return i; };
    const tg = (key, val) => { const t = toggle("", !!val); fields[key] = t.input; return t.wrap; };
    const select2 = (key, opts, val) => { const s = sel(opts, val, null); fields[key] = s; return s; };
    const k = (label) => el("label", { class: "k" }, label);

    wrap.append(
      k("Activate listener on startup"), tg("receiverAutoStart", cfg.receiverAutoStart),
      k("Local AE Title"), text("localAETitle", cfg.localAETitle),
      k("Receiver Address (host:port)"), text("receiverAddress", cfg.receiverAddress),
      k("Additional AE Titles (CSV)"), text("additionalAETitles", (cfg.additionalAETitles || []).join(",")),
      k("Preferred receive transfer syntax"),
      select2("receivePreferredTransferSyntax", [["Auto", "auto"], ["Explicit Little Endian", "1.2.840.10008.1.2.1"], ["Implicit Little Endian", "1.2.840.10008.1.2"]], cfg.receivePreferredTransferSyntax || "auto"),
      k("Incoming files: Decompress compressed images"), tg("receiveDecompressImages", cfg.receiveDecompressImages),
      k("DICOM communication timeout (s)"), text("dicomCommunicationTimeoutSeconds", cfg.dicomCommunicationTimeoutSeconds),
      k("DICOM connection timeout (s)"), text("dicomConnectionTimeoutSeconds", cfg.dicomConnectionTimeoutSeconds),
      k("Activate DICOM TLS listener"), tg("receiverUseTLS", cfg.receiverUseTLS),
      k("TLS certificate file"), text("receiverTLSCertFile", cfg.receiverTLSCertFile),
      k("TLS key file"), text("receiverTLSKeyFile", cfg.receiverTLSKeyFile));
  }
  async function saveSettings() {
    if (!cfg) return;
    const out = Object.assign({}, cfg, {
      receiverAutoStart: fields.receiverAutoStart.checked,
      localAETitle: fields.localAETitle.value,
      receiverAddress: fields.receiverAddress.value,
      additionalAETitles: fields.additionalAETitles.value.split(",").map((s) => s.trim()).filter(Boolean),
      receivePreferredTransferSyntax: fields.receivePreferredTransferSyntax.value,
      receiveDecompressImages: fields.receiveDecompressImages.checked,
      dicomCommunicationTimeoutSeconds: parseInt(fields.dicomCommunicationTimeoutSeconds.value, 10) || 0,
      dicomConnectionTimeoutSeconds: parseInt(fields.dicomConnectionTimeoutSeconds.value, 10) || 0,
      receiverUseTLS: fields.receiverUseTLS.checked,
      receiverTLSCertFile: fields.receiverTLSCertFile.value,
      receiverTLSKeyFile: fields.receiverTLSKeyFile.value,
    });
    const r = await apiSend("/api/config", "PUT", out);
    if (r.ok) { setStatus("Listener settings saved (restart receiver to apply)", "ok"); cfg = r.data; }
    else setStatus(`Save failed: ${r.error}`, "error");
  }

  return {
    init(p) { panel = p; render(); },
    refresh() { if (panel) { refreshStatus(); refreshNodes(); } },
  };
})();
