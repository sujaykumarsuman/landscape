# Prompt 002 — Events browser, logs viewer, and Metrics controls

> Read `CLAUDE.md`, `docs/ARCHITECTURE.md`, `docs/DEPLOY.md`, `docs/GIT-STRATEGY.md`
> and `STATUS.md` first. Honour the read-only invariant. Ship in small PRs
> (branch → green `make lint test` → PR → adversarial review → squash-merge →
> tag → GitOps deploy → verify live), each verified in-browser at desktop +
> phone width.

## Goal

Give the console **observability**: Kubernetes Events and pod Logs (per-app and
combined), plus interactive controls on the Metrics tab. Everything read-only.

## Scope

### 1. Events browser (three surfaces, one data source)
- **Per-app** — wire the existing **Events** tab in the app-detail header
  (`renderApp`; today Graph/Events/Logs are static spans). Show Events involving
  that app's Deployment → ReplicaSet → Pods (and its Service/IngressRoute),
  newest first, with Type (Normal/Warning), Reason, Message, count, age, source.
- **Top-right menu** — an events/logs affordance near the admin profile (a small
  dropdown or icon buttons) for quick access to the combined browser / recent
  warnings from anywhere.
- **Combined browser** — make the top-nav **Events** tab (currently a
  placeholder `renderEvents`) a real browser: all cluster Events with **filters**
  (namespace, Type Normal/Warning, involved-object kind) and **free-text
  search** over reason/message/object, newest first, paginated/capped.

### 2. Logs viewer (app view)
- Wire the app-detail **Logs** tab: pod stdout for the app. Container picker when
  the pod has >1 container; tail-N with a "load more"/refresh; wrap/scroll; mono
  font; clear Warning/Error emphasis if cheap. Start with tail-on-demand; add
  live follow (SSE) only if it stays simple.

### 3. Metrics tab controls
- Add a small toolbar to `renderMetrics`: namespace filter, sort (mem/cpu/name)
  for the namespace + top-pods lists, and a refresh-interval / pause control.
  Optionally a compact time context. Keep it read-only and fast.

## Non-goals (for this prompt)
- No writing to the cluster (no deletes, no cordon, nothing). No exec/attach.
- No log persistence/storage in landscape (stream/tail live only).
- No metrics history store (metrics-server is instantaneous) — don't fake trends.

## Architecture guidance

### Endpoints (server.go, all behind the auth cookie, cached where sensible)
- `GET /api/app/{name}/events` → the app's events.
- `GET /api/app/{name}/logs?container=&tail=&since=` → pod logs (tail-on-demand).
  If adding follow, a separate `GET /api/app/{name}/logs/stream` as **SSE**
  (the server has no streaming yet — add a small SSE writer; the browser reads it
  with a streaming `fetch`, mirroring how airlift does SSE).
- `GET /api/events?ns=&type=&kind=&q=&limit=&cursor=` → combined browser.
- Keep the ~10s cache for non-streaming reads; **do not cache** streams.

### Collector (internal/collect)
- `AppEvents(ctx, ns, name)`: list core Events in the ns and keep those whose
  `involvedObject` is the Deployment, its ReplicaSet(s), its Pods, Service, or
  IngressRoute (reuse the ownership logic already in `AppDetail`).
- `Events(ctx, filter)`: list Events (optionally all namespaces), apply
  ns/type/kind/text filters server-side, sort by lastTimestamp desc, cap.
- `AppLogs(ctx, ns, name, opts)`: pick the app's pod(s) via the
  `app.kubernetes.io/instance=<name>` selector (as `AppDetail` does), then
  `Typed.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{Container, TailLines,
  SinceSeconds, Follow})`. For follow, `.Stream(ctx)` and copy to the SSE writer;
  for tail, `.Do(ctx).Raw()`. **Cap TailLines** (e.g. default 500, max ~5000).

### Model (internal/model)
- `Event{Namespace, InvolvedKind, InvolvedName, Reason, Type, Message, Count,
  FirstSeen, LastSeen, Component}` (+ a relative-age helper like `relAge`).
- Logs: stream raw text lines, or `LogLine{Ts?, Text}`; keep it simple.

### RBAC (infra `apps/landscape.yaml`, chart `project` `rbac.clusterRole.rules`)
This work **requires new read grants** — land them in an infra PR before/with the
tag (see `docs/DEPLOY.md`), and call them out:
- core `events` — `get, list` (add `watch` only if you implement live events).
- core `pods/log` — `get` (the log subresource).
These are reads of Events objects and pod stdout — **not** Secret/ConfigMap
contents, so the invariant holds. Note: pod logs are the app's own output and
could contain data the app logged; the console is already admin-gated. Still, do
not add `secrets`, `configmaps`, `exec`, or any write verb.

### Frontend (web/app.js, style.css)
- App-detail tabs become interactive: track `state.appTab` (`graph`|`events`|
  `logs`); clicking a tab re-renders the app view's body; keep the header + right
  rail. Fetch the tab's data on switch + poll while active.
- Top-right menu: a dropdown next to `.profile` (keep it keyboard-accessible).
- Combined Events view: filter chips + a search input + a capped, scrollable
  list; reuse `.chip`, mono, and the status dot styles. Debounce search.
- Metrics toolbar: plain controls bound to `state`; re-render on change.
- Reuse existing tokens/icons; no new deps/CDN; verify no horizontal overflow.

## Suggested slices (one PR each)
1. **Events data + per-app Events tab** (endpoint + collector + app-detail tab)
   + the infra RBAC PR for `events`.
2. **Combined Events browser** (nav tab) with filters + search.
3. **Logs viewer** (app-detail Logs tab, tail-on-demand) + infra RBAC PR for
   `pods/log`. (Add SSE follow as a follow-up slice only if wanted.)
4. **Top-right events/logs menu.**
5. **Metrics tab controls.**

## Acceptance
- Each surface shows real cluster data live; empty/timeout states handled (the
  API keeps only ~1h of Events by default — label the window, don't imply more).
- Read-only invariant intact; only `events` + `pods/log` added to RBAC.
- `make lint test` green; CI green; no horizontal page scroll at any width;
  verified live via `/api/info` on the new tag after GitOps rollout.

## References
- Ownership + pod-selector logic to reuse: `internal/collect/app.go`
  (`AppDetail`, `podUsage`).
- Auth/cache/handler patterns: `internal/server/server.go`.
- View/tab/poll/edge patterns: `internal/server/web/app.js`.
- SSE reference implementation: the airlift tower (batched POST up, SSE down).
