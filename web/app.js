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
  save: "M12 4v10m0 0-4-4m4 4 4-4M4 15v4a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-4", more: "M5 12h.01M12 12h.01M19 12h.01", info: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z M12 11v5M12 8h.01",
  copy: "M9 9h10v11H9Z M5 15V4h10",
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

const cap = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);
// toast: a short message in the corner; action { label, run } or { label, href } adds a button
function toast(msg, kind = "", action = null) {
  const t = h("div", { class: "toast " + kind }, h("span", {}, typeof msg === "string" ? cap(msg) : msg),
    action ? h(action.href ? "a" : "button", { class: "btn sm ghost tact", href: action.href || null,
      onclick: () => { t.remove(); if (action.run) action.run(); } }, action.label) : null);
  $("#toasts").append(t); setTimeout(() => t.remove(), action ? 9000 : kind === "bad" ? 7000 : 3800);
}
const fail = (e) => toast(e.message || String(e), "bad");

// modal: a dialog. actions: { label, kind, run, ref } -- run returning false keeps it open,
// ref(button) hands over the button. Enter in a one-line field runs the primary action.
function modal({ title, body, actions = [], wide = false, onclose }) {
  let open = true;
  const close = () => { if (!open) return; open = false; back.remove(); document.removeEventListener("keydown", esc); onclose && onclose(); };
  const esc = (ev) => { if (ev.key === "Escape" && back === $("#modal-root").lastElementChild) close(); };
  const buttons = actions.map((a) => {
    const b = h("button", { class: "btn " + (a.kind || ""), type: "button", onclick: async () => {
      if (!a.run) return close();
      b.disabled = true;
      try { if ((await a.run()) !== false) close(); } catch (e) { fail(e); } finally { b.disabled = false; }
    } }, a.label);
    if (a.ref) a.ref(b);
    return b;
  });
  const back = h("div", { class: "modal-back", onmousedown: (ev) => { if (ev.target === back) close(); } },
    h("div", { class: "modal" + (wide ? " wide" : ""), role: "dialog", "aria-label": title,
      onkeydown: (ev) => {
        const t = ev.target;
        if (ev.key !== "Enter" || ev.isComposing || t.tagName !== "INPUT" || ["checkbox", "radio", "button", "file"].includes(t.type)) return;
        const p = [...buttons].reverse().find((b) => b.classList.contains("primary") && !b.disabled);
        if (p) { ev.preventDefault(); p.click(); }
      } },
      h("div", { class: "mh" }, h("h3", {}, title), h("button", { class: "btn ghost icon", onclick: close, title: "Close" }, icon("x"))),
      h("div", { class: "mb" }, body),
      buttons.length ? h("div", { class: "mf" }, buttons) : null));
  $("#modal-root").append(back); document.addEventListener("keydown", esc);
  const f = back.querySelector(".mb input:not([type=radio]):not([type=checkbox]),.mb textarea,.mb select"); if (f) setTimeout(() => f.focus(), 30);
  return close;
}

// menu: a small list of labelled actions under a button ([label, run, kind] items)
function menu(anchor, items) {
  document.querySelectorAll(".popmenu").forEach((m) => m.remove());
  const r = anchor.getBoundingClientRect();
  const m = h("div", { class: "popmenu", style: { top: `${r.bottom + 4}px`, right: `${Math.max(8, window.innerWidth - r.right)}px` } },
    items.filter(Boolean).map(([label, run, kind]) => h("button", { class: "mi " + (kind || ""), type: "button", onclick: () => { m.remove(); run(); } }, label)));
  document.body.append(m);
  const away = (ev) => { if (!m.contains(ev.target)) { m.remove(); document.removeEventListener("mousedown", away, true); } };
  setTimeout(() => document.addEventListener("mousedown", away, true), 0);
}

// emptyState: the one look for "nothing here yet"
function emptyState(ic, title, text, ...extra) {
  return h("div", { class: "empty" }, ic ? icon(ic) : null, title ? h("h3", {}, title) : null, text ? h("div", { class: "muted" }, text) : null, ...extra);
}

