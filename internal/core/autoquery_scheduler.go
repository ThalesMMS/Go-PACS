package core

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
)

// Errors for the auto-query scheduler lifecycle.
var (
	ErrAutoQueryRunning    = errors.New("auto-query is already running")
	ErrAutoQueryNotRunning = errors.New("auto-query is not running")
)

const minAutoQueryInterval = 30 * time.Second

// AutoQueryStatus describes the background auto-query scheduler for a frontend.
type AutoQueryStatus struct {
	Running         bool      `json:"running"`
	ProfileName     string    `json:"profileName,omitempty"`
	IntervalSeconds int       `json:"intervalSeconds,omitempty"`
	LastRun         time.Time `json:"lastRun,omitempty"`
	LastMatches     int       `json:"lastMatches"`
	LastRetrieved   int       `json:"lastRetrieved"`
	NextRun         time.Time `json:"nextRun,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
}

// AutoQueryStatus returns the current scheduler status.
func (s *Session) AutoQueryStatus() AutoQueryStatus {
	s.autoQueryMu.Lock()
	defer s.autoQueryMu.Unlock()
	return s.autoQueryStatus
}

// StartAutoQuery starts the background scheduler: it runs the profile's query
// immediately and then every intervalSeconds (clamped to a 30s minimum). When
// the profile enables auto-retrieve, up to MaxMatches matches are retrieved each
// cycle. Only one scheduler runs at a time. The loop uses a background context
// so it survives requests.
func (s *Session) StartAutoQuery(profile autoquery.Profile, intervalSeconds int) (AutoQueryStatus, error) {
	s.autoQueryMu.Lock()
	defer s.autoQueryMu.Unlock()
	if s.autoQueryCancel != nil {
		return s.autoQueryStatus, ErrAutoQueryRunning
	}
	interval := time.Duration(intervalSeconds) * time.Second
	if interval < minAutoQueryInterval {
		interval = minAutoQueryInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.autoQueryCancel = cancel
	s.autoQueryStatus = AutoQueryStatus{
		Running:         true,
		ProfileName:     profile.Name,
		IntervalSeconds: int(interval / time.Second),
		NextRun:         time.Now(),
	}
	go s.autoQueryLoop(ctx, profile, interval)
	return s.autoQueryStatus, nil
}

// StopAutoQuery stops the background scheduler.
func (s *Session) StopAutoQuery() (AutoQueryStatus, error) {
	s.autoQueryMu.Lock()
	defer s.autoQueryMu.Unlock()
	if s.autoQueryCancel == nil {
		return s.autoQueryStatus, ErrAutoQueryNotRunning
	}
	s.autoQueryCancel()
	s.autoQueryCancel = nil
	s.autoQueryStatus.Running = false
	s.autoQueryStatus.NextRun = time.Time{}
	return s.autoQueryStatus, nil
}

func (s *Session) autoQueryLoop(ctx context.Context, profile autoquery.Profile, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.runAutoQueryCycle(ctx, profile, interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAutoQueryCycle(ctx, profile, interval)
		}
	}
}

func (s *Session) runAutoQueryCycle(ctx context.Context, profile autoquery.Profile, interval time.Duration) {
	result, err := s.RunAutoQuery(ctx, profile, nil)

	retrieved := 0
	if err == nil && profile.Settings.AutoRetrieve {
		retrieved = s.autoRetrieveMatches(ctx, profile, result.Matches)
	}

	s.autoQueryMu.Lock()
	if s.autoQueryCancel != nil { // still running
		s.autoQueryStatus.LastRun = time.Now()
		s.autoQueryStatus.LastMatches = len(result.Matches)
		s.autoQueryStatus.LastRetrieved = retrieved
		s.autoQueryStatus.NextRun = time.Now().Add(interval)
		if err != nil {
			s.autoQueryStatus.LastError = err.Error()
		} else {
			s.autoQueryStatus.LastError = ""
		}
	}
	s.autoQueryMu.Unlock()
}

// autoRetrieveMatches retrieves up to MaxMatches matches at the profile's
// configured level and returns how many were attempted.
func (s *Session) autoRetrieveMatches(ctx context.Context, profile autoquery.Profile, matches []query.Match) int {
	limit := len(matches)
	if max := parseMaxMatches(profile.Settings.MaxMatches); max > 0 && max < limit {
		limit = max
	}
	level := strings.ToUpper(strings.TrimSpace(profile.Settings.RetrieveLevel))
	if level == "" {
		level = "STUDY"
	}
	nodeList, err := s.ListNodes()
	if err != nil {
		return 0
	}
	index := map[string]nodes.Node{}
	for _, n := range nodeList {
		index[n.ID] = n
		index[n.Name] = n
	}
	attempted := 0
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			break
		}
		m := matches[i]
		node, ok := index[m.SourceNodeID]
		if !ok {
			node, ok = index[m.SourceNodeName]
		}
		if !ok {
			continue
		}
		_, _ = s.Retrieve(ctx, node, level, m.StudyInstanceUID, m.SeriesInstanceUID, m.SOPInstanceUID, nil)
		attempted++
	}
	return attempted
}

func parseMaxMatches(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
