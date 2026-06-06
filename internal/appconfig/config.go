package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
)

const (
	DefaultMaxZipEntryBytes                 int64 = 2 * 1024 * 1024 * 1024
	DefaultMaxZipTotalBytes                 int64 = 50 * 1024 * 1024 * 1024
	DefaultMaxZipEntryCount                       = 100000
	DefaultMaxFileImportBytes               int64 = 2 * 1024 * 1024 * 1024
	DefaultMaxStoreObjectBytes              int64 = 2 * 1024 * 1024 * 1024
	DefaultMaxImportTotalFiles                    = 1000000
	DefaultMaxImportPathLength                    = 4096
	DefaultMaxImportDirectoryDepth                = 64
	DefaultDICOMCommunicationTimeoutSeconds       = 40
	DefaultDICOMConnectionTimeoutSeconds          = 10
)

type Config struct {
	LocalAETitle                     string                    `json:"localAETitle"`
	ReceiverAddress                  string                    `json:"receiverAddress"`
	ReceiverAutoStart                bool                      `json:"receiverAutoStart"`
	AdditionalAETitles               []string                  `json:"additionalAETitles,omitempty"`
	ReceivePreferredTransferSyntax   string                    `json:"receivePreferredTransferSyntax"`
	DICOMCommunicationTimeoutSeconds int                       `json:"dicomCommunicationTimeoutSeconds"`
	DICOMConnectionTimeoutSeconds    int                       `json:"dicomConnectionTimeoutSeconds"`
	UISortPreferences                map[string]SortPreference `json:"uiSortPreferences,omitempty"`
	OpenedArchiveStudyUIDs           []string                  `json:"openedArchiveStudyUIDs,omitempty"`
	MaxZipEntryBytes                 *int64                    `json:"max_zip_entry_bytes"`
	MaxZipTotalBytes                 *int64                    `json:"max_zip_total_bytes"`
	MaxZipEntryCount                 *int                      `json:"max_zip_entry_count"`
	MaxFileImportBytes               *int64                    `json:"max_file_import_bytes"`
	MaxStoreObjectBytes              *int64                    `json:"max_store_object_bytes"`
	MaxImportTotalFiles              *int                      `json:"max_import_total_files"`
	MaxImportPathLength              *int                      `json:"max_import_path_length"`
	MaxImportDirectoryDepth          *int                      `json:"max_import_directory_depth"`
}

type SortPreference struct {
	Column     int  `json:"column"`
	Descending bool `json:"descending"`
}

func Defaults() Config {
	return Config{
		LocalAETitle:                     netverify.DefaultCallingAETitle,
		ReceiverAddress:                  receive.DefaultAddress,
		ReceivePreferredTransferSyntax:   receive.PreferredTransferSyntaxAuto,
		DICOMCommunicationTimeoutSeconds: DefaultDICOMCommunicationTimeoutSeconds,
		DICOMConnectionTimeoutSeconds:    DefaultDICOMConnectionTimeoutSeconds,
		MaxZipEntryBytes:                 int64Ptr(DefaultMaxZipEntryBytes),
		MaxZipTotalBytes:                 int64Ptr(DefaultMaxZipTotalBytes),
		MaxZipEntryCount:                 intPtr(DefaultMaxZipEntryCount),
		MaxFileImportBytes:               int64Ptr(DefaultMaxFileImportBytes),
		MaxStoreObjectBytes:              int64Ptr(DefaultMaxStoreObjectBytes),
		MaxImportTotalFiles:              intPtr(DefaultMaxImportTotalFiles),
		MaxImportPathLength:              intPtr(DefaultMaxImportPathLength),
		MaxImportDirectoryDepth:          intPtr(DefaultMaxImportDirectoryDepth),
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read app config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return Defaults(), nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse app config: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse app config: %w", err)
	}
	applyMissingSafetyLimitDefaults(&cfg, raw)
	return Normalize(cfg)
}

func Save(path string, cfg Config) error {
	cfg, err := Normalize(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create app config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode app config: %w", err)
	}
	data = append(data, '\n')
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("write app config: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace app config: %w", err)
	}
	return nil
}

