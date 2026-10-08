# Client profile management API

CPA-002 implements contract version 1 on `/v8/management` only. Every route below uses the existing management authentication and availability middleware. Send the management Bearer credential over the authenticated management connection. No v0 profile routes exist. The dashboard is developed separately by Claude.

CPA-003 enables subscription enforcement. Authenticated Only requests resolve a single enrolled credential from the complete current inventory. That credential serves the request, or the gateway returns an explicit error. Legacy keys and independent Automatic provider rules retain the eligible pool. Strict mixed-provider routes reject before execution so a Claude failure cannot escape through a Codex Automatic rule. Scope covers matching requests authenticated by this gateway. Native auxiliary generation, explicit delegation and traffic bypassing the gateway are outside it. The dashboard is owned separately and should use the advertised capabilities.

## Identities and persistence

- `profile_ref` is a random UUID for a named client profile.
- `key_ref` is a different random UUID for an association to one existing client API key. A key can belong to one profile. Raw values remain in the unchanged legacy `api-keys` list; association records persist a SHA-256 fingerprint, never another raw-key copy.
- `account_ref` is a random UUID stored in credential JSON metadata. Enrollment is explicit, never a startup migration. It identifies a credential record, not a vendor subscription.
- `credential_ref` is an opaque transient handle for enrollment of a currently loaded credential. It changes after filename/runtime-ID changes and is never a policy target.

The canonical YAML paths are `access.client-profiles` and `access.client-profile-keys`. `access.revoked-client-profile-keys` is a profile-owned persisted list of lowercase SHA-256 fingerprints for rotated/deleted keys, never raw values. It is read-only through generic config writes and omitted from JSON projections; sensitive full YAML backup retains it. Older flat layout keys `client-profiles` and `client-profile-keys` round-trip through the existing YAML boundary. Profiles have mandatory rules for both `claude` and `codex`. No omitted rule defaults to Automatic. Unknown providers, missing/unknown modes, invalid UUIDs and an Automatic rule with an account target are rejected. Prefer and fallback are unsupported.

File-backed enrollment supports plain metadata and native Claude/Codex token storage. It privately copies native storage, saves to a sibling temporary file, syncs and renames before manager publication. Other stores must implement the explicit atomic `EnrollmentStore.SaveEnrollment` contract; built-in non-file stores do not yet support enrollment. Custom storage, virtual plugin credentials and configuration API-key credentials are unsupported. The account inventory returns `enrollment_supported` and `target_supported` for the actual loaded record. A caller/imported UUID cannot turn configuration API-key, virtual plugin, runtime-only, custom storage or a nontransactional-store credential into an Only target; it returns `target_unsupported`. These records still participate in global duplicate checks. Unsupported providers still appear in inventory so copied UUIDs cannot hide from duplicate checks.

Refresh and request preparation preserve the current reserved UUID even if an executor omits or replaces it. Ordinary file/store reload and external file rename preserve metadata. A copied file with the same UUID is ambiguous, including disabled copies or a copy on an unsupported provider. No reference is regenerated. Removal leaves saved profiles unresolved; the operator must explicitly retarget them. Rename/reload does not promise uninterrupted availability during watcher reconciliation.

Profile ownership runs independently after frontend authentication, including exclusive plugins and the empty-provider permissive path. It derives a bound identity only from a single distinct credential presented in the supported Authorization, X-Goog-Api-Key, X-Api-Key, `key`, or `auth_token` channels and independently validates it against a copied configuration inventory. Multiple distinct credentials involving a bound key are rejected with 401, including repeated header/query values. Plugin-supplied binding metadata is discarded; plugin claims cannot create or replace a binding. A plugin's unvalidated claim naming a protected/revoked credential is rejected. Unrelated unbound plugin identities keep existing authentication behavior. Custom opaque credential channels cannot establish profile ownership; integrations must present profile keys through the supported channels. A manager that has installed configured ownership fails closed if that owner is deliberately cleared. Restore a configured owner or initialize a new manager; removing ownership never downgrades protected keys to legacy.

