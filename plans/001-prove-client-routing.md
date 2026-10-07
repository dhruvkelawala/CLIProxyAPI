# CPA-001: Prove strict client account routing with an isolated recording fixture

- **Status:** TODO
- **Tracking issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/5
- **Issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/1
- **Priority:** P1
- **Effort:** 1–2 days (rough)
- **Risk:** Medium
- **Planned at:** a4acc9f752bd46571f737a10c04bf413656ab06b (v8.0.15), 2026-10-07
- **Branch:** create a topic branch from `sumo/main`; retain existing Go module/package identity.
- **Depends on:** None; ready to pick up

Read this ticket and AGENTS.md before editing. Drift check: `git diff --stat a4acc9f752bd46571f737a10c04bf413656ab06b..HEAD -- test/ sdk/cliproxy/auth/scheduler_test.go sdk/api/handlers/handlers_metadata_test.go sdk/api/handlers/handlers_stream_bootstrap_test.go sdk/api/handlers/openai/openai_responses_websocket_test.go plans/`. If relevant source changed, re-read the cited behavior before following the steps. Keep changes focused; update plans/README.md when finished.

## Outcome
Choose a supported enforcement boundary for account-bound client profiles before implementing them. Produce repeatable evidence that a strict client can use account A while account B stays enabled, and cannot silently fall back when A fails. This is a spike and fixture ticket; production profiles are implemented in CPA-002 and CPA-003.

## Context and current state
The backend is CLIProxyAPI 8.0.15. Client keys currently authenticate access, without an account policy. The deployment to support later is one gateway on a Mac mini, accessed locally by the Mini and through a persistent localhost tunnel by a MacBook. T3 source must remain unchanged.

- `internal/access/config_access/provider.go` authenticates the configured string-list keys.
- `sdk/api/handlers/handlers.go` puts a hashed caller scope in execution metadata.
- `sdk/cliproxy/auth/conductor_selection.go` passes scheduler options and candidates to plugins. A handled reject terminates selection. An unknown selected auth ID can return unhandled and fall back, so returning an ID alone is insufficient.
- `sdk/cliproxy/auth/scheduler_test.go` already contains `TestManagerPluginSchedulerSkippedWhenHomeEnabled`. A scheduler-only extension cannot be assumed to enforce Home requests.
- Internal `WithPinnedAuthID` restricts selection but is not a public client-key policy.

Follow the in-memory Go test pattern in `scheduler_test.go`, `TestRequestExecutionMetadataIncludesHashedCallerScope`, and `TestExecuteStreamWithAuthManager_PinnedAuthKeepsSameUpstream`. Do not replace these with source-string assertions.

## Steps
1. Run the baseline and inspect the named tests. Confirm caller identity propagation, explicit rejection, fast scheduling and the Home bypass. Verify: `go test ./sdk/cliproxy/auth ./sdk/api/handlers` exits 0.
2. Add a reusable fixture with two in-memory/stub accounts and recorders that expose account identity and request type only. Use temporary dirs and ephemeral ports. Cover ordinary model IDs for main and background-style requests, streaming, retries, aliases and WebSocket/session continuation. No real OAuth files, prompts, credential headers or production gateway calls.
3. Probe the plugin seam versus an ingress policy/internal-pin seam. Force A to be unavailable while B remains available. If a plugin misses any supported path, document the small backend change needed. A missing/inactive plugin must not be a silent bypass of a future strict profile.
4. Write `plans/routing-enforcement-decision.md`: coverage matrix, recommended owner/interface, durable account-reference approach, conflict rules, and which Home modes are supported or explicitly rejected.

## Verification and done criteria
- [ ] `go test -count=1 -run TestClientProfileFixture ./test ./sdk/cliproxy/auth ./sdk/api/handlers/...` exercises new fixture tests and exits 0.
- [ ] At least one request reaches A, and the fixture detects a deliberately wrong choice of B.
- [ ] Each probed path has observed evidence; missing paths are marked unsupported rather than claimed as passed.
- [ ] The decision explains why disabling the other account would hide leakage and is not a valid canary.
- [ ] `go test ./...` and `go build -o /tmp/cliproxyapi-ticket-build ./cmd/server` exit 0.
- [ ] No production config, T3 source, live services or source outside the listed scope changes.

## Stop conditions and maintenance
If a complete guarantee needs Home control-plane changes outside this repository, document that boundary and prefer an explicit unsupported-mode rejection for release 1. Do not make broad executor/translator refactors. Track new protocol paths against the coverage matrix when rebasing upstream.

## Scope

Files/directories in scope:

- `test/`
- `sdk/cliproxy/auth/scheduler_test.go`
- `sdk/api/handlers/handlers_metadata_test.go`
- `sdk/api/handlers/handlers_stream_bootstrap_test.go`
- `sdk/api/handlers/openai/openai_responses_websocket_test.go`
- `plans/`

Do not change T3 source, live gateway configuration or unrelated files. Match existing repository conventions. Open a PR against the fork's `sumo/main` when publishing authorized implementation work; do not target upstream `main` by accident.
