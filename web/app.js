// usnfetch web UI — no framework, no build step. Hash routes:
//   #/catalog  #/share/<id>[/<folder path>]  #/activity  #/settings  #/system  #/login

// ---------------------------------------------------------------- helpers
const $ = (s, el = document) => el.querySelector(s);
const PROPS = new Set(["value", "checked", "disabled", "selected", "indeterminate", "htmlFor"]);

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === "class") el.className = v;
    else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
    else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
    else if (k === "html") el.innerHTML = v;
    else if (PROPS.has(k)) el[k] = v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const c of kids.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

// replaceChildren that skips null/false placeholders (the DOM would print them as text)
const put = (el, ...kids) => el.replaceChildren(...kids.flat(Infinity).filter((k) => k != null && k !== false));

const ICONS = {
  catalog: "M4 5h16M4 12h16M4 19h10", activity: "M12 4v11m0 0-4.5-4.5M12 15l4.5-4.5M5 20h14",
  settings: "M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z",
  system: "M4 17l6-6-6-6M12 19h8", folder: "M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z",
  file: "M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z M14 3v5h5", eye: "M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z",
  download: "M12 4v11m0 0-4.5-4.5M12 15l4.5-4.5M5 20h14", plus: "M12 5v14M5 12h14", search: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14Z M20 20l-4-4",
  upload: "M12 20V9m0 0-4.5 4.5M12 9l4.5 4.5M5 4h14", trash: "M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13", stop: "M7 7h10v10H7Z",
  play: "M7 5l12 7-12 7Z", refresh: "M20 11a8 8 0 1 0-2.3 5.7M20 11V5m0 6h-6", shield: "M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6Z",
  link: "M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1", x: "M6 6l12 12M18 6 6 18",
  ext: "M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5", check: "M5 12l5 5 9-10", pulse: "M3 12h4l3 7 4-14 3 7h4",
  edit: "M4 20h4L19 9l-4-4L4 16Z", back: "M15 18l-6-6 6-6", server: "M4 5h16v6H4Z M4 13h16v6H4Z M8 8h.01M8 16h.01",
  grid: "M4 4h7v7H4Z M13 4h7v7h-7Z M4 13h7v7H4Z M13 13h7v7h-7Z", list: "M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01",
  pause: "M8 5v14M16 5v14", up: "M12 19V5m0 0-6 6m6-6 6 6", drag: "M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01", gauge: "M4 18a8 8 0 1 1 16 0M12 18l4-6",
};
function icon(name, cls = "i") {
  const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  s.setAttribute("viewBox", "0 0 24 24"); s.setAttribute("class", cls);
  for (const d of ICONS[name].split(" M").map((p, i) => (i ? "M" + p : p))) {
    const p = document.createElementNS("http://www.w3.org/2000/svg", "path"); p.setAttribute("d", d); s.append(p);
  }
  return s;
}

