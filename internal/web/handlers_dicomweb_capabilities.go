package web

import (
	"encoding/json"
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
)

type dicomwebCapabilitiesResponse struct {
	Service        string                          `json:"service"`
	BasePath       string                          `json:"basePath"`
	Authentication dicomwebAuthCapabilities        `json:"authentication"`
	MediaTypes     dicomwebMediaTypeCapabilities   `json:"mediaTypes"`
	Limits         dicomwebLimitCapabilities       `json:"limits"`
	Transactions   []dicomwebTransactionCapability `json:"transactions"`
	Unsupported    []string                        `json:"unsupported"`
}

type dicomwebAuthCapabilities struct {
	Required                         bool     `json:"required"`
	Scheme                           string   `json:"scheme"`
	TokenRoles                       []string `json:"tokenRoles"`
	ReadOperationsRole               string   `json:"readOperationsRole"`
	WriteOperationsRole              string   `json:"writeOperationsRole"`
	CapabilitiesEndpointRequiresAuth bool     `json:"capabilitiesEndpointRequiresAuth"`
}

type dicomwebMediaTypeCapabilities struct {
	DICOMJSON      string `json:"dicomJSON"`
	DICOM          string `json:"dicom"`
	MultipartDICOM string `json:"multipartDICOM"`
}

type dicomwebLimitCapabilities struct {
	MaxFileImportBytes      *int64 `json:"maxFileImportBytes"`
	MaxStoreObjectBytes     *int64 `json:"maxStoreObjectBytes"`
	MaxImportTotalFiles     *int   `json:"maxImportTotalFiles"`
	MaxImportPathLength     *int   `json:"maxImportPathLength"`
	MaxImportDirectoryDepth *int   `json:"maxImportDirectoryDepth"`
}

type dicomwebTransactionCapability struct {
	Name               string   `json:"name"`
	Method             string   `json:"method"`
	Path               string   `json:"path"`
	Role               string   `json:"role"`
	RequestMediaTypes  []string `json:"requestMediaTypes,omitempty"`
	ResponseMediaTypes []string `json:"responseMediaTypes,omitempty"`
	Notes              string   `json:"notes,omitempty"`
}

func (s *Server) handleDICOMwebCapabilities(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.session.LoadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(dicomwebCapabilities(cfg))
}

// dicomwebCapabilities constructs the DICOMweb service capabilities response with the provided configuration limits.
func dicomwebCapabilities(cfg appconfig.Config) dicomwebCapabilitiesResponse {
	return dicomwebCapabilitiesResponse{
		Service:  "go-pacs DICOMweb",
		BasePath: "/dicomweb",
		Authentication: dicomwebAuthCapabilities{
			Required:                         true,
			Scheme:                           "Bearer",
			TokenRoles:                       []string{"read", "write"},
			ReadOperationsRole:               "read or write",
			WriteOperationsRole:              "write",
			CapabilitiesEndpointRequiresAuth: true,
		},
		MediaTypes: dicomwebMediaTypeCapabilities{
			DICOMJSON:      "application/dicom+json",
			DICOM:          "application/dicom",
			MultipartDICOM: `multipart/related; type="application/dicom"`,
		},
		Limits: dicomwebLimitCapabilities{
			MaxFileImportBytes:      cfg.MaxFileImportBytes,
			MaxStoreObjectBytes:     cfg.MaxStoreObjectBytes,
			MaxImportTotalFiles:     cfg.MaxImportTotalFiles,
			MaxImportPathLength:     cfg.MaxImportPathLength,
			MaxImportDirectoryDepth: cfg.MaxImportDirectoryDepth,
		},
		Transactions: []dicomwebTransactionCapability{
			{
				Name:               "QIDO-RS",
				Method:             http.MethodGet,
				Path:               "/dicomweb/studies",
				Role:               "read",
				ResponseMediaTypes: []string{"application/dicom+json"},
				Notes:              "Study, series, and instance search are backed by local archive metadata.",
			},
			{
				Name:               "WADO-RS metadata",
				Method:             http.MethodGet,
				Path:               "/dicomweb/studies/{studyUID}/metadata",
				Role:               "read",
				ResponseMediaTypes: []string{"application/dicom+json"},
				Notes:              "Study, series, and instance metadata responses omit Pixel Data.",
			},
			{
				Name:               "WADO-RS retrieval",
				Method:             http.MethodGet,
				Path:               "/dicomweb/studies/{studyUID}",
				Role:               "read",
				ResponseMediaTypes: []string{"application/dicom", `multipart/related; type="application/dicom"`},
				Notes:              "Returns stored Part 10 objects without rendered image conversion.",
			},
			{
				Name:               "STOW-RS",
				Method:             http.MethodPost,
				Path:               "/dicomweb/studies",
				Role:               "write",
				RequestMediaTypes:  []string{`multipart/related; type="application/dicom"`},
				ResponseMediaTypes: []string{"application/dicom+json"},
				Notes:              "Accepted Part 10 objects are imported into the local archive with configured import limits.",
			},
		},
		Unsupported: []string{
			"WADO-URI",
			"WADO-WS",
			"UPS-RS",
			"rendered JPEG/PNG retrieval",
			"enterprise IAM integration",
		},
	}
}
