"use strict";
const $ = (s, r = document) => r.querySelector(s);
const h = (html) => { const t = document.createElement("template"); t.innerHTML = html.trim(); return t.content.firstElementChild; };
const esc = (s) => String(s == null ? "" : s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const api = (p, o) => fetch("./api" + p, Object.assign({ headers: { "Content-Type": "application/json" } }, o));

const state = { graph: null, metrics: null, view: "map" };
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
  document.querySelectorAll(".nav button").forEach(b => b.onclick = () => setView(b.dataset.view));
  window.addEventListener("resize", () => { if (state.view === "map") drawEdges(); });
  await refresh();
  pollTimer = setInterval(refresh, 15000);
}
function setView(v) {
  state.view = v;
  document.querySelectorAll(".nav button").forEach(b => b.classList.toggle("on", b.dataset.view === v));
  render();
}

async function refresh() {
  try {
    const [gr, mr] = await Promise.all([api("/graph"), api("/metrics")]);
    if (gr.status === 401 || mr.status === 401) { clearInterval(pollTimer); return showLogin(); }
    state.graph = await gr.json();
    state.metrics = await mr.json();
    renderHeader();
    render();
  } catch (e) { /* keep last view */ }
}

/* ---------- header ---------- */
function renderHeader() {
  const c = state.graph && state.graph.cluster || {};
  const ok = c.fluxReady;
  const hp = $("#health");
  hp.className = "chip " + (ok ? "ok" : "");
  hp.innerHTML = `<span class="dot ${ok ? "s-ok" : "s-progressing"}"></span>${ok ? "cluster healthy" : "attention"}`;
  const m = state.metrics && state.metrics.node || {};
  $("#nodestat").textContent = m.cpuPct != null ? `CPU ${m.cpuPct}%  MEM ${m.memPct}%` : "";
}

/* ---------- icons ---------- */
function icon(kind) {
  const P = { fill: "none", stroke: "currentColor", "stroke-width": "1.9", "stroke-linecap": "round", "stroke-linejoin": "round" };
  const s = Object.entries(P).map(([k, v]) => `${k}="${v}"`).join(" ");
  const w = `width="13" height="13" viewBox="0 0 24 24"`;
  const paths = {
    repo: `<circle cx="6" cy="6" r="2.2"/><circle cx="6" cy="18" r="2.2"/><path d="M6 8.2V15.8M6 12h7a4 4 0 0 0 4-4V6"/><circle cx="17" cy="4" r="2.2"/>`,
    image: `<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>`,
    actions: `<path d="M3 7l9-4 9 4-9 4z"/><path d="M3 12l9 4 9-4M3 17l9 4 9-4"/>`,
    flux: `<path d="M21 12a9 9 0 1 1-3-6.7M21 4v4h-4"/>`,
    controller: `<path d="M21 12a9 9 0 1 1-3-6.7M21 4v4h-4"/>`,
    kustomization: `<path d="M3 7l9-4 9 4-9 4z"/><path d="M3 12l9 4 9-4"/>`,
    helmRelease: `<path d="M3 7l9-4 9 4-9 4z"/><path d="M3 12l9 4 9-4M3 17l9 4 9-4"/>`,
    app: `<path d="M4 7h16M4 12h10M4 17h16"/><circle cx="18" cy="12" r="2"/>`,
    ingress: `<path d="M2 12h20M12 2c3 3 3 17 0 20M12 2c-3 3-3 17 0 20"/>`,
    infraTool: `<path d="M12 2 3 7v10l9 5 9-5V7z"/>`,
    default: `<rect x="4" y="4" width="16" height="16" rx="2"/>`,
  };
  return `<svg ${w} ${s}>${paths[kind] || paths.default}</svg>`;
}

/* ---------- render ---------- */
function render() {
  const v = $("#view");
  if (!state.graph) { v.innerHTML = `<div class="warn">loading…</div>`; return; }
  if (state.view === "map") renderMap(v); else renderMetrics(v);
}

