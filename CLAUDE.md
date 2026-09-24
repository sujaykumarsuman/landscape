# landscape — project rules

A **read-only** control-plane view of the `projects.sujaykumar.dev` k3s cluster.
One Go binary reads the Kubernetes API + Flux CRDs + metrics-server through a
read-only ServiceAccount, builds a graph model, and serves it plus an embedded
vanilla-JS UI. It maps **git repos → build (Actions/GHCR) → Flux → running
workloads**, explains each hop, deep-links to the places you maintain, and shows
live metrics. Admin-password gated. **Nothing is ever written to the cluster.**

Live at **https://projects.sujaykumar.dev/landscape** — deployed by GitOps from
`sujaykumarsuman/infra` (Flux + Helm). See `docs/ARCHITECTURE.md`,
`docs/DEPLOY.md`, `docs/GIT-STRATEGY.md`, `STATUS.md`, and `docs/prompts/` for
the next planned work.

## The one invariant (never break)

The console is **read-only and must never read Secret or ConfigMap _contents_.**
It reads resource metadata/specs only. ConfigMap/Secret/PVC **names** shown on
the app page are derived from the Deployment pod spec
(`envFrom`/`env.valueFrom`/`volumes`/`imagePullSecrets`) — never by reading the
objects' data. The SOPS badge is inferred from the owning Kustomization's
`spec.decryption.provider == sops`. The ClusterRole grants only read verbs:
`get,list` on core `nodes,namespaces,pods,services,persistentvolumeclaims,events`,
apps `deployments,replicasets`, `metrics.k8s.io`, `storage.k8s.io storageclasses`,
the Flux CRD groups, and `traefik.io ingressroutes`; plus `get` on core
`pods/log`. **No `secrets`, no `configmaps`, no write verbs.**
Any new collector read must fit this set — if a feature needs a new
resource/verb, add it to `apps/landscape.yaml` in the infra repo (chart
`project`'s `rbac.clusterRole.rules`) and call it out in the PR.

## Components

- `cmd/landscape/main.go` — env config + `var Version` (ldflag `-X main.Version`).
- `internal/kube` — builds the read-only clients: typed clientset, dynamic
  client (Flux + Traefik CRDs), metrics clientset. In-cluster first, then
  `KUBECONFIG`/`~/.kube/config` fallback (this is what the local dev loop uses).
- `internal/collect` — the collector. `collect.go` `Graph(ctx)` builds the
  landscape graph (nodes/edges + cluster roll-ups); `app.go` has
  `AppDetail(ctx,name)` (per-app component graph), `Traefik(ctx)` (ingress
  routing), and `clusterRollup` (namespaces, GitOps controllers/kustomizations,
  node capacity); `metrics.go` `Metrics(ctx)`; `links.go` (image/git parsing,
  `ghLinks`, infra-tool docs). GVRs for the Flux/Traefik CRDs live at the top of
  `collect.go`.
- `internal/model` — the JSON types the UI renders (`model.go`: Graph/Node/Edge/
  Cluster/Metrics; `app.go`: AppDetail + component detail types, NsSummary,
  GitOpsInfo, TraefikInfo).
- `internal/server` — HTTP: admin-password gate (HMAC cookie), handlers, and the
  embedded `web/` UI via `//go:embed`. Per-endpoint caches guarded by `s.mu`
  with `CacheTTL` (default 10s).
- `internal/server/web` — the whole UI: `index.html` (shell + login), `app.js`
  (all views + interactions), `style.css` (one dark theme, tokens at `:root`).
  No framework, no build step, no CDN — everything inline/embedded.

## HTTP surface

Public: `GET /healthz`, `GET /api/info`, `GET /api/forward-auth`. Auth (POST)
`/api/login`, `/api/logout`, `/api/session` (lists the Tools once signed in).
Behind the `ls_session` cookie: `GET /api/graph`, `GET /api/metrics`,
`GET /api/app/{name}`, `GET /api/traefik`, … Everything else is the embedded UI
at `/`. Session = a stateless, expiring token `v2.<expiry>.<HMAC-SHA256>` whose
key is PBKDF2(admin password) mixed with the optional random
`LANDSCAPE_SESSION_KEY` (derived once at startup) — survives restarts/redeploys,
**expires server-side after 12 h** (also the cookie lifetime), canonical expiry
only, and all sessions die when the password or session key rotates. The cookie is `Path=/` so it also reaches the gated UIs on the host.

**Auth gateway.** `/api/forward-auth` is the Traefik ForwardAuth target that gates
other UIs (Longhorn `/longhorn/`, kubescope `/kubescope/` — IngressRoutes in the
infra repo): 204 when the session is valid; otherwise a page navigation gets a
302 to the login with `?next=<X-Forwarded-Uri>` (same-host paths only — checked
server-side and again in the UI before following) and API/WebSocket/non-GET calls
get a 401. The redirect is an **absolute** URL (`LANDSCAPE_PUBLIC_URL`, else
Traefik's `X-Forwarded-Proto/Host`): Traefik resolves a relative `Location`
against the auth address, i.e. the in-cluster service. Successful checks are not
logged (they run on every gated request). The gated UIs' own powers (kubescope runs cluster-admin) are theirs,
not landscape's — landscape's ServiceAccount stays read-only. The admin
password is a SOPS secret (`apps/secrets/landscape-admin.enc.yaml` in infra),
rotated with `sops` — there is deliberately **no in-app password change** (it
would fight the GitOps/SOPS source of truth).

## UI model (web/app.js)

Views: `map` (the 4-lane pipeline), `metrics`, `events` (placeholder for now),
`app` (full-page per-app component graph + right rail), `traefik` (ingress
routing page). `state` holds `graph`/`metrics`/`app`/`traefik`/`view`/`appName`/
`pin`; a 15 s poll re-renders the active view. Edges on the app + traefik pages
are drawn as SVG from DOM rects (`drawAppEdges`/`drawTraefikEdges` + `anchor()`),
redrawn on resize. Map interactions: hover traces a component's `data-app` chain
across lanes and shows a reachable hover card (grace-timer so its links are
clickable); **click pins** the highlight and hides the card (Esc / empty-space /
click-again to unpin); `.flowall` cards (image-automation) highlight on any
trace. The 4 lanes shrink to fit and **stack below 1080px** — never allow
horizontal page scroll (`#view` is `overflow-x:hidden`; scroll rows use
`min-width:0`).

## Conventions

- **Trunk-based, one short-lived branch per change** (`feat/…`, `fix/…`),
  Conventional Commits, **squash-merge** to `main` via PR. Details +
  attribution lines in `docs/GIT-STRATEGY.md`.
- **Gate before every commit**: `make lint test` (= `gofmt -l` must be empty +
  `go vet ./...` + `go test ./...`); `make build` embeds the UI. CI
  (`.github/workflows/ci.yml`) runs the same on push/PR. Keep it green.
- **Deployment is GitOps** (`docs/DEPLOY.md`): tag `vX.Y.Z` on `main` →
  `deploy.yml` builds `ghcr.io/sujaykumarsuman/landscape:X.Y.Z` → Flux
  image-automation bumps `apps/landscape.yaml` in infra → rollout. `make` only
  builds; it never deploys. Verify live after: pod on the new tag +
  `GET /api/info` reports the tag (mind the rolling-update overlap — the old pod
  can answer for a few seconds).
- **UI**: one committed dark theme (CSS variables at `:root`, dark by design —
  the page always renders dark), inline SVG icons (the `icon()` map in app.js),
  no icon font / no CDN, everything embedded. Verify UI work in-browser at
  several widths (desktop + phone) — page must never scroll horizontally.
- **Go**: standard library + client-go + apimachinery + metrics + Flux/Traefik
  unstructured reads via the dynamic client. Prefer `unstructured.Nested*`
  helpers; numbers from the API decode as `int64`/`float64` (handle both) and
  named ports as `string`. British English in docs.
- **Deep-links** are convention-derived (image ref → GHCR/source, Flux git
  source → infra repo, `spec.path` → repo folder). Live CI status (GitHub API)
  and an events/logs stream are future adds (`docs/prompts/`).

## Local dev loop (against the live cluster)

No local cluster needed — tunnel to the VPS k3s API and run the binary with its
kubeconfig (server is already `127.0.0.1:6443`):

```
ssh -N -L 6443:127.0.0.1:6443 airlift-vps &          # ~/.ssh/config host
ssh airlift-vps 'cat /etc/rancher/k3s/k3s.yaml' > /tmp/kc
KUBECONFIG=/tmp/kc LANDSCAPE_ADMIN_PASSWORD=dev \
  LANDSCAPE_GITHUB_OWNER=sujaykumarsuman LANDSCAPE_LISTEN=127.0.0.1:8099 \
  ./bin/landscape                                      # → http://127.0.0.1:8099
```

The dev password is whatever you pass; the live one is the SOPS secret. To read
the live admin password: `ssh airlift-vps "k3s kubectl get secret
landscape-admin -n landscape -o jsonpath='{.data.LANDSCAPE_ADMIN_PASSWORD}' |
base64 -d"`.

## Output style

Concise. Report outcome + blockers. Verify UI changes in-browser before
shipping; run an adversarial review over non-trivial diffs and fix confirmed
findings before merge.
