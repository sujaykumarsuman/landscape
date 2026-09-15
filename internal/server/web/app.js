"use strict";
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
const esc = (s) => String(s == null ? "" : s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const api = (p, o) => fetch("./api" + p, Object.assign({ headers: { "Content-Type": "application/json" } }, o));
const cls = (st) => st === "ok" ? "ok" : st === "failed" ? "err" : "warn";

const state = {
  graph: null, metrics: null, view: "map", appName: null, app: null, traefik: null, pin: null,
  appTab: "graph", appEvents: null, appError: null, logs: null, events: null,
  eventsFilter: { ns: "", type: "", kind: "", q: "" },
  logsCtl: { container: "", tail: 500 },
  metricsCtl: { ns: "", sort: "mem", interval: 15000 },
};
let pollTimer = null;
let started = false;
const DEFAULT_POLL = 15000;

// window listeners bound once at load (startApp may run again after a re-login)
window.addEventListener("resize", () => { if (state.view === "app") drawAppEdges(); if (state.view === "traefik") drawTraefikEdges(); });
window.addEventListener("keydown", (e) => { if (e.key === "Escape") { if (state.pin) unpin(); closeMenu(); } });
// click outside the events menu closes it
document.addEventListener("click", (e) => { if (!e.target.closest("#evmenu")) closeMenu(); });
// browser back/forward: reflect the URL into the view (no new history entry)
window.addEventListener("popstate", () => {
  if (!started) return;
  const r = routeFromURL();
  applyRoute(r.view, r.appName);
  syncChrome();
  render();
  refresh();
});
// keep the hover card alive while the cursor is over it (so its links are clickable)
{
  const hv = document.querySelector("#hover");
  if (hv) { hv.addEventListener("mouseenter", cancelHide); hv.addEventListener("mouseleave", scheduleHide); }
}

init();
async function init() {
  try {
    const r = await api("/session");
    const j = await r.json();
    if (j.authed) startApp(); else showLogin();
  } catch (_) { showLogin(); }
}

/* ---------- auth ---------- */
function showLogin() {
  $("#app").classList.add("hide");
  const lg = $("#login"); lg.classList.remove("hide");
  $("#loginform").onsubmit = async (e) => {
    e.preventDefault();
    $("#loginerr").textContent = "";
    const r = await api("/login", { method: "POST", body: JSON.stringify({ password: $("#pw").value }) });
    if (r.ok) { lg.classList.add("hide"); startApp(); }
    else { $("#loginerr").textContent = "Wrong password"; $("#pw").value = ""; $("#pw").focus(); }
  };
}
async function startApp() {
  started = true;
  $("#login").classList.add("hide");
  $("#app").classList.remove("hide");
  $("#logout").onclick = async () => { await api("/logout", { method: "POST" }); location.reload(); };
  $$(".nav button").forEach(b => b.onclick = () => setView(b.dataset.view));
  $("#crumb .home").onclick = () => setView("map");
  // adopt the deep-linked URL as the initial view (e.g. /landscape/airlift)
  const r = routeFromURL();
  applyRoute(r.view, r.appName);
  history.replaceState(routeState(), "", pathFor(state.view, state.appName));
  wireMenu();
  syncChrome();
  render();
  await refresh();
  setPoll(DEFAULT_POLL);
}

/* ---------- client-side router (History API) ----------
   The UI is one page served under a path prefix (…/landscape/). Each view gets a
   real URL: the map at the mount root, apps at …/<app>, and the reserved words
   metrics|events|traefik at …/<word>. The base is the mount path (dirname of the
   current pathname), so links stay prefix-agnostic and work in local dev too. */
const RESERVED = { metrics: 1, events: 1, traefik: 1 };
function currentBase() { const p = location.pathname; return p.slice(0, p.lastIndexOf("/") + 1); }
function pathFor(view, appName) {
  const b = currentBase();
  if (view === "app") return b + encodeURIComponent(appName);
  if (RESERVED[view]) return b + view;
  return b; // map
}
function routeState() { return { view: state.view, appName: state.appName }; }
function routeFromURL() {
  const p = location.pathname;
  const seg = decodeURIComponent(p.slice(p.lastIndexOf("/") + 1));
  if (seg === "metrics" || seg === "events" || seg === "traefik") return { view: seg, appName: null };
  if (seg === "") return { view: "map", appName: null };
  return { view: "app", appName: seg };
}
// set view state (no history change, no render) — shared by nav + popstate
function applyRoute(view, appName) {
  state.view = view;
  state.appName = view === "app" ? appName : null;
  state.app = null;
  state.appError = null;
  state.traefik = null;
  state.appTab = "graph";
  state.appEvents = null;
  state.logs = null;
  state.pin = null;
  $("#hover").classList.add("hide");
  closeMenu();
  // a paused/altered metrics cadence shouldn't linger once we leave that view
  if (view !== "metrics" && state.metricsCtl.interval !== DEFAULT_POLL) {
    state.metricsCtl.interval = DEFAULT_POLL;
    setPoll(DEFAULT_POLL);
  }
}
// (re)build the background poll timer at the given cadence (0 = paused)
function setPoll(ms) {
  clearInterval(pollTimer);
  pollTimer = ms > 0 ? setInterval(refresh, ms) : null;
}
// navigate: change state, push a history entry, re-render + fetch
function navigate(view, appName) {
  const samePath = pathFor(view, appName) === (location.pathname);
  applyRoute(view, appName);
  if (!samePath) history.pushState(routeState(), "", pathFor(view, appName));
  else history.replaceState(routeState(), "", pathFor(view, appName));
  syncChrome();
  render();
  refresh();
}
function setView(v) { navigate(v, null); }
function openApp(name) { navigate("app", name); }
function openTraefik() { navigate("traefik", null); }

/* ---------- top-right events/logs menu ---------- */
function wireMenu() {
  const btn = $("#evmenubtn"), pop = $("#evmenupop");
  if (!btn || !pop) return;
  btn.onclick = (e) => { e.stopPropagation(); toggleMenu(); };
  btn.onkeydown = (e) => {
    if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
      e.preventDefault(); openMenu();
      const first = pop.querySelector('[role="menuitem"]'); if (first) first.focus();
    }
  };
  pop.querySelectorAll('[role="menuitem"]').forEach(mi => {
    mi.onclick = () => { closeMenu(); menuAction(mi.dataset.act); };
  });
  pop.onkeydown = (e) => { if (e.key === "Escape") { closeMenu(); btn.focus(); } };
}
function menuAction(a) {
  if (a === "warnings") state.eventsFilter = { ns: "", type: "Warning", kind: "", q: "" };
  state.events = null;
  setView("events");
}
function toggleMenu() { $("#evmenupop").classList.contains("hide") ? openMenu() : closeMenu(); }
function openMenu() {
  const pop = $("#evmenupop"), btn = $("#evmenubtn");
  if (pop) pop.classList.remove("hide");
  if (btn) btn.setAttribute("aria-expanded", "true");
}
function closeMenu() {
  const pop = $("#evmenupop"), btn = $("#evmenubtn");
  if (pop) pop.classList.add("hide");
  if (btn) btn.setAttribute("aria-expanded", "false");
}
function syncChrome() {
  const sub = state.view === "app" || state.view === "traefik";
  $(".nav").classList.toggle("hide", sub);
  $("#crumb").classList.toggle("hide", !sub);
  if (sub) $("#crumb .cur").textContent = state.view === "traefik" ? "Traefik" : (state.appName || "");
  $$(".nav button").forEach(b => b.classList.toggle("on", b.dataset.view === state.view));
}

