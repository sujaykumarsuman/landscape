# landscape

A read-only control-plane view of the `projects.sujaykumar.dev` k3s cluster: it
maps the whole flow from **your git repos → build (Actions/GHCR) → Flux → the
running workloads**, explains how each component got there, and deep-links to the
places you maintain (source, workflow, config, image). It also shows live
cluster metrics. Admin-password gated, no write access to the cluster.

Live at **https://projects.sujaykumar.dev/landscape** (deployed by GitOps —
`sujaykumarsuman/infra`).

## What it does

- **Landscape map** — repos, images, Flux (kustomizations, HelmReleases,
  image-automation) and cluster apps as a graph. Hover a component to trace its
  path from source to pod and dim the rest; the hover card links to its Source,
  Workflow, Config and Image. Click an app for its k8s components.
- **Metrics** — node CPU/memory, memory by namespace, top pods, pod counts, and
  Flux reconciliation status, from the in-cluster metrics-server.

It distinguishes **your services** (your GitHub repos / GHCR) from **infra
tools** (Flux, cert-manager, Traefik — linked to their docs).

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
