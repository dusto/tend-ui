// Package web holds tend-ui's embedded frontend assets (htmx, Alpine, CSS,
// fonts) and its templ templates. The loopback UI server serves these under the
// per-run token path; nothing is fetched from the network at runtime.
package web

import "embed"

// Assets is the static asset tree, served under "<token>/assets/".
//
//go:embed assets
var Assets embed.FS
