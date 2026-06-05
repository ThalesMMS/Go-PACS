package receive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

const (
	DefaultAddress = "127.0.0.1:11112"

	statusOutOfResources      = 0xA700
	statusDataSetDoesNotMatch = 0xA900
	statusCannotUnderstand    = 0xC000
)

var (
	errStoreObjectLimitExceeded = errors.New("max_store_object_bytes exceeded")
	tagSOPClassUID              = core.NewTag(0x0008, 0x0016)
	tagSOPInstanceUID           = core.NewTag(0x0008, 0x0018)
)

type Config struct {
	Catalog                *archive.Catalog
	Address                string
	AETitle                string
	AllowedCalledAETitles  []string
	AllowedCallingAETitles []string
	AllowedRemoteHosts     []string
	MaxStoreObjectBytes    int64
}

type Snapshot struct {
	Address      string
	AETitle      string
	Associations int64
	Rejected     int64
	Stored       int64
	Duplicates   int64
	Failed       int64
}

type Server struct {
	catalog                *archive.Catalog
	listener               *ul.Listener
	address                string
	aeTitle                string
	allowedCalledAETitles  map[string]struct{}
	allowedCallingAETitles map[string]struct{}
	allowedRemoteHosts     map[string]struct{}
	maxStoreObjectBytes    int64
	ctx                    context.Context
	cancel                 context.CancelFunc
	done                   chan error
	wg                     sync.WaitGroup

	associations atomic.Int64
	rejected     atomic.Int64
	stored       atomic.Int64
	duplicates   atomic.Int64
	failed       atomic.Int64
}

func Start(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.Catalog == nil {
		return nil, errors.New("receive catalog is required")
	}
	if cfg.Address == "" {
		cfg.Address = DefaultAddress
	}
	if cfg.AETitle == "" {
		cfg.AETitle = netverify.DefaultCallingAETitle
	}
	cfg.AETitle = nodes.NormalizeAETitle(cfg.AETitle)
	if err := nodes.ValidateAETitle(cfg.AETitle); err != nil {
		return nil, fmt.Errorf("receive AE title: %w", err)
	}
	allowedCalledAETitles, err := normalizeAllowedCalledAETitles(cfg.AETitle, cfg.AllowedCalledAETitles)
	if err != nil {
		return nil, err
	}
	allowedCallingAETitles, err := normalizeAllowedCallingAETitles(cfg.AllowedCallingAETitles)
	if err != nil {
		return nil, err
	}
	allowedRemoteHosts, err := normalizeAllowedRemoteHosts(cfg.AllowedRemoteHosts)
	if err != nil {
		return nil, err
	}
	if cfg.MaxStoreObjectBytes < 0 {
		return nil, errors.New("max store object bytes must be greater than zero or unlimited")
	}

	ctx, cancel := context.WithCancel(ctx)
	listener, err := ul.Listen(ul.ListenOptions{Address: cfg.Address, Context: ctx})
	if err != nil {
		cancel()
		return nil, err
	}
	server := &Server{
		catalog:                cfg.Catalog,
		listener:               listener,
		address:                listener.Addr().String(),
		aeTitle:                cfg.AETitle,
		allowedCalledAETitles:  allowedCalledAETitles,
		allowedCallingAETitles: allowedCallingAETitles,
		allowedRemoteHosts:     allowedRemoteHosts,
		maxStoreObjectBytes:    cfg.MaxStoreObjectBytes,
		ctx:                    ctx,
		cancel:                 cancel,
		done:                   make(chan error, 1),
	}
	go server.serve()
	return server, nil
}

func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	return s.address
}

func (s *Server) AETitle() string {
	if s == nil {
		return ""
	}
	return s.aeTitle
}

func (s *Server) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	return Snapshot{
		Address:      s.address,
		AETitle:      s.aeTitle,
		Associations: s.associations.Load(),
		Rejected:     s.rejected.Load(),
		Stored:       s.stored.Load(),
		Duplicates:   s.duplicates.Load(),
		Failed:       s.failed.Load(),
	}
}

func (s *Server) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.cancel()
	_ = s.listener.Close()
	wait := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(wait)
	}()
	select {
	case err := <-s.done:
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) serve() {
	var finalErr error
	defer func() {
		s.done <- finalErr
		close(s.done)
	}()
	for {
		assoc, err := s.listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   s.aeTitle,
			Context:                   s.ctx,
			SupportedAbstractSyntaxes: acceptedAbstractSyntaxes(),
			SupportedTransferSyntaxes: supportedTransferSyntaxUIDs(),
		})
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			finalErr = err
			return
		}
		s.associations.Add(1)
		if !s.allowsRemoteHost(assoc) || !s.allowsCalledAE(assoc.CalledAETitle) || !s.allowsCallingAE(assoc.CallingAETitle) {
			s.rejected.Add(1)
			_ = assoc.Abort(ul.AbortReasonNotSpecified)
			_ = assoc.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.handleAssociation(assoc); err != nil {
				s.failed.Add(1)
			}
		}()
	}
}

