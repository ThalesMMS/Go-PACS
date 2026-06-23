package nodes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/jsonstore"
	"github.com/google/uuid"
)

type Node struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	AETitle                  string `json:"aeTitle"`
	Host                     string `json:"host"`
	Port                     uint16 `json:"port"`
	Disabled                 bool   `json:"disabled,omitempty"`
	QueryDisabled            bool   `json:"queryDisabled,omitempty"`
	SendDisabled             bool   `json:"sendDisabled,omitempty"`
	RetrieveMethod           string `json:"retrieveMethod,omitempty"`
	SendTransferSyntax       string `json:"sendTransferSyntax,omitempty"`
	UseTLS                   bool   `json:"useTLS,omitempty"`
	TLSSkipVerify            bool   `json:"tlsSkipVerify,omitempty"`
	TLSServerName            string `json:"tlsServerName,omitempty"`
	TLSCAFile                string `json:"tlsCAFile,omitempty"`
	TLSCertFile              string `json:"tlsCertFile,omitempty"`
	TLSKeyFile               string `json:"tlsKeyFile,omitempty"`
	PreferredMoveDestination string `json:"preferredMoveDestination,omitempty"`
	Notes                    string `json:"notes,omitempty"`
	CreatedAt                string `json:"createdAt"`
	UpdatedAt                string `json:"updatedAt"`
}

type Draft struct {
	Name                     string
	AETitle                  string
	Host                     string
	Port                     uint16
	Disabled                 bool
	QueryDisabled            bool
	SendDisabled             bool
	RetrieveMethod           string
	SendTransferSyntax       string
	UseTLS                   bool
	TLSSkipVerify            bool
	TLSServerName            string
	TLSCAFile                string
	TLSCertFile              string
	TLSKeyFile               string
	PreferredMoveDestination string
	Notes                    string
}

const (
	RetrieveMethodAuto = "Auto"
	RetrieveMethodMove = "C-MOVE"
	RetrieveMethodGet  = "C-GET"

	SendTransferSyntaxAuto                   = "Auto"
	SendTransferSyntaxExplicitVRLittleEndian = "1.2.840.10008.1.2.1"
	SendTransferSyntaxImplicitVRLittleEndian = "1.2.840.10008.1.2"
)

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) List() ([]Node, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read nodes config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var nodes []Node
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, &jsonstore.LoadError{Err: fmt.Errorf("parse nodes config: %w", err), BackupExists: jsonstore.CheckBackupExists(s.path)}
	}
	return nodes, nil
}

func (s *Store) Add(draft Draft) (Node, error) {
	node, err := NewNode(draft)
	if err != nil {
		return Node{}, err
	}
	existing, err := s.List()
	if err != nil {
		return Node{}, err
	}
	for _, item := range existing {
		if strings.EqualFold(item.Name, node.Name) {
			return Node{}, fmt.Errorf("node name %q already exists", node.Name)
		}
	}
	existing = append(existing, node)
	if err := s.Save(existing); err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *Store) Update(id string, draft Draft) (Node, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Node{}, errors.New("node ID cannot be empty")
	}
	node, err := NewNode(draft)
	if err != nil {
		return Node{}, err
	}
	existing, err := s.List()
	if err != nil {
		return Node{}, err
	}
	index := -1
	for i, item := range existing {
		if item.ID == id {
			index = i
			continue
		}
		if strings.EqualFold(item.Name, node.Name) {
			return Node{}, fmt.Errorf("node name %q already exists", node.Name)
		}
	}
	if index < 0 {
		return Node{}, fmt.Errorf("node ID %q not found", id)
	}
	node.ID = existing[index].ID
	node.CreatedAt = existing[index].CreatedAt
	node.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	existing[index] = node
	if err := s.Save(existing); err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *Store) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("node ID cannot be empty")
	}
	existing, err := s.List()
	if err != nil {
		return err
	}
	next := make([]Node, 0, len(existing))
	found := false
	for _, node := range existing {
		if node.ID == id {
			found = true
			continue
		}
		next = append(next, node)
	}
	if !found {
		return fmt.Errorf("node ID %q not found", id)
	}
	return s.Save(next)
}

