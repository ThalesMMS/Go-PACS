package receive

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/send"
	"github.com/ThalesMMS/Go-PACS/internal/testutil"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/pixeldata/codecfixture"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const (
	testStorageSOPClassUID     = "1.2.840.10008.5.1.4.1.1.2"
	testStudyInstanceUID       = "1.2.826.0.1.3680043.10.543.100"
	testSeriesInstanceUID      = "1.2.826.0.1.3680043.10.543.101"
	testSOPInstanceUID         = "1.2.826.0.1.3680043.10.543.102"
	testReceiverCallingAETitle = "STORESCU"
	testReceiverCalledAETitle  = "RECVSCP"
)

func TestServerReceivesCStoreIntoArchive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	node := receiverNode(t, server)
	outcome, err := send.SendFiles(ctx, node, []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 1 || outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want one successful send", outcome)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("len(studies) = %d, want 1", len(studies))
	}
	if studies[0].StudyInstanceUID != testStudyInstanceUID {
		t.Fatalf("StudyInstanceUID = %q, want %q", studies[0].StudyInstanceUID, testStudyInstanceUID)
	}

	instances, err := catalog.InstancesForStudy(ctx, testStudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].SourcePath != "dicom://"+testReceiverCallingAETitle+"/"+testSOPInstanceUID {
		t.Fatalf("SourcePath = %q", instances[0].SourcePath)
	}

	snapshot := server.Snapshot()
	if snapshot.Stored != 1 {
		t.Fatalf("Snapshot.Stored = %d, want 1", snapshot.Stored)
	}
	if snapshot.Failed != 0 {
		t.Fatalf("Snapshot.Failed = %d, want 0", snapshot.Failed)
	}
}

