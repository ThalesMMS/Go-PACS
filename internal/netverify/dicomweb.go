package netverify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/net/dicomweb"
)

const (
	CredentialTypeNone   = ""
	CredentialTypeBasic  = "basic"
	CredentialTypeBearer = "bearer"
)

type Credential struct {
	Type     string
	Username string
	Password string
	Token    string
}

type CredentialResolver interface {
	Resolve(ref string) (Credential, error)
}

type NoOpCredentialResolver struct{}

func (NoOpCredentialResolver) Resolve(string) (Credential, error) {
	return Credential{}, nil
}

type VerifyResult struct {
	NodeName   string        `json:"nodeName"`
	BaseURL    string        `json:"baseURL"`
	URL        string        `json:"url,omitempty"`
	StatusCode int           `json:"statusCode,omitempty"`
	Status     string        `json:"status,omitempty"`
	Success    bool          `json:"success"`
	Duration   time.Duration `json:"duration"`
	StartedAt  time.Time     `json:"startedAt"`
}

func VerifyDICOMweb(ctx context.Context, node nodes.Node, resolver CredentialResolver) (VerifyResult, error) {
	if !node.IsDICOMweb() {
		return VerifyResult{}, errors.New("node is not a DICOMweb profile")
	}
	if resolver == nil {
		resolver = NoOpCredentialResolver{}
	}
	credential, err := resolver.Resolve(strings.TrimSpace(node.CredentialRef))
	if err != nil {
		return VerifyResult{NodeName: node.Name, BaseURL: node.BaseURL}, fmt.Errorf("resolve DICOMweb credential for %s: %w", node.Name, err)
	}
	opts, err := optionsForCredential(credential)
	if err != nil {
		return VerifyResult{NodeName: node.Name, BaseURL: node.BaseURL}, err
	}
	opts.Timeout = DefaultDialTimeout

	client := dicomweb.Client{
		Endpoint: dicomweb.Endpoint{
			BaseURL:  strings.TrimSpace(node.BaseURL),
			QIDOPath: nodes.NormalizeDICOMwebPathPrefix(node.QIDOPathPrefix),
			WADOPath: nodes.NormalizeDICOMwebPathPrefix(node.WADOPathPrefix),
			STOWPath: nodes.NormalizeDICOMwebPathPrefix(node.STOWPathPrefix),
		},
		Options: opts,
	}
	verify, err := client.Verify(ctx)
	result := VerifyResult{
		NodeName:   node.Name,
		BaseURL:    strings.TrimSpace(node.BaseURL),
		URL:        verify.URL,
		StatusCode: verify.StatusCode,
		Status:     verify.Status,
		Success:    err == nil,
		Duration:   verify.Duration,
		StartedAt:  verify.StartedAt,
	}
	if err != nil {
		return result, fmt.Errorf("verify DICOMweb with %s (%s): %w", node.Name, node.BaseURL, err)
	}
	return result, nil
}

func optionsForCredential(credential Credential) (dicomweb.Options, error) {
	switch strings.ToLower(strings.TrimSpace(credential.Type)) {
	case CredentialTypeNone:
		return dicomweb.Options{}, nil
	case CredentialTypeBasic:
		return dicomweb.Options{
			BasicUsername: strings.TrimSpace(credential.Username),
			BasicPassword: credential.Password,
		}, nil
	case CredentialTypeBearer:
		return dicomweb.Options{BearerToken: strings.TrimSpace(credential.Token)}, nil
	default:
		return dicomweb.Options{}, fmt.Errorf("unsupported DICOMweb credential type %q", credential.Type)
	}
}
