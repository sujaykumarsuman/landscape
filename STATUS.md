# STATUS

_Last updated: 2026-09-15 (v0.4.0)._

## Live

- **v0.4.0** at https://projects.sujaykumar.dev/landscape, deployed by GitOps
  (Flux + Helm from `sujaykumarsuman/infra`, `apps/landscape.yaml`).
- Read-only ClusterRole (see the invariant in `CLAUDE.md`). Admin-password gated
  (SOPS secret `landscape-admin`).

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

## Next (planned) — see `docs/prompts/002-events-logs-metrics.md`

- **Events browser**: per-app events (an Events tab in the app-detail graph
  header), an events/logs menu in the top-right, and a combined events browser
  with filters + search reached from the top nav (the `Events` tab).
- **Logs viewer** in the app view (pod logs).
- **Metrics tab controls** (range/refresh/namespace filters, etc.).
- These need new endpoints and **new RBAC** (core `events` read; `pods/log` for
  logs) — plan carefully against the read-only invariant (logs are pod stdout,
  not Secret contents; `pods/log` is a `get` subresource). Details in the prompt.

## Open questions

- Logs: stream (SSE) vs. tail-on-demand; retention/scrollback; multi-container
  pods. (See the prompt.)
- Events browser: how far back (the API server only keeps ~1h of Events by
  default) — may need to note the window rather than promise history.
- Whether to add live CI status via the GitHub API (currently deep-links only).