func Normalize(cfg Config) (Config, error) {
	defaults := Defaults()
	cfg.LocalAETitle = nodes.NormalizeAETitle(cfg.LocalAETitle)
	if cfg.LocalAETitle == "" {
		cfg.LocalAETitle = defaults.LocalAETitle
	}
	if err := nodes.ValidateAETitle(cfg.LocalAETitle); err != nil {
		return Config{}, fmt.Errorf("local AE title: %w", err)
	}
	cfg.ReceiverAddress = strings.TrimSpace(cfg.ReceiverAddress)
	if cfg.ReceiverAddress == "" {
		cfg.ReceiverAddress = defaults.ReceiverAddress
	}
	if _, _, err := net.SplitHostPort(cfg.ReceiverAddress); err != nil {
		return Config{}, fmt.Errorf("receiver address: %w", err)
	}
	additional, err := normalizeAdditionalAETitles(cfg.LocalAETitle, cfg.AdditionalAETitles)
	if err != nil {
		return Config{}, err
	}
	cfg.AdditionalAETitles = additional
	if err := validateOptionalInt64("max_zip_entry_bytes", cfg.MaxZipEntryBytes); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt64("max_zip_total_bytes", cfg.MaxZipTotalBytes); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt("max_zip_entry_count", cfg.MaxZipEntryCount); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt64("max_file_import_bytes", cfg.MaxFileImportBytes); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt64("max_store_object_bytes", cfg.MaxStoreObjectBytes); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt("max_import_total_files", cfg.MaxImportTotalFiles); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt("max_import_path_length", cfg.MaxImportPathLength); err != nil {
		return Config{}, err
	}
	if err := validateOptionalInt("max_import_directory_depth", cfg.MaxImportDirectoryDepth); err != nil {
		return Config{}, err
	}
	cfg.ReceivePreferredTransferSyntax = strings.TrimSpace(cfg.ReceivePreferredTransferSyntax)
	if cfg.ReceivePreferredTransferSyntax == "" {
		cfg.ReceivePreferredTransferSyntax = defaults.ReceivePreferredTransferSyntax
	}
	if _, err := receive.TransferSyntaxUIDsForPreference(cfg.ReceivePreferredTransferSyntax); err != nil {
		return Config{}, err
	}
	if cfg.DICOMCommunicationTimeoutSeconds == 0 {
		cfg.DICOMCommunicationTimeoutSeconds = defaults.DICOMCommunicationTimeoutSeconds
	}
	if cfg.DICOMConnectionTimeoutSeconds == 0 {
		cfg.DICOMConnectionTimeoutSeconds = defaults.DICOMConnectionTimeoutSeconds
	}
	if cfg.DICOMCommunicationTimeoutSeconds < 0 {
		return Config{}, fmt.Errorf("dicomCommunicationTimeoutSeconds must be greater than zero")
	}
	if cfg.DICOMConnectionTimeoutSeconds < 0 {
		return Config{}, fmt.Errorf("dicomConnectionTimeoutSeconds must be greater than zero")
	}
	cfg.OpenedArchiveStudyUIDs = normalizeStringList(cfg.OpenedArchiveStudyUIDs)
	return cfg, nil
}

func normalizeAdditionalAETitles(localAETitle string, aeTitles []string) ([]string, error) {
	seen := map[string]bool{localAETitle: true}
	var normalized []string
	for _, aeTitle := range aeTitles {
		aeTitle = nodes.NormalizeAETitle(aeTitle)
		if aeTitle == "" || seen[aeTitle] {
			continue
		}
		if err := nodes.ValidateAETitle(aeTitle); err != nil {
			return nil, fmt.Errorf("additional AE title: %w", err)
		}
		seen[aeTitle] = true
		normalized = append(normalized, aeTitle)
	}
	return normalized, nil
}

func normalizeStringList(values []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	return normalized
}

func applyMissingSafetyLimitDefaults(cfg *Config, raw map[string]json.RawMessage) {
	if _, ok := raw["max_zip_entry_bytes"]; !ok {
		cfg.MaxZipEntryBytes = int64Ptr(DefaultMaxZipEntryBytes)
	}
	if _, ok := raw["max_zip_total_bytes"]; !ok {
		cfg.MaxZipTotalBytes = int64Ptr(DefaultMaxZipTotalBytes)
	}
	if _, ok := raw["max_zip_entry_count"]; !ok {
		cfg.MaxZipEntryCount = intPtr(DefaultMaxZipEntryCount)
	}
	if _, ok := raw["max_file_import_bytes"]; !ok {
		cfg.MaxFileImportBytes = int64Ptr(DefaultMaxFileImportBytes)
	}
	if _, ok := raw["max_store_object_bytes"]; !ok {
		cfg.MaxStoreObjectBytes = int64Ptr(DefaultMaxStoreObjectBytes)
	}
	if _, ok := raw["max_import_total_files"]; !ok {
		cfg.MaxImportTotalFiles = intPtr(DefaultMaxImportTotalFiles)
	}
	if _, ok := raw["max_import_path_length"]; !ok {
		cfg.MaxImportPathLength = intPtr(DefaultMaxImportPathLength)
	}
	if _, ok := raw["max_import_directory_depth"]; !ok {
		cfg.MaxImportDirectoryDepth = intPtr(DefaultMaxImportDirectoryDepth)
	}
}

func validateOptionalInt64(name string, value *int64) error {
	if value != nil && *value <= 0 {
		return fmt.Errorf("%s must be greater than zero or null", name)
	}
	return nil
}

func validateOptionalInt(name string, value *int) error {
	if value != nil && *value <= 0 {
		return fmt.Errorf("%s must be greater than zero or null", name)
	}
	return nil
}

func int64Ptr(value int64) *int64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}
