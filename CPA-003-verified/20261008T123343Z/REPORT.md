# Independent SHIP verdict: PASS+NOTES

PR #8 exact HEAD 59cdcae79256653b9b141ecc4033afaa8b551337, tree 349bc20be97a6cd9fcb5c28cb34f5c7b8d9733db, base ac9202277cf354d414d5bb0e8ad21e5688699921. Fresh independent verifier, not author or formal reviewer. Local worktree clean and remote PR head confirmed equal after verification. No implementation changes, publication or merge by verifier. Only synthetic temporary fixtures and ephemeral API servers used; no running proxy, real configuration/credentials, service or deployment touched.

The prior FAIL remains valid for old head f18c988f. Appended repair 59cdcae resolves the observed preparation-denial panic. No new blocking findings.

## Gates

Full discovery and exceptions retained in plan.txt. Exact run commands are in commands.txt. All exit statuses have corresponding .exit files; logs recorded independently:

- `go vet ./...`: failed exit 1, vet.log. Exactly the three existing Kimi/Claude ForAPIKey lock-copy diagnostics. Independent exact-base vet.log comparison is byte-identical to base-vet.log. Baseline exception; not a new regression.
- `go test ./...`: passed exit 0, full-tests.log.
- Contract command `go test -count=1 -v -run 'TestClientProfile|TestManagementClientProfile' ./internal/clientprofiles ./internal/config ./internal/access/config_access ./internal/api/... ./sdk/cliproxy/auth ./sdk/auth`: passed exit 0, contract.log.
- Issue/interface command `go test -count=1 -v -run TestClientProfileEnforcement ./test ./internal/api/... ./sdk/api/handlers/... ./sdk/cliproxy/auth ./sdk/cliproxy/session`: passed exit 0, live.log.
- Focused race covering enforcement/binding/exclusive frontend/derived management cases: passed exit 0, race.log.
- Existing Payload/Prefix/Session/Websocket/Bootstrap regression command: passed exit 0, regressions.log.
- Required/CI server build `go build -o /tmp/cliproxy-ship-20261008/cpa003-ship/final/test-output ./cmd/server`: passed exit 0, build.log. Output relocated outside exact checkout, same build effect.
- Read-only `gofmt -l` on every changed Go file: no differences, passed exit 0, format.log. No implementation edits; mutating repository format command not needed for independent verifier.
- `git diff --check ac920227..HEAD`: passed exit 0, diff-check.log.
- CI translator and AGENTS path guards: no forbidden paths changed, guards.log.
- Independent public Manager adversarial overlay: passed exit 0, independent-adversarial.log.
- Added typed503/no-retry/both-account-health matrix with retry budget three: passed exit 0, terminal-no-retry.log.
- Actual head common public HTTP probe: passed exit 0, head-common-live.log.

CI catalog refresh not run because it fetches/writes moving external inputs; retained catalog built/tested. Dev server with working configuration not run because task prohibits live config/services; disposable real HTTP/WS server fixtures replace that check. Placeholder single-test AGENTS command deduped by focused suite; equivalent build invocations deduped. Tag release, cross-platform packaging, Docker publication and unrelated separate multi-language plugin Makefile builds not run: release environment/publication outside scope and no packaging/example changes. No discovered command omitted without reason. Full-verify aggregate remains red from baseline vet; no hosted green CI claim.

## Live

EVIDENCE.md is absent. Independently reran repo-native API fixtures at exact SHA. live.log and api-websocket-transcript.txt contain actual HTTP/WebSocket responses and upstream recording evidence:

- Strict bare helper HTTP200 uses A; disabling A yields HTTP503 target_unavailable without another upstream call. Legacy control HTTP200 observes enabled B.
- Images use A; disabled A yields explicit 503 without B.
- Bound Responses WebSocket initial completion uses A. Key/profile edits and Automatic key deletion yield typed503 client_profile_stale. Only and Automatic owner loss yield typed503 profile_owner_unavailable with exactly one original A call.
- Unsupported strict search/videos/direct SDK injection, bound realtime, duplex and optional wsrelay reject before effects. Attached relay handler count is zero.
- Realtime known association/revocation reject; opaque legacy issuer remains accepted to the normal 426 websocket_upgrade_required stage.
- ClaudeOnly uses A while independent CodexAutomatic uses C.

Base comparison evidence retained from the exact immutable ac920227 checkout and newly rerun at head. The same common public APIs and existing routing fixture are used, not new enforcement helpers. base-live.log shows strict HTTP503 before any business attempt, legacy HTTP200. head-common-live.log shows strict HTTP200 A, legacy HTTP200 B. Both probes actually executed and exited zero. common_probe_test.go and base_common_combined_test.go preserve the setup; Go overlay base mapping uses canonical /private/tmp paths. Earlier fixture setup mistakes were corrected before final evidence and are not implementation findings.

## Blast radius and repair

Safety fact: selected A can never be substituted by enabled B. Independently rerun permanent matrix covers ordinary/helper/alias execute/count/stream, retry/refresh/bootstrap, disabled/cooldown/expired/deleted targets, global disabled/unsupported duplicates, provider/executor/pin conflicts, scheduler wrong/unknown IDs, strict Home/mixed/plugin/direct rejection, independent provider rules and WS continuation. Recorder assertions plus B controls pass.

Original independently authored overlay includes 24 preparation mutation cases, copied snapshot immutability, trusted owner unavailable, six pre-preparation mutations and nine account-health replacement cases. All pass. Pre-preparation scheduler callback changes current target account_ref or removes trusted owner availability after selection: each execute/count/stream call returns expected typed policy error, scheduler count one, request-preparer count zero, upstream count zero, selected health unchanged.

Additional TestShipTerminalNoRetry configures retry budget three and tests returned B, changed ID and changed account_ref across all three public Manager methods. Each returns typed ExecutionError code profile_target_changed/status503 after one preparer call, zero upstream attempts, no B execution, and unchanged A/B LastError/status/unavailability/retry/model/Quota/failure state. terminal-no-retry.log records all nine cases. The new permanent author test adds real target removal before preparation and checks exact preparation counts and typed errors without recover masking panics.

Repair diff inspected independently: Execute and ExecuteCount return request-stop errors immediately before nil-auth dereference, MarkResult and retries. Stream releases the attempt then returns the terminal error. Nonterminal ordinary preparation failure accounting retains original selected Auth. Outer public boundaries unwrap typed policy errors and stop retry. No business payload mutation introduced. Existing final target/binding checks remain read-only, so executor payload configuration barrier remains intact.

No retrospective guarantee is asserted for already unbound sessions. Current capabilities/preview share enforcement inventory and list explicit unsupported modes. No new timeout or deployment change.

PASS+NOTES is tied only to exact 59cdcae79256653b9b141ecc4033afaa8b551337. Notes are baseline vet findings and explicit non-run environment/publication checks above. Earlier FAIL is repaired; no merge or review comment posted by verifier.
