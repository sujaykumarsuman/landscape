# STATUS

_Last updated: 2026-09-24 (v0.9.1)._

## Live

- **v0.9.1** at https://projects.sujaykumar.dev/landscape, deployed by GitOps
  (Flux + Helm from `sujaykumarsuman/infra`, `apps/landscape.yaml`).
- Read-only ClusterRole (see the invariant in `CLAUDE.md`). Admin-password gated
  (SOPS secret `landscape-admin`). The same session gates Longhorn (`/longhorn/`)
  and kubescope (`/kubescope/`) via Traefik ForwardAuth → `/api/forward-auth`.

## Done

- **v0.1.x** — first console: 4-lane landscape map (repos → build → Flux →
  cluster), hover flow-highlight with deep-links, app drill-in (side drawer),
  live metrics, admin login.
- **v0.2.0** — mockup-fidelity rewrite: boxed lanes with build→scan→deploy flow
  arrows; structured cluster box (Traefik rail, per-namespace app cards,
  platform-namespaces sub-box, footer); GitOps controllers/kustomizations/
  image-automation boxes; **full-page app detail** (`GET /api/app/{name}`) with
  the k8s component graph + right rail; admin profile; responsive to phone.
  RBAC gained `services` + `persistentvolumeclaims` (read-only).
- **v0.3.0** — **Traefik routing page** (`GET /api/traefik`, reached from the
  Traefik rail); horizontal-scroll cluster rows (app + platform namespaces);
  **pinnable** flow-highlight (click pins, hides hover card; Esc/again unpins).
- **v0.4.0** — layout + polish: the 4 lanes shrink to fit and stack below
  1080px (no more horizontal page scroll); image-automation highlights and
  reveals an explainer bubble when a flow is pinned; kustomization cards link to
  the exact repo folder each one applies; the hover card is reachable so its
  deep-links are clickable; Flux version label fixed (`v2.9.5`).
- **v0.5.0** — **per-app URL routing** (History-API router + server SPA-fallback:
  map at the root, apps at `…/<app>`, `metrics`/`events`/`traefik` at `…/<word>`;
  deep-links, reload, back/forward); **observability** — per-app **Events** tab, a
  combined **Events browser** (namespace/type/kind filters + search), a **Logs
  viewer** (tail-on-demand), an events/warnings menu, and **Metrics controls**
  (namespace filter, sort, refresh/pause). RBAC gained core `events` (get,list) +
  `pods/log` (get), read-only.
- **v0.5.1** — fix: projects-hub deep-links point at its real source repo.
- **v0.5.2** — richer map hover cards; projects-hub source-link fix in the map.
- **v0.5.3** — keep completed Job pods out of the running-pod total.
- **v0.6.0** — **Storage view** (`GET /api/storage`, nav tab): the cluster's
  StorageClasses and PVCs across namespaces, read live; reworded the map footer to
  reflect the whole platform (Longhorn + local-path PVCs · CloudNativePG Postgres).
  RBAC gained `storage.k8s.io/storageclasses` (get,list), read-only.
- **v0.7.0** — group multi-component apps by source repo in the map (one source
  box whose trace fans out to every component) and surface each app's storage
  dependencies (CloudNativePG Postgres + cross-namespace PVCs) on the app page.
- **v0.7.1** — fix: tracing any single component highlights the whole app group.
- **v0.8.0** — label-driven source grouping in the collector (drop the hardcoded
  repo→components map; derive the grouping from resource labels).
- **v0.8.1** — fix: keep Components as workload names so the flow highlight and
  the build-lane group box render correctly.
- _(repo)_ stopped tracking the built `bin/landscape` in git and added a
  `.gitignore` (#15) — the image is built from source by CI, never the committed
  binary.
- **v0.8.2** — map layout: the build lane shows the image tag inline with the
  name (`name:tag`); the cluster lane is transposed so each app namespace is a
  row whose workloads are a single horizontal strip (scrolls sideways), the
  namespace list scrolls vertically, and the 4-lane map is bounded to the
  viewport (each lane scrolls its own overflow).
- **v0.8.3** — fix: collapse the top-bar nav to icons on small screens (≤640px)
  so it no longer overflows the viewport — no horizontal page scroll on phones.
- **v0.8.4** — fix: show each PVC's real storageClass.
- **v0.9.0** — **auth gateway for other UIs**: `GET /api/forward-auth` (Traefik
  ForwardAuth target — 204 / 302 to login with `?next=` / 401) gates Longhorn and
  kubescope behind the admin session; login returns to `?next`; a top-bar
  **Tools** menu (`LANDSCAPE_TOOLS`). Sessions are now **expiring** stateless
  tokens (12 h server-side; everyone signs in once after the upgrade), signed
  with PBKDF2(password) + an optional random `LANDSCAPE_SESSION_KEY` (SOPS).
  `LANDSCAPE_GITHUB_OWNER` takes a comma list so `skriptvalley/*` images render
  as your apps; the `sujaykumar.dev/source-workflow` label fixes the workflow
  deep-link for repos not built by `deploy.yml` (kubescope → `release.yml`).
- **v0.9.1** — the forward-auth gate returns 403 for state-changing or WebSocket
  requests that another origin started, even with a valid session. `SameSite=Lax`
  lets sibling subdomains through, so this guards Longhorn and kubescope against
  forged form POSTs.

## Later

- Logs live-follow via SSE (currently tail-on-demand); live CI status via the
  GitHub API (currently deep-links only).

## Open questions

- Logs: stream (SSE) vs. tail-on-demand; retention/scrollback; multi-container
  pods. (See the prompt.)
- Events browser: how far back (the API server only keeps ~1h of Events by
  default) — may need to note the window rather than promise history.
- Whether to add live CI status via the GitHub API (currently deep-links only).
