# Shared Mac gateway release

This fork keeps one Mini gateway on loopback port 8317. The MacBook uses its existing localhost tunnel to that gateway. T3 chooses Claude A, Claude B or Automatic through provider instances whose launchers each read a dedicated client key. The dashboard controls the corresponding provider policies. Codex and native providers keep their existing launchers and configuration.

## Build and identify artifacts

Run backend `go test ./...`, `go vet ./...` and `go build -o /tmp/cpa-compile ./cmd/server`. Run dashboard `bunx bun@1.3.14 run verify`. Use clean checkouts at reviewed commits.

```sh
python3 deployment/package_release.py \
  --backend <backend-checkout> --panel <panel-checkout> \
  --output <new-release-directory> \
  --backend-tag sumo-v8.0.15-YYYYMMDD.N \
  --panel-tag sumo-v1.25.4-YYYYMMDD.N
```

The package contains a Darwin arm64 gateway, the single-file `management.html`, an archive retaining upstream license/readmes/config example, SHA256SUMS and a manifest with both source commits. The builder uses committed model catalogs instead of fetching changing catalogs during compilation. Go uses `-trimpath` and embeds the exact backend commit. The panel embeds its fork version.

Review the upstream release workflows before publishing. Backend tag pushes match every tag and run multi-platform builds that refresh catalogs. The panel workflow matches `v*`. This rollout publishes manually built assets with fork-specific `sumo-v*` tags and explicit source targets, without pushing tags or changing Actions/workflows. Never overwrite an existing upstream or fork release. Record the manifest alongside published assets and verify downloaded bytes before installation.

## Isolated proof and rollback

```sh
python3 -m unittest discover -s deployment -p 'test_*.py'
python3 deployment/stage_release.py --release <release-directory> \
  --rollback-binary <previous-gateway-binary> \
  --rollback-panel <previous-management.html>
```

Staging binds a separate ephemeral loopback port, disables remote model updates and uses disposable storage, synthetic client keys and synthetic enrolled file records. An egress trap blocks vendor inference. Disabled Only A must return 503 with `target_unavailable` and no upstream connection for messages, streaming preflight and token count. Only B, Automatic and the legacy key must pass local token counting. The served panel must match its manifest digest and advertise enforcement. The script rehearses the old binary/panel with legacy configuration, then restores the new binary, panel and profile configuration and repeats the matrix.

This artifact check proves startup, strict rejection, management compatibility and restoration. Positive vendor inference and historical serving-account proof require the separate live acceptance matrix. The source recording fixture `go test -count=1 -v -run TestClientProfileEnforcement ./test` proves account selection/retry/HTTP/WebSocket behavior with synthetic upstreams. Keep that transcript alongside the artifact check.

Use `--keep-running` for dashboard inspection. Its output gives the temporary root/port/PID. Terminate the staging process after capturing evidence. It never receives production OAuth files.

## Client instances

Create distinct A, B and Automatic profiles/keys for each machine. A/B policies use the enrolled credential's durable `account_ref`; the Codex rule stays Automatic. Profile association requires WebSocket authentication. Retain existing legacy keys so Codex and rollback continue to work.

Copy `claude-profile.py` to an operator-controlled launcher directory. Each T3 provider wrapper supplies a fixed profile, actual Claude binary, dedicated client root and isolated Claude config directory:

```sh
exec python3 <launcher-directory>/claude-profile.py \
  a <claude-binary> <client-root> <claude-a-config-directory> "$@"
```

Use `a`, `b` and `automatic` key subdirectories, each containing an owner-only `client-key`. Each launcher overrides inherited base URL/auth/API-key and clears native OAuth/Bedrock/Vertex/Foundry credentials. This ensures helpers inheriting their provider use the same client identity. Test precedence with synthetic conflicting credentials before installation. Keep tokens out of T3 labels, launch arguments, source and captures.

Preserve the existing `claude-cliproxyapi` provider ID for Automatic. Add `claude-cliproxyapi-a` and `claude-cliproxyapi-b`, using the `claudeAgent` driver and names `Claude · A`, `Claude · B`, `Claude · Automatic`. Keep standard model IDs. Give A/B separate homes; preserve the existing Automatic home. Native providers and Codex remain untouched. Both Macs must use independent profile/key records.

The installed T3 release watches `settings.json` and emits validated changes when it changes. Back up settings privately, merge only these three provider entries into the latest file, write atomically, and confirm the live catalog contains the choices. Do not overwrite unrelated settings or restart T3. Require a new thread when changing instances; cross-instance continuation is unsupported.

## Production operation

Keep the actual host paths, credentials and commands in a private operator runbook outside Git. Before changing the gateway, save its config, auth inventory, binary identity, panel, service wrapper/LaunchAgent and both T3 configurations privately. Record how to restore the exact old files and key associations. Preserve the tunnel configuration and existing legacy keys.

Provision the pinned panel explicitly and set `management.disable-auto-update-panel: true` plus the fork panel repository. Disabling updates alone does not install a particular panel. Pin `MANAGEMENT_STATIC_PATH` to the provisioned release directory and verify the served digest.

Stage the release first. Install the reviewed binary/panel by switching the existing service wrapper, restart only the proxy LaunchAgent, and verify its version, capabilities and old client keys. Enroll both Claude records through the v8 management API and create six client profiles plus six distinct key associations. Enable B only after isolated strict routing is proven. Creating a strict B profile requires B to be available; complete that change and association together, then verify A still works.

Run this acceptance matrix from each Mac:

| Scenario | Required result |
| --- | --- |
| New A/B requests | Requested durable account reference matches the serving credential |
| Main and helper model | Same selected subscription |
| Continued same-instance session | Same client policy, no cross-client binding rewrite |
| Stream cancellation | Upstream cancellation completes without another-account fallback |
| Unavailable target | Strict error, no other-account attempt |
| Automatic and old key | Eligible shared routing remains available |
| Closed loopback connection | Launcher fails closed without native credential fallback |
| MacBook tunnel | Same gateway/build/panel, distinct client identities |

Use a disposable tunnel endpoint to test connection failure, rather than interrupting the existing production tunnel. Captures may include release hashes, profile/account UUIDs, operation/status/account-match flags and byte counts. Exclude OAuth material, raw keys and prompt text. If vendor availability prevents a proof, record the exact limitation and keep the item open.

## Updates

Fetch upstream deliberately into a separate upgrade branch. Compare the patchset, resolve changes in policy/credential/management contracts, run both projects' verification and the recording fixtures, build a new immutable release, then repeat staging, both-client acceptance and rollback. Never let the production panel updater or a floating upstream tag replace the pinned artifacts. Global load-balancing changes affect Automatic clients on both Macs; Only policies remain strict.
