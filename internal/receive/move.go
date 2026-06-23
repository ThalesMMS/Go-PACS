package receive

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

func (s *Server) moveCMove(ctx context.Context, req dimse.CMoveRequestContext) ([]dimse.CMoveSubOperation, error) {
	destinationAE := nodes.NormalizeAETitle(req.Request.MoveDestination)
	node, ok := s.nodeLookup(destinationAE)
	if !ok {
		return nil, dimse.NewCMoveSCPError(dimse.StatusCMoveMoveDestinationUnknown, fmt.Sprintf("move destination %q unknown", destinationAE), nil)
	}

	instances, err := s.instancesForCMove(ctx, req)
	if err != nil {
		return nil, err
	}
	ops := make([]dimse.CMoveSubOperation, 0, len(instances))
	for _, inst := range instances {
		inst := inst
		ops = append(ops, dimse.CMoveSubOperation{
			AffectedSOPClassUID:    inst.SOPClassUID,
			AffectedSOPInstanceUID: inst.SOPInstanceUID,
			Store: func(ctx context.Context) dimse.CMoveSubOperationResult {
				return s.storeMoveInstance(ctx, node, inst, req.Request.MessageID, req.Request.Priority)
			},
		})
	}
	return ops, nil
}

func (s *Server) instancesForCMove(ctx context.Context, req dimse.CMoveRequestContext) ([]archive.Instance, error) {
	return s.instancesForRetrieve(ctx, req.QueryRetrieveLevel, req.Identifier, "C-MOVE")
}

func (s *Server) instancesForRetrieve(ctx context.Context, level string, identifier *object.Object, operation string) ([]archive.Instance, error) {
	switch level {
	case dimse.QueryRetrieveLevelStudy:
		studyUID := findCriterion(identifier, findTagStudyInstanceUID)
		if studyUID == "" {
			return nil, fmt.Errorf("StudyInstanceUID is required for STUDY %s", operation)
		}
		return s.catalog.InstancesForStudy(ctx, studyUID)
	case dimse.QueryRetrieveLevelSeries:
		seriesUID := findCriterion(identifier, findTagSeriesInstanceUID)
		if seriesUID == "" {
			return nil, fmt.Errorf("SeriesInstanceUID is required for SERIES %s", operation)
		}
		instances, err := s.catalog.InstancesForSeries(ctx, seriesUID)
		if err != nil {
			return nil, err
		}
		return filterRetrieveInstances(instances, identifier), nil
	case dimse.QueryRetrieveLevelImage:
		sopUID := findCriterion(identifier, findTagSOPInstanceUID)
		if sopUID == "" {
			return nil, fmt.Errorf("SOPInstanceUID is required for IMAGE %s", operation)
		}
		inst, err := s.catalog.InstanceBySOPInstanceUID(ctx, sopUID)
		if err != nil {
			return nil, err
		}
		if !instanceMatchesFind(inst, identifier) {
			return nil, nil
		}
		return []archive.Instance{inst}, nil
	default:
		return nil, fmt.Errorf("unsupported QueryRetrieveLevel %q", level)
	}
}

func filterRetrieveInstances(instances []archive.Instance, identifier *object.Object) []archive.Instance {
	out := instances[:0]
	for _, inst := range instances {
		if instanceMatchesFind(inst, identifier) {
			out = append(out, inst)
		}
	}
	return out
}

func (s *Server) storeMoveInstance(ctx context.Context, node nodes.Node, inst archive.Instance, originatorMessageID, priority uint16) dimse.CMoveSubOperationResult {
	file, err := object.OpenFile(inst.StoredPath)
	if err != nil {
		return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
	}
	defer file.Close()
	if file.Dataset == nil {
		return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: fmt.Errorf("%s: DICOM file has no dataset", inst.StoredPath)}
	}

	tlsConfig, err := netverify.TLSConfigForNode(node)
	if err != nil {
		return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
	}
	transferSyntaxUID := inst.TransferSyntaxUID
	if transferSyntaxUID == "" {
		transferSyntaxUID = file.TransferSyntax.UID
	}
	// ponytail: one storage association per instance; batch per destination if C-MOVE throughput matters.
	assoc, err := ul.DialContext(ctx, net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port))), ul.DialOptions{
		CalledAETitle:  nodes.NormalizeAETitle(node.AETitle),
		CallingAETitle: s.aeTitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  inst.SOPClassUID,
			TransferSyntaxUIDs: transfer.ProposedStoreTransferSyntaxUIDs(transferSyntaxUID, transfer.NativeStoreSourceFirst),
		}},
		TLSConfig: tlsConfig,
	})
	if err != nil {
		return dimse.CMoveSubOperationResult{Status: dimse.StatusCMoveUnableToProcess, Err: err}
	}
	defer assoc.Close()

	result, err := dimse.NewStoreClient(assoc).StoreWithOptions(ctx, file.Dataset, dimse.CStoreOptions{
		AffectedSOPClassUID:          inst.SOPClassUID,
		AffectedSOPInstanceUID:       inst.SOPInstanceUID,
		Priority:                     priority,
		MoveOriginatorMessageIDOrNil: &originatorMessageID,
	})
	status := dimse.StatusSuccess
	if result.Response != nil {
		status = result.Response.Status
	}
	_ = assoc.Release(ctx)
	return dimse.CMoveSubOperationResult{Status: status, Err: err}
}
