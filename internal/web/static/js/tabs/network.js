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
  const PROTOCOLS = [["DIMSE", "dimse"], ["DICOMweb", "dicomweb"]];

  /**
   * Renders the Network tab with all UI components and starts periodic status monitoring.
   */
  function render() {
    panel.innerHTML = "";
    panel.appendChild(receiverCard());
    panel.appendChild(nodesCard());
    panel.appendChild(diagnosticsCard());
    panel.appendChild(settingsCard());
    refreshStatus();
    refreshNodes();
    loadSettings();
    if (!statusTimer) statusTimer = setInterval(refreshStatus, 5000);
  }

  /**
   * Creates the DICOM listener control card.
   * @returns {Element} The card element containing listener status, control buttons, counters, and warnings.
   */
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

  /**
   * Builds the DICOM nodes management interface for Query/Retrieve and Send operations.
   * @returns {Element} A section element containing the editable node table and action buttons.
   */
  function nodesCard() {
    return el("section", { class: "card" },
      el("h2", null, "DICOM nodes for Query/Retrieve and Send"),
      el("table", { class: "edit" },
        el("thead", null, el("tr", null,
          ...["", "Protocol", "Endpoint", "Query", "Retrieve", "Send", "Name", "Options", ""].map((h, i) =>
            el("th", { class: (i === 0 || (i >= 3 && i <= 5)) ? "c" : "" }, h)))),
        el("tbody", { id: "net-nodes" })),
      el("div", { class: "row", style: "margin-top:10px" },
        el("button", { class: "btn", onclick: addDimseNode }, "Add DIMSE node"),
        el("button", { class: "btn secondary", onclick: addDICOMwebNode }, "Add DICOMweb node"),
        el("button", { class: "btn secondary", onclick: () => setAllQuery(true) }, "All"),
        el("button", { class: "btn secondary", onclick: () => setAllQuery(false) }, "None"),
        el("button", { class: "btn secondary", onclick: verifyAll }, "Verify all")));
  }

  /**
   * Fetches and displays the list of DICOM nodes in the network table.
   */
  async function refreshNodes() {
    const r = await apiGet("/api/nodes");
    nodes = (r.ok && r.data) || [];
    const body = document.getElementById("net-nodes");
    if (!body) return;
    body.innerHTML = "";
    if (!nodes.length) {
      body.appendChild(el("tr", null, el("td", { colspan: "9", class: "empty" }, "no nodes — Add new node")));
      renderDiagnosticNodes();
      return;
    }
    for (const n of nodes) body.appendChild(nodeRow(n));
    renderDiagnosticNodes();
  }

  /**
   * Creates an editable table row for a DICOM node.
   * @param {Object} n - A node object containing endpoint and configuration data.
   * @returns {HTMLElement} A table row with protocol-specific fields, inline editing controls, and action buttons.
   */
  function nodeRow(n) {
    const draft = () => {
      const protocol = cells.protocol.value;
      const base = {
        name: cells.name.value, protocol,
        disabled: !cells.enabled.checked, queryDisabled: !cells.query.checked, sendDisabled: !cells.send.checked,
        notes: n.notes || "",
      };
      if (protocol === "dicomweb") {
        return Object.assign(base, {
          baseURL: cells.baseURL.value,
          qidoPathPrefix: cells.qido.value,
          wadoPathPrefix: cells.wado.value,
          stowPathPrefix: cells.stow.value,
          credentialRef: cells.credential.value,
        });
      }
      return Object.assign(base, {
        aeTitle: cells.aeTitle.value, host: cells.host.value,
        port: parseInt(cells.port.value, 10) || 0,
        retrieveMethod: cells.retrieve.value, sendTransferSyntax: cells.syntax.value,
        useTLS: cells.tls.checked, preferredMoveDestination: n.preferredMoveDestination || "",
      });
    };
    const save = async (refreshAfterSave = false) => {
      const r = await apiSend(`/api/nodes/${encodeURIComponent(n.id)}`, "PUT", draft());
      if (!r.ok) setStatus(`Save failed: ${r.error}`, "error");
      else {
        setStatus("Node saved", "ok");
        if (refreshAfterSave) refreshNodes();
      }
    };
    const txt = (v) => el("input", { type: "text", value: v || "", onchange: save });
    const smallText = (v, placeholder) => el("input", { type: "text", value: v || "", placeholder, onchange: save });
    const cells = {
      enabled: chk(!n.disabled, save),
      protocol: sel(PROTOCOLS, protocolOf(n), () => save(true)),
      host: txt(n.host), aeTitle: txt(n.aeTitle),
      port: el("input", { type: "number", value: String(n.port ?? ""), onchange: save }),
      baseURL: smallText(n.baseURL, "https://host/dicom-web"),
      qido: smallText(n.qidoPathPrefix, "qido-rs"),
      wado: smallText(n.wadoPathPrefix, "wado-rs"),
      stow: smallText(n.stowPathPrefix, "stow-rs"),
      credential: smallText(n.credentialRef, "credential ref"),
      query: chk(!n.queryDisabled, save), send: chk(!n.sendDisabled, save), tls: chk(!!n.useTLS, save),
      retrieve: sel(RETRIEVE_METHODS.map((m) => [m, m]), n.retrieveMethod || "Auto", save),
      syntax: sel(SEND_SYNTAXES, n.sendTransferSyntax || "Auto", save),
      name: txt(n.name),
    };
    const verifyPill = el("span");
    const dicomweb = protocolOf(n) === "dicomweb";
    const tr = el("tr", null,
      el("td", { class: "c" }, cells.enabled),
      el("td", null, cells.protocol),
      el("td", null, dicomweb ? dicomwebEndpointFields(cells) : dimseEndpointFields(cells)),
      el("td", { class: "c" }, cells.query),
      el("td", null, dicomweb ? el("span", { class: "muted" }, "WADO-RS") : cells.retrieve),
      el("td", { class: "c" }, cells.send),
      el("td", null, cells.name),
      el("td", null, dicomweb ? dicomwebOptions(cells) : dimseOptions(cells)),
      el("td", null, el("div", { class: "row", style: "gap:4px;flex-wrap:nowrap" },
        el("button", { class: "btn secondary", onclick: (e) => verify(n, e.target, verifyPill) }, "Verify"),
        el("button", { class: "btn danger", onclick: () => deleteNode(n) }, "✕"), verifyPill)));
    return tr;
  }
  /**
 * Creates a checkbox input element.
 * @param {boolean} checked - The initial checked state.
 * @param {Function} onchange - Handler invoked when the checkbox state changes.
 * @return {HTMLInputElement} A checkbox input element.
 */