func (s *Store) Save(nodes []Node) error {
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return fmt.Errorf("encode nodes config: %w", err)
	}
	data = append(data, '\n')
	if err := jsonstore.WriteWithBackup(s.path, data); err != nil {
		return fmt.Errorf("write nodes config: %w", err)
	}
	return nil
}

func (s *Store) RecoverFromBackup() error {
	return jsonstore.RecoverFromBackup(s.path)
}

func NewNode(draft Draft) (Node, error) {
	name := NormalizeNodeName(draft.Name)
	aeTitle := NormalizeAETitle(draft.AETitle)
	host := strings.TrimSpace(draft.Host)
	moveDestination := NormalizeAETitle(draft.PreferredMoveDestination)
	retrieveMethod, err := NormalizeRetrieveMethod(draft.RetrieveMethod)
	if err != nil {
		return Node{}, err
	}
	sendTransferSyntax, err := NormalizeSendTransferSyntax(draft.SendTransferSyntax)
	if err != nil {
		return Node{}, err
	}
	tlsCertFile := strings.TrimSpace(draft.TLSCertFile)
	tlsKeyFile := strings.TrimSpace(draft.TLSKeyFile)
	if (tlsCertFile == "") != (tlsKeyFile == "") {
		return Node{}, errors.New("TLS client certificate and key must be provided together")
	}

	if name == "" {
		return Node{}, errors.New("node name cannot be empty")
	}
	if err := ValidateAETitle(aeTitle); err != nil {
		return Node{}, err
	}
	if host == "" {
		return Node{}, errors.New("node host cannot be empty")
	}
	if draft.Port == 0 {
		return Node{}, errors.New("node port must be between 1 and 65535")
	}
	if moveDestination != "" {
		if err := ValidateAETitle(moveDestination); err != nil {
			return Node{}, fmt.Errorf("move destination: %w", err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Node{
		ID:                       uuid.NewString(),
		Name:                     name,
		AETitle:                  aeTitle,
		Host:                     host,
		Port:                     draft.Port,
		Disabled:                 draft.Disabled,
		QueryDisabled:            draft.QueryDisabled,
		SendDisabled:             draft.SendDisabled,
		RetrieveMethod:           retrieveMethod,
		SendTransferSyntax:       sendTransferSyntax,
		UseTLS:                   draft.UseTLS,
		TLSSkipVerify:            draft.TLSSkipVerify,
		TLSServerName:            strings.TrimSpace(draft.TLSServerName),
		TLSCAFile:                strings.TrimSpace(draft.TLSCAFile),
		TLSCertFile:              tlsCertFile,
		TLSKeyFile:               tlsKeyFile,
		PreferredMoveDestination: moveDestination,
		Notes:                    strings.TrimSpace(draft.Notes),
		CreatedAt:                now,
		UpdatedAt:                now,
	}, nil
}

// Key returns a stable identity string for the node, preferring its ID and
// falling back to its endpoint. It is the canonical key for indexing per-node
// state (verify statuses, query-source failures, …) so every frontend keys the
// same node identically.
func (n Node) Key() string {
	if id := strings.TrimSpace(n.ID); id != "" {
		return "id:" + id
	}
	return fmt.Sprintf("endpoint:%s:%s:%d", strings.TrimSpace(n.Name), strings.TrimSpace(n.Host), n.Port)
}

func (n Node) Enabled() bool {
	return !n.Disabled
}

func (n Node) QueryEnabled() bool {
	return !n.QueryDisabled
}

func (n Node) SendEnabled() bool {
	return !n.SendDisabled
}

func (n Node) RetrieveMethodOrDefault() string {
	method, err := NormalizeRetrieveMethod(n.RetrieveMethod)
	if err != nil || method == "" {
		return RetrieveMethodAuto
	}
	return method
}

func (n Node) SendTransferSyntaxOrDefault() string {
	syntax, err := NormalizeSendTransferSyntax(n.SendTransferSyntax)
	if err != nil || syntax == "" {
		return SendTransferSyntaxAuto
	}
	return syntax
}

func FindByAETitle(nodeList []Node, aeTitle string) (Node, bool) {
	for _, node := range nodeList {
		if node.AETitle == aeTitle {
			return node, true
		}
	}
	return Node{}, false
}

// CallingAETitles returns the de-duplicated, normalized AE titles of the given
// nodes. It is the canonical source for a receiver's Calling-AE allowlist, so
// every frontend and the headless receiver derive the same set.
func CallingAETitles(nodeList []Node) []string {
	seen := map[string]bool{}
	var aeTitles []string
	for _, node := range nodeList {
		aeTitle := NormalizeAETitle(node.AETitle)
		if aeTitle == "" || seen[aeTitle] {
			continue
		}
		seen[aeTitle] = true
		aeTitles = append(aeTitles, aeTitle)
	}
	return aeTitles
}

type RemoteHostAllowlistResult struct {
	Hosts    []string
	Warnings []string
}

func RemoteHostAllowlist(nodeList []Node) RemoteHostAllowlistResult {
	return remoteHostAllowlist(nodeList, net.LookupIP)
}

func remoteHostAllowlist(nodeList []Node, lookupIP func(string) ([]net.IP, error)) RemoteHostAllowlistResult {
	if lookupIP == nil {
		lookupIP = net.LookupIP
	}
	seen := map[string]bool{}
	var result RemoteHostAllowlistResult
	for _, node := range nodeList {
		host := strings.Trim(strings.TrimSpace(node.Host), "[]")
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			result.Hosts = appendRemoteHost(result.Hosts, seen, ip)
			continue
		}
		ips, err := lookupIP(host)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("resolve node host %q: %v", host, err))
			continue
		}
		if len(ips) == 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("resolve node host %q: no IP addresses", host))
			continue
		}
		for _, ip := range ips {
			result.Hosts = appendRemoteHost(result.Hosts, seen, ip)
		}
	}
	return result
}

