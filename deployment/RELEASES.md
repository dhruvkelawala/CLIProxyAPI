# Shared Mac release, 2026-10-08

CPA-007 is complete. Both Macs use one Mini gateway at localhost port 8317; the MacBook retains its existing tunnel. Both Claude subscriptions are enabled. Codex and native provider settings retain their previous configuration.

## Using the installed choices

In T3, select `Claude · A`, `Claude · B` or `Claude · Automatic`. A is the originally enabled subscription. B is the subscription that was previously disabled. Each Mac has three distinct client profiles and client keys. A and B have separate Claude configuration homes; Automatic retains its previous home and provider ID.

Start a new thread when changing provider instances. Continuation within the same instance was verified. Helpers that inherit the provider use its client identity. Native auxiliary generation and explicit delegates to another provider use their own provider configuration.

Open `http://127.0.0.1:8317/management.html#/client-routes` on either Mac. The six profiles are labelled by machine and choice. Changing a profile's rule affects matching requests from that instance. Only A/B rules fail with `target_unavailable` when the selected account is unavailable. Automatic retains the previous fill-first strategy and conversation affinity. The Shared pool editor changes balancing, account priorities and weights for Automatic clients across the gateway; Only rules remain strict.

## Artifact identities

- [Gateway release](https://github.com/dhruvkelawala/CLIProxyAPI/releases/tag/sumo-v8.0.15-20261008.1), source `128e4039fc049bd984046d4715626e08f31330e3`.
- [Dashboard release](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/releases/tag/sumo-v1.25.4-20261008.1), source `33d16202ccc5662f2886ea83c5b86bdfaa793751`.
- Gateway SHA-256: `30d53438eff5802eb8d74ca51c04bce8d438bcda0213cb9da942360245cdb642`.
- Dashboard SHA-256: `9c974b4e7e1b31bb0dff6ab29de77e9235940c28faa65251ced53fc89f628a8e`.

Both releases contain a manifest and checksums. The gateway archive preserves upstream attribution; `panel-LICENSE.txt` preserves the dashboard license. Downloaded assets matched their published checksums before installation. The backend tag targets the exact merged source. Its tag-push release workflow was briefly paused during manual publication, then restored. No workflow source changed.

The service pins the provisioned panel directory, disables panel auto-update and uses the committed local model catalog. Prepare upgrades intentionally with [the deployment procedure](README.md). Private backups, client keys, OAuth records and the host-specific rollback runbook remain outside Git.

## Verification

[Sanitized acceptance report](https://github.com/dhruvkelawala/CLIProxyAPI/blob/evidence/4-live/20261008T221519Z/cpa-live-acceptance.json?raw=true), [live transcript](https://github.com/dhruvkelawala/CLIProxyAPI/blob/evidence/4-live/20261008T221519Z/cpa-live-acceptance-transcript.txt?raw=true), [exact final artifact staging and rollback](https://github.com/dhruvkelawala/CLIProxyAPI/blob/evidence/4-live/20261008T221519Z/cpa-stage-final.txt?raw=true).

- Full backend tests, vet and build passed on `sumo/main`; dashboard verification passed with 1,755 tests, lint, TypeScript and production build. Bun 1.3.14 cache repair is complete.
- Actual main/helper model calls and native Claude execution passed for all six client choices. Gateway usage records confirmed 23 successful serving-account matches across every machine/profile combination.
- Actual inherited helper-agent calls passed on Mini A and MacBook B. Same-instance continuation passed on both Macs.
- Live stream closure passed on both Macs. The recording fixture additionally proved cancellation propagation, retries and HTTP/WebSocket enforcement.
- With A temporarily disabled, both Macs returned 503 with `target_unavailable` on messages, streaming preflight and token count. B remained enabled and its execution counters did not change. A was restored immediately.
- A disposable MacBook tunnel passed while connected; native Claude failed with a connection error after that tunnel closed. The production tunnel stayed unchanged.
- Both legacy Codex client keys completed actual Responses requests. Both Macs served the pinned dashboard digest.
- Previous binary/panel/configuration restoration passed in isolated staging. No staging gateway or usage collector remains running.

Backend fixes [#9](https://github.com/dhruvkelawala/CLIProxyAPI/pull/11), [#10](https://github.com/dhruvkelawala/CLIProxyAPI/pull/12) and [the mutex verification cleanup](https://github.com/dhruvkelawala/CLIProxyAPI/pull/14) are included in this release.