const GB = 1e9;
function fmtBytes(n, digits = 1) {
  if (n == null) return "—";
  const u = [["TB", 1e12], ["GB", 1e9], ["MB", 1e6], ["KB", 1e3]];
  for (const [s, v] of u) if (Math.abs(n) >= v) return (n / v).toFixed(n / v >= 100 ? 0 : digits) + " " + s;
  return n + " B";
}
const fmtRate = (r) => (r > 0 ? fmtBytes(r) + "/s" : "—");
function fmtEta(s) {
  if (!s || !isFinite(s)) return "—";
  s = Math.round(s); const hh = Math.floor(s / 3600), mm = Math.floor((s % 3600) / 60);
  return hh ? `${hh}h ${mm}m` : mm ? `${mm}m ${s % 60}s` : `${s}s`;
}
function ago(t) {
  if (!t) return "—";
  const s = Date.now() / 1000 - t;
  if (s < 60) return "just now"; if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`; return new Date(t * 1000).toLocaleDateString();
}
const pct = (a, b) => (b ? Math.min(100, (100 * a) / b) : 0);

async function api(path, opts = {}) {
  const o = { method: opts.method || (opts.body !== undefined ? "POST" : "GET"), headers: {}, credentials: "same-origin", signal: opts.signal };
  if (opts.body !== undefined) { o.headers["Content-Type"] = "application/json"; o.body = JSON.stringify(opts.body); }
  const r = await fetch("/api" + path, o);
  let data = null;
  try { data = await r.json(); } catch { /* empty body */ }
  if (!r.ok) {
    const d = (data && data.detail) || {};
    const e = new Error(d.message || (typeof d === "string" ? d : r.statusText));
    e.code = d.error; e.status = r.status;
    if (r.status === 401 && d.error === "login") { location.hash = "#/login"; }
    throw e;
  }
  return data;
}

function toast(msg, kind = "") {
  const t = h("div", { class: "toast " + kind }, msg);
  $("#toasts").append(t); setTimeout(() => t.remove(), kind === "bad" ? 7000 : 3800);
}
const fail = (e) => toast(e.message || String(e), "bad");

function modal({ title, body, actions = [], wide = false, onclose }) {
  const close = () => { back.remove(); document.removeEventListener("keydown", esc); onclose && onclose(); };
  const esc = (ev) => { if (ev.key === "Escape") close(); };
  const back = h("div", { class: "modal-back", onmousedown: (ev) => { if (ev.target === back) close(); } },
    h("div", { class: "modal" + (wide ? " wide" : "") },
      h("div", { class: "mh" }, h("h3", {}, title), h("button", { class: "btn ghost icon", onclick: close, title: "Close" }, icon("x"))),
      h("div", { class: "mb" }, body),
      actions.length ? h("div", { class: "mf" }, actions.map((a) => h("button", {
        class: "btn " + (a.kind || ""), onclick: async (ev) => {
          if (!a.run) return close();
          ev.target.disabled = true;
          try { if ((await a.run()) !== false) close(); } catch (e) { fail(e); } finally { ev.target.disabled = false; }
        } }, a.label))) : null));
  $("#modal-root").append(back); document.addEventListener("keydown", esc);
  const f = back.querySelector("input,textarea,select"); if (f) setTimeout(() => f.focus(), 30);
  return close;
}
function confirmBox(title, text, label = "OK", kind = "primary") {
  return new Promise((res) => modal({ title, body: h("div", { class: "muted" }, text), onclose: () => res(false),
    actions: [{ label: "Cancel" }, { label, kind, run: () => { res(true); } }] }));
}

const store = {
  get(k, d) { try { const v = localStorage.getItem("usnf." + k); return v == null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem("usnf." + k, JSON.stringify(v)); } catch { /* private mode */ } },
};

// how the site looks, per browser: layout (side | top | rail), density, theme (dark | light | system)
const UI = { layout: store.get("ui.layout", "side"), density: store.get("ui.density", "comfortable"), theme: store.get("ui.theme", "dark") };
const lightQuery = window.matchMedia ? window.matchMedia("(prefers-color-scheme: light)") : null;
function applyUI() {
  const r = document.documentElement.dataset;
  r.layout = UI.layout; r.density = UI.density;
  r.theme = UI.theme === "system" ? (lightQuery && lightQuery.matches ? "light" : "dark") : UI.theme;
}
function setUI(k, v) { UI[k] = v; store.set("ui." + k, v); applyUI(); }
if (lightQuery) lightQuery.addEventListener("change", () => { if (UI.theme === "system") applyUI(); });
applyUI();

const STATE_CHIP = { queued: "", planning: "info", downloading: "info", extracting: "info", stopping: "warn",
  completed: "ok", failed: "bad", stopped: "warn" };
const stateChip = (s) => h("span", { class: "chip " + (STATE_CHIP[s] ?? "") }, s);
const VERDICT = { available: ["ok", "Available"], partial: ["warn", "Partial"], missing: ["bad", "Missing"], error: ["", "Check failed"], empty: ["", "Empty"] };
function healthChip(hl) {
  if (!hl || !hl.verdict) return h("span", { class: "faint" }, "—");
  const [k, label] = VERDICT[hl.verdict] || ["", hl.verdict];
  return h("span", { class: "chip " + k, title: hl.sampled ? `${hl.available}/${hl.sampled} sampled articles found · ${ago(hl.checked_at)}` : "" },
    h("span", { class: "dot" }), label);
}

// ---------------------------------------------------------------- shell
const NAV = [["catalog", "Catalog", "catalog"], ["activity", "Activity", "activity"], ["settings", "Settings", "settings"], ["system", "System", "system"]];
let page = { stop: [] };
function every(ms, fn) { fn(); const t = setInterval(fn, ms); page.stop.push(() => clearInterval(t)); }
function setTitle(text, crumb) { const t = $("#title"); t.replaceChildren(crumb ? h("span", { class: "crumb" }, crumb + " / ") : "", text); }
function toolbar(...els) { put($("#toolbar"), ...els); }

function buildNav(active) {
  $("#nav").replaceChildren(...NAV.map(([id, label, ic]) => h("a", { href: "#/" + id, class: active === id ? "on" : "", title: label },
    icon(ic), label, id === "activity" ? h("span", { class: "badge hidden", id: "nav-badge" }) : null)));
}

let lastStatus = null;
async function pollStatus() {
  try {
    const s = await api("/status"); lastStatus = s;
    const th = s.throttle || {};
    // the app icon follows the state: red while downloading, grey while paused
    const look = th.paused ? "paused" : s.running > 0 ? "busy" : "idle";
    const src = `/static/icon-${look}.png`;
    if ($("#logo") && !$("#logo").src.endsWith(src)) { $("#logo").src = src; $("#favicon").href = src; }
    renderStatusBar(s, look);
    $("#ver").textContent = "v" + s.version;
    const b = $("#nav-badge"); if (b) { b.textContent = s.active; b.classList.toggle("hidden", !s.active); }
  } catch { /* login page or offline */ }
}

// the status bar at the bottom of every page: the transfer in progress, speed, queue, free space, pause
const RUNNING = new Set(["planning", "downloading", "extracting", "stopping"]);
function renderStatusBar(s, look) {
  const bar = $("#statusbar"); if (!bar) return;
  const th = s.throttle || {};
  const jobs = s.transfer || [];
  const running = jobs.filter((j) => RUNNING.has(j.state));
  const cur = running[0];
  const waiting = Math.max(0, (s.active || 0) - running.length);
  const p = cur && cur.size ? pct(cur.bytes, cur.size) : 0;
  let main;
  if (th.paused) {
    main = [h("b", {}, throttleText(th)), h("span", { class: "faint" }, s.active ? `${s.active} waiting in the queue` : "nothing waiting")];
  } else if (cur) {
    const verb = { planning: "Preparing", downloading: "Downloading", extracting: "Extracting", stopping: "Stopping" }[cur.state] || cur.state;
    const fp = cur.file_pos && cur.file_pos[1] > 1 ? `file ${cur.file_pos[0].toLocaleString()} of ${cur.file_pos[1].toLocaleString()}` : "";
    main = [h("span", { class: "sb-verb" }, verb), h("b", { class: "sb-name", title: cur.name }, cur.name),
      cur.state === "planning" ? h("span", { class: "faint" }, "loading the share index…") : null,
      fp ? h("span", { class: "faint nowrap sb-fp" }, fp) : null,
      cur.current ? h("span", { class: "faint sb-file", title: cur.current }, cur.current) : null,
      running.length > 1 ? h("span", { class: "chip plain" }, `+${running.length - 1} more running`) : null];
  } else {
    main = [h("b", {}, waiting ? `${waiting} queued` : "Idle"), h("span", { class: "faint" }, waiting ? "waiting to start" : "nothing downloading")];
  }
  const prog = cur && cur.state === "downloading" ? h("div", { class: "sb-prog" },
    h("div", { class: "bar" }, h("i", { style: { width: p + "%" } })),
    h("div", { class: "sb-pline" }, h("b", {}, p.toFixed(1) + "%"), h("span", { class: "sb-of" }, ` · ${fmtBytes(cur.bytes)} of ${fmtBytes(cur.size)}`),
      cur.eta ? [" · ETA ", h("b", {}, fmtEta(cur.eta))] : null)) : null;
  put(bar,
    h("img", { class: "sb-icon", src: `/static/icon-${look}.png`, alt: "" }),
    h("div", { class: "sb-main" }, main),
    prog,
    h("div", { class: "sb-stats" },
      th.paused ? null : h("span", { class: "sb-speed", title: th.limit ? `Limited to ${fmtRate(th.limit)}` : "Download speed" },
        icon("download"), h("b", {}, s.speed > 0 ? fmtRate(s.speed) : "0 B/s"), th.limit ? h("span", { class: "faint" }, " / " + fmtRate(th.limit)) : null),
      cur && waiting ? h("span", { class: "sb-queue" }, h("b", {}, waiting), " queued") : null,
      s.disk && s.disk.free != null ? h("span", { class: "sb-free", title: s.disk.path }, h("b", {}, fmtBytes(s.disk.free)), " free") : null),
    h("button", { class: "btn sm " + (th.paused ? "primary" : "ghost"), title: th.paused ? "Resume downloads" : "Pause downloads",
      onclick: (ev) => { ev.stopPropagation(); (th.paused ? resumeAll() : pausePrompt()).catch(fail); } }, icon(th.paused ? "play" : "pause"), h("span", { class: "sb-btxt" }, th.paused ? "Resume" : "Pause")));
  bar.className = "statusbar " + look;
  document.title = cur && cur.state === "downloading" ? `${p.toFixed(0)}% · ${fmtRate(s.speed)} — usnfetch` : th.paused ? "Paused — usnfetch" : "usnfetch";
}

const ROUTES = { catalog: pageCatalog, share: pageShare, activity: pageActivity, settings: pageSettings, system: pageSystem, login: pageLogin, setup: pageSetup };
async function route() {
  page.stop.forEach((f) => f()); page = { stop: [] };
  const parts = location.hash.replace(/^#\/?/, "").split("/");
  const name = ROUTES[parts[0]] ? parts[0] : "catalog";
  buildNav(name === "share" ? "catalog" : name);
  $("#view").replaceChildren(); toolbar();
  try { await ROUTES[name](parts.slice(1)); } catch (e) { if (e.status !== 401) { fail(e); } }
}

// ---------------------------------------------------------------- login
async function pageLogin() {
  setTitle("Log in");
  const pw = h("input", { class: "input", type: "password", placeholder: "Password", autocomplete: "current-password" });
  const go = async (ev) => {
    ev && ev.preventDefault();
    try { await api("/login", { body: { password: pw.value } }); location.hash = "#/catalog"; } catch (e) { fail(e); pw.select(); }
  };
  $("#view").append(h("div", { class: "center" }, h("form", { class: "panel login", onsubmit: go },
    h("h2", {}, "usnfetch"), h("div", { class: "muted" }, "Enter the web password set on the machine running usnfetch."),
    pw, h("button", { class: "btn primary", type: "submit" }, "Log in"))));
  pw.focus();
}

// ---------------------------------------------------------------- catalog
async function openShare(body) {
  const s = await api("/shares/open", { body });
  if (body.link) store.set("link." + s.id, body.link);
  location.hash = "#/share/" + s.id;
}

function importDialog(after) {
  const name = h("input", { class: "input", placeholder: "Source name (optional)" });
  const text = h("textarea", { class: "input", rows: 9, placeholder: "Paste text with usn:// links, or pick a file above (a .txt list, CSV, saved web page…)" });
  const file = h("input", { type: "file", accept: ".txt,.csv,.html,.htm,.md,.json,text/*", onchange: async () => {
    const f = file.files[0]; if (!f) return; text.value = await f.text(); if (!name.value) name.value = f.name; } });
  modal({ title: "Import a link list", body: [h("div", { class: "muted" }, "Any text with usn:// links works. The exported layout (SOURCE / title [size] / SKU / label / link) keeps titles, sizes and sources. Re-importing an updated list only adds what's new."), file, text, name],
    actions: [{ label: "Cancel" }, { label: "Import", kind: "primary", run: async () => {
      const r = await api("/catalog/import", { body: { text: text.value, name: name.value || "import" } });
      toast(`${r.found} links: ${r.added} new, ${r.updated} updated`, "ok"); after && after();
    } }] });
}

function openLinkDialog() {
  const link = h("textarea", { class: "input", rows: 4, placeholder: "usn://…" });
  modal({ title: "Open a share link", body: [h("div", { class: "muted" }, "Browse a share without adding it to the catalog."), link],
    actions: [{ label: "Cancel" }, { label: "Open", kind: "primary", run: () => openShare({ link: link.value.trim() }) }] });
}

async function pageCatalog() {
  setTitle("Catalog");
  const f = { q: store.get("cat.q", ""), source: store.get("cat.source", ""), hide: store.get("cat.hide", false), sort: store.get("cat.sort", "title") };
  let data = { entries: [], sources: [], categories: ["Default"] };
  const sel = new Set();
  const view = $("#view");

  const q = h("input", { class: "input", placeholder: "Search titles, sources, SKUs…", value: f.q, oninput: () => { f.q = q.value; store.set("cat.q", f.q); render(); } });
  const src = h("select", { class: "input", onchange: () => { f.source = src.value; store.set("cat.source", f.source); render(); } });
  const sort = h("select", { class: "input", onchange: () => { f.sort = sort.value; store.set("cat.sort", f.sort); render(); } },
    [["title", "Title"], ["size-desc", "Size: largest"], ["size-asc", "Size: smallest"], ["added", "Recently added"], ["status", "Status"]]
      .map(([v, l]) => h("option", { value: v, selected: f.sort === v }, l)));
  const hide = h("input", { type: "checkbox", checked: f.hide, onchange: () => { f.hide = hide.checked; store.set("cat.hide", f.hide); render(); } });
  toolbar(h("div", { class: "search" }, icon("search"), q), src, sort, h("label", { class: "check", title: "Hide entries that are queued or already downloaded" }, hide, "Hide queued / done"),
    h("button", { class: "btn", onclick: openLinkDialog }, icon("link"), "Open link"),
    h("button", { class: "btn primary", onclick: () => importDialog(load) }, icon("upload"), "Import list"));

  const selbar = h("div", { class: "selbar hidden" });
  const count = h("div", { class: "countline" });
  const tbody = h("tbody");
  const head = h("input", { type: "checkbox", onchange: () => { for (const e of visible()) head.checked ? sel.add(e.id) : sel.delete(e.id); render(); } });
  const sortHeads = [];
  function cth(label, key, cls = "") {   // a header that sorts; size toggles largest / smallest first
    const th = h("th", { class: "sortable " + cls, title: "Sort", onclick: () => {
      const v = key === "size" ? (f.sort === "size-desc" ? "size-asc" : "size-desc") : key;
      f.sort = v; sort.value = v; store.set("cat.sort", v); render();
    } }, label, h("span", { class: "arrow" }));
    sortHeads.push([th, key]);
    return th;
  }
  const table = h("div", { class: "tablewrap" }, h("table", { class: "t" },
    h("thead", {}, h("tr", {}, h("th", { class: "cb" }, head), cth("Title", "title"), cth("Listed", "size", "right"), cth("Actual", "size", "right"),
      h("th", {}, "Health"), cth("Status", "status"), h("th", { class: "act" }, ""))), tbody));
  view.append(selbar, count, table);
  api("/status").then((st) => {
    if (st.needs_setup) view.prepend(h("div", { class: "note warn", style: { marginBottom: "14px" } }, "No Usenet provider yet. ",
      h("a", { href: "#/setup" }, "Run the setup"), " to add one and choose where downloads go."));
  }).catch(() => {});

  const size = (e) => (e.share && e.share.size) || e.listed_size || 0;
  const done = (e) => e.job && ["queued", "planning", "downloading", "extracting", "completed"].includes(e.job.state);
  function visible() {
    const words = f.q.toLowerCase().split(/\s+/).filter(Boolean);
    let out = data.entries.filter((e) => (!f.source || e.source === f.source) && !(f.hide && done(e)) &&
      words.every((w) => `${e.title} ${e.variant} ${e.listed} ${e.source} ${e.sku} ${e.name} ${(e.also || []).join(" ")}`.toLowerCase().includes(w)));
    const by = { "size-desc": (a, b) => size(b) - size(a), "size-asc": (a, b) => size(a) - size(b), added: (a, b) => (b.added || 0) - (a.added || 0),
      status: (a, b) => (b.job ? 1 : 0) - (a.job ? 1 : 0), title: (a, b) => a.title.localeCompare(b.title) }[f.sort];
    return out.sort(by);
  }

  function statusCell(e) {
    const j = e.job; if (!j) return h("span", { class: "faint" }, "—");
    if (j.state === "downloading" && j.size) {
      const p = pct(j.bytes, j.size);
      return h("div", { class: "nowrap" }, h("span", { class: "bar mini" }, h("i", { style: { width: p + "%" } })), " ", h("span", { class: "muted" }, p.toFixed(1) + "%"));
    }
    return stateChip(j.state);
  }

  function render() {
    const vis = visible();
    tbody.replaceChildren(...vis.map((e) => {
      const cb = h("input", { type: "checkbox", checked: sel.has(e.id), onchange: () => { cb.checked ? sel.add(e.id) : sel.delete(e.id); render(); } });
      return h("tr", { class: sel.has(e.id) ? "sel" : "" },
        h("td", { class: "cb" }, cb),
        h("td", { class: "title-cell" }, h("div", { class: "t1" }, e.title),
          h("div", { class: "t2" }, e.variant ? h("span", {}, e.variant) : null, e.source ? h("span", { class: "chip plain" }, e.source) : null,
            e.sku ? h("span", { class: "faint mono" }, e.sku) : null,
            e.info ? h("a", { href: e.info, target: "_blank", rel: "noopener", title: e.info }, icon("ext")) : null)),
        h("td", { class: "right nowrap muted" }, e.listed || "—"),
        h("td", { class: "right nowrap" }, e.share ? [fmtBytes(e.share.size), h("div", { class: "faint", style: { fontSize: "12px" } }, `${e.share.files} file${e.share.files === 1 ? "" : "s"}`)] : h("span", { class: "faint" }, "—")),
        h("td", {}, healthChip(e.health)),
        h("td", {}, statusCell(e)),
        h("td", { class: "act" },
          h("button", { class: "btn sm", title: "Browse files, check availability, stream", onclick: () => openShare({ catalog_id: e.id }).catch(fail) }, icon("folder"), "Browse"),
          " ", h("button", { class: "btn sm ghost icon", title: "Download everything", onclick: () => queue([e.id]) }, icon("download"))));
    }));
    head.checked = vis.length > 0 && vis.every((e) => sel.has(e.id));
    for (const [th, key] of sortHeads) {
      const on = key === "size" ? f.sort.startsWith("size") : f.sort === key;
      th.classList.toggle("sorted", on);
      th.querySelector(".arrow").textContent = on ? (f.sort === "size-asc" ? " ↑" : key === "size" ? " ↓" : " ↓") : "";
    }
    const total = data.entries.length, selected = data.entries.filter((e) => sel.has(e.id));
    put(count, h("span", {}, `${vis.length} of ${total} entries`),
      h("span", {}, `${data.entries.filter((e) => e.job && e.job.state === "completed").length} downloaded`),
      h("span", {}, `${data.entries.filter((e) => e.health && e.health.verdict === "available").length} checked available`));
    selbar.classList.toggle("hidden", !selected.length);
    if (selected.length) {
      const cat = h("select", { class: "input" }, data.categories.map((c) => h("option", { value: c, selected: c === data.default_category }, c)));
      put(selbar, h("b", {}, `${selected.length} selected`), h("span", { class: "muted" }, fmtBytes(selected.reduce((a, e) => a + size(e), 0))),
        h("span", { class: "grow" }), h("span", { class: "muted" }, "Category"), cat,
        h("button", { class: "btn primary", onclick: () => queue([...sel], cat.value) }, icon("download"), "Download selected"),
        h("button", { class: "btn danger", onclick: async () => {
          if (!(await confirmBox("Remove from catalog", `Remove ${selected.length} entr${selected.length === 1 ? "y" : "ies"} from the catalog? Downloads are not touched.`, "Remove", "danger solid"))) return;
          await api("/catalog/remove", { body: { ids: [...sel] } }).catch(fail); sel.clear(); load();
        } }, icon("trash"), "Remove"),
        h("button", { class: "btn ghost", onclick: () => { sel.clear(); render(); } }, "Clear"));
    }
    if (!total) {
      tbody.replaceChildren(h("tr", {}, h("td", { colspan: 7 }, h("div", { class: "empty left" }, h("h3", {}, "No shares yet"),
        h("div", { class: "muted" }, "usnfetch doesn't come with any shares. Add the share links you have:"), addSharesHelp(true)))));
    }
  }

  async function queue(ids, category) {
    try {
      const r = await api("/catalog/download", { body: { ids, category: category || data.default_category } });
      toast(`Queued ${r.jobs.length} download${r.jobs.length === 1 ? "" : "s"}` + (r.errors.length ? ` · ${r.errors.length} skipped` : ""), "ok");
      sel.clear(); load();
    } catch (e) { fail(e); }
  }

  async function load() {
    data = await api("/catalog");
    src.replaceChildren(h("option", { value: "" }, "All sources"), ...data.sources.map((s) => h("option", { value: s, selected: f.source === s }, s)));
    render();
  }
  await load();
  if (pendingCatalogAction) {
    const act = pendingCatalogAction; pendingCatalogAction = null;
    act === "import" ? importDialog(load) : openLinkDialog();
  }
  every(4000, () => { if (!document.hidden && !$(".modal-back")) load().catch(() => {}); });
}

// ---------------------------------------------------------------- basket
// What you tick in share browsers, per share: kept in this browser while you move between
// folders and shares, until you queue it or clear it.
const shareTitle = (s) => (s.catalog && s.catalog.title) || s.title || "Share";
const basket = {
  all: store.get("basket", {}),        // sid -> { title, items: { path: { folder, size, count } } }
  save() { store.set("basket", this.all); renderBasket(); if (page.onBasket) page.onBasket(); },
  items(sid) { return (this.all[sid] && this.all[sid].items) || {}; },
  has(sid, p) { return Object.prototype.hasOwnProperty.call(this.items(sid), p); },
  coveredBy(sid, p) {                  // the selected folder above p, if any ("" = the whole share)
    const it = this.items(sid);
    if (p !== "" && it[""]) return "";
    for (let i = p.lastIndexOf("/"); i > 0; i = p.lastIndexOf("/", i - 1)) { const d = p.slice(0, i); if (it[d] && it[d].folder) return d; }
    return null;
  },
  add(s, p, meta, save = true) {
    const g = this.all[s.id] || (this.all[s.id] = { title: shareTitle(s), items: {} });
    if (meta.folder) for (const k of Object.keys(g.items)) if (p === "" || k.startsWith(p + "/")) delete g.items[k];
    g.items[p] = meta;
    if (save) this.save();
  },
  remove(sid, p, save = true) {
    const g = this.all[sid]; if (!g) return;
    delete g.items[p]; if (!Object.keys(g.items).length) delete this.all[sid];
    if (save) this.save();
  },
  clear(sid) { delete this.all[sid]; this.save(); },
  totals(sid) {
    let n = 0, size = 0, files = 0;
    for (const v of Object.values(this.items(sid))) { n++; size += v.size || 0; files += v.folder ? v.count || 0 : 1; }
    return { n, size, files };
  },
  body(sid) { const it = this.items(sid), ks = Object.keys(it); return { paths: ks.filter((k) => !it[k].folder), folders: ks.filter((k) => it[k].folder) }; },
};

function renderBasket() {
  const el = $("#basket"); if (!el) return;
  const groups = Object.entries(basket.all);
  el.classList.toggle("hidden", !groups.length);
  const items = groups.reduce((a, [sid]) => a + basket.totals(sid).n, 0);
  put(el, h("button", { class: "bh", type: "button", title: "What you have ticked in share pages" }, icon("check"), h("span", { class: "lbl" }, "Selected"), h("span", { class: "count" }, items)),
    h("div", { class: "bl" }, groups.map(([sid, g]) => {
      const t = basket.totals(sid);
      return h("div", { class: "bg" },
        h("a", { href: "#/share/" + sid, title: g.title }, h("span", { class: "nm" }, g.title || "Share"),
          h("span", { class: "meta" }, `${t.n} item${t.n === 1 ? "" : "s"} · ${fmtBytes(t.size)}`)),
        h("button", { class: "btn ghost icon", title: "Clear this selection", onclick: () => basket.clear(sid) }, icon("x")));
    })));
}

// ---------------------------------------------------------------- review before queueing
const safeSeg = (x) => String(x || "").replace(/[<>:"|?*\x00-\x1f]/g, "_").replace(/[ .]+$/, "") || "_";

// reviewDownload shows what a download will do -- size against free space, where the files
// land, what happens to archives -- and queues it. sel: null = the whole share.
async function reviewDownload(s, sel, done) {
  const plan = await api(`/shares/${s.id}/plan`, { body: sel || {} });
  let mode = plan.extract_default;
  const cat = h("select", { class: "input" }, plan.categories.map((c) => h("option", { value: c, selected: c === plan.category }, c)));
  const name = h("input", { class: "input", value: plan.name });
  const dest = h("div", { class: "dest mono" });
  const showDest = () => { dest.textContent = [plan.complete_dir, safeSeg(cat.value), safeSeg(name.value.trim() || plan.name)].join(plan.sep) + plan.sep + (plan.example || ""); };
  let existing = "skip";
  const haveLine = h("span", { class: "muted" }, " · checking…");
  const haveChoices = h("div", { class: "choices hidden" }, [
    ["skip", "Skip them", "A file with the same name and size counts as done."],
    ["verify", "Check them first", "Compare each one's SHA-256 with the share and download only what differs. Repairs a damaged copy, but reads every file."],
    ["redownload", "Download everything again", "Overwrite what is there."],
  ].map(([v, label, help]) => h("label", { class: "choice" },
    h("input", { type: "radio", name: "xexist", value: v, checked: v === existing, onchange: () => { existing = v; } }),
    h("div", {}, h("div", { class: "cl" }, label), h("div", { class: "ch" }, help)))));
  let haveTimer = null, haveSeq = 0;
  async function countHave() {
    const seq = ++haveSeq;
    haveLine.textContent = " · checking…";
    let r;
    try { r = await api(`/shares/${s.id}/ondisk`, { body: { ...(sel || {}), category: cat.value, name: name.value.trim() } }); }
    catch (e) { haveLine.textContent = ""; return; }
    if (seq !== haveSeq) return;
    if (!r.counted) { haveLine.textContent = " · not counted for a selection this large"; haveChoices.classList.remove("hidden"); return; }
    const any = r.files > 0 || r.partial > 0;
    haveChoices.classList.toggle("hidden", !any);
    haveLine.textContent = !any ? " · none of these files are there yet"
      : ` · ${r.files.toLocaleString()} of ${r.of.toLocaleString()} file${r.of === 1 ? "" : "s"} (${fmtBytes(r.bytes)}) already there` +
        (r.partial ? `, ${r.partial.toLocaleString()} partly downloaded (those carry on)` : "") + ` · about ${fmtBytes(Math.max(0, plan.size - r.bytes))} left to download`;
  }
  const recount = () => { clearTimeout(haveTimer); haveTimer = setTimeout(countHave, 400); };
  cat.onchange = () => { showDest(); recount(); }; name.oninput = () => { showDest(); recount(); }; showDest(); countHave();
  const a = plan.archives;
  const found = [a.single ? `${a.single.toLocaleString()} single .zip/.7z` : null, a.rar ? `${a.rar.toLocaleString()} RAR` : null,
    a.split ? `${a.split.toLocaleString()} multi-part set${a.split === 1 ? "" : "s"}` : null, a.empty ? `${a.empty} empty file${a.empty === 1 ? "" : "s"} named like archives` : null].filter(Boolean);
  const opts = [
    ["off", "Don't extract", "Keep every file exactly as it was shared."],
    ["sets", "RAR and multi-part sets only", "Single .zip and .7z files stay zipped, so ROM, BIOS and asset packs keep working."],
    ["all", "Everything", "Also unpack every single .zip and .7z" + (a.single ? ` (${a.single.toLocaleString()} in this selection)` : "") + "."],
  ];
  const choices = h("div", { class: "choices" }, opts.map(([v, label, help]) => h("label", { class: "choice" },
    h("input", { type: "radio", name: "xmode", value: v, checked: v === mode, onchange: () => { mode = v; } }),
    h("div", {}, h("div", { class: "cl" }, label, v === plan.extract_default ? h("span", { class: "chip plain" }, "default") : null), h("div", { class: "ch" }, help)))));
  const over = plan.free != null && plan.size > plan.free;
  modal({ title: "Download from " + shareTitle(s), body: [
    h("div", { class: "facts" },
      h("div", {}, h("div", { class: "k" }, "Files"), h("div", { class: "v" }, plan.files.toLocaleString())),
      h("div", {}, h("div", { class: "k" }, "Size"), h("div", { class: "v" }, fmtBytes(plan.size))),
      h("div", {}, h("div", { class: "k" }, "Free space"), h("div", { class: "v" + (over ? " bad" : "") }, plan.free != null ? fmtBytes(plan.free) : "—"))),
    over ? h("div", { class: "note bad" }, `Not enough space: ${fmtBytes(plan.size - plan.free)} more is needed in ${plan.complete_dir}. The job fails when it starts unless space is freed first.`) : null,
    h("div", { class: "form tight" }, h("label", { class: "k" }, "Category"), cat, h("label", { class: "k" }, "Folder"), name),
    h("div", {}, h("div", { class: "k2" }, "Files go to"), dest),
    h("div", {}, h("div", { class: "k2" }, "Archives", h("span", { class: "muted" }, found.length ? " · this selection has " + found.join(", ") : " · none in this selection")), choices,
      plan.delete_archives ? h("div", { class: "help" }, "Archives that extract cleanly are deleted afterwards (Settings → Downloads).") : null),
    h("div", {}, h("div", { class: "k2" }, "Files already there", haveLine), haveChoices),
    plan.active_jobs && plan.active_jobs.length ? h("div", { class: "note" }, `Another download from this share is ${plan.active_jobs[0].state}: this one starts when it finishes.`) : null,
  ], actions: [{ label: "Cancel" }, { label: over ? "Queue anyway" : "Queue download", kind: "primary", run: async () => {
    const r = await api(`/shares/${s.id}/download`, { body: { ...(sel || {}), category: cat.value, name: name.value.trim(), extract: mode, existing } });
    toast(`Queued ${r.files.toLocaleString()} file${r.files === 1 ? "" : "s"} · ${fmtBytes(r.size)}`, "ok");
    if (done) done(r);
  } }] });
}

// ---------------------------------------------------------------- share browser
async function pageShare(parts) {
  const sid = parts[0];
  const path = decodeURIComponent(parts.slice(1).join("/"));
  const view = $("#view");
  setTitle("Opening share…", "Catalog");
  let s;
  try { s = await api("/shares/" + sid); }
  catch (e) {
    if (e.code !== "not_open") throw e;
    const link = store.get("link." + sid, null);
    s = await api("/shares/open", { body: link ? { link } : { catalog_id: sid } });
  }

  // loading / error / password
  if (s.state !== "ready") {
    const box = h("div", { class: "panel" }, h("div", { class: "head" }, h("span", { class: "spin" }), h("h2", {}, s.title || "Loading share")),
      h("div", { class: "body" }, h("div", { class: "muted", style: { marginBottom: "10px" } }, "Fetching the share's manifest and index from Usenet. Large shares take a few seconds per TB the first time; after that the index is cached."), h("div", { class: "loglines" })));
    view.replaceChildren(box);
    await new Promise((resolve) => {
      every(900, async () => {
        try { s = await api("/shares/" + sid); }
        catch (e) {
          if (e.code !== "not_open") { fail(e); return; }
          try { const link = store.get("link." + sid, null); s = await api("/shares/open", { body: link ? { link } : { catalog_id: sid } }); }   // server restarted: start again
          catch (e2) { fail(e2); }
          return;
        }
        $(".loglines", box).textContent = (s.log || []).join("\n");
        if (s.state !== "loading") { page.stop.forEach((f) => f()); page.stop = []; resolve(); }
      });
    });
    if (s.state === "password") return sharePassword(s);
    if (s.state === "error") {
      setTitle(s.title || "Share", "Catalog");
      view.replaceChildren(h("div", { class: "note bad" }, s.error), h("p", {}, h("button", { class: "btn", onclick: () => openShare(store.get("link." + sid, null) ? { link: store.get("link." + sid) } : { catalog_id: sid }).then(route).catch(fail) }, icon("refresh"), "Try again")),
        h("div", { class: "loglines" }, (s.log || []).join("\n")));
      return;
    }
  }
  renderShare(s, path);
}

function sharePassword(s) {
  setTitle(s.title || "Share", "Catalog");
  const pw = h("input", { class: "input", type: "password", placeholder: "Share password" });
  $("#view").replaceChildren(h("form", { class: "panel login", onsubmit: async (ev) => {
    ev.preventDefault(); try { await api("/shares/open", { body: { link: s.link, password: pw.value } }); route(); } catch (e) { fail(e); } } },
  h("h2", {}, "Password-protected share"), h("div", { class: "muted" }, s.error), pw, h("button", { class: "btn primary" }, "Open")));
}

async function renderShare(s, path) {
  const sid = s.id, info = s.info || {}, cat = s.catalog;
  setTitle(shareTitle(s), "Catalog");
  if (basket.all[sid] && basket.all[sid].title !== shareTitle(s)) { basket.all[sid].title = shareTitle(s); basket.save(); }
  toolbar(h("button", { class: "btn ghost", onclick: () => (location.hash = "#/catalog") }, icon("back"), "Catalog"),
    h("button", { class: "btn", onclick: () => runCheck(64) }, icon("pulse"), "Check availability"),
    h("button", { class: "btn primary", onclick: () => reviewDownload(s, null).catch(fail) }, icon("download"), "Download all…"));

  const cards = h("div", { class: "cards" });
  const selbar = h("div", { class: "selbar hidden" });
  const crumbs = h("div", { class: "crumbs" });
  const leaf = path ? path.split("/").pop() : "";
  let view = store.get("view." + sid, "list"), galleryKind = "";
  let hideHave = store.get("hidehave", false);
  const viewSw = h("div", { class: "seg hidden" },
    h("button", { class: "btn sm", title: "List", onclick: () => setView("list") }, icon("list"), "List"),
    h("button", { class: "btn sm", title: "Box art", onclick: () => setView("gallery") }, icon("grid"), "Gallery"));
  let ssort = store.get("share.sort", "name");
  const sortSel = h("select", { class: "input sm", title: "Sort", onchange: () => setSort(sortSel.value) },
    [["name", "Name"], ["size-desc", "Largest first"], ["size-asc", "Smallest first"]].map(([v, l]) => h("option", { value: v, selected: ssort === v }, l)));
  const thName = h("th", { class: "sortable", title: "Sort by name", onclick: () => setSort("name") }, "Name", h("span", { class: "arrow" }));
  const thSize = h("th", { class: "sortable right", title: "Sort by size", onclick: () => setSort(ssort === "size-desc" ? "size-asc" : "size-desc") }, "Size", h("span", { class: "arrow" }));
  function showSort() {
    sortSel.value = ssort;
    thName.classList.toggle("sorted", ssort === "name"); thSize.classList.toggle("sorted", ssort !== "name");
    thName.querySelector(".arrow").textContent = ssort === "name" ? " ↓" : "";
    thSize.querySelector(".arrow").textContent = ssort === "size-desc" ? " ↓" : ssort === "size-asc" ? " ↑" : "";
  }
  function setSort(v) {
    ssort = v; store.set("share.sort", v); showSort();
    if (mode === "search") runSearch(); else load();
  }
  showSort();
  const hideBox = h("input", { type: "checkbox", checked: hideHave, onchange: () => { hideHave = hideBox.checked; store.set("hidehave", hideHave); if (mode === "search") runSearch(); else load(); } });
  let scopeAll = !path;
  const q = h("input", { class: "input", type: "search", placeholder: path ? `Search in ${leaf}…` : "Search this share…", autocomplete: "off",
    oninput: () => schedule(), onkeydown: (ev) => { if (ev.key === "Escape" && q.value) { ev.stopPropagation(); q.value = ""; schedule(0); } } });
  const tbody = h("tbody");
  const head = h("input", { type: "checkbox", title: "Select everything listed" });
  const listing = h("div", { class: "tablewrap" }, h("table", { class: "t" }, h("thead", {}, h("tr", {}, h("th", { class: "cb" }, head), thName,
    thSize, h("th", {}, ""), h("th", { class: "act" }, ""))), tbody));
  const foot = h("div", { class: "countline", style: { marginTop: "10px" } });
  const grid = h("div", { class: "gallery hidden" });
  $("#view").replaceChildren(cards, selbar, h("div", { class: "browsebar" }, crumbs, viewSw,
    sortSel, h("label", { class: "check", title: "Hide files that are already in your download folder" }, hideBox, "Hide what I have"),
    h("div", { class: "search" }, icon("search"), q)), listing, grid, foot);

  function renderCards() {
    const ck = s.check;
    const fp = (info.owner_fingerprint || "").slice(0, 16);
    const avail = s.checking ? h("div", { class: "v" }, h("span", { class: "spin" }), " checking…")
      : ck && ck.verdict ? h("div", { class: "v" }, healthChip(ck)) : h("div", { class: "v faint" }, "not checked");
    put(cards,
      h("div", { class: "card" }, h("div", { class: "k" }, "Size"), h("div", { class: "v" }, fmtBytes(info.size)),
        h("div", { class: "s" }, cat && cat.listed ? `listed as ${cat.listed}` : "")),
      h("div", { class: "card" }, h("div", { class: "k" }, "Files"), h("div", { class: "v" }, (info.files || 0).toLocaleString()),
        h("div", { class: "s" }, info.packed ? `${info.packed.toLocaleString()} small files packed` : "no packed files")),
      h("div", { class: "card" }, h("div", { class: "k" }, "Format"), h("div", { class: "v" }, `v${info.version ?? "?"} · ${info.access || "?"}`),
        h("div", { class: "s" }, info.created_at ? `published ${new Date(info.created_at).toLocaleDateString()}` : info.format || "")),
      h("div", { class: "card" }, h("div", { class: "k" }, "Uploader"), h("div", { class: "v mono", title: info.owner_fingerprint || "" }, fp || "—"),
        h("div", { class: "s" }, info.signature || "")),
      h("div", { class: "card wide" }, h("div", { class: "k" }, "Availability"), avail,
        ck && ck.accounts && ck.accounts.length ? h("div", { class: "acct-list" }, ck.accounts.map((a) => h("div", { class: "r" },
          h("span", {}, a.account, a.backup ? h("span", { class: "faint" }, ` (priority ${a.priority ?? 1})`) : ""),
          a.error ? h("span", { class: "chip bad", title: a.error }, "error") : h("b", {}, `${a.available}/${ck.sampled}`)))) : null,
        h("div", { class: "s" }, ck && ck.checked_at ? `${ck.sampled} articles sampled · ${ago(ck.checked_at)} · ` : "",
          h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); runCheck(512); } }, "thorough check (512)"))));
  }

  async function runCheck(sample) {
    try { s = await api(`/shares/${sid}/check`, { body: { sample } }); renderCards(); }
    catch (e) { fail(e); return; }
    const t = setInterval(async () => {
      try { s = await api("/shares/" + sid); } catch { clearInterval(t); return; }
      renderCards();
      if (!s.checking) { clearInterval(t); const v = (s.check || {}).verdict; toast(`Availability: ${(VERDICT[v] || ["", v])[1]}`, v === "available" ? "ok" : v === "partial" ? "" : "bad"); }
    }, 1200);
    page.stop.push(() => clearInterval(t));
  }

  // ---- selection: rows bind to the basket
  let boxes = [];   // { cb, p, meta } of the rows on screen
  function selBox(p, meta) {
    const cb = h("input", { type: "checkbox", onclick: (ev) => ev.stopPropagation(),
      onchange: () => { cb.checked ? basket.add(s, p, meta) : basket.remove(sid, p); } });
    boxes.push({ cb, p, meta });
    return cb;
  }
  function refreshSel() {
    for (const b of boxes) {
      const via = basket.coveredBy(sid, b.p);
      b.cb.checked = via != null || basket.has(sid, b.p);
      b.cb.disabled = via != null;
      b.cb.title = via == null ? "" : via === "" ? "Included: the whole share is selected" : `Included: folder ${via} is selected`;
      const tr = b.cb.closest("tr, .gcard"); if (tr) tr.classList.toggle("sel", b.cb.checked);
    }
    head.checked = boxes.length > 0 && boxes.every((b) => b.cb.checked);
    const t = basket.totals(sid);
    selbar.classList.toggle("hidden", !t.n);
    if (t.n) put(selbar, h("b", {}, `${t.n.toLocaleString()} selected`),
      h("span", { class: "muted" }, `${t.files.toLocaleString()} file${t.files === 1 ? "" : "s"} · ${fmtBytes(t.size)}`),
      h("span", { class: "faint" }, "kept while you browse other folders"), h("span", { class: "grow" }),
      h("button", { class: "btn ghost", onclick: () => basket.clear(sid) }, "Clear"),
      h("button", { class: "btn primary", onclick: () => reviewDownload(s, basket.body(sid), () => basket.clear(sid)).catch(fail) }, icon("download"), "Review & download…"));
  }
  page.onBasket = refreshSel;
  head.onchange = () => {
    const on = head.checked;
    if (mode === "list" && lst.t && lst.t.truncated) {     // not every file is listed: take the folder itself
      on ? basket.add(s, path, { folder: true, size: lst.t.size, count: lst.t.count }) : basket.remove(sid, path);
      return;
    }
    for (const b of boxes) if (!b.cb.disabled) on ? basket.add(s, b.p, b.meta, false) : basket.remove(sid, b.p, false);
    basket.save();
  };

  // ---- rows
  const fileUrl = (f, dl) => `/api/shares/${sid}/file/${f.fid}${dl ? "?download=1" : ""}`;
  async function preview(f) {
    const url = fileUrl(f);
    let body;
    if (f.mime.startsWith("video/")) body = h("video", { src: url, controls: true, autoplay: true });
    else if (f.mime.startsWith("audio/")) body = h("audio", { src: url, controls: true, autoplay: true });
    else if (f.mime.startsWith("image/")) body = h("img", { src: url, alt: f.name });
    else {
      const pre = h("pre", {}, "loading…"); body = pre;
      fetch(url, { headers: { Range: "bytes=0-262143" }, credentials: "same-origin" }).then((r) => r.text())
        .then((t) => { pre.textContent = t + (f.size > 262144 ? "\n\n… (first 256 KB)" : ""); }).catch((e) => { pre.textContent = String(e); });
    }
    modal({ title: f.name, wide: true, body: [body, h("div", { class: "muted" }, `${fmtBytes(f.size)} · streamed from Usenet, nothing is saved`)],
      actions: [{ label: "Save to this computer", run: () => { location.href = fileUrl(f, true); } }, { label: "Close", kind: "primary" }] });
  }
  const previewable = (f) => /^(video|audio|image|text)\//.test(f.mime) || /\.(txt|nfo|xml|json|cfg|ini|md|log|csv|sh|bat|yml|yaml|dat)$/i.test(f.name);
  const goFolder = (p) => { location.hash = `#/share/${sid}/${p.split("/").map(encodeURIComponent).join("/")}`; };
  const parentOf = (p) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");

  function folderRow(d, inSearch) {
    const sub = inSearch && parentOf(d.path) ? h("div", { class: "sub" }, parentOf(d.path)) : null;
    return h("tr", { class: "folder-row", onclick: () => goFolder(d.path) },
      h("td", { class: "cb" }, selBox(d.path, { folder: true, size: d.size, count: d.count })),
      h("td", {}, h("div", { class: "name-cell folder" }, icon("folder"), h("div", { class: "nmwrap" }, h("span", { class: "nm" }, d.name), sub))),
      h("td", { class: "right nowrap" }, fmtBytes(d.size)), h("td", { class: "muted nowrap" }, `${d.count.toLocaleString()} file${d.count === 1 ? "" : "s"}`,
        d.local ? [" ", h("span", { class: "chip ok plain", title: "This folder is in your download folder" }, "on disk")] : null), h("td", { class: "act" }));
  }
  function localChip(f) {
    if (f.local === "ok") return h("span", { class: "chip ok", title: "Already in your download folder" }, icon("check"), "on disk");
    if (f.local === "partial") return h("span", { class: "chip warn", title: "A download of this file is under way or was stopped part-way" }, "partial");
    if (f.local === "differs") return h("span", { class: "chip bad", title: "Your download folder has a file of this name but another size" }, "differs");
    return null;
  }
  function fileRow(f, inSearch) {
    const sub = inSearch ? h("a", { class: "sub", href: "#", title: "Open this folder", onclick: (ev) => { ev.preventDefault(); goFolder(f.folder || ""); } }, f.folder || "All files") : null;
    return h("tr", {},
      h("td", { class: "cb" }, selBox(f.path, { size: f.size })),
      h("td", {}, h("div", { class: "name-cell" }, icon("file"), h("div", { class: "nmwrap" },
        previewable(f) ? h("a", { class: "nm", href: "#", onclick: (ev) => { ev.preventDefault(); preview(f); } }, f.name) : h("span", { class: "nm" }, f.name), sub))),
      h("td", { class: "right nowrap" }, fmtBytes(f.size)),
      h("td", { class: "nowrap" }, localChip(f) || (f.packed ? h("span", { class: "chip packed", title: "Stored with other small files in one article" }, "packed") : f.size === 0 ? h("span", { class: "chip" }, "empty") : h("span", { class: "faint" }, `${f.segments.toLocaleString()} seg`))),
      h("td", { class: "act" },
        previewable(f) ? h("button", { class: "btn ghost icon", title: "Preview (streamed from Usenet)", onclick: () => preview(f) }, icon("eye")) : null,
        h("a", { class: "btn ghost icon", title: "Open in a new tab, straight from Usenet", href: fileUrl(f), target: "_blank" }, icon("ext")),
        h("a", { class: "btn ghost icon", title: "Save to this computer, straight from Usenet", href: fileUrl(f, true) }, icon("download"))));
  }

  // ---- folder listing: one folder at a time, its files in pages of PAGE
  const PAGE = 1000;
  let mode = "list";
  let lst = { t: null, files: [] };
  function renderFoot() {
    const t = lst.t;
    put(foot, h("span", {}, `${t.folders.length.toLocaleString()} folder${t.folders.length === 1 ? "" : "s"}, ${t.file_total.toLocaleString()} file${t.file_total === 1 ? "" : "s"} here`),
      h("span", {}, `${fmtBytes(t.size)} in this folder (${t.count.toLocaleString()} files in all)`),
      t.truncated ? [h("span", { class: "muted" }, `showing ${lst.files.length.toLocaleString()}`),
        h("button", { class: "btn sm", onclick: () => load(true) }, `Load ${Math.min(PAGE, t.file_total - lst.files.length).toLocaleString()} more`)] : null,
      lst.hidden ? h("span", { class: "muted" }, `${lst.hidden.toLocaleString()} you already have hidden`) : null,
      h("span", { class: "faint" }, "Open / save stream the file straight from Usenet — nothing is written on the server."));
  }

  // ---- gallery: box art for game folders (LaunchBox, RetroBat / EmulationStation) and picture folders
  function setView(v) {
    view = v; store.set("view." + sid, v);
    if (mode === "search") { q.value = ""; mode = "list"; }
    load();
  }
  let gal = { next: 0, truncated: false, shown: 0, matched: 0, hidden: 0 };
  const initials = (t) => (t.match(/[\p{L}\p{N}]+/gu) || []).slice(0, 3).map((w) => w[0]).join("").toUpperCase();
  function card(it) {
    const f = it.file;
    const ph = () => h("div", { class: "ph" }, h("b", {}, initials(it.title) || "?"), h("span", {}, it.title));
    const art = it.art ? h("img", { loading: "lazy", decoding: "async", alt: it.title, src: `/api/shares/${sid}/file/${it.art}`, onerror: (ev) => ev.target.replaceWith(ph()) }) : ph();
    return h("div", { class: "gcard" },
      h("div", { class: "art", title: "Details", onclick: () => cardInfo(it) }, art, localChip(f)),
      h("label", { class: "gmeta" }, selBox(f.path, { size: f.size }), h("div", { class: "gt" }, h("div", { class: "nm", title: f.name }, it.title), h("div", { class: "sub" }, fmtBytes(f.size)))));
  }
  function cardInfo(it) {
    const f = it.file;
    const inSel = () => basket.has(sid, f.path) || basket.coveredBy(sid, f.path) != null;
    modal({ title: it.title, wide: false, body: [
      it.art ? h("img", { class: "cover", src: `/api/shares/${sid}/file/${it.art}`, alt: it.title }) : null,
      h("div", { class: "facts" },
        h("div", {}, h("div", { class: "k" }, "File"), h("div", { class: "v small", title: f.name }, f.name)),
        h("div", {}, h("div", { class: "k" }, "Size"), h("div", { class: "v" }, fmtBytes(f.size))),
        h("div", {}, h("div", { class: "k" }, "On disk"), h("div", { class: "v" }, f.local === "ok" ? "yes" : f.local || "no"))),
    ], actions: [
      { label: "Save to this computer", run: () => { location.href = fileUrl(f, true); } },
      { label: inSel() ? "Selected" : "Add to selection", kind: "primary", run: () => { if (!inSel()) basket.add(s, f.path, { size: f.size }); } },
    ] });
  }
  async function loadGallery(more = false) {
    let g;
    if (!more) { put(grid, h("div", { class: "empty" }, h("span", { class: "spin" }), " finding box art…")); boxes = []; }
    try { g = await api(`/shares/${sid}/gallery?path=${encodeURIComponent(path)}&offset=${more ? gal.next : 0}&limit=300&sort=${ssort}`); }
    catch (e) { if (e.code === "not_open") { route(); return; } fail(e); return; }
    if (mode !== "list" || view !== "gallery") return;
    if (!more) {
      gal = { next: 0, truncated: false, shown: 0, matched: 0, hidden: 0 };
      grid.replaceChildren();
      if (g.folders.length) grid.append(h("div", { class: "gfolders" }, g.folders.map((d) => h("a", { class: "gfolder", href: "#", onclick: (ev) => { ev.preventDefault(); goFolder(d.path); } },
        icon("folder"), h("span", {}, d.name), h("span", { class: "faint" }, d.count.toLocaleString())))));
    }
    gal.next = g.next; gal.truncated = g.truncated; gal.matched += g.matched;
    const items = g.items.filter((it) => !(hideHave && it.file.local === "ok"));
    gal.hidden += g.items.length - items.length; gal.shown += items.length;
    grid.append(...items.map(card));
    if (!gal.shown && !g.folders.length) grid.append(h("div", { class: "empty" }, "Nothing to show here."));
    const kind = { launchbox: `LaunchBox art for ${g.platform}`, es: "art from the folders beside the games", images: "pictures in this folder" }[g.kind] || "";
    put(foot, h("span", {}, `${gal.shown.toLocaleString()} shown${kind ? " · " + kind : ""}`),
      g.kind !== "images" ? h("span", { class: "muted" }, `${gal.matched.toLocaleString()} with box art`) : null,
      gal.hidden ? h("span", { class: "muted" }, `${gal.hidden.toLocaleString()} you already have hidden`) : null,
      gal.truncated ? h("button", { class: "btn sm", onclick: () => loadGallery(true) }, "Load more") : null,
      h("span", { class: "faint" }, "Covers stream from Usenet as you scroll."));
    refreshSel();
  }
  async function load(more = false) {
    let t;
    const offset = more ? lst.files.length : 0;
    try { t = await api(`/shares/${sid}/tree?path=${encodeURIComponent(path)}&offset=${offset}&limit=${PAGE}&sort=${ssort}`); }
    catch (e) { if (e.code === "not_open") { route(); return; } fail(e); return; }   // closed (idle or restart): re-open
    if (mode !== "list") return;
    if (!more) {
      lst = { t, files: [] }; boxes = [];
      galleryKind = t.gallery || "";
      viewSw.classList.toggle("hidden", !galleryKind);
      for (const [i, b] of [...viewSw.children].entries()) b.classList.toggle("on", (i === 1) === (view === "gallery" && !!galleryKind));
      if (view === "gallery" && galleryKind) { listing.classList.add("hidden"); grid.classList.remove("hidden"); loadGallery(); return; }
      listing.classList.remove("hidden"); grid.classList.add("hidden");
    }
    lst.t.truncated = t.truncated;
    lst.files.push(...t.files);
    const rows = more ? [] : t.folders.map((d) => folderRow(d, false));
    let hidden = 0;
    for (const f of t.files) { if (hideHave && f.local === "ok") { hidden++; continue; } rows.push(fileRow(f, false)); }
    lst.hidden = (more ? lst.hidden || 0 : 0) + hidden;
    if (more) tbody.append(...rows);
    else {
      if (!rows.length) rows.push(h("tr", {}, h("td", { colspan: 5 }, h("div", { class: "empty" }, lst.hidden ? "You already have everything listed here." : "Empty folder"))));
      tbody.replaceChildren(...rows);
    }
    renderFoot(); refreshSel();
  }
  const segs = path ? path.split("/") : [];
  put(crumbs, h("a", { href: `#/share/${sid}`, class: segs.length ? "" : "cur" }, icon("folder"), " All files"),
    ...segs.flatMap((p, i) => [h("span", { class: "sep" }, "›"), h("a", { href: `#/share/${sid}/${segs.slice(0, i + 1).map(encodeURIComponent).join("/")}`, class: i === segs.length - 1 ? "cur" : "" }, p)]));

  // ---- search: words match anywhere in the path, at least one in the name itself
  let timer = null, ctl = null;
  function schedule(ms = 250) { clearTimeout(timer); timer = setTimeout(runSearch, ms); }
  page.stop.push(() => { clearTimeout(timer); if (ctl) ctl.abort(); });
  async function runSearch() {
    const text = q.value.trim();
    if (ctl) ctl.abort();
    if (text.length < 2) { if (mode !== "list") { mode = "list"; load(); } return; }
    mode = "search";
    ctl = new AbortController();
    const scope = scopeAll ? "" : path;
    put(foot, h("span", {}, h("span", { class: "spin" }), " searching…"));
    let r;
    try { r = await api(`/shares/${sid}/search?q=${encodeURIComponent(text)}&path=${encodeURIComponent(scope)}&limit=300&sort=${ssort}`, { signal: ctl.signal }); }
    catch (e) { if (e.name === "AbortError") return; if (e.code === "not_open") { route(); return; } fail(e); return; }
    if (!r || mode !== "search" || q.value.trim() !== text) return;
    boxes = [];
    listing.classList.remove("hidden"); grid.classList.add("hidden");
    const rows = [...r.folders.map((d) => folderRow(d, true)), ...r.files.filter((f) => !(hideHave && f.local === "ok")).map((f) => fileRow(f, true))];
    if (!rows.length) rows.push(h("tr", {}, h("td", { colspan: 5 }, h("div", { class: "empty" }, `Nothing matches “${text}”${scope ? " in " + leaf : ""}.`))));
    tbody.replaceChildren(...rows);
    const n = (x, w) => `${x.toLocaleString()} ${w}${x === 1 ? "" : "s"}`;
    put(foot, h("span", {}, `${n(r.file_matches, "file")} and ${n(r.folder_matches, "folder")} match${scope ? " in " + leaf : ""}`),
      r.files.length < r.file_matches || r.folders.length < r.folder_matches ? h("span", { class: "muted" }, ssort === "name" ? "showing the first ones by path: add words to narrow it down" : "showing the largest / smallest of the matches") : null,
      scope ? h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); scopeAll = true; q.placeholder = "Search this share…"; runSearch(); } }, "search the whole share") : null,
      h("span", { class: "faint" }, `${r.ms} ms`));
    refreshSel();
  }

  renderCards(); refreshSel(); await load();
  if (s.checking) runCheck(0).catch(() => {});
}

