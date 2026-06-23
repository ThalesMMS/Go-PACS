// Tasks tab: persisted operation history (import/query/retrieve/send/receiver
// summaries) with a JSON detail pane. Backed by /api/tasks.
window.TABS.tasks = (function () {
  let panel;
  let history = [];
  let selectedIndex = -1;

  function render() {
    panel.innerHTML = "";
    panel.appendChild(el("section", { class: "card" },
      el("div", { class: "row" },
        el("h2", { class: "grow" }, "Activity history"),
        el("button", { class: "btn secondary", onclick: load }, "Refresh")),
      el("div", { class: "split", style: "grid-template-columns:1.4fr 1fr" },
        el("div", null,
          el("table", { class: "grid" },
            el("thead", null, el("tr", null,
              el("th", null, "Kind"), el("th", null, "Status"),
              el("th", null, "Counts"), el("th", null, "Duration"))),
            el("tbody", { id: "tasks-rows" }))),
        el("div", null,
          el("div", { class: "row", style: "margin-bottom:8px" },
            el("button", { class: "btn secondary", id: "tasks-retry", disabled: true, onclick: retrySelected }, "Retry"),
            el("span", { class: "muted", id: "tasks-retry-reason" })),
          el("pre", { class: "detail", id: "tasks-detail" }, "Select an operation to see its JSON detail.")),
      ),
    ));
    load();
  }

  function statusClass(s) {
    const v = String(s || "").toLowerCase();
    if (v.includes("success") || v === "ok") return "ok";
    if (v.includes("warn") || v.includes("partial")) return "warn";
    if (v.includes("fail") || v.includes("error")) return "fail";
    return "idle";
  }

  function countsSummary(c) {
    if (!c) return "";
    const parts = [];
    for (const [k, v] of Object.entries(c)) {
      if (v) parts.push(`${k}:${v}`);
    }
    return parts.join(" ");
  }

  function durationText(ms) {
    if (!ms) return "";
    if (ms < 1000) return `${ms} ms`;
    return `${(ms / 1000).toFixed(1)} s`;
  }

  async function load() {
    const r = await apiGet("/api/tasks");
    const body = document.getElementById("tasks-rows");
    if (!body) return;
    if (!r.ok) { body.innerHTML = `<tr><td colspan="4">error: ${escapeHTML(r.error)}</td></tr>`; return; }
    history = r.data || [];
    body.innerHTML = "";
    if (!history.length) {
      selectedIndex = -1;
      updateRetryControl({ canRetry: false, reason: "Select a retryable failed or warning task" });
      body.appendChild(el("tr", null, el("td", { colspan: "4", class: "empty" }, "no operations recorded")));
      return;
    }
    history.forEach((op, i) => {
      const row = el("tr", { onclick: () => showDetail(i, row) },
        el("td", null, String(op.kind || op.Kind || "")),
        el("td", null, el("span", { class: "pill " + statusClass(op.status || op.Status) }, String(op.status || op.Status || ""))),
        el("td", { class: "muted" }, countsSummary(op.counts || op.Counts)),
        el("td", { class: "muted" }, durationText(op.duration_ms ?? op.DurationMS)));
      body.appendChild(row);
    });
  }

  async function showDetail(i, row) {
    selectedIndex = i;
    for (const tr of document.querySelectorAll("#tasks-rows tr")) tr.classList.remove("selected");
    if (row) row.classList.add("selected");
    const pre = document.getElementById("tasks-detail");
    pre.textContent = JSON.stringify(history[i], null, 2);
    updateRetryControl({ canRetry: false, reason: "Checking retry state..." });
    const r = await apiGet(`/api/tasks/${encodeURIComponent(i)}/can-retry`);
    if (!r.ok) {
      updateRetryControl({ canRetry: false, reason: r.error || "Retry check failed" });
      return;
    }
    updateRetryControl(r.data || {});
  }

  function updateRetryControl(state) {
    const btn = document.getElementById("tasks-retry");
    const reason = document.getElementById("tasks-retry-reason");
    if (!btn || !reason) return;
    const canRetry = !!(state && state.canRetry);
    const message = canRetry ? "" : String((state && state.reason) || "Task cannot be retried");
    btn.disabled = !canRetry;
    btn.title = message;
    reason.textContent = message;
  }

  async function retrySelected() {
    if (selectedIndex < 0) return;
    const r = await apiSend(`/api/tasks/${encodeURIComponent(selectedIndex)}/retry`, "POST");
    if (!r.ok || !r.data || !r.data.jobID) {
      setStatus(`Retry failed to start: ${r.error || "no job"}`, "error");
      return;
    }
    updateRetryControl({ canRetry: false, reason: "Retry running..." });
    streamJob(r.data.jobID, {
      onDone: () => { setStatus("Retry completed", "ok"); load(); },
      onError: (message) => { setStatus(`Retry failed: ${message || "unknown error"}`, "error"); load(); },
    });
  }

  return {
    init(p) { panel = p; render(); },
    refresh() { if (panel) load(); },
  };
})();