let reqGen = 0;
async function refresh() {
  const gen = ++reqGen;
  try {
    const calls = [["metrics", api("/metrics")]];
    if (state.view === "app" && state.appName) {
      const n = encodeURIComponent(state.appName);
      calls.push(["app", api("/app/" + n)]);
      if (!state.graph) calls.push(["graph", api("/graph")]); // for the header, once
      if (state.appTab === "events") calls.push(["ev", api("/app/" + n + "/events")]);
      if (state.appTab === "logs") calls.push(["logs", api("/app/" + n + "/logs" + logsQuery())]);
    } else if (state.view === "traefik") {
      calls.push(["traefik", api("/traefik")]);
      if (!state.graph) calls.push(["graph", api("/graph")]);
    } else if (state.view === "events") {
      calls.push(["events", api("/events" + eventsQuery())]);
      if (!state.graph) calls.push(["graph", api("/graph")]);
    } else {
      calls.push(["graph", api("/graph")]);
    }
    const R = await gather(calls);
    if (is401(R)) return loginOut();
    // resolve all bodies first, then commit atomically — but only if a newer
    // navigation/poll hasn't superseded this fetch (guards out-of-order writes)
    const P = {};
    for (const k of Object.keys(R)) {
      if (R[k] && R[k].ok) { try { P[k] = await R[k].json(); } catch (_) { /* ignore */ } }
    }
    if (gen !== reqGen) return;
    if (P.metrics) state.metrics = P.metrics;
    if (P.graph) state.graph = P.graph;
    if (R.app) {
      if (P.app) { state.app = P.app; state.appError = null; }
      else { state.app = null; state.appError = R.app.status === 404 ? "notfound" : "error"; }
    }
    if (R.traefik && P.traefik) state.traefik = P.traefik;
    if (R.ev) state.appEvents = P.ev || { error: true };
    if (R.logs) state.logs = P.logs || { error: true };
    if (R.events) state.events = P.events || { error: true };
    renderHeader();
    render();
  } catch (e) { /* keep last view */ }
}
function gather(pairs) {
  return Promise.all(pairs.map(async ([k, p]) => [k, await p.catch(() => null)])).then(Object.fromEntries);
}
function is401(R) { return Object.values(R).some(r => r && r.status === 401); }
function loginOut() { setPoll(0); showLogin(); }
function logsQuery() {
  const c = state.logsCtl, p = new URLSearchParams();
  if (c.container) p.set("container", c.container);
  if (c.tail) p.set("tail", c.tail);
  const s = p.toString();
  return s ? "?" + s : "";
}
function eventsQuery() {
  const f = state.eventsFilter, p = new URLSearchParams();
  if (f.ns) p.set("ns", f.ns);
  if (f.type) p.set("type", f.type);
  if (f.kind) p.set("kind", f.kind);
  if (f.q) p.set("q", f.q);
  const s = p.toString();
  return s ? "?" + s : "";
}

/* ---------- header ---------- */
function renderHeader() {
  const c = (state.graph && state.graph.cluster) || {};
  const ok = c.fluxReady;
  const hp = $("#health");
  hp.className = "chip " + (ok ? "ok" : "warn");
  hp.innerHTML = `<span class="dot ${ok ? "s-ok" : "s-progressing"}"></span>${ok ? "cluster healthy" : "attention"}`;
  const m = (state.metrics && state.metrics.node) || {};
  $("#usage").innerHTML = m.cpuPct != null
    ? `<span>CPU <b>${m.cpuPct}%</b></span><span>MEM <b>${m.memPct}%</b></span>` : "";
}

/* ---------- icons ---------- */
function icon(kind, w = 13, stroke = "currentColor") {
  const paths = {
    repo: `<circle cx="6" cy="6" r="2.2"/><circle cx="6" cy="18" r="2.2"/><path d="M6 8.2V15.8M6 12h7a4 4 0 0 0 4-4V6"/><circle cx="17" cy="4" r="2.2"/>`,
    sources: `<circle cx="6" cy="6" r="2.4"/><circle cx="6" cy="18" r="2.4"/><path d="M6 8.4V15.6M6 12h7a4 4 0 0 0 4-4V6"/><circle cx="17" cy="4" r="2.4"/>`,
    image: `<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>`,
    build: `<path d="M21 16V8a2 2 0 0 0-1-1.7l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.7l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="M3.3 7 12 12l8.7-5M12 22V12"/>`,
    ghcr: `<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>`,
    flux: `<path d="M21 12a9 9 0 1 1-3-6.7M21 4v4h-4"/>`,
    controller: `<path d="M21 12a9 9 0 1 1-3-6.7M21 4v4h-4"/>`,
    helmRelease: `<path d="M3 7l9-4 9 4-9 4z"/><path d="M3 12l9 4 9-4M3 17l9 4 9-4"/>`,
    kustomization: `<path d="M3 7l9-4 9 4-9 4z"/><path d="M3 12l9 4 9-4"/>`,
    cluster: `<path d="M12 2 3 7v10l9 5 9-5V7z"/><path d="M3 7l9 5 9-5M12 12v10"/>`,
    deployment: `<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>`,
    replicaSet: `<rect x="3" y="3" width="18" height="18" rx="2"/>`,
    pod: `<circle cx="12" cy="12" r="9"/><path d="M12 3v18M3 12h18"/>`,
    service: `<circle cx="6" cy="12" r="2.5"/><circle cx="18" cy="6" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M8.2 11 15.8 7M8.2 13 15.8 17"/>`,
    ingressRoute: `<path d="M2 12h20M12 2c3 3 3 17 0 20M12 2c-3 3-3 17 0 20"/>`,
    ingress: `<path d="M2 12h20M12 2c3 3 3 17 0 20M12 2c-3 3-3 17 0 20"/>`,
    configMap: `<path d="M4 5h16v14H4z"/><path d="M8 9h8M8 13h5"/>`,
    secret: `<rect x="4" y="10" width="16" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/>`,
    pvc: `<ellipse cx="12" cy="6" rx="8" ry="3"/><path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6"/>`,
    app: `<path d="M4 7h16M4 12h10M4 17h16"/><circle cx="18" cy="12" r="2"/>`,
    clock: `<circle cx="12" cy="12" r="9"/><path d="M12 8v4l3 2"/>`,
    ext: `<path d="M7 17 17 7M8 7h9v9"/>`,
    chevron: `<path d="M9 6l6 6-6 6"/>`,
  };
  return `<svg width="${w}" height="${w}" viewBox="0 0 24 24" fill="none" stroke="${stroke}" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">${paths[kind] || paths.app}</svg>`;
}

/* ---------- render dispatch ---------- */
function render() {
  const v = $("#view");
  if (state.view === "events") return renderEvents(v);
  if (state.view === "traefik") return renderTraefik(v);
  if (state.view === "app") return renderApp(v);
  if (state.view === "metrics") { if (!state.graph) { v.innerHTML = loading(); return; } return renderMetrics(v); }
  if (!state.graph) { v.innerHTML = loading(); return; }
  renderMap(v);
}
function loading() { return `<div class="warn">loading cluster…</div>`; }

