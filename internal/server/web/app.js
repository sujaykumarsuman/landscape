"use strict";
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
const esc = (s) => String(s == null ? "" : s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const api = (p, o) => fetch("./api" + p, Object.assign({ headers: { "Content-Type": "application/json" } }, o));
const cls = (st) => st === "ok" ? "ok" : st === "failed" ? "err" : "warn";

const state = { graph: null, metrics: null, view: "map", appName: null, app: null };
let pollTimer = null;

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
  $("#login").classList.add("hide");
  $("#app").classList.remove("hide");
  $("#logout").onclick = async () => { await api("/logout", { method: "POST" }); location.reload(); };
  $$(".nav button").forEach(b => b.onclick = () => setView(b.dataset.view));
  $("#crumb .home").onclick = () => setView("map");
  window.addEventListener("resize", () => { if (state.view === "app") drawAppEdges(); });
  await refresh();
  pollTimer = setInterval(refresh, 15000);
}
function setView(v) {
  state.view = v;
  if (v !== "app") { state.appName = null; state.app = null; }
  $("#hover").classList.add("hide");
  syncChrome();
  render();
  refresh();
}
function openApp(name) {
  state.view = "app"; state.appName = name; state.app = null;
  $("#hover").classList.add("hide");
  syncChrome();
  render();
  refresh();
}
function syncChrome() {
  const inApp = state.view === "app";
  $(".nav").classList.toggle("hide", inApp);
  $("#crumb").classList.toggle("hide", !inApp);
  if (inApp) $("#crumb .cur").textContent = state.appName || "";
  $$(".nav button").forEach(b => b.classList.toggle("on", b.dataset.view === state.view));
}

