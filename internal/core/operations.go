package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/qido"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
	"github.com/ThalesMMS/Go-PACS/internal/wadors"
	"github.com/ThalesMMS/dicom-go/net/dicomweb"
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

// QueryStudies runs a study-level query across the given sources, merging and
// annotating results and reporting progress to obs (may be nil).
func (s *Session) QueryStudies(ctx context.Context, sources []nodes.Node, criteria query.Criteria, obs QueryObserver) (query.Result, error) {
	ae := s.callingAETitle()
	return RunQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
		return QueryStudySource(ctx, node, criteria, ae)
	}, obs)
}

// QuerySeries runs a series-level query across the given sources.
func (s *Session) QuerySeries(ctx context.Context, sources []nodes.Node, criteria query.SeriesCriteria, obs QueryObserver) (query.Result, error) {
	ae := s.callingAETitle()
	return RunQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
		return QuerySeriesSource(ctx, node, criteria, ae)
	}, obs)
}

// QueryImages runs an image-level query across the given sources.
func (s *Session) QueryImages(ctx context.Context, sources []nodes.Node, criteria query.ImageCriteria, obs QueryObserver) (query.Result, error) {
	ae := s.callingAETitle()
	return RunQueryAcrossSources(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
		return QueryImageSource(ctx, node, criteria, ae)
	}, obs)
}

// QueryStudySource queries studies from a node.
func QueryStudySource(ctx context.Context, node nodes.Node, criteria query.Criteria, callingAETitle string) (query.Result, error) {
	if node.IsDICOMweb() {
		return qido.StudyQuery(ctx, node, criteria)
	}
	return query.StudyRootFind(ctx, node, criteria, callingAETitle)
}

// QuerySeriesSource queries for series using the specified node and criteria, routing to the appropriate backend based on the node type.
func QuerySeriesSource(ctx context.Context, node nodes.Node, criteria query.SeriesCriteria, callingAETitle string) (query.Result, error) {
	if node.IsDICOMweb() {
		return qido.SeriesQuery(ctx, node, criteria)
	}
	return query.StudyRootSeriesFind(ctx, node, criteria, callingAETitle)
}

// QueryImageSource queries images from a node, selecting DICOMweb or traditional DICOM based on the node type.
func QueryImageSource(ctx context.Context, node nodes.Node, criteria query.ImageCriteria, callingAETitle string) (query.Result, error) {
	if node.IsDICOMweb() {
		return qido.ImageQuery(ctx, node, criteria)
	}
	return query.StudyRootImageFind(ctx, node, criteria, callingAETitle)
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

	if node.IsDICOMweb() {
		outcome, err := s.retrieveDICOMweb(ctx, node, level, studyUID, seriesUID, sopUID, opts.MaxStoreObjectBytes, obs)
		if history, herr := s.LoadHistory(); herr == nil {
			_ = s.SaveHistory(ops.Prepend(history, ops.RetrieveSummary(outcome, node.ID, level, studyUID, seriesUID, sopUID)))
		}
		return outcome, err
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
		_ = s.SaveHistory(ops.Prepend(history, ops.RetrieveSummary(outcome, node.ID, level, studyUID, seriesUID, sopUID)))
	}
	return outcome, err
}

