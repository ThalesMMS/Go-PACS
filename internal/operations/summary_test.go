package operations

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

func TestImportSummaryConvertsReportToWarningSummary(t *testing.T) {
	report := archive.ImportReport{
		ScannedFiles: 3,
		StoredFiles:  1,
		Duplicates:   1,
		InvalidFiles: 1,
		Rejections: []archive.Rejection{
			{Path: "/incoming/bad.dcm", Reason: "DICOM parse failed"},
		},
	}

	summary := ImportSummary(report, 1500*time.Millisecond, "/incoming")

	if summary.Version != 1 {
		t.Fatalf("Version = %d, want 1", summary.Version)
	}
	if summary.Kind != KindImport {
		t.Fatalf("Kind = %q, want %q", summary.Kind, KindImport)
	}
	if summary.Status != StatusWarning {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusWarning)
	}
	if summary.DurationMS != 1500 {
		t.Fatalf("DurationMS = %d, want 1500", summary.DurationMS)
	}
	if summary.RetryInput == nil || summary.RetryInput.Path != "/incoming" {
		t.Fatalf("RetryInput = %#v, want import path", summary.RetryInput)
	}
	if summary.Counts.Requested == nil || *summary.Counts.Requested != 3 {
		t.Fatalf("Requested = %#v, want 3", summary.Counts.Requested)
	}
	if summary.Counts.Stored == nil || *summary.Counts.Stored != 1 {
		t.Fatalf("Stored = %#v, want 1", summary.Counts.Stored)
	}
	if summary.Counts.Duplicates == nil || *summary.Counts.Duplicates != 1 {
		t.Fatalf("Duplicates = %#v, want 1", summary.Counts.Duplicates)
	}
	if summary.Counts.Failed == nil || *summary.Counts.Failed != 1 {
		t.Fatalf("Failed = %#v, want 1", summary.Counts.Failed)
	}
	if len(summary.Failures) != 1 || summary.Failures[0].Message != "/incoming/bad.dcm: DICOM parse failed" {
		t.Fatalf("Failures = %#v", summary.Failures)
	}
}

func TestImportSummaryMarksAllRejectedImportAsFailure(t *testing.T) {
	summary := ImportSummary(archive.ImportReport{
		ScannedFiles: 1,
		InvalidFiles: 1,
		Rejections: []archive.Rejection{
			{Path: "/incoming/bad.dcm", Reason: "DICOM parse failed"},
		},
	}, 20*time.Millisecond, "/incoming")

	if summary.Status != StatusFailure {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusFailure)
	}
}

func TestOperationSummaryJSONUsesStableSnakeCaseValues(t *testing.T) {
	summary := ImportSummary(archive.ImportReport{ScannedFiles: 1, StoredFiles: 1}, time.Second, "")
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !containsAll(got, `"version":1`, `"kind":"import"`, `"status":"success"`, `"duration_ms":1000`, `"requested":1`, `"stored":1`) {
		t.Fatalf("summary JSON = %s", got)
	}
}

func TestOperationSummaryJSONIncludesLogReferences(t *testing.T) {
	summary := Summary{
		Version: SummaryVersion,
		Kind:    KindImport,
		Status:  StatusSuccess,
		Logs: []LogReference{{
			Path:          "logs/import.log",
			CorrelationID: "abc123",
			LineRange:     [2]uint64{10, 20},
		}},
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !containsAll(got, `"logs"`, `"path":"logs/import.log"`, `"correlation_id":"abc123"`, `"line_range":[10,20]`) {
		t.Fatalf("summary JSON = %s", got)
	}
}

func TestQuerySummaryCountsMatches(t *testing.T) {
	summary := QuerySummary(query.Result{
		Matches:     []query.Match{{}, {}},
		FinalStatus: 0,
		Duration:    250 * time.Millisecond,
	})

	if summary.Kind != KindQueryFind {
		t.Fatalf("Kind = %q, want %q", summary.Kind, KindQueryFind)
	}
	if summary.Status != StatusSuccess {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusSuccess)
	}
	if summary.Counts.Matched == nil || *summary.Counts.Matched != 2 {
		t.Fatalf("Matched = %#v, want 2", summary.Counts.Matched)
	}
}