func appendRemoteHost(hosts []string, seen map[string]bool, ip net.IP) []string {
	if ip == nil {
		return hosts
	}
	host := ip.String()
	if host == "<nil>" || seen[host] {
		return hosts
	}
	seen[host] = true
	return append(hosts, host)
}

func NormalizeNodeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func NormalizeRetrieveMethod(method string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "", "AUTO":
		return "", nil
	case "C-MOVE", "MOVE":
		return RetrieveMethodMove, nil
	case "C-GET", "GET":
		return RetrieveMethodGet, nil
	default:
		return "", fmt.Errorf("retrieve method must be %s, %s, or %s", RetrieveMethodAuto, RetrieveMethodMove, RetrieveMethodGet)
	}
}

func NormalizeSendTransferSyntax(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", SendTransferSyntaxAuto:
		return "", nil
	case SendTransferSyntaxExplicitVRLittleEndian:
		return SendTransferSyntaxExplicitVRLittleEndian, nil
	case SendTransferSyntaxImplicitVRLittleEndian:
		return SendTransferSyntaxImplicitVRLittleEndian, nil
	}
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "EXPLICIT VR LITTLE ENDIAN", "EXPLICIT LITTLE ENDIAN":
		return SendTransferSyntaxExplicitVRLittleEndian, nil
	case "IMPLICIT VR LITTLE ENDIAN", "IMPLICIT LITTLE ENDIAN":
		return SendTransferSyntaxImplicitVRLittleEndian, nil
	default:
		return "", fmt.Errorf("send transfer syntax must be %s, Explicit VR Little Endian, or Implicit VR Little Endian", SendTransferSyntaxAuto)
	}
}

func NormalizeAETitle(aeTitle string) string {
	return strings.ToUpper(strings.TrimSpace(aeTitle))
}

func ValidateAETitle(aeTitle string) error {
	trimmed := strings.TrimSpace(aeTitle)
	if trimmed == "" {
		return errors.New("AE title cannot be empty")
	}
	if aeTitle != trimmed {
		return errors.New("AE title cannot have leading or trailing whitespace")
	}
	if len([]rune(aeTitle)) > 16 {
		return errors.New("AE title must be at most 16 characters")
	}
	for _, r := range aeTitle {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			continue
		}
		return fmt.Errorf("AE title contains invalid character %q; allowed: A-Z, 0-9, space", r)
	}
	return nil
}
