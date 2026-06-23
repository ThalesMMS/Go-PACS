package core

import (
	"context"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
)

func TestPurgeExpiredTrashRejectsOverflowingAutoPurgeDays(t *testing.T) {
	sess := openTestSession(t)
	cfg := appconfig.Defaults()
	cfg.TrashAutoPurgeDays = int(^uint(0) >> 1)
	if err := sess.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	_, err := sess.PurgeExpiredTrash(context.Background())
	if err == nil || !strings.Contains(err.Error(), "trash auto purge days") {
		t.Fatalf("PurgeExpiredTrash error = %v, want trash auto purge days overflow", err)
	}
}
