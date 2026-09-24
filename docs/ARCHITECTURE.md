# Architecture

`landscape` is a single Go binary (module `github.com/sujaykumarsuman/landscape`,
Go 1.26) that reads the cluster read-only and serves a graph model + an embedded
UI. No database, no write path, no external calls at runtime (deep-links are just
URLs rendered in the browser).

```
browser ──HTTP──▶ server (auth gate, caches, embedded UI)
                     │
                     ▼
                  collector ──client-go──▶ Kubernetes API (read-only SA)
                     │                      • typed:   nodes/namespaces/pods/services/pvc, deployments/replicasets
                     │                      • dynamic: Flux CRDs, Traefik IngressRoutes
                     │                      • metrics: metrics.k8s.io
                     ▼
                  model (JSON) ──▶ web/app.js renders views
```

## Packages

### `internal/kube`
`New()` builds `Clients{Typed, Dynamic, Metrics}`. In-cluster config first, then
`KUBECONFIG` / `~/.kube/config` (the local dev loop). Metrics client is optional
(nil ⇒ "metrics-server unavailable", handled gracefully).

### `internal/collect`
The read side. A `Collector{c *kube.Clients, githubOwner}`.

- `Graph(ctx) *model.Graph` (`collect.go`): lists nodes/namespaces/deployments/
  pods + the Flux CRDs (dynamic) + IngressRoutes, and builds:
  - **Nodes** in four layers (`source`/`build`/`gitops`/`cluster`) — repos,
    images, GitHub Actions, Flux + controllers + kustomizations + HelmReleases,
    apps, Traefik. An app is "owned" when its image is `ghcr.io/<githubOwner>/…`.
  - **Edges** (`flow`/`deploy`/`route`/`watch`/`owns`) between them, tagged by
    `app` for the hover/pin highlight.
  - **Cluster roll-ups** via `clusterRollup` (`app.go`): per-namespace summaries
    (app vs platform), GitOps controllers + Flux version + kustomizations (with
    repo-folder deep-links), node capacity, TLS descriptor, Flux recency.
- `AppDetail(ctx, name) *model.AppDetail` (`app.go`): the per-app component graph
  — Deployment (+resources, strategy, restarts), owning ReplicaSet (revision),
  Pods (+live usage from metrics), Service, enriched IngressRoute
  (path/entrypoint/tls/middlewares), and the ConfigMap/Secret/PVC **names** from
  the pod spec (see the invariant), plus AppGitOps (HelmRelease chart+ready,
  owning Kustomization, image-automation). PVC size/access is a safe object read.
- `Traefik(ctx) *model.TraefikInfo` (`app.go`): every IngressRoute path→service
  mapping with entrypoints/TLS/middlewares/port, owner flag, and namespace
  (honouring cross-namespace `services[].namespace` and named string ports).
- `Metrics(ctx) *model.Metrics` (`metrics.go`): node + per-namespace + top-pod
  usage from metrics-server, plus pod readiness counts (returns up to 50 pods so
  the Metrics tab can sort/filter client-side).
- `events.go` — the observability reads (all read-only): `Events(ctx, filter)`
  lists cluster Events (all namespaces, then ns/type/kind/text filters in memory,
  newest-first, capped); `AppEvents(ctx, name)` keeps the Events whose
  involvedObject is the app's Deployment/ReplicaSet(s)/Pods/Service/IngressRoute
  (same instance-label ownership as `AppDetail`); `AppLogs(ctx, name, opts)` tails
  a pod's stdout via `pods/log` (`GetLogs(...).DoRaw`, container picker,
  `TailLines` capped 500…5000). Event timestamps tolerate both classic
  (`first/lastTimestamp`) and series/`eventTime` shapes; `relAgeTime` is shared
  with `relAge`.
