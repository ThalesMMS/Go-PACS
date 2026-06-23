package core

import (
	"context"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

// callingAETitle returns the effective Calling AE title from configuration.
func (s *Session) callingAETitle() string {
	cfg, _ := s.LoadConfig()
	return effectiveAETitle(cfg.LocalAETitle)
}

// QueryEnabledNodes returns the configured nodes that participate in queries.
func (s *Session) QueryEnabledNodes() ([]nodes.Node, error) {
	list, err := s.ListNodes()
	if err != nil {
		return nil, err
	}
	var out []nodes.Node
	for _, n := range list {
		if n.Enabled() && n.QueryEnabled() {
			out = append(out, n)
		}
	}
	return out, nil
}

// QueryStudies runs a Study Root study-level C-FIND across the given sources,
// merging and annotating results and reporting progress to obs (may be nil).
func (s *Session) QueryStudies(ctx context.Context, sources []nodes.Node, criteria query.Criteria, obs QueryObserver) (query.Result, error) {
	ae := s.callingAETitle()
	return RunQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
		return query.StudyRootFind(ctx, node, criteria, ae)
	}, obs)
}

// QuerySeries runs a Study Root series-level C-FIND across the given sources.
func (s *Session) QuerySeries(ctx context.Context, sources []nodes.Node, criteria query.SeriesCriteria, obs QueryObserver) (query.Result, error) {
	ae := s.callingAETitle()
	return RunQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
		return query.StudyRootSeriesFind(ctx, node, criteria, ae)
	}, obs)
}

// QueryImages runs a Study Root image-level C-FIND across the given sources.
func (s *Session) QueryImages(ctx context.Context, sources []nodes.Node, criteria query.ImageCriteria, obs QueryObserver) (query.Result, error) {
	ae := s.callingAETitle()
	return RunQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
		return query.StudyRootImageFind(ctx, node, criteria, ae)
	}, obs)
}

// RetrieveObserver receives retrieve progress. A nil observer ignores progress.
type RetrieveObserver interface {
	RetrieveProgress(retrieve.Progress)
}

// RetrieveObserverFunc adapts a function to a RetrieveObserver.
type RetrieveObserverFunc func(retrieve.Progress)

// RetrieveProgress implements RetrieveObserver. A nil func value is a no-op.
func (f RetrieveObserverFunc) RetrieveProgress(p retrieve.Progress) {
	if f != nil {
		f(p)
	}
}

// Retrieve fetches a study, series, or image (by level) from node into the local
// archive. It assembles retrieve options from configuration, the node's retrieve
// method, and the running receiver (for C-MOVE), records a retrieve summary in
// the operation history, and reports progress to obs (may be nil). level is
// "IMAGE", "SERIES", or anything else for study-level.
func (s *Session) Retrieve(ctx context.Context, node nodes.Node, level, studyUID, seriesUID, sopUID string, obs RetrieveObserver) (retrieve.Outcome, error) {
	cfg, err := s.LoadConfig()
	if err != nil {
		return retrieve.Outcome{}, err
	}
	moveDestination := node.PreferredMoveDestination
	if moveDestination == "" {
		moveDestination = effectiveAETitle(cfg.LocalAETitle)
	}
	opts := retrieve.Options{
		CallingAETitle:  effectiveAETitle(cfg.LocalAETitle),
		Method:          node.RetrieveMethodOrDefault(),
		MoveDestination: moveDestination,
		ReceiveAddress:  cfg.ReceiverAddress,
	}
	if cfg.MaxStoreObjectBytes != nil {
		opts.MaxStoreObjectBytes = *cfg.MaxStoreObjectBytes
	}
	s.receiverMu.Lock()
	opts.Receiver = s.receiver
	s.receiverMu.Unlock()
	if obs != nil {
		opts.OnProgress = obs.RetrieveProgress
	}

	var outcome retrieve.Outcome
	switch level {
	case "IMAGE":
		outcome, err = retrieve.RetrieveImage(ctx, s.catalog, node, studyUID, seriesUID, sopUID, opts)
	case "SERIES":
		outcome, err = retrieve.RetrieveSeries(ctx, s.catalog, node, studyUID, seriesUID, opts)
	default:
		outcome, err = retrieve.RetrieveStudy(ctx, s.catalog, node, studyUID, opts)
	}

	if history, herr := s.LoadHistory(); herr == nil {
		_ = s.SaveHistory(ops.Prepend(history, ops.RetrieveSummary(outcome)))
	}
	return outcome, err
}

