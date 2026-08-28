# AGENTS.md

Guardrails and conventions for working in **tend-ui** — the standalone rich session
surface for [TEND](https://github.com/dusto/tend). Harness-agnostic: this file is the
source of truth so any agent or contributor knows the standards, tech choices, and
available guidance.

## Project

tend-ui is a **webview client of the tend daemon (`tendd`)** — a peer of Neovim and the
CLI over the same socket, not served by `tendd`. It renders richer content than a
terminal can: a live session timeline, tool calls, diffs, artifacts, markdown, and
sandboxed previews. It is **mostly Go**: a `webview_go` window + an in-process loopback
HTTP server that adapts daemon JSON-RPC to HTML. The architecture + stack decision is
recorded in **ADR 0005** in the tend repo (`docs/adr/0005-tend-ui-webview-client.md`).

## Dev commands

```sh
go build ./...                 # build (needs cgo + GTK3 + WebKitGTK 4.1 — see README)
go test ./...                  # Go tests (stdlib testing, table-driven)
go vet ./...                   # vet
gofmt -l . | grep -v '^third_party/'   # format check (must be empty; third_party is vendored)
golangci-lint run              # lint (config: .golangci.yml)
go generate ./...              # regenerate templ *_templ.go (run via go generate, NOT bare `templ generate`)
npm test                       # JS unit tests (Node built-in runner)
```

Toolchain is pinned in `.mise.toml` (Go, Node, templ). Prefer `make`: `build`, `run`,
`test`, `generate`, `vet`, `fmt`.

> **templ codegen:** always regenerate with `go generate ./...` (or `make generate`), which
> runs `templ generate` from the package directory. Running bare `templ generate` from the
> repo root produces different `FileName` paths in the output and will fail the CI drift
> check. The `*_templ.go` files are committed so a plain `go build` needs no templ CLI.

### Before pushing a PR (required)

Run **all** of these locally and ensure they pass — do not push until they are green. CI
enforces the same checks.

```sh
go build ./...                                 # must succeed
go test ./...                                  # all tests pass
go vet ./...                                   # no findings
gofmt -l . | grep -v '^third_party/'           # output must be empty
golangci-lint run                              # 0 issues
go generate ./... && git diff --exit-code web/ # no templ drift
npm test                                       # JS tests pass
```

## Tech choices (ADR 0005)

These are deliberate. Do not introduce the rejected alternatives without an explicit decision.

| Area | Choice | Not |
|---|---|---|
| Shell | `webview_go` (WebKitGTK) | Fyne / gogpu / Wails |
| UI transport | in-process **loopback** HTTP server inside tend-ui | HTTP served by `tendd` |
| Frontend | htmx + SSE (live stream) + Alpine.js (local state) + templ (Go HTML) | a JS/TS build step, SPA framework, Datastar |
| Daemon access | the shared `github.com/dusto/tend/client` package over the socket | a private wire surface |
| Go testing | stdlib `testing`, table-driven | testify |
| JS testing | Node's built-in test runner (`node --test`) | jest / vitest / a bundler |
| Logging | `log/slog` | zap / logrus / zerolog |
| DI | manual constructor injection | wire / dig / fx / samber-do |

### The `tend` dependency

`github.com/dusto/tend` (the `api` + `client` packages) is **pinned in `go.mod`** at a
pseudo-version, so CI and fresh clones resolve it from the public proxy — no local
checkout required. For local co-development against a sibling `../tend`, use a **gitignored
`go.work`** (`use .` + `use ../tend`); it overrides the pin locally and is absent in CI.
When tend-ui starts using a newer tend feature, re-pin: `GOWORK=off go get github.com/dusto/tend@main && GOWORK=off go mod tidy`. `third_party/webview_go` is a committed in-repo replace, so it needs no such handling.

## Frontend rules (load-bearing)

- **Main-thread affinity.** `runtime.LockOSThread()` must run before the webview; GTK/WebKit
  crash the process on close otherwise (heap corruption — verified in the webview spike).
- **`tendd` stays non-HTTP.** All HTTP lives in tend-ui's loopback server (127.0.0.1, random
  port, unguessable per-run token in the path). Never make the daemon serve UI assets.
- **Privileged shell vs sandboxed agent content.** Owned tend-ui HTML/JS may talk to the
  loopback server; agent-authored artifact/preview content runs sandboxed (separate origin,
  loopback-only, artifact-dir confinement) with **no** daemon socket or privileged API.
- **No JS build step.** htmx/Alpine are vendored and `go:embed`-ed; first-party JS is small,
  dependency-free ES modules under `web/assets/js/`, unit-tested in `web/assets/js/_tests/`.
- **Artifacts render inline in the session timeline**, where their tool call produced them —
  not as a separate browser.
- **Inspector off in release.** The WebKitGTK inspector is opt-in via `TENDUI_DEBUG=1`.

## Conventions

- **Commits & branches: conventional style.** Subjects `feat: …`, `fix: …`, `refactor: …`,
  `chore: …`, `docs: …`. Branch names `type/short-desc`. Never prefix with task keys.
- **Branch + PR per task.** Reference the bead in a `Refs: <id>` footer, not the subject.
- **Layout:** `main.go` (shell), `internal/ui/` (loopback server + session surfaces),
  `web/` (`assets/` embedded static + `templates/` templ), `third_party/` (vendored, via
  `go.mod` replace — do not hand-lint or reformat).

## Coding guidance (skill index)

The Go practice areas below are the standards for this repo. They are **vendored** under
`.claude/skills/` (from `samber/cc-skills-golang`, MIT, pinned — see
`.claude/skills/ATTRIBUTION.md`) so they work across harnesses. The expectations apply even
when a skill is not auto-loaded.

**Core (this stack)**

- `golang-project-layout`, `golang-naming`, `golang-code-style`, `golang-documentation`
- `golang-error-handling` — wrap with `%w`, sentinel errors, `errors.Is/As`, slog
- `golang-concurrency`, `golang-context` — the SSE fan-out and daemon stream goroutines
- `golang-structs-interfaces`, `golang-design-patterns` — accept interfaces, functional options, graceful shutdown
- `golang-security` — **the loopback server, per-run token, and the agent-content sandbox live or die here**; also injection, file/socket safety, secrets
- `golang-safety` — nil/panic/concurrent-map/numeric safety
- `golang-testing` — table-driven, fakes, httptest
- `golang-lint`, `golang-continuous-integration`, `golang-modernize`, `golang-dependency-management`

**Also in scope**

- `golang-observability`, `golang-samber-slog` — structured logging
- `golang-performance`, `golang-benchmark` — the event stream hot path
- `golang-troubleshooting` — debugging the webview + server (pprof, race, GODEBUG)

**Deliberately excluded:** cobra/viper, testify, gRPC, GraphQL, Swagger, DI frameworks, and
the other `samber-*` libraries (lo/mo/ro/hot/oops).

## License

MIT.