func (s *Server) handleAssociation(assoc *ul.Association) error {
	defer assoc.Close()
	for {
		incoming, err := receiveCommandOrRelease(assoc)
		if err != nil {
			return err
		}
		if incoming.release {
			return assoc.WritePDU(&ul.ReleaseRP{})
		}

		field, err := dimse.CommandUint16(incoming.command, dimse.CommandField)
		if err != nil {
			return err
		}
		switch field {
		case dimse.CEchoRQ:
			if err := handleCEchoCommand(assoc, incoming); err != nil {
				return err
			}
		case dimse.CStoreRQ:
			if err := s.handleCStoreCommand(assoc, incoming); err != nil {
				return err
			}
		default:
			_ = assoc.Abort(ul.AbortReasonNotSpecified)
			return fmt.Errorf("unsupported DIMSE command 0x%04X", field)
		}
	}
}

func normalizeAllowedCalledAETitles(primary string, aeTitles []string) (map[string]struct{}, error) {
	allowed := map[string]struct{}{}
	primary = nodes.NormalizeAETitle(primary)
	if err := nodes.ValidateAETitle(primary); err != nil {
		return nil, fmt.Errorf("called AE title: %w", err)
	}
	allowed[primary] = struct{}{}
	for _, aeTitle := range aeTitles {
		aeTitle = nodes.NormalizeAETitle(aeTitle)
		if err := nodes.ValidateAETitle(aeTitle); err != nil {
			return nil, fmt.Errorf("allowed called AE title: %w", err)
		}
		allowed[aeTitle] = struct{}{}
	}
	return allowed, nil
}

func normalizeAllowedCallingAETitles(aeTitles []string) (map[string]struct{}, error) {
	if len(aeTitles) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(aeTitles))
	for _, aeTitle := range aeTitles {
		aeTitle = nodes.NormalizeAETitle(aeTitle)
		if err := nodes.ValidateAETitle(aeTitle); err != nil {
			return nil, fmt.Errorf("allowed calling AE title: %w", err)
		}
		allowed[aeTitle] = struct{}{}
	}
	return allowed, nil
}

func normalizeAllowedRemoteHosts(hosts []string) (map[string]struct{}, error) {
	if len(hosts) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, fmt.Errorf("allowed remote host %q is not an IP address", host)
		}
		allowed[ip.String()] = struct{}{}
	}
	return allowed, nil
}

func (s *Server) allowsCalledAE(aeTitle string) bool {
	aeTitle = nodes.NormalizeAETitle(aeTitle)
	_, ok := s.allowedCalledAETitles[aeTitle]
	return ok
}

func (s *Server) allowsCallingAE(aeTitle string) bool {
	if len(s.allowedCallingAETitles) == 0 {
		return true
	}
	aeTitle = nodes.NormalizeAETitle(aeTitle)
	_, ok := s.allowedCallingAETitles[aeTitle]
	return ok
}

