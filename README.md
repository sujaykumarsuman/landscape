# landscape

A read-only control-plane view of the `projects.sujaykumar.dev` k3s cluster: it
maps the whole flow from **your git repos → build (Actions/GHCR) → Flux → the
running workloads**, explains how each component got there, and deep-links to the
places you maintain (source, workflow, config, image). It also shows live
cluster metrics. Admin-password gated, no write access to the cluster.

Live at **https://projects.sujaykumar.dev/landscape** (deployed by GitOps —
`sujaykumarsuman/infra`).

## What it does

- **Landscape map** — a boxed four-lane pipeline (repos → build/GHCR → Flux →
  cluster) with build→scan→deploy flow arrows. Hover a component to trace its
  path across the lanes and dim the rest; the hover card links to its Source,
  Workflow, Config and Image. **Click to pin** the highlight (the card hides so
  the whole flow stays visible; Esc / click-again to unpin). Kustomizations link
  to the exact infra-repo folder each applies. The lanes shrink to fit and stack
  on narrow screens.
- **App page** — click an app for its full k8s component graph (HelmRelease →
  Deployment → ReplicaSet → Pod, IngressRoute → middlewares → Service, and
  ConfigMap/Secret/PVC mounts) plus a right rail (selected resource, managed-by,
  live usage).
- **Traefik page** — click the Traefik rail for the ingress routing view: every
  path → app with service, middlewares and TLS.
- **Metrics** — node CPU/memory, memory by namespace, top pods, pod counts, and
  Flux reconciliation status, from the in-cluster metrics-server.

It distinguishes **your services** (your GitHub repos / GHCR) from **infra
tools** (Flux, cert-manager, Traefik — linked to their docs).

## Docs

`CLAUDE.md` (repo rules + the read-only invariant), `docs/ARCHITECTURE.md`,
`docs/DEPLOY.md` (GitOps flow + verify), `docs/GIT-STRATEGY.md`, `STATUS.md`, and
`docs/prompts/` (planned work).

## How it works

A single Go binary reads the Kubernetes API, the Flux CRDs
(`GitRepository`/`Kustomization`/`HelmRelease`/`ImageRepository`) and
metrics-server through a **read-only** ServiceAccount, builds a graph model, and
serves it plus an embedded UI. Deep-links are derived from image references, the
Flux git source and repo conventions. Nothing is written to the cluster.

## Config (env)

| Var | Default | Meaning |
| --- | --- | --- |
| `LANDSCAPE_ADMIN_PASSWORD` | — | required; gates the console (a SOPS Secret in GitOps) |
| `LANDSCAPE_LISTEN` | `0.0.0.0:8080` | listen address |
| `LANDSCAPE_GITHUB_OWNER` | `sujaykumarsuman` | owner used to detect "your" images/repos |
| `LANDSCAPE_PUBLIC_URL` | — | advertised URL (display only) |

## Develop

```
make build      # bin/landscape (embeds the UI)
make lint test  # gofmt + go vet + go test
```

Run locally against a cluster you have a kubeconfig for:
`LANDSCAPE_ADMIN_PASSWORD=dev ./bin/landscape` → http://localhost:8080.
