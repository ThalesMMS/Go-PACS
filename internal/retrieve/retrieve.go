package retrieve

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nettimeout"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
)

const DefaultTimeout = 60 * time.Second

const (
	MethodMove = "C-MOVE"
	MethodGet  = "C-GET"
)

var (
	tagStudyInstanceUID  = core.NewTag(0x0020, 0x000D)
	tagSeriesInstanceUID = core.NewTag(0x0020, 0x000E)
	tagSOPInstanceUID    = core.NewTag(0x0008, 0x0018)
)

type Options struct {
	CallingAETitle      string
	Method              string
	MoveDestination     string
	ReceiveAddress      string
	Receiver            *receive.Server
	MaxStoreObjectBytes int64
	OnProgress          func(Progress)
}

type Progress struct {
	FinalStatus uint16
	StatusClass dimse.CMoveStatus
	Remaining   uint16
	Completed   uint16
	Failed      uint16
	Warnings    uint16
}

type Outcome struct {
	Method      string
	FinalStatus uint16
	StatusClass dimse.CMoveStatus
	Remaining   uint16
	Completed   uint16
	Failed      uint16
	Warnings    uint16
	Stored      int64
	Duplicates  int64
	Receiver    receive.Snapshot
	Progress    []Progress
	Duration    time.Duration
}

func RetrieveStudy(ctx context.Context, catalog *archive.Catalog, node nodes.Node, studyInstanceUID string, opts Options) (Outcome, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" || studyInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("study instance UID is required")
	}
	identifier, err := studyRootStudyMoveIdentifier(studyInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return retrieveWithIdentifier(ctx, catalog, node, identifier, opts)
}

func RetrieveSeries(ctx context.Context, catalog *archive.Catalog, node nodes.Node, studyInstanceUID, seriesInstanceUID string, opts Options) (Outcome, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" || studyInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("study instance UID is required")
	}
	seriesInstanceUID = strings.TrimSpace(seriesInstanceUID)
	if seriesInstanceUID == "" || seriesInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("series instance UID is required")
	}
	identifier, err := studyRootSeriesMoveIdentifier(studyInstanceUID, seriesInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return retrieveWithIdentifier(ctx, catalog, node, identifier, opts)
}

func RetrieveImage(ctx context.Context, catalog *archive.Catalog, node nodes.Node, studyInstanceUID, seriesInstanceUID, sopInstanceUID string, opts Options) (Outcome, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" || studyInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("study instance UID is required")
	}
	seriesInstanceUID = strings.TrimSpace(seriesInstanceUID)
	if seriesInstanceUID == "" || seriesInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("series instance UID is required")
	}
	sopInstanceUID = strings.TrimSpace(sopInstanceUID)
	if sopInstanceUID == "" || sopInstanceUID == "(missing)" {
		return Outcome{}, fmt.Errorf("SOP instance UID is required")
	}
	identifier, err := studyRootImageMoveIdentifier(studyInstanceUID, seriesInstanceUID, sopInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return retrieveWithIdentifier(ctx, catalog, node, identifier, opts)
}

