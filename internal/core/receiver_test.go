package core

import (
	"context"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
)

func TestReceiverStatusWhenStopped(t *testing.T) {
	sess := openTestSession(t)
	st := sess.ReceiverStatus()
	if st.Running {
		t.Fatal("fresh session reports receiver running")
	}
	if st.AETitle == "" {
		t.Error("stopped status should report an effective AE title")
	}
}

func TestStartStopReceiverLifecycle(t *testing.T) {
	sess := openTestSession(t)

	cfg := appconfig.Defaults()
	cfg.LocalAETitle = "GOPACSRX"
	cfg.ReceiverAddress = "127.0.0.1:0" // ephemeral port
	if err := sess.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	st, err := sess.StartReceiver(context.Background())
	if err != nil {
		t.Fatalf("StartReceiver: %v", err)
	}
	if !st.Running {
		t.Fatal("status after start is not running")
	}
	if st.Address == "" || st.AETitle != "GOPACSRX" {
		t.Fatalf("unexpected running status: %+v", st)
	}

	// Starting again is rejected.
	if _, err := sess.StartReceiver(context.Background()); err != ErrReceiverRunning {
		t.Fatalf("double start error = %v, want ErrReceiverRunning", err)
	}

	st, err = sess.StopReceiver(context.Background())
	if err != nil {
		t.Fatalf("StopReceiver: %v", err)
	}
	if st.Running {
		t.Fatal("status after stop still running")
	}

	// A receiver run summary should be recorded in history.
	history, err := sess.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history has %d entries, want 1 receiver summary", len(history))
	}

	// Stopping when not running is rejected.
	if _, err := sess.StopReceiver(context.Background()); err != ErrReceiverNotRunning {
		t.Fatalf("stop-when-stopped error = %v, want ErrReceiverNotRunning", err)
	}
}