Rotation/deletion atomically persists revocation before activating it. Revoked credentials are rejected with 401 across reload and a new manager/process using the saved config, even after all profiles are deleted and even if a plugin would still accept them. Merely adding the raw key back to `api-keys` does not reactivate it. Explicit association to a profile (after adding the existing key) or rotation to that value clears its revocation. Rotation to the currently active key returns 409 `key_already_exists`. Revocations have no automatic expiry or pruning API; editing the trusted local YAML can remove them. Revoked-only configurations still require WebSocket authentication. Existing sessions remain subject to the CPA-003 limitation below.

## Optimistic edits and sessions

`GET /client-profiles` and `GET /client-profile-keys` return `ETag`. Profile/key mutations require that exact value in `If-Match`. The `revision` JSON field contains the same quoted ETag string, for example `"\"sha256hex\""`. Missing preconditions return 428; stale preconditions return 412. Refresh the list and retry deliberately. If the persisted profile/key/key-value snapshot differs from the runtime snapshot, reads and mutations return 409 `reload_pending` until reload completes. An edit detected during save returns 412 and leaves runtime unchanged.

Profile and key `revision` numbers start at 1 and increase on management edits. Requests capture copied bindings with the accepted key fingerprint, key/profile revisions and policies. The snapshot stays unchanged through retries and refresh. Each Responses WebSocket turn compares the original association with the current owner. Key deletion, association changes, fingerprint rotation, profile edits and owner loss terminate old sessions, including all-Automatic sessions. An edit never widens or retargets an existing request. Reconnect authenticates anew. Connections authenticated before their key gained a profile have no captured binding and are not retrospectively constrained. Use dedicated keys and reconnect after association. Legacy realtime secrets whose opaque plugin issuer identity differs from their original raw credential cannot recover that credential; no profile guarantee is claimed for those earlier unbound sessions.

Writes persist the validated configuration atomically before publishing access policy, and successful responses wait for immediate access activation. Missing activation ownership returns 503 before writing. A failed save publishes nothing. Sibling-file rename must be supported; a read-only directory or mounted-file replacement failure returns a persistence error. Generic v8 config mutations cannot alter profile-owned subtrees. JSON config views redact association fingerprints and restore omitted fingerprints when writing an unchanged projection back. Authenticated YAML backup retains the persisted fingerprint and existing legacy API-key secrets.

Deleting a profile with associated keys returns 409. Deleting the last configured API key returns 409 `last_client_key`, because the legacy empty-key configuration permits public access. Add another key before revoking the final key. Deleting an association also revokes its raw legacy API key; it never converts that key into an unrestricted key. Rotation replaces the associated legacy value while retaining `key_ref` and incrementing its revision. It rejects a replacement already present in the key list. Dedicated API keys are required when clients need independent profiles.

## Routes and JSON

All successful operations return HTTP 200. All read/preview results exclude raw client-key values and OAuth metadata. Account labels may contain operator-supplied names. `accounts` is an array; empty `profiles` is an array and an empty `keys` value may be null. Responses use `Cache-Control: no-store` where ETags or capability data are returned.

| Method and path | Request | Response |
|---|---|---|
| GET `/client-profiles/capabilities` | None | Capability object below |
| GET `/client-profiles` | None | `{revision, profiles, keys, accounts, target_states}` |
| POST `/client-profiles` | `{label, policies}` | `{revision, result: Profile, session_behavior}` |
| PUT `/client-profiles/:profile_ref` | Complete `{label, policies}` replacement | Same as create, revision increased |
| DELETE `/client-profiles/:profile_ref` | None | `{revision, result:{deleted:profile_ref}, session_behavior}` |
| POST `/client-profiles/preview` | `{profile_ref}` OR `{policies}` | `{revision, policies, target_states, enforcement:true, strict_requests:"enforced", session_behavior}` |
| GET `/client-profile-keys` | None | `{keys}` plus ETag |
| POST `/client-profile-keys` | `{label, profile_ref, api_key}` with an existing legacy key value | `{revision, result: Key, session_behavior}` |
| PUT `/client-profile-keys/:key_ref` | Optional `{api_key, label, profile_ref}`; omitted fields retained | Same as association, revision increased |
| DELETE `/client-profile-keys/:key_ref` | None | `{revision, result:{deleted:key_ref}, session_behavior}` |
| GET `/client-profile-accounts` | None | `{accounts}` |
| POST `/client-profile-accounts/enroll` | `{credential_ref}` | `{credential_ref, account_ref}`; no caller-selected UUID allowed |

