package rebuildmode

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/core"
)

type Options struct {
	ArchiveDir string
	Verbose    bool
}

func Run(ctx context.Context, opts Options, out io.Writer) error {
	opts.ArchiveDir = strings.TrimSpace(opts.ArchiveDir)
	if opts.ArchiveDir == "" {
		return fmt.Errorf("archive directory is required")
	}
	info, err := os.Stat(opts.ArchiveDir)
	if err != nil {
		return fmt.Errorf("stat archive directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("archive path is not a directory: %s", opts.ArchiveDir)
	}

	report, err := core.RebuildArchiveCatalog(ctx, opts.ArchiveDir)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(out, "Warning: %s\n", warning)
	}
	if report.CatalogBackupPath != "" {
		fmt.Fprintf(out, "Moved existing catalog to %s\n", report.CatalogBackupPath)
	}
	fmt.Fprintf(out, "Catalog rebuild complete for %s\n", opts.ArchiveDir)
	fmt.Fprintf(out, "Scanned: %d\n", report.ScannedFiles)
	fmt.Fprintf(out, "Stored: %d\n", report.StoredFiles)
	fmt.Fprintf(out, "Skipped: %d\n", report.SkippedFiles)
	fmt.Fprintf(out, "Failed: %d\n", report.FailedFiles)
	fmt.Fprintf(out, "Verification passed: %t\n", report.VerificationPassed)
	if len(report.Rejections) > 0 {
		fmt.Fprintf(out, "Rejections: %d\n", len(report.Rejections))
		if opts.Verbose {
			for _, rejection := range report.Rejections {
				fmt.Fprintf(out, "- %s: %s\n", rejection.Path, rejection.Reason)
			}
		} else {
			fmt.Fprintln(out, "Use -verbose to show rejection details.")
		}
	}
	return nil
}
