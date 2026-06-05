package send

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

const DefaultTimeout = 30 * time.Second

var (
	tagMediaStorageSOPClassUID    = core.NewTag(0x0002, 0x0002)
	tagMediaStorageSOPInstanceUID = core.NewTag(0x0002, 0x0003)
	tagTransferSyntaxUID          = core.NewTag(0x0002, 0x0010)
	tagSOPClassUID                = core.NewTag(0x0008, 0x0016)
	tagSOPInstanceUID             = core.NewTag(0x0008, 0x0018)
)

type Outcome struct {
	Attempted int
	Sent      int
	Warnings  int
	Failed    int
	Failures  []string
	Results   []Result
	Duration  time.Duration
}

type Result struct {
	Path                        string
	SOPClassUID                 string
	SOPInstanceUID              string
	RequestedTransferSyntaxUID  string
	NegotiatedTransferSyntaxUID string
	Status                      uint16
	Error                       string
}

func SendStudy(ctx context.Context, catalog *archive.Catalog, node nodes.Node, studyInstanceUID string, callingAETitle string) (Outcome, error) {
	instances, err := catalog.InstancesForStudy(ctx, studyInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return sendInstances(ctx, node, instances, callingAETitle)
}

func SendSeries(ctx context.Context, catalog *archive.Catalog, node nodes.Node, seriesInstanceUID string, callingAETitle string) (Outcome, error) {
	instances, err := catalog.InstancesForSeries(ctx, seriesInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return sendInstances(ctx, node, instances, callingAETitle)
}

func SendInstance(ctx context.Context, catalog *archive.Catalog, node nodes.Node, sopInstanceUID string, callingAETitle string) (Outcome, error) {
	instance, err := catalog.InstanceBySOPInstanceUID(ctx, sopInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return sendInstances(ctx, node, []archive.Instance{instance}, callingAETitle)
}

func sendInstances(ctx context.Context, node nodes.Node, instances []archive.Instance, callingAETitle string) (Outcome, error) {
	paths := make([]string, 0, len(instances))
	for _, instance := range instances {
		paths = append(paths, instance.StoredPath)
	}
	return SendFiles(ctx, node, paths, callingAETitle)
}

func SendFiles(ctx context.Context, node nodes.Node, paths []string, callingAETitle string) (Outcome, error) {
	if callingAETitle == "" {
		callingAETitle = netverify.DefaultCallingAETitle
	}
	callingAETitle = nodes.NormalizeAETitle(callingAETitle)
	if err := nodes.ValidateAETitle(callingAETitle); err != nil {
		return Outcome{}, fmt.Errorf("calling AE title: %w", err)
	}
	if len(paths) == 0 {
		return Outcome{}, nil
	}
	files, err := inspectFiles(paths)
	if err != nil {
		return Outcome{}, err
	}
	timeout := DefaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	start := time.Now()
	assoc, err := ul.DialContext(ctx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: callingAETitle,
		Contexts:       presentationContexts(files),
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

	outcome := Outcome{Attempted: len(files)}
	for i, file := range files {
		if err := ctx.Err(); err != nil {
			outcome.Duration = time.Since(start)
			return outcome, err
		}
		result := Result{
			Path:                       file.path,
			SOPClassUID:                file.sopClassUID,
			SOPInstanceUID:             file.sopInstanceUID,
			RequestedTransferSyntaxUID: file.transferSyntaxUID,
		}
		status, negotiatedTransferSyntaxUID, err := sendOne(assoc, file, uint16(i+1))
		result.Status = status
		result.NegotiatedTransferSyntaxUID = negotiatedTransferSyntaxUID
		if err != nil {
			outcome.Failed++
			result.Error = err.Error()
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("%s: %v", file.path, err))
			outcome.Results = append(outcome.Results, result)
			continue
		}
		if isWarningStatus(status) {
			outcome.Warnings++
		}
		outcome.Sent++
		outcome.Results = append(outcome.Results, result)
	}

	releaseCtx, cancelRelease := context.WithTimeout(context.Background(), netverify.DefaultReleaseTimeout)
	defer cancelRelease()
	if err := assoc.Release(releaseCtx); err != nil {
		outcome.Duration = time.Since(start)
		if outcome.Failed > 0 {
			return outcome, nil
		}
		return outcome, fmt.Errorf("release association with %s (%s): %w", node.Name, address, err)
	}
	released = true
	outcome.Duration = time.Since(start)
	return outcome, nil
}

type storeFile struct {
	path              string
	file              *object.File
	sopClassUID       string
	sopInstanceUID    string
	transferSyntaxUID string
}

func inspectFiles(paths []string) ([]storeFile, error) {
	files := make([]storeFile, 0, len(paths))
	closeFiles := func() {
		for _, file := range files {
			_ = file.file.Close()
		}
	}
	for _, path := range paths {
		file, err := object.OpenFile(path)
		if err != nil {
			closeFiles()
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		if file.Dataset == nil {
			_ = file.Close()
			closeFiles()
			return nil, fmt.Errorf("%s: DICOM file has no dataset", path)
		}
		sopClassUID, err := fileSOPClassUID(file)
		if err != nil {
			_ = file.Close()
			closeFiles()
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		sopInstanceUID, err := fileSOPInstanceUID(file)
		if err != nil {
			_ = file.Close()
			closeFiles()
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		transferSyntaxUID, err := fileTransferSyntaxUID(file)
		if err != nil {
			_ = file.Close()
			closeFiles()
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		files = append(files, storeFile{
			path:              path,
			file:              file,
			sopClassUID:       sopClassUID,
			sopInstanceUID:    sopInstanceUID,
			transferSyntaxUID: transferSyntaxUID,
		})
	}
	return files, nil
}

func presentationContexts(files []storeFile) []ul.PresentationContext {
	seen := map[string]bool{}
	contexts := make([]ul.PresentationContext, 0, len(files))
	for _, file := range files {
		if seen[file.sopClassUID] {
			continue
		}
		seen[file.sopClassUID] = true
		contexts = append(contexts, ul.PresentationContext{
			AbstractSyntaxUID:  file.sopClassUID,
			TransferSyntaxUIDs: proposedTransferSyntaxes(file.transferSyntaxUID),
		})
	}
	return contexts
}

func sendOne(assoc *ul.Association, file storeFile, messageID uint16) (uint16, string, error) {
	defer file.file.Close()
	pc, err := dimse.AcceptedContextForSOPClass(assoc, file.sopClassUID)
	if err != nil {
		return 0, "", err
	}
	negotiatedTransferSyntaxUID := pc.TransferSyntaxUID
	negotiatedSyntax, ok := transfer.DefaultRegistry.Get(pc.TransferSyntaxUID)
	if !ok {
		return 0, negotiatedTransferSyntaxUID, fmt.Errorf("%w: %q", transfer.ErrUnknownTransferSyntax, pc.TransferSyntaxUID)
	}
	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    file.sopClassUID,
		MessageID:              messageID,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: file.sopInstanceUID,
	}); err != nil {
		return 0, negotiatedTransferSyntaxUID, err
	}
	if err := dimse.SendDataSet(assoc, pc.ID, file.file.Dataset, negotiatedSyntax); err != nil {
		return 0, negotiatedTransferSyntaxUID, err
	}
	response, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		return 0, negotiatedTransferSyntaxUID, err
	}
	if response.MessageIDBeingRespondedTo != messageID {
		return response.Status, negotiatedTransferSyntaxUID, fmt.Errorf("C-STORE response message ID %d, want %d", response.MessageIDBeingRespondedTo, messageID)
	}
	if response.AffectedSOPInstanceUID != file.sopInstanceUID {
		return response.Status, negotiatedTransferSyntaxUID, fmt.Errorf("C-STORE response SOP Instance UID %q, want %q", response.AffectedSOPInstanceUID, file.sopInstanceUID)
	}
	if response.Status != dimse.StatusSuccess && !isWarningStatus(response.Status) {
		return response.Status, negotiatedTransferSyntaxUID, fmt.Errorf("C-STORE failed with status 0x%04X", response.Status)
	}
	return response.Status, negotiatedTransferSyntaxUID, nil
}

func fileSOPClassUID(file *object.File) (string, error) {
	if uid, ok := file.GetUID(tagMediaStorageSOPClassUID); ok && uid != "" {
		return uid, nil
	}
	if file != nil && file.Dataset != nil {
		if uid, ok := file.Dataset.GetUID(tagSOPClassUID); ok && uid != "" {
			return uid, nil
		}
	}
	return "", object.ErrMissingSOPClassUID
}

func fileSOPInstanceUID(file *object.File) (string, error) {
	if uid, ok := file.GetUID(tagMediaStorageSOPInstanceUID); ok && uid != "" {
		return uid, nil
	}
	if file != nil && file.Dataset != nil {
		if uid, ok := file.Dataset.GetUID(tagSOPInstanceUID); ok && uid != "" {
			return uid, nil
		}
	}
	return "", object.ErrMissingSOPInstanceUID
}

func fileTransferSyntaxUID(file *object.File) (string, error) {
	if uid, ok := file.GetUID(tagTransferSyntaxUID); ok && uid != "" {
		return uid, nil
	}
	if file != nil && file.TransferSyntax.UID != "" {
		return file.TransferSyntax.UID, nil
	}
	return "", object.ErrMissingTransferSyntax
}

func proposedTransferSyntaxes(fileTransferSyntaxUID string) []string {
	seen := map[string]bool{}
	var uids []string
	for _, uid := range []string{
		fileTransferSyntaxUID,
		transfer.ExplicitVRLittleEndian.UID,
		transfer.ImplicitVRLittleEndian.UID,
	} {
		uid = transfer.NormalizeUID(uid)
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		uids = append(uids, uid)
	}
	return uids
}

func isWarningStatus(status uint16) bool {
	return status == 0x0001 || status == 0x0107 || status == 0x0116 || (status >= 0xB000 && status <= 0xBFFF)
}