func TestServerServesStudyRootCFindFromArchive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	studyUID := "1.2.826.0.1.3680043.10.543.250"
	seriesUID := "1.2.826.0.1.3680043.10.543.251"
	sopUID := "1.2.826.0.1.3680043.10.543.252"
	source := filepath.Join(t.TempDir(), "find-source.dcm")
	if err := os.WriteFile(source, testFindPart10File(t, "FIND^LOCAL", "F001", "ACC-FIND", "CT", studyUID, seriesUID, sopUID, "7", "Axial", "3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	} else if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	node := receiverNode(t, server)
	studies, err := query.StudyRootFind(ctx, node, query.Criteria{
		PatientName:      "FIND*",
		PatientID:        "F001",
		StudyDateFrom:    "20260604",
		StudyDateTo:      "20260604",
		AccessionNumber:  "ACC-FIND",
		Modality:         "CT",
		StudyInstanceUID: studyUID,
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(studies.Matches) != 1 {
		t.Fatalf("study matches = %d, want 1", len(studies.Matches))
	}
	if studies.Matches[0].StudyInstanceUID != studyUID || studies.Matches[0].PatientID != "F001" {
		t.Fatalf("study match = %+v", studies.Matches[0])
	}

	series, err := query.StudyRootSeriesFind(ctx, node, query.SeriesCriteria{
		StudyInstanceUID:  studyUID,
		SeriesInstanceUID: seriesUID,
		Modality:          "CT",
		SeriesNumber:      "7",
		SeriesDescription: "Axial",
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Matches) != 1 {
		t.Fatalf("series matches = %d, want 1", len(series.Matches))
	}
	if series.Matches[0].SeriesInstanceUID != seriesUID || series.Matches[0].ImageCount != "1" {
		t.Fatalf("series match = %+v", series.Matches[0])
	}

	images, err := query.StudyRootImageFind(ctx, node, query.ImageCriteria{
		StudyInstanceUID:  studyUID,
		SeriesInstanceUID: seriesUID,
		SOPInstanceUID:    sopUID,
		SOPClassUID:       testStorageSOPClassUID,
		Modality:          "CT",
		InstanceNumber:    "3",
	}, "FINDSCU")
	if err != nil {
		t.Fatal(err)
	}
	if len(images.Matches) != 1 {
		t.Fatalf("image matches = %d, want 1", len(images.Matches))
	}
	if images.Matches[0].SOPInstanceUID != sopUID || images.Matches[0].InstanceNumber != "3" {
		t.Fatalf("image match = %+v", images.Matches[0])
	}
}

func TestServerServesStudyRootCMoveFromArchive(t *testing.T) {
	tests := []struct {
		name  string
		level string
		keys  map[string]string
	}{
		{
			name:  "study",
			level: dimse.QueryRetrieveLevelStudy,
			keys:  map[string]string{"StudyInstanceUID": testStudyInstanceUID},
		},
		{
			name:  "series",
			level: dimse.QueryRetrieveLevelSeries,
			keys: map[string]string{
				"StudyInstanceUID":  testStudyInstanceUID,
				"SeriesInstanceUID": testSeriesInstanceUID,
			},
		},
		{
			name:  "image",
			level: dimse.QueryRetrieveLevelImage,
			keys: map[string]string{
				"StudyInstanceUID":  testStudyInstanceUID,
				"SeriesInstanceUID": testSeriesInstanceUID,
				"SOPInstanceUID":    testSOPInstanceUID,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceCatalog, err := archive.Open(filepath.Join(t.TempDir(), "source-archive"))
			if err != nil {
				t.Fatal(err)
			}
			defer sourceCatalog.Close()
			source := filepath.Join(t.TempDir(), "move-source.dcm")
			if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
				t.Fatal(err)
			}
			if report, err := sourceCatalog.ImportPath(ctx, source); err != nil {
				t.Fatal(err)
			} else if report.StoredFiles != 1 {
				t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
			}

			destinationCatalog, err := archive.Open(filepath.Join(t.TempDir(), "destination-archive"))
			if err != nil {
				t.Fatal(err)
			}
			defer destinationCatalog.Close()
			destinationServer, err := Start(ctx, Config{
				Catalog: destinationCatalog,
				Address: "127.0.0.1:0",
				AETitle: "DESTAE",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stopServer(t, destinationServer)
			destinationNode := receiverNode(t, destinationServer)

			sourceServer, err := Start(ctx, Config{
				Catalog: sourceCatalog,
				Address: "127.0.0.1:0",
				AETitle: "MOVESCP",
				NodeLookup: func(aeTitle string) (nodes.Node, bool) {
					return nodes.FindByAETitle([]nodes.Node{destinationNode}, aeTitle)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stopServer(t, sourceServer)

			rsp := sendStudyRootCMove(t, ctx, sourceServer, "DESTAE", studyRootMoveIdentifier(t, test.level, test.keys))
			if rsp.Status != dimse.StatusSuccess {
				t.Fatalf("C-MOVE status = 0x%04X, want success", rsp.Status)
			}
			if cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil) != 1 || cMoveCount(rsp.NumberOfFailedSuboperationsOrNil) != 0 {
				t.Fatalf("C-MOVE counts completed=%d failed=%d, want completed=1 failed=0",
					cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil),
					cMoveCount(rsp.NumberOfFailedSuboperationsOrNil))
			}

			instances, err := destinationCatalog.InstancesForStudy(ctx, testStudyInstanceUID)
			if err != nil {
				t.Fatal(err)
			}
			if len(instances) != 1 || instances[0].SOPInstanceUID != testSOPInstanceUID {
				t.Fatalf("moved instances = %+v, want one moved SOPInstanceUID %s", instances, testSOPInstanceUID)
			}
		})
	}
}

func TestServerRejectsUnknownCMoveDestination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: "MOVESCP",
		NodeLookup: func(string) (nodes.Node, bool) {
			return nodes.Node{}, false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	rsp := sendStudyRootCMove(t, ctx, server, "MISSING", studyRootMoveIdentifier(t, dimse.QueryRetrieveLevelStudy, map[string]string{
		"StudyInstanceUID": testStudyInstanceUID,
	}))
	if rsp.Status != dimse.StatusCMoveMoveDestinationUnknown {
		t.Fatalf("C-MOVE status = 0x%04X, want destination unknown 0x%04X", rsp.Status, dimse.StatusCMoveMoveDestinationUnknown)
	}
}

func TestServerServesStudyRootCGetFromArchive(t *testing.T) {
	tests := []struct {
		name  string
		level string
		keys  map[string]string
	}{
		{
			name:  "study",
			level: dimse.QueryRetrieveLevelStudy,
			keys:  map[string]string{"StudyInstanceUID": testStudyInstanceUID},
		},
		{
			name:  "series",
			level: dimse.QueryRetrieveLevelSeries,
			keys: map[string]string{
				"StudyInstanceUID":  testStudyInstanceUID,
				"SeriesInstanceUID": testSeriesInstanceUID,
			},
		},
		{
			name:  "image",
			level: dimse.QueryRetrieveLevelImage,
			keys: map[string]string{
				"StudyInstanceUID":  testStudyInstanceUID,
				"SeriesInstanceUID": testSeriesInstanceUID,
				"SOPInstanceUID":    testSOPInstanceUID,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sourceCatalog, err := archive.Open(filepath.Join(t.TempDir(), "source-archive"))
			if err != nil {
				t.Fatal(err)
			}
			defer sourceCatalog.Close()
			source := filepath.Join(t.TempDir(), "get-source.dcm")
			if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
				t.Fatal(err)
			}
			if report, err := sourceCatalog.ImportPath(ctx, source); err != nil {
				t.Fatal(err)
			} else if report.StoredFiles != 1 {
				t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
			}

			destinationCatalog, err := archive.Open(filepath.Join(t.TempDir(), "destination-archive"))
			if err != nil {
				t.Fatal(err)
			}
			defer destinationCatalog.Close()

			server, err := Start(ctx, Config{
				Catalog: sourceCatalog,
				Address: "127.0.0.1:0",
				AETitle: "GETSCP",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stopServer(t, server)

			rsp := sendStudyRootCGet(t, ctx, server, studyRootMoveIdentifier(t, test.level, test.keys), importCGetStoreHandler(t, destinationCatalog))
			if rsp.Status != dimse.StatusSuccess {
				t.Fatalf("C-GET status = 0x%04X, want success", rsp.Status)
			}
			if cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil) != 1 || cMoveCount(rsp.NumberOfFailedSuboperationsOrNil) != 0 {
				t.Fatalf("C-GET counts completed=%d failed=%d, want completed=1 failed=0",
					cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil),
					cMoveCount(rsp.NumberOfFailedSuboperationsOrNil))
			}

			instances, err := destinationCatalog.InstancesForStudy(ctx, testStudyInstanceUID)
			if err != nil {
				t.Fatal(err)
			}
			if len(instances) != 1 || instances[0].SOPInstanceUID != testSOPInstanceUID {
				t.Fatalf("retrieved instances = %+v, want one retrieved SOPInstanceUID %s", instances, testSOPInstanceUID)
			}
		})
	}
}

func TestServerReportsCGetStoreFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	source := filepath.Join(t.TempDir(), "get-failure-source.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	} else if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: "GETSCP",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	rsp := sendStudyRootCGet(t, ctx, server, studyRootMoveIdentifier(t, dimse.QueryRetrieveLevelStudy, map[string]string{
		"StudyInstanceUID": testStudyInstanceUID,
	}), dimse.CGetStoreHandlerFunc(func(context.Context, dimse.CGetStoreRequestContext) (uint16, error) {
		return dimse.StatusCGetUnableToProcess, nil
	}))
	if rsp.Status != dimse.StatusCGetSubOperationsCompleteOneOrMoreFailures {
		t.Fatalf("C-GET status = 0x%04X, want failure-count warning 0x%04X", rsp.Status, dimse.StatusCGetSubOperationsCompleteOneOrMoreFailures)
	}
	if cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil) != 0 || cMoveCount(rsp.NumberOfFailedSuboperationsOrNil) != 1 {
		t.Fatalf("C-GET counts completed=%d failed=%d, want completed=0 failed=1",
			cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil),
			cMoveCount(rsp.NumberOfFailedSuboperationsOrNil))
	}
}

func TestServerHandlesSimultaneousAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sourceCatalog, err := archive.Open(filepath.Join(t.TempDir(), "source-archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceCatalog.Close()
	seedPath := filepath.Join(t.TempDir(), "seed.dcm")
	if err := os.WriteFile(seedPath, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := sourceCatalog.ImportPath(ctx, seedPath); err != nil {
		t.Fatal(err)
	} else if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}

	moveCatalog, err := archive.Open(filepath.Join(t.TempDir(), "move-destination"))
	if err != nil {
		t.Fatal(err)
	}
	defer moveCatalog.Close()
	moveDestination, err := Start(ctx, Config{
		Catalog: moveCatalog,
		Address: "127.0.0.1:0",
		AETitle: "MOVEDEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, moveDestination)
	moveNode := receiverNode(t, moveDestination)

	getCatalog, err := archive.Open(filepath.Join(t.TempDir(), "get-destination"))
	if err != nil {
		t.Fatal(err)
	}
	defer getCatalog.Close()

	server, err := Start(ctx, Config{
		Catalog:         sourceCatalog,
		Address:         "127.0.0.1:0",
		AETitle:         "QRSCP",
		MaxAssociations: 10,
		NodeLookup: func(aeTitle string) (nodes.Node, bool) {
			return nodes.FindByAETitle([]nodes.Node{moveNode}, aeTitle)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)
	sourceNode := receiverNode(t, server)

	first, firstPC, err := dialVerificationAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, secondPC, err := dialVerificationAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if rsp, err := dimse.SendCEcho(first, firstPC.ID, 1); err != nil || rsp.Status != dimse.StatusSuccess {
		t.Fatalf("first C-ECHO status=%v err=%v", rsp, err)
	}
	if rsp, err := dimse.SendCEcho(second, secondPC.ID, 2); err != nil || rsp.Status != dimse.StatusSuccess {
		t.Fatalf("second C-ECHO status=%v err=%v", rsp, err)
	}

	storePath := filepath.Join(t.TempDir(), "concurrent-store.dcm")
	if err := os.WriteFile(storePath, testFindPart10File(t, "CONCURRENT^STORE", "C001", "ACC-CONC", "MR", "1.2.826.0.1.3680043.10.543.710", "1.2.826.0.1.3680043.10.543.711", "1.2.826.0.1.3680043.10.543.712", "1", "Concurrent", "1"), 0o644); err != nil {
		t.Fatal(err)
	}
	moveIdentifier := studyRootMoveIdentifier(t, dimse.QueryRetrieveLevelSeries, map[string]string{
		"StudyInstanceUID":  testStudyInstanceUID,
		"SeriesInstanceUID": testSeriesInstanceUID,
	})
	getIdentifier := studyRootMoveIdentifier(t, dimse.QueryRetrieveLevelImage, map[string]string{
		"StudyInstanceUID":  testStudyInstanceUID,
		"SeriesInstanceUID": testSeriesInstanceUID,
		"SOPInstanceUID":    testSOPInstanceUID,
	})

	operations := []struct {
		name string
		run  func() error
	}{
		{
			name: "C-STORE",
			run: func() error {
				outcome, err := send.SendFiles(ctx, sourceNode, []string{storePath}, "STORESCU2")
				if err != nil {
					return err
				}
				if outcome.Sent != 1 || outcome.Failed != 0 {
					return fmt.Errorf("C-STORE outcome = %#v, want one sent", outcome)
				}
				return nil
			},
		},
		{
			name: "C-FIND",
			run: func() error {
				result, err := query.StudyRootFind(ctx, sourceNode, query.Criteria{StudyInstanceUID: testStudyInstanceUID}, "FINDSCU")
				if err != nil {
					return err
				}
				if len(result.Matches) != 1 || result.Matches[0].StudyInstanceUID != testStudyInstanceUID {
					return fmt.Errorf("C-FIND matches = %+v, want seeded study", result.Matches)
				}
				return nil
			},
		},
		{
			name: "C-MOVE",
			run: func() error {
				rsp, err := performStudyRootCMove(ctx, server, moveNode.AETitle, moveIdentifier)
				if err != nil {
					return err
				}
				if rsp.Status != dimse.StatusSuccess || cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil) != 1 {
					return fmt.Errorf("C-MOVE response = %+v, want one completed success", rsp)
				}
				return nil
			},
		},
		{
			name: "C-GET",
			run: func() error {
				rsp, err := performStudyRootCGet(ctx, server, getIdentifier, catalogCGetStoreHandler(getCatalog))
				if err != nil {
					return err
				}
				if rsp.Status != dimse.StatusSuccess || cMoveCount(rsp.NumberOfCompletedSuboperationsOrNil) != 1 {
					return fmt.Errorf("C-GET response = %+v, want one completed success", rsp)
				}
				return nil
			},
		},
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(operations))
	for _, op := range operations {
		op := op
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := op.run(); err != nil {
				errs <- fmt.Errorf("%s: %w", op.name, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	releaseCtx, releaseCancel := context.WithTimeout(ctx, time.Second)
	defer releaseCancel()
	if err := first.Release(releaseCtx); err != nil {
		t.Fatal(err)
	}
	if err := second.Release(releaseCtx); err != nil {
		t.Fatal(err)
	}

	snapshot := server.Snapshot()
	if snapshot.Associations < int64(2+len(operations)) {
		t.Fatalf("Associations = %d, want at least %d", snapshot.Associations, 2+len(operations))
	}
	if snapshot.Stored != 1 || snapshot.Rejected != 0 || snapshot.Failed != 0 {
		t.Fatalf("snapshot = %+v, want Stored=1 Rejected=0 Failed=0", snapshot)
	}
	moveInstances, err := moveCatalog.InstancesForStudy(ctx, testStudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(moveInstances) != 1 {
		t.Fatalf("C-MOVE destination instances = %d, want 1", len(moveInstances))
	}
	getInstances, err := getCatalog.InstancesForStudy(ctx, testStudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(getInstances) != 1 {
		t.Fatalf("C-GET destination instances = %d, want 1", len(getInstances))
	}
}

func TestServerDecompressesIncomingCompressedImages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:          catalog,
		Address:          "127.0.0.1:0",
		AETitle:          testReceiverCalledAETitle,
		DecompressImages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	data, wantPixels := testCompressedPart10File(t)
	source := filepath.Join(t.TempDir(), "compressed.dcm")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want one successful compressed send", outcome)
	}

	instances, err := catalog.InstancesForStudy(ctx, testStudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].TransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("TransferSyntaxUID = %q, want %q", instances[0].TransferSyntaxUID, transfer.ExplicitVRLittleEndian.UID)
	}
	storedFile, err := os.Open(instances[0].StoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer storedFile.Close()
	stored, err := object.ReadFile(storedFile)
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := stored.GetRaw(core.TagPixelData); !ok || !bytes.Equal(raw, wantPixels) {
		t.Fatalf("stored PixelData = %v, want %v", raw, wantPixels)
	}
}

func TestServerRejectsOversizedCStoreObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:             catalog,
		Address:             "127.0.0.1:0",
		AETitle:             testReceiverCalledAETitle,
		MaxStoreObjectBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 || outcome.Failed != 1 {
		t.Fatalf("outcome = %#v, want one failed send", outcome)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("len(studies) = %d, want 0", len(studies))
	}

	snapshot := server.Snapshot()
	if snapshot.Stored != 0 {
		t.Fatalf("Snapshot.Stored = %d, want 0", snapshot.Stored)
	}
	if snapshot.Failed != 1 {
		t.Fatalf("Snapshot.Failed = %d, want 1", snapshot.Failed)
	}
}

func TestServerDrainsMalformedCStoreDataSetBeforeNextCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	assoc, pc, err := dialStorageAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer assoc.Close()

	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testStorageSOPClassUID,
		MessageID:              1,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: "1.2.826.0.1.3680043.10.543.199",
	}); err != nil {
		t.Fatalf("SendCStoreRequest(malformed) error = %v", err)
	}
	if err := sendMalformedExplicitVRDataSet(assoc, pc.ID); err != nil {
		t.Fatalf("sendMalformedExplicitVRDataSet() error = %v", err)
	}
	malformedRsp, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		t.Fatalf("ReceiveCStoreResponse(malformed) error = %v", err)
	}
	if malformedRsp.Status != dimse.StatusCStoreCannotUnderstand {
		t.Fatalf("malformed C-STORE status = 0x%04X, want 0x%04X", malformedRsp.Status, dimse.StatusCStoreCannotUnderstand)
	}

	validFile, err := object.ReadFile(bytes.NewReader(testPart10File(t)))
	if err != nil {
		t.Fatalf("ReadFile(valid) error = %v", err)
	}
	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testStorageSOPClassUID,
		MessageID:              2,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: testSOPInstanceUID,
	}); err != nil {
		t.Fatalf("SendCStoreRequest(valid) error = %v", err)
	}
	if err := dimse.SendDataSet(assoc, pc.ID, validFile.Dataset, transfer.ExplicitVRLittleEndian); err != nil {
		t.Fatalf("SendDataSet(valid) error = %v", err)
	}
	validRsp, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		t.Fatalf("ReceiveCStoreResponse(valid) error = %v", err)
	}
	if validRsp.Status != dimse.StatusSuccess {
		t.Fatalf("valid C-STORE status = 0x%04X, want success", validRsp.Status)
	}

	snapshot := server.Snapshot()
	if snapshot.Stored != 1 {
		t.Fatalf("Snapshot.Stored = %d, want 1", snapshot.Stored)
	}
	if snapshot.Failed != 1 {
		t.Fatalf("Snapshot.Failed = %d, want 1", snapshot.Failed)
	}
}

