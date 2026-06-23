package core

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// JobEvent is a single event in a long-running operation's lifecycle, streamed
// to subscribers (e.g. over Server-Sent Events). Type is "progress", "done", or
// "error". Data carries the domain progress value for "progress" and the final
// outcome for "done".
type JobEvent struct {
	Type  string `json:"type"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

const (
	jobEventProgress = "progress"
	jobEventDone     = "done"
	jobEventError    = "error"
	jobBufferCap     = 512 // replay/backlog cap per job
)

// Job is a running (or finished) asynchronous operation.
type Job struct {
	ID   string
	Kind string

	cancel   context.CancelFunc
	mu       sync.Mutex
	buf      []JobEvent
	subs     map[chan JobEvent]struct{}
	finished bool
}

// emit appends an event and fans it out to current subscribers. Terminal events
// (done/error) mark the job finished and close subscriber channels. Sends happen
// outside the lock so a slow subscriber cannot stall the operation goroutine or
// other subscribers.
func (j *Job) emit(ev JobEvent) {
	j.mu.Lock()
	j.buf = append(j.buf, ev)
	if len(j.buf) > jobBufferCap {
		// Keep the most recent events for late subscribers.
		j.buf = j.buf[len(j.buf)-jobBufferCap:]
	}
	terminal := ev.Type != jobEventProgress
	if terminal {
		j.finished = true
	}
	subs := make([]chan JobEvent, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	if terminal {
		j.subs = map[chan JobEvent]struct{}{}
	}
	j.mu.Unlock()

	for _, ch := range subs {
		ch <- ev
		if terminal {
			close(ch)
		}
	}
}

// Subscribe returns a channel that first replays buffered events, then receives
// live events until the job finishes (at which point the channel is closed). A
// subscriber to an already-finished job receives the buffered events and a
// closed channel.
func (j *Job) Subscribe() <-chan JobEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch := make(chan JobEvent, jobBufferCap+8)
	for _, ev := range j.buf {
		ch <- ev
	}
	if j.finished {
		close(ch)
		return ch
	}
	j.subs[ch] = struct{}{}
	return ch
}

// JobManager owns the process-scoped registry of asynchronous operations. It is
// embedded in Session (see jobs field) and shared by every frontend client.
type jobManager struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func newJobManager() *jobManager {
	return &jobManager{jobs: map[string]*Job{}}
}

// start registers a new job and runs fn in a goroutine. fn receives a cancelable
// context and an emit function for progress; its (result, error) become the
// job's terminal event.
func (s *Session) startJob(kind string, fn func(ctx context.Context, emit func(any)) (any, error)) *Job {
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		ID:     uuid.NewString(),
		Kind:   kind,
		cancel: cancel,
		subs:   map[chan JobEvent]struct{}{},
	}
	s.jobs.mu.Lock()
	s.jobs.jobs[job.ID] = job
	s.jobs.mu.Unlock()

	go func() {
		defer cancel()
		result, err := fn(ctx, func(p any) { job.emit(JobEvent{Type: jobEventProgress, Data: p}) })
		if err != nil {
			job.emit(JobEvent{Type: jobEventError, Data: result, Error: err.Error()})
			return
		}
		job.emit(JobEvent{Type: jobEventDone, Data: result})
	}()
	return job
}

// Job returns the job with the given id, if any.
func (s *Session) Job(id string) (*Job, bool) {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	job, ok := s.jobs.jobs[id]
	return job, ok
}

// CancelJob cancels the job's context, requesting the operation to stop.
func (s *Session) CancelJob(id string) bool {
	job, ok := s.Job(id)
	if !ok {
		return false
	}
	job.cancel()
	return true
}
