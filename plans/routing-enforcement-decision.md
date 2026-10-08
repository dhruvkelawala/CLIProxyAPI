# Routing enforcement decision

CPA-001 is a disposable proof, not a client-profile implementation. The fixture uses the public auth manager and real Responses HTTP/WebSocket handlers with two synthetic accounts. No OAuth files, client credentials, production configuration, T3 settings or running services are read or changed. The HTTP listeners use ephemeral loopback ports. The recorder retains only synthetic account identity and operation kind. Requests have empty inputs; transcript output contains operation, response status/type and recorder events.

## Observed behavior

Run from the repository root:

```sh
go test -count=1 -v -run TestClientProfileFixture ./test ./sdk/cliproxy/auth ./sdk/api/handlers/...
go test -count=1 -v -run 'TestClientProfileFixture(HTTP|WebsocketReplay|HomeBypass|Canary)' ./test
```

`newRoutingFixture` is reusable within the integration-test package. Its manager owns two active accounts, both advertising bare main/helper model IDs and a configured OAuth alias. Its recorder can make A return 503, or 401 for the WebSocket replay probe, while B remains usable. `onlyAccount` detects B independently of a request's apparent success. The canary deliberately executes B and requires the detector to reject it. Disabling B would hide leakage and invalidate this proof.

| Path | Internal pin / ingress evidence | Scheduler-plugin evidence | Release-1 implication |
|---|---|---|---|
| Manager Execute, bare main and helper IDs | A succeeds; subsequent A failure never executes B; explicit B still succeeds | A handled selection plus explicit reject fails closed locally | Enforce provider policy at the manager boundary |
| Manager ExecuteStream, same IDs | Same account recorder evidence, chunks consumed | Same selection/reject behavior | Preserve constraint through attempts |
| Manager ExecuteCount, same IDs | Same account recorder evidence | Same selection/reject behavior | Count requests also require enforcement |
| OAuth alias, all three methods | Configured alias requests stay on A; B canary still works | Active strict plugin stays on A | Alias translation must not change account eligibility |
| Missing, unhandled or unknown-ID plugin | Unpinned controls reach B | Local fallthrough observed across methods/models | Plugin presence or selected ID is insufficient |
| Inactive plugin | Bare-ID controls reach B with zero plugin calls | Alias control returns A failure with no B attempt; zero plugin calls | An incidental alias failure is not a policy guarantee |
| Responses HTTP helper request | Real POST returns 200 on A, then 503 on unavailable A; B remains usable after removal of advisory plugin | Strict plugin drives selection | This does not prove authenticated profile ingress; CPA-003 supplies it |
| Handler stream bootstrap failure | Internal pin propagates; A returns unauthorized first chunk; terminal failure never executes B; B separately succeeds | Not probed | Retry is configured, but only one upstream A attempt is observed because A becomes unavailable |
| Responses WebSocket continuation | Real first create completes on A; continuation after A returns 401 closes with typed service-restart/replay signal and second A attempt | No strict plugin attached in this probe | Session affinity does not implement client policy |
| Responses WebSocket reconnect | Real new connection completes on B after A failure | Strict plugin reconnect not probed | Re-resolve authenticated policy on every reconnect |
| Authenticated caller metadata | Public handler execution forwards hashed Gin authenticated identity and internal pin to the synthetic executor, ignoring spoofed caller-scope header | Not probed as a real auth exchange | Hashed scope is isolation input, not a policy authorization token |
| Home Execute/ExecuteStream/ExecuteCount | Published in-memory Home dispatcher returns B | Strict plugin called zero times; B executes | Strict profiles must reject before Home dispatch |

The tests assert current behavior, including observed bypasses. They do not assert that production client profiles already exist. The existing fast scheduler remains usable by legacy requests; the fixture's inactive-plugin controls execute it through the manager API. Ordinary IDs represent background requests without relying on a suffix, prefix or caller-supplied account marker.

Unsupported/unproven paths are native upstream Codex WebSocket transport, HTTP reconnect after partial delivery, image endpoints, live credential refresh, persisted reload/rename, provider-changing plugin routes, mixed-provider policy enforcement and Home failover/continuation. The recorder implements executor interfaces but never opens an upstream transport. Account expiration, deletion and cooldown variants need CPA-003 tests. No guarantee for these paths may be advertised until an executable enforcement probe covers them, or release 1 explicitly rejects them for strict profiles.