// ---------------------------------------------------------------- activity
function addLinksDialog() {
  const text = h("textarea", { class: "input", rows: 6, placeholder: "One or more usn:// links" });
  const name = h("input", { class: "input", placeholder: "Job name (optional, single link only)" });
  modal({ title: "Add links", body: [text, name],
    actions: [{ label: "Cancel" }, { label: "Queue", kind: "primary", run: async () => {
      const r = await api("/jobs", { body: { text: text.value, name: name.value } });
      toast(`Queued ${r.jobs.length}` + (r.errors.length ? ` · ${r.errors.length} rejected` : ""), r.errors.length ? "" : "ok");
    } }] });
}

// pause / resume: from the Activity page, the sidebar and the tray
const throttleText = (th) => !th ? "" : th.paused ? th.reason.charAt(0).toUpperCase() + th.reason.slice(1)
  : th.limit ? `Limited to ${fmtRate(th.limit)}` + (th.reason.startsWith("schedule") ? " by the schedule" : "") : "";
function pausePrompt() {
  modal({ title: "Pause downloads", body: h("div", { class: "muted" }, "Running downloads stop and wait in the queue. They carry on from where they were when downloads resume."),
    actions: [{ label: "30 minutes", run: () => pauseFor(30) }, { label: "1 hour", run: () => pauseFor(60) }, { label: "3 hours", run: () => pauseFor(180) },
      { label: "Until I resume", kind: "primary", run: () => pauseFor(0) }] });
  return Promise.resolve();
}
async function pauseFor(min) {
  await api("/queue/pause", { body: { minutes: min } });
  toast(min ? `Paused for ${min >= 60 ? min / 60 + " h" : min + " min"}` : "Paused until you resume", "ok");
  pollStatus(); if (page.onThrottle) page.onThrottle();
}
async function resumeAll() {
  await api("/queue/resume", { body: {} });
  toast("Downloads resumed", "ok");
  pollStatus(); if (page.onThrottle) page.onThrottle();
}