- `links.go`: `parseImage`, `parseGitURL`, `ghLinks` (Source/Workflow/Image/
  Config deep-links), and the infra-tool docs map. Helpers `kustURL`/`trimPath`
  build the repo-folder links for kustomizations. **Source grouping is
  label-driven** (not a hardcoded map): `resolveSource` reads
  `app.kubernetes.io/part-of` (the app group / Sources-lane dedup key + default
  source repo), `app.kubernetes.io/component` (the granular component), and the
  exception pair `sujaykumar.dev/source-repo`/`-subdir` (only when the image is
  built from a differently-named repo or a subdir — `projects-hub`). Unlabeled
  workloads fall back to the image name. Apps set these via the shared
  `charts/project` values in the infra repo, so a new multi-service app groups with
  no landscape change.

GVRs (top of `collect.go`): `helm.toolkit.fluxcd.io/v2 helmreleases`,
`kustomize.toolkit.fluxcd.io/v1 kustomizations`,
`source.toolkit.fluxcd.io/v1 gitrepositories`,
`image.toolkit.fluxcd.io/v1 imagerepositories` (+ `imagepolicies` in `app.go`),
`traefik.io/v1alpha1 ingressroutes`. **Flux 2.9 serves image APIs as `v1`, not
`v1beta2`.**

- `longhorn.go` `Longhorn(ctx)`: the Longhorn page — `longhorn.io/v1beta2`
  volumes (anchor read: NotFound ⇒ not installed, Forbidden ⇒ grant missing),
  engines (healthy replicas = `RW` in `replicaModeMap`), snapshots (count per
  volume), nodes (`spec.disks` + `status.diskStatus` → capacity rolled into the
  summary), recurring jobs, the backup target (URL userinfo password redacted),
  engine image version and the `default-replica-count` setting; volumes are joined
  to their PVC and, via `claimApps`, to the Deployment mounting it (the app page).
  `UIPath` is the IngressRoute path to `longhorn-frontend`.

### `internal/model`
Plain JSON structs the UI consumes. `model.go`: `Graph`, `Node`, `Edge`,
`Cluster` (+ `NsSummary`, `GitOpsInfo`), `Metrics`. `app.go`: `AppDetail` and its
detail types (`DeploymentDetail`, `ReplicaSetDetail`, `PodDetail`,
`ServiceDetail`, `IngressDetail`, `RefName`, `AppGitOps`, `HRDetail`,
`KustDetail`), `TraefikInfo`/`TraefikRoute`. `events.go`: `Event`, `EventList`
(events-browser payload, carrying the ~1h retention `Window` note) and `Logs` (a
tail of pod stdout — lines + container/pod pickers).