func retrieveWithIdentifier(ctx context.Context, catalog *archive.Catalog, node nodes.Node, identifier *object.Object, opts Options) (Outcome, error) {
	if catalog == nil && opts.Receiver == nil {
		return Outcome{}, fmt.Errorf("archive catalog is required")
	}
	if opts.CallingAETitle == "" {
		opts.CallingAETitle = netverify.DefaultCallingAETitle
	}
	opts.CallingAETitle = nodes.NormalizeAETitle(opts.CallingAETitle)
	if err := nodes.ValidateAETitle(opts.CallingAETitle); err != nil {
		return Outcome{}, fmt.Errorf("calling AE title: %w", err)
	}
	method, err := retrieveMethodPreference(opts)
	if err != nil {
		return Outcome{}, err
	}
	if method == MethodGet {
		return retrieveWithGet(ctx, catalog, node, identifier, opts)
	}

	receiver, stopReceiver, err := ensureReceiver(ctx, catalog, opts, []string{node.AETitle}, remoteHostAllowlist(node.Host))
	if err != nil {
		return Outcome{}, err
	}
	defer func() {
		if stopReceiver {
			stopCtx, cancel := context.WithTimeout(context.Background(), netverify.DefaultReleaseTimeout)
			defer cancel()
			_ = receiver.Stop(stopCtx)
		}
	}()
	moveDestination := opts.MoveDestination
	if moveDestination == "" {
		moveDestination = receiver.AETitle()
	}
	moveDestination = nodes.NormalizeAETitle(moveDestination)
	if err := nodes.ValidateAETitle(moveDestination); err != nil {
		return Outcome{}, fmt.Errorf("move destination AE title: %w", err)
	}

	moveOutcome, moveErr := retrieveWithMove(ctx, node, identifier, opts, receiver, moveDestination)
	if moveErr == nil {
		return moveOutcome, nil
	}
	if shouldTryGetFallback(ctx, catalog, moveOutcome, opts) {
		getOutcome, getErr := retrieveWithGet(ctx, catalog, node, identifier, opts)
		if getErr == nil {
			return getOutcome, nil
		}
		return moveOutcome, fmt.Errorf("%w; C-GET fallback failed: %v", moveErr, getErr)
	}
	return moveOutcome, moveErr
}

func retrieveMethodPreference(opts Options) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(opts.Method)) {
	case "", "AUTO":
		return "", nil
	case "C-MOVE", "MOVE":
		return MethodMove, nil
	case "C-GET", "GET":
		return MethodGet, nil
	default:
		return "", fmt.Errorf("retrieve method must be Auto, %s, or %s", MethodMove, MethodGet)
	}
}

func retrieveWithMove(ctx context.Context, node nodes.Node, identifier *object.Object, opts Options, receiver *receive.Server, moveDestination string) (Outcome, error) {
	moveCtx, cancel := nettimeout.WithDefault(ctx, DefaultTimeout)
	defer cancel()

	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	start := time.Now()
	assoc, err := ul.DialContext(moveCtx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: opts.CallingAETitle,
		Contexts:       []ul.PresentationContext{dimse.StudyRootMovePresentationContext()},
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("associate with %s (%s): %w", node.Name, address, err)
	}
	released := false
	defer func() {
		if !released {
			_ = assoc.Close()
		}
	}()

	pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootMoveSOPClassUID)
	if err != nil {
		return Outcome{}, fmt.Errorf("no accepted Study Root MOVE presentation context: %w", err)
	}

	progress, err := dimse.SendCMoveWithProgress(moveCtx, assoc, pc.ID, dimse.CMoveRequest{
		AffectedSOPClassUID: dimse.StudyRootMoveSOPClassUID,
		MessageID:           1,
		Priority:            dimse.PriorityMedium,
		MoveDestination:     moveDestination,
	}, identifier, transferSyntaxForContext(pc))
	if err != nil {
		return Outcome{}, err
	}

	var final *dimse.CMoveResponse
	var updates []Progress
	for event := range progress {
		if event.Err != nil {
			return Outcome{}, event.Err
		}
		if event.Response != nil {
			final = event.Response
			update := progressFromResponse(event.Response)
			updates = append(updates, update)
			if opts.OnProgress != nil {
				opts.OnProgress(update)
			}
		}
	}
	if final == nil {
		return Outcome{}, fmt.Errorf("C-MOVE completed without a response")
	}

	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), netverify.DefaultReleaseTimeout)
	defer cancelRelease()
	if err := assoc.Release(releaseCtx); err != nil {
		return Outcome{}, fmt.Errorf("release association with %s (%s): %w", node.Name, address, err)
	}
	released = true

	outcome := outcomeFromResponse(final)
	outcome.Progress = updates
	outcome.Duration = time.Since(start)
	outcome.Receiver = receiver.Snapshot()
	outcome.Stored = outcome.Receiver.Stored
	outcome.Duplicates = outcome.Receiver.Duplicates
	switch outcome.StatusClass {
	case dimse.CMoveStatusSuccess, dimse.CMoveStatusWarning:
		return outcome, nil
	default:
		return outcome, fmt.Errorf("C-MOVE failed with status 0x%04X", outcome.FinalStatus)
	}
}