/* ---------- MAP (boxed landscape) ---------- */
function renderMap(v) {
  const g = state.graph, c = g.cluster || {}, go = c.gitops || {};
  const nodes = g.nodes || [];
  const byId = {}; nodes.forEach(n => byId[n.id] = n);

  const repos = nodes.filter(n => n.kind === "repo");
  const root = repos.find(n => (n.summary || "").startsWith("GitOps root"));
  const ci = repos.find(n => /\/\.github$/.test(n.name));
  const appRepos = repos.filter(n => n !== root && n !== ci);
  const images = nodes.filter(n => n.kind === "image" && n.owner);
  const apps = nodes.filter(n => n.kind === "app");
  const appsByNs = {}; apps.forEach(a => (appsByNs[a.namespace] = appsByNs[a.namespace] || []).push(a));
  const nsAll = c.nsSummary || [];
  const nsApps = nsAll.filter(n => !n.platform);
  const nsPlat = nsAll.filter(n => n.platform);

  /* ribbon */
  const ribbon = `<div class="ribbon">
    <span class="chip mono"><span class="dot s-ok"></span>${esc(c.version || "k3s")} · ${esc(c.node || "")}</span>
    <span class="chip">1 node</span>
    <span class="chip">${c.namespaces || 0} namespaces</span>
    <span class="chip">${c.apps || 0} apps</span>
    <span class="chip">${c.podsReady || 0}/${c.podsTotal || 0} pods running</span>
    <span class="chip ${c.fluxReady ? "ok" : "warn"}"><span class="dot ${c.fluxReady ? "s-ok" : "s-progressing"}"></span>Flux ${esc(c.fluxMsg || "")}${c.fluxAgo ? " · " + esc(c.fluxAgo) : ""}</span>
    ${c.tls ? `<span class="chip">TLS · ${esc(c.tls)}</span>` : ""}
    <span class="grow"></span>
    <span class="flow">flow: git → build → GitOps → cluster</span>
  </div>`;

  /* lane 1: sources */
  const srcCards = [];
  if (root) srcCards.push(mapCard(root, { accent: "teal", kindLabel: "GITOPS ROOT · flux source", kindTeal: true }));
  appRepos.forEach(r => srcCards.push(mapCard(r, { kindLabel: "app source" })));
  if (ci) srcCards.push(mapCard(ci, { kindLabel: "reusable CI", sub: "build-push.yml" }));
  const lane1 = lane("sources", "Sources · GitHub", srcCards.join(""));

  /* lane 2: build */
  const imgCards = images.map(im => {
    const [nm, tag] = String(im.name).split(":");
    return `<div class="card appflow" data-id="${esc(im.id)}" data-app="${esc(im.app || "")}">
      <div class="mono img-name" style="font-size:11.5px">${esc(nm)}</div>
      <div class="mono img-name" style="font-size:11px;color:var(--teal);margin-top:2px">:${esc(tag || "")}</div></div>`;
  }).join("");
  const buildInner = `
    <div class="card">
      <div class="kind">GitHub Actions</div>
      <div style="font-size:13px;margin-top:3px">build-push</div>
      <div class="sub">on tag vX.Y.Z / push</div>
    </div>
    <div class="ghcr-label">${icon("ghcr", 13, "#616b7a")} GHCR · public</div>
    ${imgCards}`;
  const lane2 = lane("build", "Build · Actions → GHCR", buildInner);

  /* lane 3: gitops */
  const ctrls = (go.controllers && go.controllers.length ? go.controllers
    : ["source", "kustomize", "helm", "notification", "image-reflector", "image-automation"]);
  const ctrlChips = ctrls.map(x => `<span class="chip sm">${esc(x)}</span>`).join("");
  const kusts = (go.kustomizations || []).map(k => `<div class="kr">
      <span class="dot s-${k.ready}"></span><span class="mono">${esc(k.name)}</span>
      ${k.reconciledAt ? `<span class="ago">${esc(k.reconciledAt)}</span>` : ""}
      ${k.url ? `<a class="krlink" href="${esc(k.url)}" target="_blank" rel="noopener" title="${esc(k.path || "repo folder")}" onclick="event.stopPropagation()">${icon("ext", 11)}</a>` : ""}
    </div>`).join("");
  const gitopsInner = `
    <div class="card"><div class="kind">controllers</div><div class="ctrls">${ctrlChips}</div></div>
    <div class="card"><div class="kind">kustomizations</div><div class="kustlist">${kusts || '<span class="mono" style="color:var(--mut);font-size:11px">—</span>'}</div></div>
    <div class="card accent-violet flowall">
      <div class="row1">${icon("flux", 14, "#9b8cf0")}<span style="font-size:12.5px;color:#c7bdf7">image-automation</span></div>
      <div class="mono" style="font-size:10.5px;color:var(--mut);margin-top:4px">watches GHCR → bumps tag → commits</div>
      <div class="ia-bubble">image-automation writes the bumped tag to infra → Flux deploys the new image</div>
    </div>`;
  const lane3 = lane("gitops", "GitOps · Flux" + vlabel(go.version), gitopsInner);

  /* lane 4: cluster */
  const nsBoxes = nsApps.map(ns => {
    const list = (appsByNs[ns.name] || []);
    const cardsH = list.map(a => appCardHTML(a)).join("") ||
      `<div class="mono" style="font-size:11px;color:var(--mut);margin-top:8px">no owned workloads</div>`;
    return `<div class="ns-box">
      <div class="nsh"><span class="kind" style="color:var(--dim)">ns · ${esc(ns.name)}</span><span class="rdy">● Ready</span></div>
      ${cardsH}</div>`;
  }).join("");
  const platCards = nsPlat.map(ns => `<div class="card">
      <div class="pn">${esc(ns.name)}</div>
      <div class="ps">${esc(ns.note || (ns.pods + " pods"))}</div></div>`).join("");
  const cap = [c.nodeCpu, c.nodeMem, c.diskFree].filter(Boolean).join(" · ");
  const lane4 = `<div class="lane cluster">
    <div class="lanehdr"><span style="display:flex;align-items:center;gap:8px">${icon("cluster", 14, "#9aa4b2")} Cluster · k3s @ ${esc(c.node || "node")}</span>${cap ? `<span class="cap">${esc(cap)}</span>` : ""}</div>
    <div class="clusterbody">
      <div class="traefik-rail" title="Open Traefik routing">${icon("ingress", 18, "#35d0c0")}<div class="vt">Traefik · :443 TLS</div></div>
      <div class="clustercol">
        <div class="nsrow hscroll">${nsBoxes}</div>
        <div class="platform-box"><span class="kind" style="color:var(--dim)">platform namespaces</span><div class="platform-grid hscroll">${platCards}</div></div>
        <div class="footer-note">${icon("clock", 13, "#616b7a")} local-path PVCs · memory-only sessions · data wiped on start</div>
      </div>
    </div></div>`;

  const gapBuild = flowgap("build", "build");
  const gapScan = flowgap("scan", "scan");
  const gapDeploy = flowgap("deploy", "deploy");

  v.innerHTML = ribbon +
    `<div class="map">${lane1}${gapBuild}${lane2}${gapScan}${lane3}${gapDeploy}${lane4}</div>` +
    `<div class="autobump-cap">↑ image-automation writes the new tag back to ${esc(root ? root.name.split("/").pop() : "infra")}</div>` +
    legendHTML();

  wireMap(byId);
}

function lane(id, title, inner) {
  return `<div class="lane" data-lane="${id}">
    <div class="lanehdr">${icon(id === "sources" ? "sources" : id === "build" ? "build" : id === "gitops" ? "flux" : "cluster", 14, "#9aa4b2")} ${esc(title)}</div>
    <div class="lanebody">${inner}</div></div>`;
}
function flowgap(kindClass, label) {
  const col = kindClass === "deploy" ? "#35d0c0" : "#4a5566";
  return `<div class="flowgap ${kindClass}"><div class="lbl">${label}</div>
    <svg width="34" height="12" viewBox="0 0 34 12"><path d="M1 6h26" stroke="${col}" stroke-width="1.5"/><path d="M26 2l5 4-5 4" fill="none" stroke="${col}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg></div>`;
}
function mapCard(n, opt = {}) {
  const dotColor = opt.accent === "teal" ? "s-ok" : "s-unknown";
  const kindLine = opt.kindLabel ? `<div class="kind ${opt.kindTeal ? "teal" : ""}" style="margin-top:5px">${esc(opt.kindLabel)}</div>` : "";
  const sub = opt.sub != null ? opt.sub : reposub(n);
  return `<div class="card appflow ${opt.accent === "teal" ? "accent-teal" : ""}" data-id="${esc(n.id)}" data-app="${esc(n.app || "")}">
    <div class="row1"><span class="dot ${dotColor}"></span><span class="nm mono">${esc(shortRepo(n.name))}</span></div>
    ${kindLine}${sub ? `<div class="sub">${esc(sub)}</div>` : ""}</div>`;
}
function reposub(n) { return ""; }
function shortRepo(name) {
  const p = String(name).split("/");
  return p.length > 1 ? "…/" + p[p.length - 1] : name;
}
function appCardHTML(a) {
  const m = a.meta || {};
  const tag = (m.image || "").split(":").pop();
  const violet = !a.owner;
  return `<div class="app-card ${violet ? "violet" : ""}" data-id="${esc(a.id)}" data-app="${esc(a.app || a.name)}" data-appname="${esc(a.name)}">
    <div class="ah"><span class="an">${esc(a.name)}</span><span class="av">${esc(tag || "")}</span></div>
    ${m.route ? `<div class="art">${esc(m.route)}</div>` : ""}
    <div class="achips">
      <span class="chip xs">Deploy ${esc(m.replicas || "")}</span>
      <span class="chip xs">Svc</span>
      <span class="chip xs">Route</span>
    </div>
    <div class="open">open components ${icon("chevron", 12, "#35d0c0")}</div>
  </div>`;
}
function legendHTML() {
  return `<div class="mapfoot">
    <span><svg width="24" height="8"><path d="M1 4h22" stroke="#4a5566" stroke-width="1.5"/></svg> pipeline flow</span>
    <span><svg width="24" height="8"><path d="M1 4h22" stroke="#35d0c0" stroke-width="1.5"/></svg> GitOps deploy</span>
    <span><svg width="24" height="8"><path d="M1 4h22" stroke="#9b8cf0" stroke-width="1.4" stroke-dasharray="5 4"/></svg> auto-bump loop</span>
    <span><span class="dot s-ok"></span> Ready</span>
    <span><span class="dot s-progressing"></span> Progressing</span>
    <span><span class="dot s-failed"></span> Failed</span>
  </div>`;
}

