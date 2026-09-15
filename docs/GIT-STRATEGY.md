# Git strategy

Trunk-based. `main` is always deployable (a tag off it ships — see
`docs/DEPLOY.md`).

## Branches

- One short-lived branch per change, named by type: `feat/<slug>`, `fix/<slug>`,
  `chore/<slug>`, `docs/<slug>`.
- Branch off latest `main`. Keep branches small and single-purpose.

## Commits

- **Conventional Commits**: `type(scope): summary` — `feat`, `fix`, `chore`,
  `docs`, `refactor`, `test`, `ci`. Imperative mood, British English.
- Body: what changed and why (not how). Note any infra-repo change the deploy
  depends on (RBAC, secret, route).
- Attribution — end every commit made by the agent with:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
  (Match the model actually used if different.)

## Pre-commit gate (must pass)

```
make lint test     # gofmt -l empty + go vet ./... + go test ./...
make build         # embeds the UI; must compile
```
For UI changes, also verify in-browser at desktop + phone width (no horizontal
page scroll). CI runs the same Go gate on push/PR.

## Pull requests

- Open a PR for every change: `gh pr create` with a body covering **what**,
  **verification** (how you tested — live check, screenshots, widths), and any
  **review** done. End PR descriptions with:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)
  ```
- Prefer running an adversarial review over non-trivial diffs and fixing
  confirmed findings before merge.
- **Squash-merge** to `main` (`gh pr merge <n> --squash --delete-branch`). Keep
  `main` history one-commit-per-change.

## Tag → deploy

After merge, deploy by tagging `main` (`vX.Y.Z`, semver: patch for fixes, minor
for features). The tag triggers the image build; Flux rolls it out. Full steps +
verification in `docs/DEPLOY.md`. Bump minor for user-facing features (this is
how v0.2/0.3/0.4 were cut), patch for fixes.

## Infra changes

Cluster access (RBAC), the admin secret, and routing live in
`sujaykumarsuman/infra`, not here. Those are separate PRs to infra (or direct
commits per that repo's flow) — land them before/with the tag that needs them.
Never widen the landscape ClusterRole beyond the read-only invariant in
`CLAUDE.md` without a deliberate, called-out reason.