func shouldTryGetFallback(ctx context.Context, catalog *archive.Catalog, moveOutcome Outcome, opts Options) bool {
	if catalog == nil || ctx.Err() != nil {
		return false
	}
	method, err := retrieveMethodPreference(opts)
	if err != nil || method == MethodMove {
		return false
	}
	if moveOutcome.StatusClass == dimse.CMoveStatusSuccess || moveOutcome.StatusClass == dimse.CMoveStatusWarning {
		return false
	}
	return moveOutcome.Receiver.Stored+moveOutcome.Receiver.Duplicates == 0
}

func retrieveWithGet(ctx context.Context, catalog *archive.Catalog, node nodes.Node, identifier *object.Object, opts Options) (Outcome, error) {
	if catalog == nil {
		return Outcome{}, fmt.Errorf("archive catalog is required for C-GET")
	}
	getCtx, cancel := nettimeout.WithDefault(ctx, DefaultTimeout)
	defer cancel()

	contexts, roles := cGetPresentationContexts()
	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	start := time.Now()
	assoc, err := ul.DialContext(getCtx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: opts.CallingAETitle,
		Contexts:       contexts,
		RoleSelections: roles,
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("associate C-GET with %s (%s): %w", node.Name, address, err)
	}
	released := false
	defer func() {
		if !released {
			_ = assoc.Close()
		}
	}()

	pc, err := dimse.AcceptedContextForSOPClass(assoc, dimse.StudyRootGetSOPClassUID)
	if err != nil {
		return Outcome{}, fmt.Errorf("no accepted Study Root GET presentation context: %w", err)
	}

	outcome := Outcome{Method: MethodGet}
	progress, err := dimse.SendCGetWithProgress(getCtx, assoc, pc.ID, dimse.CGetRequest{
		AffectedSOPClassUID: dimse.StudyRootGetSOPClassUID,
		MessageID:           1,
		Priority:            dimse.PriorityMedium,
	}, identifier, transferSyntaxForContext(pc), dimse.CGetStoreHandlerFunc(func(storeCtx context.Context, req dimse.CGetStoreRequestContext) (uint16, error) {
		if req.DataSet == nil {
			return dimse.StatusCGetUnableToProcess, fmt.Errorf("missing C-STORE dataset")
		}
		report, err := catalog.ImportObject(storeCtx, cGetSourcePath(node.AETitle, req.Request.AffectedSOPInstanceUID), req.DataSet, req.DataSetSyntax)
		if err != nil {
			return dimse.StatusCGetUnableToProcess, err
		}
		if report.InvalidFiles > 0 || len(report.Rejections) > 0 {
			return dimse.StatusCGetUnableToProcess, fmt.Errorf("archive rejected C-GET object")
		}
		outcome.Stored += int64(report.StoredFiles)
		outcome.Duplicates += int64(report.Duplicates)
		return dimse.StatusSuccess, nil
	}))
	if err != nil {
		return outcome, err
	}

	var final *dimse.CGetResponse
	var updates []Progress
	for event := range progress {
		if event.Err != nil {
			return outcome, event.Err
		}
		if event.Response != nil {
			final = event.Response
			update := progressFromGetResponse(event.Response)
			updates = append(updates, update)
			if opts.OnProgress != nil {
				opts.OnProgress(update)
			}
		}
	}
	if final == nil {
		return outcome, fmt.Errorf("C-GET completed without a response")
	}

	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), netverify.DefaultReleaseTimeout)
	defer cancelRelease()
	if err := assoc.Release(releaseCtx); err != nil {
		return outcome, fmt.Errorf("release C-GET association with %s (%s): %w", node.Name, address, err)
	}
	released = true

	outcome.Progress = updates
	outcome.Duration = time.Since(start)
	outcome.FinalStatus = final.Status
	outcome.StatusClass = dimse.ClassifyCMoveStatus(final.Status)
	outcome.Remaining = cGetCountValue(final.NumberOfRemainingSuboperationsOrNil)
	outcome.Completed = cGetCountValue(final.NumberOfCompletedSuboperationsOrNil)
	outcome.Failed = cGetCountValue(final.NumberOfFailedSuboperationsOrNil)
	outcome.Warnings = cGetCountValue(final.NumberOfWarningSuboperationsOrNil)
	switch outcome.StatusClass {
	case dimse.CMoveStatusSuccess, dimse.CMoveStatusWarning:
		return outcome, nil
	default:
		return outcome, fmt.Errorf("C-GET failed with status 0x%04X", outcome.FinalStatus)
	}
}

