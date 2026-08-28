// Command tend-ui is the standalone rich session surface for TEND: a webview
// shell that attaches to the tend daemon over its socket as a peer client. It
// serves its own UI from an in-process loopback server and renders richer
// content than a terminal can (see the tend repo's ADR 0005). This is the
// shell; the daemon client and session surfaces land in later work.
package main

import (
	"context"
	"log"
	"os"
	"runtime"

	webview "github.com/webview/webview_go"

	"github.com/dusto/tend-ui/internal/bridge"
	"github.com/dusto/tend-ui/internal/ui"
)

func init() {
	// GTK/WebKit must be driven from the OS main thread. Without this the main
	// goroutine can migrate threads and the process crashes on window close with
	// heap corruption (verified in the webview spike, tend-du1.11).
	runtime.LockOSThread()
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("tend-ui: %v", err)
	}
}

// run wires the bridge, the loopback server, and the webview, and blocks in the
// webview loop until the window closes. Split from main so its defers (cancel,
// server close, webview destroy) run — a log.Fatalf in main would skip them.
func run() error {
	// Follow the daemon's workspace stream for the launch directory and fan its
	// events out to the UI's SSE endpoint. The bridge reconnects on its own, so
	// tend-ui opens whether or not the daemon is up yet.
	hub := bridge.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if dir, err := os.Getwd(); err == nil {
		go bridge.New(dir, hub).Run(ctx)
	} else {
		log.Printf("tend-ui: no working directory, not following a workspace: %v", err)
	}

	srv, err := ui.NewServer(hub)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	// Inspector off by default (keeps WebKit teardown clean and quiet);
	// TENDUI_DEBUG=1 opens the WebKitGTK inspector for development.
	w := webview.New(os.Getenv("TENDUI_DEBUG") != "")
	defer w.Destroy()
	w.SetTitle("tend-ui")
	w.SetSize(1100, 760, webview.HintNone)
	w.Navigate(srv.Base())
	w.Run()
	return nil
}
