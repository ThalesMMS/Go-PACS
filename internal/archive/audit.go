package archive

import (
	"context"
	"fmt"
	"time"
)

type AuditRecord struct {
	ID           int64
	RequestID    string
	TokenID      string
	RemoteAddr   string
	Operation    string
	UIDScope     string
	Status       int
	Bytes        int64
	DurationMS   int64
	ErrorSummary string
	OccurredAt   time.Time
}

func (c *Catalog) WriteAuditRecord(ctx context.Context, record AuditRecord) error {
	occurredAt := record.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if _, err := c.db.ExecContext(ctx, `
INSERT INTO audit_log(
  request_id, token_id, remote_addr, operation, uid_scope,
  status, bytes, duration_ms, error_summary, occurred_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RequestID,
		record.TokenID,
		record.RemoteAddr,
		record.Operation,
		record.UIDScope,
		record.Status,
		record.Bytes,
		record.DurationMS,
		record.ErrorSummary,
		occurredAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("write audit record: %w", err)
	}
	return nil
}

func (c *Catalog) AuditRecords(ctx context.Context, limit int) ([]AuditRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.db.QueryContext(ctx, `
SELECT id, request_id, token_id, remote_addr, operation, COALESCE(uid_scope, ''),
       status, bytes, duration_ms, COALESCE(error_summary, ''), occurred_at
FROM audit_log
ORDER BY id ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit records: %w", err)
	}
	defer rows.Close()

	var records []AuditRecord
	for rows.Next() {
		var record AuditRecord
		var occurredAt string
		if err := rows.Scan(
			&record.ID,
			&record.RequestID,
			&record.TokenID,
			&record.RemoteAddr,
			&record.Operation,
			&record.UIDScope,
			&record.Status,
			&record.Bytes,
			&record.DurationMS,
			&record.ErrorSummary,
			&occurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit record: %w", err)
		}
		record.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("parse audit occurred_at %q: %w", occurredAt, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit records: %w", err)
	}
	return records, nil
}
