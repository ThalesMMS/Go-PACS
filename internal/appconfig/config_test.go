package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
)

func TestLoadMissingConfigReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalAETitle != netverify.DefaultCallingAETitle {
		t.Fatalf("LocalAETitle = %q, want %q", cfg.LocalAETitle, netverify.DefaultCallingAETitle)
	}
	if cfg.ReceiverAddress != receive.DefaultAddress {
		t.Fatalf("ReceiverAddress = %q, want %q", cfg.ReceiverAddress, receive.DefaultAddress)
	}
	if cfg.ReceiverAutoStart {
		t.Fatal("ReceiverAutoStart = true, want false by default")
	}
	if cfg.ReceivePreferredTransferSyntax != receive.PreferredTransferSyntaxAuto {
		t.Fatalf("ReceivePreferredTransferSyntax = %q, want %q", cfg.ReceivePreferredTransferSyntax, receive.PreferredTransferSyntaxAuto)
	}
	if cfg.DICOMCommunicationTimeoutSeconds != DefaultDICOMCommunicationTimeoutSeconds {
		t.Fatalf("DICOMCommunicationTimeoutSeconds = %d, want %d", cfg.DICOMCommunicationTimeoutSeconds, DefaultDICOMCommunicationTimeoutSeconds)
	}
	if cfg.DICOMConnectionTimeoutSeconds != DefaultDICOMConnectionTimeoutSeconds {
		t.Fatalf("DICOMConnectionTimeoutSeconds = %d, want %d", cfg.DICOMConnectionTimeoutSeconds, DefaultDICOMConnectionTimeoutSeconds)
	}
	if cfg.MaxFileImportBytes == nil || *cfg.MaxFileImportBytes != DefaultMaxFileImportBytes {
		t.Fatalf("MaxFileImportBytes = %#v, want %d", cfg.MaxFileImportBytes, DefaultMaxFileImportBytes)
	}
	if cfg.MaxZipEntryBytes == nil || *cfg.MaxZipEntryBytes != DefaultMaxZipEntryBytes {
		t.Fatalf("MaxZipEntryBytes = %#v, want %d", cfg.MaxZipEntryBytes, DefaultMaxZipEntryBytes)
	}
	if cfg.MaxZipTotalBytes == nil || *cfg.MaxZipTotalBytes != DefaultMaxZipTotalBytes {
		t.Fatalf("MaxZipTotalBytes = %#v, want %d", cfg.MaxZipTotalBytes, DefaultMaxZipTotalBytes)
	}
	if cfg.MaxZipEntryCount == nil || *cfg.MaxZipEntryCount != DefaultMaxZipEntryCount {
		t.Fatalf("MaxZipEntryCount = %#v, want %d", cfg.MaxZipEntryCount, DefaultMaxZipEntryCount)
	}
	if cfg.MaxStoreObjectBytes == nil || *cfg.MaxStoreObjectBytes != DefaultMaxStoreObjectBytes {
		t.Fatalf("MaxStoreObjectBytes = %#v, want %d", cfg.MaxStoreObjectBytes, DefaultMaxStoreObjectBytes)
	}
	if cfg.MaxImportTotalFiles == nil || *cfg.MaxImportTotalFiles != DefaultMaxImportTotalFiles {
		t.Fatalf("MaxImportTotalFiles = %#v, want %d", cfg.MaxImportTotalFiles, DefaultMaxImportTotalFiles)
	}
	if cfg.MaxImportPathLength == nil || *cfg.MaxImportPathLength != DefaultMaxImportPathLength {
		t.Fatalf("MaxImportPathLength = %#v, want %d", cfg.MaxImportPathLength, DefaultMaxImportPathLength)
	}
	if cfg.MaxImportDirectoryDepth == nil || *cfg.MaxImportDirectoryDepth != DefaultMaxImportDirectoryDepth {
		t.Fatalf("MaxImportDirectoryDepth = %#v, want %d", cfg.MaxImportDirectoryDepth, DefaultMaxImportDirectoryDepth)
	}
}

func TestSaveAndLoadConfigNormalizesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{
		LocalAETitle:                     " local ",
		ReceiverAddress:                  "127.0.0.1:12345",
		ReceiverAutoStart:                true,
		AdditionalAETitles:               []string{" alias ", "LOCAL"},
		ReceivePreferredTransferSyntax:   receive.PreferredTransferSyntaxExplicitVRLittleEndian,
		DICOMCommunicationTimeoutSeconds: 55,
		DICOMConnectionTimeoutSeconds:    12,
		MaxFileImportBytes:               int64Ptr(123),
		MaxZipEntryBytes:                 int64Ptr(234),
		MaxZipTotalBytes:                 int64Ptr(345),
		MaxZipEntryCount:                 intPtr(4),
		MaxStoreObjectBytes:              int64Ptr(456),
		MaxImportTotalFiles:              intPtr(5),
		MaxImportPathLength:              intPtr(6),
		MaxImportDirectoryDepth:          intPtr(7),
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocalAETitle != "LOCAL" {
		t.Fatalf("LocalAETitle = %q, want LOCAL", loaded.LocalAETitle)
	}
	if loaded.ReceiverAddress != "127.0.0.1:12345" {
		t.Fatalf("ReceiverAddress = %q, want 127.0.0.1:12345", loaded.ReceiverAddress)
	}
	if !loaded.ReceiverAutoStart {
		t.Fatal("ReceiverAutoStart = false, want true")
	}
	if got, want := loaded.AdditionalAETitles, []string{"ALIAS"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("AdditionalAETitles = %#v, want %#v", got, want)
	}
	if loaded.ReceivePreferredTransferSyntax != receive.PreferredTransferSyntaxExplicitVRLittleEndian {
		t.Fatalf("ReceivePreferredTransferSyntax = %q, want %q", loaded.ReceivePreferredTransferSyntax, receive.PreferredTransferSyntaxExplicitVRLittleEndian)
	}
	if loaded.DICOMCommunicationTimeoutSeconds != 55 {
		t.Fatalf("DICOMCommunicationTimeoutSeconds = %d, want 55", loaded.DICOMCommunicationTimeoutSeconds)
	}
	if loaded.DICOMConnectionTimeoutSeconds != 12 {
		t.Fatalf("DICOMConnectionTimeoutSeconds = %d, want 12", loaded.DICOMConnectionTimeoutSeconds)
	}
	if loaded.MaxFileImportBytes == nil || *loaded.MaxFileImportBytes != 123 {
		t.Fatalf("MaxFileImportBytes = %#v, want 123", loaded.MaxFileImportBytes)
	}
	if loaded.MaxZipEntryBytes == nil || *loaded.MaxZipEntryBytes != 234 {
		t.Fatalf("MaxZipEntryBytes = %#v, want 234", loaded.MaxZipEntryBytes)
	}
	if loaded.MaxZipTotalBytes == nil || *loaded.MaxZipTotalBytes != 345 {
		t.Fatalf("MaxZipTotalBytes = %#v, want 345", loaded.MaxZipTotalBytes)
	}
	if loaded.MaxZipEntryCount == nil || *loaded.MaxZipEntryCount != 4 {
		t.Fatalf("MaxZipEntryCount = %#v, want 4", loaded.MaxZipEntryCount)
	}
	if loaded.MaxStoreObjectBytes == nil || *loaded.MaxStoreObjectBytes != 456 {
		t.Fatalf("MaxStoreObjectBytes = %#v, want 456", loaded.MaxStoreObjectBytes)
	}
	if loaded.MaxImportTotalFiles == nil || *loaded.MaxImportTotalFiles != 5 {
		t.Fatalf("MaxImportTotalFiles = %#v, want 5", loaded.MaxImportTotalFiles)
	}
	if loaded.MaxImportPathLength == nil || *loaded.MaxImportPathLength != 6 {
		t.Fatalf("MaxImportPathLength = %#v, want 6", loaded.MaxImportPathLength)
	}
	if loaded.MaxImportDirectoryDepth == nil || *loaded.MaxImportDirectoryDepth != 7 {
		t.Fatalf("MaxImportDirectoryDepth = %#v, want 7", loaded.MaxImportDirectoryDepth)
	}
}

func TestSaveRejectsInvalidLocalAETitle(t *testing.T) {
	err := Save(filepath.Join(t.TempDir(), "config.json"), Config{
		LocalAETitle:    "LOCAL_AE",
		ReceiverAddress: receive.DefaultAddress,
	})
	if err == nil {
		t.Fatal("Save accepted invalid local AE title")
	}
}

func TestLoadBackfillsMissingSafetyLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "localAETitle": "LOCAL",
  "receiverAddress": "127.0.0.1:11112"
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxFileImportBytes == nil || *cfg.MaxFileImportBytes != DefaultMaxFileImportBytes {
		t.Fatalf("MaxFileImportBytes = %#v, want %d", cfg.MaxFileImportBytes, DefaultMaxFileImportBytes)
	}
	if cfg.MaxZipEntryCount == nil || *cfg.MaxZipEntryCount != DefaultMaxZipEntryCount {
		t.Fatalf("MaxZipEntryCount = %#v, want %d", cfg.MaxZipEntryCount, DefaultMaxZipEntryCount)
	}
	if cfg.MaxImportDirectoryDepth == nil || *cfg.MaxImportDirectoryDepth != DefaultMaxImportDirectoryDepth {
		t.Fatalf("MaxImportDirectoryDepth = %#v, want %d", cfg.MaxImportDirectoryDepth, DefaultMaxImportDirectoryDepth)
	}
	if cfg.DICOMCommunicationTimeoutSeconds != DefaultDICOMCommunicationTimeoutSeconds {
		t.Fatalf("DICOMCommunicationTimeoutSeconds = %d, want %d", cfg.DICOMCommunicationTimeoutSeconds, DefaultDICOMCommunicationTimeoutSeconds)
	}
	if cfg.DICOMConnectionTimeoutSeconds != DefaultDICOMConnectionTimeoutSeconds {
		t.Fatalf("DICOMConnectionTimeoutSeconds = %d, want %d", cfg.DICOMConnectionTimeoutSeconds, DefaultDICOMConnectionTimeoutSeconds)
	}
	if cfg.ReceivePreferredTransferSyntax != receive.PreferredTransferSyntaxAuto {
		t.Fatalf("ReceivePreferredTransferSyntax = %q, want %q", cfg.ReceivePreferredTransferSyntax, receive.PreferredTransferSyntaxAuto)
	}
}

func TestSaveRejectsInvalidDICOMTimeouts(t *testing.T) {
	err := Save(filepath.Join(t.TempDir(), "config.json"), Config{
		LocalAETitle:                     "LOCAL",
		ReceiverAddress:                  receive.DefaultAddress,
		DICOMCommunicationTimeoutSeconds: -1,
		DICOMConnectionTimeoutSeconds:    10,
	})
	if err == nil {
		t.Fatal("Save accepted invalid DICOM communication timeout")
	}

	err = Save(filepath.Join(t.TempDir(), "config.json"), Config{
		LocalAETitle:                     "LOCAL",
		ReceiverAddress:                  receive.DefaultAddress,
		DICOMCommunicationTimeoutSeconds: 40,
		DICOMConnectionTimeoutSeconds:    -1,
	})
	if err == nil {
		t.Fatal("Save accepted invalid DICOM connection timeout")
	}
}

func TestSaveRejectsInvalidReceivePreferredTransferSyntax(t *testing.T) {
	err := Save(filepath.Join(t.TempDir(), "config.json"), Config{
		LocalAETitle:                   "LOCAL",
		ReceiverAddress:                receive.DefaultAddress,
		ReceivePreferredTransferSyntax: "1.2.3.4",
	})
	if err == nil {
		t.Fatal("Save accepted invalid receive preferred transfer syntax")
	}
}

func TestLoadPreservesNullSafetyLimitsAsUnlimited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "localAETitle": "LOCAL",
  "receiverAddress": "127.0.0.1:11112",
  "max_file_import_bytes": null,
  "max_zip_entry_bytes": null,
  "max_zip_total_bytes": null,
  "max_zip_entry_count": null,
  "max_store_object_bytes": null,
  "max_import_total_files": null,
  "max_import_path_length": null,
  "max_import_directory_depth": null
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxFileImportBytes != nil {
		t.Fatalf("MaxFileImportBytes = %#v, want nil", cfg.MaxFileImportBytes)
	}
	if cfg.MaxZipEntryBytes != nil {
		t.Fatalf("MaxZipEntryBytes = %#v, want nil", cfg.MaxZipEntryBytes)
	}
	if cfg.MaxZipTotalBytes != nil {
		t.Fatalf("MaxZipTotalBytes = %#v, want nil", cfg.MaxZipTotalBytes)
	}
	if cfg.MaxZipEntryCount != nil {
		t.Fatalf("MaxZipEntryCount = %#v, want nil", cfg.MaxZipEntryCount)
	}
	if cfg.MaxStoreObjectBytes != nil {
		t.Fatalf("MaxStoreObjectBytes = %#v, want nil", cfg.MaxStoreObjectBytes)
	}
	if cfg.MaxImportTotalFiles != nil {
		t.Fatalf("MaxImportTotalFiles = %#v, want nil", cfg.MaxImportTotalFiles)
	}
	if cfg.MaxImportPathLength != nil {
		t.Fatalf("MaxImportPathLength = %#v, want nil", cfg.MaxImportPathLength)
	}
	if cfg.MaxImportDirectoryDepth != nil {
		t.Fatalf("MaxImportDirectoryDepth = %#v, want nil", cfg.MaxImportDirectoryDepth)
	}
}