async function copyText(t) {
  try { await navigator.clipboard.writeText(t); toast("Copied", "ok"); }
  catch { modal({ title: "Copy", body: h("input", { class: "input", value: t, readonly: true, onfocus: (ev) => ev.target.select() }) }); }
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

const STATE_CHIP = { queued: "", paused: "warn", planning: "info", downloading: "info", extracting: "info", stopping: "warn",
  completed: "ok", failed: "bad", stopped: "warn" };
const jobLabel = (j) => (j.picked ? `${j.name} — ${j.picked}` : j.name);
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
function setTitle(text, crumb, href) {
  const c = !crumb ? "" : href ? h("a", { class: "crumb", href }, crumb) : h("span", { class: "crumb" }, crumb);
  $("#title").replaceChildren(c, crumb ? h("span", { class: "crumb" }, " / ") : "", text);
}
function toolbar(...els) { put($("#toolbar"), ...els); }

function buildNav(active) {
  $("#nav").replaceChildren(...NAV.map(([id, label, ic]) => h("a", { href: "#/" + id, class: active === id ? "on" : "", title: label },
    icon(ic), label, id === "activity" ? h("span", { class: "badge hidden", id: "nav-badge" }) : null)));
}

let lastStatus = null;
let seenFinished = null;   // the newest finish time already told about (null: not polled yet)
function announceFinished(s) {
  const recent = s.recent || [];
  const newest = recent.reduce((a, j) => Math.max(a, j.finished || 0), 0);
  if (seenFinished == null) { seenFinished = newest; return; }   // first look: old news
  for (const j of recent.filter((x) => x.finished > seenFinished).reverse()) {
    if (j.state === "completed") toast(`Finished: ${jobLabel(j)}`, "ok", { label: "View", href: "#/activity" });
    else if (j.state === "failed") toast(`Failed: ${jobLabel(j)}${j.detail ? " — " + j.detail : ""}`, "bad", { label: "View", href: "#/activity" });
    if (page.onJobDone) page.onJobDone(j);
  }
  seenFinished = Math.max(seenFinished, newest);
}
async function pollStatus() {
  try {
    const s = await api("/status"); lastStatus = s;
    announceFinished(s);
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
  const held = jobs.filter((j) => j.state === "paused").length;   // paused one by one: not "waiting to start"
  const waiting = Math.max(0, (s.active || 0) - running.length - held);
  const p = cur && cur.size ? pct(cur.bytes, cur.size) : 0;
  let main;
  if (th.paused) {
    main = [h("b", {}, throttleText(th)), h("span", { class: "faint" }, s.active ? `${s.active} waiting in the queue` : "nothing waiting")];
  } else if (cur) {
    const checking = cur.state === "planning" && /^(verifying|checking)/.test(cur.current || "");
    const verb = checking ? "Checking" : { planning: "Preparing", downloading: /^verifying/.test(cur.phase || "") ? "Verifying" : "Downloading", extracting: "Extracting", stopping: "Stopping" }[cur.state] || cur.state;
    const fp = cur.file_pos && cur.file_pos[1] > 1 ? `file ${cur.file_pos[0].toLocaleString()} of ${cur.file_pos[1].toLocaleString()}` : "";
    main = [h("span", { class: "sb-verb" }, verb), h("b", { class: "sb-name", title: jobLabel(cur) }, jobLabel(cur)),
      cur.state === "planning" && !cur.current ? h("span", { class: "faint" }, "loading the share index…") : null,
      fp ? h("span", { class: "faint nowrap sb-fp" }, fp) : null,
      cur.current ? h("span", { class: "faint sb-file", title: cur.current }, cur.current) : null,
      running.length > 1 ? h("span", { class: "chip plain" }, `+${running.length - 1} more running`) : null];
  } else {
    main = [h("b", {}, waiting ? `${waiting} queued` : "Idle"), h("span", { class: "faint" }, waiting ? "waiting to start" : "nothing downloading"),
      held ? h("span", { class: "faint" }, `· ${held} paused in Activity`) : null];
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
  document.querySelectorAll(".popmenu").forEach((m) => m.remove());
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
// no provider yet: say so, with the way to add one (instead of a page that can only fail)
function noProviderDialog() {
  modal({ title: "No Usenet provider yet", body: h("div", { class: "muted" }, "usnfetch reads shares from your own Usenet account. Add your provider first: it takes a minute."),
    actions: [{ label: "Not now" }, { label: "Add a provider", kind: "primary", run: () => { location.hash = "#/setup/providers"; } }] });
}
const needsProvider = () => !!(lastStatus && lastStatus.needs_setup);

async function openShare(body) {
  if (needsProvider()) { noProviderDialog(); return; }
  const s = await api("/shares/open", { body });
  if (body.link) store.set("link." + s.id, body.link);
  location.hash = "#/share/" + s.id;
}

// prepareShare opens a share for its download review, with a small progress dialog while
// its index loads; resolves the ready share, or null (cancelled, password, or a problem shown)
async function prepareShare(body, title) {
  if (needsProvider()) { noProviderDialog(); return null; }
  const s0 = await api("/shares/open", { body });
  if (body.link) store.set("link." + s0.id, body.link);
  if (s0.state === "ready") return s0;
  return new Promise((resolve) => {
    let done = false, t = null;
    const lines = h("div", { class: "loglines" });
    const finish = (v) => { if (done) return; done = true; clearInterval(t); close(); resolve(v); };
    const close = modal({ title: "Preparing the review", body: [h("div", { class: "muted" }, h("span", { class: "spin" }), " Loading the index of ", h("b", {}, title || s0.title || "the share"),
      ". The first time takes a few seconds per TB; after that it is cached."), lines], actions: [{ label: "Cancel" }], onclose: () => finish(null) });
    t = setInterval(async () => {
      let s; try { s = await api("/shares/" + s0.id); } catch (e) { fail(e); finish(null); return; }
      lines.textContent = (s.log || []).slice(-6).join("\n");
      if (s.state === "loading") return;
      if (s.state === "ready") return finish(s);
      finish(null);
      if (s.state === "password") location.hash = "#/share/" + s.id;
      else if (s.error_code === "no_provider") noProviderDialog();
      else modal({ title: "Can't open this share", body: h("div", { class: "note bad" }, cap(s.error)), actions: [{ label: "Close", kind: "primary" }] });
    }, 700);
  });
}

// importDialog: paste or pick a list, see what it adds and what it would change, then import
function importDialog(after) {
  const name = h("input", { class: "input", placeholder: "Source name (optional)" });
  const text = h("textarea", { class: "input", rows: 9, placeholder: "Paste text with usn:// links, or pick a file above (a .txt list, CSV, saved web page…)" });
  const file = h("input", { type: "file", accept: ".txt,.csv,.html,.htm,.md,.json,text/*", onchange: async () => {
    const f = file.files[0]; if (!f) return; text.value = await f.text(); if (!name.value) name.value = f.name; preview = null; showPreview(); } });
  const pv = h("div", { class: "hidden" });
  const err = h("div", { class: "fstate bad hidden" });
  let preview = null, overwrite = false, go = null;
  text.addEventListener("input", () => { preview = null; showPreview(); });
  function showPreview() {
    err.classList.add("hidden");
    if (go) go.textContent = preview ? "Import" : "Next";
    pv.classList.toggle("hidden", !preview);
    if (!preview) return;
    const p = preview, ch = p.changes || [];
    const ow = h("input", { type: "checkbox", checked: overwrite, onchange: () => { overwrite = ow.checked; } });
    put(pv, h("div", { class: "note" }, h("b", {}, `${p.found.toLocaleString()} link${p.found === 1 ? "" : "s"}: `),
      `${p.added.toLocaleString()} new`, p.filled ? ` · ${p.filled.toLocaleString()} known ones get missing details filled in` : "",
      ch.length ? ` · ${ch.length.toLocaleString()} known one${ch.length === 1 ? " is" : "s are"} listed differently` : p.added ? "" : " · nothing new"),
      ch.length ? h("div", { class: "changes" }, ch.slice(0, 50).map((c) => h("div", { class: "chg" }, h("b", {}, c.title),
        c.fields.map((f) => h("div", { class: "muted small" }, `${f.field}: `, h("s", {}, f.from), " → ", f.to)))),
        ch.length > 50 ? h("div", { class: "faint small" }, `and ${ch.length - 50} more`) : null) : null,
      ch.length ? h("label", { class: "check" }, ow, `Use the new details for ${ch.length === 1 ? "that entry" : `those ${ch.length} entries`} (otherwise they stay as they are)`) : null);
  }
  modal({ title: "Import a link list", wide: true, body: [h("div", { class: "muted" }, "Any text with usn:// links works. The exported layout (SOURCE / title [size] / SKU / label / link) keeps titles, sizes and sources. Importing a list again adds what's new and never blanks out what the catalog knows."), file, text, name, err, pv],
    actions: [{ label: "Cancel" }, { label: "Next", kind: "primary", ref: (b) => { go = b; }, run: async () => {
      const body = { text: text.value, name: name.value || "import" };
      if (!preview) {
        try { preview = await api("/catalog/import", { body: { ...body, preview: true } }); }
        catch (e) { err.textContent = cap(e.message); err.classList.remove("hidden"); return false; }
        if (preview.changes.length || !preview.added) { showPreview(); return false; }   // something to look at first
      }
      const r = await api("/catalog/import", { body: { ...body, overwrite } });
      const parts = [`${r.added} new`, r.filled ? `${r.filled} filled in` : "", r.changed ? `${r.changed} changed` : "", r.kept ? `${r.kept} kept as they were` : ""].filter(Boolean);
      toast(`Imported ${r.found} link${r.found === 1 ? "" : "s"}: ${parts.join(", ")}`, "ok", r.undo ? { label: "Undo", run: async () => {
        try { await api("/catalog/undo", { body: {} }); toast("Import undone", "ok"); after && after(); } catch (e) { fail(e); }
      } } : null);
      after && after();
    } }] });
}

function openLinkDialog() {
  const link = h("textarea", { class: "input", rows: 4, placeholder: "usn://…" });
  const err = h("div", { class: "fstate bad hidden" });
  link.addEventListener("input", () => err.classList.add("hidden"));
  modal({ title: "Open a share link", body: [h("div", { class: "muted" }, "Browse a share, check that it is complete and pick what you want. You can add it to the catalog from its page."), link, err],
    actions: [{ label: "Cancel" }, { label: "Open", kind: "primary", run: async () => {
      const v = link.value.trim();
      if (!v) { err.textContent = "Paste a usn:// link first."; err.classList.remove("hidden"); return false; }
      try { await openShare({ link: v }); }
      catch (e) { err.textContent = cap(e.message); err.classList.remove("hidden"); return false; }
    } }] });
}

// reviewShares: the review of whole shares (from the catalog, or pasted links), before they are
// queued: sizes against free space, folders, archives, files already there.
// src: { ids } (catalog entries) or { links }.
async function reviewShares(src, done) {
  const plan = src.ids ? await api("/catalog/plan", { body: { ids: src.ids } }) : await api("/jobs/plan", { body: { text: src.links.join("\n") } });
  const items = plan.items;
  if (!items.length) { toast("Nothing to queue"); return; }
  let mode = plan.extract_default, existing = "skip";
  const cat = h("select", { class: "input" }, plan.categories.map((c) => h("option", { value: c, selected: c === plan.category }, c)));
  const list = h("div", { class: "rv-list" });
  const showList = () => put(list, items.map((it) => h("div", { class: "rv-item" },
    h("div", { class: "rv-t" }, h("b", {}, it.title), it.active_jobs && it.active_jobs.length ? h("span", { class: "chip info" }, "already " + it.active_jobs[0].state) : null,
      it.folder_exists ? h("span", { class: "chip warn", title: it.out_dir }, "folder already there") : null),
    h("div", { class: "rv-s muted small" }, it.size != null ? (it.size_listed ? `listed as ${fmtBytes(it.size)}` : fmtBytes(it.size)) : "size known once it starts",
      " · ", h("span", { class: "mono" }, [plan.complete_dir, safeSeg(cat.value), it.name_known ? safeSeg(it.name) : "<the share's folder name>"].join(plan.sep))))));
  cat.onchange = showList; showList();
  const over = plan.free != null && plan.size > plan.free;
  const anyThere = items.some((it) => it.folder_exists);
  const choice = (name, cur, opts, set) => h("div", { class: "choices" }, opts.map(([v, label, help]) => h("label", { class: "choice" },
    h("input", { type: "radio", name, value: v, checked: v === cur, onchange: () => set(v) }),
    h("div", {}, h("div", { class: "cl" }, label, name === "xmode" && v === plan.extract_default ? h("span", { class: "chip plain" }, "default") : null), h("div", { class: "ch" }, help)))));
  modal({ title: items.length === 1 ? "Download " + items[0].title : `Download ${items.length} shares`, wide: true, body: [
    h("div", { class: "facts" },
      h("div", {}, h("div", { class: "k" }, "Shares"), h("div", { class: "v" }, items.length.toLocaleString())),
      h("div", {}, h("div", { class: "k" }, "Size"), h("div", { class: "v" }, fmtBytes(plan.size) + (plan.unknown ? " +" : "")),
        plan.unknown ? h("div", { class: "faint small" }, `${plan.unknown} not opened yet: their size shows once they start`) : null),
      h("div", {}, h("div", { class: "k" }, "Free space"), h("div", { class: "v" + (over ? " bad" : "") }, plan.free != null ? fmtBytes(plan.free) : "—"))),
    over ? h("div", { class: "note bad" }, `Not enough space: ${fmtBytes(plan.size - plan.free)} more is needed in ${plan.complete_dir}. A job fails when it starts unless space is freed first.`) : null,
    h("div", { class: "form tight" }, h("label", { class: "k" }, "Category"), cat),
    h("div", {}, h("div", { class: "k2" }, "Files go to"), list),
    h("div", {}, h("div", { class: "k2" }, "Archives", h("span", { class: "muted" }, " · counted when each download starts")),
      choice("xmode", mode, [["off", "Don't extract", "Keep every file exactly as it was shared."],
        ["sets", "RAR and multi-part sets only", "Single .zip and .7z files stay zipped, so ROM, BIOS and asset packs keep working."],
        ["all", "Everything", "Also unpack every single .zip and .7z."]], (v) => { mode = v; })),
    anyThere ? h("div", {}, h("div", { class: "k2" }, "Files already there", h("span", { class: "muted" }, " · some of these folders exist in your download folder")),
      choice("xexist", existing, [["skip", "Skip them", "A file with the same name and size counts as done."],
        ["verify", "Check them first", "Compare each one's SHA-256 with the share and download only what differs."],
        ["redownload", "Download everything again", "Overwrite what is there."]], (v) => { existing = v; })) : null,
  ], actions: [{ label: "Cancel" }, { label: over ? "Queue anyway" : items.length === 1 ? "Queue download" : `Queue ${items.length} downloads`, kind: "primary", run: async () => {
    const body = { category: cat.value, extract: mode, existing };
    const r = src.ids ? await api("/catalog/download", { body: { ...body, ids: src.ids } }) : await api("/jobs", { body: { ...body, text: src.links.join("\n") } });
    toast(`Queued ${r.jobs.length} download${r.jobs.length === 1 ? "" : "s"}` + (r.errors.length ? ` · ${r.errors.length} not queued: ${r.errors[0]}` : ""), r.errors.length ? "" : "ok",
      { label: "View in Activity", href: "#/activity" });
    if (done) done(r);
  } }] });
}

// downloadEntries: everything goes through a review. One share: the full review (its index is
// opened for it); several: the review of whole shares.
async function downloadEntries(entries, done) {
  if (entries.length === 1) {
    const e = entries[0];
    const s = await prepareShare({ catalog_id: e.id }, e.title);
    if (s) await reviewDownload(s, null, done);
    return;
  }
  await reviewShares({ ids: entries.map((e) => e.id) }, done);
}

async function pageCatalog() {
  setTitle("Catalog");
  const f = { q: store.get("cat.q", ""), source: store.get("cat.source", ""), launcher: store.get("cat.launcher", ""), device: store.get("cat.device", ""),
    hide: store.get("cat.hide", false), sort: store.get("cat.sort", "title") };
  let data = { entries: [], sources: [], categories: ["Default"], tags: { launchers: [], devices: [], groups: [] } };
  let groupOf = {}, deviceRank = {};   // device -> its group; device -> its place in the device list
  const sel = new Set();
  const view = $("#view");

  const q = h("input", { class: "input", placeholder: "Search titles, sources, SKUs…", value: f.q, oninput: () => { f.q = q.value; store.set("cat.q", f.q); render(); } });
  const src = h("select", { class: "input", onchange: () => { f.source = src.value; store.set("cat.source", f.source); render(); } });
  // launcher (Batocera, RetroBat, LaunchBox…) and device (PC, Steam Deck, Raspberry Pi 5…), read
  // from the titles and labels by the server. Device values: a device, "g:<group>", or "-" (none named)
  const lsel = h("select", { class: "input", title: "What the build runs: Batocera, RetroBat, LaunchBox…", onchange: () => setTag("launcher", lsel.value) });
  const dsel = h("select", { class: "input", title: "What the build is for: PC, Steam Deck, ROG Ally, Raspberry Pi…", onchange: () => setTag("device", dsel.value) });
  function setTag(which, v) {
    f[which] = v; store.set("cat." + which, v);
    (which === "launcher" ? lsel : dsel).value = v;
    render();
  }
  const sort = h("select", { class: "input", onchange: () => { f.sort = sort.value; store.set("cat.sort", f.sort); render(); } },
    [["title", "Title"], ["size-desc", "Size: largest"], ["size-asc", "Size: smallest"], ["added", "Recently added"], ["status", "Status"],
      ["launcher", "Launcher"], ["device", "Device"]]
      .map(([v, l]) => h("option", { value: v, selected: f.sort === v }, l)));
  const hide = h("input", { type: "checkbox", checked: f.hide, onchange: () => { f.hide = hide.checked; store.set("cat.hide", f.hide); render(); } });
  const filters = [h("div", { class: "search" }, icon("search"), q), src, lsel, dsel, sort, h("label", { class: "check", title: "Hide entries that are queued or downloaded in full" }, hide, "Hide queued / done")];
  const importBtn = h("button", { class: "btn primary", onclick: () => importDialog(load) }, icon("upload"), "Import list");
  toolbar(...filters, h("button", { class: "btn", onclick: openLinkDialog }, icon("link"), "Open link"), importBtn);

  const dock = h("div", { class: "selbar dock hidden" });
  const count = h("div", { class: "countline" });
  const tbody = h("tbody");
  const head = h("input", { type: "checkbox", title: "Select everything listed", onchange: () => { for (const e of visible()) head.checked ? sel.add(e.id) : sel.delete(e.id); render(); } });
  const sortHeads = [];
  function cth(label, key, cls = "") {   // a header that sorts; size toggles largest / smallest first
    const th = h("th", { class: "sortable " + cls, title: "Sort", onclick: () => {
      const v = key === "size" ? (f.sort === "size-desc" ? "size-asc" : "size-desc") : key;
      f.sort = v; sort.value = v; store.set("cat.sort", v); render();
    } }, label, h("span", { class: "arrow" }));
    sortHeads.push([th, key]);
    return th;
  }
  const thead = h("thead", {}, h("tr", {}, h("th", { class: "cb" }, head), cth("Title", "title"), cth("Size", "size", "right"),
    h("th", {}, "Health"), cth("Status", "status"), h("th", { class: "act" }, "")));
  const table = h("div", { class: "tablewrap" }, h("table", { class: "t" }, thead, tbody));
  view.append(count, table, dock);
  api("/status").then((st) => {
    if (st.needs_setup) view.prepend(h("div", { class: "note warn", style: { marginBottom: "14px" } }, "No Usenet provider yet. ",
      h("a", { href: "#/setup/providers" }, "Add your provider"), " so shares can be opened and downloaded."));
  }).catch(() => {});

  const size = (e) => (e.share && e.share.size) || e.listed_size || 0;
  const ACTIVE = new Set(["queued", "paused", "planning", "downloading", "extracting", "stopping"]);
  const active = (e) => e.job && ACTIVE.has(e.job.state);
  const complete = (e) => !!(e.got && e.got.complete);
  const done = (e) => active(e) || complete(e);
  const launchers = (e) => e.launchers || [], devices = (e) => e.devices || [];
  function tagOk(tags, want) {
    if (!want) return true;
    if (want === "-") return !tags.length;
    if (want.startsWith("g:")) return tags.some((t) => groupOf[t] === want.slice(2));
    return tags.includes(want);
  }
  function visible() {
    const words = f.q.toLowerCase().split(/\s+/).filter(Boolean);
    let out = data.entries.filter((e) => (!f.source || e.source === f.source) && !(f.hide && done(e)) &&
      tagOk(launchers(e), f.launcher) && tagOk(devices(e), f.device) &&
      words.every((w) => `${e.title} ${e.variant} ${e.listed} ${e.source} ${e.sku} ${e.name} ${(e.also || []).join(" ")} ${launchers(e).join(" ")} ${devices(e).join(" ")}`.toLowerCase().includes(w)));
    const rank = (e) => (active(e) ? 3 : complete(e) ? 2 : e.got ? 1 : 0);
    const title = (a, b) => a.title.localeCompare(b.title);
    // grouped by the first launcher (A to Z) or device (in the device list's order); none named last
    const firstL = (e) => launchers(e)[0] || "\uffff", firstD = (e) => (devices(e).length ? deviceRank[devices(e)[0]] ?? 1e6 : 1e7);
    const by = { "size-desc": (a, b) => size(b) - size(a), "size-asc": (a, b) => size(a) - size(b), added: (a, b) => (b.added || 0) - (a.added || 0),
      status: (a, b) => rank(b) - rank(a), title,
      launcher: (a, b) => firstL(a).localeCompare(firstL(b)) || title(a, b), device: (a, b) => firstD(a) - firstD(b) || title(a, b) }[f.sort] || title;
    return out.sort(by);
  }

  // the launcher and device chips of a row: a click shows only entries with that one (again: all)
  const tagChip = (which, t) => {
    const pick = () => setTag(which, f[which] === t ? "" : t);
    return h("span", { class: "chip tag " + which, role: "button", tabindex: "0",
      title: f[which] === t ? "Show every entry again" : `Show only ${which === "launcher" ? "builds running" : "builds for"} ${t}`,
      onclick: pick, onkeydown: (ev) => { if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); pick(); } } }, t);
  };

  function fillTagMenus() {
    const tags = data.tags || { launchers: [], devices: [], groups: [] };
    groupOf = {}; deviceRank = {};
    tags.devices.forEach((d, i) => { groupOf[d.name] = d.group; deviceRank[d.name] = i; });
    const noneL = data.entries.filter((e) => !launchers(e).length).length, noneD = data.entries.filter((e) => !devices(e).length).length;
    const opt = (v, label, n, cur) => h("option", { value: v, selected: cur === v }, n == null ? label : `${label} (${n})`);
    lsel.replaceChildren(opt("", "All launchers", null, f.launcher), ...tags.launchers.map((l) => opt(l.name, l.name, l.count, f.launcher)),
      noneL ? opt("-", "None named", noneL, f.launcher) : null);
    const ANY = { "Handheld PCs": "Any handheld PC", "Raspberry Pi": "Any Raspberry Pi", "Retro handhelds": "Any retro handheld", "Other boards": "Any other board", Consoles: "Any console" };
    dsel.replaceChildren(opt("", "All devices", null, f.device), ...tags.groups.map((g) => {
      const ds = tags.devices.filter((d) => d.group === g);
      if (!ds.length) return null;
      const n = data.entries.filter((e) => devices(e).some((t) => groupOf[t] === g)).length;
      return h("optgroup", { label: g }, ds.length > 1 ? opt("g:" + g, ANY[g] || "Any " + g, n, f.device) : null, ...ds.map((d) => opt(d.name, d.name === g ? d.name + " (model not named)" : d.name, d.count, f.device)));
    }), noneD ? opt("-", "None named", noneD, f.device) : null);
    // a remembered choice the catalog no longer has: show everything rather than nothing
    for (const [which, el] of [["launcher", lsel], ["device", dsel]]) {
      if (f[which] && el.value !== f[which]) { f[which] = ""; store.set("cat." + which, ""); el.value = ""; }
    }
  }

  // status: a running job first; then whether the share is on disk in full or in part
  function statusCell(e) {
    const j = e.job, g = e.got;
    if (j && j.state === "downloading" && j.size) {
      const p = pct(j.bytes, j.size);
      return h("div", { class: "nowrap", title: j.picked ? "Downloading " + j.picked : "" }, h("span", { class: "bar mini" }, h("i", { style: { width: p + "%" } })), " ", h("span", { class: "muted" }, p.toFixed(1) + "%"));
    }
    if (j && ACTIVE.has(j.state)) return stateChip(j.state);
    if (j && j.state === "failed") return h("span", { title: j.detail || "" }, stateChip("failed"));   // the last try failed: say so first
    if (g && g.complete) return h("span", { class: "chip ok", title: "Every file of this share has been downloaded" }, icon("check"), "downloaded");
    if (g && g.files) return h("span", { class: "chip warn", title: "Some of this share has been downloaded" }, g.of ? `partial · ${g.files.toLocaleString()} of ${g.of.toLocaleString()} files` : `partial · ${g.files.toLocaleString()} files`);
    if (j) return h("span", { title: j.detail || "" }, stateChip(j.state));
    return h("span", { class: "faint" }, "—");
  }

  function sizeCell(e) {
    if (e.share) return [fmtBytes(e.share.size), h("div", { class: "faint small", title: e.listed ? "Listed as " + e.listed : "" }, `${e.share.files.toLocaleString()} file${e.share.files === 1 ? "" : "s"}`)];
    if (e.listed) return h("span", { class: "muted", title: "The size the list gives; the share's own size shows once it has been opened" }, e.listed, h("div", { class: "faint small" }, "listed"));
    return h("span", { class: "faint" }, "—");
  }

  function render() {
    const total = data.entries.length;
    lsel.classList.toggle("on", !!f.launcher); dsel.classList.toggle("on", !!f.device);   // a filter is narrowing the list
    for (const el of filters) el.classList.toggle("hidden", !total);   // nothing to filter yet
    thead.classList.toggle("hidden", !total);
    const vis = visible();
    tbody.replaceChildren(...vis.map((e) => {
      const cb = h("input", { type: "checkbox", checked: sel.has(e.id), onchange: () => { cb.checked ? sel.add(e.id) : sel.delete(e.id); render(); } });
      return h("tr", { class: sel.has(e.id) ? "sel" : "" },
        h("td", { class: "cb" }, cb),
        h("td", { class: "title-cell" }, h("a", { class: "t1", href: "#", title: "Browse this share", onclick: (ev) => { ev.preventDefault(); openShare({ catalog_id: e.id }).catch(fail); } }, e.title),
          h("div", { class: "t2" }, e.variant ? h("span", {}, e.variant) : null,
            launchers(e).map((t) => tagChip("launcher", t)), devices(e).map((t) => tagChip("device", t)),
            e.source ? h("span", { class: "chip plain" }, e.source) : null,
            e.sku ? h("span", { class: "faint mono" }, e.sku) : null,
            e.info ? h("a", { href: e.info, target: "_blank", rel: "noopener", title: e.info }, icon("ext")) : null)),
        h("td", { class: "right nowrap" }, sizeCell(e)),
        h("td", {}, healthChip(e.health)),
        h("td", {}, statusCell(e)),
        h("td", { class: "act" },
          h("button", { class: "btn sm", title: "Browse files, check availability, stream", onclick: () => openShare({ catalog_id: e.id }).catch(fail) }, icon("folder"), "Browse"),
          " ", h("button", { class: "btn sm ghost icon", title: "Download this share… (shows what it takes first)", onclick: () => downloadEntries([e], () => load()).catch(fail) }, icon("download"))));
    }));
    head.checked = vis.length > 0 && vis.every((e) => sel.has(e.id));
    for (const [th, key] of sortHeads) {
      const on = key === "size" ? f.sort.startsWith("size") : f.sort === key;
      th.classList.toggle("sorted", on);
      th.querySelector(".arrow").textContent = on ? (f.sort === "size-asc" ? " ↑" : " ↓") : "";
    }
    const selected = data.entries.filter((e) => sel.has(e.id));
    const partial = data.entries.filter((e) => e.got && !e.got.complete).length;
    put(count, total ? [h("span", {}, `${vis.length} of ${total} entries`),
      h("span", {}, `${data.entries.filter(complete).length} downloaded`), partial ? h("span", {}, `${partial} in part`) : null,
      h("span", {}, `${data.entries.filter((e) => e.health && e.health.verdict === "available").length} checked available`)] : null);
    dock.classList.toggle("hidden", !selected.length);
    importBtn.classList.toggle("primary", !selected.length);   // one main action at a time
    if (selected.length) {
      put(dock, h("b", {}, `${selected.length} selected`), h("span", { class: "muted" }, fmtBytes(selected.reduce((a, e) => a + size(e), 0))),
        h("span", { class: "grow" }),
        h("button", { class: "btn ghost", onclick: () => {
          const was = [...sel]; sel.clear(); render();
          toast("Selection cleared", "", { label: "Undo", run: () => { was.forEach((id) => sel.add(id)); render(); } });
        } }, "Clear"),
        h("button", { class: "btn danger", onclick: async () => {
          if (!(await confirmBox("Remove from catalog", `Remove ${selected.length} entr${selected.length === 1 ? "y" : "ies"} from the catalog? Downloads are not touched.`, "Remove", "danger solid"))) return;
          await api("/catalog/remove", { body: { ids: [...sel] } }).catch(fail); sel.clear(); load();
        } }, icon("trash"), "Remove"),
        h("button", { class: "btn primary", onclick: () => downloadEntries(selected, () => { sel.clear(); load(); }).catch(fail) }, icon("download"), "Download selected…"));
    }
    if (!total) {
      tbody.replaceChildren(h("tr", {}, h("td", { colspan: 6 }, h("div", { class: "empty left" }, h("h3", {}, "No shares yet"),
        h("div", { class: "muted" }, "usnfetch doesn't come with any shares. Add the share links you have:"), addSharesHelp(true)))));
    }
  }

  async function load() {
    data = await api("/catalog");
    src.replaceChildren(h("option", { value: "" }, "All sources"), ...data.sources.map((s) => h("option", { value: s, selected: f.source === s }, s)));
    fillTagMenus();
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
  clearUndo(sid) {                      // clear, with a moment to take it back
    const was = this.all[sid]; if (!was) return;
    this.clear(sid);
    toast(`Selection in ${was.title || "this share"} cleared`, "", { label: "Undo", run: () => { this.all[sid] = was; this.save(); } });
  },
  list(sid) { return Object.entries(this.items(sid)).sort(([a], [b]) => a.localeCompare(b)); },
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
        h("button", { class: "btn ghost icon", title: "Clear this selection", onclick: () => basket.clearUndo(sid) }, icon("x")));
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
  let haveTimer = null, haveSeq = 0, allThere = false, queueBtn = null;
  const nothingNote = h("div", { class: "note ok-note hidden" });
  const over = plan.free != null && plan.size > plan.free;
  function showQueue() {   // everything already there and skipped: nothing to queue
    const nothing = allThere && existing === "skip";
    nothingNote.classList.toggle("hidden", !nothing);
    if (queueBtn) { queueBtn.disabled = nothing; queueBtn.textContent = nothing ? "Nothing to download" : over ? "Queue anyway" : "Queue download"; }
  }
  for (const r of haveChoices.querySelectorAll("input")) r.addEventListener("change", showQueue);
  async function countHave() {
    const seq = ++haveSeq;
    haveLine.textContent = " · checking…";
    let r;
    try { r = await api(`/shares/${s.id}/ondisk`, { body: { ...(sel || {}), category: cat.value, name: name.value.trim() } }); }
    catch (e) { haveLine.textContent = ""; return; }
    if (seq !== haveSeq) return;
    allThere = false;
    if (!r.counted) { haveLine.textContent = " · not counted for a selection this large"; haveChoices.classList.remove("hidden"); showQueue(); return; }
    const any = r.files > 0 || r.partial > 0;
    allThere = r.of > 0 && r.files >= r.of && !r.partial;
    nothingNote.textContent = `Nothing to download: all ${r.of.toLocaleString()} file${r.of === 1 ? " is" : "s are"} already in ${r.dir}. To check or replace them, pick an option above.`;
    haveChoices.classList.toggle("hidden", !any);
    haveLine.textContent = !any ? " · none of these files are there yet"
      : ` · ${r.files.toLocaleString()} of ${r.of.toLocaleString()} file${r.of === 1 ? "" : "s"} (${fmtBytes(r.bytes)}) already there` +
        (r.partial ? `, ${r.partial.toLocaleString()} partly downloaded (those carry on)` : "") + ` · about ${fmtBytes(Math.max(0, plan.size - r.bytes))} left to download`;
    showQueue();
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
  // what is ticked, item by item (a selection kept while browsing is easy to lose track of)
  const picked = sel ? basket.list(s.id) : [];
  const pickedList = picked.length ? h("details", { class: "picklist" }, h("summary", {}, `Show the ${picked.length.toLocaleString()} selected item${picked.length === 1 ? "" : "s"}`),
    h("div", { class: "pl" }, picked.slice(0, 500).map(([p, m]) => h("div", { class: "pr" }, icon(m.folder ? "folder" : "file"), h("span", { class: "pp", title: p }, p || "All files"),
      h("span", { class: "faint" }, m.folder ? `${(m.count || 0).toLocaleString()} files · ${fmtBytes(m.size)}` : fmtBytes(m.size)))),
    picked.length > 500 ? h("div", { class: "faint small" }, `and ${picked.length - 500} more`) : null)) : null;
  modal({ title: "Download from " + shareTitle(s), body: [
    h("div", { class: "facts" },
      h("div", {}, h("div", { class: "k" }, "Files"), h("div", { class: "v" }, plan.files.toLocaleString())),
      h("div", {}, h("div", { class: "k" }, "Size"), h("div", { class: "v" }, fmtBytes(plan.size))),
      h("div", {}, h("div", { class: "k" }, "Free space"), h("div", { class: "v" + (over ? " bad" : "") }, plan.free != null ? fmtBytes(plan.free) : "—"))),
    pickedList,
    over ? h("div", { class: "note bad" }, `Not enough space: ${fmtBytes(plan.size - plan.free)} more is needed in ${plan.complete_dir}. The job fails when it starts unless space is freed first.`) : null,
    h("div", { class: "form tight" }, h("label", { class: "k" }, "Category"), cat, h("label", { class: "k" }, "Folder"), name),
    h("div", {}, h("div", { class: "k2" }, "Files go to"), dest),
    found.length ? h("div", {}, h("div", { class: "k2" }, "Archives", h("span", { class: "muted" }, " · this selection has " + found.join(", "))), choices,
      plan.delete_archives ? h("div", { class: "help" }, "Archives that extract cleanly are deleted afterwards (Settings → Downloads).") : null) : null,
    h("div", {}, h("div", { class: "k2" }, "Files already there", haveLine), haveChoices),
    nothingNote,
    plan.active_jobs && plan.active_jobs.length ? h("div", { class: "note" }, `Another download from this share is ${plan.active_jobs[0].state}: this one starts when it finishes.`) : null,
  ], actions: [{ label: "Cancel" }, { label: over ? "Queue anyway" : "Queue download", kind: "primary", ref: (b) => { queueBtn = b; }, run: async () => {
    const r = await api(`/shares/${s.id}/download`, { body: { ...(sel || {}), category: cat.value, name: name.value.trim(), extract: mode, existing } });
    toast(`Queued ${r.files.toLocaleString()} file${r.files === 1 ? "" : "s"} · ${fmtBytes(r.size)}`, "ok", { label: "View in Activity", href: "#/activity" });
    if (done) done(r);
  } }] });
}

// ---------------------------------------------------------------- share browser
const loneTop = new Map();   // share id -> its one top folder (a share that is a single folder starts inside it)

async function pageShare(parts) {
  const sid = parts[0];
  const path = decodeURIComponent(parts.slice(1).join("/"));
  const view = $("#view");
  setTitle("Opening share…", "Catalog", "#/catalog");
  if (needsProvider()) {
    view.replaceChildren(emptyState("server", "No Usenet provider yet", "usnfetch reads shares from your own Usenet account. Add your provider, then come back to this share.",
      h("p", {}, h("a", { class: "btn primary", href: "#/setup/providers" }, "Add a provider"))));
    setTitle("Share", "Catalog", "#/catalog");
    return;
  }
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
      setTitle(s.title || "Share", "Catalog", "#/catalog");
      const again = h("button", { class: "btn", onclick: () => openShare(store.get("link." + sid, null) ? { link: store.get("link." + sid) } : { catalog_id: sid }).then(route).catch(fail) }, icon("refresh"), "Try again");
      view.replaceChildren(h("div", { class: "note bad" }, cap(s.error)),
        h("p", { class: "row-btns" }, s.error_code === "no_provider" ? h("a", { class: "btn primary", href: "#/setup/providers" }, "Add a provider") : null, again),
        h("div", { class: "loglines" }, (s.log || []).join("\n")));
      return;
    }
  }
  renderShare(s, path);
}

function sharePassword(s) {
  setTitle(s.title || "Share", "Catalog", "#/catalog");
  const pw = h("input", { class: "input", type: "password", placeholder: "Share password" });
  $("#view").replaceChildren(h("form", { class: "panel login", onsubmit: async (ev) => {
    ev.preventDefault(); try { await api("/shares/open", { body: { link: s.link, password: pw.value } }); route(); } catch (e) { fail(e); } } },
  h("h2", {}, "Password-protected share"), h("div", { class: "muted" }, s.error), pw, h("button", { class: "btn primary" }, "Open")));
}

async function renderShare(s, path) {
  const sid = s.id, info = s.info || {}, cat = s.catalog;
  setTitle(shareTitle(s), "Catalog", "#/catalog");
  if (basket.all[sid] && basket.all[sid].title !== shareTitle(s)) { basket.all[sid].title = shareTitle(s); basket.save(); }
  const allBtn = h("button", { class: "btn primary", onclick: () => reviewDownload(s, null).catch(fail) }, icon("download"), "Download all…");
  const addBtn = cat ? null : h("button", { class: "btn", title: "Keep this share in the catalog", onclick: async () => {
    try { await api("/catalog/add", { body: { sid } }); toast("Added to the catalog", "ok"); addBtn.remove(); s = await api("/shares/" + sid); } catch (e) { fail(e); }
  } }, icon("plus"), "Add to catalog");
  toolbar(addBtn, h("button", { class: "btn", onclick: () => runCheck(64) }, icon("pulse"), "Check availability"), allBtn);

  const summary = h("div", { class: "sharesum" });
  const dock = h("div", { class: "selbar dock hidden" });
  const crumbs = h("div", { class: "crumbs" });
  const leaf = path ? path.split("/").pop() : "";
  let view = store.get("view." + sid, null), galleryKind = "";
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
  let scopeAll = !path || path === loneTop.get(sid);   // at the share's start: search all of it
  const q = h("input", { class: "input", type: "search", placeholder: scopeAll ? "Search this share…" : `Search in ${leaf}…`, autocomplete: "off",
    oninput: () => schedule(), onkeydown: (ev) => { if (ev.key === "Escape" && q.value) { ev.stopPropagation(); q.value = ""; schedule(0); } } });
  const tbody = h("tbody");
  const head = h("input", { type: "checkbox", title: "Select everything listed" });
  const galAll = h("input", { type: "checkbox" });
  const galAllLbl = h("label", { class: "check hidden", title: "Select every game shown here" }, galAll, "Select all");
  const listing = h("div", { class: "tablewrap" }, h("table", { class: "t" }, h("thead", {}, h("tr", {}, h("th", { class: "cb" }, head), thName,
    thSize, h("th", {}, ""), h("th", { class: "act" }, ""))), tbody));
  const foot = h("div", { class: "countline", style: { marginTop: "10px" } });
  const grid = h("div", { class: "gallery hidden" });
  $("#view").replaceChildren(summary, h("div", { class: "browsebar" }, crumbs, viewSw, galAllLbl,
    sortSel, h("label", { class: "check", title: "Hide files that are already in your download folder" }, hideBox, "Hide what I have"),
    h("div", { class: "search" }, icon("search"), q)), listing, grid, foot, dock);

  // ---- the share in one line; the rest under Details
  let detailsOpen = store.get("sharedetails", false);
  function renderSummary() {
    const ck = s.check;
    const fp = (info.owner_fingerprint || "").slice(0, 16);
    const avail = s.checking ? h("span", {}, h("span", { class: "spin" }), " checking availability…")
      : ck && ck.verdict ? h("span", { title: ck.checked_at ? `${ck.sampled} articles sampled · ${ago(ck.checked_at)}` : "" }, healthChip(ck))
      : h("span", { class: "faint" }, "availability not checked");
    const fact = (k, v, sub) => h("div", { class: "card" }, h("div", { class: "k" }, k), h("div", { class: "v" }, v), sub ? h("div", { class: "s" }, sub) : null);
    put(summary, h("div", { class: "sum-line" },
      h("b", {}, fmtBytes(info.size)), cat && cat.listed ? h("span", { class: "faint" }, `(listed as ${cat.listed})`) : null,
      h("span", {}, `${(info.files || 0).toLocaleString()} files`), avail, h("span", { class: "grow" }),
      h("button", { class: "btn sm ghost", "aria-expanded": detailsOpen ? "true" : "false", onclick: () => { detailsOpen = !detailsOpen; store.set("sharedetails", detailsOpen); renderSummary(); } }, icon("info"), detailsOpen ? "Hide details" : "Details")),
    detailsOpen ? h("div", { class: "cards" },
      fact("Format", `v${info.version ?? "?"} · ${info.access || "?"}`, info.created_at ? `published ${new Date(info.created_at).toLocaleDateString()}` : info.format || ""),
      fact("Small files", (info.packed || 0).toLocaleString(), "packed several to an article"),
      h("div", { class: "card" }, h("div", { class: "k" }, "Uploader"), h("div", { class: "v mono", title: info.owner_fingerprint || "" }, fp || "—"), h("div", { class: "s" }, info.signature || "")),
      h("div", { class: "card wide" }, h("div", { class: "k" }, "Availability"), h("div", { class: "v" }, avail),
        ck && ck.accounts && ck.accounts.length ? h("div", { class: "acct-list" }, ck.accounts.map((a) => h("div", { class: "r" },
          h("span", {}, a.account, a.backup ? h("span", { class: "faint" }, ` (priority ${a.priority ?? 1})`) : ""),
          a.error ? h("span", { class: "chip bad", title: a.error }, "error") : h("b", {}, `${a.available}/${ck.sampled}`)))) : null,
        h("div", { class: "s" }, ck && ck.checked_at ? `${ck.sampled} articles sampled · ${ago(ck.checked_at)} · ` : "",
          h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); runCheck(512); } }, "Check more thoroughly")))) : null);
  }

  async function runCheck(sample) {
    try { s = await api(`/shares/${sid}/check`, { body: { sample } }); renderSummary(); }
    catch (e) { fail(e); return; }
    const t = setInterval(async () => {
      try { s = await api("/shares/" + sid); } catch { clearInterval(t); return; }
      renderSummary();
      if (!s.checking) { clearInterval(t); const v = (s.check || {}).verdict; toast(`Availability: ${(VERDICT[v] || ["", v])[1]}`, v === "available" ? "ok" : v === "partial" ? "" : "bad"); }
    }, 1200);
    page.stop.push(() => clearInterval(t));
  }

  // ---- selection: rows bind to the basket
  let boxes = [];   // { cb, p, meta, note } of the rows / cards on screen
  function selBox(p, meta, note) {
    const cb = h("input", { type: "checkbox", onclick: (ev) => ev.stopPropagation(),
      onchange: () => { cb.checked ? basket.add(s, p, meta) : basket.remove(sid, p); } });
    boxes.push({ cb, p, meta, note });
    return cb;
  }
  const toggleSel = (p, meta) => { if (basket.coveredBy(sid, p) != null) return; basket.has(sid, p) ? basket.remove(sid, p) : basket.add(s, p, meta); };
  let drawer = false;
  function refreshSel() {
    const items = basket.items(sid), keys = Object.keys(items);
    for (const b of boxes) {
      const via = basket.coveredBy(sid, b.p);
      b.cb.checked = via != null || basket.has(sid, b.p);
      b.cb.disabled = via != null;
      b.cb.title = via == null ? "" : via === "" ? "Included: the whole share is selected" : `Included: folder ${via} is selected`;
      // a folder with ticked things inside it shows a part tick and how many
      const inside = b.meta.folder && !b.cb.checked ? keys.filter((k) => b.p === "" || k.startsWith(b.p + "/")).length : 0;
      b.cb.indeterminate = inside > 0;
      if (b.note) b.note.textContent = inside ? `${inside.toLocaleString()} selected inside` : "";
      const tr = b.cb.closest("tr, .gcard"); if (tr) tr.classList.toggle("sel", b.cb.checked);
    }
    const free = boxes.filter((b) => !b.cb.disabled);
    head.checked = boxes.length > 0 && boxes.every((b) => b.cb.checked);
    galAll.checked = free.length > 0 && free.every((b) => b.cb.checked) && boxes.length > 0;
    const t = basket.totals(sid);
    dock.classList.toggle("hidden", !t.n);
    allBtn.classList.toggle("primary", !t.n);    // one main action at a time
    if (!t.n) { drawer = false; return; }
    const nf = keys.filter((k) => items[k].folder).length, nx = t.n - nf;
    const what = [nf ? `${nf.toLocaleString()} folder${nf === 1 ? "" : "s"}` : "", nx ? `${nx.toLocaleString()} file${nx === 1 ? "" : "s"}` : ""].filter(Boolean).join(" and ");
    put(dock, h("div", { class: "dock-row" },
      h("button", { class: "btn ghost sm", "aria-expanded": drawer ? "true" : "false", title: "List what is selected", onclick: () => { drawer = !drawer; refreshSel(); } },
        icon(drawer ? "x" : "list"), h("b", {}, `${what} selected`)),
      h("span", { class: "muted" }, nf ? `${t.files.toLocaleString()} file${t.files === 1 ? "" : "s"} in all · ${fmtBytes(t.size)}` : fmtBytes(t.size)),
      h("span", { class: "faint dock-hint" }, "kept while you browse other folders"), h("span", { class: "grow" }),
      h("button", { class: "btn ghost", onclick: () => basket.clearUndo(sid) }, "Clear"),
      h("button", { class: "btn primary", onclick: () => reviewDownload(s, basket.body(sid), () => basket.clear(sid)).catch(fail) }, icon("download"), "Review & download…")),
    drawer ? h("div", { class: "drawer" }, basket.list(sid).map(([p, m]) => h("div", { class: "pr" }, icon(m.folder ? "folder" : "file"),
      h("a", { class: "pp", href: "#", title: p, onclick: (ev) => { ev.preventDefault(); goFolder(m.folder ? p : parentOf(p)); } }, p || "All files"),
      h("span", { class: "faint nowrap" }, m.folder ? `${(m.count || 0).toLocaleString()} files · ${fmtBytes(m.size)}` : fmtBytes(m.size)),
      h("button", { class: "btn ghost icon sm", title: "Take out of the selection", onclick: () => basket.remove(sid, p) }, icon("x"))))) : null);
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
  galAll.onchange = () => {
    const on = galAll.checked;
    for (const b of boxes) if (!b.cb.disabled && !b.meta.folder) on ? basket.add(s, b.p, b.meta, false) : basket.remove(sid, b.p, false);
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
  const storage = (f) => (f.packed ? "stored with other small files in one article" : f.size === 0 ? "empty file" : `${f.segments.toLocaleString()} article${f.segments === 1 ? "" : "s"}`);

  function folderRow(d, inSearch) {
    const sub = inSearch && parentOf(d.path) ? h("div", { class: "sub" }, parentOf(d.path)) : null;
    const note = h("span", { class: "chip info plain insel" });
    return h("tr", { class: "folder-row", onclick: () => goFolder(d.path) },
      h("td", { class: "cb" }, selBox(d.path, { folder: true, size: d.size, count: d.count }, note)),
      h("td", {}, h("div", { class: "name-cell folder" }, icon("folder"), h("div", { class: "nmwrap" }, h("span", { class: "nm" }, d.name), sub))),
      h("td", { class: "right nowrap" }, fmtBytes(d.size)), h("td", { class: "muted nowrap" }, `${d.count.toLocaleString()} file${d.count === 1 ? "" : "s"}`,
        d.local ? [" ", h("span", { class: "chip ok plain", title: "This folder is in your download folder" }, "on disk")] : null, " ", note), h("td", { class: "act" }));
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
      h("td", { class: "right nowrap", title: storage(f) }, fmtBytes(f.size)),
      h("td", { class: "nowrap" }, localChip(f) || (f.size === 0 ? h("span", { class: "chip" }, "empty") : null)),
      h("td", { class: "act" },
        previewable(f) ? h("button", { class: "btn ghost icon", title: "Preview (streamed from Usenet)", onclick: () => preview(f) }, icon("eye")) : null,
        h("a", { class: "btn ghost icon", title: "Open in a new tab, straight from Usenet", href: fileUrl(f), target: "_blank" }, icon("ext")),
        h("a", { class: "btn ghost icon", title: "Save to this computer through the browser (not the download folder)", href: fileUrl(f, true) }, icon("save"))));
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
  const showGallery = () => !!galleryKind && (view === "gallery" || (view == null && (galleryKind === "launchbox" || galleryKind === "es")));
  function setView(v) {
    view = v; store.set("view." + sid, v);
    if (mode === "search") { q.value = ""; mode = "list"; }
    load();
  }
  let gal = { next: 0, truncated: false, shown: 0, matched: 0, hidden: 0 };
  const initials = (t) => (t.match(/[\p{L}\p{N}]+/gu) || []).slice(0, 3).map((w) => w[0]).join("").toUpperCase();
  function card(it) {
    const f = it.file, meta = { size: f.size };
    const ph = () => h("div", { class: "ph" }, h("b", {}, initials(it.title) || "?"), h("span", {}, it.title));
    const art = it.art ? h("img", { loading: "lazy", decoding: "async", alt: it.title, src: `/api/shares/${sid}/file/${it.art}`, onerror: (ev) => ev.target.replaceWith(ph()) }) : ph();
    return h("div", { class: "gcard" },
      h("div", { class: "art", title: "Select (or take out)", onclick: () => toggleSel(f.path, meta) }, art, localChip(f),
        h("button", { class: "btn icon sm ginfo", title: "Details", onclick: (ev) => { ev.stopPropagation(); cardInfo(it); } }, icon("info"))),
      h("label", { class: "gmeta" }, selBox(f.path, meta), h("div", { class: "gt" }, h("div", { class: "nm", title: f.name }, it.title), h("div", { class: "sub" }, fmtBytes(f.size)))));
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
    if (mode !== "list" || !showGallery()) return;
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
    if (!gal.shown && !g.folders.length) grid.append(emptyState(null, null, "Nothing to show here."));
    const kind = { launchbox: `LaunchBox art for ${g.platform}`, es: "art from the folders beside the games", images: "pictures in this folder" }[g.kind] || "";
    put(foot, h("span", {}, `${gal.shown.toLocaleString()} shown${kind ? " · " + kind : ""}`),
      g.kind !== "images" ? h("span", { class: "muted" }, `${gal.matched.toLocaleString()} with box art`) : null,
      gal.hidden ? h("span", { class: "muted" }, `${gal.hidden.toLocaleString()} you already have hidden`) : null,
      gal.truncated ? h("button", { class: "btn sm", onclick: () => loadGallery(true) }, "Load more") : null,
      h("span", { class: "faint" }, "Click a cover to select it. Covers stream from Usenet as you scroll."));
    refreshSel();
  }
  async function load(more = false) {
    let t;
    const offset = more ? lst.files.length : 0;
    try { t = await api(`/shares/${sid}/tree?path=${encodeURIComponent(path)}&offset=${offset}&limit=${PAGE}&sort=${ssort}`); }
    catch (e) { if (e.code === "not_open") { route(); return; } fail(e); return; }   // closed (idle or restart): re-open
    if (mode !== "list") return;
    // a share that is one folder at the top: that folder is where it starts
    if (!more && !path && t.folders.length === 1 && !t.files.length) {
      loneTop.set(sid, t.folders[0].path);
      history.replaceState(null, "", `#/share/${sid}/${t.folders[0].path.split("/").map(encodeURIComponent).join("/")}`);
      route();
      return;
    }
    if (!more) {
      lst = { t, files: [] }; boxes = [];
      galleryKind = t.gallery || "";
      viewSw.classList.toggle("hidden", !galleryKind);
      const gview = showGallery();
      galAllLbl.classList.toggle("hidden", !gview);
      for (const [i, b] of [...viewSw.children].entries()) b.classList.toggle("on", (i === 1) === gview);
      if (gview) { listing.classList.add("hidden"); grid.classList.remove("hidden"); loadGallery(); return; }
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
      if (!rows.length) rows.push(h("tr", {}, h("td", { colspan: 5 }, emptyState(null, null, lst.hidden ? "You already have everything listed here." : "Empty folder"))));
      tbody.replaceChildren(...rows);
    }
    renderFoot(); refreshSel();
  }
  // the path from the share's start: "All files", or its lone top folder
  const segs = path ? path.split("/") : [];
  const top = loneTop.get(sid);
  const fromTop = top && (path === top || path.startsWith(top + "/")) ? 1 : 0;
  const href = (n) => `#/share/${sid}/${segs.slice(0, n).map(encodeURIComponent).join("/")}`;
  put(crumbs, fromTop ? h("a", { href: href(1), class: segs.length === 1 ? "cur" : "", title: "The top of this share" }, icon("folder"), " " + segs[0])
    : h("a", { href: `#/share/${sid}`, class: segs.length ? "" : "cur" }, icon("folder"), " All files"),
  ...segs.slice(fromTop).flatMap((p, i) => [h("span", { class: "sep" }, "›"), h("a", { href: href(fromTop + i + 1), class: fromTop + i === segs.length - 1 ? "cur" : "" }, p)]));

  // ---- search: words match anywhere in the path, at least one in the name itself
  let timer = null, ctl = null;
  function schedule(ms = 250) { clearTimeout(timer); timer = setTimeout(runSearch, ms); }
  page.stop.push(() => { clearTimeout(timer); if (ctl) ctl.abort(); });
  async function runSearch() {
    const text = q.value.trim();
    if (ctl) ctl.abort();
    if (text.length < 2) { if (mode !== "list") { mode = "list"; load(); } return; }
    mode = "search";
    galAllLbl.classList.add("hidden");
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
    if (!rows.length) rows.push(h("tr", {}, h("td", { colspan: 5 }, emptyState(null, null, `Nothing matches “${text}”${scope ? " in " + leaf : ""}.`))));
    tbody.replaceChildren(...rows);
    const n = (x, w) => `${x.toLocaleString()} ${w}${x === 1 ? "" : "s"}`;
    put(foot, h("span", {}, `${n(r.file_matches, "file")} and ${n(r.folder_matches, "folder")} match${scope ? " in " + leaf : ""}`),
      r.files.length < r.file_matches || r.folders.length < r.folder_matches ? h("span", { class: "muted" }, ssort === "name" ? "showing the first ones by path: add words to narrow it down" : "showing the largest / smallest of the matches") : null,
      scope ? h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); scopeAll = true; q.placeholder = "Search this share…"; runSearch(); } }, "search the whole share") : null,
      h("span", { class: "faint" }, `${r.ms} ms`));
    refreshSel();
  }

  // a download of this share finished: its files are on disk now
  page.onJobDone = (j) => { if (j.share === sid) { if (mode === "search") runSearch(); else load(); } };

  renderSummary(); refreshSel(); await load();
  if (s.checking) runCheck(0).catch(() => {});
}

