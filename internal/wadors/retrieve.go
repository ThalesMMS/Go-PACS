package wadors

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/net/dicomweb"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/object"
)

const (
	MethodWADORS   = "WADO-RS"
	DefaultTimeout = 60 * time.Second
)

var (
	tagStudyInstanceUID  = core.NewTag(0x0020, 0x000D)
	tagSeriesInstanceUID = core.NewTag(0x0020, 0x000E)
	tagSOPInstanceUID    = core.NewTag(0x0008, 0x0018)
)

type Options struct {
	MaxObjectBytes int64
	OnProgress     func(Progress)
}

type Progress struct {
	Requested int64
	Completed int64
	Failed    int64
	Rejected  int64
}

type Outcome struct {
	Method     string
	Requested  int64
	Stored     int64
	Duplicates int64
	Failed     int64
	Rejected   int64
	Failures   []string
	Progress   []Progress
	Duration   time.Duration
}

// ClientForNode constructs a DICOMweb client for the given node with a default HTTP timeout and optional custom TLS configuration.
func ClientForNode(node nodes.Node, tlsConfig *tls.Config) dicomweb.Client {
	opts := dicomweb.Options{Timeout: DefaultTimeout}
	if tlsConfig != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConfig
		opts.HTTPClient = &http.Client{Transport: transport}
	}
	return dicomweb.Client{
		Endpoint: dicomweb.Endpoint{
			BaseURL:  strings.TrimSpace(node.BaseURL),
			QIDOPath: nodes.NormalizeDICOMwebPathPrefix(node.QIDOPathPrefix),
			WADOPath: nodes.NormalizeDICOMwebPathPrefix(node.WADOPathPrefix),
			STOWPath: nodes.NormalizeDICOMwebPathPrefix(node.STOWPathPrefix),
		},
		Options: opts,
	}
}

// RetrieveStudy retrieves all instances in a study from a WADO-RS server and imports them into the archive catalog. It validates the study instance UID, queries metadata to discover instances, and retrieves and imports each instance. The returned Outcome summarizes the operation, including counts of requested, stored, rejected, and failed instances, along with the total duration.
func RetrieveStudy(ctx context.Context, catalog *archive.Catalog, client dicomweb.Client, studyInstanceUID string, opts Options) (Outcome, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" || studyInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("study instance UID is required")
	}
	start := time.Now()
	outcome := newOutcome()
	refs, err := client.StudyMetadata(ctx, studyInstanceUID)
	if err != nil {
		outcome.Duration = time.Since(start)
		recordFailure(&outcome, fmt.Errorf("WADO-RS study metadata: %w", err), opts)
		return outcome, err
	}
	outcome.Requested = int64(len(refs))
	err = retrieveRefs(ctx, catalog, client, refs, opts, &outcome)
	outcome.Duration = time.Since(start)
	return outcome, err
}

// RetrieveSeries retrieves DICOM instances from a series using WADO-RS and imports them into an archive catalog.
// Study and series instance UIDs must be non-empty and not "(missing)".
// It returns an Outcome with retrieval statistics and any error from querying series metadata or retrieving instances.
func RetrieveSeries(ctx context.Context, catalog *archive.Catalog, client dicomweb.Client, studyInstanceUID, seriesInstanceUID string, opts Options) (Outcome, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" || studyInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("study instance UID is required")
	}
	seriesInstanceUID = strings.TrimSpace(seriesInstanceUID)
	if seriesInstanceUID == "" || seriesInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("series instance UID is required")
	}
	start := time.Now()
	outcome := newOutcome()
	refs, err := client.SeriesMetadata(ctx, studyInstanceUID, seriesInstanceUID)
	if err != nil {
		outcome.Duration = time.Since(start)
		recordFailure(&outcome, fmt.Errorf("WADO-RS series metadata: %w", err), opts)
		return outcome, err
	}
	outcome.Requested = int64(len(refs))
	err = retrieveRefs(ctx, catalog, client, refs, opts, &outcome)
	outcome.Duration = time.Since(start)
	return outcome, err
}

// RetrieveInstance retrieves a single DICOM instance from a DICOMweb server and imports it into the archive catalog.
func RetrieveInstance(ctx context.Context, catalog *archive.Catalog, client dicomweb.Client, ref dicomweb.InstanceRef, opts Options) (Outcome, error) {
	ref = normalizeRef(ref)
	if err := validateRef(ref); err != nil {
		return Outcome{}, err
	}
	start := time.Now()
	outcome := newOutcome()
	outcome.Requested = 1
	err := retrieveOne(ctx, catalog, client, ref, opts, &outcome)
	outcome.Duration = time.Since(start)
	return outcome, err
}

