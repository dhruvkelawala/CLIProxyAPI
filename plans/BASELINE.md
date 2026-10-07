# Setup verification — 2026-10-07

These checks cover the pinned upstream baseline and planning/setup artifacts. No account-routing feature has been implemented yet.

| Check | Result |
|---|---|
| CLIProxyAPI baseline `go test ./...` | Passed |
| CLIProxyAPI `go build -o /tmp/cliproxyapi-fork-setup-20261007 ./cmd/server` | Passed |
| Dashboard `bunx bun@1.3.14 install --frozen-lockfile` | Passed; tracked lockfile unchanged |
| Dashboard `bunx bun@1.3.14 run verify` | Passed: 1,493 tests, lint, TypeScript/build |
| Dashboard artifact | Single-file dist/index.html built |

Host toolchain selected by the Go launcher: go1.27.1 (go.mod minimum 1.26). Panel checks ran with Bun 1.3.14 and Node 24.20.0. The initial dashboard tests emitted an existing react-i18next initialization warning, but no tests failed.

The review-ready contract's production-code gate does not apply to this handoff: additions are plans, an illustrative concept and repository/project metadata. Runtime source, tests and workflows are unchanged. Future implementation tickets must run the full applicable gate.

Production service, credential states, tunnel, T3 source and client credentials were not changed. No dev server was started. Build artifacts/dependencies stay in ignored directories or temporary/cache locations.
