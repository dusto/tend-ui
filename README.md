# tend-ui

The standalone **rich session surface** for [TEND](https://github.com/dusto/tend)
— a `webview` shell that attaches to the `tendd` daemon over its socket as a peer
client and renders richer content than a terminal can: a live session timeline,
tool calls, diffs, artifacts, markdown, and sandboxed previews.

tend-ui is not a second editor and it is **not served by `tendd`**. The daemon
stays a pure protocol/runtime daemon; tend-ui runs its own in-process loopback
HTTP server that adapts daemon JSON-RPC to HTML. See ADR 0005 in the tend repo.

> Status: early — this is the shell (loopback server + embedded frontend). The
> daemon client, the SSE event stream, and the session surfaces come next.

## Stack

- **webview_go** shell (WebKitGTK) — the window.
- An in-process **loopback HTTP server** (127.0.0.1, random port, per-run token
  in the URL path) — the only thing the webview talks to.
- **htmx** + **SSE** for the wire, **Alpine.js** for local UI state, **templ**
  for type-safe Go HTML — all vendored/embedded, no JS build step.
- The shared **`github.com/dusto/tend/client`** package for the daemon socket.

## Requirements

- **Go 1.26+**, cgo enabled, a C toolchain (`gcc`).
- **GTK 3** and **WebKitGTK 4.1** dev packages (Fedora:
  `gtk3-devel webkit2gtk4.1-devel`). The vendored `webview_go`
  (`third_party/webview_go`) carries the one-line `webkit2gtk-4.0 → 4.1`
  pkg-config change via a `go.mod` replace.
- [`templ`](https://templ.guide) only to regenerate templates
  (`go install github.com/a-h/templ/cmd/templ@latest`); the generated
  `*_templ.go` are committed, so a plain build does not need it.

## Build & run

```sh
make build      # go build -o tend-ui .
make run        # build + launch the window
make test       # go test ./...
make js-test    # JS unit tests (Node built-in runner)
make lint       # golangci-lint
make generate   # regenerate templ + tidy
make check      # the full pre-PR gate (mirrors CI)
```

Standards, tech-choice rationale, and the pre-PR checklist live in
[AGENTS.md](AGENTS.md).

`TENDUI_DEBUG=1 ./tend-ui` opens the WebKitGTK inspector for development.

## License

MIT — see [LICENSE](LICENSE).
