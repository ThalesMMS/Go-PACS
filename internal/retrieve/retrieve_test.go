package retrieve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/send"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const (
	testMoveStorageSOPClassUID = "1.2.840.10008.5.1.4.1.1.2"
	testMoveStudyInstanceUID   = "1.2.826.0.1.3680043.10.543.200"
	testMoveSeriesInstanceUID  = "1.2.826.0.1.3680043.10.543.201"
	testMoveSOPInstanceUID     = "1.2.826.0.1.3680043.10.543.202"
)

func TestRetrieveStudyMovesIntoLocalArchive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	receiver, err := receive.Start(ctx, receive.Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: "MOVEDEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopReceiver(t, receiver)

	source := filepath.Join(t.TempDir(), "move-source.dcm")
	if err := os.WriteFile(source, testMovePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	moveNode, done := startMoveSCP(t, ctx, source, receiver)
	outcome, err := RetrieveStudy(ctx, catalog, moveNode, testMoveStudyInstanceUID, Options{
		CallingAETitle:  "MOVESCU",
		MoveDestination: "MOVEDEST",
		Receiver:        receiver,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.FinalStatus != dimse.StatusSuccess {
		t.Fatalf("FinalStatus = 0x%04X, want success", outcome.FinalStatus)
	}
	if outcome.Completed != 1 {
		t.Fatalf("Completed = %d, want 1", outcome.Completed)
	}
	if outcome.Failed != 0 || outcome.Warnings != 0 {
		t.Fatalf("outcome = %#v, want no failures or warnings", outcome)
	}
	if outcome.Receiver.Stored != 1 {
		t.Fatalf("Receiver.Stored = %d, want 1", outcome.Receiver.Stored)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("len(studies) = %d, want 1", len(studies))
	}
	if studies[0].StudyInstanceUID != testMoveStudyInstanceUID {
		t.Fatalf("StudyInstanceUID = %q, want %q", studies[0].StudyInstanceUID, testMoveStudyInstanceUID)
	}
	if err := <-done; err != nil {
		t.Fatalf("move SCP error = %v", err)
	}
}

func TestRetrieveSeriesUsesSeriesLevelMoveIdentifier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	receiver, err := receive.Start(ctx, receive.Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: "MOVEDEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopReceiver(t, receiver)

	source := filepath.Join(t.TempDir(), "move-series-source.dcm")
	if err := os.WriteFile(source, testMovePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	moveNode, done := startSeriesMoveSCP(t, ctx, source, receiver)
	outcome, err := RetrieveSeries(ctx, catalog, moveNode, testMoveStudyInstanceUID, testMoveSeriesInstanceUID, Options{
		CallingAETitle:  "MOVESCU",
		MoveDestination: "MOVEDEST",
		Receiver:        receiver,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.FinalStatus != dimse.StatusSuccess {
		t.Fatalf("FinalStatus = 0x%04X, want success", outcome.FinalStatus)
	}
	if outcome.Completed != 1 {
		t.Fatalf("Completed = %d, want 1", outcome.Completed)
	}
	if outcome.Failed != 0 || outcome.Warnings != 0 {
		t.Fatalf("outcome = %#v, want no failures or warnings", outcome)
	}
	if outcome.Receiver.Stored != 1 {
		t.Fatalf("Receiver.Stored = %d, want 1", outcome.Receiver.Stored)
	}

	instances, err := catalog.InstancesForSeries(ctx, testMoveSeriesInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].SOPInstanceUID != testMoveSOPInstanceUID {
		t.Fatalf("SOPInstanceUID = %q, want %q", instances[0].SOPInstanceUID, testMoveSOPInstanceUID)
	}
	if err := <-done; err != nil {
		t.Fatalf("move SCP error = %v", err)
	}
}

func TestRetrieveImageUsesImageLevelMoveIdentifier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	receiver, err := receive.Start(ctx, receive.Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: "MOVEDEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopReceiver(t, receiver)

	source := filepath.Join(t.TempDir(), "move-image-source.dcm")
	if err := os.WriteFile(source, testMovePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	moveNode, done := startImageMoveSCP(t, ctx, source, receiver)
	outcome, err := RetrieveImage(ctx, catalog, moveNode, testMoveStudyInstanceUID, testMoveSeriesInstanceUID, testMoveSOPInstanceUID, Options{
		CallingAETitle:  "MOVESCU",
		MoveDestination: "MOVEDEST",
		Receiver:        receiver,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.FinalStatus != dimse.StatusSuccess {
		t.Fatalf("FinalStatus = 0x%04X, want success", outcome.FinalStatus)
	}
	if outcome.Completed != 1 {
		t.Fatalf("Completed = %d, want 1", outcome.Completed)
	}
	if outcome.Failed != 0 || outcome.Warnings != 0 {
		t.Fatalf("outcome = %#v, want no failures or warnings", outcome)
	}
	if outcome.Receiver.Stored != 1 {
		t.Fatalf("Receiver.Stored = %d, want 1", outcome.Receiver.Stored)
	}

	instance, err := catalog.InstanceBySOPInstanceUID(ctx, testMoveSOPInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.SeriesInstanceUID != testMoveSeriesInstanceUID {
		t.Fatalf("SeriesInstanceUID = %q, want %q", instance.SeriesInstanceUID, testMoveSeriesInstanceUID)
	}
	if err := <-done; err != nil {
		t.Fatalf("move SCP error = %v", err)
	}
}

func TestRetrieveImageFallsBackToCGetWhenMoveStoresNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := testMoveDataSet(t)
	node, done := startMoveFailureThenGetSCP(t, ctx, source)
	outcome, err := RetrieveImage(ctx, catalog, node, testMoveStudyInstanceUID, testMoveSeriesInstanceUID, testMoveSOPInstanceUID, Options{
		CallingAETitle:  "GETSCU",
		MoveDestination: "MOVEDEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Method != MethodGet {
		t.Fatalf("Method = %q, want %q", outcome.Method, MethodGet)
	}
	if outcome.FinalStatus != dimse.StatusSuccess {
		t.Fatalf("FinalStatus = 0x%04X, want success", outcome.FinalStatus)
	}
	if outcome.Completed != 1 || outcome.Failed != 0 || outcome.Warnings != 0 {
		t.Fatalf("outcome = %#v, want one completed C-GET sub-operation", outcome)
	}
	if outcome.Stored != 1 || outcome.Duplicates != 0 {
		t.Fatalf("stored counts = %d/%d, want 1/0", outcome.Stored, outcome.Duplicates)
	}
	instance, err := catalog.InstanceBySOPInstanceUID(ctx, testMoveSOPInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.SeriesInstanceUID != testMoveSeriesInstanceUID {
		t.Fatalf("SeriesInstanceUID = %q, want %q", instance.SeriesInstanceUID, testMoveSeriesInstanceUID)
	}
	if err := <-done; err != nil {
		t.Fatalf("retrieve SCP error = %v", err)
	}
}

func TestRetrieveMethodPreferenceNormalizes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"empty_auto", "", ""},
		{"auto", "Auto", ""},
		{"move", " c-move ", MethodMove},
		{"get", " c-get ", MethodGet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := retrieveMethodPreference(Options{Method: tt.value})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("retrieveMethodPreference(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestRetrieveMethodPreferenceRejectsUnknownMethod(t *testing.T) {
	if _, err := retrieveMethodPreference(Options{Method: "WADO"}); err == nil {
		t.Fatal("retrieveMethodPreference accepted WADO")
	}
}

func TestShouldTryGetFallbackRespectsMoveOnlyPreference(t *testing.T) {
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	outcome := Outcome{StatusClass: dimse.CMoveStatusFailure}
	if !shouldTryGetFallback(context.Background(), catalog, outcome, Options{}) {
		t.Fatal("auto retrieve should allow C-GET fallback")
	}
	if shouldTryGetFallback(context.Background(), catalog, outcome, Options{Method: MethodMove}) {
		t.Fatal("C-MOVE preference should disable C-GET fallback")
	}
}

func TestRetrieveStudyReportsProgressResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	receiver, err := receive.Start(ctx, receive.Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: "MOVEDEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopReceiver(t, receiver)

	source := filepath.Join(t.TempDir(), "move-progress-source.dcm")
	if err := os.WriteFile(source, testMovePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	moveNode, done := startProgressMoveSCP(t, ctx, source, receiver)
	var progressUpdates []Progress
	outcome, err := RetrieveStudy(ctx, catalog, moveNode, testMoveStudyInstanceUID, Options{
		CallingAETitle:  "MOVESCU",
		MoveDestination: "MOVEDEST",
		Receiver:        receiver,
		OnProgress: func(update Progress) {
			progressUpdates = append(progressUpdates, update)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(progressUpdates) != 2 {
		t.Fatalf("progress updates = %d, want pending and final", len(progressUpdates))
	}
	if progressUpdates[0].Remaining != 1 || progressUpdates[0].Completed != 1 {
		t.Fatalf("pending progress = %#v", progressUpdates[0])
	}
	if progressUpdates[1].Remaining != 0 || progressUpdates[1].Completed != 2 {
		t.Fatalf("final progress = %#v", progressUpdates[1])
	}
	if len(outcome.Progress) != 2 {
		t.Fatalf("outcome progress = %d, want 2", len(outcome.Progress))
	}
	if outcome.Completed != 2 {
		t.Fatalf("Completed = %d, want 2", outcome.Completed)
	}
	if err := <-done; err != nil {
		t.Fatalf("move SCP error = %v", err)
	}
}

func startMoveSCP(t *testing.T, ctx context.Context, source string, receiver *receive.Server) (nodes.Node, <-chan error) {
	t.Helper()
	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "MOVESCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootMoveSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCMove(ctx, assoc, pc.ID, dimse.CMoveHandlerFunc(func(_ context.Context, req dimse.CMoveRequestContext) ([]dimse.CMoveSubOperation, error) {
			if req.Request.MoveDestination != "MOVEDEST" {
				return nil, fmt.Errorf("MoveDestination = %q, want MOVEDEST", req.Request.MoveDestination)
			}
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelStudy {
				return nil, fmt.Errorf("QueryRetrieveLevel = %q, want STUDY", req.QueryRetrieveLevel)
			}
			if studyUID, ok := req.Identifier.GetUID(core.NewTag(0x0020, 0x000D)); !ok || studyUID != testMoveStudyInstanceUID {
				return nil, fmt.Errorf("StudyInstanceUID in identifier = %q", studyUID)
			}
			return []dimse.CMoveSubOperation{{
				AffectedSOPClassUID:    testMoveStorageSOPClassUID,
				AffectedSOPInstanceUID: testMoveSOPInstanceUID,
				Store: func(ctx context.Context) dimse.CMoveSubOperationResult {
					node := receiverNode(t, receiver)
					outcome, err := send.SendFiles(ctx, node, []string{source}, "MOVESCP")
					if err != nil {
						return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
					}
					if outcome.Failed > 0 {
						return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: errors.New(strings.Join(outcome.Failures, "; "))}
					}
					return dimse.CMoveSubOperationResult{Status: dimse.StatusSuccess}
				},
			}}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	return nodes.Node{
		Name:    "movescp",
		AETitle: "MOVESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}, done
}

func startSeriesMoveSCP(t *testing.T, ctx context.Context, source string, receiver *receive.Server) (nodes.Node, <-chan error) {
	t.Helper()
	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "MOVESCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootMoveSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCMove(ctx, assoc, pc.ID, dimse.CMoveHandlerFunc(func(_ context.Context, req dimse.CMoveRequestContext) ([]dimse.CMoveSubOperation, error) {
			if req.Request.MoveDestination != "MOVEDEST" {
				return nil, fmt.Errorf("MoveDestination = %q, want MOVEDEST", req.Request.MoveDestination)
			}
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelSeries {
				return nil, fmt.Errorf("QueryRetrieveLevel = %q, want SERIES", req.QueryRetrieveLevel)
			}
			if studyUID, ok := req.Identifier.GetUID(core.NewTag(0x0020, 0x000D)); !ok || studyUID != testMoveStudyInstanceUID {
				return nil, fmt.Errorf("StudyInstanceUID in identifier = %q", studyUID)
			}
			if seriesUID, ok := req.Identifier.GetUID(core.NewTag(0x0020, 0x000E)); !ok || seriesUID != testMoveSeriesInstanceUID {
				return nil, fmt.Errorf("SeriesInstanceUID in identifier = %q", seriesUID)
			}
			return []dimse.CMoveSubOperation{{
				AffectedSOPClassUID:    testMoveStorageSOPClassUID,
				AffectedSOPInstanceUID: testMoveSOPInstanceUID,
				Store: func(ctx context.Context) dimse.CMoveSubOperationResult {
					node := receiverNode(t, receiver)
					outcome, err := send.SendFiles(ctx, node, []string{source}, "MOVESCP")
					if err != nil {
						return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
					}
					if outcome.Failed > 0 {
						return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: errors.New(strings.Join(outcome.Failures, "; "))}
					}
					return dimse.CMoveSubOperationResult{Status: dimse.StatusSuccess}
				},
			}}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	return nodes.Node{
		Name:    "movescp",
		AETitle: "MOVESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}, done
}

func startProgressMoveSCP(t *testing.T, ctx context.Context, source string, receiver *receive.Server) (nodes.Node, <-chan error) {
	t.Helper()
	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "MOVESCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootMoveSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCMove(ctx, assoc, pc.ID, dimse.CMoveHandlerFunc(func(_ context.Context, req dimse.CMoveRequestContext) ([]dimse.CMoveSubOperation, error) {
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelStudy {
				return nil, fmt.Errorf("QueryRetrieveLevel = %q, want STUDY", req.QueryRetrieveLevel)
			}
			store := func(ctx context.Context) dimse.CMoveSubOperationResult {
				node := receiverNode(t, receiver)
				outcome, err := send.SendFiles(ctx, node, []string{source}, "MOVESCP")
				if err != nil {
					return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
				}
				if outcome.Failed > 0 {
					return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: errors.New(strings.Join(outcome.Failures, "; "))}
				}
				return dimse.CMoveSubOperationResult{Status: dimse.StatusSuccess}
			}
			return []dimse.CMoveSubOperation{
				{
					AffectedSOPClassUID:    testMoveStorageSOPClassUID,
					AffectedSOPInstanceUID: testMoveSOPInstanceUID,
					Store:                  store,
				},
				{
					AffectedSOPClassUID:    testMoveStorageSOPClassUID,
					AffectedSOPInstanceUID: testMoveSOPInstanceUID,
					Store:                  store,
				},
			}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	return nodes.Node{
		Name:    "movescp",
		AETitle: "MOVESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}, done
}

func startImageMoveSCP(t *testing.T, ctx context.Context, source string, receiver *receive.Server) (nodes.Node, <-chan error) {
	t.Helper()
	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "MOVESCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.StudyRootMoveSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		defer assoc.Close()

		pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
		if err != nil {
			done <- err
			return
		}
		err = dimse.ServeStudyRootCMove(ctx, assoc, pc.ID, dimse.CMoveHandlerFunc(func(_ context.Context, req dimse.CMoveRequestContext) ([]dimse.CMoveSubOperation, error) {
			if req.Request.MoveDestination != "MOVEDEST" {
				return nil, fmt.Errorf("MoveDestination = %q, want MOVEDEST", req.Request.MoveDestination)
			}
			if req.QueryRetrieveLevel != dimse.QueryRetrieveLevelImage {
				return nil, fmt.Errorf("QueryRetrieveLevel = %q, want IMAGE", req.QueryRetrieveLevel)
			}
			if studyUID, ok := req.Identifier.GetUID(core.NewTag(0x0020, 0x000D)); !ok || studyUID != testMoveStudyInstanceUID {
				return nil, fmt.Errorf("StudyInstanceUID in identifier = %q", studyUID)
			}
			if seriesUID, ok := req.Identifier.GetUID(core.NewTag(0x0020, 0x000E)); !ok || seriesUID != testMoveSeriesInstanceUID {
				return nil, fmt.Errorf("SeriesInstanceUID in identifier = %q", seriesUID)
			}
			if sopUID, ok := req.Identifier.GetUID(core.NewTag(0x0008, 0x0018)); !ok || sopUID != testMoveSOPInstanceUID {
				return nil, fmt.Errorf("SOPInstanceUID in identifier = %q", sopUID)
			}
			return []dimse.CMoveSubOperation{{
				AffectedSOPClassUID:    testMoveStorageSOPClassUID,
				AffectedSOPInstanceUID: testMoveSOPInstanceUID,
				Store: func(ctx context.Context) dimse.CMoveSubOperationResult {
					node := receiverNode(t, receiver)
					outcome, err := send.SendFiles(ctx, node, []string{source}, "MOVESCP")
					if err != nil {
						return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
					}
					if outcome.Failed > 0 {
						return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: errors.New(strings.Join(outcome.Failures, "; "))}
					}
					return dimse.CMoveSubOperationResult{Status: dimse.StatusSuccess}
				},
			}}, nil
		}))
		if err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- errors.New("server expected A-RELEASE-RQ")
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	return nodes.Node{
		Name:    "movescp",
		AETitle: "MOVESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}, done
}

func startMoveFailureThenGetSCP(t *testing.T, ctx context.Context, dataset *object.Object) (nodes.Node, <-chan error) {
	t.Helper()
	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan error, 1)
	go func() {
		if err := serveFailingMove(ctx, listener); err != nil {
			done <- err
			return
		}
		done <- serveSuccessfulGet(ctx, listener, dataset)
	}()

	return nodes.Node{
		Name:    "getscp",
		AETitle: "GETSCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}, done
}

func serveFailingMove(ctx context.Context, listener *ul.Listener) error {
	assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
		AETitle:                   "GETSCP",
		Context:                   ctx,
		SupportedAbstractSyntaxes: []string{dimse.StudyRootMoveSOPClassUID},
		SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
	})
	if err != nil {
		return err
	}
	defer assoc.Close()

	pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
	if err != nil {
		return err
	}
	req, err := dimse.ReceiveCMoveRequest(assoc, pc.ID)
	if err != nil {
		return err
	}
	if _, err := dimse.ReceiveDataSet(assoc, pc.ID, transfer.ImplicitVRLittleEndian); err != nil {
		return err
	}
	zero := uint16(0)
	if err := dimse.SendCMoveResponse(assoc, pc.ID, dimse.CMoveResponse{
		AffectedSOPClassUID:                 dimse.StudyRootMoveSOPClassUID,
		MessageIDBeingRespondedTo:           req.MessageID,
		Status:                              0xA702,
		NumberOfRemainingSuboperationsOrNil: &zero,
		NumberOfCompletedSuboperationsOrNil: &zero,
		NumberOfFailedSuboperationsOrNil:    &zero,
		NumberOfWarningSuboperationsOrNil:   &zero,
	}); err != nil {
		return err
	}
	pdu, err := assoc.ReadPDU()
	if err != nil {
		return err
	}
	if _, ok := pdu.(*ul.ReleaseRQ); !ok {
		return errors.New("move server expected A-RELEASE-RQ")
	}
	return assoc.WritePDU(&ul.ReleaseRP{})
}

func serveSuccessfulGet(ctx context.Context, listener *ul.Listener, dataset *object.Object) error {
	assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
		AETitle:                   "GETSCP",
		Context:                   ctx,
		SupportedAbstractSyntaxes: []string{dimse.StudyRootGetSOPClassUID, testMoveStorageSOPClassUID},
		SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		RoleSelections: []ul.RoleSelectionItem{
			{SopClassUID: testMoveStorageSOPClassUID, SCPRole: true},
		},
	})
	if err != nil {
		return err
	}
	defer assoc.Close()

	getPC, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootGetSOPClassUID)
	if err != nil {
		return err
	}
	storePC, err := dimse.AcceptedContextForSOPClass(assoc, testMoveStorageSOPClassUID)
	if err != nil {
		return err
	}
	req, identifier, err := dimse.ReceiveCGetRequest(assoc, getPC.ID, transfer.ImplicitVRLittleEndian)
	if err != nil {
		return err
	}
	if identifier == nil {
		return errors.New("get server received nil identifier")
	}
	if err := dimse.SendCStoreRequest(assoc, storePC.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testMoveStorageSOPClassUID,
		MessageID:              21,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: testMoveSOPInstanceUID,
	}); err != nil {
		return err
	}
	if err := dimse.SendDataSet(assoc, storePC.ID, dataset, transfer.ImplicitVRLittleEndian); err != nil {
		return err
	}
	storeRsp, err := dimse.ReceiveCStoreResponse(assoc, storePC.ID)
	if err != nil {
		return err
	}
	if storeRsp.Status != dimse.StatusSuccess {
		return fmt.Errorf("C-STORE response status = 0x%04X, want success", storeRsp.Status)
	}
	completed := uint16(1)
	zero := uint16(0)
	if err := dimse.SendCGetResponse(assoc, getPC.ID, dimse.CGetResponse{
		AffectedSOPClassUID:                 dimse.StudyRootGetSOPClassUID,
		MessageIDBeingRespondedTo:           req.MessageID,
		Status:                              dimse.StatusSuccess,
		NumberOfRemainingSuboperationsOrNil: &zero,
		NumberOfCompletedSuboperationsOrNil: &completed,
		NumberOfFailedSuboperationsOrNil:    &zero,
		NumberOfWarningSuboperationsOrNil:   &zero,
	}); err != nil {
		return err
	}
	pdu, err := assoc.ReadPDU()
	if err != nil {
		return err
	}
	if _, ok := pdu.(*ul.ReleaseRQ); !ok {
		return errors.New("get server expected A-RELEASE-RQ")
	}
	return assoc.WritePDU(&ul.ReleaseRP{})
}

func receiverNode(t *testing.T, receiver *receive.Server) nodes.Node {
	t.Helper()
	_, portText, err := net.SplitHostPort(receiver.Addr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return nodes.Node{
		Name:    "receiver",
		AETitle: receiver.AETitle(),
		Host:    "127.0.0.1",
		Port:    uint16(port),
	}
}

func stopReceiver(t *testing.T, receiver *receive.Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := receiver.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func testMovePart10File(t *testing.T) []byte {
	t.Helper()
	dataset := testMoveDataSet(t)
	file := &object.File{
		Dataset:        dataset,
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testMoveDataSet(t *testing.T) *object.Object {
	t.Helper()
	return object.FromElements([]core.Element{
		stringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testMoveStorageSOPClassUID),
		stringElement(core.NewTag(0x0008, 0x0018), core.VRUI, testMoveSOPInstanceUID),
		stringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "MOVE^PATIENT"),
		stringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "M001"),
		stringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		stringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		stringElement(core.NewTag(0x0020, 0x000D), core.VRUI, testMoveStudyInstanceUID),
		stringElement(core.NewTag(0x0020, 0x000E), core.VRUI, testMoveSeriesInstanceUID),
	}, std.Dictionary)
}

func stringElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: vr},
		Value:  core.StringValue{value},
	}
}