func (s *Session) retrieveDICOMweb(ctx context.Context, node nodes.Node, level, studyUID, seriesUID, sopUID string, maxObjectBytes int64, obs RetrieveObserver) (retrieve.Outcome, error) {
	tlsConfig, err := netverify.TLSConfigForNode(node)
	if err != nil {
		return retrieve.Outcome{}, err
	}
	client := wadors.ClientForNode(node, tlsConfig)
	opts := wadors.Options{MaxObjectBytes: maxObjectBytes}
	if obs != nil {
		opts.OnProgress = func(p wadors.Progress) {
			obs.RetrieveProgress(wadors.ToRetrieveProgress(p))
		}
	}
	var outcome wadors.Outcome
	switch level {
	case "IMAGE":
		outcome, err = wadors.RetrieveInstance(ctx, s.catalog, client, dicomweb.InstanceRef{StudyInstanceUID: studyUID, SeriesInstanceUID: seriesUID, SOPInstanceUID: sopUID}, opts)
	case "SERIES":
		outcome, err = wadors.RetrieveSeries(ctx, s.catalog, client, studyUID, seriesUID, opts)
	default:
		outcome, err = wadors.RetrieveStudy(ctx, s.catalog, client, studyUID, opts)
	}
	return wadors.ToRetrieveOutcome(outcome), err
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
		_ = s.SaveHistory(ops.Prepend(history, ops.SendSummary(outcome, node.ID, level, studyUID, seriesUID, sopUID)))
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

// StartRetryJob reruns a retry-capable failed or warning task asynchronously.
func (s *Session) StartRetryJob(historyIndex int) *Job {
	summary, err := s.retryTaskSummary(historyIndex)
	return s.startJob("retry", func(ctx context.Context, emit func(any)) (any, error) {
		if err != nil {
			return nil, err
		}
		return nil, s.retrySummary(ctx, summary)
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
		_ = s.SaveHistory(ops.Prepend(history, ops.ImportSummary(report, time.Since(start), path)))
	}
	return report, err
}

var (
	ErrTaskIndexOutOfRange = errors.New("task index out of range")
	ErrTaskNoRetryInput    = errors.New("task has no retry input")
	ErrTaskNotRetryable    = errors.New("only failed or warning tasks can be retried")
)

type RetryEligibility struct {
	CanRetry bool   `json:"canRetry"`
	Reason   string `json:"reason,omitempty"`
}

// CanRetryTask checks whether a persisted task-history entry is currently
// eligible for retry.
func (s *Session) CanRetryTask(ctx context.Context, historyIndex int) (RetryEligibility, error) {
	summary, err := s.retryTaskSummary(historyIndex)
	if err != nil {
		return RetryEligibility{}, err
	}
	return s.CanRetrySummary(ctx, summary)
}

// CanRetrySummary checks retry eligibility for an already-loaded task summary.
func (s *Session) CanRetrySummary(ctx context.Context, summary ops.Summary) (RetryEligibility, error) {
	if err := retryStaticError(summary); err != nil {
		return RetryEligibility{Reason: err.Error()}, nil
	}
	if _, err := s.validateRetryInput(ctx, summary); err != nil {
		return RetryEligibility{Reason: err.Error()}, nil
	}
	return RetryEligibility{CanRetry: true}, nil
}

// RetryTask reruns a retry-capable failed or warning task. The original import,
// send, or retrieve method records the new task summary when it finishes.
func (s *Session) RetryTask(ctx context.Context, historyIndex int) error {
	summary, err := s.retryTaskSummary(historyIndex)
	if err != nil {
		return err
	}
	return s.retrySummary(ctx, summary)
}

func (s *Session) retrySummary(ctx context.Context, summary ops.Summary) error {
	if err := retryStaticError(summary); err != nil {
		return err
	}

	start := time.Now()
	node, err := s.validateRetryInput(ctx, summary)
	if err != nil {
		s.recordRetryValidationFailure(summary, err, time.Since(start))
		return err
	}

	input := summary.RetryInput
	switch summary.Kind {
	case ops.KindImport:
		_, err = s.ImportPath(ctx, input.Path, nil)
	case ops.KindSendStore:
		_, err = s.Send(ctx, node, input.Level, input.StudyUID, input.SeriesUID, input.SOPUID, nil)
	case ops.KindRetrieveMove, ops.KindRetrieveWADORS:
		_, err = s.Retrieve(ctx, node, input.Level, input.StudyUID, input.SeriesUID, input.SOPUID, nil)
	default:
		err = fmt.Errorf("task kind %q cannot be retried", summary.Kind)
	}
	return err
}

func (s *Session) retryTaskSummary(historyIndex int) (ops.Summary, error) {
	history, err := s.LoadHistory()
	if err != nil {
		return ops.Summary{}, err
	}
	if historyIndex < 0 || historyIndex >= len(history) {
		return ops.Summary{}, ErrTaskIndexOutOfRange
	}
	return history[historyIndex], nil
}

// retryStaticError validates that summary has retry input with a supported version, a retryable status, and a retryable kind.
func retryStaticError(summary ops.Summary) error {
	if summary.RetryInput == nil {
		return ErrTaskNoRetryInput
	}
	if summary.RetryInput.Version != ops.RetryInputVersion {
		return fmt.Errorf("unsupported retry input version %d", summary.RetryInput.Version)
	}
	if summary.Status != ops.StatusFailure && summary.Status != ops.StatusWarning {
		return ErrTaskNotRetryable
	}
	switch summary.Kind {
	case ops.KindImport, ops.KindSendStore, ops.KindRetrieveMove, ops.KindRetrieveWADORS:
		return nil
	default:
		return fmt.Errorf("task kind %q cannot be retried", summary.Kind)
	}
}

func (s *Session) validateRetryInput(ctx context.Context, summary ops.Summary) (nodes.Node, error) {
	input := summary.RetryInput
	if input == nil {
		return nodes.Node{}, ErrTaskNoRetryInput
	}
	switch summary.Kind {
	case ops.KindImport:
		return nodes.Node{}, validateImportRetryInput(input)
	case ops.KindSendStore:
		node, err := s.retryNode(input.NodeID)
		if err != nil {
			return nodes.Node{}, err
		}
		if !node.SendEnabled() {
			return nodes.Node{}, errors.New("node send is disabled")
		}
		return node, s.validateSendRetryInput(ctx, input)
	case ops.KindRetrieveMove, ops.KindRetrieveWADORS:
		node, err := s.retryNode(input.NodeID)
		if err != nil {
			return nodes.Node{}, err
		}
		if !node.QueryEnabled() {
			return nodes.Node{}, errors.New("node query/retrieve is disabled")
		}
		if err := validateRetrieveRetryInput(input); err != nil {
			return nodes.Node{}, err
		}
		if err := s.validateRetrieveRetryPrerequisites(summary.Kind, node); err != nil {
			return nodes.Node{}, err
		}
		return node, nil
	default:
		return nodes.Node{}, fmt.Errorf("task kind %q cannot be retried", summary.Kind)
	}
}

func (s *Session) validateRetrieveRetryPrerequisites(kind ops.Kind, node nodes.Node) error {
	if kind != ops.KindRetrieveMove || node.RetrieveMethodOrDefault() == nodes.RetrieveMethodGet {
		return nil
	}
	s.receiverMu.Lock()
	running := s.receiver != nil
	s.receiverMu.Unlock()
	if !running {
		return retrieve.ErrReceiverRequired
	}
	return nil
}

// validateImportRetryInput validates that the retry input contains a non-empty, accessible path.
// It returns an error if the path is empty after trimming whitespace, does not exist, or cannot be accessed.
func validateImportRetryInput(input *ops.RetryInput) error {
	if strings.TrimSpace(input.Path) == "" {
		return errors.New("source path is required")
	}
	if _, err := os.Stat(input.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("source path no longer exists")
		}
		return fmt.Errorf("check source path: %w", err)
	}
	return nil
}

// validateRetrieveRetryInput validates that a retrieval task's retry input contains all required DICOM UIDs for its retrieval level. Study UID is always required; series UID is required for series and image level; SOP instance UID is required for image level. It returns an error if any required UID is missing.
func validateRetrieveRetryInput(input *ops.RetryInput) error {
	level := retryLevel(input.Level)
	if strings.TrimSpace(input.StudyUID) == "" {
		return errors.New("study UID is required")
	}
	if level == "SERIES" || level == "IMAGE" {
		if strings.TrimSpace(input.SeriesUID) == "" {
			return errors.New("series UID is required")
		}
	}
	if level == "IMAGE" && strings.TrimSpace(input.SOPUID) == "" {
		return errors.New("SOP instance UID is required")
	}
	return nil
}

func (s *Session) validateSendRetryInput(ctx context.Context, input *ops.RetryInput) error {
	switch retryLevel(input.Level) {
	case "IMAGE":
		if strings.TrimSpace(input.SOPUID) == "" {
			return errors.New("SOP instance UID is required")
		}
		if _, err := s.catalog.InstanceBySOPInstanceUID(ctx, input.SOPUID); err != nil {
			return errors.New("image not found in archive")
		}
	case "SERIES":
		if strings.TrimSpace(input.SeriesUID) == "" {
			return errors.New("series UID is required")
		}
		instances, err := s.catalog.InstancesForSeries(ctx, input.SeriesUID)
		if err != nil {
			return err
		}
		if len(instances) == 0 {
			return errors.New("series not found in archive")
		}
	default:
		if strings.TrimSpace(input.StudyUID) == "" {
			return errors.New("study UID is required")
		}
		exists, err := s.catalog.StudyExists(ctx, input.StudyUID)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("study not found in archive")
		}
	}
	return nil
}

func (s *Session) retryNode(nodeID string) (nodes.Node, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nodes.Node{}, errors.New("node ID is required")
	}
	list, err := s.ListNodes()
	if err != nil {
		return nodes.Node{}, err
	}
	for _, node := range list {
		if node.ID == nodeID {
			if !node.Enabled() {
				return nodes.Node{}, errors.New("node is disabled")
			}
			return node, nil
		}
	}
	return nodes.Node{}, errors.New("node no longer exists")
}

// retryLevel normalizes a level string to a canonical query level.
// It returns "IMAGE" or "SERIES" if the input matches those values
// (case-insensitive and trimmed), otherwise "STUDY".
func retryLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "IMAGE":
		return "IMAGE"
	case "SERIES":
		return "SERIES"
	default:
		return "STUDY"
	}
}

func (s *Session) recordRetryValidationFailure(original ops.Summary, err error, duration time.Duration) {
	history, herr := s.LoadHistory()
	if herr != nil {
		return
	}
	summary := ops.RetryFailureSummary(original, err.Error(), duration)
	_ = s.SaveHistory(ops.Prepend(history, summary))
}

// importLimits converts configuration values to import limits, treating nil pointers as zero.
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
