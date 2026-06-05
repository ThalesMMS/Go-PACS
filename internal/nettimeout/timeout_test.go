package nettimeout

import (
	"context"
	"testing"
	"time"
)

func TestWithDefaultTimeoutUsesDefaultWhenContextHasNoDeadline(t *testing.T) {
	ctx, cancel := WithDefault(context.Background(), 40*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 39*time.Second || remaining > 40*time.Second {
		t.Fatalf("deadline remaining = %s, want about 40s", remaining)
	}
}

func TestWithDefaultTimeoutRespectsLongerCallerDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer parentCancel()

	ctx, cancel := WithDefault(parent, 40*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 89*time.Second || remaining > 90*time.Second {
		t.Fatalf("deadline remaining = %s, want parent 90s deadline", remaining)
	}
}

func TestWithDefaultTimeoutKeepsSoonerCallerDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer parentCancel()

	ctx, cancel := WithDefault(parent, 40*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 4*time.Second || remaining > 5*time.Second {
		t.Fatalf("deadline remaining = %s, want parent 5s deadline", remaining)
	}
}