func TestServerCountsMalformedCStoreFailureOnceAfterAssociationClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	assoc, pc, err := dialStorageAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testStorageSOPClassUID,
		MessageID:              1,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: "1.2.826.0.1.3680043.10.543.200",
	}); err != nil {
		t.Fatalf("SendCStoreRequest(malformed) error = %v", err)
	}
	if err := sendMalformedExplicitVRDataSet(assoc, pc.ID); err != nil {
		t.Fatalf("sendMalformedExplicitVRDataSet() error = %v", err)
	}
	rsp, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		t.Fatalf("ReceiveCStoreResponse(malformed) error = %v", err)
	}
	if rsp.Status != dimse.StatusCStoreCannotUnderstand {
		t.Fatalf("malformed C-STORE status = 0x%04X, want 0x%04X", rsp.Status, dimse.StatusCStoreCannotUnderstand)
	}
	if err := assoc.Close(); err != nil {
		t.Fatalf("close association: %v", err)
	}
	waitForAssociationHandlers(t, server)

	snapshot := server.Snapshot()
	if snapshot.Stored != 0 {
		t.Fatalf("Snapshot.Stored = %d, want 0", snapshot.Stored)
	}
	if snapshot.Failed != 1 {
		t.Fatalf("Snapshot.Failed = %d, want 1", snapshot.Failed)
	}
}

func TestServerAnswersCEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	result, err := netverify.Echo(ctx, receiverNode(t, server), testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 {
		t.Fatalf("Status = 0x%04X, want success", result.Status)
	}
}

func TestServerAnswersCEchoOverTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:   catalog,
		Address:   "127.0.0.1:0",
		AETitle:   testReceiverCalledAETitle,
		TLSConfig: testReceiveServerTLSConfig(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	node := receiverNode(t, server)
	node.UseTLS = true
	node.TLSSkipVerify = true
	result, err := netverify.Echo(ctx, node, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 {
		t.Fatalf("Status = 0x%04X, want success", result.Status)
	}
}

func TestStartRejectsNegativeMaxAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:         catalog,
		Address:         "127.0.0.1:0",
		AETitle:         testReceiverCalledAETitle,
		MaxAssociations: -1,
	})
	if err == nil {
		stopServer(t, server)
		t.Fatal("Start succeeded with negative MaxAssociations")
	}
}

func TestServerLimitsConcurrentAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:         catalog,
		Address:         "127.0.0.1:0",
		AETitle:         testReceiverCalledAETitle,
		MaxAssociations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	first, _, err := dialVerificationAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	secondCtx, cancelSecond := context.WithTimeout(ctx, time.Second)
	second, secondPC, err := dialVerificationAssociation(secondCtx, server)
	cancelSecond()
	if err == nil {
		_, echoErr := dimse.SendCEcho(second, secondPC.ID, 1)
		_ = second.Close()
		if echoErr == nil {
			t.Fatal("second association processed C-ECHO while max associations slot was full")
		}
	}
	waitForRejectedAssociations(t, server, 1)

	releaseCtx, cancelRelease := context.WithTimeout(ctx, time.Second)
	defer cancelRelease()
	if err := first.Release(releaseCtx); err != nil {
		t.Fatal(err)
	}

	result, err := netverify.Echo(ctx, receiverNode(t, server), testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 {
		t.Fatalf("Status after releasing slot = 0x%04X, want success", result.Status)
	}
}

func TestServerRejectsCStoreFromDisallowedCallingAE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:                catalog,
		Address:                "127.0.0.1:0",
		AETitle:                testReceiverCalledAETitle,
		AllowedCallingAETitles: []string{"TRUSTED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, "BLOCKED")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 {
		t.Fatalf("Sent = %d, want 0", outcome.Sent)
	}
	if outcome.Failed != 1 {
		t.Fatalf("Failed = %d, want 1 (%v)", outcome.Failed, outcome.Failures)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("len(studies) = %d, want 0", len(studies))
	}
	snapshot := server.Snapshot()
	if snapshot.Rejected != 1 {
		t.Fatalf("Snapshot.Rejected = %d, want 1", snapshot.Rejected)
	}
	if snapshot.Stored != 0 {
		t.Fatalf("Snapshot.Stored = %d, want 0", snapshot.Stored)
	}
}

