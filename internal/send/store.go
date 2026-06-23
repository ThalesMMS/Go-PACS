package send

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nettimeout"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
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

type Options struct {
	CallingAETitle string
	OnProgress     func(Progress)
}

type Progress struct {
	Attempted int
	Sent      int
	Warnings  int
	Failed    int
	Total     int
	Path      string
	Status    uint16
	Error     string
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
	return SendStudyWithOptions(ctx, catalog, node, studyInstanceUID, Options{CallingAETitle: callingAETitle})
}

func SendStudyWithOptions(ctx context.Context, catalog *archive.Catalog, node nodes.Node, studyInstanceUID string, opts Options) (Outcome, error) {
	instances, err := catalog.InstancesForStudy(ctx, studyInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return sendInstancesWithOptions(ctx, node, instances, opts)
}

func SendSeries(ctx context.Context, catalog *archive.Catalog, node nodes.Node, seriesInstanceUID string, callingAETitle string) (Outcome, error) {
	return SendSeriesWithOptions(ctx, catalog, node, seriesInstanceUID, Options{CallingAETitle: callingAETitle})
}

func SendSeriesWithOptions(ctx context.Context, catalog *archive.Catalog, node nodes.Node, seriesInstanceUID string, opts Options) (Outcome, error) {
	instances, err := catalog.InstancesForSeries(ctx, seriesInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return sendInstancesWithOptions(ctx, node, instances, opts)
}

func SendInstance(ctx context.Context, catalog *archive.Catalog, node nodes.Node, sopInstanceUID string, callingAETitle string) (Outcome, error) {
	return SendInstanceWithOptions(ctx, catalog, node, sopInstanceUID, Options{CallingAETitle: callingAETitle})
}

func SendInstanceWithOptions(ctx context.Context, catalog *archive.Catalog, node nodes.Node, sopInstanceUID string, opts Options) (Outcome, error) {
	instance, err := catalog.InstanceBySOPInstanceUID(ctx, sopInstanceUID)
	if err != nil {
		return Outcome{}, err
	}
	return sendInstancesWithOptions(ctx, node, []archive.Instance{instance}, opts)
}

func sendInstances(ctx context.Context, node nodes.Node, instances []archive.Instance, callingAETitle string) (Outcome, error) {
	return sendInstancesWithOptions(ctx, node, instances, Options{CallingAETitle: callingAETitle})
}

func sendInstancesWithOptions(ctx context.Context, node nodes.Node, instances []archive.Instance, opts Options) (Outcome, error) {
	paths := make([]string, 0, len(instances))
	for _, instance := range instances {
		paths = append(paths, instance.StoredPath)
	}
	return SendFilesWithOptions(ctx, node, paths, opts)
}

func SendFiles(ctx context.Context, node nodes.Node, paths []string, callingAETitle string) (Outcome, error) {
	return SendFilesWithOptions(ctx, node, paths, Options{CallingAETitle: callingAETitle})
}

func SendFilesWithOptions(ctx context.Context, node nodes.Node, paths []string, opts Options) (Outcome, error) {
	callingAETitle := opts.CallingAETitle
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
	ctx, cancel := nettimeout.WithDefault(ctx, DefaultTimeout)
	defer cancel()

	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	start := time.Now()
	tlsConfig, err := netverify.TLSConfigForNode(node)
	if err != nil {
		return Outcome{}, err
	}
	assoc, err := ul.DialContext(ctx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: callingAETitle,
		Contexts:       presentationContexts(files, node.SendTransferSyntaxOrDefault()),
		TLSConfig:      tlsConfig,
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
		status, negotiatedTransferSyntaxUID, err := sendOne(ctx, assoc, file, uint16(i+1))
		result.Status = status
		result.NegotiatedTransferSyntaxUID = negotiatedTransferSyntaxUID
		if err != nil {
			outcome.Failed++
			result.Error = err.Error()
			outcome.Failures = append(outcome.Failures, fmt.Sprintf("%s: %v", file.path, err))
			outcome.Results = append(outcome.Results, result)
			reportSendProgress(opts.OnProgress, outcome, len(files), file.path, status, result.Error)
			continue
		}
		if dimse.IsCStoreWarningStatus(status) {
			outcome.Warnings++
		}
		outcome.Sent++
		outcome.Results = append(outcome.Results, result)
		reportSendProgress(opts.OnProgress, outcome, len(files), file.path, status, "")
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

func reportSendProgress(onProgress func(Progress), outcome Outcome, total int, path string, status uint16, resultError string) {
	if onProgress == nil {
		return
	}
	onProgress(Progress{
		Attempted: outcome.Sent + outcome.Failed,
		Sent:      outcome.Sent,
		Warnings:  outcome.Warnings,
		Failed:    outcome.Failed,
		Total:     total,
		Path:      path,
		Status:    status,
		Error:     resultError,
	})
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

func presentationContexts(files []storeFile, preferredTransferSyntax string) []ul.PresentationContext {
	seen := map[string]bool{}
	contexts := make([]ul.PresentationContext, 0, len(files))
	for _, file := range files {
		if seen[file.sopClassUID] {
			continue
		}
		seen[file.sopClassUID] = true
		contexts = append(contexts, ul.PresentationContext{
			AbstractSyntaxUID:  file.sopClassUID,
			TransferSyntaxUIDs: proposedTransferSyntaxes(file.transferSyntaxUID, preferredTransferSyntax),
		})
	}
	return contexts
}

func sendOne(ctx context.Context, assoc *ul.Association, file storeFile, messageID uint16) (uint16, string, error) {
	defer file.file.Close()
	result, err := dimse.NewStoreClient(assoc).StoreWithOptions(ctx, file.file.Dataset, dimse.CStoreOptions{
		AffectedSOPClassUID:    file.sopClassUID,
		MessageID:              messageID,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: file.sopInstanceUID,
	})
	status := uint16(0)
	if result.Response != nil {
		status = result.Response.Status
	}
	negotiatedTransferSyntaxUID := result.TransferSyntax.UID
	if negotiatedTransferSyntaxUID == "" {
		negotiatedTransferSyntaxUID = result.PresentationContext.TransferSyntaxUID
	}
	return status, negotiatedTransferSyntaxUID, err
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

func proposedTransferSyntaxes(fileTransferSyntaxUID string, preferredTransferSyntax string) []string {
	switch preferredTransferSyntax {
	case nodes.SendTransferSyntaxExplicitVRLittleEndian:
		return transfer.ProposedStoreTransferSyntaxUIDs(fileTransferSyntaxUID, transfer.NativeStoreExplicitLittleEndianFirst)
	case nodes.SendTransferSyntaxImplicitVRLittleEndian:
		return transfer.ProposedStoreTransferSyntaxUIDs(fileTransferSyntaxUID, transfer.NativeStoreImplicitLittleEndianFirst)
	default:
		return transfer.ProposedStoreTransferSyntaxUIDs(fileTransferSyntaxUID, transfer.NativeStoreSourceFirst)
	}
}
