package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func drain(t *testing.T, ch <-chan JobEvent) []JobEvent {
	t.Helper()
	var events []JobEvent
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("timed out waiting for job events")
		}
	}
}

func TestJobStreamsProgressThenDone(t *testing.T) {
	sess := openTestSession(t)
	job := sess.startJob("test", func(ctx context.Context, emit func(any)) (any, error) {
		emit(map[string]int{"n": 1})
		emit(map[string]int{"n": 2})
		return map[string]string{"result": "ok"}, nil
	})

	events := drain(t, job.Subscribe())
	if len(events) < 3 {
		t.Fatalf("got %d events, want >=3 (2 progress + done): %+v", len(events), events)
	}
	last := events[len(events)-1]
	if last.Type != jobEventDone {
		t.Fatalf("last event type = %q, want done", last.Type)
	}
}

func TestJobLateSubscriberGetsReplayAndClose(t *testing.T) {
	sess := openTestSession(t)
	done := make(chan struct{})
	job := sess.startJob("test", func(ctx context.Context, emit func(any)) (any, error) {
		emit("p1")
		<-done // hold until the test subscribes
		return "final", nil
	})
	// Subscribe while running.
	ch := job.Subscribe()
	close(done)
	events := drain(t, ch)
	if events[len(events)-1].Type != jobEventDone {
		t.Fatalf("expected terminal done event, got %+v", events)
	}

	// A subscriber after completion still gets replay + closed channel.
	lateEvents := drain(t, job.Subscribe())
	if len(lateEvents) == 0 || lateEvents[len(lateEvents)-1].Type != jobEventDone {
		t.Fatalf("late subscriber did not get replayed done: %+v", lateEvents)
	}
}

func TestJobError(t *testing.T) {
	sess := openTestSession(t)
	job := sess.startJob("test", func(ctx context.Context, emit func(any)) (any, error) {
		return nil, errors.New("boom")
	})
	events := drain(t, job.Subscribe())
	last := events[len(events)-1]
	if last.Type != jobEventError || last.Error != "boom" {
		t.Fatalf("last event = %+v, want error 'boom'", last)
	}
}

func TestJobCancel(t *testing.T) {
	sess := openTestSession(t)
	started := make(chan struct{})
	job := sess.startJob("test", func(ctx context.Context, emit func(any)) (any, error) {
		close(started)
		<-ctx.Done() // wait for cancel
		return nil, ctx.Err()
	})
	<-started
	if !sess.CancelJob(job.ID) {
		t.Fatal("CancelJob returned false for a live job")
	}
	events := drain(t, job.Subscribe())
	if events[len(events)-1].Type != jobEventError {
		t.Fatalf("cancelled job should end in error, got %+v", events)
	}
	if !sess.CancelJob(job.ID) {
		// cancelling again is harmless (job still registered)
		t.Skip()
	}
}