const LANES = [["source", "Sources · GitHub"], ["build", "Build · Actions → GHCR"], ["gitops", "GitOps · Flux"], ["cluster", "Cluster · k3s"]];

function renderMap(v) {
  const g = state.graph;
  const c = g.cluster || {};
  const byLayer = { source: [], build: [], gitops: [], cluster: [] };
  g.nodes.forEach(n => (byLayer[n.layer] || byLayer.cluster).push(n));

  const ribbon = `<div class="ribbon">
    <span class="chip mono"><span class="dot s-ok"></span>${esc(c.version || "k3s")} · ${esc(c.node || "")}</span>
    <span class="chip">${c.namespaces || 0} namespaces</span>
    <span class="chip">${c.apps || 0} apps</span>
    <span class="chip">${c.podsReady || 0}/${c.podsTotal || 0} pods ready</span>
    <span class="chip ${c.fluxReady ? "ok" : ""}"><span class="dot ${c.fluxReady ? "s-ok" : "s-progressing"}"></span>Flux ${esc(c.fluxMsg || "")}</span>
    <span class="grow"></span>
    <span style="font-size:11px;color:var(--mut)">hover a component to trace its path · click an app for its k8s components</span>
  </div>`;

  const laneHTML = LANES.map(([layer, title]) => {
    let inner;
    if (layer === "cluster") inner = clusterCol(byLayer.cluster);
    else inner = `<div class="col">` + byLayer[layer].map(nodeHTML).join("") + `</div>`;
    return `<div class="lane"><div class="lh">${title}</div>${inner}</div>`;
  }).join("");

  v.innerHTML = ribbon + `<div id="mapwrap"><svg id="edges"></svg><div class="lanes">${laneHTML}</div></div>` + legendHTML();
  wireNodes();
  requestAnimationFrame(drawEdges);
}

function clusterCol(nodes) {
  const groups = {};
  const others = [];
  nodes.forEach(n => {
    if (n.kind === "ingress") { others.push(n); return; }
    const ns = n.namespace || "—";
    (groups[ns] = groups[ns] || []).push(n);
  });
  let html = `<div class="col">`;
  html += others.map(nodeHTML).join("");
  Object.keys(groups).sort().forEach(ns => {
    html += `<div class="nsgroup"><div class="nsh"><span>ns · ${esc(ns)}</span></div>` + groups[ns].map(nodeHTML).join("") + `</div>`;
  });
  html += `</div>`;
  return html;
}

function nodeHTML(n) {
  const cls = ["node", n.kind, n.owner ? "owner" : ""].join(" ");
  const st = n.statusText ? `<div class="st">${esc(n.statusText)}</div>` : (n.namespace && n.kind === "image" ? `<div class="st">${esc(n.namespace)}</div>` : "");
  return `<div class="${cls}" data-id="${esc(n.id)}" data-app="${esc(n.app || "")}">
    <div class="k">${icon(n.kind)}<span>${esc(labelKind(n.kind))}</span><span class="dot s-${n.status}" style="margin-left:auto"></span></div>
    <div class="nm ${n.kind === "image" || n.kind === "repo" ? "mono" : ""}">${esc(n.name)}</div>${st}
  </div>`;
}
function labelKind(k) {
  return ({ repo: "repo", image: "image", actions: "ci", flux: "flux", controller: "flux", kustomization: "kustomization", helmRelease: "helmRelease", app: "app", ingress: "ingress", infraTool: "infra tool" })[k] || k;
}
function legendHTML() {
  return `<div class="legend">
    <span><svg width="22" height="8"><path d="M1 4h20" stroke="#4a5566" stroke-width="1.5"/></svg> flow</span>
    <span><svg width="22" height="8"><path d="M1 4h20" stroke="#35d0c0" stroke-width="1.5"/></svg> deploy</span>
    <span><svg width="22" height="8"><path d="M1 4h20" stroke="#9b8cf0" stroke-width="1.4" stroke-dasharray="5 4"/></svg> auto-bump</span>
    <span><span class="dot s-ok"></span> ready</span><span><span class="dot s-progressing"></span> progressing</span><span><span class="dot s-failed"></span> failed</span>
  </div>`;
}