async function pageActivity() {
  setTitle("Activity");
  const pauseBtn = h("button", { class: "btn" }, icon("pause"), "Pause");
  toolbar(pauseBtn, h("button", { class: "btn primary", onclick: addLinksDialog }, icon("plus"), "Add links"));
  const banner = h("div", { class: "hidden" });
  const qbox = h("div", { class: "panel" }), hbox = h("div", { class: "tablewrap" });
  $("#view").append(h("div", { class: "stack" }, banner, h("div", {}, h("h2", { style: { fontSize: "15px", margin: "0 0 10px" } }, "Queue"), qbox),
    h("div", {}, h("h2", { style: { fontSize: "15px", margin: "6px 0 10px" } }, "History"), hbox)));

  function renderThrottle(th) {
    th = th || {};
    put(pauseBtn, th.paused ? [icon("play"), "Resume"] : [icon("pause"), "Pause"]);
    pauseBtn.onclick = () => (th.paused ? resumeAll() : pausePrompt()).catch(fail);
    banner.classList.toggle("hidden", !th.paused && !th.limit);
    put(banner, th.paused
      ? h("div", { class: "note warn bar-note" }, icon("pause"), h("span", { class: "grow" }, `${throttleText(th)}. Downloads wait in the queue and carry on when resumed.`),
        h("button", { class: "btn sm", onclick: () => resumeAll().catch(fail) }, icon("play"), "Resume now"))
      : th.limit ? h("div", { class: "note bar-note" }, icon("gauge"), h("span", { class: "grow" }, `${throttleText(th)}.`), h("a", { href: "#/settings" }, "Speed settings")) : null);
  }

  const act = (fn) => async () => { try { await fn(); refresh(); } catch (e) { fail(e); } };
  let dragging = null;
  const commitOrder = () => api("/jobs/reorder", { body: { ids: [...qbox.querySelectorAll(".job")].map((e) => e.dataset.id) } }).then(refresh).catch(fail);
  async function refresh() {
    if (dragging) return;   // a re-render would drop the row being dragged
    let d; try { d = await api("/jobs"); } catch { return; }
    if (dragging) return;
    renderThrottle(d.throttle);
    const ids = d.queue.map((x) => x.job_id);
    put(qbox, ...(d.queue.length ? d.queue.map((j, idx) => {
      const p = pct(j.bytes, j.size);
      const fp = j.file_pos && j.file_pos[1] > 1 ? `file ${j.file_pos[0]}/${j.file_pos[1]}` : "";
      const row = h("div", { class: "job", draggable: "true", "data-id": j.job_id,
        ondragstart: (ev) => { dragging = row; ev.dataTransfer.effectAllowed = "move"; ev.dataTransfer.setData("text/plain", j.job_id); setTimeout(() => row.classList.add("dragging"), 0); },
        ondragend: () => { row.classList.remove("dragging"); if (dragging) { dragging = null; commitOrder(); } },
        ondragover: (ev) => {
          if (!dragging || dragging === row) return;
          ev.preventDefault();
          const r = row.getBoundingClientRect();
          qbox.insertBefore(dragging, ev.clientY > r.top + r.height / 2 ? row.nextSibling : row);
        },
        ondrop: (ev) => ev.preventDefault() },
        h("div", { class: "l1" }, h("span", { class: "handle", title: "Drag to change the order" }, icon("drag")), stateChip(j.state),
          h("span", { class: "nm", title: j.name }, j.name), h("span", { class: "chip plain" }, j.category),
          h("span", { class: "grow" }),
          idx > 0 ? h("button", { class: "btn sm ghost icon", title: "Move to the top of the queue", onclick: act(() => api("/jobs/reorder", { body: { ids: [j.job_id, ...ids.filter((x) => x !== j.job_id)] } })) }, icon("up")) : null,
          h("button", { class: "btn sm", onclick: act(() => api(`/jobs/${j.job_id}/stop`, { body: {} })) }, icon("stop"), "Stop"),
          h("button", { class: "btn sm ghost icon", title: "Delete", onclick: act(async () => { if (await confirmBox("Delete job", `Delete “${j.name}”? Downloaded files stay on disk.`, "Delete", "danger solid")) await api(`/jobs/${j.job_id}`, { method: "DELETE" }); }) }, icon("trash"))),
        h("div", { class: "bar" + (p >= 100 ? " done" : "") }, h("i", { style: { width: p + "%" } })),
        h("div", { class: "l3" }, h("span", {}, h("b", {}, p.toFixed(1) + "%"), ` · ${fmtBytes(j.bytes)} of ${fmtBytes(j.size)}`),
          h("span", {}, h("b", {}, fmtRate(j.rate))), h("span", {}, "ETA ", h("b", {}, fmtEta(j.eta))),
          j.phase && j.phase !== j.state ? h("span", {}, j.phase) : null, fp ? h("span", {}, fp) : null,
          j.current ? h("span", { class: "faint", title: j.current }, j.current) : null,
          j.missing ? h("span", { class: "chip warn" }, `${j.missing} missing`) : null, j.detail ? h("span", { class: "faint" }, j.detail) : null));
      return row;
    }) : [h("div", { class: "empty" }, icon("activity"), h("h3", {}, "Nothing downloading"), h("div", {}, "Queue shares from the catalog, a share page, or Add links."))]));
    put(hbox, d.history.length ? h("table", { class: "t" },
      h("thead", {}, h("tr", {}, h("th", {}, "Name"), h("th", {}, "State"), h("th", {}, "Detail"), h("th", { class: "right" }, "Size"), h("th", {}, "Finished"), h("th", { class: "act" }, ""))),
      h("tbody", {}, d.history.map((j) => h("tr", {},
        h("td", { class: "title-cell" }, h("div", { class: "t1" }, j.name), h("div", { class: "t2" }, h("span", { class: "chip plain" }, j.category),
          j.out_dir ? h("span", { class: "faint mono", title: j.out_dir }, j.out_dir) : null)),
        h("td", { class: "nowrap" }, stateChip(j.state), j.warnings && j.warnings.length ? [" ", h("span", { class: "chip warn", title: j.warnings.join("\n") }, `${j.warnings.length} warning${j.warnings.length === 1 ? "" : "s"}`)] : null),
        h("td", { class: "muted", style: { maxWidth: "380px" } }, j.detail || ""),
        h("td", { class: "right nowrap" }, fmtBytes(j.size)), h("td", { class: "nowrap muted" }, ago(j.finished)),
        h("td", { class: "act" },
          j.state !== "completed" ? h("button", { class: "btn sm", onclick: act(() => api(`/jobs/${j.job_id}/resume`, { body: {} })) }, icon("play"), j.state === "failed" ? "Retry" : "Resume") : null,
          " ", h("button", { class: "btn sm ghost icon", title: "Remove from history (files stay)", onclick: act(() => api(`/jobs/${j.job_id}`, { method: "DELETE" })) }, icon("x")),
          h("button", { class: "btn sm ghost icon danger", title: "Delete job and its files", onclick: act(async () => {
            if (await confirmBox("Delete files", `Delete “${j.name}” and its folder${j.out_dir ? " " + j.out_dir : ""}?`, "Delete files", "danger solid")) await api(`/jobs/${j.job_id}?files=1`, { method: "DELETE" });
          }) }, icon("trash"))))))) : h("div", { class: "empty" }, "No finished jobs yet"));
  }
  page.onThrottle = refresh;
  every(1500, () => { if (!document.hidden && !$(".modal-back")) refresh(); });
}