## Enforcement owner and interface

CPA-002 owns persisted profiles, provider policies and durable account references. CPA-003 resolves them after successful authentication and passes an immutable request constraint from ingress to the auth manager. Use a provider-to-account-reference policy, with each provider independently Automatic or Only. A global runtime `PinnedAuthMetadataKey` is a useful local experiment, but cannot represent a Claude-only policy on a request that can select Codex.

The manager must resolve Only references to current runtime credentials, intersect its existing eligible candidates with the policy, and validate the final selected credential immediately before each executor call. This applies to ordinary, fast, mixed-provider, plugin-selected, retry, count, stream and session paths. Preserve existing capability, enablement, model, cooldown and expiration checks; do not build a second scheduler. A missing resolver, zero matches, multiple matches, mismatched provider or unavailable target produces a machine-readable terminal error. No strict error becomes an unrestricted fallback.

Plugins may choose within the constrained pool. They cannot widen the pool, remove the constraint, or turn an unknown selection into unrestricted routing. Validate even a handled plugin result. Automatic and keys without a profile retain existing scheduling. Model aliases and helper IDs change no provider policy. Policy information belongs in trusted request context, never in client-supplied business payload fields or headers.

Session affinity and explicit pins are subordinate to the authenticated provider policy. Equal constraints are compatible; a different pin rejects before execution. A continuation cannot move to another account. Reconnect and full replay must resolve the same authenticated profile again, including changed or removed targets. Define profile-change behavior explicitly in CPA-003; reject stale sessions rather than keeping an obsolete target silently. Capture a request snapshot so a concurrent profile edit cannot widen eligibility midway through retries.

Release 1 supports local scheduling only when every relevant path enforces that constraint. If Home is enabled, reject strict profiles before any control-plane dispatch or runtime-session reuse. Home's external dispatcher has not agreed to the policy contract. Do not change CLIProxyAPIHome in this ticket. Automatic Home requests remain eligible for their existing behavior. Provider-changing/plugin-owned executor modes remain unsupported for strict requests until final provider and credential validation can be proved; advertise that limitation in capabilities and preview.

## Durable identity

Persist a random opaque UUID in credential metadata, named `account_ref`, assigned by explicit management enrollment in CPA-002. Do not derive it from a runtime ID, auth index, filename, email, label, provider token or subscription tier. File synthesis currently derives runtime IDs from relative auth paths. Labels and file renames therefore cannot identify the policy target reliably.

Preserve `account_ref` across store writes, metadata updates, refresh, supported rename and reload. Resolve the reference against current credentials for every request. A copy containing the same UUID creates an ambiguous target and must fail closed, even when one copy is disabled. Do not silently regenerate either reference. Enrollment must reject an already assigned reference, and imported or legacy credentials without a reference remain usable by Automatic but cannot be saved as an Only target until explicitly enrolled. Startup must not rewrite production credentials as a migration.

The reference identifies one enrolled credential record, not a vendor's global subscription or proof of which account served a historical request. Several credentials for one subscription may have separate references. Credential replacement or removal must keep existing profiles visibly unresolved until explicitly retargeted. CPA-002 must prove metadata storage and external rename/reload with temporary file stores, duplicate-reference handling, and reference preservation through refresh. These persistence guarantees are design requirements, not results of this in-memory fixture.

## Evidence and follow-up

The verbose commands above produce repeatable library/API evidence, including HTTP status changes, WebSocket completed/replay-close/completed outcomes and [A,A,B] recorder events. The deliberate wrong-account canary was first run with an A-only expectation after B execution and failed with `account fixture-b served execute, wanted fixture-a`; its final test requires this rejection. CPA-003 should reuse this recorder and extend the matrix with actual authenticated profiles. Add each upstream protocol path to the matrix when rebasing.

CPA-001 passed independent verification and landed in [PR #6](https://github.com/dhruvkelawala/CLIProxyAPI/pull/6). CPA-002 implements the [management contract](client-profile-api.md), pending review. CPA-003 still owns routing enforcement. CPA-007 deployment is deferred by the user; this work does not authorize a cutover or change the running proxy.
