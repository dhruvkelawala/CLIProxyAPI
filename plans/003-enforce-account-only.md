# CPA-003: Enforce the selected subscription across retries, helpers, streams and WebSockets

- **Status:** TODO
- **Tracking issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/5
- **Issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/3
- **Priority:** P1
- **Effort:** 2–3 days (rough)
- **Risk:** High
- **Planned at:** a4acc9f752bd46571f737a10c04bf413656ab06b (v8.0.15), 2026-10-07
- **Branch:** create a topic branch from `sumo/main`; retain existing Go module/package identity.
- **Depends on:** [CPA-001](https://github.com/dhruvkelawala/CLIProxyAPI/issues/1), [CPA-002](https://github.com/dhruvkelawala/CLIProxyAPI/issues/2)

Read this ticket and AGENTS.md before editing. Drift check: `git diff --stat a4acc9f752bd46571f737a10c04bf413656ab06b..HEAD -- internal/clientprofiles/ internal/api/server_middleware.go sdk/api/handlers/handlers sdk/api/handlers/claude/ sdk/api/handlers/openai/ sdk/cliproxy/auth/conductor sdk/cliproxy/session/ test/ plans/`. If relevant source changed, re-read the cited behavior before following the steps. Keep changes focused; update plans/README.md when finished.

## Outcome
Subscription selection is a required feature of the fork. Here, subscription means the upstream account credential; it is separate from the client/profile key used to access the gateway.

A client authenticated with an A-only profile can send standard model IDs while both accounts are enabled. Every applicable gateway request uses A, or receives an explicit error. There is no cross-account substitution, including retry, background requests and reconnect.

## Context and current state
Account prefixes already route matching models, but T3 custom Claude models lose runtime inheritance and helper IDs have not been established. Enforcing the profile at the proxy avoids depending on each caller's model-prefix behavior. The current internal `WithPinnedAuthID` feeds `PinnedAuthMetadataKey`; candidate selection/retry checks constrain matching credentials. Existing WebSocket sessions can establish their own pins, so conflicting pins must be handled deliberately.

The guarantee covers requests authenticated under this profile. Native T3 auxiliary generation, explicit other-provider delegation and calls bypassing the gateway are outside it. State that scope; never claim all application traffic is pinned.

Use CPA-001's fixture/decision and CPA-002's durable profile contract. Follow `handlers_stream_bootstrap_test.go`, `openai_responses_websocket_test.go` and scheduler tests as interface-driven exemplars.

## Steps
1. Resolve policy after successful authentication and before account selection. Centralize target/provider validation and errors in the profile owner; preserve the ordinary model ID and capabilities.
2. Apply the account constraint at the enforcement boundary proved in CPA-001. Include count-token paths where they execute upstream, aliases, retries, refresh, stream bootstrap and WebSocket continuation. Do not mutate business payloads after the executors' existing final payload-rule barrier.
3. Reject an explicit profile/session-pin conflict. Reject unsupported Home/plugin modes for strict profiles if the guarantee cannot be maintained. A missing plugin or policy owner cannot silently fall back to unrestricted routing.
4. Keep keys without a policy and Automatic profiles on their existing paths. Provider rules are independent: a Claude constraint must not force Codex onto a Claude credential.
5. Extend the recording fixture with named `TestClientProfileEnforcement...` cases for A-only success, A disabled/cooldown/expired/deleted, retry after failure, helper-style bare IDs, stream reconnect, aliasing, conflicting pins, reload and an unrestricted legacy key.

## Verification and done criteria
- [ ] `go test -count=1 -run TestClientProfileEnforcement ./test ./internal/api/... ./sdk/api/handlers/... ./sdk/cliproxy/auth ./sdk/cliproxy/session` exits 0.
- [ ] A-only requests reach A; B's recorder stays at zero for every strict failure path.
- [ ] A missing/unknown policy target returns a documented machine-readable error.
- [ ] Legacy keys and Automatic still reach the eligible pool.
- [ ] Existing model-prefix, Codex-session and payload-barrier regressions pass.
- [ ] `go test ./...` and `go build -o /tmp/cliproxyapi-ticket-build ./cmd/server` exit 0.
- [ ] Capability advertising and management preview reflect the actual supported modes.

## Stop conditions and maintenance
Stop if enforcement depends solely on a scheduler callback bypassed by supported request paths, or if a target failure falls back to another credential. Reject unsupported paths explicitly or expand the design after review. Do not create a replacement routing engine. Add each new upstream protocol path to the policy coverage matrix.

## Scope

Files/directories in scope:

- `internal/clientprofiles/`
- `internal/api/server_middleware.go`
- `sdk/api/handlers/handlers*.go`
- `sdk/api/handlers/claude/`
- `sdk/api/handlers/openai/`
- `sdk/cliproxy/auth/conductor*.go`
- `sdk/cliproxy/session/`
- `test/`
- `plans/`

Do not change T3 source, live gateway configuration or unrelated files. Match existing repository conventions. Open a PR against the fork's `sumo/main` when publishing authorized implementation work; do not target upstream `main` by accident.