// AutoQueryCriteria maps an auto-query profile's criteria to a study-level
// C-FIND criteria. It mirrors the Fyne quick-search mapping (search field →
// patient/accession, on-date → study date, first modality → modality).
func AutoQueryCriteria(c autoquery.Criteria) query.Criteria {
	qc := query.Criteria{}
	switch strings.ToLower(strings.TrimSpace(c.SearchField)) {
	case "patientid", "patient id", "id":
		qc.PatientID = c.SearchText
	case "accession", "accessionnumber":
		qc.AccessionNumber = c.SearchText
	default:
		qc.PatientName = c.SearchText
	}
	if d := strings.TrimSpace(c.OnDate); d != "" {
		qc.StudyDateFrom = d
		qc.StudyDateTo = d
	}
	if len(c.Modalities) > 0 {
		qc.Modality = c.Modalities[0]
	}
	return qc
}

// autoQuerySources resolves a profile's enabled sources to configured nodes.
func (s *Session) autoQuerySources(profile autoquery.Profile) ([]nodes.Node, error) {
	all, err := s.ListNodes()
	if err != nil {
		return nil, err
	}
	index := map[string]nodes.Node{}
	for _, n := range all {
		index[n.ID] = n
		index[n.Name] = n
	}
	var out []nodes.Node
	for _, src := range profile.Sources {
		if !src.Enabled {
			continue
		}
		if n, ok := index[src.NodeID]; ok {
			out = append(out, n)
		} else if n, ok := index[src.Name]; ok {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return s.QueryEnabledNodes()
	}
	return out, nil
}

// RunAutoQuery executes a profile's study-level C-FIND across its enabled sources.
func (s *Session) RunAutoQuery(ctx context.Context, profile autoquery.Profile, obs QueryObserver) (query.Result, error) {
	sources, err := s.autoQuerySources(profile)
	if err != nil {
		return query.Result{}, err
	}
	return s.QueryStudies(ctx, sources, AutoQueryCriteria(profile.Criteria), obs)
}

// SendObserver receives send progress. A nil observer ignores progress.
type SendObserver interface {
	SendProgress(send.Progress)
}

// SendObserverFunc adapts a function to a SendObserver.
type SendObserverFunc func(send.Progress)

// SendProgress implements SendObserver. A nil func value is a no-op.
func (f SendObserverFunc) SendProgress(p send.Progress) {
	if f != nil {
		f(p)
	}
}

// Send transmits a study, series, or image (by level) from the local archive to
// node via C-STORE, records a send summary in the operation history, and reports
// progress to obs (may be nil).
func (s *Session) Send(ctx context.Context, node nodes.Node, level, studyUID, seriesUID, sopUID string, obs SendObserver) (send.Outcome, error) {
	cfg, err := s.LoadConfig()
	if err != nil {
		return send.Outcome{}, err
	}
	opts := send.Options{CallingAETitle: effectiveAETitle(cfg.LocalAETitle)}
	if obs != nil {
		opts.OnProgress = obs.SendProgress
	}
	var outcome send.Outcome
	switch level {
	case "IMAGE":
		outcome, err = send.SendInstanceWithOptions(ctx, s.catalog, node, sopUID, opts)
	case "SERIES":
		outcome, err = send.SendSeriesWithOptions(ctx, s.catalog, node, seriesUID, opts)
	default:
		outcome, err = send.SendStudyWithOptions(ctx, s.catalog, node, studyUID, opts)
	}
	if history, herr := s.LoadHistory(); herr == nil {
		_ = s.SaveHistory(ops.Prepend(history, ops.SendSummary(outcome)))
	}
	return outcome, err
}

// StartRetrieveJob runs Retrieve asynchronously, returning a Job whose events
// stream retrieve.Progress and the final retrieve.Outcome. The job is cancelable.
func (s *Session) StartRetrieveJob(node nodes.Node, level, studyUID, seriesUID, sopUID string) *Job {
	return s.startJob("retrieve", func(ctx context.Context, emit func(any)) (any, error) {
		return s.Retrieve(ctx, node, level, studyUID, seriesUID, sopUID, RetrieveObserverFunc(func(p retrieve.Progress) { emit(p) }))
	})
}

// StartSendJob runs Send asynchronously, streaming send.Progress and the final
// send.Outcome.
func (s *Session) StartSendJob(node nodes.Node, level, studyUID, seriesUID, sopUID string) *Job {
	return s.startJob("send", func(ctx context.Context, emit func(any)) (any, error) {
		return s.Send(ctx, node, level, studyUID, seriesUID, sopUID, SendObserverFunc(func(p send.Progress) { emit(p) }))
	})
}

// StartImportJob runs ImportPath asynchronously, streaming archive.ImportProgress
// and the final archive.ImportReport.
func (s *Session) StartImportJob(path string) *Job {
	return s.startJob("import", func(ctx context.Context, emit func(any)) (any, error) {
		return s.ImportPath(ctx, path, ImportObserverFunc(func(p archive.ImportProgress) { emit(p) }))
	})
}

// ImportObserver receives import progress. A nil observer ignores progress.
type ImportObserver interface {
	ImportProgress(archive.ImportProgress)
}

// ImportObserverFunc adapts a function to an ImportObserver.
type ImportObserverFunc func(archive.ImportProgress)

// ImportProgress implements ImportObserver. A nil func value is a no-op.
func (f ImportObserverFunc) ImportProgress(p archive.ImportProgress) {
	if f != nil {
		f(p)
	}
}

// ImportPath imports a file, directory, or .zip at path into the local archive,
// applying the configured safety limits, recording an import summary, and
// reporting progress to obs (may be nil).
func (s *Session) ImportPath(ctx context.Context, path string, obs ImportObserver) (archive.ImportReport, error) {
	cfg, err := s.LoadConfig()
	if err != nil {
		return archive.ImportReport{}, err
	}
	opts := archive.ImportOptions{Limits: importLimits(cfg)}
	if obs != nil {
		opts.OnProgress = obs.ImportProgress
	}
	start := time.Now()
	report, err := s.catalog.ImportPathWithOptions(ctx, path, opts)
	if history, herr := s.LoadHistory(); herr == nil {
		_ = s.SaveHistory(ops.Prepend(history, ops.ImportSummary(report, time.Since(start))))
	}
	return report, err
}

func importLimits(cfg appconfig.Config) archive.ImportLimits {
	d64 := func(p *int64) int64 {
		if p != nil {
			return *p
		}
		return 0
	}
	di := func(p *int) int {
		if p != nil {
			return *p
		}
		return 0
	}
	return archive.ImportLimits{
		MaxFileImportBytes:      d64(cfg.MaxFileImportBytes),
		MaxZipEntryBytes:        d64(cfg.MaxZipEntryBytes),
		MaxZipTotalBytes:        d64(cfg.MaxZipTotalBytes),
		MaxZipEntryCount:        di(cfg.MaxZipEntryCount),
		MaxImportTotalFiles:     di(cfg.MaxImportTotalFiles),
		MaxImportPathLength:     di(cfg.MaxImportPathLength),
		MaxImportDirectoryDepth: di(cfg.MaxImportDirectoryDepth),
	}
}

// InspectInstance returns a DICOM element summary for a stored instance, found by
// its SOP Instance UID. The on-disk path is resolved through the catalog (never
// from client input), so only archived objects can be inspected.
func (s *Session) InspectInstance(ctx context.Context, sopInstanceUID string) (dicominspect.Summary, error) {
	inst, err := s.catalog.InstanceBySOPInstanceUID(ctx, sopInstanceUID)
	if err != nil {
		return dicominspect.Summary{}, err
	}
	return dicominspect.InspectFile(inst.StoredPath, dicominspect.DefaultOptions())
}