function chk(checked, onchange) { const i = el("input", { type: "checkbox", onchange }); i.checked = checked; return i; }
  /**
 * Creates a select element with the specified options and initial selection.
 * @param {Array<[string, string]>} opts - Array of [label, value] pairs for the select options.
 * @param {string} value - The value to pre-select.
 * @param {Function} onchange - Callback invoked when the selection changes.
 * @return {Element} The created select element.
 */
function sel(opts, value, onchange) { return el("select", { onchange }, ...opts.map(([t, v]) => el("option", { value: v, selected: v === value }, t))); }
  /**
 * Normalizes a node's protocol identifier to a canonical value.
 * @param {Object} n - A node object with an optional `protocol` property.
 * @returns {string} `'dicomweb'` if the protocol is 'dicomweb', `'dimse'` otherwise.
 */
function protocolOf(n) { return String(n.protocol || "dimse").toLowerCase() === "dicomweb" ? "dicomweb" : "dimse"; }
  /**
   * Wraps an input element with a styled label.
   * @param {string} label - The label text.
   * @param {HTMLElement} input - The input element.
   * @return {HTMLElement} A label element containing the styled label and input.
   */
  function field(label, input) {
    return el("label", { style: "display:grid;gap:2px;min-width:160px" }, el("span", { class: "muted" }, label), input);
  }
  /**
   * Stacks field elements in a vertical grid layout.
   * @param {...*} items - Elements to stack.
   * @return {Element} The container div.
   */
  function fieldStack(...items) {
    return el("div", { style: "display:grid;gap:6px;min-width:220px" }, ...items);
  }
  /**
   * Assembles the DIMSE endpoint configuration fields.
   * @param {Object} cells - Input elements for host, AE Title, and port.
   * @returns {HTMLElement} A stacked layout of the endpoint fields.
   */
  function dimseEndpointFields(cells) {
    return fieldStack(field("Host", cells.host), field("AE Title", cells.aeTitle), field("Port", cells.port));
  }
  /**
   * Assembles the options UI fields for a DIMSE node.
   * @param {object} cells - An object containing `tls` and `syntax` UI control elements.
   * @return {element} A stacked layout of the TLS and Send Transfer Syntax option fields.
   */
  function dimseOptions(cells) {
    return fieldStack(field("TLS", cells.tls), field("Send Transfer Syntax", cells.syntax));
  }
  /**
   * Assembles the DICOMweb endpoint configuration fields into a stacked layout.
   * @param {Object} cells - An object containing the Base URL, QIDO path, WADO path, and STOW path input elements.
   * @return {HTMLElement} A stacked field container with labeled DICOMweb endpoint inputs.
   */
  function dicomwebEndpointFields(cells) {
    return fieldStack(
      field("Base URL", cells.baseURL),
      field("QIDO path", cells.qido),
      field("WADO path", cells.wado),
      field("STOW path", cells.stow));
  }
  /**
   * Builds the DICOMweb-specific options UI for a node.
   * @param {object} cells - Object containing the credential UI control.
   * @return {HTMLElement} A field containing the credential reference input.
   */
  function dicomwebOptions(cells) {
    return fieldStack(field("Credential ref", cells.credential));
  }

  /**
   * Creates a new DIMSE node with default settings.
   */
  async function addDimseNode() {
    const suffix = nodes.length + 1;
    const r = await apiSend("/api/nodes", "POST", { name: `dimse-${suffix}`, protocol: "dimse", aeTitle: "AETITLE", host: "127.0.0.1", port: 104 });
    if (r.ok) { setStatus("DIMSE node added", "ok"); refreshNodes(); } else setStatus(`Add failed: ${r.error}`, "error");
  }
  /**
   * Adds a new DICOMweb node to the network configuration.
   */
  async function addDICOMwebNode() {
    const suffix = nodes.length + 1;
    const r = await apiSend("/api/nodes", "POST", { name: `dicomweb-${suffix}`, protocol: "dicomweb", baseURL: "http://127.0.0.1/dicom-web" });
    if (r.ok) { setStatus("Node added", "ok"); refreshNodes(); } else setStatus(`Add failed: ${r.error}`, "error");
  }
  /**
   * Deletes a node after prompting for user confirmation.
   * @param {Object} n - The node to delete.
   */
  async function deleteNode(n) {
    if (!confirm(`Delete node "${n.name}"?`)) return;
    const r = await apiSend(`/api/nodes/${encodeURIComponent(n.id)}`, "DELETE");
    if (r.ok) { setStatus(`Deleted ${n.name}`, "ok"); refreshNodes(); } else setStatus(`Delete failed: ${r.error}`, "error");
  }
  /**
   * Enables or disables Query/Retrieve on all nodes.
   * @param {boolean} on - `true` to enable Query, `false` to disable.
   */
  async function setAllQuery(on) {
    const failed = [];
    for (const n of nodes) {
      const r = await apiSend(`/api/nodes/${encodeURIComponent(n.id)}`, "PUT", Object.assign({}, draftOf(n), { queryDisabled: !on }));
      if (!r.ok) failed.push(n.name || n.id);
    }
    setStatus(failed.length ? `Query update failed for ${failed.join(", ")}` : (on ? "Query enabled for all" : "Query disabled for all"), failed.length ? "error" : "ok");
    refreshNodes();
  }
  /**
   * Constructs an API payload for a DICOM node, adding protocol-specific fields.
   * @param {Object} n - The node object to convert.
   * @returns {Object} A payload with common node properties and protocol-specific fields.
   */
  function draftOf(n) {
    const protocol = protocolOf(n);
    const base = { name: n.name, protocol, disabled: !!n.disabled, queryDisabled: !!n.queryDisabled,
      sendDisabled: !!n.sendDisabled, notes: n.notes || "" };
    if (protocol === "dicomweb") return Object.assign(base, {
      baseURL: n.baseURL || "", qidoPathPrefix: n.qidoPathPrefix || "", wadoPathPrefix: n.wadoPathPrefix || "",
      stowPathPrefix: n.stowPathPrefix || "", credentialRef: n.credentialRef || "",
    });
    return Object.assign(base, { aeTitle: n.aeTitle, host: n.host, port: n.port, retrieveMethod: n.retrieveMethod || "Auto",
      sendTransferSyntax: n.sendTransferSyntax || "Auto", useTLS: !!n.useTLS, preferredMoveDestination: n.preferredMoveDestination || "" });
  }
  /**
   * Verifies connectivity to a DICOM node.
   * @param {Object} node - The DICOM node to verify.
   * @param {HTMLElement} btn - The button element to disable during verification.
   * @param {HTMLElement} pill - The element to display the verification result.
   */
  async function verify(node, btn, pill) {
    btn.disabled = true;
    setStatus(`Verifying ${node.name}…`);
    const r = await apiSend("/api/echo", "POST", { nodeID: node.id || node.name });
    btn.disabled = false;
    pill.innerHTML = "";
    pill.appendChild(el("span", { class: "pill " + (r.ok ? "ok" : "fail") }, r.ok ? "OK" : "fail"));
    const okText = protocolOf(node) === "dicomweb" ? "DICOMweb verify OK" : "C-ECHO OK";
    setStatus(r.ok ? `${node.name}: ${okText}` : `${node.name}: ${r.error}`, r.ok ? "ok" : "error");
  }
  /**
 * Verifies connectivity to all configured nodes sequentially.
 */