/* ---------- edges ---------- */
function edgeColor(kind) { return kind === "deploy" || kind === "route" || kind === "source" ? "#35d0c0" : kind === "watch" ? "#9b8cf0" : "#4a5566"; }
function drawEdges() {
  const wrap = $("#mapwrap"), svg = $("#edges"); if (!wrap || !svg) return;
  const W = wrap.scrollWidth, H = wrap.scrollHeight;
  svg.setAttribute("width", W); svg.setAttribute("height", H); svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  const base = wrap.getBoundingClientRect();
  const pos = {};
  wrap.querySelectorAll(".node").forEach(el => {
    const r = el.getBoundingClientRect();
    pos[el.dataset.id] = { x: r.left - base.left, y: r.top - base.top, w: r.width, h: r.height };
  });
  let paths = "";
  (state.graph.edges || []).forEach(e => {
    const a = pos[e.from], b = pos[e.to]; if (!a || !b) return;
    const fx = a.x + a.w, fy = a.y + a.h / 2, tx = b.x, ty = b.y + b.h / 2;
    const dash = e.kind === "watch" ? `stroke-dasharray="5 4"` : "";
    paths += `<path d="M ${fx} ${fy} C ${fx + 44} ${fy}, ${tx - 44} ${ty}, ${tx} ${ty}" fill="none" stroke="${edgeColor(e.kind)}" stroke-width="1.5" ${dash} data-from="${esc(e.from)}" data-to="${esc(e.to)}" opacity="0.9"/>`;
  });
  svg.innerHTML = paths;
}

/* ---------- hover highlight + card ---------- */
function adjacency() {
  const adj = {};
  (state.graph.edges || []).forEach(e => {
    (adj[e.from] = adj[e.from] || []).push(e.to);
    (adj[e.to] = adj[e.to] || []).push(e.from);
  });
  return adj;
}
function connected(id) {
  const adj = adjacency(), seen = new Set([id]), q = [id];
  while (q.length) { const x = q.shift(); (adj[x] || []).forEach(y => { if (!seen.has(y)) { seen.add(y); q.push(y); } }); }
  return seen;
}
function wireNodes() {
  const nodeById = {};
  state.graph.nodes.forEach(n => nodeById[n.id] = n);
  document.querySelectorAll("#mapwrap .node").forEach(el => {
    const id = el.dataset.id;
    el.addEventListener("mouseenter", () => highlight(id, el, nodeById[id]));
    el.addEventListener("mouseleave", clearHighlight);
    if (nodeById[id] && nodeById[id].kind === "app") el.addEventListener("click", () => openDrawer(nodeById[id]));
  });
}
function highlight(id, el, node) {
  const set = connected(id);
  document.querySelectorAll("#mapwrap .node").forEach(n => n.classList.toggle("dim", !set.has(n.dataset.id)));
  document.querySelectorAll("#edges path").forEach(p => {
    const on = set.has(p.dataset.from) && set.has(p.dataset.to);
    p.classList.toggle("dim", !on);
    if (on) p.setAttribute("stroke-width", "2.2"); else p.setAttribute("stroke-width", "1.5");
  });
  showHoverCard(el, node);
}
function clearHighlight() {
  document.querySelectorAll("#mapwrap .node,.dim").forEach(n => n.classList.remove("dim"));
  document.querySelectorAll("#edges path").forEach(p => { p.classList.remove("dim"); p.setAttribute("stroke-width", "1.5"); });
  $("#hover").classList.add("hide");
}
function chipHTML(l) {
  return `<span class="linkchip" data-url="${esc(l.url)}">${esc(({ source: "Source", workflow: "Workflow", config: "Config", image: "Image", docs: "Docs" })[l.type] || l.type)}
    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7M8 7h9v9"/></svg></span>`;
}
function showHoverCard(el, n) {
  if (!n) return;
  const card = $("#hover");
  const links = (n.links || []).map(chipHTML).join("");
  card.innerHTML = `<div class="ttl"><span class="name">${esc(n.name)}</span><span class="badge">${n.owner ? "your service" : "infra tool"}</span></div>
    ${n.statusText ? `<div class="sub">${esc(n.statusText)}${n.meta && n.meta.image ? " · " + esc(n.meta.image) : ""}</div>` : ""}
    ${n.summary ? `<div class="expl">${esc(n.summary)}</div>` : ""}
    ${links ? `<div class="chips">${links}</div>` : ""}`;
  card.querySelectorAll(".linkchip").forEach(c => c.onclick = () => window.open(c.dataset.url, "_blank", "noopener"));
  card.classList.remove("hide");
  const r = el.getBoundingClientRect();
  let x = r.right + 12, y = r.top;
  const cw = 320, ch = card.offsetHeight || 200;
  if (x + cw > window.innerWidth - 10) x = r.left - cw - 12;
  if (y + ch > window.innerHeight - 10) y = window.innerHeight - ch - 10;
  card.style.left = Math.max(10, x) + "px";
  card.style.top = Math.max(66, y) + "px";
}

