# CPA-000: Enforced subscription selection and management dashboard v1

## Required outcome
Choose a subscription in the management dashboard and enforce that choice at CLIProxyAPI. The other subscription can remain enabled. If the selected subscription cannot serve a request, an Only policy returns a clear error instead of using another subscription. This includes applicable background/helper requests using the same client profile.

T3 source remains unchanged. Existing T3 provider instances can select dedicated client profiles after the proxy feature is implemented.

## Release 1

- Automatic and Only a named subscription, with provider-specific client policies.
- Friendly Accounts UI; enabled state separated from availability and preference.
- Client/profile scope, persisted server policy, unavailable-target errors and honest previews.
- Shared Mini gateway and MacBook localhost tunnel context.
- Existing native providers/Codex preserved; accessible controls and pinned rollout.

Per-client Prefer/fallback, actual served-by attribution and durable cost/token history are deferred. A configured target is not proof of the account that served a past request.

## Tickets

| Ticket | Work | Repository | Depends on | Status |
|---|---|---|---|---|
| [CPA-001](https://github.com/dhruvkelawala/CLIProxyAPI/issues/1) | Prove strict client account routing with an isolated recording fixture | Backend | Ready now | TODO |
| [CPA-002](https://github.com/dhruvkelawala/CLIProxyAPI/issues/2) | Add durable client profiles and a v8 management contract | Backend | CPA-001 | TODO |
| [CPA-003](https://github.com/dhruvkelawala/CLIProxyAPI/issues/3) | Enforce the selected subscription across retries, helpers, streams and WebSockets | Backend | CPA-001, CPA-002 | TODO |
| [CPA-004](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/1) | Make account labels, enablement, availability and preference clear | Dashboard | Ready now | TODO |
| [CPA-005](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/2) | Let the dashboard choose and enforce a subscription for each client | Dashboard | CPA-002, CPA-003, CPA-004 | TODO |
| [CPA-006](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/3) | Make account and client-route controls accessible in both themes | Dashboard | CPA-004, CPA-005 | TODO |
| [CPA-007](https://github.com/dhruvkelawala/CLIProxyAPI/issues/4) | Package pinned releases and configure both Macs through existing T3 instances | Backend | CPA-001, CPA-002, CPA-003, CPA-004, CPA-005, CPA-006 | TODO |
| [CPA-008](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/4) | Expose shared load-balancing strategies with priority, weights and session affinity | Dashboard | CPA-004, CPA-005, CPA-006 | TODO |

## Recommended order

1. Start [CPA-001](https://github.com/dhruvkelawala/CLIProxyAPI/issues/1) to prove the enforcement boundary. A scheduler plugin is a candidate, not an established guarantee: existing tests show it is skipped in Home mode.
2. [CPA-004](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/1) can start independently for account presentation.
3. Complete CPA-002 and CPA-003 before claiming subscription selection is enforced.
4. Build CPA-005 against the implemented backend contract, then complete CPA-006.
5. CPA-008 follows CPA-005/006 for the shared load-balancing UI; it does not delay the strict-selection release.
6. CPA-007 owns exact release artifacts, two-machine acceptance, staging and rollback. No production cutover is part of backlog setup.

## Acceptance that closes this project

- Select B while A remains enabled: requests for that profile reach B only.
- Make B unavailable in an isolated fixture: the request errors and A's recorder remains at zero.
- Main/background request IDs stay ordinary; policy applies at the gateway.
- The saved selection survives refresh/reconnect under documented session semantics.
- Unknown targets, missing enforcement support and conflicting pins never weaken an Only rule.
- Legacy keys, prefixes and independent Codex/native routes still work.
- Both client machines use the shared gateway through their existing connection paths.

## Development baseline

Backend: v8.0.15, a4acc9f752bd46571f737a10c04bf413656ab06b.
Dashboard: v1.25.4, 6abace9ffb83a9ac349464ded04bb4e7f7cb309e.
Each fork uses sumo/main as the development base. Upstream remains a separate remote.

Setup checks passed: backend go test ./... and build; dashboard 1,493 tests, lint and TypeScript/production build with Bun 1.3.14. This verifies the upstream starting point, not the proposed enforcement feature. Full pickup instructions and ticket copies are under plans/ in both forks.

## Approved load-balancing follow-up

[CPA-008](https://github.com/dhruvkelawala/Cli-Proxy-API-Management-Center/issues/4) collects the existing global round-robin, weighted-round-robin and fill-first strategies, account priority/weights and session affinity into one clear routing flow. Pick it up after CPA-005/006; strict subscription selection remains first. It is an optional follow-up and does not block the core rollout in CPA-007.

Global strategy changes affect Automatic clients across provider pools on the shared gateway, including both Macs. Only profiles remain strict. Preserve saved defaults on upgrade. Per-client/provider pools and strategy overrides, quota-aware routing and least-busy routing are deferred; they need separate backend contracts or reliable telemetry.