func TestServerRejectsCStoreFromDisallowedRemoteHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:            catalog,
		Address:            "127.0.0.1:0",
		AETitle:            testReceiverCalledAETitle,
		AllowedRemoteHosts: []string{"192.0.2.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 {
		t.Fatalf("Sent = %d, want 0", outcome.Sent)
	}
	if outcome.Failed != 1 {
		t.Fatalf("Failed = %d, want 1 (%v)", outcome.Failed, outcome.Failures)
	}
	snapshot := server.Snapshot()
	if snapshot.Rejected != 1 {
		t.Fatalf("Snapshot.Rejected = %d, want 1", snapshot.Rejected)
	}
}

func TestServerAcceptsConfiguredCalledAEAlias(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:               catalog,
		Address:               "127.0.0.1:0",
		AETitle:               testReceiverCalledAETitle,
		AllowedCalledAETitles: []string{testReceiverCalledAETitle, "ALIAS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	node := receiverNode(t, server)
	node.AETitle = "ALIAS"
	outcome, err := send.SendFiles(ctx, node, []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want one successful send", outcome)
	}
	snapshot := server.Snapshot()
	if snapshot.Stored != 1 {
		t.Fatalf("Snapshot.Stored = %d, want 1", snapshot.Stored)
	}
}

func TestServerRejectsUnlistedCalledAE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:               catalog,
		Address:               "127.0.0.1:0",
		AETitle:               testReceiverCalledAETitle,
		AllowedCalledAETitles: []string{testReceiverCalledAETitle},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	node := receiverNode(t, server)
	node.AETitle = "OTHER"
	outcome, err := send.SendFiles(ctx, node, []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 || outcome.Failed != 1 {
		t.Fatalf("outcome = %#v, want one failed send", outcome)
	}
	snapshot := server.Snapshot()
	if snapshot.Rejected != 1 {
		t.Fatalf("Snapshot.Rejected = %d, want 1", snapshot.Rejected)
	}
}

func TestTransferSyntaxUIDsForPreferenceOrdersSupportedSyntaxes(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		want       []string
	}{
		{
			name:       "auto keeps implicit first",
			preference: PreferredTransferSyntaxAuto,
			want:       []string{transfer.ImplicitVRLittleEndian.UID, transfer.ExplicitVRLittleEndian.UID},
		},
		{
			name:       "explicit first",
			preference: PreferredTransferSyntaxExplicitVRLittleEndian,
			want:       []string{transfer.ExplicitVRLittleEndian.UID, transfer.ImplicitVRLittleEndian.UID},
		},
		{
			name:       "implicit first",
			preference: PreferredTransferSyntaxImplicitVRLittleEndian,
			want:       []string{transfer.ImplicitVRLittleEndian.UID, transfer.ExplicitVRLittleEndian.UID},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TransferSyntaxUIDsForPreference(test.preference)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) < len(test.want) {
				t.Fatalf("len(got) = %d, want at least %d (%#v)", len(got), len(test.want), got)
			}
			for i := range test.want {
				if got[i] != test.want[i] {
					t.Fatalf("syntax prefix = %#v, want %#v", got[:len(test.want)], test.want)
				}
			}
		})
	}
}

func TestTransferSyntaxUIDsForPreferenceRejectsUnsupportedSyntax(t *testing.T) {
	if _, err := TransferSyntaxUIDsForPreference(transfer.JPEG2000.UID); err == nil {
		t.Fatal("raw transfer syntax UID should not be accepted as a preference")
	}
}

func receiverNode(t *testing.T, server *Server) nodes.Node {
	t.Helper()
	_, portText, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return nodes.Node{
		Name:    "receiver",
		AETitle: server.AETitle(),
		Host:    "127.0.0.1",
		Port:    uint16(port),
	}
}

func stopServer(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func dialVerificationAssociation(ctx context.Context, server *Server) (*ul.Association, ul.AcceptedContext, error) {
	assoc, err := ul.DialContext(ctx, server.Addr(), ul.DialOptions{
		CalledAETitle:  server.AETitle(),
		CallingAETitle: testReceiverCallingAETitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  dimse.VerificationSOPClassUID,
			TransferSyntaxUIDs: []string{ul.ImplicitVRLittleEndian},
		}},
	})
	if err != nil {
		return nil, ul.AcceptedContext{}, err
	}
	pc, ok := dimse.AcceptedVerificationContext(assoc)
	if !ok {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, errors.New("verification presentation context was not accepted")
	}
	return assoc, pc, nil
}

func dialStorageAssociation(ctx context.Context, server *Server) (*ul.Association, ul.AcceptedContext, error) {
	assoc, err := ul.DialContext(ctx, server.Addr(), ul.DialOptions{
		CalledAETitle:  server.AETitle(),
		CallingAETitle: testReceiverCallingAETitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  testStorageSOPClassUID,
			TransferSyntaxUIDs: []string{transfer.ExplicitVRLittleEndian.UID},
		}},
	})
	if err != nil {
		return nil, ul.AcceptedContext{}, err
	}
	pc, err := dimse.AcceptedContextForSOPClass(assoc, testStorageSOPClassUID)
	if err != nil {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, err
	}
	return assoc, pc, nil
}

