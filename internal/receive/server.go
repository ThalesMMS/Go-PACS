package receive

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const (
	DefaultAddress         = "127.0.0.1:11112"
	DefaultMaxAssociations = 100

	PreferredTransferSyntaxAuto                   = "auto"
	PreferredTransferSyntaxExplicitVRLittleEndian = "1.2.840.10008.1.2.1"
	PreferredTransferSyntaxImplicitVRLittleEndian = "1.2.840.10008.1.2"
)

type Config struct {
	Catalog                 *archive.Catalog
	Address                 string
	AETitle                 string
	AllowedCalledAETitles   []string
	AllowedCallingAETitles  []string
	AllowedRemoteHosts      []string
	MaxAssociations         int
	MaxStoreObjectBytes     int64
	PreferredTransferSyntax string
	DecompressImages        bool
	TLSConfig               *tls.Config
	// NodeLookup resolves C-MOVE Move Destination AE titles. When nil, C-MOVE
	// SCP is disabled and the receiver does not advertise Study Root MOVE.
	NodeLookup func(aeTitle string) (nodes.Node, bool)
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
	catalog                     *archive.Catalog
	listener                    *ul.Listener
	address                     string
	aeTitle                     string
	allowedCalledAETitles       map[string]struct{}
	allowedCallingAETitles      map[string]struct{}
	allowedRemoteHosts          map[string]struct{}
	associationSlots            chan struct{}
	maxStoreObjectBytes         int64
	supportedTransferSyntaxUIDs []string
	decompressImages            bool
	tlsConfig                   *tls.Config
	nodeLookup                  func(aeTitle string) (nodes.Node, bool)
	ctx                         context.Context
	cancel                      context.CancelFunc
	done                        chan error
	wg                          sync.WaitGroup

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
	maxAssociations := cfg.MaxAssociations
	if maxAssociations < 0 {
		return nil, errors.New("max associations must be greater than zero or default")
	}
	if maxAssociations == 0 {
		maxAssociations = DefaultMaxAssociations
	}
	supportedTransferSyntaxes, err := TransferSyntaxUIDsForPreference(cfg.PreferredTransferSyntax)
	if err != nil {
		return nil, err
	}
	if cfg.DecompressImages {
		supportedTransferSyntaxes = appendUniqueTransferSyntaxUIDs(supportedTransferSyntaxes, decompressionTransferSyntaxUIDs()...)
	}

	ctx, cancel := context.WithCancel(ctx)
	listener, err := ul.Listen(ul.ListenOptions{Address: cfg.Address, Context: ctx})
	if err != nil {
		cancel()
		return nil, err
	}
	server := &Server{
		catalog:                     cfg.Catalog,
		listener:                    listener,
		address:                     listener.Addr().String(),
		aeTitle:                     cfg.AETitle,
		allowedCalledAETitles:       allowedCalledAETitles,
		allowedCallingAETitles:      allowedCallingAETitles,
		allowedRemoteHosts:          allowedRemoteHosts,
		associationSlots:            make(chan struct{}, maxAssociations),
		maxStoreObjectBytes:         cfg.MaxStoreObjectBytes,
		supportedTransferSyntaxUIDs: supportedTransferSyntaxes,
		decompressImages:            cfg.DecompressImages,
		tlsConfig:                   cfg.TLSConfig,
		nodeLookup:                  cfg.NodeLookup,
		ctx:                         ctx,
		cancel:                      cancel,
		done:                        make(chan error, 1),
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
			SupportedAbstractSyntaxes: acceptedAbstractSyntaxes(s.nodeLookup != nil),
			SupportedTransferSyntaxes: s.supportedTransferSyntaxUIDs,
			RoleSelections:            storageRoleSelections(),
			TLSConfig:                 s.tlsConfig,
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
		if !s.tryAcquireAssociationSlot() {
			s.rejected.Add(1)
			_ = assoc.Abort(ul.AbortReasonNotSpecified)
			_ = assoc.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.releaseAssociationSlot()
			// C-STORE object failures are counted where their failure response is sent.
			_ = s.handleAssociation(assoc)
		}()
	}
}