// ---------------------------------------------------------------- Usenet providers (Settings and setup)
const PRIORITY_HELP = "0 is the highest. Providers with the same priority download together, each up to its own connection count. A provider with a lower priority (a higher number) is only asked for articles the ones above it don't have: use that for a block account.";
const sw = (checked) => { const i = h("input", { type: "checkbox", checked }); return [i, h("label", { class: "switch" }, i, h("span"))]; };
const field = (label, input, help) => [h("label", { class: "k" }, label), input, help ? h("div", { class: "help" }, help) : null];

// providerDialog edits one provider (a = null: a new one); onSave(provider, isNew) stores it.
function providerDialog(a, onSave) {
  const isNew = !a;
  a = a || { name: "", server: "", port: 563, ssl: true, username: "", connections: 20, enabled: true, priority: 0 };
  const f = { name: h("input", { class: "input", value: a.name || "", placeholder: "optional, e.g. the provider's name" }),
    server: h("input", { class: "input", value: a.server, placeholder: "news.example.com", autocomplete: "off" }),
    port: h("input", { class: "input", type: "number", value: a.port, style: { width: "110px" } }),
    username: h("input", { class: "input", value: a.username || "", autocomplete: "off" }),
    password: h("input", { class: "input", type: "password", placeholder: a.has_password ? "unchanged" : "", autocomplete: "new-password" }),
    connections: h("input", { class: "input", type: "number", min: 1, max: 200, value: a.connections, style: { width: "110px" } }),
    priority: h("input", { class: "input", type: "number", min: 0, max: 99, value: a.priority ?? 0, style: { width: "110px" } }) };
  const [ssl, sslSw] = sw(a.ssl), [en, enSw] = sw(a.enabled);
  ssl.addEventListener("change", () => { if (+f.port.value === 563 || +f.port.value === 119) f.port.value = ssl.checked ? 563 : 119; });
  const result = h("div", { class: "muted" });
  const collect = () => ({ ...a, name: f.name.value.trim(), server: f.server.value.trim(), port: +f.port.value || 563, username: f.username.value.trim(),
    password: f.password.value, connections: +f.connections.value || 1, ssl: ssl.checked, enabled: en.checked, priority: Math.max(0, +f.priority.value || 0) });
  modal({ title: isNew ? "Add a Usenet provider" : `Edit ${a.name || a.server}`, body: h("div", { class: "form" },
    field("Server", f.server), field("Port", f.port), field("SSL", sslSw, "Encrypted connection; most providers use port 563 for it."),
    field("Username", f.username), field("Password", f.password), field("Connections", f.connections, "As many as your plan allows; keep the total across providers within each plan."),
    field("Priority", f.priority, PRIORITY_HELP), field("Name", f.name), field("Enabled", enSw), h("div", { class: "full" }, result)),
  actions: [{ label: "Test connection", run: async () => {
    result.textContent = "connecting…"; result.className = "muted";
    const r = await api("/settings/test-account", { body: collect() }).catch((e) => ({ ok: false, message: e.message }));
    result.textContent = r.message; result.className = r.ok ? "note ok-note" : "note bad"; return false;
  } }, { label: "Cancel" }, { label: isNew ? "Add" : "Save", kind: "primary", run: async () => {
    const p = collect();
    if (!p.server) { result.textContent = "Enter the server name."; result.className = "note bad"; return false; }
    await onSave(p, isNew);
  } }] });
}