func (s *Server) allowsRemoteHost(assoc *ul.Association) bool {
	if len(s.allowedRemoteHosts) == 0 {
		return true
	}
	if assoc == nil || assoc.Conn == nil {
		return false
	}
	host, _, err := net.SplitHostPort(assoc.Conn.RemoteAddr().String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	_, ok := s.allowedRemoteHosts[ip.String()]
	return ok
}

func handleCEchoCommand(assoc *ul.Association, incoming incomingCommand) error {
	messageID, err := dimse.CommandUint16(incoming.command, dimse.MessageID)
	if err != nil {
		return err
	}
	dataSetType, err := dimse.CommandUint16(incoming.command, dimse.CommandDataSetType)
	if err != nil {
		return err
	}
	if dataSetType != dimse.NoDataSet {
		return fmt.Errorf("C-ECHO request dataset type 0x%04X, want no dataset", dataSetType)
	}
	return dimse.SendCEchoResponse(assoc, incoming.pcID, messageID, dimse.StatusSuccess)
}

func (s *Server) handleCStoreCommand(assoc *ul.Association, incoming incomingCommand) error {
	req, err := dimse.ParseCStoreRequest(incoming.command)
	if err != nil {
		return err
	}
	pc, err := acceptedContextByID(assoc, incoming.pcID)
	if err != nil {
		return err
	}
	syntax, ok := transfer.DefaultRegistry.Get(pc.TransferSyntaxUID)
	if !ok {
		return fmt.Errorf("%w: %q", transfer.ErrUnknownTransferSyntax, pc.TransferSyntaxUID)
	}

	status := uint16(dimse.StatusSuccess)
	dataset, err := receiveDataSet(assoc, incoming, syntax, s.maxStoreObjectBytes)
	if err != nil {
		if errors.Is(err, errStoreObjectLimitExceeded) {
			status = statusOutOfResources
		} else {
			status = statusCannotUnderstand
		}
	} else if err := validateCStoreDataSet(req.AffectedSOPClassUID, req.AffectedSOPInstanceUID, pc, dataset); err != nil {
		status = statusDataSetDoesNotMatch
	} else {
		report, importErr := s.catalog.ImportObject(assoc.Context, sourcePath(assoc, req), dataset, syntax)
		if importErr != nil {
			status = statusOutOfResources
		} else if report.InvalidFiles > 0 || len(report.Rejections) > 0 {
			status = statusCannotUnderstand
		} else {
			s.stored.Add(int64(report.StoredFiles))
			s.duplicates.Add(int64(report.Duplicates))
		}
	}
	if status != dimse.StatusSuccess {
		s.failed.Add(1)
	}

	rsp := dimse.CStoreResponse{
		AffectedSOPClassUID:       req.AffectedSOPClassUID,
		MessageIDBeingRespondedTo: req.MessageID,
		AffectedSOPInstanceUID:    req.AffectedSOPInstanceUID,
		Status:                    status,
	}
	if rspErr := dimse.SendCStoreResponse(assoc, incoming.pcID, rsp); rspErr != nil {
		return rspErr
	}
	return nil
}

type incomingCommand struct {
	release    bool
	pcID       byte
	command    *object.Object
	dataPrefix []byte
	dataLast   bool
}

func receiveCommandOrRelease(assoc *ul.Association) (incomingCommand, error) {
	var command bytes.Buffer
	var out incomingCommand
	commandDone := false

	for !commandDone {
		pdu, err := assoc.ReadPDU()
		if err != nil {
			return incomingCommand{}, err
		}
		switch pdu := pdu.(type) {
		case *ul.ReleaseRQ:
			return incomingCommand{release: true}, nil
		case *ul.PDataTF:
			for _, value := range pdu.Values {
				if out.pcID == 0 {
					out.pcID = value.PresentationContextID
				}
				if value.PresentationContextID != out.pcID {
					return incomingCommand{}, fmt.Errorf("%w: got %d, want %d", dimse.ErrPresentationContextMismatch, value.PresentationContextID, out.pcID)
				}
				if value.IsCommand {
					if commandDone {
						return incomingCommand{}, errors.New("unexpected command PDV after command completion")
					}
					command.Write(value.Data)
					if value.IsLast {
						commandDone = true
					}
					continue
				}
				if !commandDone {
					return incomingCommand{}, errors.New("dataset PDV received before complete command")
				}
				out.dataPrefix = append(out.dataPrefix, value.Data...)
				if value.IsLast {
					out.dataLast = true
				}
			}
		default:
			return incomingCommand{}, fmt.Errorf("%w: got %T while waiting for command P-DATA or A-RELEASE-RQ", ul.ErrUnexpectedPDU, pdu)
		}
	}

	obj, err := dimse.DecodeCommandSet(command.Bytes())
	if err != nil {
		return incomingCommand{}, err
	}
	out.command = obj
	return out, nil
}

func receiveDataSet(assoc *ul.Association, incoming incomingCommand, syntax transfer.Syntax, maxBytes int64) (*object.Object, error) {
	if maxBytes > 0 && int64(len(incoming.dataPrefix)) > maxBytes {
		if !incoming.dataLast {
			_, _ = io.Copy(io.Discard, dimse.NewPDataReader(assoc, incoming.pcID))
		}
		return nil, storeObjectLimitExceeded(int64(len(incoming.dataPrefix)), maxBytes)
	}
	if incoming.dataLast {
		return object.ReadDataSet(storeObjectLimitReader(bytes.NewReader(incoming.dataPrefix), maxBytes), syntax)
	}
	pdataReader := dimse.NewPDataReader(assoc, incoming.pcID)
	if len(incoming.dataPrefix) == 0 {
		dataset, err := object.ReadDataSet(storeObjectLimitReader(pdataReader, maxBytes), syntax)
		if errors.Is(err, errStoreObjectLimitExceeded) {
			_, _ = io.Copy(io.Discard, pdataReader)
		}
		return dataset, err
	}
	reader := io.MultiReader(bytes.NewReader(incoming.dataPrefix), pdataReader)
	dataset, err := object.ReadDataSet(storeObjectLimitReader(reader, maxBytes), syntax)
	if errors.Is(err, errStoreObjectLimitExceeded) {
		_, _ = io.Copy(io.Discard, pdataReader)
	}
	return dataset, err
}

func storeObjectLimitReader(reader io.Reader, maxBytes int64) io.Reader {
	if maxBytes <= 0 {
		return reader
	}
	return &limitedStoreObjectReader{reader: reader, maxBytes: maxBytes}
}

type limitedStoreObjectReader struct {
	reader   io.Reader
	maxBytes int64
	read     int64
}

func (r *limitedStoreObjectReader) Read(p []byte) (int, error) {
	remaining := r.maxBytes - r.read
	if remaining <= 0 {
		return 0, storeObjectLimitExceeded(r.read+1, r.maxBytes)
	}
	if remaining < int64(len(p)) {
		p = p[:int(remaining)+1]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	if r.read > r.maxBytes {
		return n, storeObjectLimitExceeded(r.read, r.maxBytes)
	}
	return n, err
}

func storeObjectLimitExceeded(got, max int64) error {
	return fmt.Errorf("%w: %d > %d", errStoreObjectLimitExceeded, got, max)
}

func acceptedContextByID(assoc *ul.Association, pcID byte) (ul.AcceptedContext, error) {
	if assoc == nil {
		return ul.AcceptedContext{}, errors.New("nil association")
	}
	for _, pc := range assoc.AcceptedContexts {
		if pc.ID == pcID {
			return pc, nil
		}
	}
	return ul.AcceptedContext{}, fmt.Errorf("no accepted presentation context with ID %d", pcID)
}

func validateCStoreDataSet(affectedSOPClassUID, affectedSOPInstanceUID string, pc ul.AcceptedContext, dataset *object.Object) error {
	if dataset == nil {
		return errors.New("missing dataset")
	}
	if affectedSOPClassUID == "" {
		return errors.New("missing affected SOP Class UID")
	}
	if affectedSOPInstanceUID == "" {
		return errors.New("missing affected SOP Instance UID")
	}
	if pc.AbstractSyntaxUID != affectedSOPClassUID {
		return fmt.Errorf("presentation context SOP Class UID %q does not match request %q", pc.AbstractSyntaxUID, affectedSOPClassUID)
	}
	if sopClassUID, ok := dataset.GetUID(tagSOPClassUID); !ok || sopClassUID != affectedSOPClassUID {
		return errors.New("dataset SOP Class UID does not match request")
	}
	if sopInstanceUID, ok := dataset.GetUID(tagSOPInstanceUID); !ok || sopInstanceUID != affectedSOPInstanceUID {
		return errors.New("dataset SOP Instance UID does not match request")
	}
	return nil
}

func sourcePath(assoc *ul.Association, req *dimse.CStoreRequest) string {
	callingAE := "unknown"
	if assoc != nil && assoc.CallingAETitle != "" {
		callingAE = nodes.NormalizeAETitle(assoc.CallingAETitle)
	}
	return fmt.Sprintf("dicom://%s/%s", callingAE, req.AffectedSOPInstanceUID)
}

func acceptedAbstractSyntaxes() []string {
	uids := []string{dimse.VerificationSOPClassUID}
	uids = append(uids, storageSOPClassUIDs()...)
	return uids
}

// StorageSOPClassUIDs returns the Storage SOP Classes accepted by the receiver.
func StorageSOPClassUIDs() []string {
	return storageSOPClassUIDs()
}

func storageSOPClassUIDs() []string {
	return []string{
		"1.2.840.10008.5.1.4.1.1.1",
		"1.2.840.10008.5.1.4.1.1.2",
		"1.2.840.10008.5.1.4.1.1.2.1",
		"1.2.840.10008.5.1.4.1.1.4",
		"1.2.840.10008.5.1.4.1.1.4.1",
		"1.2.840.10008.5.1.4.1.1.6.1",
		"1.2.840.10008.5.1.4.1.1.7",
		"1.2.840.10008.5.1.4.1.1.7.1",
		"1.2.840.10008.5.1.4.1.1.7.2",
		"1.2.840.10008.5.1.4.1.1.7.3",
		"1.2.840.10008.5.1.4.1.1.7.4",
		"1.2.840.10008.5.1.4.1.1.12.1",
		"1.2.840.10008.5.1.4.1.1.12.2",
		"1.2.840.10008.5.1.4.1.1.20",
		"1.2.840.10008.5.1.4.1.1.128",
		"1.2.840.10008.5.1.4.1.1.1.1",
		"1.2.840.10008.5.1.4.1.1.1.1.1",
	}
}

func supportedTransferSyntaxUIDs() []string {
	return []string{
		transfer.ImplicitVRLittleEndian.UID,
		transfer.ExplicitVRLittleEndian.UID,
	}
}