func sendStudyRootCMove(t *testing.T, ctx context.Context, server *Server, moveDestination string, identifier *object.Object) *dimse.CMoveResponse {
	t.Helper()
	rsp, err := performStudyRootCMove(ctx, server, moveDestination, identifier)
	if err != nil {
		t.Fatal(err)
	}
	return rsp
}

func performStudyRootCMove(ctx context.Context, server *Server, moveDestination string, identifier *object.Object) (*dimse.CMoveResponse, error) {
	assoc, pc, syntax, err := dialMoveAssociation(ctx, server)
	if err != nil {
		return nil, err
	}
	defer assoc.Close()

	rsp, err := dimse.SendCMove(ctx, assoc, pc.ID, dimse.CMoveRequest{
		AffectedSOPClassUID: dimse.StudyRootMoveSOPClassUID,
		MessageID:           1,
		Priority:            dimse.PriorityMedium,
		MoveDestination:     moveDestination,
	}, identifier, syntax)
	if err != nil {
		return nil, err
	}
	releaseCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := assoc.Release(releaseCtx); err != nil {
		return nil, err
	}
	return rsp, nil
}

func dialMoveAssociation(ctx context.Context, server *Server) (*ul.Association, ul.AcceptedContext, transfer.Syntax, error) {
	assoc, err := ul.DialContext(ctx, server.Addr(), ul.DialOptions{
		CalledAETitle:  server.AETitle(),
		CallingAETitle: "MOVESCU",
		Contexts:       []ul.PresentationContext{dimse.StudyRootMovePresentationContext()},
	})
	if err != nil {
		return nil, ul.AcceptedContext{}, transfer.Syntax{}, err
	}
	pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
	if err != nil {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, transfer.Syntax{}, err
	}
	syntax, err := dimse.TransferSyntaxForAcceptedContext(pc)
	if err != nil {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, transfer.Syntax{}, err
	}
	return assoc, pc, syntax, nil
}

func sendStudyRootCGet(t *testing.T, ctx context.Context, server *Server, identifier *object.Object, storeHandler dimse.CGetStoreHandler) *dimse.CGetResponse {
	t.Helper()
	rsp, err := performStudyRootCGet(ctx, server, identifier, storeHandler)
	if err != nil {
		t.Fatal(err)
	}
	return rsp
}

func performStudyRootCGet(ctx context.Context, server *Server, identifier *object.Object, storeHandler dimse.CGetStoreHandler) (*dimse.CGetResponse, error) {
	assoc, pc, syntax, err := dialGetAssociation(ctx, server)
	if err != nil {
		return nil, err
	}
	defer assoc.Close()

	rsp, err := dimse.SendCGet(ctx, assoc, pc.ID, dimse.CGetRequest{
		AffectedSOPClassUID: dimse.StudyRootGetSOPClassUID,
		MessageID:           1,
		Priority:            dimse.PriorityMedium,
	}, identifier, syntax, storeHandler)
	if err != nil {
		return nil, err
	}
	releaseCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := assoc.Release(releaseCtx); err != nil {
		return nil, err
	}
	return rsp, nil
}

func dialGetAssociation(ctx context.Context, server *Server) (*ul.Association, ul.AcceptedContext, transfer.Syntax, error) {
	contexts, roles := cGetTestPresentationContexts()
	assoc, err := ul.DialContext(ctx, server.Addr(), ul.DialOptions{
		CalledAETitle:  server.AETitle(),
		CallingAETitle: "GETSCU",
		Contexts:       contexts,
		RoleSelections: roles,
	})
	if err != nil {
		return nil, ul.AcceptedContext{}, transfer.Syntax{}, err
	}
	pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootGetSOPClassUID)
	if err != nil {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, transfer.Syntax{}, err
	}
	syntax, err := dimse.TransferSyntaxForAcceptedContext(pc)
	if err != nil {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, transfer.Syntax{}, err
	}
	return assoc, pc, syntax, nil
}

func cGetTestPresentationContexts() ([]ul.PresentationContext, []ul.RoleSelectionItem) {
	contexts := []ul.PresentationContext{
		dimse.StudyRootGetPresentationContext(),
		{
			AbstractSyntaxUID:  testStorageSOPClassUID,
			TransferSyntaxUIDs: []string{ul.ImplicitVRLittleEndian, ul.ExplicitVRLittleEndian},
		},
	}
	roles := []ul.RoleSelectionItem{
		{SopClassUID: testStorageSOPClassUID, SCPRole: true},
	}
	return contexts, roles
}

func importCGetStoreHandler(t *testing.T, catalog *archive.Catalog) dimse.CGetStoreHandler {
	t.Helper()
	return catalogCGetStoreHandler(catalog)
}

func catalogCGetStoreHandler(catalog *archive.Catalog) dimse.CGetStoreHandler {
	return dimse.CGetStoreHandlerFunc(func(ctx context.Context, req dimse.CGetStoreRequestContext) (uint16, error) {
		report, err := catalog.ImportObject(ctx, "dicom://GETSCU/"+req.Request.AffectedSOPInstanceUID, req.DataSet, req.DataSetSyntax)
		if err != nil {
			return dimse.StatusCGetUnableToProcess, err
		}
		if report.InvalidFiles > 0 || len(report.Rejections) > 0 {
			return dimse.StatusCGetUnableToProcess, fmt.Errorf("archive rejected C-GET object")
		}
		return dimse.StatusSuccess, nil
	})
}