const prioChip = (a, list) => {
  const top = Math.min(...list.filter((x) => x.enabled).map((x) => x.priority || 0));
  const p = a.priority || 0;
  return h("span", { class: "chip " + (a.enabled && p === top ? "info" : ""), title: p === top ? "Downloads with the other providers of this priority" : "Asked only for articles the providers above it lack" },
    `${p}`, p === top ? " · main" : " · fill-in");
};

// providersTable lists providers with test / edit / remove; status holds test results by id.
function providersTable(list, { onEdit, onRemove, onToggle, status = {} }) {
  const sorted = [...list].sort((x, y) => (x.priority || 0) - (y.priority || 0));
  return h("div", { class: "tablewrap" }, h("table", { class: "t" },
    h("thead", {}, h("tr", {}, h("th", {}, "Provider"), h("th", {}, "User"), h("th", { class: "right" }, "Conns"), h("th", {}, "Priority"), h("th", {}, "Enabled"), h("th", { class: "act" }, ""))),
    h("tbody", {}, sorted.map((a) => {
      const [en, enSw] = sw(a.enabled);
      en.onchange = () => onToggle(a, en.checked);
      const st = status[a.id];
      return h("tr", {},
        h("td", {}, h("div", { class: "t1", style: { fontWeight: 600 } }, a.name || a.server),
          h("div", { class: "faint mono" }, `${a.server}:${a.port}${a.ssl ? " · SSL" : ""}`),
          st ? h("div", { class: st.ok ? "test ok" : st.ok === false ? "test bad" : "test" }, st.message) : null),
        h("td", {}, a.username || h("span", { class: "faint" }, "—")), h("td", { class: "right" }, a.connections),
        h("td", {}, prioChip(a, list)), h("td", {}, enSw),
        h("td", { class: "act" }, h("button", { class: "btn sm", onclick: async () => {
          status[a.id] = { message: "connecting…" }; onToggle(a, a.enabled, true);
          const r = await api("/settings/test-account", { body: a }).catch((e) => ({ ok: false, message: e.message }));
          status[a.id] = r; onToggle(a, a.enabled, true);
        } }, "Test"), " ",
        h("button", { class: "btn sm ghost icon", title: "Edit", onclick: () => onEdit(a) }, icon("edit")),
        h("button", { class: "btn sm ghost icon danger", title: "Remove", onclick: () => onRemove(a) }, icon("trash"))));
    }))));
}

function providersSummary(list) {
  const on = list.filter((a) => a.enabled);
  if (!on.length) return "";
  const top = Math.min(...on.map((a) => a.priority || 0));
  const main = on.filter((a) => (a.priority || 0) === top), fill = on.filter((a) => (a.priority || 0) !== top);
  return `Downloads use ${main.reduce((n, a) => n + a.connections, 0)} connections across ${main.length} provider${main.length === 1 ? "" : "s"} at priority ${top}` +
    (fill.length ? `; ${fill.length} lower-priority provider${fill.length === 1 ? " is" : "s are"} asked only for articles those lack.` : ".");
}