async function verifyAll() {
  const failed = [];
  for (const n of nodes) {
    const r = await apiSend("/api/echo", "POST", { nodeID: n.id });
    if (!r.ok) failed.push(n.name || n.id);
  }
  setStatus(failed.length ? `Verification failed for ${failed.join(", ")}` : "Verified all nodes", failed.length ? "error" : "ok");
}

  /**
   * Creates the diagnostics card UI for running network diagnostics against DICOM nodes.
   * @returns {HTMLElement} The diagnostics card section element.
   */
  function diagnosticsCard() {
    const study = el("input", { type: "text", id: "diag-study", placeholder: "Study Instance UID" });
    const cstore = el("input", { type: "checkbox", id: "diag-cstore" });
    return el("section", { class: "card" },
      el("h2", null, "Diagnostics"),
      el("div", { class: "diag-grid" },
        el("label", { class: "k" }, "Nodes"), el("div", { id: "diag-nodes", class: "checkgrid" }),
        el("label", { class: "k" }, "Study UID"), study,
        el("label", { class: "k" }, "C-STORE/STOW-RS"), el("label", { class: "checkline" }, cstore, "Send selected study")),
      el("div", { class: "row", style: "margin-top:10px" },
        el("button", { class: "btn", onclick: runDiagnostics }, "Run diagnostics"),
        el("button", { class: "btn secondary", onclick: () => { const st = window.gopacsSelectedStudy || {}; study.value = st.StudyInstanceUID || window.gopacsSelectedStudyUID || ""; } }, "Use selected study")),
      el("div", { id: "diag-results", class: "diag-results" }));
  }

  /**
   * Populates the diagnostics node selector with checkboxes for each available DICOM node, pre-selecting those that are not disabled.
   */
  function renderDiagnosticNodes() {
    const wrap = document.getElementById("diag-nodes");
    if (!wrap) return;
    wrap.innerHTML = "";
    if (!nodes.length) { wrap.appendChild(el("span", { class: "muted" }, "No nodes")); return; }
    for (const n of nodes) {
      const input = el("input", { type: "checkbox", value: n.id });
      input.checked = !n.disabled;
      wrap.appendChild(el("label", null, input, n.name || n.aeTitle || n.id));
    }
  }

  /**
   * Runs network diagnostics on selected nodes.
   */
  async function runDiagnostics() {
    const studyInput = document.getElementById("diag-study");
    const includeCStore = document.getElementById("diag-cstore").checked;
    const selected = Array.from(document.querySelectorAll("#diag-nodes input:checked")).map((x) => x.value);
    const selectedStudy = window.gopacsSelectedStudy || {};
    const studyUID = (studyInput.value || selectedStudy.StudyInstanceUID || window.gopacsSelectedStudyUID || "").trim();
    if (includeCStore) {
      if (!studyUID) { setStatus("Select or enter a Study UID before C-STORE diagnostics", "error"); return; }
      const names = nodes.filter((n) => selected.includes(n.id)).map((n) => n.name || n.aeTitle || n.id).join(", ");
      const patient = selectedStudy.PatientName || "selected patient";
      if (!confirm(`Send study ${studyUID} for ${patient} to ${names || "selected destinations"}?`)) return;
    }
    setStatus("Running network diagnostics…");
    const r = await apiSend("/api/network/diagnostics", "POST", { nodeIDs: selected, studyUID, includeCStore });
    if (!r.ok) { setStatus(`Diagnostics failed: ${r.error}`, "error"); return; }
    renderDiagnosticResults(r.data || []);
    setStatus("Diagnostics complete", "ok");
  }

  /**
   * Displays diagnostic results in a formatted table.
   * @param {Array} results - An array of diagnostic result objects, each with node information, protocol, and an array of step results including status, error, and diagnostic counts.
   */
  function renderDiagnosticResults(results) {
    const wrap = document.getElementById("diag-results");
    if (!wrap) return;
    wrap.innerHTML = "";
    if (!results.length) { wrap.appendChild(el("div", { class: "empty" }, "No diagnostics results")); return; }
    const body = el("tbody", null);
    for (const res of results) {
      for (const step of (res.steps || [])) {
        body.appendChild(el("tr", null,
          el("td", null, res.nodeName || res.nodeID || ""),
          el("td", null, res.protocol || ""),
          el("td", null, step.name || ""),
          el("td", null, el("span", { class: "pill " + (step.success ? "ok" : (step.status === "skipped" ? "idle" : "fail")) }, step.status || "")),
          el("td", null, step.statusCode || ""),
          el("td", null, diagnosticCounts(step)),
          el("td", null, transferSyntaxSummary(step)),
          el("td", null, step.error || "")));
      }
    }
    wrap.appendChild(el("table", { class: "edit" },
      el("thead", null, el("tr", null, ...["Node", "Protocol", "Step", "Status", "Code", "Counts", "Transfer syntaxes", "Error"].map((h) => el("th", null, h)))),
      body));
  }

  /**
   * Formats diagnostic step metrics as a human-readable string.
   * @param {Object} step - An object containing optional count properties.
   * @return {string} A human-readable string of count metrics separated by ' · ', or an empty string if no counts are present.
   */
  function diagnosticCounts(step) {
    const parts = [];
    if (step.count) parts.push(`matches ${step.count}`);
    if (step.attempted) parts.push(`attempted ${step.attempted}`);
    if (step.sent) parts.push(`sent ${step.sent}`);
    if (step.failed) parts.push(`failed ${step.failed}`);
    if (step.warnings) parts.push(`warn ${step.warnings}`);
    return parts.join(" · ");
  }

  /**
   * Formats a summary of requested and negotiated transfer syntax UIDs.
   * @return {string} A summary string showing requested and negotiated transfer syntaxes, or an empty string if both are absent.
   */
  function transferSyntaxSummary(step) {
    const req = (step.requestedTransferSyntaxUIDs || []).join(",");
    const neg = (step.negotiatedTransferSyntaxUIDs || []).join(",");
    if (!req && !neg) return "";
    return `req ${req || "-"} / neg ${neg || "-"}`;
  }

  /**
   * Creates the DICOM listener configuration card UI.
   * @returns The card element containing the settings form grid and save button.
   */
  function settingsCard() {
    return el("section", { class: "card" },
      el("h2", null, "DICOM listener configuration"),
      el("div", { class: "formgrid", id: "net-settings" }, el("span", { class: "muted" }, "loading…")),
      el("div", { class: "row", style: "margin-top:12px" },
        el("button", { class: "btn", onclick: saveSettings }, "Save settings")));
  }
  let fields = {};
  /**
   * Loads listener configuration and populates the settings form.
   */
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
  /**
   * Saves the listener configuration settings from the form to the server.
   */
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
