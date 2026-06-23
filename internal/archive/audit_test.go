package archive

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAndReadAuditRecords(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	record := AuditRecord{
		RequestID:    "req-001",
		TokenID:      "tok-abc",
		RemoteAddr:   "192.0.2.1:54321",
		Operation:    "GET /dicomweb/studies",
		UIDScope:     "1.2.3.study",
		Status:       200,
		Bytes:        4096,
		DurationMS:   125,
		ErrorSummary: "",
		OccurredAt:   now,
	}

	if err := catalog.WriteAuditRecord(ctx, record); err != nil {
		t.Fatalf("WriteAuditRecord failed: %v", err)
	}

	records, err := catalog.AuditRecords(ctx, 10)
	if err != nil {
		t.Fatalf("AuditRecords failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("AuditRecords count = %d, want 1", len(records))
	}

	got := records[0]
	if got.ID == 0 {
		t.Fatal("AuditRecord ID should be non-zero after insert")
	}
	if got.RequestID != record.RequestID {
		t.Fatalf("RequestID = %q, want %q", got.RequestID, record.RequestID)
	}
	if got.TokenID != record.TokenID {
		t.Fatalf("TokenID = %q, want %q", got.TokenID, record.TokenID)
	}
	if got.RemoteAddr != record.RemoteAddr {
		t.Fatalf("RemoteAddr = %q, want %q", got.RemoteAddr, record.RemoteAddr)
	}
	if got.Operation != record.Operation {
		t.Fatalf("Operation = %q, want %q", got.Operation, record.Operation)
	}
	if got.UIDScope != record.UIDScope {
		t.Fatalf("UIDScope = %q, want %q", got.UIDScope, record.UIDScope)
	}
	if got.Status != record.Status {
		t.Fatalf("Status = %d, want %d", got.Status, record.Status)
	}
	if got.Bytes != record.Bytes {
		t.Fatalf("Bytes = %d, want %d", got.Bytes, record.Bytes)
	}
	if got.DurationMS != record.DurationMS {
		t.Fatalf("DurationMS = %d, want %d", got.DurationMS, record.DurationMS)
	}
	if got.ErrorSummary != record.ErrorSummary {
		t.Fatalf("ErrorSummary = %q, want %q", got.ErrorSummary, record.ErrorSummary)
	}
	if got.OccurredAt.IsZero() {
		t.Fatal("OccurredAt should not be zero")
	}
}

func TestWriteAuditRecordStoresErrorSummary(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	record := AuditRecord{
		RequestID:    "req-err",
		TokenID:      "tok-err",
		RemoteAddr:   "192.0.2.2:1234",
		Operation:    "GET /dicomweb/studies/1.2.3",
		Status:       500,
		Bytes:        0,
		DurationMS:   10,
		ErrorSummary: "database query failed",
		OccurredAt:   time.Now().UTC(),
	}

	if err := catalog.WriteAuditRecord(ctx, record); err != nil {
		t.Fatalf("WriteAuditRecord failed: %v", err)
	}

	records, err := catalog.AuditRecords(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records count = %d, want 1", len(records))
	}
	if records[0].ErrorSummary != "database query failed" {
		t.Fatalf("ErrorSummary = %q, want %q", records[0].ErrorSummary, "database query failed")
	}
	if records[0].Status != 500 {
		t.Fatalf("Status = %d, want 500", records[0].Status)
	}
}

func TestAuditRecordsDefaultsLimitWhenZeroOrNegative(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	// Insert 5 records.
	for i := 0; i < 5; i++ {
		record := AuditRecord{
			RequestID:  "req-limit",
			TokenID:    "tok-limit",
			RemoteAddr: "127.0.0.1:1111",
			Operation:  "GET /dicomweb/studies",
			Status:     200,
			OccurredAt: time.Now().UTC(),
		}
		if err := catalog.WriteAuditRecord(ctx, record); err != nil {
			t.Fatalf("WriteAuditRecord failed: %v", err)
		}
	}

	// limit=0 should default to 100, returning all 5 records.
	records, err := catalog.AuditRecords(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 {
		t.Fatalf("AuditRecords(limit=0) count = %d, want 5", len(records))
	}

	// limit=2 should return only 2.
	records, err = catalog.AuditRecords(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("AuditRecords(limit=2) count = %d, want 2", len(records))
	}
}

func TestWriteAuditRecordAppliesCurrentTimeWhenOccurredAtIsZero(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	before := time.Now().UTC().Add(-time.Second)
	record := AuditRecord{
		RequestID:  "req-zero-time",
		TokenID:    "tok-zt",
		RemoteAddr: "127.0.0.1:9999",
		Operation:  "POST /dicomweb/studies",
		Status:     200,
		// OccurredAt is zero — WriteAuditRecord should substitute current time.
	}

	if err := catalog.WriteAuditRecord(ctx, record); err != nil {
		t.Fatalf("WriteAuditRecord failed: %v", err)
	}

	records, err := catalog.AuditRecords(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records count = %d, want 1", len(records))
	}
	after := time.Now().UTC().Add(time.Second)
	got := records[0].OccurredAt
	if got.Before(before) || got.After(after) {
		t.Fatalf("OccurredAt = %v, want between %v and %v", got, before, after)
	}
}

func TestAuditRecordsOrdersByInsertionOrder(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	operations := []string{"GET /dicomweb/studies", "POST /dicomweb/studies", "GET /dicomweb/studies/1.2.3"}
	for _, op := range operations {
		if err := catalog.WriteAuditRecord(ctx, AuditRecord{
			RequestID:  "req-order",
			TokenID:    "tok-order",
			RemoteAddr: "10.0.0.1:80",
			Operation:  op,
			Status:     200,
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("WriteAuditRecord failed: %v", err)
		}
	}

	records, err := catalog.AuditRecords(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records count = %d, want 3", len(records))
	}
	for i, op := range operations {
		if records[i].Operation != op {
			t.Fatalf("records[%d].Operation = %q, want %q", i, records[i].Operation, op)
		}
	}
}

func TestAuditRecordsRejectsMalformedOccurredAt(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	_, err = catalog.db.ExecContext(ctx, `
	INSERT INTO audit_log(
	  request_id, token_id, remote_addr, operation, uid_scope,
	  status, bytes, duration_ms, error_summary, occurred_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"req-bad-time", "tok", "127.0.0.1:1", "GET /dicomweb/studies", "", 200, 0, 0, "", "not-a-time")
	if err != nil {
		t.Fatal(err)
	}

	_, err = catalog.AuditRecords(ctx, 10)
	if err == nil || !strings.Contains(err.Error(), "parse audit occurred_at") {
		t.Fatalf("AuditRecords error = %v, want parse audit occurred_at", err)
	}
}
