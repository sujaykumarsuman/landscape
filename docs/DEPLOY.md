# Deploy & CI/CD

Deployment is **GitOps** (Flux + Helm). `make` only builds; it never deploys.

## The pipeline

```
git tag vX.Y.Z on main
   └─▶ .github/workflows/deploy.yml (on tag push v*)
         └─▶ builds ghcr.io/sujaykumarsuman/landscape:X.Y.Z  (VERSION=<tag> ldflag)
               └─▶ Flux image-automation (ImageRepository/ImagePolicy in infra
                    apps/image-automation.yaml, policy >=0.1.0) scans GHCR (~5m),
                    bumps the tag in apps/landscape.yaml, commits to infra as fluxcdbot
                     └─▶ helm-controller upgrades the release → rollout in ns landscape
```

- **CI** (`.github/workflows/ci.yml`, on push to main + PRs): `gofmt -l` (must be
  empty), `go vet`, `go test`, `go build`. Keep it green; a PR should be green
  before merge.
- **Image build** (`deploy.yml`, on `v*` tags only): calls the reusable
  `sujaykumarsuman/.github` `build-push.yml`, tag `vX.Y.Z` → image `X.Y.Z`. The
  binary is stamped `main.Version = <tag>` and reports it at `GET /api/info`.
- **Infra repo** `sujaykumarsuman/infra` is the GitOps source of truth:
  `apps/landscape.yaml` (the HelmRelease + values incl. `rbac.clusterRole.rules`),
  `apps/image-automation.yaml` (the auto-bump markers),
  `apps/secrets/landscape-admin.enc.yaml` (SOPS admin password). RBAC/secret/route
  changes are **PRs to infra**, not to this repo.

## Release checklist

1. Land the change on `main` (green CI, PR squash-merged — see
   `docs/GIT-STRATEGY.md`).
2. If the change needs new cluster access, land the matching
   `apps/landscape.yaml` RBAC edit in **infra** first (or same time), so the new
   pod can read what it needs. Never add `secrets`/`configmaps`/write verbs.
3. Tag and push:
   ```
   git checkout main && git pull
   git tag -a vX.Y.Z -m "landscape vX.Y.Z — <summary>"
   git push origin vX.Y.Z
   ```
4. Watch the build: `gh run list --repo sujaykumarsuman/landscape` (the `deploy`
   run for the tag must succeed → image on GHCR).
5. Wait for Flux (≤5 m scan) to bump `apps/landscape.yaml` and roll out. Watch
   from a kubeconfig (see the dev loop): the deployment image should become
   `:X.Y.Z` and `availableReplicas` 1.

## Verify live (always do this)

```
curl -s https://projects.sujaykumar.dev/landscape/api/info   # → "version":"vX.Y.Z"
```
- **Rolling-update overlap**: landscape uses RollingUpdate, so for a few seconds
  both the old and new pod are Ready and Traefik may answer from either — a stale
  `/api/info` right after cutover is normal. Confirm only the new pod remains:
  `kubectl get pods -n landscape` (one pod, image `:X.Y.Z`; old ReplicaSets
  scaled to 0). Then re-check `/api/info` and click through the changed UI.
- For UI changes, verify in the browser at desktop **and** phone width (no
  horizontal page scroll) before considering it done.

## Rollback

Re-point the image (fastest): revert the tag bump in `apps/landscape.yaml`
(infra) to the previous `X.Y.Z` and push — Flux redeploys it. Or cut a new patch
tag with the fix. The ImagePolicy is `>=0.1.0` semver (highest wins), so to hold
a rollback you must also pause image-automation for landscape or the policy will
re-bump to the newest tag.

## Gotchas (learned)

- Flux 2.9 serves image APIs as `image.toolkit.fluxcd.io/v1` (not v1beta2).
- The `apps` Kustomization uses `wait: true`; a genuinely unhealthy managed
  resource can wedge it (it waits the full timeout). Keep new resources healthy.
- `flux`/`fluxcdbot` commits land on infra `main`; `git pull --rebase` infra
  before pushing local infra changes.
- GHCR packages are public (Flux pulls anonymously).