### `internal/server`
`Options{Addr, AdminPassword, GithubOwner, PublicURL, Version, CacheTTL}`.
`Handler()` wires routes; `staticHandler()` serves the embedded `web/` FS and
falls back to `index.html` for unknown non-API paths so the client-side router
handles deep-links (`/landscape/app/<app>`, `/landscape/longhorn`…) — the shell
gets a `<base href>` for the mount (`shell`, from `PublicURL`'s path) so its
relative URLs resolve from nested routes; real assets serve
as files and unknown `/api/*` paths 404. Auth: `authed(r)` validates the
`ls_session` cookie as `v2.<expiry>.<hex HMAC-SHA256(signKey, "v2.<expiry>")>`
(`mintToken`/`validToken`), where `signKey` = HMAC(`LANDSCAPE_SESSION_KEY`,
PBKDF2-SHA256(pw, 600k)) is derived once in `New` (`signingKey`) — stateless ⇒
survives restarts, expires server-side after `sessionTTL` (12 h, also the cookie
`MaxAge`), rejects far-future and non-canonical expiries, and a leaked token is
not a cheap offline oracle for the password. `forwardAuthH` (`GET /api/forward-auth`) is the Traefik
ForwardAuth target for the gated UIs (Longhorn, kubescope): 204 when authed, else
302 → the **absolute** login URL (`loginURL`: `PublicURL`, else Traefik's
`X-Forwarded-Proto/Host`; Traefik would resolve a relative one against the auth
address) with `?next=<X-Forwarded-Uri>` for page navigations (`wantsHTML`:
`Sec-Fetch-Mode: navigate`, else `Accept: text/html`, GET/HEAD only) and 401 for
everything else; `safeNext` keeps `next` a same-host path. `Tools` (from
`LANDSCAPE_TOOLS`, `ParseTools`) are returned by `/api/session` when signed in. Read endpoints have small `s.mu`-guarded caches with `CacheTTL`
(default 10 s): `graph`, `metrics`, `apps[name]`, `traefik`, and a keyed `misc`
cache for `events` / per-app events. Logs are **not** cached (each request is a
fresh tail). `PublicURL`'s host fills per-route/app public URLs.

### `internal/server/web`
The UI, embedded via `//go:embed web`. `index.html` = shell + login. `style.css`
= one dark theme (tokens at `:root`; responsive: 4 lanes → stacked at 1080px,
top bar compacts at 640px, `#view` is `overflow-x:hidden`). `app.js` (vanilla,
no deps) = auth flow, a History-API router, an adjustable poll (default 15 s), and
the render functions per view:
- A **client-side router** gives each view a real URL under the mount prefix:
  `currentBase` reads the mount from `<base href>`; `routeFromURL`/`pathFor` map
  `…/` → map, `…/<page>` (reserved `metrics`/`events`/`traefik`/`storage`/
  `longhorn`) and `…/app/<name>` → app; a legacy single-segment `…/<name>` is an
  app and is canonicalised to `…/app/<name>` on load/popstate. `navigate` pushes
  history on nav, and `popstate` syncs back/forward.
- `renderLonghorn` — tiles (volume health, provisioned vs capacity, nodes,
  backups), a volumes table (PVC → app link, size written/provisioned, healthy
  replicas, state/node, robustness, snapshots, last backup, deep-link to the
  Longhorn UI volume), node disk bars, recurring jobs; `renderStorage` adds a
  Longhorn card (`longhornCardHTML`) and a per-PVC Longhorn health column.
- `renderMap` — the 4 boxed lanes + flow-gap arrows + cluster box; `wireMap`
  binds hover-trace + pin + Traefik-rail click; `applyTrace`/`pinApp`/`unpin`.
- `renderApp` — header with interactive **Graph / Events / Logs** tabs
  (`state.appTab`, fetched on switch + polled while active, header + right rail
  kept), the component graph (`appGraphHTML` + `drawAppEdges`), the per-app events
  list (`appEventsHTML`), and the logs viewer (`appLogsHTML`: container/tail
  pickers, refresh, fixed-height mono scroller tailing to the newest line,
  warn/error emphasis).
- `renderTraefik` — the routing page (Traefik node → route cards +
  `drawTraefikEdges`).
- `renderMetrics` — gauges + bars with a toolbar: namespace filter, sort
  (memory/cpu/name), and a refresh-interval / pause control (`setPoll`).
- `renderEvents` — the combined events browser: namespace/type/kind filter chips
  + debounced free-text search + a capped scrollable list (`eventsListHTML` is
  shared with the app-detail Events tab).
- A top-right menu (next to the profile) jumps to the combined browser / recent
  warnings, keyboard-accessible (`wireMenu`). On load/sign-in, `followNext` returns to a
  validated `?next=` path (`nextTarget`), with a 10 s same-target guard so a
  gated UI that still refuses can't cause a redirect loop.
- SVG edges are computed from DOM rects and redrawn on resize; `anchor(a,b)`
  picks vertical/horizontal attachment points.

## Data flow & caching
Browser polls `/api/graph` + `/api/metrics` (map/metrics), `/api/app/{name}`
(+ `/api/app/{name}/events` or `/logs` when that tab is active) + `/api/metrics`
(app), `/api/traefik` + `/api/metrics` (traefik), or `/api/events` + `/api/metrics`
(combined events). The server answers from cache within `CacheTTL`, else rebuilds
from the cluster (logs excepted — always a fresh tail). The first request after
start is a cold build (~2–3 s).

## Deploy shape
Multi-stage `Dockerfile` → distroless static image. `apps/landscape.yaml` in the
infra repo is a HelmRelease using the shared `project` chart (Deployment +
Service + IngressRoute/middlewares + ServiceAccount + optional
`rbac.clusterRole`). Env: `LANDSCAPE_PUBLIC_URL`, `LANDSCAPE_GITHUB_OWNER`;
`envFrom` the `landscape-admin` SOPS secret. See `docs/DEPLOY.md`.
