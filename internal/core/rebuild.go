package core

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

// RebuildArchiveCatalog rebuilds an archive catalog and verifies the rebuild.
// It returns the rebuild report and any error encountered during the rebuild or verification process.
func RebuildArchiveCatalog(ctx context.Context, archiveDir string) (archive.RebuildReport, error) {
	return archive.RebuildCatalog(ctx, archiveDir, archive.RebuildOptions{
		VerifyFunc: func(ctx context.Context, catalog *archive.Catalog) error {
			sess := &Session{
				archiveDir:  archiveDir,
				catalog:     catalog,
				nodeStore:   nodes.NewStore(filepath.Join(archiveDir, nodesFileName)),
				autoQuery:   autoquery.NewStore(filepath.Join(archiveDir, autoQueryProfilesFileName)),
				configPath:  filepath.Join(archiveDir, configFileName),
				historyPath: filepath.Join(archiveDir, historyFileName),
			}
			result, err := sess.VerifyArchive(ctx)
			if err != nil {
				return err
			}
			if !result.OK {
				return fmt.Errorf("rebuilt archive verification failed with %d errors", len(result.Errors))
			}
			return nil
		},
	})
}
