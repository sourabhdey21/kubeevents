(() => {
  const state = {
    events: [],
    kinds: new Set(),
    namespaces: new Set(),
  };

  const els = {
    events: document.getElementById("events"),
    empty: document.getElementById("empty"),
    search: document.getElementById("search"),
    type: document.getElementById("type"),
    namespace: document.getElementById("namespace"),
    kind: document.getElementById("kind"),
    clear: document.getElementById("clear"),
    cluster: document.getElementById("cluster"),
    scope: document.getElementById("scope"),
    live: document.getElementById("live"),
    total: document.getElementById("stat-total"),
    warnings: document.getElementById("stat-warnings"),
    normals: document.getElementById("stat-normals"),
    countLabel: document.getElementById("count-label"),
    nsList: document.getElementById("ns-list"),
  };

  function esc(s) {
    return String(s ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;");
  }

  function fmtTime(iso) {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleString(undefined, {
      month: "short",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
  }

  function filters() {
    return {
      q: els.search.value.trim().toLowerCase(),
      type: els.type.value,
      namespace: els.namespace.value.trim(),
      kind: els.kind.value,
    };
  }

  function matches(ev, f) {
    if (f.type && ev.type !== f.type) return false;
    if (f.namespace && (ev.namespace || "").toLowerCase() !== f.namespace.toLowerCase()) return false;
    if (f.kind && ev.kind !== f.kind) return false;
    if (f.q) {
      const hay = `${ev.reason} ${ev.message} ${ev.name} ${ev.namespace} ${ev.kind}`.toLowerCase();
      if (!hay.includes(f.q)) return false;
    }
    return true;
  }

  function updateStats() {
    let warnings = 0;
    let normals = 0;
    for (const ev of state.events) {
      if (ev.type === "Warning") warnings += 1;
      else normals += 1;
    }
    els.total.textContent = String(state.events.length);
    els.warnings.textContent = String(warnings);
    els.normals.textContent = String(normals);
  }

  function refreshFilterOptions() {
    const kinds = [...state.kinds].sort();
    const current = els.kind.value;
    els.kind.innerHTML = `<option value="">All kinds</option>` +
      kinds.map((k) => `<option value="${esc(k)}">${esc(k)}</option>`).join("");
    if (kinds.includes(current)) els.kind.value = current;

    els.nsList.innerHTML = [...state.namespaces]
      .filter((n) => n && n !== "—")
      .sort()
      .map((n) => `<option value="${esc(n)}"></option>`)
      .join("");
  }

  function render() {
    const f = filters();
    const visible = state.events.filter((ev) => matches(ev, f));
    els.countLabel.textContent = `${visible.length} shown`;
    els.empty.classList.toggle("hidden", visible.length > 0);

    els.events.innerHTML = visible.map((ev) => {
      const typeClass = ev.type === "Warning" ? "warning" : "normal";
      const flags = [];
      if (ev.notified) flags.push(`<span class="flag tg">telegram</span>`);
      if (ev.count > 1) flags.push(`<span class="flag">×${esc(ev.count)}</span>`);
      if (ev.source) flags.push(`<span class="flag">${esc(ev.source)}</span>`);
      return `
        <article class="row ${typeClass}" role="listitem">
          <div class="time">${esc(fmtTime(ev.timestamp))}</div>
          <div><span class="type-badge ${typeClass}">${esc(ev.type || "Normal")}</span></div>
          <div class="object">
            <strong>${esc(ev.kind)}/${esc(ev.name)}</strong>
            <span>${esc(ev.namespace)}</span>
          </div>
          <div class="reason">${esc(ev.reason)}</div>
          <div class="message">
            ${esc(ev.message)}
            <div class="flags">${flags.join("")}</div>
          </div>
        </article>`;
    }).join("");
  }

  function upsert(ev, { prepend = true } = {}) {
    const idx = state.events.findIndex((e) => e.id === ev.id);
    if (idx >= 0) state.events.splice(idx, 1);
    if (prepend) state.events.unshift(ev);
    else state.events.push(ev);
    if (ev.kind) state.kinds.add(ev.kind);
    if (ev.namespace) state.namespaces.add(ev.namespace);
  }

  async function loadMeta() {
    const res = await fetch("/api/meta");
    const data = await res.json();
    els.cluster.textContent = data.clusterName || "cluster";
    els.scope.textContent = `ns: ${data.watchNamespace || "all"}`;
  }

  async function loadEvents() {
    const res = await fetch("/api/events?limit=300");
    const data = await res.json();
    state.events = [];
    state.kinds = new Set();
    state.namespaces = new Set();
    for (const ev of data.events || []) upsert(ev, { prepend: false });
    // API returns newest-first already; keep that order
    state.events = data.events || [];
    for (const ev of state.events) {
      if (ev.kind) state.kinds.add(ev.kind);
      if (ev.namespace) state.namespaces.add(ev.namespace);
    }
    refreshFilterOptions();
    updateStats();
    render();
  }

  function connectStream() {
    const es = new EventSource("/api/stream");
    es.addEventListener("ready", () => {
      els.live.textContent = "live";
      els.live.classList.add("on");
    });
    es.addEventListener("k8s", (msg) => {
      try {
        const ev = JSON.parse(msg.data);
        upsert(ev, { prepend: true });
        refreshFilterOptions();
        updateStats();
        render();
      } catch (_) { /* ignore */ }
    });
    es.onerror = () => {
      els.live.textContent = "reconnecting";
      els.live.classList.remove("on");
    };
  }

  ["input", "change"].forEach((evt) => {
    els.search.addEventListener(evt, render);
    els.type.addEventListener(evt, render);
    els.namespace.addEventListener(evt, render);
    els.kind.addEventListener(evt, render);
  });

  els.clear.addEventListener("click", () => {
    els.search.value = "";
    els.type.value = "";
    els.namespace.value = "";
    els.kind.value = "";
    render();
  });

  loadMeta().catch(() => {});
  loadEvents().catch((err) => {
    els.empty.textContent = `Failed to load events: ${err.message}`;
    els.empty.classList.remove("hidden");
  });
  connectStream();
})();