func (s *Server) tryAcquireAssociationSlot() bool {
	if s == nil || s.associationSlots == nil {
		return true
	}
	select {
	case s.associationSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseAssociationSlot() {
	if s == nil || s.associationSlots == nil {
		return
	}
	select {
	case <-s.associationSlots:
	default:
	}
}

func (s *Server) handleAssociation(assoc *ul.Association) error {
	defer assoc.Close()
	opts := dimse.AssociationSCPOptions{
		StorageSCPOptions: dimse.StorageSCPOptions{
			MaxDataSetBytes: s.maxStoreObjectBytes,
			StoreHandler: dimse.CStoreHandlerFunc(func(ctx context.Context, req dimse.CStoreRequestContext) (uint16, error) {
				return s.storeCStore(ctx, assoc, req)
			}),
			OnCStoreResponse: func(_ context.Context, _ dimse.CStoreRequestContext, status uint16) {
				if status != dimse.StatusSuccess {
					s.failed.Add(1)
				}
			},
		},
		CFindHandler: dimse.CFindHandlerFunc(s.findCFind),
		CGetHandler:  dimse.CGetHandlerFunc(s.getCGet),
	}
	if s.nodeLookup != nil {
		opts.CMoveHandler = dimse.CMoveHandlerFunc(s.moveCMove)
	}
	return dimse.ServeAssociation(s.ctx, assoc, opts)
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

func (s *Server) storeCStore(ctx context.Context, assoc *ul.Association, req dimse.CStoreRequestContext) (uint16, error) {
	report, err := s.catalog.ImportObjectWithOptions(ctx, sourcePath(assoc, &req.Request), req.DataSet, req.DataSetSyntax, archive.ImportOptions{
		DecompressImages: s.decompressImages,
		Limits:           archive.ImportLimits{MaxFileImportBytes: s.maxStoreObjectBytes},
	})
	status := uint16(dimse.StatusSuccess)
	if err != nil {
		status = dimse.StatusCStoreOutOfResources
	} else if report.InvalidFiles > 0 || len(report.Rejections) > 0 {
		status = dimse.StatusCStoreCannotUnderstand
	} else {
		s.stored.Add(int64(report.StoredFiles))
		s.duplicates.Add(int64(report.Duplicates))
	}
	return status, err
}

func sourcePath(assoc *ul.Association, req *dimse.CStoreRequest) string {
	callingAE := "unknown"
	if assoc != nil && assoc.CallingAETitle != "" {
		callingAE = nodes.NormalizeAETitle(assoc.CallingAETitle)
	}
	return fmt.Sprintf("dicom://%s/%s", callingAE, req.AffectedSOPInstanceUID)
}

func acceptedAbstractSyntaxes(enableMove bool) []string {
	uids := []string{dimse.VerificationSOPClassUID, dimse.StudyRootFindSOPClassUID, dimse.StudyRootGetSOPClassUID}
	if enableMove {
		uids = append(uids, dimse.StudyRootMoveSOPClassUID)
	}
	uids = append(uids, storageSOPClassUIDs()...)
	return uids
}

func storageRoleSelections() []ul.RoleSelectionItem {
	uids := storageSOPClassUIDs()
	roles := make([]ul.RoleSelectionItem, 0, len(uids))
	for _, uid := range uids {
		roles = append(roles, ul.RoleSelectionItem{SopClassUID: uid, SCPRole: true})
	}
	return roles
}

// StorageSOPClassUIDs returns the Storage SOP Classes accepted by the receiver.
func StorageSOPClassUIDs() []string {
	return storageSOPClassUIDs()
}

func storageSOPClassUIDs() []string {
	return dimse.DefaultStorageSOPClassUIDs()
}

func TransferSyntaxUIDsForPreference(preference string) ([]string, error) {
	preference = strings.TrimSpace(preference)
	if preference == "" || strings.EqualFold(preference, PreferredTransferSyntaxAuto) {
		return transfer.ReceiveTransferSyntaxUIDs(transfer.ImplicitVRLittleEndian, transfer.ExplicitVRLittleEndian), nil
	}
	switch preference {
	case PreferredTransferSyntaxExplicitVRLittleEndian:
		return transfer.ReceiveTransferSyntaxUIDs(transfer.ExplicitVRLittleEndian, transfer.ImplicitVRLittleEndian), nil
	case PreferredTransferSyntaxImplicitVRLittleEndian:
		return transfer.ReceiveTransferSyntaxUIDs(transfer.ImplicitVRLittleEndian, transfer.ExplicitVRLittleEndian), nil
	default:
		return nil, fmt.Errorf("unsupported receive preferred transfer syntax %q", preference)
	}
}

func decompressionTransferSyntaxUIDs() []string {
	return []string{
		transfer.JPEGBaseline.UID,
		transfer.JPEGExtended.UID,
		transfer.JPEGLosslessNonHierarchical.UID,
		transfer.JPEGLosslessSV1.UID,
		transfer.RLELossless.UID,
	}
}

func appendUniqueTransferSyntaxUIDs(base []string, extra ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(extra))
	for _, uid := range append(base, extra...) {
		uid = transfer.NormalizeUID(uid)
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		out = append(out, uid)
	}
	return out
}