Mutations reject unrecognized request fields. Enrollment modifies the credential record, not the configuration, so it does not take `If-Match`; the manager serializes the mutation and rejects reenrollment.

```json
{
  "contract_version": 1,
  "management": true,
  "enforcement": true,
  "strict_requests": "enforced",
  "providers": ["claude", "codex"],
  "modes": ["automatic", "only"],
  "session_behavior": "fresh_session_required",
  "binding_requires": ["websocket_auth_enabled"],
  "enrollment_stores": ["file"],
  "enrollment_storage": ["metadata", "native_claude", "native_codex"],
  "unsupported": ["home_strict", "mixed_provider_strict", "bound_duplex", "bound_realtime", "search_strict", "videos_strict", "direct_transport_strict", "bound_wsrelay", "unknown_provider_strict", "plugin_virtual_credentials", "unenrolled_credentials", "non_file_enrollment", "prefer", "fallback"]
}
```

Programmatic invalid profile configuration rejects business requests with 503 until a valid reload, including optional WebSocket routes. Authenticated v8 management and `/management.html` remain available for repair. Invalid reloads preserve the previous runtime configuration. A bound embedded server creates authentication ownership if the caller omitted an access manager; a later management association with no access manager returns `activation_unavailable`.

Association requires `oauth.providers.aistudio.ws-auth: true` so optional relay authentication cannot bypass the interim strict rejection. The API returns `websocket_auth_required` and never changes that setting automatically.

```json
{
  "profile_ref": "11111111-1111-4111-8111-111111111111",
  "label": "T3 Claude",
  "revision": 1,
  "policies": {
    "claude": {"mode": "only", "account_ref": "33333333-3333-4333-8333-333333333333"},
    "codex": {"mode": "automatic"}
  }
}
```

```json
{
  "key_ref": "22222222-2222-4222-8222-222222222222",
  "label": "T3 desktop key",
  "profile_ref": "11111111-1111-4111-8111-111111111111",
  "revision": 1
}
```

```json
{
  "credential_ref": "credential_opaquehandle",
  "account_ref": "33333333-3333-4333-8333-333333333333",
  "provider": "claude",
  "label": "Work account",
  "available": true,
  "state": "available",
  "enrollment_supported": true,
  "target_supported": true
}
```

`target_states` on the list is `{profile_ref: {provider: state}}`; on preview it is `{provider: state}`. States are `automatic`, `available`, `target_removed`, `target_unavailable`, `target_unsupported`, `duplicate_account_ref`, `provider_mismatch`, `invalid_account_ref`, `unknown_mode` or `unknown_provider`. Account state can also be `not_enrolled` or `unavailable`. Availability is inventory-level enablement/unavailability, not model eligibility, a quota guarantee or evidence of a historical serving account. Create/update reject Only targets that are missing, ambiguous, mismatched or unavailable. Preview/list preserve saved unresolved policies visibly.

Domain errors have the shape `{"error":{"code":"machine_code","field":"optional_field"}}`. HTTP 400 means malformed JSON/unknown body fields; 404 means missing profile/key/credential; 409 means profile in use, last-key deletion, reenrollment, duplicate replacement key, unsupported enrollment or pending reload; 412/428 are precondition failures; 422 means invalid policies, target or association, including a nonexistent legacy key; 500 means persistence failure; 503 means missing credential owner or access activation owner. Existing management authentication uses its existing error envelope. Generic config mutation errors retain the existing config envelope.