// ToRetrieveOutcome converts a WADO-RS retrieval outcome to the standard retrieve outcome format with DIMSE status mapping.
func ToRetrieveOutcome(outcome Outcome) retrieve.Outcome {
	failed := outcome.Failed + outcome.Rejected
	finalStatus := dimse.StatusSuccess
	if failed > 0 {
		if outcome.Stored+outcome.Duplicates > 0 {
			finalStatus = dimse.StatusCMoveSubOperationsCompleteOneOrMoreFailures
		} else {
			finalStatus = dimse.StatusCMoveUnableToProcess
		}
	}
	converted := retrieve.Outcome{
		Method:      MethodWADORS,
		FinalStatus: finalStatus,
		StatusClass: dimse.ClassifyCMoveStatus(finalStatus),
		Requested:   outcome.Requested,
		Completed:   clampUint16(outcome.Stored + outcome.Duplicates),
		Failed:      clampUint16(failed),
		Stored:      outcome.Stored,
		Duplicates:  outcome.Duplicates,
		Rejected:    outcome.Rejected,
		Failures:    append([]string(nil), outcome.Failures...),
		Duration:    outcome.Duration,
	}
	for _, progress := range outcome.Progress {
		converted.Progress = append(converted.Progress, ToRetrieveProgress(progress))
	}
	return converted
}

// ToRetrieveProgress converts a Progress into retrieve.Progress with DIMSE final status determination based on completion and failure counts.
func ToRetrieveProgress(progress Progress) retrieve.Progress {
	failed := progress.Failed + progress.Rejected
	done := progress.Completed + failed
	remaining := progress.Requested - done
	if remaining < 0 {
		remaining = 0
	}
	progressStatus := dimse.StatusSuccess
	if failed > 0 {
		if progress.Completed > 0 {
			progressStatus = dimse.StatusCMoveSubOperationsCompleteOneOrMoreFailures
		} else {
			progressStatus = dimse.StatusCMoveUnableToProcess
		}
	}
	return retrieve.Progress{
		FinalStatus: progressStatus,
		StatusClass: dimse.ClassifyCMoveStatus(progressStatus),
		Remaining:   clampUint16(remaining),
		Completed:   clampUint16(progress.Completed),
		Failed:      clampUint16(failed),
	}
}

// newOutcome creates a new Outcome with the WADO-RS method.
func newOutcome() Outcome {
	return Outcome{Method: MethodWADORS}
}

// retrieveRefs retrieves and imports each of the provided instance references, accumulating results in the outcome and returning joined errors from all operations.
func retrieveRefs(ctx context.Context, catalog *archive.Catalog, client dicomweb.Client, refs []dicomweb.InstanceRef, opts Options, outcome *Outcome) error {
	var errs []error
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := retrieveOne(ctx, catalog, client, normalizeRef(ref), opts, outcome); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// An error is returned if the archive catalog is nil, the reference is invalid, retrieval fails, or imported objects are rejected.
func retrieveOne(ctx context.Context, catalog *archive.Catalog, client dicomweb.Client, ref dicomweb.InstanceRef, opts Options, outcome *Outcome) error {
	if catalog == nil {
		return fmt.Errorf("archive catalog is required")
	}
	if err := validateRef(ref); err != nil {
		recordFailure(outcome, err, opts)
		return err
	}
	client = withBodyLimit(client, opts.MaxObjectBytes)
	beforeFailures := outcome.Failed
	beforeRejected := outcome.Rejected
	err := client.RetrieveInstanceStreamWithOptions(ctx, ref, dicomweb.RetrieveOptions{}, func(part dicomweb.ObjectPartStream) error {
		return importPart(ctx, catalog, ref, part, opts, outcome)
	})
	if err != nil {
		if outcome.Failed == beforeFailures && outcome.Rejected == beforeRejected {
			recordFailure(outcome, err, opts)
		}
		return err
	}
	if outcome.Failed > beforeFailures || outcome.Rejected > beforeRejected {
		return fmt.Errorf("WADO-RS retrieve completed with %d failed object(s) and %d rejected object(s)", outcome.Failed-beforeFailures, outcome.Rejected-beforeRejected)
	}
	return nil
}

// importPart imports a received DICOM object part from a WADO-RS stream into the archive catalog and updates the outcome counters. It returns an error if the object cannot be read, parsed, or imported, or if the archive rejects the object.
func importPart(ctx context.Context, catalog *archive.Catalog, ref dicomweb.InstanceRef, part dicomweb.ObjectPartStream, opts Options, outcome *Outcome) error {
	data, err := readObjectPart(part.Reader, opts.MaxObjectBytes)
	if err != nil {
		recordFailure(outcome, fmt.Errorf("%s: %w", ref.SOPInstanceUID, err), opts)
		return err
	}
	file, err := object.ReadFile(bytes.NewReader(data))
	if err != nil {
		recordFailure(outcome, fmt.Errorf("%s: %w", ref.SOPInstanceUID, err), opts)
		return err
	}
	if err := validateObjectUIDs(ref, file.Dataset); err != nil {
		recordFailure(outcome, err, opts)
		return err
	}
	report, err := catalog.ImportObjectWithOptions(ctx, sourcePath(ref), file.Dataset, file.TransferSyntax, archive.ImportOptions{
		Limits: archive.ImportLimits{MaxFileImportBytes: opts.MaxObjectBytes},
	})
	if err != nil {
		recordFailure(outcome, fmt.Errorf("%s: %w", ref.SOPInstanceUID, err), opts)
		return err
	}
	outcome.Stored += int64(report.StoredFiles)
	outcome.Duplicates += int64(report.Duplicates)
	rejected := int64(report.InvalidFiles + len(report.Rejections))
	if rejected > 0 {
		outcome.Rejected += rejected
		for _, rejection := range report.Rejections {
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("%s: %s", ref.SOPInstanceUID, rejection.Reason))
		}
		if report.InvalidFiles > 0 && len(report.Rejections) == 0 {
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("%s: archive rejected object", ref.SOPInstanceUID))
		}
		reportProgress(outcome, opts)
		return fmt.Errorf("archive rejected WADO-RS object %s", ref.SOPInstanceUID)
	}
	reportProgress(outcome, opts)
	return nil
}