func TestSendSummaryCountsFailures(t *testing.T) {
	summary := SendSummary(send.Outcome{
		Method:    send.MethodSTOWRS,
		Attempted: 3,
		Sent:      1,
		Warnings:  1,
		Failed:    1,
		Failures:  []string{"one.dcm: status 0xA700"},
		Results: []send.Result{{
			Path:                        "one.dcm",
			SOPClassUID:                 "1.2.840.10008.5.1.4.1.1.2",
			SOPInstanceUID:              "1.2.3",
			RequestedTransferSyntaxUID:  "1.2.840.10008.1.2.1",
			NegotiatedTransferSyntaxUID: "1.2.840.10008.1.2.1",
			Status:                      0xA700,
			Error:                       "C-STORE failed with status 0xA700",
		}},
		Duration: 2 * time.Second,
	}, "node-1", "IMAGE", "1.2.study", "1.2.series", "1.2.3")

	if summary.Kind != KindSendStore {
		t.Fatalf("Kind = %q, want %q", summary.Kind, KindSendStore)
	}
	if summary.Method != send.MethodSTOWRS {
		t.Fatalf("Method = %q, want %q", summary.Method, send.MethodSTOWRS)
	}
	if summary.Status != StatusWarning {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusWarning)
	}
	if summary.Counts.Requested == nil || *summary.Counts.Requested != 3 {
		t.Fatalf("Requested = %#v, want 3", summary.Counts.Requested)
	}
	if summary.Counts.Sent == nil || *summary.Counts.Sent != 1 {
		t.Fatalf("Sent = %#v, want 1", summary.Counts.Sent)
	}
	if summary.Counts.Failed == nil || *summary.Counts.Failed != 1 {
		t.Fatalf("Failed = %#v, want 1", summary.Counts.Failed)
	}
	if len(summary.Transfers) != 1 {
		t.Fatalf("len(Transfers) = %d, want 1", len(summary.Transfers))
	}
	if summary.Transfers[0].SOPInstanceUID != "1.2.3" || summary.Transfers[0].StatusCode != "0xA700" {
		t.Fatalf("transfer detail = %#v", summary.Transfers[0])
	}
	if len(summary.Failures) != 1 || summary.Failures[0].Message != "one.dcm: status 0xA700" {
		t.Fatalf("Failures = %#v", summary.Failures)
	}
	if summary.RetryInput == nil || summary.RetryInput.NodeID != "node-1" || summary.RetryInput.SOPUID != "1.2.3" {
		t.Fatalf("RetryInput = %#v, want send scope", summary.RetryInput)
	}
}

func TestRetrieveSummaryCountsSuboperationsAndReceiverStores(t *testing.T) {
	summary := RetrieveSummary(retrieve.Outcome{
		Completed: 2,
		Failed:    1,
		Warnings:  1,
		Receiver:  receive.Snapshot{Stored: 2, Duplicates: 1},
		Progress: []retrieve.Progress{
			{FinalStatus: 0xFF00, Remaining: 1, Completed: 1},
			{FinalStatus: 0xB000, Remaining: 0, Completed: 2, Failed: 1, Warnings: 1},
		},
		Duration: 3 * time.Second,
	}, "node-1", "STUDY", "1.2.study", "", "")

	if summary.Kind != KindRetrieveMove {
		t.Fatalf("Kind = %q, want %q", summary.Kind, KindRetrieveMove)
	}
	if summary.Status != StatusWarning {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusWarning)
	}
	if summary.Counts.Received == nil || *summary.Counts.Received != 2 {
		t.Fatalf("Received = %#v, want 2", summary.Counts.Received)
	}
	if summary.Counts.Stored == nil || *summary.Counts.Stored != 2 {
		t.Fatalf("Stored = %#v, want 2", summary.Counts.Stored)
	}
	if summary.Counts.Duplicates == nil || *summary.Counts.Duplicates != 1 {
		t.Fatalf("Duplicates = %#v, want 1", summary.Counts.Duplicates)
	}
	if summary.Counts.Failed == nil || *summary.Counts.Failed != 1 {
		t.Fatalf("Failed = %#v, want 1", summary.Counts.Failed)
	}
	if len(summary.Progress) != 2 {
		t.Fatalf("len(Progress) = %d, want 2", len(summary.Progress))
	}
	if summary.Progress[0].StatusCode != "0xFF00" || summary.Progress[1].StatusCode != "0xB000" {
		t.Fatalf("Progress = %#v", summary.Progress)
	}
	if summary.RetryInput == nil || summary.RetryInput.NodeID != "node-1" || summary.RetryInput.StudyUID != "1.2.study" {
		t.Fatalf("RetryInput = %#v, want retrieve scope", summary.RetryInput)
	}
}

func TestRetrieveSummaryFlagsRemoteCompletionWithoutLocalStores(t *testing.T) {
	summary := RetrieveSummary(retrieve.Outcome{
		Completed: 785,
		Receiver:  receive.Snapshot{Associations: 0, Stored: 0, Duplicates: 0, Failed: 0},
		Progress: []retrieve.Progress{
			{FinalStatus: 0x0000, Remaining: 0, Completed: 785},
		},
		Duration: time.Minute,
	}, "node-1", "STUDY", "1.2.study", "", "")

	if summary.Status != StatusFailure {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusFailure)
	}
	if summary.Counts.Received == nil || *summary.Counts.Received != 785 {
		t.Fatalf("Received = %#v, want 785", summary.Counts.Received)
	}
	if summary.Counts.Stored == nil || *summary.Counts.Stored != 0 {
		t.Fatalf("Stored = %#v, want 0", summary.Counts.Stored)
	}
	if len(summary.Failures) != 1 {
		t.Fatalf("Failures = %#v, want one diagnostic failure", summary.Failures)
	}
	if !strings.Contains(summary.Failures[0].Message, "remote completed 785 C-STORE suboperations") {
		t.Fatalf("Failure message = %q", summary.Failures[0].Message)
	}
}