// ---------------------------------------------------------------- activity
// addLinksDialog: paste links; the ones that can't be used are marked with the reason, and the
// rest go on to the review (one share: its full review; several: the review of whole shares)
function addLinksDialog() {
  const text = h("textarea", { class: "input", rows: 6, placeholder: "One or more usn:// links" });
  const out = h("div", { class: "hidden" });
  let checked = null, go = null;   // the checked links, while the text is unchanged
  text.addEventListener("input", () => { checked = null; out.classList.add("hidden"); if (go) go.textContent = "Next"; });
  modal({ title: "Add links", body: [h("div", { class: "muted" }, "Paste share links to download them whole. You'll see what they take before anything is queued."), text, out],
    actions: [{ label: "Cancel" }, { label: "Next", kind: "primary", ref: (b) => { go = b; }, run: async () => {
      if (!checked) {
        try { checked = await api("/jobs/plan", { body: { text: text.value } }); }
        catch (e) { put(out, h("div", { class: "fstate bad" }, e.code === "no_links" ? "No usn:// links in that text." : cap(e.message))); out.classList.remove("hidden"); return false; }
        if (checked.bad.length) {   // say which lines, and why, before going on
          const n = checked.items.length;
          put(out, h("div", { class: "note bad" }, h("b", {}, `${checked.bad.length} line${checked.bad.length === 1 ? "" : "s"} can't be used:`),
            checked.bad.map((b) => h("div", { class: "badline" }, h("span", { class: "mono" }, `line ${b.line}: ${b.text}`), h("div", {}, cap(b.reason))))),
            n ? h("div", { class: "muted small" }, `${n} link${n === 1 ? " is" : "s are"} fine.`) : null);
          out.classList.remove("hidden");
          if (!n) { checked = null; return false; }
          go.textContent = `Continue with ${n} link${n === 1 ? "" : "s"}`;
          return false;
        }
      }
      const links = checked.items.map((it) => it.link);
      if (links.length === 1) {
        setTimeout(async () => { const s = await prepareShare({ link: links[0] }, checked.items[0].title).catch((e) => { fail(e); return null; }); if (s) reviewDownload(s, null).catch(fail); }, 0);
      } else setTimeout(() => reviewShares({ links }).catch(fail), 0);
    } }] });
}