function sabImportDialog(path, after) {
  const p = h("input", { class: "input", value: path || "", placeholder: "path to sabnzbd.ini" });
  modal({ title: "Import providers from SABnzbd", body: [h("div", { class: "muted" }, "Reads the news servers from SABnzbd's settings file, with their passwords, connections and priorities, and adds the ones not here yet."), p],
    actions: [{ label: "Cancel" }, { label: "Import", kind: "primary", run: async () => {
      const r = await api("/settings/import-sab", { body: { path: p.value } });
      toast(`${r.found} server${r.found === 1 ? "" : "s"} found, ${r.added} added`, "ok"); after(r.settings);
    } }] });
}

// how shares get into usnfetch: the setup's last step and the empty catalog
let pendingCatalogAction = null;   // "import" | "link": opened by the catalog once it is shown
function addSharesHelp(compact) {
  const item = (ic, title, text, btn) => h("div", { class: "howto" }, h("div", { class: "hi" }, icon(ic)),
    h("div", {}, h("div", { class: "ht" }, title), h("div", { class: "muted" }, text), btn || null));
  return h("div", { class: "howtos" + (compact ? " compact" : "") },
    item("upload", "Import a link list", "Catalog → Import list. Choose a file or paste text: any text with usn:// links works, such as a plain list, a CSV, or a web page you saved. Exported lists also give each share its title, listed size and source.",
      h("button", { class: "btn primary sm", onclick: () => { pendingCatalogAction = "import"; location.hash = "#/catalog"; if (compact) route(); } }, icon("upload"), "Import a list")),
    item("link", "Open one link", "Catalog → Open link. Paste a usn:// link to browse that share, check that it is complete, and pick the files you want.",
      h("button", { class: "btn sm", onclick: () => { pendingCatalogAction = "link"; location.hash = "#/catalog"; if (compact) route(); } }, icon("link"), "Open a link")),
    item("download", "Download links straight away", "Activity → Add links queues one or more links in full, without browsing them first."));
}

// ---------------------------------------------------------------- first-run setup
const SETUP_STEPS = [["welcome", "Welcome"], ["providers", "Usenet provider"], ["folders", "Folders"], ["done", "Add shares"]];
async function pageSetup(parts) {
  setTitle("Set up usnfetch");
  let info = await api("/setup");
  const step = SETUP_STEPS.some(([id]) => id === parts[0]) ? parts[0] : "welcome";
  const at = SETUP_STEPS.findIndex(([id]) => id === step);
  const go = (id) => { location.hash = "#/setup/" + id; };
  const later = async () => { await api("/setup/skip", { body: {} }); location.hash = "#/catalog"; };
  const card = h("div", { class: "panel setup" });
  $("#view").append(h("div", { class: "setup-wrap" },
    h("ol", { class: "steps" }, SETUP_STEPS.map(([id, label], i) => h("li", { class: i === at ? "on" : i < at ? "done" : "" }, h("span", { class: "n" }, i < at ? icon("check") : i + 1), label))),
    card));
  const foot = (...btns) => h("div", { class: "setup-foot" }, ...btns);

  if (step === "welcome") {
    put(card, h("div", { class: "body" }, h("h2", {}, "Welcome to usnfetch"),
      h("p", { class: "muted" }, "usnfetch downloads usn:// shares straight from your own Usenet provider, decrypting and checking every file as it lands. Setting it up takes a minute:"),
      h("ol", { class: "plan" }, h("li", {}, h("b", {}, "Your Usenet provider"), " — server, username and password, or import them from SABnzbd."),
        h("li", {}, h("b", {}, "Folders"), " — where downloads go."), h("li", {}, h("b", {}, "Adding shares"), " — how to bring in the links you have.")),
      foot(h("button", { class: "btn ghost", onclick: () => later().catch(fail) }, "Set up later"), h("span", { class: "grow" }),
        h("button", { class: "btn primary", onclick: () => go("providers") }, "Start"))));
    return;
  }

  if (step === "providers") {
    const status = {};
    const listBox = h("div");
    const next = h("button", { class: "btn primary", onclick: () => go("folders") }, "Next");
    const save = async (accounts) => { const sv = await api("/settings", { method: "PUT", body: { accounts } }); info.accounts = sv.accounts; render(); return sv.accounts; };
    const testNew = async (a) => {
      status[a.id] = { message: "connecting…" }; render();
      status[a.id] = await api("/settings/test-account", { body: a }).catch((e) => ({ ok: false, message: e.message })); render();
    };
    function render() {
      const list = info.accounts;
      next.disabled = !list.some((a) => a.enabled);
      put(listBox, list.length
        ? [providersTable(list, {
            status,
            onEdit: (a) => providerDialog(a, async (p) => { await save(list.map((x) => (x.id === a.id ? p : x))); testNew(info.accounts.find((x) => x.id === a.id)); }),
            onRemove: async (a) => { if (await confirmBox("Remove provider", `Remove ${a.name || a.server}?`, "Remove", "danger solid")) save(list.filter((x) => x.id !== a.id)).catch(fail); },
            onToggle: (a, on, redraw) => { if (redraw) return render(); save(list.map((x) => (x.id === a.id ? { ...x, enabled: on } : x))).catch(fail); } }),
          h("div", { class: "muted small", style: { marginTop: "8px" } }, providersSummary(list))]
        : h("div", { class: "empty slim" }, icon("server"), h("div", {}, "No provider yet.")));
    }
    render();
    put(card, h("div", { class: "body" }, h("h2", {}, "Your Usenet provider"),
      h("p", { class: "muted" }, "usnfetch downloads from your own Usenet account, the way SABnzbd does. Add the provider you use; add more if you have them, for example a block account at a lower priority."),
      info.sab_ini && !info.accounts.length ? h("div", { class: "note callout" }, icon("server"),
        h("span", { class: "grow" }, "SABnzbd is set up on this computer. Import its servers, with their passwords and priorities?"),
        h("button", { class: "btn primary sm", onclick: async () => {
          try { const r = await api("/settings/import-sab", { body: { path: info.sab_ini } }); info.accounts = r.settings.accounts; toast(`${r.added} provider${r.added === 1 ? "" : "s"} imported`, "ok"); route(); for (const a of info.accounts) testNew(a); } catch (e) { fail(e); }
        } }, "Import from SABnzbd")) : null,
      listBox,
      h("div", { class: "row-btns" },
        h("button", { class: "btn", onclick: () => providerDialog(null, async (p) => { const before = new Set(info.accounts.map((a) => a.id)); const all = await save([...info.accounts, p]); const added = all.find((a) => !before.has(a.id)); if (added) testNew(added); }) }, icon("plus"), "Add a provider"),
        h("button", { class: "btn ghost", onclick: () => sabImportDialog(info.sab_ini, (sv) => { info.accounts = sv.accounts; render(); }) }, "Import from SABnzbd…")),
      h("div", { class: "help" }, h("b", {}, "Priority: "), PRIORITY_HELP),
      foot(h("button", { class: "btn ghost", onclick: () => go("welcome") }, icon("back"), "Back"), h("button", { class: "btn ghost", onclick: () => later().catch(fail) }, "Set up later"),
        h("span", { class: "grow" }), next)));
    return;
  }

  if (step === "folders") {
    const dl = h("input", { class: "input", value: info.complete_dir || "" });
    const ix = h("input", { class: "input", value: info.index_dir || "", placeholder: info.index_dir_default });
    const dlState = h("div", { class: "fstate" }), ixState = h("div", { class: "fstate" });
    const finish = h("button", { class: "btn primary" }, "Finish setup");
    const check = async (input, out, optional) => {
      const v = input.value.trim();
      if (!v && optional) { out.className = "fstate"; out.textContent = "Kept in the data folder: " + info.index_dir_default; return true; }
      const r = await api("/setup/folder", { body: { path: v } }).catch((e) => ({ ok: false, error: e.message }));
      out.className = "fstate " + (r.ok ? "ok" : "bad");
      out.textContent = r.ok ? `${r.exists ? "Folder exists" : "Will be created"}${r.free != null ? " · " + fmtBytes(r.free) + " free" : ""}` : r.error;
      return r.ok;
    };
    let t1 = null, t2 = null;
    dl.oninput = () => { clearTimeout(t1); t1 = setTimeout(() => check(dl, dlState), 350); };
    ix.oninput = () => { clearTimeout(t2); t2 = setTimeout(() => check(ix, ixState, true), 350); };
    check(dl, dlState); check(ix, ixState, true);
    finish.onclick = async () => {
      finish.disabled = true;
      try {
        if (!(await check(dl, dlState)) || !(await check(ix, ixState, true))) return;
        await api("/setup/finish", { body: { complete_dir: dl.value.trim(), index_dir: ix.value.trim() } });
        go("done");
      } catch (e) { fail(e); } finally { finish.disabled = false; }
    };
    put(card, h("div", { class: "body" }, h("h2", {}, "Folders"),
      h("div", { class: "form" }, h("label", { class: "k" }, "Download folder"), h("div", {}, dl, dlState),
        h("div", { class: "help" }, "Each download goes into <download folder>\\<category>\\<share name>. Shares can be very large, so pick a drive with room."),
        h("label", { class: "k" }, "Share index folder"), h("div", {}, ix, ixState),
        h("div", { class: "help" }, "Working files, about 200 MB per TB of share, kept while a share is open or its download can resume. Leave it empty to keep them in the data folder.")),
      foot(h("button", { class: "btn ghost", onclick: () => go("providers") }, icon("back"), "Back"), h("span", { class: "grow" }), finish)));
    return;
  }

  // done
  put(card, h("div", { class: "body" }, h("h2", {}, "All set: now add some shares"),
    h("p", { class: "muted" }, "usnfetch doesn't come with any shares. It opens the share links you already have, the same way the official app does:"),
    addSharesHelp(false),
    h("p", { class: "muted small" }, "Providers, folders and everything else can be changed later in Settings."),
    foot(h("span", { class: "grow" }), h("button", { class: "btn primary", onclick: () => { location.hash = "#/catalog"; } }, "Go to the catalog"))));
}

