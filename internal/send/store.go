package send

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nettimeout"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/net/dicomweb"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const DefaultTimeout = 30 * time.Second

const (
	MethodCStore = "C-STORE"
	MethodSTOWRS = "STOW-RS"
)

var (
	tagMediaStorageSOPClassUID    = core.NewTag(0x0002, 0x0002)
	tagMediaStorageSOPInstanceUID = core.NewTag(0x0002, 0x0003)
	tagTransferSyntaxUID          = core.NewTag(0x0002, 0x0010)
	tagSOPClassUID                = core.NewTag(0x0008, 0x0016)
	tagSOPInstanceUID             = core.NewTag(0x0008, 0x0018)
)

type Outcome struct {
	Method    string
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

// SendFiles sends DICOM instances from the provided file paths to a remote node using the specified calling AE title, returning transmission statistics and per-file results.
func SendFiles(ctx context.Context, node nodes.Node, paths []string, callingAETitle string) (Outcome, error) {
	return SendFilesWithOptions(ctx, node, paths, Options{CallingAETitle: callingAETitle})
}

// The operation includes per-file progress reporting via opts.OnProgress if provided.
func SendFilesWithOptions(ctx context.Context, node nodes.Node, paths []string, opts Options) (Outcome, error) {
	if node.IsDICOMweb() {
		return sendFilesWithSTOW(ctx, node, paths, opts)
	}
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

	outcome := Outcome{Method: MethodCStore, Attempted: len(files)}
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

// sendFilesWithSTOW sends DICOM files to a remote DICOMweb STOW-RS node.
func sendFilesWithSTOW(ctx context.Context, node nodes.Node, paths []string, opts Options) (Outcome, error) {
	if len(paths) == 0 {
		return Outcome{}, nil
	}
	start := time.Now()
	outcome := Outcome{Method: MethodSTOWRS, Attempted: len(paths)}
	files, err := inspectFiles(paths)
	if err != nil {
		outcome.Failed = len(paths)
		outcome.Failures = append(outcome.Failures, err.Error())
		outcome.Duration = time.Since(start)
		return outcome, err
	}
	for _, file := range files {
		_ = file.file.Close()
	}

	ctx, cancel := nettimeout.WithDefault(ctx, DefaultTimeout)
	defer cancel()
	tlsConfig, err := netverify.TLSConfigForNode(node)
	if err != nil {
		outcome.Failed = len(files)
		outcome.Failures = append(outcome.Failures, err.Error())
		outcome.Duration = time.Since(start)
		return outcome, err
	}
	client := stowClientForNode(node, tlsConfig)
	result, err := client.StoreInstances(ctx, stowInstances(files))
	outcome.Duration = time.Since(start)
	mapSTOWResult(&outcome, files, result, err, opts.OnProgress)
	return outcome, err
}

// stowClientForNode creates a dicomweb.Client configured for the given node.
// If tlsConfig is non-nil, the client uses it for TLS connections; otherwise
// the default HTTP transport is used.
func stowClientForNode(node nodes.Node, tlsConfig *tls.Config) dicomweb.Client {
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

// stowInstances produces StoreInstance structures for STOW-RS submission from inspected DICOM files.
func stowInstances(files []storeFile) []dicomweb.StoreInstance {
	instances := make([]dicomweb.StoreInstance, 0, len(files))
	for _, file := range files {
		path := file.path
		instances = append(instances, dicomweb.StoreInstance{
			SOPClassUID:    file.sopClassUID,
			SOPInstanceUID: file.sopInstanceUID,
			Path:           path,
			Open: func() (io.ReadCloser, error) {
				return os.Open(path)
			},
		})
	}
	return instances
}

// mapSTOWResult maps a STOW-RS StoreResult into the outcome, populating sent/warning/failed counters, per-file Result entries, failure messages, and invoking progress callbacks.
func mapSTOWResult(outcome *Outcome, files []storeFile, result dicomweb.StoreResult, storeErr error, onProgress func(Progress)) {
	bySOP := map[string]storeFile{}
	for _, file := range files {
		bySOP[file.sopInstanceUID] = file
	}
	accounted := map[string]bool{}
	for _, item := range result.Stored {
		file := stowFileForItem(bySOP, item)
		accounted[item.SOPInstanceUID] = true
		status := dimse.StatusSuccess
		resultError := ""
		if item.WarningReason != 0 {
			status = item.WarningReason
			outcome.Warnings++
			resultError = fmt.Sprintf("STOW-RS warning reason 0x%04X", item.WarningReason)
		}
		outcome.Sent++
		result := Result{
			Path:                       file.path,
			SOPClassUID:                firstNonEmpty(item.SOPClassUID, file.sopClassUID),
			SOPInstanceUID:             firstNonEmpty(item.SOPInstanceUID, file.sopInstanceUID),
			RequestedTransferSyntaxUID: file.transferSyntaxUID,
			Status:                     status,
			Error:                      resultError,
		}
		outcome.Results = append(outcome.Results, result)
		reportSendProgress(onProgress, *outcome, len(files), file.path, status, resultError)
	}
	for _, item := range result.Failed {
		file := stowFileForItem(bySOP, item)
		accounted[item.SOPInstanceUID] = true
		status := item.FailureReason
		message := fmt.Sprintf("STOW-RS failed SOP %s", firstNonEmpty(item.SOPInstanceUID, file.sopInstanceUID))
		if status != 0 {
			message += fmt.Sprintf(" with reason 0x%04X", status)
		}
		outcome.Failed++
		outcome.Failures = append(outcome.Failures, message)
		outcome.Results = append(outcome.Results, Result{
			Path:                       file.path,
			SOPClassUID:                firstNonEmpty(item.SOPClassUID, file.sopClassUID),
			SOPInstanceUID:             firstNonEmpty(item.SOPInstanceUID, file.sopInstanceUID),
			RequestedTransferSyntaxUID: file.transferSyntaxUID,
			Status:                     status,
			Error:                      message,
		})
		reportSendProgress(onProgress, *outcome, len(files), file.path, status, message)
	}
	if len(result.Stored) == 0 && len(result.Failed) == 0 {
		if storeErr != nil {
			for _, file := range files {
				markSTOWFailure(outcome, file, 0, storeErr.Error(), len(files), onProgress)
			}
			return
		}
		for _, file := range files {
			markSTOWSuccess(outcome, file, len(files), onProgress)
		}
		return
	}
	for _, file := range files {
		if accounted[file.sopInstanceUID] {
			continue
		}
		if storeErr != nil {
			markSTOWFailure(outcome, file, 0, storeErr.Error(), len(files), onProgress)
			continue
		}
		markSTOWSuccess(outcome, file, len(files), onProgress)
	}
}

// markSTOWSuccess records a successful STOW-RS send for a file and reports the outcome.
func markSTOWSuccess(outcome *Outcome, file storeFile, total int, onProgress func(Progress)) {
	outcome.Sent++
	outcome.Results = append(outcome.Results, Result{
		Path:                       file.path,
		SOPClassUID:                file.sopClassUID,
		SOPInstanceUID:             file.sopInstanceUID,
		RequestedTransferSyntaxUID: file.transferSyntaxUID,
		Status:                     dimse.StatusSuccess,
	})
	reportSendProgress(onProgress, *outcome, total, file.path, dimse.StatusSuccess, "")
}

// markSTOWFailure records a file's STOW-RS storage failure in the outcome, including failure statistics and progress reporting.
func markSTOWFailure(outcome *Outcome, file storeFile, status uint16, message string, total int, onProgress func(Progress)) {
	outcome.Failed++
	outcome.Failures = append(outcome.Failures, fmt.Sprintf("%s: %s", file.path, message))
	outcome.Results = append(outcome.Results, Result{
		Path:                       file.path,
		SOPClassUID:                file.sopClassUID,
		SOPInstanceUID:             file.sopInstanceUID,
		RequestedTransferSyntaxUID: file.transferSyntaxUID,
		Status:                     status,
		Error:                      message,
	})
	reportSendProgress(onProgress, *outcome, total, file.path, status, message)
}

// stowFileForItem returns the storeFile for a STOW-RS store result item, or constructs a partial entry from the item if not found.
func stowFileForItem(files map[string]storeFile, item dicomweb.StoreItem) storeFile {
	if file, ok := files[item.SOPInstanceUID]; ok {
		return file
	}
	return storeFile{sopClassUID: item.SOPClassUID, sopInstanceUID: item.SOPInstanceUID}
}

// firstNonEmpty returns the first non-empty string from the provided values, or an empty string if all values are empty.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// reportSendProgress invokes the progress callback with the current send outcome, if the callback is non-nil.
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

// PresentationContexts builds DIMSE presentation contexts, creating one context per unique SOP Class UID with transfer syntax proposals based on the preferred syntax.
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

// sendOne sends a DICOM instance via DIMSE C-STORE and returns the DIMSE response status, the negotiated transfer syntax UID, and any error.
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

// fileSOPClassUID extracts the SOP Class UID from a DICOM file, or returns an error if not found.
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

// fileTransferSyntaxUID extracts the transfer syntax UID from a DICOM file.
// It returns the transfer syntax UID string, or an error if the transfer syntax is missing.
func fileTransferSyntaxUID(file *object.File) (string, error) {
	if uid, ok := file.GetUID(tagTransferSyntaxUID); ok && uid != "" {
		return uid, nil
	}
	if file != nil && file.TransferSyntax.UID != "" {
		return file.TransferSyntax.UID, nil
	}
	return "", object.ErrMissingTransferSyntax
}

// proposedTransferSyntaxes builds a list of proposed transfer syntax UIDs ordered by the specified preference.
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