async function refresh() {
  try {
    if (state.view === "app" && state.appName) {
      const [ar, mr] = await Promise.all([api("/app/" + encodeURIComponent(state.appName)), api("/metrics")]);
      if (ar.status === 401) { clearInterval(pollTimer); return showLogin(); }
      if (ar.ok) state.app = await ar.json();
      if (mr.ok) state.metrics = await mr.json();
    } else {
      const [gr, mr] = await Promise.all([api("/graph"), api("/metrics")]);
      if (gr.status === 401 || mr.status === 401) { clearInterval(pollTimer); return showLogin(); }
      state.graph = await gr.json();
      state.metrics = await mr.json();
    }
    renderHeader();
    render();
  } catch (e) { /* keep last view */ }
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
      <div class="mono" style="font-size:11.5px">${esc(nm)}</div>
      <div class="mono" style="font-size:11px;color:var(--teal);margin-top:2px">:${esc(tag || "")}</div></div>`;
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
  const kusts = (go.kustomizations || []).map(k => `<div class="kr"><span class="dot s-${k.ready}"></span><span class="mono">${esc(k.name)}</span>${k.reconciledAt ? `<span class="ago">${esc(k.reconciledAt)}</span>` : ""}</div>`).join("");
  const gitopsInner = `
    <div class="card"><div class="kind">controllers</div><div class="ctrls">${ctrlChips}</div></div>
    <div class="card"><div class="kind">kustomizations</div><div class="kustlist">${kusts || '<span class="mono" style="color:var(--mut);font-size:11px">—</span>'}</div></div>
    <div class="card accent-violet">
      <div class="row1">${icon("flux", 14, "#9b8cf0")}<span style="font-size:12.5px;color:#c7bdf7">image-automation</span></div>
      <div class="mono" style="font-size:10.5px;color:var(--mut);margin-top:4px">watches GHCR → bumps tag → commits</div>
    </div>`;
  const lane3 = lane("gitops", "GitOps · Flux" + (go.version ? " v" + go.version : ""), gitopsInner);

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
      <div class="traefik-rail">${icon("ingress", 18, "#35d0c0")}<div class="vt">Traefik · :443 TLS</div></div>
      <div class="clustercol">
        <div class="nsrow">${nsBoxes}</div>
        <div class="platform-box"><span class="kind" style="color:var(--dim)">platform namespaces</span><div class="platform-grid">${platCards}</div></div>
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

/* map interactions: hover flow-highlight + click to open app */
function wireMap(byId) {
  const map = $(".map");
  $$(".map [data-id]").forEach(el => {
    const node = byId[el.dataset.id];
    el.addEventListener("mouseenter", () => {
      const app = el.dataset.app;
      if (app) {
        map.classList.add("tracing");
        $$(`.map [data-app="${cssq(app)}"]`).forEach(x => x.classList.add("trace"));
      }
      if (node) showHoverCard(el, node);
    });
    el.addEventListener("mouseleave", clearTrace);
  });
  $$(".map .app-card").forEach(el => el.addEventListener("click", () => openApp(el.dataset.appname)));
}
function cssq(s) { return String(s).replace(/"/g, '\\"'); }
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
  if (!d) { v.innerHTML = `<div class="warn">loading ${esc(state.appName || "")}…</div>`; return; }
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
    <div class="tabs"><span class="on">Graph</span><span>Events</span><span>Logs</span></div>
  </div>`;

  v.innerHTML = band + `<div class="appmain"><div id="graphwrap">${appGraphHTML(d)}</div>${appRailHTML(d)}</div>`;
  wireApp(d);
  requestAnimationFrame(drawAppEdges);
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
      <div class="msub">${kust ? "Kustomization " + esc(kust.name) : "Kustomization apps"}${hr && hr.chart ? " · chart " + esc(hr.chart) : ""}</div>
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
      <button class="rb" disabled>${icon("clock", 14)} View logs</button>
      <button class="rb" disabled>Events</button>
    </div>
  </div>`;
}

/* spark: a truthful minimal chart — flat line at the current level (no history collected) */
function spark(frac, color) {
  frac = Math.max(0, Math.min(1, frac));
  const y = (28 - frac * 24).toFixed(1);
  return `<svg class="spark" viewBox="0 0 308 30" preserveAspectRatio="none"><line x1="0" y1="29" x2="308" y2="29" stroke="#1b2028" stroke-width="1"/><polyline points="0,${y} 308,${y}" fill="none" stroke="${color}" stroke-width="1.5"/></svg>`;
}

function wireApp(d) {
  $$(".appband .tabs span:not(.on)").forEach(s => s.title = "not yet available");
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
function renderMetrics(v) {
  const m = state.metrics || {}, node = m.node || {}, g = state.graph || {}, c = g.cluster || {};
  const kusts = (c.gitops && c.gitops.kustomizations) || (g.nodes || []).filter(n => n.kind === "kustomization").map(n => ({ name: n.name, ready: n.status }));
  const maxMem = Math.max(1, ...(m.namespaces || []).map(n => n.memBytes));
  const maxPod = Math.max(1, ...(m.pods || []).map(p => p.memBytes));
  const nsBars = (m.namespaces || []).map(n => `<div class="mrow"><div class="lab"><span>${esc(n.name)}</span><span class="mono">${fmtMem(n.memBytes)} · ${n.pods}p</span></div><div class="barbg"><div class="barfill" style="width:${(n.memBytes / maxMem * 100).toFixed(0)}%"></div></div></div>`).join("");
  const podBars = (m.pods || []).map(p => `<div class="mrow"><div class="lab"><span class="mono">${esc(p.name)}</span><span class="mono">${fmtMem(p.memBytes)}</span></div><div class="barbg"><div class="barfill" style="width:${(p.memBytes / maxPod * 100).toFixed(0)}%;background:var(--info)"></div></div></div>`).join("");
  const recon = kusts.map(k => `<div class="kv"><span class="mono" style="display:flex;align-items:center;gap:8px"><span class="dot s-${k.ready}"></span>${esc(k.name)}</span><span class="mono" style="color:var(--mut)">${esc(k.ready)}${k.reconciledAt ? " · " + esc(k.reconciledAt) : ""}</span></div>`).join("");
  v.innerHTML = (m.available ? "" : `<div class="warn">metrics-server unavailable — node/pod usage hidden.</div>`) + `<div class="grid">
    <div class="mcard gauge">${ring(node.cpuPct || 0, "#35d0c0")}<div><div class="h">Node CPU</div><div class="big">${node.cpuPct != null ? node.cpuPct + "%" : "—"}</div><div class="mono" style="font-size:11px;color:var(--mut)">${node.cpuMilli || 0}m / ${node.cpuCap || 0}m</div></div></div>
    <div class="mcard gauge">${ring(node.memPct || 0, "#35d0c0")}<div><div class="h">Node memory</div><div class="big">${node.memPct != null ? node.memPct + "%" : "—"}</div><div class="mono" style="font-size:11px;color:var(--mut)">${fmtMem(node.memBytes || 0)} / ${fmtMem(node.memCap || 0)}</div></div></div>
    <div class="mcard gauge">${ring(100, "#57d39a")}<div><div class="h">Pods running</div><div class="big">${m.podsReady || c.podsReady || 0}</div><div class="mono" style="font-size:11px;color:var(--ok)">${m.podsReady || 0} ready · ${m.podsTotal || 0} total</div></div></div>
    <div class="mcard"><div class="h">Cluster</div>
      <div class="kv"><span class="k2">k8s</span><span class="mono">${esc(c.version || "—")}</span></div>
      <div class="kv"><span class="k2">Namespaces</span><span class="mono">${c.namespaces || 0}</span></div>
      <div class="kv"><span class="k2">Apps</span><span class="mono">${c.apps || 0}</span></div>
      <div class="kv"><span class="k2">Flux</span><span class="mono" style="color:${c.fluxReady ? "var(--ok)" : "var(--warn)"}">${esc(c.fluxMsg || "")}</span></div></div>
    <div class="mcard wide"><div class="h">Memory by namespace</div><div style="margin-top:14px">${nsBars || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
    <div class="mcard wide"><div class="h">Top pods · memory</div><div style="margin-top:14px">${podBars || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
    <div class="mcard wide"><div class="h">GitOps reconciliation</div><div style="margin-top:8px">${recon || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
  </div>`;
}

/* ---------- EVENTS (placeholder) ---------- */
function renderEvents(v) {
  v.innerHTML = `<div class="empty"><div class="box">
    ${icon("clock", 30, "#616b7a")}
    <h3>Events stream</h3>
    <p>Kubernetes events and Flux reconciliation history aren't wired into the console yet. For now, per-app status and reconcile times live on each app's page, and GitOps reconciliation is on the Metrics tab.</p>
  </div></div>`;
}