/* ---------- drawer (app drill-in) ---------- */
function openDrawer(app) {
  const g = state.graph;
  const parts = g.nodes.filter(n => n.app === app.app);
  const order = { repo: 0, image: 1, helmRelease: 2, app: 3 };
  parts.sort((a, b) => (order[a.kind] ?? 9) - (order[b.kind] ?? 9));
  const meta = app.meta || {};
  const kv = (k, val) => val ? `<div class="kv"><span class="k2">${esc(k)}</span><span class="mono">${esc(val)}</span></div>` : "";
  const comps = parts.map(n => `<div class="rescard">
      <div class="k">${icon(n.kind)}<span>${esc(labelKind(n.kind))}</span><span class="dot s-${n.status}" style="margin-left:auto"></span></div>
      <div class="nm">${esc(n.name)}</div>
      ${(n.links || []).length ? `<div class="chips" style="margin-top:8px">${(n.links || []).map(chipHTML).join("")}</div>` : ""}
    </div>`).join("");
  const d = $("#drawer");
  d.innerHTML = `<div class="dh"><span class="k" style="font-size:10px;letter-spacing:.6px;color:var(--mut)">APP · ${esc(app.namespace)}</span>
      <button class="iconbtn" id="dclose"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18"/></svg></button></div>
    <h2>${esc(app.name)} <span class="dot s-${app.status}"></span></h2>
    <div class="mono" style="font-size:11.5px;color:var(--dim)">${esc(app.statusText || "")}${meta.route ? " · " + esc(meta.route) : ""}</div>
    <div class="rescard">${kv("Image", meta.image)}${kv("Replicas", meta.replicas)}${kv("Strategy", meta.strategy)}${kv("Route", meta.route)}</div>
    <div style="font-size:10px;letter-spacing:.6px;text-transform:uppercase;color:var(--mut);margin:16px 0 2px">components &amp; sources</div>
    ${comps}`;
  d.querySelector("#dclose").onclick = () => d.classList.remove("open");
  d.classList.add("open");
}

