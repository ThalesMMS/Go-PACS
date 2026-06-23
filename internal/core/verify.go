package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
)

type VerifyError struct {
	Category string `json:"category"`
	Message  string `json:"message"`
}

type VerifyResult struct {
	OK            bool          `json:"ok"`
	StudyCount    int           `json:"studyCount"`
	InstanceCount int           `json:"instanceCount"`
	ObjectCount   int           `json:"objectCount"`
	Errors        []VerifyError `json:"errors"`
}

func (s *Session) VerifyArchive(ctx context.Context) (*VerifyResult, error) {
	result := &VerifyResult{}
	addError := func(category string, format string, args ...any) {
		result.Errors = append(result.Errors, VerifyError{
			Category: category,
			Message:  fmt.Sprintf(format, args...),
		})
	}
	hardErr := func(err error) bool {
		return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	}

	if err := s.catalog.IntegrityCheck(ctx); err != nil {
		if hardErr(err) {
			return nil, err
		}
		addError("catalog", "%v", err)
	}

	studies, err := s.catalog.Studies(ctx)
	if err != nil {
		if hardErr(err) {
			return nil, err
		}
		addError("catalog", "%v", err)
	} else {
		result.StudyCount = len(studies)
	}

	paths, err := s.catalog.StoredPaths(ctx)
	if err != nil {
		if hardErr(err) {
			return nil, err
		}
		addError("catalog", "%v", err)
	} else {
		result.InstanceCount = len(paths)
		result.ObjectCount = verifyStoredObjects(s.catalog.ObjectDir(), paths, addError)
	}

	verifySidecar("config", configFileName, func() error {
		_, err := appconfig.Load(s.configPath)
		return err
	}, addError)
	verifySidecar("nodes", nodesFileName, func() error {
		_, err := s.nodeStore.List()
		return err
	}, addError)
	verifySidecar("profiles", autoQueryProfilesFileName, func() error {
		_, err := s.autoQuery.List()
		return err
	}, addError)
	verifySidecar("history", historyFileName, func() error {
		_, err := ops.LoadHistory(s.historyPath)
		return err
	}, addError)

	result.OK = len(result.Errors) == 0
	return result, nil
}

func verifySidecar(category string, name string, load func() error, addError func(string, string, ...any)) {
	if err := load(); err != nil {
		addError(category, "%s: %v", name, err)
	}
}

func verifyStoredObjects(root string, paths []string, addError func(string, string, ...any)) int {
	seen := map[string]bool{}
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			addError("storage", "catalogued object has empty stored_path")
			continue
		}
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		if !pathInside(root, trimmed) {
			addError("storage", "catalogued object is outside objects/: %s", trimmed)
			continue
		}
		info, err := os.Stat(trimmed)
		if errors.Is(err, os.ErrNotExist) {
			addError("storage", "catalogued object is missing: %s", trimmed)
			continue
		}
		if err != nil {
			addError("storage", "catalogued object cannot be read: %s: %v", trimmed, err)
			continue
		}
		if info.IsDir() {
			addError("storage", "catalogued object is a directory: %s", trimmed)
		}
	}
	return len(seen)
}

func pathInside(root string, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == "." || rel == "" || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
