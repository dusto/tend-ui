# third_party

Vendored third-party code, used via `replace` directives in the root `go.mod`.

## webview_go

A local copy of [`github.com/webview/webview_go`](https://github.com/webview/webview_go)
(MIT) with **one change**: the Linux cgo `pkg-config` line in `webview.go` is
`webkit2gtk-4.1` instead of the upstream `webkit2gtk-4.0`, so it builds against
the WebKitGTK 4.1 stack shipped on current Fedora-style distros. Everything else
is upstream. This is the "tiny fork/replace for the UI binary" from ADR 0005;
revisit if upstream gains 4.1 support (then drop the replace).
