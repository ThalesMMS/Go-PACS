package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ThalesMMS/Go-PACS/internal/receivermode"
)

func main() {
	archiveDir := flag.String("archive-dir", defaultArchiveDir(), "directory for the local archive catalog and object store")
	address := flag.String("address", "", "override receiver listen address")
	aeTitle := flag.String("ae", "", "override local receiver AE title")
	noAllowlist := flag.Bool("no-allowlist", false, "accept inbound associations from any Calling AE and remote IP")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := receivermode.Run(ctx, receivermode.Options{
		ArchiveDir:      *archiveDir,
		AddressOverride: *address,
		AETitleOverride: *aeTitle,
		NoAllowlist:     *noAllowlist,
	}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultArchiveDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".", ".Go-PACS")
	}
	return filepath.Join(dir, "Go-PACS")
}