// pause / resume: from the Activity page, the status bar and the tray
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

// a job that failed for a provider reason: where to fix it
const providerTrouble = (d) => /username or password|can't find the server|refused the connection|no answer from|SSL|did not greet|turned the connection away|Usenet provider/i.test(d || "");

let pendingActivityAction = null;   // "add": open Add links once the page is shown
async function pageActivity() {
  setTitle("Activity");
  const pauseBtn = h("button", { class: "btn" }, icon("pause"), "Pause all");
  toolbar(pauseBtn, h("button", { class: "btn primary", onclick: addLinksDialog }, icon("plus"), "Add links"));
  const banner = h("div", { class: "hidden" });
  const qbox = h("div", { class: "panel" }), hbox = h("div", { class: "tablewrap" });
  $("#view").append(h("div", { class: "stack" }, banner, h("div", {}, h("h2", { style: { fontSize: "15px", margin: "0 0 10px" } }, "Queue"), qbox),
    h("div", {}, h("h2", { style: { fontSize: "15px", margin: "6px 0 10px" } }, "History"), hbox)));

  function renderThrottle(th) {
    th = th || {};
    put(pauseBtn, th.paused ? [icon("play"), "Resume all"] : [icon("pause"), "Pause all"]);
    pauseBtn.onclick = () => (th.paused ? resumeAll() : pausePrompt()).catch(fail);
    banner.classList.toggle("hidden", !th.paused && !th.limit);
    put(banner, th.paused
      ? h("div", { class: "note warn bar-note" }, icon("pause"), h("span", { class: "grow" }, `${throttleText(th)}. Downloads wait in the queue and carry on when resumed.`),
        h("button", { class: "btn sm", onclick: () => resumeAll().catch(fail) }, icon("play"), "Resume now"))
      : th.limit ? h("div", { class: "note bar-note" }, icon("gauge"), h("span", { class: "grow" }, `${throttleText(th)}.`), h("a", { href: "#/settings" }, "Speed settings")) : null);
  }

  const act = (fn) => async () => { try { await fn(); refresh(); } catch (e) { fail(e); } };
  let dragging = null, moved = null;
  const commitOrder = () => api("/jobs/reorder", { body: { ids: [...qbox.querySelectorAll(".job")].map((e) => e.dataset.id) } }).then(refresh).catch(fail);
  async function refresh() {
    if (dragging) return;   // a re-render would drop the row being dragged
    let d; try { d = await api("/jobs"); } catch { return; }
    if (dragging) return;
    renderThrottle(d.throttle);
    const ids = d.queue.map((x) => x.job_id);
    put(qbox, ...(d.queue.length ? d.queue.map((j, idx) => {
      const p = pct(j.bytes, j.size);
      const waiting = j.state === "queued" || j.state === "paused";
      const fp = j.file_pos && j.file_pos[1] > 1 ? `file ${j.file_pos[0]}/${j.file_pos[1]}` : "";
      const row = h("div", { class: "job" + (moved === j.job_id ? " moved" : ""), draggable: "true", "data-id": j.job_id,
        ondragstart: (ev) => { dragging = row; ev.dataTransfer.effectAllowed = "move"; ev.dataTransfer.setData("text/plain", j.job_id); setTimeout(() => row.classList.add("dragging"), 0); },
        ondragend: () => { row.classList.remove("dragging"); if (dragging) { dragging = null; moved = j.job_id; setTimeout(() => { moved = null; }, 2500); commitOrder(); } },
        ondragover: (ev) => {
          if (!dragging || dragging === row) return;
          ev.preventDefault();
          const r = row.getBoundingClientRect();
          qbox.insertBefore(dragging, ev.clientY > r.top + r.height / 2 ? row.nextSibling : row);
        },
        ondrop: (ev) => ev.preventDefault() },
        h("div", { class: "l1" }, h("span", { class: "handle", title: "Drag to change the order" }, icon("drag")), stateChip(j.state),
          h("span", { class: "nm", title: jobLabel(j) }, j.name, j.picked ? h("span", { class: "picked" }, " — " + j.picked) : null), h("span", { class: "chip plain" }, j.category),
          h("span", { class: "grow" }),
          idx > 0 ? h("button", { class: "btn sm ghost icon", title: "Move to the top of the queue", onclick: act(() => { moved = j.job_id; setTimeout(() => { moved = null; }, 2500); return api("/jobs/reorder", { body: { ids: [j.job_id, ...ids.filter((x) => x !== j.job_id)] } }); }) }, icon("up")) : null,
          j.state === "paused" ? h("button", { class: "btn sm", title: "Let this download carry on", onclick: act(() => api(`/jobs/${j.job_id}/resume`, { body: {} })) }, icon("play"), "Resume")
            : j.state !== "stopping" ? h("button", { class: "btn sm", title: "Hold this download in the queue; it carries on from where it was when resumed", onclick: act(() => api(`/jobs/${j.job_id}/pause`, { body: {} })) }, icon("pause"), "Pause") : null,
          h("button", { class: "btn sm ghost icon", title: "More", onclick: (ev) => menu(ev.currentTarget, [
            ["Stop and move to History", act(() => api(`/jobs/${j.job_id}/stop`, { body: {} }))],
            ["Delete (files stay)", act(async () => { if (await confirmBox("Delete job", `Delete “${jobLabel(j)}”? Downloaded files stay on disk.`, "Delete", "danger solid")) await api(`/jobs/${j.job_id}`, { method: "DELETE" }); }), "danger"],
          ]) }, icon("more"))),
        waiting ? h("div", { class: "l3" }, h("span", {}, j.state === "paused" ? "Paused" : idx === 0 ? "Next up" : "Waiting"),
            j.size ? h("span", {}, h("b", {}, fmtBytes(j.size)), j.bytes ? ` · ${fmtBytes(j.bytes)} already done` : "") : h("span", { class: "faint" }, "size known once it starts"),
            j.detail && j.detail !== "paused" ? h("span", { class: "faint" }, j.detail) : null)
          : [h("div", { class: "bar" + (p >= 100 ? " done" : "") }, h("i", { style: { width: p + "%" } })),
            h("div", { class: "l3" }, h("span", {}, h("b", {}, p.toFixed(1) + "%"), ` · ${fmtBytes(j.bytes)} of ${fmtBytes(j.size)}`),
              h("span", {}, h("b", {}, fmtRate(j.rate))), h("span", {}, "ETA ", h("b", {}, fmtEta(j.eta))),
              j.phase && j.phase !== j.state ? h("span", {}, j.phase) : null, fp ? h("span", {}, fp) : null,
              j.current ? h("span", { class: "faint", title: j.current }, j.current) : null,
              j.missing ? h("span", { class: "chip warn" }, `${j.missing} missing`) : null, j.detail ? h("span", { class: "faint" }, j.detail) : null)]);
      return row;
    }) : [emptyState("activity", "Nothing downloading", "Queue shares from the catalog, a share page, or Add links.")]));
    put(hbox, d.history.length ? h("table", { class: "t" },
      h("thead", {}, h("tr", {}, h("th", {}, "Name"), h("th", {}, "State"), h("th", {}, "Detail"), h("th", { class: "right" }, "Size"), h("th", {}, "Finished"), h("th", { class: "act" }, ""))),
      h("tbody", {}, d.history.map((j) => h("tr", {},
        h("td", { class: "title-cell" }, h("div", { class: "t1" }, j.name, j.picked ? h("span", { class: "picked" }, " — " + j.picked) : null),
          h("div", { class: "t2" }, h("span", { class: "chip plain" }, j.category),
            j.out_dir ? h("a", { class: "faint mono path", href: "#", title: "Copy the folder path: " + j.out_dir, onclick: (ev) => { ev.preventDefault(); copyText(j.out_dir); } }, j.out_dir) : null)),
        h("td", { class: "nowrap" }, stateChip(j.state), j.warnings && j.warnings.length ? [" ", h("span", { class: "chip warn", title: j.warnings.join("\n") }, `${j.warnings.length} warning${j.warnings.length === 1 ? "" : "s"}`)] : null),
        h("td", { class: "muted", style: { maxWidth: "380px" } }, j.detail || "",
          j.state === "failed" && providerTrouble(j.detail) ? h("div", {}, h("a", { href: "#/settings" }, "Check your Usenet provider settings"), ", then Retry.") : null),
        h("td", { class: "right nowrap" }, fmtBytes(j.size)), h("td", { class: "nowrap muted" }, ago(j.finished)),
        h("td", { class: "act" },
          j.state !== "completed" ? h("button", { class: "btn sm", title: j.state === "failed" ? "Try again: files already done are kept" : "Carry on from where it stopped", onclick: act(() => api(`/jobs/${j.job_id}/resume`, { body: {} })) }, icon("play"), j.state === "failed" ? "Retry" : "Resume") : null,
          " ", h("button", { class: "btn sm ghost icon", title: "More", onclick: (ev) => menu(ev.currentTarget, [
            j.out_dir ? ["Copy folder path", () => copyText(j.out_dir)] : null,
            ["Remove from history (files stay)", act(() => api(`/jobs/${j.job_id}`, { method: "DELETE" }))],
            ["Delete the files too…", act(async () => {
              if (await confirmBox("Delete files", `Delete “${jobLabel(j)}” and its folder${j.out_dir ? " " + j.out_dir : ""}? This can't be undone.`, "Delete files", "danger solid")) await api(`/jobs/${j.job_id}?files=1`, { method: "DELETE" });
            }), "danger"],
          ]) }, icon("more"))))))) : emptyState(null, null, "No finished jobs yet"));
  }
  page.onThrottle = refresh;
  page.onJobDone = () => refresh();
  every(1500, () => { if (!document.hidden && !$(".modal-back") && !$(".popmenu")) refresh(); });
  if (pendingActivityAction) { pendingActivityAction = null; addLinksDialog(); }
}

