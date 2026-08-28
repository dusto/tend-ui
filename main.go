// Command tend-ui is the standalone rich session surface for TEND: a webview
// shell that attaches to the tend daemon over its socket as a peer client. It
// serves its own UI from an in-process loopback server and renders richer
// content than a terminal can (see the tend repo's ADR 0005). This is the
// shell; the daemon client and session surfaces land in later work.
package main

import (
	"log"
	"os"
	"runtime"

	webview "github.com/webview/webview_go"

	"github.com/dusto/tend-ui/internal/ui"
)

func init() {
	// GTK/WebKit must be driven from the OS main thread. Without this the main
	// goroutine can migrate threads and the process crashes on window close with
	// heap corruption (verified in the webview spike, tend-du1.11).
	runtime.LockOSThread()
}

func main() {
	srv, err := ui.NewServer()
	if err != nil {
		log.Fatalf("tend-ui: %v", err)
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
}
