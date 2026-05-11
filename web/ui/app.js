// AetherFlow incident console — single-file SPA, no build step.
//
// Connects an SSE stream from /v1/events?incident=* and renders incoming
// events into a live timeline. Handles incident submission, corpus ingest,
// and the BYO LLM key drawer.

(function () {
  const GATEWAY = ""; // same-origin
  const $ = (id) => document.getElementById(id);

  // ---- connection status ----
  function setStatus(state) {
    const dot = $("dot"), txt = $("statusText");
    if (state === "ok") { dot.className = "w-2 h-2 rounded-full bg-mint-500"; txt.textContent = "connected"; }
    else if (state === "wait") { dot.className = "w-2 h-2 rounded-full bg-amber-400"; txt.textContent = "reconnecting…"; }
    else { dot.className = "w-2 h-2 rounded-full bg-rose-500"; txt.textContent = "offline"; }
  }

  // ---- SSE wiring ----
  let es = null;
  function connectStream() {
    if (es) es.close();
    setStatus("wait");
    es = new EventSource(GATEWAY + "/v1/events?incident=*");
    es.onopen = () => setStatus("ok");
    es.onerror = () => setStatus("err");

    const events = [
      "incident.created.v1",
      "context.assembled.v1",
      "analysis.completed.v1",
      "analysis.validated.v1",
      "validation.rejected.v1",
      "action.executed.v1",
    ];
    for (const t of events) {
      es.addEventListener(t, (e) => {
        try { renderEvent(t, JSON.parse(e.data)); }
        catch (err) { console.error("parse", t, err); }
      });
    }
  }

  // ---- timeline render ----
  const timeline = $("timeline");
  const empty = $("emptyState");

  function colorFor(type) {
    if (type.startsWith("incident.")) return "text-aether-400 border-aether-400/40";
    if (type.startsWith("context.")) return "text-mint-400 border-mint-400/40";
    if (type.startsWith("analysis.completed")) return "text-amber-400 border-amber-400/40";
    if (type.startsWith("analysis.validated")) return "text-mint-400 border-mint-400/40";
    if (type.startsWith("validation.rejected")) return "text-rose-400 border-rose-400/40";
    if (type.startsWith("action.executed")) return "text-aether-400 border-aether-400/40";
    return "text-slate-400 border-slate-700";
  }

  function el(html) {
    const t = document.createElement("template");
    t.innerHTML = html.trim();
    return t.content.firstChild;
  }

  function esc(s) {
    return String(s == null ? "" : s)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;");
  }

  function renderEvent(type, payload) {
    if (empty && empty.parentNode) empty.remove();
    const e = payload || {};
    const ev = e || {};
    const incidentId = (ev.incident_id || (ev.header && ev.header.incident_id) || "");
    const occurred = ev.occurred_at || new Date().toISOString();

    let body = "";
    if (type === "incident.created.v1") {
      body = `
        <div class="text-sm">${esc(ev.description || "")}</div>
        <div class="mt-2 flex gap-2 text-[10px]">
          <span class="badge text-aether-400">severity: ${esc(ev.severity || "?")}</span>
          <span class="badge text-slate-400">source: ${esc(ev.source || "?")}</span>
          ${ev.llm ? `<span class="badge text-mint-400">llm: ${esc(ev.llm.provider)}/${esc(ev.llm.model||"default")}</span>` : ""}
        </div>`;
    } else if (type === "context.assembled.v1") {
      const obs = (ev.dns_observations || []).map(o => `
        <li class="text-xs text-slate-300"><span class="text-aether-300 font-mono">${esc(o.tool)}</span> <span class="font-mono text-slate-400">${esc(o.target)}</span> ${o.verdict?`<span class="text-rose-400">→ ${esc(o.verdict)}</span>`:""} ${o.records?`<span class="text-slate-400">→ ${esc(o.records.join(", "))}</span>`:""} ${o.error?`<span class="text-rose-400">err: ${esc(o.error)}</span>`:""}</li>
      `).join("");
      const ev_list = (ev.evidence || []).map(c => `
        <div class="evidence-card text-xs my-2">
          <div class="text-slate-300"><span class="font-mono text-aether-400">${esc((c.chunk_id||"").slice(0,8))}</span> · ${esc(c.source||"")} / ${esc(c.title||"")}
            <span class="text-slate-500">(fused ${(+c.fused_score||0).toFixed(3)})</span>
          </div>
          <div class="text-slate-400 mt-0.5">${esc(c.snippet||"")}</div>
        </div>`).join("");
      body = `
        <details ${(ev.dns_observations||[]).length?"open":""}>
          <summary class="text-xs text-slate-400"><span class="chev">▶</span> DNS observations · <span class="text-slate-200">${(ev.dns_observations||[]).length}</span></summary>
          <ul class="mt-2 space-y-1">${obs || '<li class="text-xs text-slate-500">(none)</li>'}</ul>
        </details>
        <details open>
          <summary class="text-xs text-slate-400 mt-2"><span class="chev">▶</span> Retrieved evidence · <span class="text-slate-200">${(ev.evidence||[]).length}</span></summary>
          ${ev_list || '<div class="text-xs text-slate-500">(no evidence — corpus may be empty)</div>'}
        </details>
        ${ev.degraded ? `<div class="text-xs text-rose-400 mt-2">⚠ retrieval degraded</div>` : ""}`;
    } else if (type === "analysis.completed.v1") {
      const a = ev.analysis || {};
      const actions = (a.actions || []).map(x => `
        <li class="text-xs"><span class="badge text-aether-400">${esc(x.kind)}</span>
          <span class="font-mono">${esc(x.target)}</span>
          <span class="text-slate-400">— ${esc(x.reason||"")}</span></li>`).join("");
      const cites = (a.citations || []).map(c => `<span class="badge text-mint-400">${esc(c.slice(0,8))}</span>`).join(" ");
      body = `
        <div class="text-sm"><span class="badge ${a.verdict==='malicious'?'text-rose-400':a.verdict==='suspicious'?'text-amber-400':'text-mint-400'}">verdict: ${esc(a.verdict||"?")}</span>
          <span class="ml-2 text-xs text-slate-400">confidence ${(+a.confidence||0).toFixed(2)}</span></div>
        <div class="text-sm mt-2 text-slate-200">${esc(a.summary||"")}</div>
        <div class="mt-2 text-xs text-slate-400">IOCs: ${(a.iocs||[]).map(esc).join(", ") || "(none)"}</div>
        <div class="mt-2 text-xs text-slate-400">Citations: ${cites || "(none)"}</div>
        <ul class="mt-2 space-y-1">${actions || '<li class="text-xs text-slate-500">no recommended actions</li>'}</ul>`;
    } else if (type === "analysis.validated.v1") {
      const notes = (ev.notes||[]).map(n => `<li class="text-xs text-amber-400">${esc(n)}</li>`).join("");
      body = `<div class="text-sm text-mint-400">✓ analysis passed validator checks</div>
        ${notes ? `<ul class="mt-2 space-y-1">${notes}</ul>` : ""}`;
    } else if (type === "validation.rejected.v1") {
      const markers = (ev.markers||[]).map(m => `<li class="text-xs font-mono">${esc(m)}</li>`).join("");
      body = `<div class="text-sm text-rose-400">✕ ${esc(ev.reason||"validation rejected")}</div>
        ${markers ? `<details class="mt-2"><summary class="text-xs text-slate-400"><span class="chev">▶</span> markers (${(ev.markers||[]).length})</summary><ul class="mt-1 space-y-0.5">${markers}</ul></details>` : ""}`;
    } else if (type === "action.executed.v1") {
      const a = ev.action || {};
      body = `<div class="text-sm">
          <span class="badge ${ev.outcome==='executed'?'text-mint-400':ev.outcome==='skipped'?'text-amber-400':'text-rose-400'}">${esc(ev.outcome||"?")}</span>
          <span class="ml-2 font-mono text-aether-400">${esc(a.kind||"")}</span>
          <span class="font-mono">${esc(a.target||"")}</span>
        </div>
        ${ev.receipt ? `<div class="text-xs text-slate-400 mt-1 font-mono">${esc(ev.receipt)}</div>` : ""}
        ${ev.detail ? `<div class="text-xs text-rose-400 mt-1">${esc(ev.detail)}</div>` : ""}`;
    } else {
      body = `<pre class="text-xs text-slate-400 overflow-x-auto">${esc(JSON.stringify(payload, null, 2))}</pre>`;
    }

    const node = el(`
      <div class="event-row px-5 py-4 hover:bg-ink-800/50">
        <div class="flex items-baseline gap-3 mb-2">
          <span class="badge ${colorFor(type)}">${esc(type)}</span>
          <span class="text-[11px] text-slate-500 font-mono">${esc(occurred)}</span>
          <span class="flex-1"></span>
          <span class="text-[11px] text-slate-500 font-mono">${esc(incidentId.slice(0,8))}</span>
        </div>
        ${body}
      </div>
    `);
    timeline.insertBefore(node, timeline.firstChild);
    node.classList.add("pulse");
    setTimeout(() => node.classList.remove("pulse"), 600);
  }

  // ---- submit ----
  $("incidentForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const body = { description: $("description").value.trim(), severity: $("severity").value, source: "ui" };
    if (!body.description) return;
    const r = await fetch("/v1/incidents", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    if (r.ok) { $("description").value = ""; }
    else { alert("Failed: " + await r.text()); }
  });

  // ---- demo button ----
  $("demoBtn").addEventListener("click", async () => {
    const body = {
      description: "Endpoint 10.0.4.17 is making periodic DNS queries for paypa1-secure-login.com (note the digit 1 instead of letter l) and downloading TLS certificates from the resolved IP. This looks like a typosquat domain used for credential phishing. Investigate and recommend containment.",
      severity: "high",
      source: "ui",
    };
    await fetch("/v1/incidents", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  });

  // ---- corpus ingest ----
  $("corpusForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const body = {
      source: $("corpusSource").value || "manual",
      title:  $("corpusTitle").value || "Untitled",
      text:   $("corpusText").value || "",
    };
    if (!body.text) return;
    const r = await fetch("/v1/corpus", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    if (r.ok) {
      const j = await r.json();
      $("corpusText").value = "";
      alert(`Ingested ${j.chunks} chunks (document ${j.document_id.slice(0,8)})`);
    } else {
      alert("Ingest failed: " + await r.text());
    }
  });

  $("clearBtn").addEventListener("click", () => {
    timeline.innerHTML = "";
    timeline.appendChild(empty);
  });

  // ---- keys drawer ----
  const drawer = $("keysDrawer");
  function openDrawer() { drawer.classList.remove("hidden"); refreshKeyStatus(); }
  function closeDrawer() { drawer.classList.add("hidden"); }
  $("keysBtn").addEventListener("click", openDrawer);
  $("keysClose").addEventListener("click", closeDrawer);
  $("keysBackdrop").addEventListener("click", closeDrawer);

  $("kProvider").addEventListener("change", () => {
    const p = $("kProvider").value;
    $("kKeyWrap").style.display = p === "ollama" ? "none" : "block";
    $("kBaseURLWrap").style.display = (p === "ollama" || p === "openai_compatible") ? "block" : "block";
    const defaults = {
      ollama:           { model: "llama3.1:8b-instruct", baseURL: "http://localhost:11434" },
      openai:           { model: "gpt-4o-mini",          baseURL: "https://api.openai.com/v1" },
      anthropic:        { model: "claude-3-5-sonnet-latest", baseURL: "https://api.anthropic.com" },
      openai_compatible:{ model: "",                     baseURL: "" },
    };
    $("kModel").placeholder = defaults[p].model;
    $("kBaseURL").placeholder = defaults[p].baseURL;
  });

  $("kSave").addEventListener("click", async () => {
    const body = {
      provider: $("kProvider").value,
      model:    $("kModel").value || "",
      base_url: $("kBaseURL").value || "",
      api_key:  $("kKey").value || "",
    };
    const r = await fetch("/v1/config/llm", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    if (r.ok || r.status === 204) {
      $("kStatus").textContent = "saved";
      $("kKey").value = "";
      refreshKeyStatus();
    } else {
      $("kStatus").textContent = "failed: " + await r.text();
    }
  });

  $("kReset").addEventListener("click", async () => {
    await fetch("/v1/config/llm", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ provider: "ollama", model: "", base_url: "", api_key: "" }) });
    refreshKeyStatus();
  });

  async function refreshKeyStatus() {
    try {
      const r = await fetch("/v1/config/llm");
      const j = await r.json();
      $("kCurrent").textContent = JSON.stringify(j, null, 2);
    } catch (e) {
      $("kCurrent").textContent = "(unavailable)";
    }
  }

  // ---- boot ----
  connectStream();
  refreshKeyStatus();
})();