func TestRetrieveSummaryCountsDirectGetStores(t *testing.T) {
	summary := RetrieveSummary(retrieve.Outcome{
		Method:    retrieve.MethodGet,
		Completed: 1,
		Stored:    1,
		Progress: []retrieve.Progress{
			{FinalStatus: 0x0000, Remaining: 0, Completed: 1},
		},
		Duration: time.Second,
	}, "node-1", "SERIES", "1.2.study", "1.2.series", "")

	if summary.Status != StatusSuccess {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusSuccess)
	}
	if summary.Counts.Received == nil || *summary.Counts.Received != 1 {
		t.Fatalf("Received = %#v, want 1", summary.Counts.Received)
	}
	if summary.Counts.Stored == nil || *summary.Counts.Stored != 1 {
		t.Fatalf("Stored = %#v, want 1", summary.Counts.Stored)
	}
	if len(summary.Failures) != 0 {
		t.Fatalf("Failures = %#v, want none", summary.Failures)
	}
}

func TestRetrieveSummaryRecordsWADORSMethodAndCounts(t *testing.T) {
	summary := RetrieveSummary(retrieve.Outcome{
		Method:     "WADO-RS",
		Requested:  3,
		Completed:  2,
		Failed:     1,
		Stored:     1,
		Duplicates: 1,
		Rejected:   1,
		Failures:   []string{"1.2.3: malformed DICOM object"},
		Progress: []retrieve.Progress{
			{FinalStatus: 0x0000, Remaining: 2, Completed: 1},
			{FinalStatus: 0xB000, Remaining: 0, Completed: 2, Failed: 1},
		},
		Duration: 4 * time.Second,
	}, "web-node", "IMAGE", "1.2.study", "1.2.series", "1.2.3")

	if summary.Kind != KindRetrieveWADORS {
		t.Fatalf("Kind = %q, want %q", summary.Kind, KindRetrieveWADORS)
	}
	if summary.Status != StatusWarning {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusWarning)
	}
	if summary.Counts.Requested == nil || *summary.Counts.Requested != 3 {
		t.Fatalf("Requested = %#v, want 3", summary.Counts.Requested)
	}
	if summary.Counts.Stored == nil || *summary.Counts.Stored != 1 {
		t.Fatalf("Stored = %#v, want 1", summary.Counts.Stored)
	}
	if summary.Counts.Duplicates == nil || *summary.Counts.Duplicates != 1 {
		t.Fatalf("Duplicates = %#v, want 1", summary.Counts.Duplicates)
	}
	if summary.Counts.Failed == nil || *summary.Counts.Failed != 1 {
		t.Fatalf("Failed = %#v, want 1", summary.Counts.Failed)
	}
	if summary.Counts.Rejected == nil || *summary.Counts.Rejected != 1 {
		t.Fatalf("Rejected = %#v, want 1", summary.Counts.Rejected)
	}
	if len(summary.Failures) != 1 || summary.Failures[0].Message != "1.2.3: malformed DICOM object" {
		t.Fatalf("Failures = %#v", summary.Failures)
	}
	if summary.RetryInput == nil || summary.RetryInput.NodeID != "web-node" || summary.RetryInput.SOPUID != "1.2.3" {
		t.Fatalf("RetryInput = %#v, want WADO retrieve scope", summary.RetryInput)
	}
}

func TestReceiverSummaryCountsSnapshot(t *testing.T) {
	summary := ReceiverSummary(receive.Snapshot{
		Associations: 4,
		Stored:       2,
		Duplicates:   1,
		Rejected:     1,
		Failed:       1,
	}, time.Second)

	if summary.Kind != KindStorageSCP {
		t.Fatalf("Kind = %q, want %q", summary.Kind, KindStorageSCP)
	}
	if summary.Status != StatusWarning {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusWarning)
	}
	if summary.Counts.Received == nil || *summary.Counts.Received != 4 {
		t.Fatalf("Received = %#v, want 4", summary.Counts.Received)
	}
	if summary.Counts.Stored == nil || *summary.Counts.Stored != 2 {
		t.Fatalf("Stored = %#v, want 2", summary.Counts.Stored)
	}
	if summary.Counts.Failed == nil || *summary.Counts.Failed != 2 {
		t.Fatalf("Failed = %#v, want 2", summary.Counts.Failed)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
