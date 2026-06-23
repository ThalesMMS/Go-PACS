package core

import (
	"context"
	"fmt"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
)

type StoragePolicy struct {
	TrashAutoPurgeDays int `json:"trashAutoPurgeDays"`
}

type StorageStatus struct {
	Policy StoragePolicy        `json:"policy"`
	Stats  archive.ArchiveStats `json:"stats"`
	Trash  []archive.TrashEntry `json:"trash"`
}

func (s *Session) StoragePolicy() (StoragePolicy, error) {
	cfg, err := s.LoadConfig()
	if err != nil {
		return StoragePolicy{}, err
	}
	return StoragePolicy{TrashAutoPurgeDays: cfg.TrashAutoPurgeDays}, nil
}

func (s *Session) SaveStoragePolicy(policy StoragePolicy) (StoragePolicy, error) {
	if policy.TrashAutoPurgeDays < 0 {
		return StoragePolicy{}, fmt.Errorf("trashAutoPurgeDays must be zero or greater")
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return StoragePolicy{}, err
	}
	cfg.TrashAutoPurgeDays = policy.TrashAutoPurgeDays
	if err := s.SaveConfig(cfg); err != nil {
		return StoragePolicy{}, err
	}
	return policy, nil
}

func (s *Session) StorageStatus(ctx context.Context) (StorageStatus, error) {
	policy, err := s.StoragePolicy()
	if err != nil {
		return StorageStatus{}, err
	}
	stats, err := s.catalog.ArchiveStats(ctx)
	if err != nil {
		return StorageStatus{}, err
	}
	trash, err := s.catalog.ListTrash(ctx)
	if err != nil {
		return StorageStatus{}, err
	}
	return StorageStatus{Policy: policy, Stats: stats, Trash: trash}, nil
}

func (s *Session) PurgeExpiredTrash(ctx context.Context) (archive.PurgeExpiredTrashReport, error) {
	policy, err := s.StoragePolicy()
	if err != nil {
		return archive.PurgeExpiredTrashReport{}, err
	}
	if policy.TrashAutoPurgeDays == 0 {
		return archive.PurgeExpiredTrashReport{}, nil
	}
	start := time.Now()
	days := int64(policy.TrashAutoPurgeDays)
	const nanosPerDay = int64(24 * time.Hour)
	if days > (1<<63-1)/nanosPerDay {
		return archive.PurgeExpiredTrashReport{}, fmt.Errorf("trash auto purge days overflows duration")
	}
	cutoff := start.UTC().Add(-time.Duration(days * nanosPerDay))
	report, err := s.catalog.PurgeExpiredTrash(ctx, cutoff)
	if err != nil {
		return report, err
	}
	s.recordStoragePolicySummary(report, time.Since(start))
	return report, nil
}

func (s *Session) recordStoragePolicySummary(report archive.PurgeExpiredTrashReport, duration time.Duration) {
	if report.Scanned == 0 && report.Purged == 0 && len(report.Errors) == 0 {
		return
	}
	history, err := s.LoadHistory()
	if err != nil {
		return
	}
	_ = s.SaveHistory(ops.Prepend(history, ops.StoragePolicySummary(report, duration)))
}
