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
  usage from metrics-server, plus pod readiness counts.
- `links.go`: `parseImage`, `parseGitURL`, `ghLinks` (Source/Workflow/Image/
  Config deep-links), and the infra-tool docs map. Helpers `kustURL`/`trimPath`
  build the repo-folder links for kustomizations.

GVRs (top of `collect.go`): `helm.toolkit.fluxcd.io/v2 helmreleases`,
`kustomize.toolkit.fluxcd.io/v1 kustomizations`,
`source.toolkit.fluxcd.io/v1 gitrepositories`,
`image.toolkit.fluxcd.io/v1 imagerepositories` (+ `imagepolicies` in `app.go`),
`traefik.io/v1alpha1 ingressroutes`. **Flux 2.9 serves image APIs as `v1`, not
`v1beta2`.**

### `internal/model`
Plain JSON structs the UI consumes. `model.go`: `Graph`, `Node`, `Edge`,
`Cluster` (+ `NsSummary`, `GitOpsInfo`), `Metrics`. `app.go`: `AppDetail` and its
detail types (`DeploymentDetail`, `ReplicaSetDetail`, `PodDetail`,
`ServiceDetail`, `IngressDetail`, `RefName`, `AppGitOps`, `HRDetail`,
`KustDetail`), `TraefikInfo`/`TraefikRoute`.

### `internal/server`
`Options{Addr, AdminPassword, GithubOwner, PublicURL, Version, CacheTTL}`.
`Handler()` wires routes; the embedded `web/` FS serves `/`. Auth: `authed(r)`
constant-time-compares the `ls_session` cookie against `sessionToken(pw)` =
`HMAC-SHA256("landscape-session/"+pw, "v1")` (deterministic ⇒ survives restarts;
12 h cookie). Each read endpoint has a small `s.mu`-guarded cache with `CacheTTL`
(default 10 s): `graph`, `metrics`, `apps[name]`, `traefik`. `PublicURL`'s host is
used to fill per-route/app public URLs.

### `internal/server/web`
The UI, embedded via `//go:embed web`. `index.html` = shell + login. `style.css`
= one dark theme (tokens at `:root`; responsive: 4 lanes → stacked at 1080px,
top bar compacts at 640px, `#view` is `overflow-x:hidden`). `app.js` (vanilla,
no deps) = auth flow, a 15 s poll, and the render functions per view:
- `renderMap` — the 4 boxed lanes + flow-gap arrows + cluster box; `wireMap`
  binds hover-trace + pin + Traefik-rail click; `applyTrace`/`pinApp`/`unpin`.
- `renderApp` — the full-page component graph (`appGraphHTML` columns +
  `drawAppEdges`) and right rail (`appRailHTML`).
- `renderTraefik` — the routing page (Traefik node → route cards +
  `drawTraefikEdges`).
- `renderMetrics`, `renderEvents` (placeholder).
- SVG edges are computed from DOM rects and redrawn on resize; `anchor(a,b)`
  picks vertical/horizontal attachment points.

## Data flow & caching
Browser polls `/api/graph` + `/api/metrics` (map/metrics), or
`/api/app/{name}` + `/api/metrics` (app), or `/api/traefik` + `/api/metrics`
(traefik). The server answers from cache within `CacheTTL`, else rebuilds from
the cluster. The first request after start is a cold build (~2–3 s).

## Deploy shape
Multi-stage `Dockerfile` → distroless static image. `apps/landscape.yaml` in the
infra repo is a HelmRelease using the shared `project` chart (Deployment +
Service + IngressRoute/middlewares + ServiceAccount + optional
`rbac.clusterRole`). Env: `LANDSCAPE_PUBLIC_URL`, `LANDSCAPE_GITHUB_OWNER`;
`envFrom` the `landscape-admin` SOPS secret. See `docs/DEPLOY.md`.