func validateObjectUIDs(ref dicomweb.InstanceRef, dataset *object.Object) error {
	for _, item := range []struct {
		name string
		tag  core.Tag
		want string
	}{
		{name: "StudyInstanceUID", tag: tagStudyInstanceUID, want: ref.StudyInstanceUID},
		{name: "SeriesInstanceUID", tag: tagSeriesInstanceUID, want: ref.SeriesInstanceUID},
		{name: "SOPInstanceUID", tag: tagSOPInstanceUID, want: ref.SOPInstanceUID},
	} {
		got, ok := dataset.GetUID(item.tag)
		if !ok || got != item.want {
			return fmt.Errorf("WADO-RS object %s mismatch: got %q want %q", item.name, got, item.want)
		}
	}
	return nil
}

// readObjectPart reads all available data from r, returning an error if the data exceeds maxBytes bytes. If maxBytes is less than or equal to zero, no limit is enforced.
func readObjectPart(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(r)
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("DICOM object exceeds %d bytes", maxBytes)
	}
	return data, nil
}

// recordFailure records a retrieval failure and reports progress.
func recordFailure(outcome *Outcome, err error, opts Options) {
	outcome.Failed++
	if err != nil {
		outcome.Failures = append(outcome.Failures, err.Error())
	}
	reportProgress(outcome, opts)
}

// reportProgress appends a progress snapshot to the outcome and invokes the optional callback.
func reportProgress(outcome *Outcome, opts Options) {
	progress := Progress{
		Requested: outcome.Requested,
		Completed: outcome.Stored + outcome.Duplicates,
		Failed:    outcome.Failed,
		Rejected:  outcome.Rejected,
	}
	outcome.Progress = append(outcome.Progress, progress)
	if opts.OnProgress != nil {
		opts.OnProgress(progress)
	}
}

// withBodyLimit sets the maximum HTTP request body size on the client if maxBytes is positive.
func withBodyLimit(client dicomweb.Client, maxBytes int64) dicomweb.Client {
	if maxBytes > 0 {
		client.Options.MaxBodyBytes = maxBytes
	}
	return client
}

// normalizeRef returns a new instance reference with all UIDs trimmed of whitespace.
func normalizeRef(ref dicomweb.InstanceRef) dicomweb.InstanceRef {
	return dicomweb.InstanceRef{
		StudyInstanceUID:  strings.TrimSpace(ref.StudyInstanceUID),
		SeriesInstanceUID: strings.TrimSpace(ref.SeriesInstanceUID),
		SOPInstanceUID:    strings.TrimSpace(ref.SOPInstanceUID),
	}
}

// validateRef reports an error if the instance reference has an empty or missing study, series, or SOP instance UID.
func validateRef(ref dicomweb.InstanceRef) error {
	if ref.StudyInstanceUID == "" || ref.StudyInstanceUID == "(missing)" {
		return fmt.Errorf("study instance UID is required")
	}
	if ref.SeriesInstanceUID == "" || ref.SeriesInstanceUID == "(missing)" {
		return fmt.Errorf("series instance UID is required")
	}
	if ref.SOPInstanceUID == "" || ref.SOPInstanceUID == "(missing)" {
		return fmt.Errorf("SOP instance UID is required")
	}
	return nil
}

// sourcePath formats a WADO-RS source identifier for the instance reference.
func sourcePath(ref dicomweb.InstanceRef) string {
	return fmt.Sprintf("wadors://studies/%s/series/%s/instances/%s", ref.StudyInstanceUID, ref.SeriesInstanceUID, ref.SOPInstanceUID)
}

// clampUint16 clamps value to the valid range of uint16.
func clampUint16(value int64) uint16 {
	if value <= 0 {
		return 0
	}
	if value > int64(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(value)
}
