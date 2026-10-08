# CPA-007: Package pinned releases and configure both Macs through existing T3 instances

- **Status:** DONE
- **Tracking issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/5
- **Issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/4
- **Priority:** P1
- **Effort:** 1–2 days (rough)
- **Risk:** High
- **Planned at:** a4acc9f752bd46571f737a10c04bf413656ab06b (v8.0.15), 2026-10-07
- **Branch:** create a topic branch from `sumo/main`; retain existing Go module/package identity.
- **Depends on:** [CPA-001](https://github.com/dhruvkelawala/CLIProxyAPI/issues/1), [CPA-002](https://github.com/dhruvkelawala/CLIProxyAPI/issues/2), [CPA-003](https://github.com/dhruvkelawala/CLIProxyAPI/issues/3), [CPA-004](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/1), [CPA-005](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/2), [CPA-006](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/3)

Read this ticket and AGENTS.md before editing. Drift check: `git diff --stat a4acc9f752bd46571f737a10c04bf413656ab06b..HEAD -- plans/ deployment/ .github/workflows/release.yaml .github/workflows/pr-test-build.yml`. If relevant source changed, re-read the cited behavior before following the steps. Keep changes focused; update plans/README.md when finished.

## Outcome
Provide a reproducible staged release and rollback for the shared Mini gateway, the custom dashboard and account-specific T3 provider configurations on both Macs. No T3 source changes or second production gateway are needed.

## Context and current state
Development branches are based on backend 8.0.15 and CPAMC 1.25.4. The live topology to preserve is Mini localhost:8317, with MacBook localhost:8317 forwarded through SSH/Cloudflare Access. Each machine has its own client identity. Native providers and auxiliary generation remain separate.

The v8 configuration supports `management.panel-github-repository` and `management.disable-auto-update-panel`. The panel updater fetches a release asset named `management.html`. Turning periodic updates off alone does not prove a specific version was installed: pin/provision the artifact and verify its digest.
Backend release workflow builds Darwin arm64 assets; panel `.github/workflows/release.yml` renames its single-file `dist/index.html` to `management.html`. Review workflows before enabling fork Actions or pushing release tags.

## Steps
1. Record backend/panel commit hashes and artifact SHA-256 values. Use fork-specific release tags, preserve license/upstream attribution and do not overwrite upstream tags. Keep the patchset rebased only during intentional upgrades.
2. Run staging on a separate loopback port with separate disposable auth/storage directories and synthetic client configuration. Re-run the enforcement fixture and UI contract against the exact release artifacts.
3. Write a private operator runbook and a sanitized committed template. Back up existing service/config privately; never commit secrets, real credential files or host-specific secret locations.
4. Configure existing T3 instances Claude · A, Claude · B and Claude · Automatic on both machines with isolated client-profile configuration and standard model IDs. Verify wrapper precedence instead of assuming env overrides. Keep Mini/MacBook distinguishable and Codex unaffected.
5. Test new threads on both machines: A-only and B-only, inherited background requests, cancellation/continuation, MacBook tunnel failure, disabled target and legacy key. Scope excludes native auxiliary generation and explicit delegates to another provider. If continuation across instance changes is unproven, require a new thread.
6. Produce a concrete cutover/rollback procedure with exact artifact identities and both-client results. Apply the production cutover only when authorized for that operation. Re-enable B only after its isolation is proven, then verify A and B remain independently selectable.
7. Document how to update from upstream, compare non-secret settings and repeat the acceptance matrix.

## Verification and done criteria
- [x] Backend `go test ./...` and build pass; dashboard `bunx bun@1.3.14 run verify` passes.
- [x] `shasum -a 256 <backend-artifact> <management.html>` matches the release manifest.
- [x] Mini and MacBook recorded tests use the same gateway/version and appropriate distinct profiles.
- [x] A-only failure produces no B traffic, and B-only succeeds with A still enabled.
- [x] Rollback is rehearsed in staging, including the panel artifact and client configuration.
- [x] Evidence contains hashes, safe profile IDs and outcomes without credentials or prompt contents.
- [x] No T3 application rebuild/fork and no replacement of the existing tunnel.

## Stop conditions and maintenance
If wrapper credentials are shared despite different instance labels, stop and fix the isolated configuration before live rollout. If any production key/account change is outside authorization, finish the staging package/runbook first and leave that final operation pending. Do not add fabricated cost history or deploy a stale panel/backend contract. Record upstream rebase conflicts and repeat account-enforcement proof for each release.

## Scope

Files/directories in scope:

- `plans/`
- `deployment/ (new, sanitized templates only if needed)`
- `.github/workflows/release.yaml (only if fork release changes are required)`
- `.github/workflows/pr-test-build.yml (only if fork CI changes are required)`

Committed scope excludes T3 source, live credentials and host-specific configuration. The user authorized the private gateway and both-Mac rollout on 2026-10-08 by asking to finish all listed items. Keep those operations in the private runbook outside Git. Match existing repository conventions. Open a PR against the fork's `sumo/main` when publishing authorized implementation work; do not target upstream `main` by accident.

## Execution record

Backend #9 and #10 are merged. Bun 1.3.14 and dashboard verification pass. The deployment tooling in [deployment/](../deployment/README.md) builds immutable fork artifacts, proves strict rejection without vendor traffic, rehearses restoration of the previous binary/panel/configuration and tests launcher credential precedence. The reviewed tooling merged in [PR #15](https://github.com/dhruvkelawala/CLIProxyAPI/pull/15). Both pinned releases are published, the shared gateway is installed and both Macs passed live acceptance. [Release notes](../deployment/RELEASES.md) record hashes, supported behavior and sanitized proof.
