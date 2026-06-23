package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/deid"
	"github.com/ThalesMMS/dicom-go/object"
)

var (
	anonTagStudyInstanceUID = core.NewTag(0x0020, 0x000D)
	anonTagSOPInstanceUID   = core.NewTag(0x0008, 0x0018)
)

type AnonymizeOutcome struct {
	SourceStudyUID string              `json:"sourceStudyUID"`
	NewStudyUID    string              `json:"newStudyUID"`
	TotalFiles     int                 `json:"totalFiles"`
	StoredFiles    int                 `json:"storedFiles"`
	Duplicates     int                 `json:"duplicates"`
	FailedFiles    int                 `json:"failedFiles"`
	Rejections     []archive.Rejection `json:"rejections,omitempty"`
}

func (s *Session) AnonymizeStudy(ctx context.Context, studyUID string) (AnonymizeOutcome, error) {
	studyUID = strings.TrimSpace(studyUID)
	out := AnonymizeOutcome{SourceStudyUID: studyUID}
	if studyUID == "" {
		return out, errors.New("study UID is required")
	}
	instances, err := s.catalog.InstancesForStudy(ctx, studyUID)
	if err != nil {
		return out, err
	}
	out.TotalFiles = len(instances)
	if len(instances) == 0 {
		return out, fmt.Errorf("study %q has no archived instances", studyUID)
	}

	tempDir, err := os.MkdirTemp("", "gopacs-anonymize-*")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(tempDir)

	uids := deid.NewUIDRemapper()
	files := make([]anonymizedPart10File, 0, len(instances))
	for i, instance := range instances {
		path, newStudyUID, source, err := writeAnonymizedPart10(tempDir, i, instance, uids)
		if err != nil {
			return out, err
		}
		if out.NewStudyUID == "" {
			out.NewStudyUID = newStudyUID
		} else if out.NewStudyUID != newStudyUID {
			return out, fmt.Errorf("anonymized study UID changed within one study: %q then %q", out.NewStudyUID, newStudyUID)
		}
		files = append(files, anonymizedPart10File{path: path, source: source})
	}

	for _, file := range files {
		report, err := s.catalog.ImportPart10File(ctx, file.path, file.source)
		out.StoredFiles += report.StoredFiles
		out.Duplicates += report.Duplicates
		out.FailedFiles += report.InvalidFiles + len(report.Rejections)
		out.Rejections = append(out.Rejections, report.Rejections...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Session) StartAnonymizeStudyJob(studyUID string) *Job {
	return s.startJob("anonymize", func(ctx context.Context, emit func(any)) (any, error) {
		return s.AnonymizeStudy(ctx, studyUID)
	})
}

type anonymizedPart10File struct {
	path   string
	source string
}

// writeAnonymizedPart10 opens a DICOM Part 10 file, anonymizes it using the provided UID remapper, and writes it to a temporary directory with a deterministic filename. It returns the output path, new Study Instance UID, and a source identifier formatted as "anonymized://<newStudyUID>/<newSOPUID>". It returns an error if the anonymized file does not contain non-empty Study Instance UID or SOP Instance UID values, or if any file operation fails.
func writeAnonymizedPart10(tempDir string, index int, instance archive.Instance, uids *deid.UIDRemapper) (string, string, string, error) {
	file, err := object.OpenFile(instance.StoredPath)
	if err != nil {
		return "", "", "", err
	}
	defer file.Close()

	clone, _, err := deid.CloneAnonymizedFile(file, deid.Options{}, uids)
	if err != nil {
		return "", "", "", err
	}
	newStudyUID, ok := clone.Dataset.GetUID(anonTagStudyInstanceUID)
	if !ok || strings.TrimSpace(newStudyUID) == "" {
		return "", "", "", errors.New("anonymized file has no Study Instance UID")
	}
	newSOPUID, ok := clone.Dataset.GetUID(anonTagSOPInstanceUID)
	if !ok || strings.TrimSpace(newSOPUID) == "" {
		return "", "", "", errors.New("anonymized file has no SOP Instance UID")
	}

	path := filepath.Join(tempDir, fmt.Sprintf("%06d.dcm", index+1))
	out, err := os.Create(path)
	if err != nil {
		return "", "", "", err
	}
	if err := object.WriteFile(out, clone); err != nil {
		_ = out.Close()
		return "", "", "", err
	}
	if err := out.Close(); err != nil {
		return "", "", "", err
	}
	return path, newStudyUID, "anonymized://" + newStudyUID + "/" + newSOPUID, nil
}
