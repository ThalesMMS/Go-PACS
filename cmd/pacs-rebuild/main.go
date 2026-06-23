package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThalesMMS/Go-PACS/internal/rebuildmode"
)

func main() {
	archiveDir := flag.String("archive-dir", "", "directory containing the local archive objects/ store")
	verbose := flag.Bool("verbose", false, "show per-file rejection details")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := rebuildmode.Run(ctx, rebuildmode.Options{
		ArchiveDir: *archiveDir,
		Verbose:    *verbose,
	}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