/* map interactions: hover flow-highlight (transient) + click to pin the
   highlight (hides the hover card so the whole flow stays visible) */
function wireMap(byId) {
  const map = $(".map");
  $$(".map [data-id]").forEach(el => {
    const node = byId[el.dataset.id];
    const app = el.dataset.app;
    el.addEventListener("mouseenter", () => {
      cancelHide();
      if (state.pin) return;                 // locked: don't re-trace on hover
      if (app) applyTrace(app);
      if (node) showHoverCard(el, node);
    });
    el.addEventListener("mouseleave", scheduleHide); // grace so the card is reachable
    el.addEventListener("click", (e) => {
      if (e.target.closest(".open") || e.target.closest(".linkchip")) return; // nav / link
      e.stopPropagation();
      $("#hover").classList.add("hide");
      if (!app) return;
      if (state.pin && state.pin === app) unpin(); else pinApp(app);
    });
  });
  // "open components →" navigates to the app page (does not pin)
  $$(".map .app-card .open").forEach(el => el.addEventListener("click", (e) => {
    e.stopPropagation();
    openApp(el.closest(".app-card").dataset.appname);
  }));
  // Traefik rail → routing page
  const rail = $(".map .traefik-rail");
  if (rail) rail.addEventListener("click", openTraefik);
  // click empty space clears a pin
  map.addEventListener("click", (e) => { if (!e.target.closest("[data-id]") && !e.target.closest(".traefik-rail")) unpin(); });
  // re-apply a live pin after a poll re-render
  if (state.pin) { map.classList.add("pinned"); applyTrace(state.pin); }
}
function cssq(s) { return String(s).replace(/"/g, '\\"'); }
// hover card hide is deferred so the cursor can travel to the card and click its links
let hoverHideTimer = null;
function scheduleHide() { clearTimeout(hoverHideTimer); hoverHideTimer = setTimeout(() => { if (!state.pin) clearTrace(); }, 220); }
function cancelHide() { clearTimeout(hoverHideTimer); }
function applyTrace(app) {
  const map = $(".map"); if (!map) return;
  map.classList.add("tracing");
  $$(".map .trace").forEach(x => x.classList.remove("trace"));
  $$(`.map [data-app="${cssq(app)}"]`).forEach(x => x.classList.add("trace"));
  // GitOps steps that apply to every app (image-automation, apps kustomization)
  $$(".map .flowall").forEach(x => x.classList.add("trace"));
}
function pinApp(app) {
  cancelHide();
  state.pin = app;
  const map = $(".map"); map.classList.add("pinned");
  applyTrace(app);
}
function unpin() {
  if (!state.pin) return;
  state.pin = null;
  const map = $(".map"); if (map) map.classList.remove("pinned");
  clearTrace();
}
function clearTrace() {
  const map = $(".map"); if (map) map.classList.remove("tracing");
  $$(".map .trace").forEach(x => x.classList.remove("trace"));
  $("#hover").classList.add("hide");
}

/* ---------- hover card ---------- */
function chipHTML(l) {
  const label = ({ source: "Source", workflow: "Workflow", config: "Config", image: "Image", docs: "Docs" })[l.type] || l.type;
  return `<span class="linkchip" data-url="${esc(l.url)}">${esc(label)} ${icon("ext", 11)}</span>`;
}
function showHoverCard(el, n) {
  if (!n) return;
  const card = $("#hover");
  const links = (n.links || []).map(chipHTML).join("");
  card.innerHTML = `<div class="ttl"><span class="name">${esc(n.name)}</span><span class="badge">${n.owner ? "your service" : "infra tool"}</span></div>
    ${n.statusText ? `<div class="sub">${esc(n.statusText)}${n.meta && n.meta.image ? " · " + esc(n.meta.image) : ""}</div>` : ""}
    ${n.summary ? `<div class="expl">${esc(n.summary)}</div>` : ""}
    ${links ? `<div class="chips">${links}</div>` : ""}`;
  card.querySelectorAll(".linkchip").forEach(c => c.onclick = (e) => { e.stopPropagation(); window.open(c.dataset.url, "_blank", "noopener"); });
  card.classList.remove("hide");
  const r = el.getBoundingClientRect();
  let x = r.right + 12, y = r.top;
  const cw = 320, ch = card.offsetHeight || 200;
  if (x + cw > window.innerWidth - 10) x = r.left - cw - 12;
  if (y + ch > window.innerHeight - 10) y = window.innerHeight - ch - 10;
  card.style.left = Math.max(10, x) + "px";
  card.style.top = Math.max(66, y) + "px";
}

/* ---------- APP DETAIL (full page) ---------- */
function renderApp(v) {
  const d = state.app;
  if (!d) {
    if (state.appError) {
      v.innerHTML = state.appError === "notfound"
        ? errBox("App not found", `No app “${state.appName || ""}” in the cluster.`)
        : errBox("Couldn’t load app", `The API returned an error for “${state.appName || ""}”.`);
      return;
    }
    v.innerHTML = `<div class="warn">loading ${esc(state.appName || "")}…</div>`;
    return;
  }
  const prevLog = state.appTab === "logs" ? logScrollState() : null;
  const violet = !d.owner;
  const st = d.status || "unknown";
  const badge = st === "ok" ? ["ok", "Healthy"] : st === "failed" ? ["err", "Failed"] : ["warn", "Progressing"];
  const hr = d.gitops && d.gitops.helmRelease;
  const reconLine = `ns ${esc(d.namespace)}${hr ? " · Helm release · reconciled by Flux (apps)" : ""}${hr && hr.reconciledAt ? " " + esc(hr.reconciledAt) : ""}`;

  const band = `<div class="appband">
    <div class="tile ${violet ? "violet" : ""}">${icon("app", 22, violet ? "#9b8cf0" : "#35d0c0")}</div>
    <div>
      <div class="titlerow">
        <h1>${esc(d.name)}</h1>
        <span class="badge ${badge[0]}"><span class="dot s-${st}"></span>${badge[1]}</span>
        ${d.version ? `<span class="ver">${esc(d.version)}</span>` : ""}
      </div>
      <div class="meta">${reconLine}</div>
    </div>
    <span class="grow"></span>
    ${d.publicUrl ? `<a class="puburl" href="${esc(d.publicUrl)}" target="_blank" rel="noopener">${esc(d.publicUrl.replace(/^https?:\/\//, ""))} ${icon("ext", 14, "#35d0c0")}</a>` : ""}
    <div class="tabs">${["graph", "events", "logs"].map(t => `<button class="tab${state.appTab === t ? " on" : ""}" data-tab="${t}">${t[0].toUpperCase() + t.slice(1)}</button>`).join("")}</div>
  </div>`;

  let body;
  if (state.appTab === "events") body = `<div id="appbody" class="pane">${appEventsHTML()}</div>`;
  else if (state.appTab === "logs") body = `<div id="appbody" class="pane">${appLogsHTML()}</div>`;
  else body = `<div id="graphwrap">${appGraphHTML(d)}</div>`;

  v.innerHTML = band + `<div class="appmain">${body}${appRailHTML(d)}</div>`;
  wireApp(d, prevLog);
  if (state.appTab === "graph") requestAnimationFrame(drawAppEdges);
}

function setAppTab(t) {
  if (state.appTab === t) return;
  state.appTab = t;
  if (t === "events") state.appEvents = null;
  if (t === "logs") state.logs = null;
  render();
  refresh();
}

/* ---------- app-detail: events + logs tabs ---------- */
function appEventsHTML() {
  const e = state.appEvents;
  if (!e) return `<div class="warn">loading events…</div>`;
  if (e.error) return errBox("Events unavailable", "The console may lack the events read grant (core events: get, list), or the API returned an error.");
  if (!e.events || !e.events.length) return emptyBox("No recent events", `${e.window || ""} Nothing recorded for this app's workloads right now.`);
  return `<div class="evpane">${eventsListHTML(e.events, { showNs: false })}<div class="evfoot">${esc(e.window || "")}</div></div>`;
}

function appLogsHTML() {
  const l = state.logs;
  const picker = (l && l.containers && l.containers.length > 1)
    ? `<label class="lc">container <select id="logcontainer">${l.containers.map(c => `<option ${l.container === c ? "selected" : ""}>${esc(c)}</option>`).join("")}</select></label>` : "";
  const tailSel = `<label class="lc">tail <select id="logtail">${[200, 500, 1000, 2000, 5000].map(n => `<option ${state.logsCtl.tail === n ? "selected" : ""}>${n}</option>`).join("")}</select></label>`;
  const podLabel = l && l.pod ? `<span class="logpod mono">${esc(l.pod)}${l.container ? " · " + esc(l.container) : ""}</span>` : "";
  const ctl = `<div class="logctl">${picker}${tailSel}<button class="lbtn" id="logrefresh">Refresh</button><span class="grow"></span>${podLabel}</div>`;
  let box;
  if (!l) box = `<div class="warn">loading logs…</div>`;
  else if (l.error) box = errBox("Logs unavailable", "The console may lack the pod-log read grant (core pods/log: get), or the pod has no logs yet.");
  else if (!l.lines || !l.lines.length) box = `<div class="logbox empty mono">— ${esc(l.note || "no log lines")} —</div>`;
  else box = `<pre class="logbox mono" id="logbox">${l.lines.map(logLineHTML).join("\n")}</pre>`;
  return `<div class="logwrap">${ctl}${box}</div>`;
}
function logLineHTML(line) {
  const low = line.toLowerCase();
  const cls = /(?:^|[^a-z])(error|fatal|panic|fail|exception|"level":"error")/.test(low) ? "lg-err"
    : /(?:^|[^a-z])(warn)/.test(low) ? "lg-warn" : "";
  return `<span class="lgl ${cls}">${esc(line)}</span>`;
}

/* shared events list (per-app + combined browser) */
function eventsListHTML(events, opts = {}) {
  return `<div class="evlist">` + events.map(ev => {
    const warn = ev.type === "Warning";
    return `<div class="evrow${warn ? " warn" : ""}">
      <div class="evtype ${warn ? "w" : "n"}"><span class="dot ${warn ? "s-progressing" : "s-ok"}"></span>${esc(ev.type)}</div>
      <div class="evbody">
        <div class="evtop">
          <span class="evreason">${esc(ev.reason)}</span>
          <span class="evobj mono">${esc(ev.involvedKind)}/${esc(ev.involvedName)}</span>
          ${opts.showNs ? `<span class="evns mono">ns ${esc(ev.namespace)}</span>` : ""}
          <span class="grow"></span>
          <span class="evage mono">${esc(ev.lastSeen || "")}</span>
        </div>
        ${ev.message ? `<div class="evmsg">${esc(ev.message)}</div>` : ""}
        <div class="evmeta mono">${ev.count > 1 ? `×${ev.count}` : "once"}${ev.component ? " · " + esc(ev.component) : ""}${ev.firstSeen && ev.firstSeen !== ev.lastSeen ? " · first " + esc(ev.firstSeen) : ""}</div>
      </div>
    </div>`;
  }).join("") + `</div>`;
}

function emptyBox(title, msg) {
  return `<div class="stub"><div class="box">${icon("clock", 26, "#616b7a")}<h3>${esc(title)}</h3><p>${esc(msg)}</p></div></div>`;
}
function errBox(title, msg) {
  return `<div class="stub"><div class="box">${icon("clock", 26, "#f0b429")}<h3>${esc(title)}</h3><p>${esc(msg)}</p></div></div>`;
}

function gnode(gid, kind, kindLabel, name, sub, extra = "") {
  return `<div class="gnode ${extra}" data-gid="${gid}">
    <div class="gk">${icon(kind, 12, kindColor(kind))}${esc(kindLabel)}</div>
    <div class="gn">${name}</div>${sub ? `<div class="gs">${sub}</div>` : ""}</div>`;
}
function kindColor(k) {
  return ({ helmRelease: "#35d0c0", pod: "#57d39a", secret: "#f0b429", ingressRoute: "#35d0c0", service: "#9aa4b2" })[k] || "#9aa4b2";
}
function appGraphHTML(d) {
  const dep = d.deployment || {};
  const rs = d.replicaSet;
  const pods = d.pods || [];
  const svc = d.service;
  const ing = d.ingress;
  const hr = d.gitops && d.gitops.helmRelease;

  /* source strip */
  const repoPill = d.sourceRepo ? `<span class="srcpill">…/${esc(d.sourceRepo.split("/").pop())}</span>
     <svg width="18" height="10"><path d="M1 5h13" stroke="#4a5566" stroke-width="1.4"/><path d="M13 2l4 3-4 3" fill="none" stroke="#4a5566" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>` : "";
  const imgPill = `<span class="srcpill teal" data-gid="img">${esc(shortImage(d.image))}</span>`;
  const srcstrip = `<div class="srcstrip">${repoPill}${imgPill}</div>`;

  /* left column: config / secret / pvc */
  const left = [];
  (d.configMaps || []).forEach((r, i) => left.push(gnode("cm" + i, "configMap", "ConfigMap", esc(r.name), esc(r.origin || ""))));
  (d.secrets || []).forEach((r, i) => left.push(gnode("sec" + i, "secret", "Secret" + (r.sops ? " · SOPS" : ""), esc(r.name), esc(r.origin || ""), "warnb")));
  (d.pvcs || []).forEach((r, i) => left.push(gnode("pvc" + i, "pvc", "PVC · local-path", esc(r.name), esc(r.detail || r.origin || ""))));
  if (!left.length) left.push(`<div class="gnode" style="opacity:.6"><div class="gk">mounts</div><div class="gs">no config, secrets or volumes</div></div>`);

  /* mid spine: HR -> Deploy -> RS -> Pod(s) */
  const mid = [];
  if (hr) mid.push(gnode("hr", "helmRelease", "HelmRelease · Flux", esc(hr.name), `${esc(hr.chart || "")} · <span style="color:var(--ok)">${esc(hr.ready || "")}</span>`, "teal"));
  const depColor = d.status === "ok" ? "var(--ok)" : d.status === "failed" ? "var(--err)" : "var(--warn)";
  mid.push(gnode("dep", "deployment", "Deployment", `${esc(dep.name || d.name)} <span style="color:${depColor}">${esc(dep.ready || "")}</span>`, `strategy ${esc(dep.strategy || "")}`));
  if (rs) mid.push(gnode("rs", "replicaSet", "ReplicaSet", esc(rs.name), rs.revision ? "revision " + esc(rs.revision) : ""));
  pods.forEach((p, i) => mid.push(gnode("pod" + i, "pod", "Pod", esc(p.name),
    `<span style="color:var(--${p.ready ? "ok" : "warn"})">${esc(p.phase)}</span> · ${p.restarts} restarts · ${p.cpuMilli || 0}m · ${fmtMem(p.memBytes || 0)}`,
    p.ready ? "ok" : "")));
  if (!pods.length) mid.push(gnode("pod0", "pod", "Pod", "—", "no pods"));

  /* right column: ingress -> mw -> service */
  const right = [];
  if (ing) {
    right.push(gnode("ing", "ingressRoute", "IngressRoute · Traefik", `${esc(d.name)} → ${esc(ing.path || "/")}`,
      `${esc(ing.entryPoint || "websecure")}${ing.tls ? " · TLS default store" : ""}`, "teal"));
    if (ing.middlewares && ing.middlewares.length) {
      right.push(`<div class="mws" data-gid="mw">${ing.middlewares.map(m => `<span class="mwchip">mw ${esc(m)}</span>`).join("")}</div>`);
    }
  }
  if (svc) right.push(gnode("svc", "service", "Service · " + esc(svc.type || "ClusterIP"), `${esc(svc.name)} :${esc(svc.port || "")}`, svc.targetPort ? "→ pod :" + esc(svc.targetPort) : ""));

  return srcstrip + `<svg id="gedges"></svg>
    <div class="gcols">
      <div class="gcol left">${left.join("")}</div>
      <div class="gcol mid">${mid.join("")}</div>
      <div class="gcol right">${right.join("")}</div>
    </div>`;
}

function appRailHTML(d) {
  const dep = d.deployment || {};
  const st = d.status || "unknown";
  const avail = st === "ok" ? "Available" : st === "failed" ? "Unavailable" : "Progressing";
  const pods = d.pods || [];
  const cpu = pods.reduce((s, p) => s + (p.cpuMilli || 0), 0);
  const mem = pods.reduce((s, p) => s + (p.memBytes || 0), 0);
  const cpuLim = dep.cpuLimitMilli || 0, memLim = dep.memLimitBytes || 0;
  const hr = d.gitops && d.gitops.helmRelease;
  const kust = d.gitops && d.gitops.kustomization;
  const auto = d.gitops && d.gitops.imageAutomation;
  const row = (k, val, mono) => `<div class="row"><span class="rk">${esc(k)}</span><span class="rv ${mono ? "mono" : ""}">${val}</span></div>`;

  return `<div class="rail">
    <div class="rhead"><span class="rlabel">Selected · Deployment</span>
      <span class="rstat" style="color:var(--${cls(st)})"><span class="dot s-${st}"></span>${avail}</span></div>
    <div class="rname">${esc(d.name)}</div>
    <div class="rsub">apps/Deployment · ns ${esc(d.namespace)}</div>
    <div class="rbox">
      ${row("Image", `<span style="color:var(--teal)">${esc(shortImage(dep.image || d.image))}</span>`, true)}
      ${d.version ? row("Version", esc(d.version), true) : ""}
      ${row("Replicas", `${dep.available || 0} / ${dep.replicas || 0}`, true)}
      ${row("Strategy", esc(dep.strategy || "—"))}
      ${row("Age", esc(fmtAge(dep.ageSeconds || 0)), true)}
      ${row("Restarts", esc(String(dep.restarts || 0)), true)}
    </div>

    <div class="rsec">Managed by</div>
    <div class="rbox pad managed">
      <div class="ln">${icon("flux", 14, "#35d0c0")}<span class="mono">HelmRelease ${esc(hr ? hr.name : d.name)}</span></div>
      <div class="msub">Kustomization ${kust && kust.url ? `<a href="${esc(kust.url)}" target="_blank" rel="noopener">${esc(kust.name)} ${icon("ext", 10)}</a>` : esc(kust ? kust.name : "apps")}${hr && hr.chart ? " · chart " + esc(hr.chart) : ""}</div>
      ${auto ? `<div class="mauto">image-automation: ${esc(auto)}</div>` : ""}
    </div>

    <div class="rsec">Live usage</div>
    <div class="rbox pad usebox">
      <div class="ul"><span>CPU</span><span class="mono">${cpu}m${cpuLim ? " / " + cpuLim + "m" : ""}</span></div>
      ${spark(cpuLim ? cpu / cpuLim : 0, "#35d0c0")}
      <div class="ul"><span>Memory</span><span class="mono">${fmtMem(mem)}${memLim ? " / " + fmtMem(memLim) : ""}</span></div>
      ${spark(memLim ? mem / memLim : 0, "#6aa6ff")}
    </div>

    <div class="railbtns">
      <button class="rb" data-tab="logs">${icon("clock", 14)} View logs</button>
      <button class="rb" data-tab="events">Events</button>
    </div>
  </div>`;
}

/* spark: a truthful minimal chart — flat line at the current level (no history collected) */
function spark(frac, color) {
  frac = Math.max(0, Math.min(1, frac));
  const y = (28 - frac * 24).toFixed(1);
  return `<svg class="spark" viewBox="0 0 308 30" preserveAspectRatio="none"><line x1="0" y1="29" x2="308" y2="29" stroke="#1b2028" stroke-width="1"/><polyline points="0,${y} 308,${y}" fill="none" stroke="${color}" stroke-width="1.5"/></svg>`;
}

function wireApp(d, prevLog) {
  $$(".appband .tab").forEach(b => b.onclick = () => setAppTab(b.dataset.tab));
  $$(".railbtns .rb").forEach(b => b.onclick = () => setAppTab(b.dataset.tab));
  if (state.appTab === "logs") wireLogs(prevLog);
}
function logScrollState() {
  const b = $("#logbox");
  if (!b) return null;
  return { atBottom: b.scrollTop + b.clientHeight >= b.scrollHeight - 8, top: b.scrollTop };
}
function wireLogs(prevLog) {
  const c = $("#logcontainer");
  if (c) c.onchange = () => { state.logsCtl.container = c.value; state.logs = null; render(); refresh(); };
  const t = $("#logtail");
  if (t) t.onchange = () => { state.logsCtl.tail = +t.value; state.logs = null; render(); refresh(); };
  const rb = $("#logrefresh");
  if (rb) rb.onclick = () => refresh();
  const box = $("#logbox");
  if (box) {
    // tail to newest on first open / after a refresh, but preserve the reader's
    // position if they've scrolled up to read older lines
    if (!prevLog || prevLog.atBottom) box.scrollTop = box.scrollHeight;
    else box.scrollTop = prevLog.top;
  }
}

/* app graph edges: computed from DOM node rects */
function drawAppEdges() {
  const wrap = $("#graphwrap"), svg = $("#gedges");
  if (!wrap || !svg) return;
  const W = wrap.scrollWidth, H = wrap.scrollHeight;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`); svg.setAttribute("width", W); svg.setAttribute("height", H);
  const base = wrap.getBoundingClientRect();
  const pos = {};
  wrap.querySelectorAll("[data-gid]").forEach(el => {
    const r = el.getBoundingClientRect();
    pos[el.dataset.gid] = { x: r.left - base.left, y: r.top - base.top, w: r.width, h: r.height };
  });
  const d = state.app || {};
  const pods = (d.pods || []).map((_, i) => "pod" + i);
  const firstPod = pods[0] || "pod0";
  const rsOrDep = pos["rs"] ? "rs" : "dep";
  const edges = [];
  edges.push(["img", "hr" in pos ? "hr" : "dep", "flow"]);
  if (pos["hr"]) edges.push(["hr", "dep", "teal"]);
  if (pos["rs"]) edges.push(["dep", "rs", "teal"]);
  pods.forEach(p => edges.push([rsOrDep, p, "flow"]));
  // mounts (dashed) from deployment to each config/secret/pvc
  Object.keys(pos).filter(k => /^(cm|sec|pvc)\d+$/.test(k)).forEach(k => edges.push(["dep", k, "mount"]));
  // networking chain
  if (pos["ing"]) edges.push(["ing", pos["mw"] ? "mw" : (pos["svc"] ? "svc" : firstPod), "flow"]);
  if (pos["mw"] && pos["svc"]) edges.push(["mw", "svc", "flow"]);
  if (pos["svc"]) edges.push(["svc", firstPod, "teal"]);

  let paths = "";
  edges.forEach(([a, b, kind]) => {
    const A = pos[a], B = pos[b]; if (!A || !B) return;
    const col = kind === "teal" ? "#35d0c0" : "#4a5566";
    const dash = kind === "mount" ? `stroke-dasharray="4 4"` : "";
    const p = anchor(A, B);
    paths += `<path d="M ${p.x1} ${p.y1} C ${p.cx1} ${p.cy1}, ${p.cx2} ${p.cy2}, ${p.x2} ${p.y2}" fill="none" stroke="${col}" stroke-width="1.5" ${dash} marker-end="url(#gm-${kind})"/>`;
  });
  const defs = `<defs>
    ${["teal", "flow", "mount"].map(k => `<marker id="gm-${k}" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M2 2 8 5 2 8" fill="none" stroke="${k === "teal" ? "#35d0c0" : "#4a5566"}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></marker>`).join("")}
  </defs>`;
  svg.innerHTML = defs + paths;
}
function anchor(A, B) {
  const ac = { x: A.x + A.w / 2, y: A.y + A.h / 2 }, bc = { x: B.x + B.w / 2, y: B.y + B.h / 2 };
  const dx = bc.x - ac.x, dy = bc.y - ac.y;
  if (Math.abs(dy) >= Math.abs(dx)) {
    // vertical
    const down = dy >= 0;
    const y1 = down ? A.y + A.h : A.y, y2 = down ? B.y : B.y + B.h;
    const m = (y1 + y2) / 2;
    return { x1: ac.x, y1, x2: bc.x, y2, cx1: ac.x, cy1: m, cx2: bc.x, cy2: m };
  }
  // horizontal
  const right = dx >= 0;
  const x1 = right ? A.x + A.w : A.x, x2 = right ? B.x : B.x + B.w;
  const m = (x1 + x2) / 2;
  return { x1, y1: ac.y, x2, y2: bc.y, cx1: m, cy1: ac.y, cx2: m, cy2: bc.y };
}

/* ---------- helpers ---------- */
function vlabel(v) { return v ? " " + (String(v).startsWith("v") ? v : "v" + v) : ""; }
function shortImage(img) {
  if (!img) return "—";
  const parts = String(img).split("/");
  return parts.length > 1 ? "…/" + parts.slice(-1)[0] : img;
}
function fmtMem(b) { const g = b / 1073741824; return g >= 1 ? g.toFixed(2) + " GiB" : (b / 1048576).toFixed(0) + "Mi"; }
function fmtAge(s) {
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 86400) return Math.floor(s / 3600) + "h";
  return Math.floor(s / 86400) + "d";
}

/* ---------- METRICS ---------- */
function ring(pct, color) {
  const c = 188.5, dash = Math.max(0, Math.min(100, pct)) / 100 * c;
  return `<svg width="76" height="76" viewBox="0 0 76 76"><circle cx="38" cy="38" r="30" fill="none" stroke="#1b2028" stroke-width="8"/>
    <circle cx="38" cy="38" r="30" fill="none" stroke="${color}" stroke-width="8" stroke-linecap="round" stroke-dasharray="${dash} ${c}" transform="rotate(-90 38 38)"/></svg>`;
}
function relClock(ts) {
  if (!ts) return "—";
  const t = new Date(ts).getTime();
  if (isNaN(t)) return "—";
  const s = Math.max(0, Math.round((Date.now() - t) / 1000));
  return s < 60 ? s + "s ago" : Math.floor(s / 60) + "m ago";
}
const metricSort = {
  mem: (a, b) => b.memBytes - a.memBytes,
  cpu: (a, b) => b.cpuMilli - a.cpuMilli,
  name: (a, b) => String(a.name).localeCompare(String(b.name)),
};
function renderMetrics(v) {
  const m = state.metrics || {}, node = m.node || {}, g = state.graph || {}, c = g.cluster || {};
  const mc = state.metricsCtl;
  const kusts = (c.gitops && c.gitops.kustomizations) || (g.nodes || []).filter(n => n.kind === "kustomization").map(n => ({ name: n.name, ready: n.status }));
  const cmp = metricSort[mc.sort] || metricSort.mem;
  let nsList = (m.namespaces || []).slice();
  let podList = (m.pods || []).slice();
  if (mc.ns) { nsList = nsList.filter(n => n.name === mc.ns); podList = podList.filter(p => p.namespace === mc.ns); }
  nsList.sort(cmp);
  podList.sort(cmp);
  podList = podList.slice(0, 12);
  // bars represent the sorted metric (cpu when sorting by cpu, else memory)
  const barKey = mc.sort === "cpu" ? "cpuMilli" : "memBytes";
  const maxMem = Math.max(1, ...nsList.map(n => n[barKey]));
  const maxPod = Math.max(1, ...podList.map(p => p[barKey]));
  const nsNames = (m.namespaces || []).map(n => n.name);
  const toolbar = `<div class="mtoolbar">
    <select id="mns" title="Namespace"><option value="">all namespaces</option>${nsNames.map(n => `<option ${mc.ns === n ? "selected" : ""}>${esc(n)}</option>`).join("")}</select>
    <div class="seg" id="msort" role="group" aria-label="Sort by">${[["mem", "memory"], ["cpu", "cpu"], ["name", "name"]].map(([val, lab]) => `<button data-sort="${val}" class="${mc.sort === val ? "on" : ""}">${lab}</button>`).join("")}</div>
    <label class="lc">refresh <select id="mrefresh">${[["5000", "5s"], ["15000", "15s"], ["30000", "30s"], ["0", "paused"]].map(([val, lab]) => `<option value="${val}" ${mc.interval === +val ? "selected" : ""}>${lab}</option>`).join("")}</select></label>
    <span class="grow"></span>
    <span class="mono mupd">updated ${relClock(m.updatedAt)}</span>
  </div>`;
  const nsBars = nsList.map(n => `<div class="mrow"><div class="lab"><span>${esc(n.name)}</span><span class="mono">${fmtMem(n.memBytes)} · ${n.cpuMilli || 0}m · ${n.pods}p</span></div><div class="barbg"><div class="barfill" style="width:${(n[barKey] / maxMem * 100).toFixed(0)}%"></div></div></div>`).join("");
  const podBars = podList.map(p => `<div class="mrow"><div class="lab"><span class="mono">${esc(p.name)}</span><span class="mono">${fmtMem(p.memBytes)} · ${p.cpuMilli || 0}m</span></div><div class="barbg"><div class="barfill" style="width:${(p[barKey] / maxPod * 100).toFixed(0)}%;background:var(--info)"></div></div></div>`).join("");
  const recon = kusts.map(k => `<div class="kv"><span class="mono" style="display:flex;align-items:center;gap:8px"><span class="dot s-${k.ready}"></span>${esc(k.name)}</span><span class="mono" style="color:var(--mut)">${esc(k.ready)}${k.reconciledAt ? " · " + esc(k.reconciledAt) : ""}</span></div>`).join("");
  const podTitle = mc.sort === "cpu" ? "Top pods · cpu" : mc.sort === "name" ? "Pods · by name" : "Top pods · memory";
  const nsTitle = mc.sort === "cpu" ? "CPU by namespace" : mc.sort === "name" ? "Namespaces · by name" : "Memory by namespace";
  v.innerHTML = toolbar + (m.available ? "" : `<div class="warn">metrics-server unavailable — node/pod usage hidden.</div>`) + `<div class="grid">
    <div class="mcard gauge">${ring(node.cpuPct || 0, "#35d0c0")}<div><div class="h">Node CPU</div><div class="big">${node.cpuPct != null ? node.cpuPct + "%" : "—"}</div><div class="mono" style="font-size:11px;color:var(--mut)">${node.cpuMilli || 0}m / ${node.cpuCap || 0}m</div></div></div>
    <div class="mcard gauge">${ring(node.memPct || 0, "#35d0c0")}<div><div class="h">Node memory</div><div class="big">${node.memPct != null ? node.memPct + "%" : "—"}</div><div class="mono" style="font-size:11px;color:var(--mut)">${fmtMem(node.memBytes || 0)} / ${fmtMem(node.memCap || 0)}</div></div></div>
    <div class="mcard gauge">${ring(100, "#57d39a")}<div><div class="h">Pods running</div><div class="big">${m.podsReady || c.podsReady || 0}</div><div class="mono" style="font-size:11px;color:var(--ok)">${m.podsReady || 0} ready · ${m.podsTotal || 0} total</div></div></div>
    <div class="mcard"><div class="h">Cluster</div>
      <div class="kv"><span class="k2">k8s</span><span class="mono">${esc(c.version || "—")}</span></div>
      <div class="kv"><span class="k2">Namespaces</span><span class="mono">${c.namespaces || 0}</span></div>
      <div class="kv"><span class="k2">Apps</span><span class="mono">${c.apps || 0}</span></div>
      <div class="kv"><span class="k2">Flux</span><span class="mono" style="color:${c.fluxReady ? "var(--ok)" : "var(--warn)"}">${esc(c.fluxMsg || "")}</span></div></div>
    <div class="mcard wide"><div class="h">${esc(nsTitle)}</div><div style="margin-top:14px">${nsBars || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
    <div class="mcard wide"><div class="h">${esc(podTitle)}</div><div style="margin-top:14px">${podBars || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
    <div class="mcard wide"><div class="h">GitOps reconciliation</div><div style="margin-top:8px">${recon || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
  </div>`;
  wireMetrics();
}
function wireMetrics() {
  const ns = $("#mns");
  if (ns) ns.onchange = () => { state.metricsCtl.ns = ns.value; render(); };
  $$("#msort button").forEach(b => b.onclick = () => { state.metricsCtl.sort = b.dataset.sort; render(); });
  const rf = $("#mrefresh");
  if (rf) rf.onchange = () => { state.metricsCtl.interval = +rf.value; setPoll(+rf.value); };
}

/* ---------- TRAEFIK (routing page) ---------- */
function renderTraefik(v) {
  const t = state.traefik;
  if (!t) { v.innerHTML = `<div class="warn">loading Traefik routes…</div>`; return; }
  const eps = (t.entryPoints || []).join(" · ");
  const meta = [eps, t.tls].filter(Boolean).join(" · ");

  const band = `<div class="appband">
    <div class="tile">${icon("ingress", 22, "#35d0c0")}</div>
    <div>
      <div class="titlerow">
        <h1>Traefik</h1>
        <span class="badge ok"><span class="dot s-ok"></span>ingress</span>
        ${t.version ? `<span class="ver">${esc(vlabel(t.version).trim())}</span>` : ""}
      </div>
      <div class="meta">${esc(meta || "edge router · path-prefix routing")}</div>
    </div>
    <span class="grow"></span>
  </div>`;

  const routes = (t.routes || []).map((r, i) => {
    const mws = (r.middlewares || []).map(m => {
      const label = m.startsWith(r.app + "-") ? m.slice(r.app.length + 1) : m;
      return `<span class="mwchip">mw ${esc(label)}</span>`;
    }).join("");
    const portLabel = r.port ? ":" + r.port : (r.portName ? ":" + esc(r.portName) : "");
    const svc = r.service ? `→ svc ${esc(r.service)}${portLabel}` : "";
    return `<div class="troute ${r.owner ? "owner" : ""}" data-gid="r${i}" data-appname="${esc(r.app)}">
      <div class="trhead"><span class="tpath mono">${esc(r.path)}</span>${r.owner ? icon("chevron", 13, "#35d0c0") : ""}</div>
      <div class="trapp">${esc(r.app)}<span class="trns">ns ${esc(r.namespace || "")}</span></div>
      ${svc ? `<div class="trsvc mono">${svc}</div>` : ""}
      ${mws ? `<div class="mws">${mws}</div>` : ""}
      <div class="trep mono">${esc(r.entryPoint || "websecure")}${r.tls ? " · TLS" : ""}</div>
    </div>`;
  }).join("");

  v.innerHTML = band + `<div class="tfmain"><div id="tfwrap">
    <svg id="tfedges"></svg>
    <div class="tfgraph">
      <div class="tfsource">
        <div class="tfnet mono">Internet ↓</div>
        <div class="gnode teal" data-gid="tf">
          <div class="gk">${icon("ingress", 12, "#35d0c0")}ingress · Traefik</div>
          <div class="gn">Traefik</div>
          <div class="gs">${esc((t.entryPoints || ["websecure"]).join(" · "))}</div>
          ${t.tls ? `<div class="gs">${esc(t.tls)}</div>` : ""}
        </div>
        <div class="tfhint">TLS terminates here · routes by path prefix</div>
      </div>
      <div class="troutes">${routes || '<div class="warn">no ingress routes found.</div>'}</div>
    </div></div></div>`;

  $$("#tfwrap .troute.owner").forEach(el => el.addEventListener("click", () => openApp(el.dataset.appname)));
  requestAnimationFrame(drawTraefikEdges);
}
function drawTraefikEdges() {
  const wrap = $("#tfwrap"), svg = $("#tfedges");
  if (!wrap || !svg) return;
  const W = wrap.scrollWidth, H = wrap.scrollHeight;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`); svg.setAttribute("width", W); svg.setAttribute("height", H);
  const base = wrap.getBoundingClientRect();
  const pos = {};
  wrap.querySelectorAll("[data-gid]").forEach(el => {
    const r = el.getBoundingClientRect();
    pos[el.dataset.gid] = { x: r.left - base.left, y: r.top - base.top, w: r.width, h: r.height };
  });
  const tf = pos["tf"]; if (!tf) { svg.innerHTML = ""; return; }
  let paths = "";
  Object.keys(pos).filter(k => /^r\d+$/.test(k)).forEach(k => {
    const p = anchor(tf, pos[k]);
    paths += `<path d="M ${p.x1} ${p.y1} C ${p.cx1} ${p.cy1}, ${p.cx2} ${p.cy2}, ${p.x2} ${p.y2}" fill="none" stroke="#35d0c0" stroke-width="1.5" marker-end="url(#tf-ar)"/>`;
  });
  svg.innerHTML = `<defs><marker id="tf-ar" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M2 2 8 5 2 8" fill="none" stroke="#35d0c0" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></marker></defs>` + paths;
}

/* ---------- EVENTS (combined browser) ---------- */
let evTimer = null;
function renderEvents(v) {
  const e = state.events, f = state.eventsFilter;
  const nss = (e && e.namespaces) || [];
  const nsOpts = [`<option value="">all namespaces</option>`]
    .concat(nss.map(n => `<option ${f.ns === n ? "selected" : ""}>${esc(n)}</option>`)).join("");
  const kinds = ["", "Pod", "Deployment", "ReplicaSet", "Service", "IngressRoute", "HelmRelease", "HelmChart", "Kustomization", "GitRepository", "ImageRepository", "ImagePolicy"];
  const kindOpts = kinds.map(k => `<option value="${esc(k)}" ${f.kind === k ? "selected" : ""}>${k || "any kind"}</option>`).join("");
  const typeSeg = ["", "Normal", "Warning"].map(t => `<button data-type="${t}" class="${f.type === t ? "on" : ""}">${t || "all"}</button>`).join("");
  const count = e && !e.error ? `${e.capped ? e.events.length + " of " + e.total : e.total} events` : "";
  const toolbar = `<div class="evtoolbar">
    <select id="evns" title="Namespace">${nsOpts}</select>
    <div class="seg" id="evtype" role="group" aria-label="Event type">${typeSeg}</div>
    <select id="evkind" title="Involved object kind">${kindOpts}</select>
    <input id="evq" type="search" placeholder="search reason / message / object" value="${esc(f.q)}">
    <span class="grow"></span>
    <span class="evcount mono">${count}</span>
  </div>`;
  let list;
  if (!e) list = `<div class="warn">loading events…</div>`;
  else if (e.error) list = errBox("Events unavailable", "The console may lack the events read grant (core events: get, list).");
  else if (!e.events.length) list = emptyBox("No matching events", e.window || "");
  else list = `<div class="evscroll">${eventsListHTML(e.events, { showNs: true })}</div>`;
  // preserve the search caret across a poll re-render while the user is typing
  const active = document.activeElement;
  const keepFocus = active && active.id === "evq";
  const caret = keepFocus ? active.selectionStart : null;
  v.innerHTML = `<div class="evpage">${toolbar}${list}<div class="evfoot">${e && !e.error ? esc(e.window || "") : ""}${e && e.capped ? " · list capped — narrow with filters" : ""}</div></div>`;
  wireEvents(keepFocus, caret);
}
function wireEvents(keepFocus, caret) {
  const ns = $("#evns");
  if (ns) ns.onchange = () => { state.eventsFilter.ns = ns.value; render(); refresh(); };
  const kind = $("#evkind");
  if (kind) kind.onchange = () => { state.eventsFilter.kind = kind.value; render(); refresh(); };
  $$("#evtype button").forEach(b => b.onclick = () => { state.eventsFilter.type = b.dataset.type; render(); refresh(); });
  const q = $("#evq");
  if (q) {
    q.oninput = () => { state.eventsFilter.q = q.value; clearTimeout(evTimer); evTimer = setTimeout(refresh, 300); };
    if (keepFocus) { q.focus(); const n = caret == null ? q.value.length : caret; q.setSelectionRange(n, n); }
  }
}