func cGetPresentationContexts() ([]ul.PresentationContext, []ul.RoleSelectionItem) {
	storageSOPClassUIDs := receive.StorageSOPClassUIDs()
	contexts := make([]ul.PresentationContext, 0, 1+len(storageSOPClassUIDs))
	roles := make([]ul.RoleSelectionItem, 0, len(storageSOPClassUIDs))
	contexts = append(contexts, dimse.StudyRootGetPresentationContext())
	for _, uid := range storageSOPClassUIDs {
		contexts = append(contexts, ul.PresentationContext{
			AbstractSyntaxUID: uid,
			TransferSyntaxUIDs: []string{
				ul.ImplicitVRLittleEndian,
				ul.ExplicitVRLittleEndian,
			},
		})
		roles = append(roles, ul.RoleSelectionItem{SopClassUID: uid, SCPRole: true})
	}
	return contexts, roles
}

func cGetSourcePath(aeTitle, sopInstanceUID string) string {
	aeTitle = nodes.NormalizeAETitle(aeTitle)
	if aeTitle == "" {
		aeTitle = "unknown"
	}
	return fmt.Sprintf("dicom://%s-cget/%s", aeTitle, sopInstanceUID)
}

func ensureReceiver(ctx context.Context, catalog *archive.Catalog, opts Options, allowedCallingAETitles, allowedRemoteHosts []string) (*receive.Server, bool, error) {
	if opts.Receiver != nil {
		return opts.Receiver, false, nil
	}
	address := opts.ReceiveAddress
	if address == "" {
		address = receive.DefaultAddress
	}
	aeTitle := opts.MoveDestination
	if aeTitle == "" {
		aeTitle = netverify.DefaultCallingAETitle
	}
	server, err := receive.Start(ctx, receive.Config{
		Catalog:                catalog,
		Address:                address,
		AETitle:                aeTitle,
		AllowedCallingAETitles: allowedCallingAETitles,
		AllowedRemoteHosts:     allowedRemoteHosts,
		MaxStoreObjectBytes:    opts.MaxStoreObjectBytes,
	})
	if err != nil {
		return nil, false, err
	}
	return server, true, nil
}

func remoteHostAllowlist(host string) []string {
	if net.ParseIP(strings.TrimSpace(host)) == nil {
		return nil
	}
	return []string{strings.TrimSpace(host)}
}

func studyRootStudyMoveIdentifier(studyInstanceUID string) (*object.Object, error) {
	elements, err := dimse.BuildStudyRootStudyFindKeys(map[string]string{
		"StudyInstanceUID": studyInstanceUID,
	})
	if err != nil {
		return nil, err
	}
	obj := object.FromElements(elements, std.Dictionary)
	if got, ok := obj.GetUID(tagStudyInstanceUID); !ok || got != studyInstanceUID {
		return nil, fmt.Errorf("study move identifier missing StudyInstanceUID %q", studyInstanceUID)
	}
	return obj, nil
}