## Reproducible backend evidence

```sh
go test -count=1 -v -run 'TestClientProfile|TestManagementClientProfile' ./internal/clientprofiles ./internal/config ./internal/access/config_access ./internal/api/... ./sdk/cliproxy/auth ./sdk/auth
```

`TestManagementClientProfileContract` drives the actual authenticated v8 router using temporary configuration, file-backed synthetic credentials and the business authentication middleware. Its transcript contains safe response shapes, authenticated Only bindings, legacy independence, rotation, stale edits and revocation. Additional tests exercise failed saves without activation, missing activation ownership, external edits before reload, unsupported-provider duplicates, token-storage/file rollback and UUID survival across refresh/store/watcher rename/reload.

CPA-007 is deferred. This contract does not authorize changing a running proxy, real credentials/configuration, T3, tunnels, services or releases.


## Request coverage and errors

| Request path | Supported behavior for a strict profile |
|---|---|
| Chat, completions, Claude messages, Responses and compact | Manager enforces the canonical provider's rule before selection and every upstream attempt |
| Bare helper models and OAuth aliases | Same constraint applies after routing and alias resolution |
| Count tokens | Same constraint applies when an executor performs the count |
| Streaming and bootstrap retry | Snapshot and target survive bootstrap, retry and refresh |
| Images generation and editing | Common manager execution applies the same constraint |
| Responses HTTP aliases under `/backend-api/codex` | Same manager enforcement |
| Responses WebSocket continuation and reconnect | Each turn validates the original binding; each reconnect authenticates afresh |
| Scheduler plugins | Receive the constrained pool; a handled ID outside it fails closed |
| Home, mixed provider routing and plugin-owned executors | Reject before dispatch or execution |
| Native duplex/steering and realtime/live | Reject all profile-bound sessions, including Automatic, before creating a secret or relaying traffic |
| Optional wsrelay routes | Reject all profile-bound requests before invoking the attached relay handler |
| Standalone search, videos and direct manager HTTP/selection APIs | Reject strict requests before selection, acquisition or transport |
| Providers without a Claude/Codex policy | Reject strict requests explicitly |

Business policy failures return HTTP 503 with `{"error":{"code":"machine_code","field":"optional_field"}}`. WebSockets send the same error within an event with `type:"error"` and `status:503`, then terminate the session. Codes include `target_removed`, `target_unavailable`, `duplicate_account_ref`, `provider_mismatch`, `target_unsupported`, `profile_target_changed`, `profile_pin_conflict`, `client_profile_stale`, `profile_owner_unavailable`, `profile_home_unsupported`, `profile_mixed_provider_unsupported`, `profile_provider_unsupported`, `profile_protocol_unsupported`, `profile_plugin_unsupported` and `profile_scheduler_invalid`. Existing upstream, expired-token and model cooldown errors retain their normal envelopes. No policy failure substitutes another subscription.

Final target and current-binding checks are read-only. They do not change model IDs or business payloads, and leave executor payload rules as the final semantic barrier before transport.

The isolated evidence command is `go test -count=1 -v -run TestClientProfileEnforcement ./test ./internal/api/... ./sdk/api/handlers/... ./sdk/cliproxy/auth ./sdk/cliproxy/session`. The fixture uses actual server HTTP/WebSocket routes, synthetic credentials, temporary files and ephemeral loopback ports. A separate B canary proves the other account remains enabled.

### Conversation bindings

Local conversation affinity is isolated by the authenticated client-key identity, including for unprofiled keys. Reused session IDs, parent aliases and legacy content hashes cannot read, rewrite or release another client's binding. Helpers using the same key retain parent affinity. SDK callers without caller-scope metadata retain their existing namespace. LCP matching already applies caller isolation.

Trusted host-plugin affinity diagnostics aggregate clients without selecting or refreshing a binding. If one session is independently bound to different accounts, the diagnostic returns `ambiguous`.