func studyRootMoveIdentifier(t *testing.T, level string, keys map[string]string) *object.Object {
	t.Helper()
	var (
		elems []core.Element
		err   error
	)
	switch level {
	case dimse.QueryRetrieveLevelStudy:
		elems, err = dimse.BuildStudyRootStudyFindKeys(keys)
	case dimse.QueryRetrieveLevelSeries:
		elems, err = dimse.BuildStudyRootSeriesFindKeys(keys)
	case dimse.QueryRetrieveLevelImage:
		elems, err = dimse.BuildStudyRootImageFindKeys(keys)
	default:
		t.Fatalf("unsupported move level %q", level)
	}
	if err != nil {
		t.Fatal(err)
	}
	return object.FromElements(elems, std.Dictionary)
}

func cMoveCount(value *uint16) uint16 {
	if value == nil {
		return 0
	}
	return *value
}

func sendMalformedExplicitVRDataSet(assoc *ul.Association, pcID byte) error {
	firstPDV := []byte{
		0x10, 0x00, 0x10, 0x00,
		'Z', 'Z',
		0x04, 0x00,
		'T', 'E', 'S', 'T',
	}
	if err := assoc.WritePDU(&ul.PDataTF{Values: []ul.PDataValue{{
		PresentationContextID: pcID,
		IsCommand:             false,
		IsLast:                false,
		Data:                  firstPDV,
	}}}); err != nil {
		return err
	}
	return assoc.WritePDU(&ul.PDataTF{Values: []ul.PDataValue{{
		PresentationContextID: pcID,
		IsCommand:             false,
		IsLast:                true,
		Data:                  []byte{0xde, 0xad, 0xbe, 0xef},
	}}})
}

func waitForRejectedAssociations(t *testing.T, server *Server, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := server.Snapshot().Rejected; got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Snapshot.Rejected = %d, want at least %d", server.Snapshot().Rejected, want)
}

func waitForAssociationHandlers(t *testing.T, server *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		server.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for association handlers")
	}
}

func testReceiveServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("rand.Int() error = %v", err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() error = %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

func testPart10File(t *testing.T) []byte {
	t.Helper()
	dataset := []core.Element{
		testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testStorageSOPClassUID),
		testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, testSOPInstanceUID),
		testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "RECEIVE^PATIENT"),
		testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "R001"),
		testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, testStudyInstanceUID),
		testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, testSeriesInstanceUID),
	}
	file := &object.File{
		Dataset:        object.FromElements(dataset, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testFindPart10File(t *testing.T, patientName, patientID, accession, modality, studyUID, seriesUID, sopUID, seriesNumber, seriesDescription, instanceNumber string) []byte {
	t.Helper()
	dataset := []core.Element{
		testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testStorageSOPClassUID),
		testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
		testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, patientName),
		testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, patientID),
		testutil.StringElement(core.NewTag(0x0010, 0x0030), core.VRDA, "19700102"),
		testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		testutil.StringElement(core.NewTag(0x0008, 0x0030), core.VRTM, "134501"),
		testutil.StringElement(core.NewTag(0x0008, 0x0050), core.VRSH, accession),
		testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, modality),
		testutil.StringElement(core.NewTag(0x0008, 0x1030), core.VRLO, "Find study"),
		testutil.StringElement(core.NewTag(0x0008, 0x103E), core.VRLO, seriesDescription),
		testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
		testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
		testutil.StringElement(core.NewTag(0x0020, 0x0011), core.VRIS, seriesNumber),
		testutil.StringElement(core.NewTag(0x0020, 0x0013), core.VRIS, instanceNumber),
	}
	file := &object.File{
		Dataset:        object.FromElements(dataset, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testCompressedPart10File(t *testing.T) ([]byte, []byte) {
	t.Helper()
	tc := codecfixture.JPEGLosslessSmall()
	replaced := map[core.Tag]bool{
		core.NewTag(0x0008, 0x0016): true,
		core.NewTag(0x0008, 0x0018): true,
		core.NewTag(0x0010, 0x0010): true,
		core.NewTag(0x0010, 0x0020): true,
		core.NewTag(0x0008, 0x0020): true,
		core.NewTag(0x0008, 0x0060): true,
		core.NewTag(0x0020, 0x000D): true,
		core.NewTag(0x0020, 0x000E): true,
	}
	elements := make([]core.Element, 0, len(tc.Elements)+len(replaced))
	for _, element := range tc.Elements {
		if !replaced[element.Tag()] {
			elements = append(elements, element)
		}
	}
	elements = append(elements,
		testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testStorageSOPClassUID),
		testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, testSOPInstanceUID),
		testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "RECEIVE^COMPRESSED"),
		testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "RC001"),
		testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, testStudyInstanceUID),
		testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, testSeriesInstanceUID),
	)
	file := &object.File{
		Dataset:        object.FromElements(elements, std.Dictionary),
		TransferSyntax: tc.Syntax,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), tc.ExpectedFrames[0]
}
