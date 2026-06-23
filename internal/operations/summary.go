package operations

import (
	"fmt"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

const (
	SummaryVersion    = 1
	RetryInputVersion = 1
)

type Kind string

const (
	KindQueryFind      Kind = "query_find"
	KindRetrieveMove   Kind = "retrieve_move"
	KindRetrieveWADORS Kind = "retrieve_wado_rs"
	KindSendStore      Kind = "send_store"
	KindImport         Kind = "import"
	KindStorageSCP     Kind = "storage_scp"
	KindStoragePolicy  Kind = "storage_policy"
)

type Status string

const (
	StatusSuccess   Status = "success"
	StatusWarning   Status = "warning"
	StatusFailure   Status = "failure"
	StatusCancelled Status = "cancelled"
)

type Counts struct {
	Requested  *uint64 `json:"requested,omitempty"`
	Matched    *uint64 `json:"matched,omitempty"`
	Sent       *uint64 `json:"sent,omitempty"`
	Received   *uint64 `json:"received,omitempty"`
	Stored     *uint64 `json:"stored,omitempty"`
	Failed     *uint64 `json:"failed,omitempty"`
	Rejected   *uint64 `json:"rejected,omitempty"`
	Duplicates *uint64 `json:"duplicates,omitempty"`
	Skipped    *uint64 `json:"skipped,omitempty"`
	Purged     *uint64 `json:"purged,omitempty"`
}

type FailureDetail struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type LogReference struct {
	Path          string    `json:"path"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	LineRange     [2]uint64 `json:"line_range,omitempty"`
}

type TransferDetail struct {
	Path                        string `json:"path,omitempty"`
	SOPClassUID                 string `json:"sop_class_uid,omitempty"`
	SOPInstanceUID              string `json:"sop_instance_uid,omitempty"`
	RequestedTransferSyntaxUID  string `json:"requested_transfer_syntax_uid,omitempty"`
	NegotiatedTransferSyntaxUID string `json:"negotiated_transfer_syntax_uid,omitempty"`
	StatusCode                  string `json:"status_code,omitempty"`
	Error                       string `json:"error,omitempty"`
}

type ProgressDetail struct {
	StatusCode string `json:"status_code"`
	Remaining  uint16 `json:"remaining"`
	Completed  uint16 `json:"completed"`
	Failed     uint16 `json:"failed"`
	Warnings   uint16 `json:"warnings"`
}

type RetryInput struct {
	Version   int    `json:"version"`
	Path      string `json:"path,omitempty"`
	NodeID    string `json:"nodeID,omitempty"`
	Level     string `json:"level,omitempty"`
	StudyUID  string `json:"studyUID,omitempty"`
	SeriesUID string `json:"seriesUID,omitempty"`
	SOPUID    string `json:"sopUID,omitempty"`
}

type Summary struct {
	Version    uint32           `json:"version"`
	Kind       Kind             `json:"kind"`
	Method     string           `json:"method,omitempty"`
	DurationMS uint64           `json:"duration_ms"`
	Status     Status           `json:"status"`
	Counts     Counts           `json:"counts,omitempty"`
	Failures   []FailureDetail  `json:"failures,omitempty"`
	Logs       []LogReference   `json:"logs,omitempty"`
	Transfers  []TransferDetail `json:"transfers,omitempty"`
	Progress   []ProgressDetail `json:"progress,omitempty"`
	RetryInput *RetryInput      `json:"retryInput,omitempty"`
}

func ImportSummary(report archive.ImportReport, duration time.Duration, sourcePath string) Summary {
	failed := maxInt(len(report.Rejections), report.InvalidFiles)
	summary := Summary{
		Version:    SummaryVersion,
		Kind:       KindImport,
		DurationMS: uint64(duration / time.Millisecond),
		Status:     statusFromCounts(report.StoredFiles+report.Duplicates, 0, failed),
		Counts: Counts{
			Requested:  uint64Ptr(uint64(report.ScannedFiles)),
			Stored:     uint64Ptr(uint64(report.StoredFiles)),
			Failed:     uint64Ptr(uint64(failed)),
			Duplicates: uint64Ptr(uint64(report.Duplicates)),
		},
		RetryInput: retryInputForImport(sourcePath),
	}
	for _, rejection := range report.Rejections {
		message := rejection.Reason
		if rejection.Path != "" {
			message = fmt.Sprintf("%s: %s", rejection.Path, rejection.Reason)
		}
		summary.Failures = append(summary.Failures, FailureDetail{Message: message})
	}
	return summary
}

func StoragePolicySummary(report archive.PurgeExpiredTrashReport, duration time.Duration) Summary {
	summary := Summary{
		Version:    SummaryVersion,
		Kind:       KindStoragePolicy,
		Method:     "trash_purge_expired",
		DurationMS: uint64(duration / time.Millisecond),
		Status:     statusFromCounts(report.Purged, 0, len(report.Errors)),
		Counts: Counts{
			Requested: uint64Ptr(uint64(report.Scanned)),
			Purged:    uint64Ptr(uint64(report.Purged)),
			Skipped:   uint64Ptr(uint64(report.Skipped)),
			Failed:    uint64Ptr(uint64(len(report.Errors))),
		},
	}
	for _, err := range report.Errors {
		summary.Failures = append(summary.Failures, FailureDetail{Message: err})
	}
	return summary
}

func QuerySummary(result query.Result) Summary {
	status := StatusSuccess
	if result.FinalStatus != 0 {
		status = StatusFailure
		if len(result.Matches) > 0 {
			status = StatusWarning
		}
	}
	return Summary{
		Version:    SummaryVersion,
		Kind:       KindQueryFind,
		DurationMS: uint64(result.Duration / time.Millisecond),
		Status:     status,
		Counts: Counts{
			Matched: uint64Ptr(uint64(len(result.Matches))),
		},
	}
}

func SendSummary(outcome send.Outcome, nodeID, level, studyUID, seriesUID, sopUID string) Summary {
	summary := Summary{
		Version:    SummaryVersion,
		Kind:       KindSendStore,
		Method:     outcome.Method,
		DurationMS: uint64(outcome.Duration / time.Millisecond),
		Status:     statusFromCounts(outcome.Sent, outcome.Warnings, outcome.Failed),
		Counts: Counts{
			Requested: uint64Ptr(uint64(outcome.Attempted)),
			Sent:      uint64Ptr(uint64(outcome.Sent)),
			Failed:    uint64Ptr(uint64(outcome.Failed)),
		},
		RetryInput: retryInputForScopedOperation(nodeID, level, studyUID, seriesUID, sopUID),
	}
	for _, failure := range outcome.Failures {
		summary.Failures = append(summary.Failures, FailureDetail{Message: failure})
	}
	for _, result := range outcome.Results {
		summary.Transfers = append(summary.Transfers, TransferDetail{
			Path:                        result.Path,
			SOPClassUID:                 result.SOPClassUID,
			SOPInstanceUID:              result.SOPInstanceUID,
			RequestedTransferSyntaxUID:  result.RequestedTransferSyntaxUID,
			NegotiatedTransferSyntaxUID: result.NegotiatedTransferSyntaxUID,
			StatusCode:                  fmt.Sprintf("0x%04X", result.Status),
			Error:                       result.Error,
		})
	}
	return summary
}

func RetrieveSummary(outcome retrieve.Outcome, nodeID, level, studyUID, seriesUID, sopUID string) Summary {
	if outcome.Method == "WADO-RS" {
		return retrieveWADORSSummary(outcome, nodeID, level, studyUID, seriesUID, sopUID)
	}
	stored := outcome.Stored
	duplicates := outcome.Duplicates
	if stored == 0 && duplicates == 0 {
		stored = outcome.Receiver.Stored
		duplicates = outcome.Receiver.Duplicates
	}
	localStored := stored + duplicates
	receiverFailed := outcome.Receiver.Failed + outcome.Receiver.Rejected
	failed := maxInt(int(outcome.Failed), int(receiverFailed))
	status := statusFromCounts(int(localStored), int(outcome.Warnings), failed)
	if outcome.Completed > 0 && localStored == 0 {
		status = StatusFailure
	}
	summary := Summary{
		Version:    SummaryVersion,
		Kind:       KindRetrieveMove,
		DurationMS: uint64(outcome.Duration / time.Millisecond),
		Status:     status,
		Counts: Counts{
			Received:   uint64Ptr(uint64(outcome.Completed)),
			Stored:     uint64Ptr(uint64(stored)),
			Duplicates: uint64Ptr(uint64(duplicates)),
			Failed:     uint64Ptr(uint64(failed)),
		},
		RetryInput: retryInputForScopedOperation(nodeID, level, studyUID, seriesUID, sopUID),
	}
	appendOutcomeProgress(&summary, outcome.Progress)
	if outcome.Completed > 0 && localStored == 0 {
		summary.Failures = append(summary.Failures, FailureDetail{
			Message: fmt.Sprintf("remote completed %d C-STORE suboperations, but local receiver stored no objects; check Move Destination AE routing and receiver address", outcome.Completed),
		})
	}
	if outcome.Receiver.Rejected > 0 {
		summary.Failures = append(summary.Failures, FailureDetail{
			Message: fmt.Sprintf("local receiver rejected %d inbound association(s)", outcome.Receiver.Rejected),
		})
	}
	return summary
}

func retrieveWADORSSummary(outcome retrieve.Outcome, nodeID, level, studyUID, seriesUID, sopUID string) Summary {
	completed := outcome.Stored + outcome.Duplicates
	failed := int(outcome.Failed) + int(outcome.Rejected)
	if outcome.Failed > 0 && outcome.Rejected > 0 && int(outcome.Failed) >= int(outcome.Rejected) {
		failed = int(outcome.Failed)
	}
	summary := Summary{
		Version:    SummaryVersion,
		Kind:       KindRetrieveWADORS,
		DurationMS: uint64(outcome.Duration / time.Millisecond),
		Status:     statusFromCounts(int(completed), int(outcome.Warnings), failed),
		Counts: Counts{
			Requested:  uint64Ptr(uint64(maxInt64(outcome.Requested, completed+int64(failed)))),
			Received:   uint64Ptr(uint64(completed)),
			Stored:     uint64Ptr(uint64(outcome.Stored)),
			Duplicates: uint64Ptr(uint64(outcome.Duplicates)),
			Failed:     uint64Ptr(uint64(outcome.Failed)),
			Rejected:   uint64Ptr(uint64(outcome.Rejected)),
		},
		RetryInput: retryInputForScopedOperation(nodeID, level, studyUID, seriesUID, sopUID),
	}
	for _, failure := range outcome.Failures {
		summary.Failures = append(summary.Failures, FailureDetail{Message: failure})
	}
	appendOutcomeProgress(&summary, outcome.Progress)
	return summary
}

func RetryFailureSummary(original Summary, message string, duration time.Duration) Summary {
	return Summary{
		Version:    SummaryVersion,
		Kind:       original.Kind,
		DurationMS: uint64(duration / time.Millisecond),
		Status:     StatusFailure,
		Failures:   []FailureDetail{{Message: message}},
		RetryInput: cloneRetryInput(original.RetryInput),
	}
}

func ReceiverSummary(snapshot receive.Snapshot, duration time.Duration) Summary {
	failed := snapshot.Rejected + snapshot.Failed
	return Summary{
		Version:    SummaryVersion,
		Kind:       KindStorageSCP,
		DurationMS: uint64(duration / time.Millisecond),
		Status:     statusFromCounts(int(snapshot.Stored+snapshot.Duplicates), 0, int(failed)),
		Counts: Counts{
			Received:   uint64Ptr(uint64(snapshot.Associations)),
			Stored:     uint64Ptr(uint64(snapshot.Stored)),
			Duplicates: uint64Ptr(uint64(snapshot.Duplicates)),
			Failed:     uint64Ptr(uint64(failed)),
		},
	}
}

func appendOutcomeProgress(summary *Summary, progress []retrieve.Progress) {
	for _, p := range progress {
		summary.Progress = append(summary.Progress, ProgressDetail{
			StatusCode: fmt.Sprintf("0x%04X", p.FinalStatus),
			Remaining:  p.Remaining,
			Completed:  p.Completed,
			Failed:     p.Failed,
			Warnings:   p.Warnings,
		})
	}
}

func statusFromCounts(successes, warnings, failures int) Status {
	if failures > 0 {
		if successes > 0 || warnings > 0 {
			return StatusWarning
		}
		return StatusFailure
	}
	if warnings > 0 {
		return StatusWarning
	}
	return StatusSuccess
}

func uint64Ptr(value uint64) *uint64 {
	return &value
}

func retryInputForImport(sourcePath string) *RetryInput {
	if sourcePath == "" {
		return nil
	}
	return &RetryInput{
		Version: RetryInputVersion,
		Path:    sourcePath,
	}
}

func retryInputForScopedOperation(nodeID, level, studyUID, seriesUID, sopUID string) *RetryInput {
	if nodeID == "" && level == "" && studyUID == "" && seriesUID == "" && sopUID == "" {
		return nil
	}
	return &RetryInput{
		Version:   RetryInputVersion,
		NodeID:    nodeID,
		Level:     level,
		StudyUID:  studyUID,
		SeriesUID: seriesUID,
		SOPUID:    sopUID,
	}
}

func cloneRetryInput(input *RetryInput) *RetryInput {
	if input == nil {
		return nil
	}
	clone := *input
	return &clone
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
