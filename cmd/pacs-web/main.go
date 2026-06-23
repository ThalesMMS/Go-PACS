// Command pacs-web serves the go-pacs web frontend and opens it in a native window.
//
// It mirrors the TaskForge frontend scheme: a local net/http server (internal/web)
// exposes the shared backend (internal/core) over JSON and serves an embedded
// web UI, which this launcher opens in a native webview window when built with
// CGO (the default for packaged macOS builds). Without CGO it falls back to the
// system browser.
// It is a second frontend option alongside the Fyne desktop app (cmd/pacs-gui),
// both built on the same core.Session.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/ThalesMMS/Go-PACS/internal/core"
	"github.com/ThalesMMS/Go-PACS/internal/web"
)

func main() {
	archiveDir := flag.String("archive-dir", core.DefaultArchiveDir(), "directory for the local archive catalog and object store")
	addr := flag.String("addr", "127.0.0.1:0", "listen address (host:port; port 0 picks a free port)")
	noWindow := flag.Bool("no-window", false, "serve only; do not open a window or browser")
	flag.Parse()

	session, err := core.Open(*archiveDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer session.Close()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	url := "http://" + ln.Addr().String()
	log.Printf("go-pacs web frontend listening on %s", url)

	handler := web.NewServer(session).Handler()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- http.Serve(ln, handler)
	}()

	if !*noWindow {
		runUI(url, serverDone)
		if err := ln.Close(); err != nil {
			log.Printf("close listener: %v", err)
		}
	}

	if err := <-serverDone; err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