/* ---------- metrics ---------- */
function ring(pct, color) {
  const c = 188.5, dash = Math.max(0, Math.min(100, pct)) / 100 * c;
  return `<svg width="76" height="76" viewBox="0 0 76 76"><circle cx="38" cy="38" r="30" fill="none" stroke="#1b2028" stroke-width="8"/>
    <circle cx="38" cy="38" r="30" fill="none" stroke="${color}" stroke-width="8" stroke-linecap="round" stroke-dasharray="${dash} ${c}" transform="rotate(-90 38 38)"/></svg>`;
}
function fmtMem(b) { const g = b / 1073741824; return g >= 1 ? g.toFixed(2) + " GiB" : (b / 1048576).toFixed(0) + " Mi"; }
function renderMetrics(v) {
  const m = state.metrics || {}, node = m.node || {}, g = state.graph || {}, c = g.cluster || {};
  if (!m.available) { v.innerHTML = `<div class="warn">metrics-server did not answer — showing cluster facts only.</div>`; }
  const kusts = (g.nodes || []).filter(n => n.kind === "kustomization");
  const maxMem = Math.max(1, ...(m.namespaces || []).map(n => n.memBytes));
  const maxPod = Math.max(1, ...(m.pods || []).map(p => p.memBytes));
  const nsBars = (m.namespaces || []).map(n => `<div class="mrow"><div class="lab"><span>${esc(n.name)}</span><span class="mono">${fmtMem(n.memBytes)} · ${n.pods}p</span></div><div class="barbg"><div class="barfill" style="width:${(n.memBytes / maxMem * 100).toFixed(0)}%"></div></div></div>`).join("");
  const podBars = (m.pods || []).map(p => `<div class="mrow"><div class="lab"><span class="mono">${esc(p.name)}</span><span class="mono">${fmtMem(p.memBytes)}</span></div><div class="barbg"><div class="barfill" style="width:${(p.memBytes / maxPod * 100).toFixed(0)}%;background:var(--info)"></div></div></div>`).join("");
  const recon = kusts.map(k => `<div class="kv"><span class="mono" style="display:flex;align-items:center;gap:8px"><span class="dot s-${k.status}"></span>${esc(k.name)}</span><span class="mono" style="color:var(--mut)">${esc(k.status)}</span></div>`).join("");
  v.innerHTML = (m.available ? "" : `<div class="warn">metrics-server unavailable — node/pod usage hidden.</div>`) + `<div class="grid">
    <div class="card gauge">${ring(node.cpuPct || 0, "#35d0c0")}<div><div class="h">Node CPU</div><div class="big">${node.cpuPct != null ? node.cpuPct + "%" : "—"}</div><div class="mono" style="font-size:11px;color:var(--mut)">${node.cpuMilli || 0}m / ${node.cpuCap || 0}m</div></div></div>
    <div class="card gauge">${ring(node.memPct || 0, "#35d0c0")}<div><div class="h">Node memory</div><div class="big">${node.memPct != null ? node.memPct + "%" : "—"}</div><div class="mono" style="font-size:11px;color:var(--mut)">${fmtMem(node.memBytes || 0)} / ${fmtMem(node.memCap || 0)}</div></div></div>
    <div class="card gauge">${ring(100, "#57d39a")}<div><div class="h">Pods running</div><div class="big">${m.podsReady || c.podsReady || 0}</div><div class="mono" style="font-size:11px;color:var(--ok)">${m.podsReady || 0} ready · ${m.podsTotal || 0} total</div></div></div>
    <div class="card"><div class="h">Cluster</div>
      <div class="kv"><span class="k2">k8s</span><span class="mono">${esc(c.version || "—")}</span></div>
      <div class="kv"><span class="k2">Namespaces</span><span class="mono">${c.namespaces || 0}</span></div>
      <div class="kv"><span class="k2">Apps</span><span class="mono">${c.apps || 0}</span></div>
      <div class="kv"><span class="k2">Flux</span><span class="mono" style="color:${c.fluxReady ? "var(--ok)" : "var(--warn)"}">${esc(c.fluxMsg || "")}</span></div></div>
    <div class="card wide"><div class="h">Memory by namespace</div><div style="margin-top:14px">${nsBars || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
    <div class="card wide"><div class="h">Top pods · memory</div><div style="margin-top:14px">${podBars || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
    <div class="card wide"><div class="h">GitOps reconciliation</div><div style="margin-top:8px">${recon || '<div class="mono" style="color:var(--mut)">—</div>'}</div></div>
  </div>`;
}