// ---------------------------------------------------------------- Usenet providers (Settings and setup)
const PRIORITY_HELP = "0 is the highest. Providers with the same priority download together, each up to its own connection count. A provider with a lower priority (a higher number) is only asked for articles the ones above it don't have: use that for a block account.";
const sw = (checked) => { const i = h("input", { type: "checkbox", checked }); return [i, h("label", { class: "switch" }, i, h("span"))]; };
const field = (label, input, help) => [h("label", { class: "k" }, label), input, help ? h("div", { class: "help" }, help) : null];
const testMsg = (r) => cap(r.message || "");

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
  // the first provider needs only the basics; priority, name and on/off are there for more
  const more = h("details", { class: "more full", open: !isNew }, h("summary", {}, "More options: priority, name, on / off"),
    h("div", { class: "form" }, field("Priority", f.priority, PRIORITY_HELP), field("Name", f.name), field("Enabled", enSw)));
  modal({ title: isNew ? "Add a Usenet provider" : `Edit ${a.name || a.server}`, body: h("div", { class: "form" },
    field("Server", f.server), field("Port", f.port), field("SSL", sslSw, "Encrypted connection; most providers use port 563 for it."),
    field("Username", f.username), field("Password", f.password), field("Connections", f.connections, "As many as your plan allows; keep the total across providers within each plan."),
    more, h("div", { class: "full" }, result)),
  actions: [{ label: "Test connection", run: async () => {
    result.textContent = "connecting…"; result.className = "muted";
    const r = await api("/settings/test-account", { body: collect() }).catch((e) => ({ ok: false, message: e.message }));
    result.textContent = testMsg(r); result.className = r.ok ? "note ok-note" : "note bad"; return false;
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
          st ? h("div", { class: st.ok ? "test ok" : st.ok === false ? "test bad" : "test" }, testMsg(st)) : null),
        h("td", {}, a.username || h("span", { class: "faint" }, "—")), h("td", { class: "right" }, a.connections),
        h("td", {}, prioChip(a, list)), h("td", {}, enSw),
        h("td", { class: "act" }, h("button", { class: "btn sm", onclick: async () => {
          status[a.id] = { message: "connecting…" }; onToggle(a, a.enabled, true);
          status[a.id] = await testProvider(a); onToggle(a, a.enabled, true);
        } }, "Test"), " ",
        h("button", { class: "btn sm ghost icon", title: "Edit", onclick: () => onEdit(a) }, icon("edit")),
        h("button", { class: "btn sm ghost icon danger", title: "Remove", onclick: () => onRemove(a) }, icon("trash"))));
    }))));
}
const testProvider = (a) => api("/settings/test-account", { body: a }).catch((e) => ({ ok: false, message: e.message }));

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