func studyRootSeriesMoveIdentifier(studyInstanceUID, seriesInstanceUID string) (*object.Object, error) {
	elements, err := dimse.BuildStudyRootSeriesFindKeys(map[string]string{
		"StudyInstanceUID":  studyInstanceUID,
		"SeriesInstanceUID": seriesInstanceUID,
	})
	if err != nil {
		return nil, err
	}
	obj := object.FromElements(elements, std.Dictionary)
	if got, ok := obj.GetUID(tagStudyInstanceUID); !ok || got != studyInstanceUID {
		return nil, fmt.Errorf("series move identifier missing StudyInstanceUID %q", studyInstanceUID)
	}
	if got, ok := obj.GetUID(tagSeriesInstanceUID); !ok || got != seriesInstanceUID {
		return nil, fmt.Errorf("series move identifier missing SeriesInstanceUID %q", seriesInstanceUID)
	}
	return obj, nil
}

func studyRootImageMoveIdentifier(studyInstanceUID, seriesInstanceUID, sopInstanceUID string) (*object.Object, error) {
	elements, err := dimse.BuildStudyRootImageFindKeys(map[string]string{
		"StudyInstanceUID":  studyInstanceUID,
		"SeriesInstanceUID": seriesInstanceUID,
		"SOPInstanceUID":    sopInstanceUID,
	})
	if err != nil {
		return nil, err
	}
	obj := object.FromElements(elements, std.Dictionary)
	if got, ok := obj.GetUID(tagStudyInstanceUID); !ok || got != studyInstanceUID {
		return nil, fmt.Errorf("image move identifier missing StudyInstanceUID %q", studyInstanceUID)
	}
	if got, ok := obj.GetUID(tagSeriesInstanceUID); !ok || got != seriesInstanceUID {
		return nil, fmt.Errorf("image move identifier missing SeriesInstanceUID %q", seriesInstanceUID)
	}
	if got, ok := obj.GetUID(tagSOPInstanceUID); !ok || got != sopInstanceUID {
		return nil, fmt.Errorf("image move identifier missing SOPInstanceUID %q", sopInstanceUID)
	}
	return obj, nil
}

func transferSyntaxForContext(pc ul.AcceptedContext) transfer.Syntax {
	if pc.TransferSyntaxUID != "" {
		if syntax, ok := transfer.DefaultRegistry.Get(pc.TransferSyntaxUID); ok {
			return syntax
		}
	}
	return transfer.ImplicitVRLittleEndian
}

func outcomeFromResponse(response *dimse.CMoveResponse) Outcome {
	progress := progressFromResponse(response)
	out := Outcome{Method: MethodMove, FinalStatus: progress.FinalStatus, StatusClass: progress.StatusClass}
	out.Remaining = progress.Remaining
	out.Completed = progress.Completed
	out.Failed = progress.Failed
	out.Warnings = progress.Warnings
	return out
}

func progressFromResponse(response *dimse.CMoveResponse) Progress {
	out := Progress{
		FinalStatus: response.Status,
		StatusClass: dimse.ClassifyCMoveStatus(response.Status),
	}
	if response.NumberOfRemainingSuboperationsOrNil != nil {
		out.Remaining = *response.NumberOfRemainingSuboperationsOrNil
	}
	if response.NumberOfCompletedSuboperationsOrNil != nil {
		out.Completed = *response.NumberOfCompletedSuboperationsOrNil
	}
	if response.NumberOfFailedSuboperationsOrNil != nil {
		out.Failed = *response.NumberOfFailedSuboperationsOrNil
	}
	if response.NumberOfWarningSuboperationsOrNil != nil {
		out.Warnings = *response.NumberOfWarningSuboperationsOrNil
	}
	return out
}

func progressFromGetResponse(response *dimse.CGetResponse) Progress {
	out := Progress{
		FinalStatus: response.Status,
		StatusClass: dimse.ClassifyCMoveStatus(response.Status),
	}
	out.Remaining = cGetCountValue(response.NumberOfRemainingSuboperationsOrNil)
	out.Completed = cGetCountValue(response.NumberOfCompletedSuboperationsOrNil)
	out.Failed = cGetCountValue(response.NumberOfFailedSuboperationsOrNil)
	out.Warnings = cGetCountValue(response.NumberOfWarningSuboperationsOrNil)
	return out
}

func cGetCountValue(value *uint16) uint16 {
	if value == nil {
		return 0
	}
	return *value
}
