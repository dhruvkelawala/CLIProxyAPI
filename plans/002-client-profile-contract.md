# CPA-002: Add durable client profiles and a v8 management contract

- **Status:** TODO
- **Tracking issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/5
- **Issue:** https://github.com/dhruvkelawala/CLIProxyAPI/issues/2
- **Priority:** P1
- **Effort:** 1–2 days (rough)
- **Risk:** Medium
- **Planned at:** a4acc9f752bd46571f737a10c04bf413656ab06b (v8.0.15), 2026-10-07
- **Branch:** create a topic branch from `sumo/main`; retain existing Go module/package identity.
- **Depends on:** [CPA-001](https://github.com/dhruvkelawala/CLIProxyAPI/issues/1)

Read this ticket and AGENTS.md before editing. Drift check: `git diff --stat a4acc9f752bd46571f737a10c04bf413656ab06b..HEAD -- internal/config/ internal/access/config_access/ internal/api/server_management internal/api/handlers/management/ sdk/cliproxy/auth/types.go sdk/cliproxy/executor/types.go sdk/api/handlers/handlers.go internal/clientprofiles/ plans/`. If relevant source changed, re-read the cited behavior before following the steps. Keep changes focused; update plans/README.md when finished.

## Outcome
Persist named client profiles with provider-specific `automatic` or `only(accountRef)` policies and expose a management contract the dashboard can consume. Profile identity, client-key association and upstream account identity must remain distinct. Prefer/fallback is deferred.

## Context and current state
The deployment uses one shared Mini gateway with two machine clients. Changing a generic machine profile can affect several applications or sessions; dedicated profiles are needed for separate T3 provider-instance choices. Model IDs stay ordinary and T3 source stays unchanged.

The current schema contains:
```go
APIKeys []string `yaml:"api-keys" json:"api-keys"`
```
`internal/access/config_access/provider.go` accepts a matching key and returns its principal. `internal/api/server_middleware.go` sets the authenticated request identity. `internal/api/handlers/management/auth_files_fields.go` updates credential metadata through the auth manager. Credential filenames, labels and auth indexes must not be assumed to be durable account references.

Follow the decision from CPA-001, existing config clone/round-trip tests, management tests in `internal/api/server_management_v8_test.go`, and the API-layer conventions in AGENTS.md. New management features use v8 only.

## Steps
1. Define one owning client-profile module with types, validation, persistence/association semantics and structured errors. Keep the legacy `api-keys` list valid. A profile that exists but contains an unknown mode/target is invalid, not Automatic.
2. Implement the durable account reference chosen in CPA-001. Validate uniqueness and prove survival through refresh/reload and any supported rename. Handle removed accounts explicitly.
3. Add authenticated v8 list/create/update/delete profile APIs and a documented capability/probe contract. Return labels, opaque profile/key references, provider policies, target availability and validation errors. Do not include client-key values or OAuth metadata in list/preview results.
4. Define profile edits, key rotation, deletion and active-session behavior. Prefer requiring a fresh session/reconnection initially. Prevent edits from silently changing a running pinned session.
5. Add round-trip, duplicate-reference, unknown-target, missing-provider-policy, authentication and stale-save/error tests. Publish the exact API shapes in `plans/client-profile-api.md` for CPA-005.

## Verification and done criteria
- [ ] `go test -count=1 -run 'TestClientProfile|TestManagementClientProfile' ./internal/config ./internal/access/config_access ./internal/api/... ./sdk/cliproxy/auth` exits 0 with new tests.
- [ ] Existing keys/configuration round-trip unchanged; no automatic migration of production account material.
- [ ] A Claude-only policy leaves the Codex rule independent.
- [ ] Unknown modes/targets and duplicate account refs are rejected.
- [ ] Runtime refresh/reload preserves the chosen durable reference.
- [ ] Unauthorized management requests fail, and list responses contain no credential values.
- [ ] `go test ./...` and `go build -o /tmp/cliproxyapi-ticket-build ./cmd/server` exit 0.

## Boundaries and stop conditions
No public pinning header, new token/cost history, T3 changes or v0 adapters. If durable identity requires modifying watcher serialization beyond this scope, propose that precise addition before implementing. Do not publish the feature as available to clients until CPA-003 enforces it. Keep config cloning, validation and secret projections aligned when adding policies later.

## Scope

Files/directories in scope:

- `internal/config/`
- `internal/access/config_access/`
- `internal/api/server_management*.go`
- `internal/api/handlers/management/`
- `sdk/cliproxy/auth/types.go`
- `sdk/cliproxy/executor/types.go`
- `sdk/api/handlers/handlers.go`
- `internal/clientprofiles/ (new)`
- `plans/`

Do not change T3 source, live gateway configuration or unrelated files. Match existing repository conventions. Open a PR against the fork's `sumo/main` when publishing authorized implementation work; do not target upstream `main` by accident.