// folderPicker: browse this machine's folders (on the machine running usnfetch) and pick one
function folderPicker(start, onPick) {
  const pathIn = h("input", { class: "input mono", value: start || "", placeholder: "a full path, or pick below", onkeydown: (ev) => { if (ev.key === "Enter") { ev.preventDefault(); ev.stopPropagation(); go(pathIn.value.trim()); } } });
  const list = h("div", { class: "fpick" }), err = h("div", { class: "fstate bad hidden" });
  let cur = "", useBtn = null;
  async function go(p) {
    let r;
    try { r = await api("/fs/folders?path=" + encodeURIComponent(p || "")); }
    catch (e) { err.textContent = cap(e.message); err.classList.remove("hidden"); if (p) return go(""); return; }
    err.classList.add("hidden");
    cur = r.path; pathIn.value = r.path;
    if (useBtn) useBtn.disabled = !cur;
    put(list, r.path ? h("button", { class: "fi up", type: "button", onclick: () => go(r.parent || "") }, icon("up"), r.parent ? "Up to " + r.parent : "All drives") : null,
      r.folders.map((d) => h("button", { class: "fi", type: "button", onclick: () => go(d.path) }, icon(r.path ? "folder" : "server"), d.name)),
      !r.folders.length ? h("div", { class: "faint small", style: { padding: "8px" } }, "No folders in here.") : null,
      r.free != null ? h("div", { class: "faint small fifree" }, fmtBytes(r.free) + " free on this drive") : null);
  }
  modal({ title: "Choose a folder", body: [pathIn, err, list], actions: [{ label: "Cancel" },
    { label: "Use this folder", kind: "primary", ref: (b) => { useBtn = b; }, run: () => { if (!cur) return false; onPick(cur); } }] });
  go(start || "");
}
const browseBtn = (input, after) => h("button", { class: "btn", type: "button", onclick: () => folderPicker(input.value.trim(), (p) => { input.value = p; input.dispatchEvent(new Event("input")); input.dispatchEvent(new Event("change")); after && after(p); }) }, icon("folder"), "Browse…");

