# CPA-002 final independent ship verification

Verdict: PASS+NOTES

PR https://github.com/dhruvkelawala/CLIProxyAPI/pull/7
Head d876f1ac2bdc0b1fcb85c834ce680ace010c183b
Tree 22e823ad1b1656c01010b0040735498c5a92d0d6
Base f25648a8a563864fa37141dad5b619e456c672d2

The earlier FAIL at 822d69a9 is fixed at this head. No outstanding behavior or safety finding was found. The original failing evidence remains in ../exclusive-bypass-red.log. This current verdict covers the repaired patch, including persisted revocation fingerprints and credential ownership independent of frontend provider replacement. Full verify remains red for three unchanged baseline vet findings.

## Gates

Re-read repaired implementation and public contract, plus updated PR body/head/status. Original discovery read AGENTS.md, CLAUDE.md, CI workflows, issue 2 and full implementation diff. Final worktree was clean and exact head before and after verification. GitHub statusCheckRollup is empty.

| Check | Result | Evidence |
|---|---|---|
| Read-only gofmt audit, all 23 changed Go files | Passed | format.log |
| git diff --check base..HEAD | Passed | command exit 0 |
| go test ./... | Passed | full-test.log |
| Focused -count=1 -v tests, all profile packages plus independent overlay | Passed | live-and-blast.log |
| Focused -race -count=1 -v tests, including SDK access and TestShip tests | Passed | race.log |
| Required build go build -o test-output ./cmd/server then remove generated binary | Passed | command exit 0 |
| Ticket server build to temporary report directory | Passed | ticket-build artifact |
| Retained Codex model catalog validation | Passed | catalog.log |
| go vet ./... | Failed, identical three baseline lock-copy defects | vet.log is byte-identical to ../base-vet.log |
| Translator and AGENTS path guards | Passed | no changes to those paths |
| CI catalog refresh | Not run | fetches and rewrites moving unrelated remote inputs; incompatible with exact clean-head verification |
| go run ./cmd/server with working configuration | Not run | actual registered Gin routes ran with synthetic temporary fixtures instead |
| Tag-triggered release/Docker/platform packaging | Not applicable | no release or deployment authorized |
| Separate plugin example Makefile packaging | Not run | separate Go/C/Rust example modules, unchanged; outside server ticket validation |

Full verify aggregate remains red for baseline vet. Original whole-tree gofmt audit reported three unrelated baseline files. All changed files remain formatted. No hosted CI pass is claimed.

## Live

live-and-blast.log contains captured actual authenticated v8 management requests and business AuthMiddleware responses. The normal lifecycle contract still passes: unauthenticated management401, capability enforcement:false, enrollment and safe inventory/list/preview, independent provider policies, Only association503, unrelated legacy200, rotation old401/new503, stale412, explicit Automatic edit200, deletion401. Safe responses exclude raw keys, OAuth metadata and fingerprints.

The original independent exclusive-provider probe now passes at this head. It enrolls and associates a strict profile through actual management routes, then uses public SDK RegisterProvider, SetExclusiveProvider and manager.SetProviders(RegisteredProviders). The strict request remains503 and the deleted key returns401. New native tests also verify empty provider sets cannot bypass strict/revoked identities. These are the exact previously failing request paths.

adversarial-http.log contains two additional fresh independent tests:

- A fake provider returns an opaque principal and forged binding metadata. Strict configured credentials still return503. Whitespace-wrapped protected X-Api-Key, mixed Authorization/X-Api-Key and protected query credentials return401. Spaced Bearer strict credentials return503.
- An unrelated plugin identity returns200 with binding_metadata_present:false; other plugin metadata is retained. A plugin claim to a protected configured principal absent from the actual request returns401.
- A newly constructed access manager loaded from persisted disk rejects a deleted key401. Generic api-key re-add succeeds as a config operation but does not lift the persisted denial, which remains401. Redacted generic access JSON round-trip retains revocation. Direct mutation of revoked-client-profile-keys returns400 read_only_field. Explicit profile association clears the fingerprint and restores its valid Automatic request200.

Base comparison remains ../base-live.log at exact base. Existing synthetic keys200, invalid401 and profile route404. No behavior introduced here exists on base; legacy key behavior is preserved.

## Blast radius

The safety fact is that persisted identity/policy/revocation precedes publication, and provider replacement cannot drop configured profile ownership.

The fresh overlay recomposes the current repository test file and appends independent probes, so no new repository tests were hidden. TestShipIndependentPersistenceBoundary observes every successful publication callback and reloads the already persisted profile/key snapshots. Enrollment's HTTP response UUID matches disk metadata. A real storage failure during association yields500 with no publisher invocation. Successful strict association returns503; its deletion revokes401 and leaves the separate legacy key200. TestShipIndependentEnrollmentFailureHTTP still returns500 for unencodable synthetic metadata and preserves byte-identical credential JSON plus absent runtime UUID.

The owner is installed by ApplyAccessProviders through SetProvidersAndCredentialOwner. Subsequent SetProviders replaces the providers but retains the owner. The owner independently scans actual credential locations, rejects revoked fingerprints, reauthenticates protected configured bindings, and strips plugin binding metadata. Copied inventory/maps avoid external mutation. The explicit BindAcceptedCredential seam applies that owner to trusted derived results; native tests reject removed/revoked native issuers while retaining unrelated opaque plugin issuers.

Native final-head tests independently executed by this verifier prove:

- Rotation/deletion fingerprints persist, survive fresh manager/restart and remain denied under exclusive or empty providers. Explicit reuse clears the old fingerprint.
- Failed rotation save500 leaves the original bound identity valid. No partial revocation or activation is published.
- Revocation is redacted from access/full JSON projections, preserved by projected writes, protected against direct PUT/DELETE, and retained in canonical persisted configuration.
- Revocation-only startup creates access ownership when none was supplied. Revoked credentials fail401 on business and attached websocket routes. Disabling websocket authentication is rejected on reload.
- Last-key deletion fails409 rather than producing public empty-key access. Missing activation owner fails503 before save. Stale revision412 and disk/runtime mismatch409 preserve runtime/disk integrity.
- Runtime-only mixed-case/whitespace variants reject enrollment and imported Only targets; no UUID publication/store invocation. Unsupported provider copies remain ambiguous.
- UUID identity survives native Claude/Codex refresh, file/store reload, watcher synthesis and external rename. Enrollment failure preserves shared storage and file. Immutable copied bindings survive later configuration edits.

Safety scrutiny of the repair found no unresolved defect. The previous authentication finding is resolved, rather than waived. The persistent denylist is a new public schema covered by one-way merge approval. Strict account routing remains deliberately unavailable until CPA-003. Existing Automatic-session invalidation and CPA-007 deployment remain deferred. Dashboard work remains external.

## Reproduction and integrity

Overlay source independent_test.go, mapping overlay.json. Run from the exact final worktree:

```sh
go test -overlay /tmp/cliproxy-ship-20261008/cpa002-ship/final/overlay.json -count=1 -v -run 'TestShip|TestClientProfile|TestManagementClientProfile' ./internal/api ./sdk/auth ./sdk/cliproxy/auth ./internal/config ./internal/access/config_access ./internal/clientprofiles
```

All probes use synthetic temporary config/auth and direct ephemeral httptest requests to registered Gin routes. No production proxy, localhost8317, real config/auth, T3, tunnels, service, dashboard, tag or release was touched. No implementation, PR, push or merge mutation occurred. Exact head and clean worktree confirmed after all checks.