// ---------------------------------------------------------------- settings
async function pageSettings() {
  setTitle("Settings");
  let s = await api("/settings");
  const view = $("#view");


  // Usenet providers
  toolbar(h("a", { class: "btn", href: "#/setup" }, "Run setup again"));
  const accBody = h("div");
  const accStatus = {};
  async function saveAccounts(accounts) { s = await api("/settings", { method: "PUT", body: { accounts } }); renderAccounts(); }
  function renderAccounts() {
    put(accBody, s.accounts.length
      ? [providersTable(s.accounts, { status: accStatus,
          onEdit: (a) => providerDialog(a, (p) => saveAccounts(s.accounts.map((x) => (x.id === a.id ? p : x)))),
          onRemove: async (a) => { if (await confirmBox("Remove provider", `Remove ${a.name || a.server}?`, "Remove", "danger solid")) saveAccounts(s.accounts.filter((x) => x.id !== a.id)).catch(fail); },
          onToggle: (a, on, redraw) => { if (redraw) return renderAccounts(); saveAccounts(s.accounts.map((x) => (x.id === a.id ? { ...x, enabled: on } : x))).catch(fail); } }),
        h("div", { class: "muted", style: { marginTop: "10px", fontSize: "12.5px" } }, providersSummary(s.accounts))]
      : h("div", { class: "note warn" }, "No Usenet provider yet: add one, import them from SABnzbd, or ", h("a", { href: "#/setup/providers" }, "run the setup"), "."));
  }
  const accounts = h("div", { class: "panel" }, h("div", { class: "head" }, icon("server"), h("h2", {}, "Usenet providers"), h("span", { class: "grow" }),
    h("button", { class: "btn", onclick: () => sabImportDialog(s.sab_ini, (sv) => { s = sv; renderAccounts(); }) }, "Import from SABnzbd"),
    h("button", { class: "btn primary", onclick: () => providerDialog(null, (p) => saveAccounts([...s.accounts, p])) }, icon("plus"), "Add provider")),
  h("div", { class: "body" }, accBody, h("div", { class: "help", style: { marginTop: "10px" } }, h("b", {}, "Priority: "), PRIORITY_HELP)));

  // downloads
  const d = { complete_dir: h("input", { class: "input", value: s.complete_dir }), categories: h("input", { class: "input", value: (s.categories || []).join(", ") }),
    missing_retries: h("input", { class: "input", type: "number", min: 0, max: 10, value: s.missing_retries }), max_parallel_jobs: h("input", { class: "input", type: "number", min: 1, max: 8, value: s.max_parallel_jobs }),
    stream_connections: h("input", { class: "input", type: "number", min: 1, max: 64, value: s.stream_connections }),
    index_dir: h("input", { class: "input", value: s.index_dir || "", placeholder: "index_cache next to the app" }),
    sevenzip_path: h("input", { class: "input", value: s.sevenzip_path || "", placeholder: "auto-detect" }), unrar_path: h("input", { class: "input", value: s.unrar_path || "", placeholder: "auto-detect" }) };
  const [verify, verifySw] = sw(s.verify), [extract, extractSw] = sw(s.extract), [delArc, delArcSw] = sw(s.delete_archives), [single, singleSw] = sw(s.extract_standalone);
  const downloads = h("div", { class: "panel" }, h("div", { class: "head" }, icon("download"), h("h2", {}, "Downloads"), h("span", { class: "grow" }),
    h("button", { class: "btn primary", onclick: async () => {
      try {
        s = await api("/settings", { method: "PUT", body: { complete_dir: d.complete_dir.value, categories: d.categories.value, missing_retries: +d.missing_retries.value,
          max_parallel_jobs: +d.max_parallel_jobs.value, stream_connections: +d.stream_connections.value, index_dir: d.index_dir.value, sevenzip_path: d.sevenzip_path.value,
          unrar_path: d.unrar_path.value, verify: verify.checked, extract: extract.checked, extract_standalone: single.checked, delete_archives: delArc.checked } });
        toast("Saved", "ok");
      } catch (e) { fail(e); }
    } }, "Save")),
  h("div", { class: "body" }, h("div", { class: "form" },
    field("Download folder", d.complete_dir, "Jobs land in <folder>/<category>/<job name>."), field("Categories", d.categories, "Comma separated."),
    field("Verify SHA-256", verifySw, "Hash each file against the share's index when it completes."),
    field("Extract archives", extractSw, "RAR archives and multi-part sets (.z01, .zip.001, .7z.001). Each download can choose otherwise when you queue it."),
    field("Also single .zip / .7z", singleSw, "Off keeps them zipped: ROM, BIOS and asset packs are meant to stay that way."),
    field("Delete archives after", delArcSw, "Only for sets that extracted cleanly."),
    field("Missing-article retries", d.missing_retries, "Retry passes (1, 2, 3… minutes apart) for articles missing on every account."),
    field("Parallel jobs", d.max_parallel_jobs), field("Streaming connections", d.stream_connections, "Used when opening files straight from Usenet in the browser, on top of download connections."),
    field("Share index folder", d.index_dir, "About 200 MB per TB of share while a share is open or a job can resume."),
    field("7-Zip", d.sevenzip_path), field("UnRAR", d.unrar_path))));

  // web access
  const cur = h("input", { class: "input", type: "password", autocomplete: "current-password", placeholder: "Current password" });
  const nw = h("input", { class: "input", type: "password", autocomplete: "new-password", placeholder: "New password (8+ characters)" });
  const nw2 = h("input", { class: "input", type: "password", autocomplete: "new-password", placeholder: "New password again" });
  const web = h("div", { class: "panel" }, h("div", { class: "head" }, icon("shield"), h("h2", {}, "Web access"), h("span", { class: "grow" }),
    s.web.has_password ? h("span", { class: "chip ok" }, "password set") : h("span", { class: "chip warn" }, "no password")),
  h("div", { class: "body" }, h("div", { class: "form" },
    h("div", { class: "full note" }, s.web.has_password ? "Other computers can log in with this password." : "Without a password only this machine can use the web app. Set one to reach it from other computers — start it with ", h("span", { class: "mono" }, "usnfetch serve --host 0.0.0.0"), s.web.has_password ? "" : " and open http://<this machine>:" + (s.web.port || 7979) + "/ there."),
    s.web.has_password ? field("Current password", cur) : null, field("New password", nw), field("Confirm", nw2),
    h("div", {}), h("div", {}, h("button", { class: "btn primary", onclick: async () => {
      if (nw.value !== nw2.value) return toast("The new passwords differ", "bad");
      try { await api("/settings/web-password", { body: { current: cur.value, new: nw.value } }); toast(nw.value ? "Password set" : "Password removed", "ok"); route(); } catch (e) { fail(e); }
    } }, s.web.has_password ? "Change password" : "Set password")))));

  // speed limit and schedule
  const limit = h("input", { class: "input", type: "number", min: 0, step: "0.5", value: s.speed_limit_mbps || "", placeholder: "no limit", style: { width: "140px" } });
  let rules = (s.schedule || []).map((r) => ({ ...r, days: [...(r.days || [])] }));
  const rulesBox = h("div", { class: "rules" });
  const DAYS = [[1, "Mo"], [2, "Tu"], [3, "We"], [4, "Th"], [5, "Fr"], [6, "Sa"], [0, "Su"]];
  function ruleRow(r, i) {
    const [en, enSw] = sw(r.enabled); en.onchange = () => { r.enabled = en.checked; };
    const from = h("input", { class: "input", type: "time", value: r.from, onchange: () => { r.from = from.value; } });
    const to = h("input", { class: "input", type: "time", value: r.to, onchange: () => { r.to = to.value; } });
    const action = h("select", { class: "input", onchange: () => { r.action = action.value; renderRules(); } },
      h("option", { value: "limit", selected: r.action === "limit" }, "Limit to"), h("option", { value: "pause", selected: r.action === "pause" }, "Pause"));
    const mb = h("input", { class: "input", type: "number", min: "0.5", step: "0.5", value: r.limit_mbps || 10, style: { width: "84px" }, onchange: () => { r.limit_mbps = +mb.value; } });
    return h("div", { class: "rule" }, enSw,
      h("div", { class: "days", title: "No day picked = every day" }, DAYS.map(([d, n]) => h("button", { class: "day" + (r.days.includes(d) ? " on" : ""),
        onclick: () => { r.days = r.days.includes(d) ? r.days.filter((x) => x !== d) : [...r.days, d]; renderRules(); } }, n))),
      from, h("span", { class: "muted" }, "to"), to, action, r.action === "limit" ? [mb, h("span", { class: "muted" }, "MB/s")] : null,
      h("span", { class: "grow" }),
      h("button", { class: "btn ghost icon danger", title: "Remove this rule", onclick: () => { rules.splice(i, 1); renderRules(); } }, icon("trash")));
  }
  function renderRules() {
    put(rulesBox, rules.length ? rules.map(ruleRow) : h("div", { class: "faint" }, "No schedule: downloads use the limit above all the time."),
      h("div", {}, h("button", { class: "btn sm", onclick: () => { rules.push({ enabled: true, days: [1, 2, 3, 4, 5], from: "08:00", to: "18:00", action: "limit", limit_mbps: 10 }); renderRules(); } }, icon("plus"), "Add a rule")));
  }
  renderRules();
  const speed = h("div", { class: "panel" }, h("div", { class: "head" }, icon("gauge"), h("h2", {}, "Speed & schedule"), h("span", { class: "grow" }),
    h("button", { class: "btn primary", onclick: async () => {
      try {
        s = await api("/settings", { method: "PUT", body: { speed_limit_mbps: +limit.value || 0, schedule: rules } });
        rules = (s.schedule || []).map((r) => ({ ...r, days: [...(r.days || [])] })); renderRules();
        toast("Saved", "ok"); pollStatus();
      } catch (e) { fail(e); }
    } }, "Save")),
  h("div", { class: "body" }, h("div", { class: "form" },
    h("label", { class: "k" }, "Speed limit"), h("div", { class: "inline" }, limit, h("span", { class: "muted" }, "MB/s for downloads (empty = as fast as the connections go)")),
    h("div", { class: "help" }, "Opening files in the browser is not limited."),
    h("label", { class: "k" }, "Schedule"), h("div", {}, h("div", { class: "muted", style: { fontSize: "12.5px", marginBottom: "8px" } }, "The first rule that covers the current time wins. A rule from 23:00 to 07:00 runs past midnight."), rulesBox))));
  // appearance: saved in this browser, applied at once
  const appearance = h("div", { class: "panel" });
  function renderAppearance() {
    const group = (key, label, options) => [h("label", { class: "k" }, label), h("div", { class: "tiles" }, options.map(([v, name, pv]) =>
      h("button", { class: "tile" + (UI[key] === v ? " on" : ""), type: "button", "aria-pressed": UI[key] === v ? "true" : "false",
        onclick: () => { setUI(key, v); renderAppearance(); } }, pv, h("span", {}, name))))];
    const mini = (cls) => h("div", { class: "pv " + cls }, h("i", { class: "n" }), h("i", { class: "m" }, h("b"), h("b"), h("b")));
    put(appearance, h("div", { class: "head" }, icon("grid"), h("h2", {}, "Appearance"), h("span", { class: "grow" }), h("span", { class: "faint", style: { fontSize: "12.5px" } }, "Saved in this browser")),
      h("div", { class: "body" }, h("div", { class: "form" },
        ...group("layout", "Layout", [["side", "Menu on the left", mini("side")], ["top", "Menu across the top", mini("top")], ["rail", "Icon rail", mini("rail")]]),
        ...group("theme", "Theme", [["dark", "Dark", h("div", { class: "pv sw dark" })], ["light", "Light", h("div", { class: "pv sw light" })], ["system", "Match the system", h("div", { class: "pv sw system" })]]),
        ...group("density", "Density", [["comfortable", "Comfortable", h("div", { class: "pv lines" }, h("b"), h("b"), h("b"))], ["compact", "Compact", h("div", { class: "pv lines tight" }, h("b"), h("b"), h("b"), h("b"), h("b"))]]))));
  }
  renderAppearance();
  view.append(h("div", { class: "stack" }, appearance, accounts, downloads, speed, web));
  renderAccounts();
}

// ---------------------------------------------------------------- system
async function pageSystem() {
  setTitle("System");
  const cards = h("div", { class: "cards" });
  const logs = h("div", { class: "loglines tall" });
  const follow = h("input", { type: "checkbox", checked: true });
  toolbar(h("label", { class: "check" }, follow, "Follow log"));
  $("#view").append(cards, h("div", { class: "panel" }, h("div", { class: "head" }, icon("system"), h("h2", {}, "Log")), h("div", { class: "body" }, logs)));
  let seq = 0;
  every(2000, async () => {
    try {
      const st = await api("/status");
      put(cards, 
        h("div", { class: "card" }, h("div", { class: "k" }, "Version"), h("div", { class: "v" }, st.version), h("div", { class: "s" }, st.runtime || ("Python " + st.python))),
        h("div", { class: "card" }, h("div", { class: "k" }, "Free space"), h("div", { class: "v" }, fmtBytes(st.disk.free)), h("div", { class: "s", title: st.disk.path }, st.disk.path)),
        h("div", { class: "card" }, h("div", { class: "k" }, "Jobs"), h("div", { class: "v" }, `${st.running} running`), h("div", { class: "s" }, `${st.active} active · ${st.open_shares} shares open`)),
        h("div", { class: "card wide" }, h("div", { class: "k" }, "Accounts"), h("div", { class: "v", style: { fontSize: "14px", whiteSpace: "normal" } }, st.accounts || "none configured"), h("div", { class: "s", title: st.data_dir }, "data: " + st.data_dir)));
      const l = await api("/logs?after=" + seq);
      if (l.lines.length) {
        logs.append(document.createTextNode((logs.textContent ? "\n" : "") + l.lines.join("\n")));
        if (logs.textContent.length > 400000) logs.textContent = logs.textContent.slice(-300000);
        if (follow.checked) logs.scrollTop = logs.scrollHeight;
      }
      seq = l.seq;
    } catch { /* ignore */ }
  });
}

// ---------------------------------------------------------------- boot
async function boot() {
  let sess;
  try { sess = await (await fetch("/api/session", { credentials: "same-origin" })).json(); } catch { sess = {}; }
  if (sess.remote_blocked) {
    buildNav("");
    setTitle("Web password needed");
    $("#view").replaceChildren(h("div", { class: "center" }, h("div", { class: "panel login" }, h("h2", {}, "Set a password first"),
      h("div", { class: "muted" }, "This usnfetch has no web password, so it only accepts the machine it runs on. Open it there (http://localhost:" + location.port + "/), go to Settings → Web access and set a password, or run ", h("span", { class: "mono" }, "usnfetch set-password"), "."))));
    return;
  }
  // where to start: the login, the first-run setup, or the page in the address bar
  let start = null;
  if (sess.auth_required && !sess.logged_in) { if (!location.hash.startsWith("#/login")) start = "#/login"; }
  else {
    try { const st = await api("/status"); if (st.setup_done === false && !location.hash.startsWith("#/setup")) start = "#/setup"; } catch { /* offline */ }
  }
  window.addEventListener("hashchange", route);
  $("#statusbar").addEventListener("click", () => { location.hash = "#/activity"; });
  (function poll() {   // every 1.5 s while something runs, every 3 s otherwise
    setTimeout(async () => { if (!document.hidden) await pollStatus(); poll(); }, lastStatus && lastStatus.running > 0 ? 1500 : 3000);
  })();
  pollStatus(); renderBasket();
  window.addEventListener("storage", (ev) => { if (ev.key === "usnf.basket") { basket.all = store.get("basket", {}); renderBasket(); if (page.onBasket) page.onBasket(); } });
  if (start) location.hash = start;   // its hashchange routes, once
  else await route();
}
boot();