// how shares get into usnfetch: the setup's last step and the empty catalog
let pendingCatalogAction = null;   // "import" | "link": opened by the catalog once it is shown
function addSharesHelp(compact) {
  const item = (ic, title, text, btn) => h("div", { class: "howto" }, h("div", { class: "hi" }, icon(ic)),
    h("div", {}, h("div", { class: "ht" }, title), h("div", { class: "muted" }, text), btn || null));
  const goCatalog = (what) => () => { pendingCatalogAction = what; if (location.hash === "#/catalog") route(); else location.hash = "#/catalog"; };
  return h("div", { class: "howtos" + (compact ? " compact" : "") },
    item("upload", "Import a link list", "Catalog → Import list. Choose a file or paste text: any text with usn:// links works, such as a plain list, a CSV, or a web page you saved. Exported lists also give each share its title, listed size and source.",
      h("button", { class: "btn primary sm", onclick: goCatalog("import") }, icon("upload"), "Import a list")),
    item("link", "Open one link", "Catalog → Open link. Paste a usn:// link to browse that share, check that it is complete, and pick the files you want.",
      h("button", { class: "btn sm", onclick: goCatalog("link") }, icon("link"), "Open a link")),
    item("download", "Download links straight away", "Activity → Add links downloads one or more shares in full, without browsing them first.",
      h("button", { class: "btn sm", onclick: () => { pendingActivityAction = "add"; if (location.hash === "#/activity") route(); else location.hash = "#/activity"; } }, icon("plus"), "Add links")));
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
    const listBox = h("div"), hint = h("span", { class: "faint small" });
    const next = h("button", { class: "btn primary", onclick: async () => {
      const bad = info.accounts.filter((a) => a.enabled && status[a.id] && status[a.id].ok === false);
      const why = bad.map((a) => testMsg(status[a.id]).replace(/\.?$/, ".")).join(" ");
      if (bad.length && !(await confirmBox("The connection test failed", `${why} Downloads will fail until that is fixed. Continue anyway?`, "Continue anyway"))) return;
      go("folders");
    } }, "Next");
    const save = async (accounts) => { const sv = await api("/settings", { method: "PUT", body: { accounts } }); info.accounts = sv.accounts; render(); return sv.accounts; };
    const testNew = async (a) => {
      status[a.id] = { message: "connecting…" }; render();
      status[a.id] = await testProvider(a); render();
    };
    const sabCallout = !!info.sab_ini && !info.accounts.length;
    const help = h("div", { class: "help" }, h("b", {}, "Priority: "), PRIORITY_HELP);
    function render() {
      const list = info.accounts;
      next.disabled = !list.some((a) => a.enabled);
      hint.textContent = next.disabled ? (list.length ? "Turn on a provider to continue" : "Add a provider to continue, or set up later") : "";
      help.classList.toggle("hidden", list.length < 2);
      put(listBox, list.length
        ? [providersTable(list, {
            status,
            onEdit: (a) => providerDialog(a, async (p) => { await save(list.map((x) => (x.id === a.id ? p : x))); testNew(info.accounts.find((x) => x.id === a.id)); }),
            onRemove: async (a) => { if (await confirmBox("Remove provider", `Remove ${a.name || a.server}?`, "Remove", "danger solid")) save(list.filter((x) => x.id !== a.id)).catch(fail); },
            onToggle: (a, on, redraw) => { if (redraw) return render(); save(list.map((x) => (x.id === a.id ? { ...x, enabled: on } : x))).catch(fail); } }),
          h("div", { class: "muted small", style: { marginTop: "8px" } }, providersSummary(list))]
        : emptyState("server", null, "No provider yet."));
    }
    render();
    put(card, h("div", { class: "body" }, h("h2", {}, "Your Usenet provider"),
      h("p", { class: "muted" }, "usnfetch downloads from your own Usenet account, the way SABnzbd does. Add the provider you use; add more if you have them, for example a block account at a lower priority."),
      sabCallout ? h("div", { class: "note callout" }, icon("server"),
        h("span", { class: "grow" }, "SABnzbd is set up on this computer. Import its servers, with their passwords and priorities?"),
        h("button", { class: "btn primary sm", onclick: async () => {
          try { const r = await api("/settings/import-sab", { body: { path: info.sab_ini } }); info.accounts = r.settings.accounts; toast(`${r.added} provider${r.added === 1 ? "" : "s"} imported`, "ok"); route(); for (const a of info.accounts) testNew(a); } catch (e) { fail(e); }
        } }, "Import from SABnzbd")) : null,
      listBox,
      h("div", { class: "row-btns" },
        h("button", { class: "btn", onclick: () => providerDialog(null, async (p) => { const before = new Set(info.accounts.map((a) => a.id)); const all = await save([...info.accounts, p]); const added = all.find((a) => !before.has(a.id)); if (added) testNew(added); }) }, icon("plus"), "Add a provider"),
        sabCallout ? null : h("button", { class: "btn ghost", onclick: () => sabImportDialog(info.sab_ini, (sv) => { info.accounts = sv.accounts; render(); for (const a of info.accounts) if (!status[a.id]) testNew(a); }) }, "Import from SABnzbd…")),
      help,
      foot(h("button", { class: "btn ghost", onclick: () => go("welcome") }, icon("back"), "Back"), h("button", { class: "btn ghost", onclick: () => later().catch(fail) }, "Set up later"),
        h("span", { class: "grow" }), hint, next)));
    return;
  }

  if (step === "folders") {
    const dl = h("input", { class: "input", value: info.complete_dir || "" });
    const ix = h("input", { class: "input", value: info.index_dir || "" });
    const dlState = h("div", { class: "fstate" }), ixState = h("div", { class: "fstate" });
    const finish = h("button", { class: "btn primary" }, "Finish setup");
    const check = async (input, out, optional) => {
      const v = input.value.trim();
      if (!v && optional) { out.className = "fstate"; out.textContent = "Empty: kept in the data folder, " + info.index_dir_default; return true; }
      const r = await api("/setup/folder", { body: { path: v } }).catch((e) => ({ ok: false, error: e.message }));
      out.className = "fstate " + (r.ok ? "ok" : "bad");
      out.textContent = r.ok ? `${r.exists ? "Folder exists" : "Will be created"}${r.free != null ? " · " + fmtBytes(r.free) + " free" : ""}` : cap(r.error);
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
      h("div", { class: "form" }, h("label", { class: "k" }, "Download folder"), h("div", {}, h("div", { class: "inline nowrap-row" }, dl, browseBtn(dl)), dlState),
        h("div", { class: "help" }, "Each download gets a folder of its own in here (inside a category folder, \"Default\" unless you choose another). Shares can be very large, so pick a drive with room.")),
      h("details", { class: "more" }, h("summary", {}, "Advanced: where share indexes are kept"),
        h("div", { class: "form" }, h("label", { class: "k" }, "Share index folder"), h("div", {}, h("div", { class: "inline nowrap-row" }, ix, browseBtn(ix)), ixState),
          h("div", { class: "help" }, "Working files, about 200 MB per TB of share, kept while a share is open or its download can resume. Leave it empty to keep them in the data folder."))),
      foot(h("button", { class: "btn ghost", onclick: () => go("providers") }, icon("back"), "Back"), h("span", { class: "grow" }), finish)));
    return;
  }

  // done
  put(card, h("div", { class: "body" }, h("h2", {}, "All set: now add some shares"),
    h("p", { class: "muted" }, "usnfetch doesn't come with any shares. It opens the share links you already have, the same way the official app does:"),
    addSharesHelp(false),
    h("p", { class: "muted small" }, "Providers, folders and everything else can be changed later in Settings."),
    foot(h("span", { class: "grow" }), h("button", { class: "btn", onclick: () => { location.hash = "#/catalog"; } }, "Go to the catalog"))));
}

