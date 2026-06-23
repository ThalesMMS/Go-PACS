package core

import (
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
)

func TestAutoQuerySchedulerLifecycle(t *testing.T) {
	sess := openTestSession(t)
	if sess.AutoQueryStatus().Running {
		t.Fatal("scheduler should start stopped")
	}
	// No query-enabled nodes → cycles are no-ops, safe to start/stop.
	st, err := sess.StartAutoQuery(autoquery.Profile{Name: "P"}, 30)
	if err != nil {
		t.Fatalf("StartAutoQuery: %v", err)
	}
	if !st.Running || st.ProfileName != "P" {
		t.Fatalf("unexpected status: %+v", st)
	}
	if _, err := sess.StartAutoQuery(autoquery.Profile{Name: "Q"}, 30); err != ErrAutoQueryRunning {
		t.Fatalf("double start err = %v, want ErrAutoQueryRunning", err)
	}
	if _, err := sess.StopAutoQuery(); err != nil {
		t.Fatalf("StopAutoQuery: %v", err)
	}
	if sess.AutoQueryStatus().Running {
		t.Fatal("scheduler should be stopped")
	}
	if _, err := sess.StopAutoQuery(); err != ErrAutoQueryNotRunning {
		t.Fatalf("stop-when-stopped err = %v, want ErrAutoQueryNotRunning", err)
	}
}