// ---------------------------------------------------------------- settings
// Settings save as they change (no Save buttons to forget); each panel says when it saved.
async function pageSettings() {
  setTitle("Settings");
  let s = await api("/settings");
  const view = $("#view");
  toolbar(h("a", { class: "btn", href: "#/setup" }, "Run setup again"));
  const panel = (id, ic, title, extra, ...body) => {
    const st = h("span", { class: "saved faint" });
    const p = h("div", { class: "panel", id: "set-" + id }, h("div", { class: "head" }, icon(ic), h("h2", {}, title), h("span", { class: "grow" }), st, extra), h("div", { class: "body" }, ...body));
    p.saved = st;
    return p;
  };
  // saveOn: save some settings now, and say so in the panel
  async function saveOn(p, body) {
    p.saved.textContent = "saving…"; p.saved.className = "saved faint";
    try {
      s = await api("/settings", { method: "PUT", body });
      p.saved.textContent = "✓ saved"; p.saved.className = "saved ok";
      clearTimeout(p.savedT); p.savedT = setTimeout(() => { p.saved.textContent = ""; }, 2500);
      return true;
    } catch (e) { p.saved.textContent = "not saved"; p.saved.className = "saved bad"; fail(e); return false; }
  }

  // Usenet providers: edits are tested as soon as they are saved
  const accBody = h("div");
  const accStatus = {};
  async function saveAccounts(accounts, test) {   // test: the id of an edited provider, or "new"
    const before = new Set(s.accounts.map((x) => x.id));
    s = await api("/settings", { method: "PUT", body: { accounts } }); renderAccounts();
    const a = test === "new" ? s.accounts.find((x) => !before.has(x.id)) : test ? s.accounts.find((x) => x.id === test) : null;
    if (a) { accStatus[a.id] = { message: "testing the connection…" }; renderAccounts(); accStatus[a.id] = await testProvider(a); renderAccounts(); }
  }
  function renderAccounts() {
    put(accBody, s.accounts.length
      ? [providersTable(s.accounts, { status: accStatus,
          onEdit: (a) => providerDialog(a, (p) => saveAccounts(s.accounts.map((x) => (x.id === a.id ? p : x)), a.id)),
          onRemove: async (a) => { if (await confirmBox("Remove provider", `Remove ${a.name || a.server}?`, "Remove", "danger solid")) saveAccounts(s.accounts.filter((x) => x.id !== a.id)).catch(fail); },
          onToggle: (a, on, redraw) => { if (redraw) return renderAccounts(); saveAccounts(s.accounts.map((x) => (x.id === a.id ? { ...x, enabled: on } : x))).catch(fail); } }),
        h("div", { class: "muted", style: { marginTop: "10px", fontSize: "12.5px" } }, providersSummary(s.accounts))]
      : h("div", { class: "note warn" }, "No Usenet provider yet: add one, import them from SABnzbd, or ", h("a", { href: "#/setup/providers" }, "run the setup"), "."));
  }
  const accounts = panel("providers", "server", "Usenet providers", [
    h("button", { class: "btn", onclick: () => sabImportDialog(s.sab_ini, (sv) => { s = sv; renderAccounts(); }) }, "Import from SABnzbd"),
    h("button", { class: "btn", onclick: () => providerDialog(null, (p) => saveAccounts([...s.accounts, p], "new")) }, icon("plus"), "Add provider")],
    accBody, h("div", { class: "help", style: { marginTop: "10px" } }, h("b", {}, "Priority: "), PRIORITY_HELP));

  // downloads
  const d = { complete_dir: h("input", { class: "input", value: s.complete_dir }), categories: h("input", { class: "input", value: (s.categories || []).join(", ") }),
    missing_retries: h("input", { class: "input", type: "number", min: 0, max: 10, value: s.missing_retries, style: { width: "110px" } }),
    max_parallel_jobs: h("input", { class: "input", type: "number", min: 1, max: 8, value: s.max_parallel_jobs, style: { width: "110px" } }),
    stream_connections: h("input", { class: "input", type: "number", min: 1, max: 64, value: s.stream_connections, style: { width: "110px" } }),
    index_dir: h("input", { class: "input", value: s.index_dir || "", placeholder: "empty: index_cache in the data folder" }),
    sevenzip_path: h("input", { class: "input", value: s.sevenzip_path || "", placeholder: "empty: found by itself" }), unrar_path: h("input", { class: "input", value: s.unrar_path || "", placeholder: "empty: found by itself" }) };
  const sws = { verify: sw(s.verify), extract: sw(s.extract), delete_archives: sw(s.delete_archives), extract_standalone: sw(s.extract_standalone) };
  const toolLine = () => h("div", { class: "help tool" });
  const tools = { sevenzip_path: toolLine(), unrar_path: toolLine() };
  const showTools = () => {
    const t = s.tools || {};
    tools.sevenzip_path.replaceChildren(t.sevenzip ? h("span", { class: "ok-text" }, "Found: " + t.sevenzip) : h("span", { class: "bad-text" }, "Not found: .zip / .7z sets can't be extracted. Install 7-Zip or give its path."));
    tools.unrar_path.replaceChildren(t.unrar ? h("span", { class: "ok-text" }, "Found: " + t.unrar) : h("span", { class: "faint" }, "Not found: 7-Zip is used for RAR archives instead."));
  };
  showTools();
  const downloads = panel("downloads", "download", "Downloads", null,
    h("div", { class: "form" },
      h("label", { class: "k" }, "Download folder"), h("div", { class: "inline nowrap-row" }, d.complete_dir, browseBtn(d.complete_dir)), h("div", { class: "help" }, "Jobs land in <folder>/<category>/<job name>."),
      ...field("Categories", d.categories, "Comma separated."),
      ...field("Verify SHA-256", sws.verify[1], "Hash each file against the share's index when it completes."),
      ...field("Extract archives", sws.extract[1], "RAR archives and multi-part sets (.z01, .zip.001, .7z.001). Each download can choose otherwise when you queue it."),
      ...field("Also single .zip / .7z", sws.extract_standalone[1], "Off keeps them zipped: ROM, BIOS and asset packs are meant to stay that way."),
      ...field("Delete archives after", sws.delete_archives[1], "Only for sets that extracted cleanly."),
      ...field("Parallel jobs", d.max_parallel_jobs, "How many downloads run at once.")),
    h("details", { class: "more" }, h("summary", {}, "Advanced"),
      h("div", { class: "form" },
        ...field("Missing-article retries", d.missing_retries, "Retry passes (1, 2, 3… minutes apart) for articles missing on every account."),
        ...field("Streaming connections", d.stream_connections, "Used when opening files straight from Usenet in the browser, on top of download connections."),
        h("label", { class: "k" }, "Share index folder"), h("div", { class: "inline nowrap-row" }, d.index_dir, browseBtn(d.index_dir)), h("div", { class: "help" }, "About 200 MB per TB of share while a share is open or a job can resume."),
        h("label", { class: "k" }, "7-Zip"), d.sevenzip_path, tools.sevenzip_path,
        h("label", { class: "k" }, "UnRAR"), d.unrar_path, tools.unrar_path)));
  const nums = new Set(["missing_retries", "max_parallel_jobs", "stream_connections"]);
  for (const [k, el] of Object.entries(d)) el.addEventListener("change", async () => {
    if (await saveOn(downloads, { [k]: nums.has(k) ? +el.value : el.value })) {
      if (k === "categories") el.value = (s.categories || []).join(", ");
      if (k === "sevenzip_path" || k === "unrar_path") showTools();
    }
  });
  for (const [k, [el]] of Object.entries(sws)) el.addEventListener("change", () => saveOn(downloads, { [k]: el.checked }));

  // web access
  const cur = h("input", { class: "input", type: "password", autocomplete: "current-password", placeholder: "Current password" });
  const nw = h("input", { class: "input", type: "password", autocomplete: "new-password", placeholder: "New password (8+ characters)" });
  const nw2 = h("input", { class: "input", type: "password", autocomplete: "new-password", placeholder: "New password again" });
  const port = location.port || (location.protocol === "https:" ? "443" : "80");
  const web = panel("web", "shield", "Web access", s.web.has_password ? h("span", { class: "chip ok" }, "password set") : h("span", { class: "chip warn" }, "no password"),
    h("div", { class: "form" },
      h("div", { class: "full note" }, s.web.has_password ? "Other computers can log in with this password." : ["Without a password only this machine can use the web app. Set one to reach it from other computers — start it with ", h("span", { class: "mono" }, "usnfetch serve --host 0.0.0.0"), ` and open http://<this machine>:${port}/ there.`]),
      s.web.has_password ? field("Current password", cur) : null, field("New password", nw), field("Confirm", nw2),
      h("div", {}), h("div", {}, h("button", { class: "btn", onclick: async () => {
        if (nw.value !== nw2.value) return toast("The new passwords differ", "bad");
        try { await api("/settings/web-password", { body: { current: cur.value, new: nw.value } }); toast(nw.value ? "Password set" : "Password removed", "ok"); route(); } catch (e) { fail(e); }
      } }, s.web.has_password ? "Change password" : "Set password"))));

  // speed limit and schedule: saved as they change
  const limit = h("input", { class: "input", type: "number", min: 0, step: "0.5", value: s.speed_limit_mbps || "", placeholder: "no limit", style: { width: "140px" } });
  let rules = (s.schedule || []).map((r) => ({ ...r, days: [...(r.days || [])] }));
  const rulesBox = h("div", { class: "rules" });
  const DAYS = [[1, "Mo"], [2, "Tu"], [3, "We"], [4, "Th"], [5, "Fr"], [6, "Sa"], [0, "Su"]];
  let ruleT = null;
  const saveRules = () => { clearTimeout(ruleT); ruleT = setTimeout(async () => { if (await saveOn(speed, { schedule: rules })) pollStatus(); }, 500); };
  function ruleRow(r, i) {
    const [en, enSw] = sw(r.enabled); en.onchange = () => { r.enabled = en.checked; saveRules(); };
    const from = h("input", { class: "input", type: "time", value: r.from, onchange: () => { r.from = from.value; saveRules(); } });
    const to = h("input", { class: "input", type: "time", value: r.to, onchange: () => { r.to = to.value; saveRules(); } });
    const action = h("select", { class: "input", onchange: () => { r.action = action.value; renderRules(); saveRules(); } },
      h("option", { value: "limit", selected: r.action === "limit" }, "Limit to"), h("option", { value: "pause", selected: r.action === "pause" }, "Pause"));
    const mb = h("input", { class: "input", type: "number", min: "0.5", step: "0.5", value: r.limit_mbps || 10, style: { width: "84px" }, onchange: () => { r.limit_mbps = +mb.value; saveRules(); } });
    return h("div", { class: "rule" }, enSw,
      h("div", { class: "days", title: "No day picked = every day" }, DAYS.map(([d, n]) => h("button", { class: "day" + (r.days.includes(d) ? " on" : ""), "aria-pressed": r.days.includes(d) ? "true" : "false",
        onclick: () => { r.days = r.days.includes(d) ? r.days.filter((x) => x !== d) : [...r.days, d]; renderRules(); saveRules(); } }, n))),
      from, h("span", { class: "muted" }, "to"), to, action, r.action === "limit" ? [mb, h("span", { class: "muted" }, "MB/s")] : null,
      h("span", { class: "grow" }),
      h("button", { class: "btn ghost icon danger", title: "Remove this rule", onclick: () => { rules.splice(i, 1); renderRules(); saveRules(); } }, icon("trash")));
  }
  function renderRules() {
    put(rulesBox, rules.length ? rules.map(ruleRow) : h("div", { class: "faint" }, "No schedule: downloads use the limit above all the time."),
      h("div", {}, h("button", { class: "btn sm", onclick: () => { rules.push({ enabled: true, days: [1, 2, 3, 4, 5], from: "08:00", to: "18:00", action: "limit", limit_mbps: 10 }); renderRules(); saveRules(); } }, icon("plus"), "Add a rule")));
  }
  const speed = panel("speed", "gauge", "Speed & schedule", null,
    h("div", { class: "form" },
      h("label", { class: "k" }, "Speed limit"), h("div", { class: "inline" }, limit, h("span", { class: "muted" }, "MB/s for downloads (empty = as fast as the connections go)")),
      h("div", { class: "help" }, "Opening files in the browser is not limited."),
      h("label", { class: "k" }, "Schedule"), h("div", {}, h("div", { class: "muted", style: { fontSize: "12.5px", marginBottom: "8px" } }, "The first rule that covers the current time wins. A rule from 23:00 to 07:00 runs past midnight."), rulesBox)));
  limit.addEventListener("change", async () => { if (await saveOn(speed, { speed_limit_mbps: +limit.value || 0 })) pollStatus(); });
  renderRules();

  // appearance: saved in this browser, applied at once
  const appearance = h("div", { class: "panel", id: "set-appearance" });
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
  const jump = h("nav", { class: "jumps" }, [["providers", "Usenet providers"], ["downloads", "Downloads"], ["speed", "Speed & schedule"], ["web", "Web access"], ["appearance", "Appearance"]]
    .map(([id, label]) => h("a", { href: "#", onclick: (ev) => { ev.preventDefault(); $("#set-" + id).scrollIntoView({ behavior: "smooth", block: "start" }); } }, label)));
  view.append(jump, h("div", { class: "stack" }, accounts, downloads, speed, web, appearance));
  renderAccounts();
}

// ---------------------------------------------------------------- system
async function pageSystem() {
  setTitle("System");
  const cards = h("div", { class: "cards" });
  const logs = h("div", { class: "loglines tall" });
  const follow = h("input", { type: "checkbox", checked: true });
  let all = [];   // the log lines so far (the box shows those matching the filter)
  const filter = h("input", { class: "input", type: "search", placeholder: "Filter the log…", oninput: () => showLogs(true) });
  const showLogs = (redo, add) => {
    const f = filter.value.trim().toLowerCase();
    const pick = (ls) => (f ? ls.filter((l) => l.toLowerCase().includes(f)) : ls);
    if (redo) logs.textContent = pick(all).join("\n");
    else if (add && add.length) { const ls = pick(add); if (ls.length) logs.append(document.createTextNode((logs.textContent ? "\n" : "") + ls.join("\n"))); }
    if (follow.checked) logs.scrollTop = logs.scrollHeight;
  };
  const restartBtn = h("button", { class: "btn hidden", title: "Stop and start usnfetch again; downloads carry on where they were", onclick: async () => {
    if (!(await confirmBox("Restart usnfetch", "usnfetch stops and starts again. Downloads pause for a moment and carry on where they were.", "Restart"))) return;
    try { await api("/system/restart", { body: {} }); } catch (e) { fail(e); return; }
    toast("Restarting…");
    await new Promise((r) => setTimeout(r, 2500));
    for (let i = 0; i < 40; i++) {      // back up? then reload
      try { const r = await fetch("/api/session", { credentials: "same-origin" }); if (r.ok) { location.reload(); return; } } catch { /* not yet */ }
      await new Promise((r) => setTimeout(r, 750));
    }
    toast("usnfetch has not come back yet; check the tray icon", "bad");
  } }, icon("refresh"), "Restart");
  toolbar(restartBtn);
  $("#view").append(cards, h("div", { class: "panel" }, h("div", { class: "head" }, icon("system"), h("h2", {}, "Log"), h("span", { class: "grow" }),
    h("div", { class: "search" }, icon("search"), filter), h("label", { class: "check" }, follow, "Follow"),
    h("button", { class: "btn sm", title: "Copy the lines shown", onclick: () => copyText(logs.textContent) }, icon("copy"), "Copy")),
  h("div", { class: "body" }, logs)));
  let seq = 0;
  every(2000, async () => {
    try {
      const st = await api("/status");
      const mem = await api("/debug/memory").catch(() => null);
      restartBtn.classList.toggle("hidden", !st.can_restart);
      put(cards,
        h("div", { class: "card" }, h("div", { class: "k" }, "Version"), h("div", { class: "v" }, st.version), h("div", { class: "s" }, st.runtime || ("Python " + st.python)),
          h("div", { class: "s credit" }, "Made by ", h("a", { href: "https://github.com/dothiggity", target: "_blank", rel: "noopener" }, "dothiggity"))),
        h("div", { class: "card" }, h("div", { class: "k" }, "Free space"), h("div", { class: "v" }, fmtBytes(st.disk.free)), h("div", { class: "s", title: st.disk.path }, st.disk.path)),
        h("div", { class: "card" }, h("div", { class: "k" }, "Jobs"), h("div", { class: "v" }, `${st.running} running`), h("div", { class: "s" }, `${st.active} in the queue · ${st.open_shares} shares open`)),
        h("div", { class: "card", title: "Go: usnfetch's own data. SQLite & other: the share indexes' working memory and the rest." },
          h("div", { class: "k" }, "Memory"), h("div", { class: "v" }, mem && mem.private ? fmtBytes(mem.private) : "—"),
          h("div", { class: "s" }, mem && mem.private ? `Go ${fmtBytes(mem.go_heap_in_use)} · SQLite & other ${fmtBytes(mem.other || 0)}` : "")),
        h("div", { class: "card wide" }, h("div", { class: "k" }, "Accounts"), h("div", { class: "v", style: { fontSize: "14px", whiteSpace: "normal" } }, st.accounts || "none configured"), h("div", { class: "s", title: st.data_dir }, "data: " + st.data_dir)));
      const l = await api("/logs?after=" + seq);
      if (l.lines.length) {
        all.push(...l.lines);
        if (all.length > 6000) { all = all.slice(-4000); showLogs(true); } else showLogs(false, l.lines);
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
  await pollStatus(); renderBasket();   // the first status before the first page (it says whether a provider is set up)
  window.addEventListener("storage", (ev) => { if (ev.key === "usnf.basket") { basket.all = store.get("basket", {}); renderBasket(); if (page.onBasket) page.onBasket(); } });
  if (start) location.hash = start;   // its hashchange routes, once
  else await route();
}
boot();
