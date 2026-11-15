package kb

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Directory import bounds. A folder can be a whole repository, so both the
// depth and the number of copied files are capped; hitting a cap is reported
// back (`Truncated`) rather than silently dropping the rest.
const (
	maxDirImportFiles = 500
	maxDirImportDepth = 12
)

// DirImportResult summarizes what a folder import did.
//
// Skipped files are ones the knowledge base cannot index (images, archives,
// legacy Office, oversized, hidden…); failed ones hit an I/O or database error
// and are worth retrying. Duplicates already exist in the collection.
type DirImportResult struct {
	Added      int  `json:"added"`
	Duplicates int  `json:"duplicates"`
	Skipped    int  `json:"skipped"`
	Failed     int  `json:"failed"`
	Truncated  bool `json:"truncated"`
}

// dirImportSkip lists directory names that never hold knowledge the user means
// to import and would otherwise dominate the walk (dependency caches, build
// output, VCS metadata). Matched by name at any depth.
//
// This is deliberately its own list instead of the handler's search-pruning
// set: that one is tuned for keystroke latency and its comments explain why
// `.clawbench` must stay walkable there. Import has the opposite priority —
// one bad pick should not copy thousands of dependency files.
var dirImportSkip = map[string]bool{
	".git": true, ".svn": true, ".hg": true,
	"node_modules": true, "vendor": true, "bower_components": true,
	".venv": true, "venv": true, "__pycache__": true, ".pytest_cache": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true,
	"dist": true, "target": true, ".gradle": true, "Pods": true, ".dart_tool": true,
	".cache": true, "__MACOSX": true,
}

// ImportDirectory copies every supported file under dirPath into the
// collection. Symlinks and hidden entries are ignored: the former can escape
// the chosen folder (or loop), the latter are tool state (`.git`, `.env`) the
// user did not mean to publish into a knowledge base.
func ImportDirectory(collectionID int64, dirPath string) (*DirImportResult, error) {
	return importDirectory(collectionID, dirPath, false)
}

// PreviewDirectory reports what ImportDirectory would do without copying or
// writing anything. The panel uses it to show the counts before a large folder
// is pulled in.
func PreviewDirectory(collectionID int64, dirPath string) (*DirImportResult, error) {
	return importDirectory(collectionID, dirPath, true)
}

func importDirectory(collectionID int64, dirPath string, dryRun bool) (*DirImportResult, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	if _, err := st.GetCollection(collectionID); err != nil {
		return nil, err
	}
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, fmt.Errorf("read folder: %w", err)
	}
	if !info.IsDir() {
		return nil, ErrNotDirectory
	}

	files, truncated, err := collectDirFiles(dirPath)
	if err != nil {
		return nil, err
	}
	res := importFiles(collectionID, files, dryRun)
	res.Truncated = truncated
	return res, nil
}

// collectDirFiles walks the tree and returns the regular files worth trying,
// capped at maxDirImportFiles. `truncated` says the cap actually cut something
// off (a full cap alone does not mean there was more).
func collectDirFiles(root string) ([]string, bool, error) {
	root = filepath.Clean(root)
	var files []string
	truncated := false
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry is skipped; one locked file must not abort
			// the rest of the folder.
			//nolint:nilerr // skipping unreadable entries is the intent
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return fs.SkipDir
			}
			depth := strings.Count(rel, string(filepath.Separator)) + 1
			if depth > maxDirImportDepth || strings.HasPrefix(name, ".") || dirImportSkip[name] {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(name, ".") {
			return nil
		}
		if len(files) >= maxDirImportFiles {
			truncated = true
			return fs.SkipAll
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return nil, false, fmt.Errorf("walk folder: %w", walkErr)
	}
	return files, truncated, nil
}

// importFiles copies each path into the collection, classifying per-file
// outcomes. One bad file never stops the batch.
func importFiles(collectionID int64, files []string, dryRun bool) *DirImportResult {
	res := &DirImportResult{}
	st := currentStore()
	for _, path := range files {
		fi, statErr := os.Stat(path)
		if statErr != nil {
			res.Failed++
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		if vErr := ValidateFile(path, ext, fi.Size(), maxFileBytes()); vErr != nil {
			if errors.Is(vErr, ErrUnsupportedFile) || errors.Is(vErr, ErrFileTooLarge) {
				res.Skipped++
			} else {
				res.Failed++
			}
			continue
		}
		if dryRun {
			// Read-only preview: hash the file and ask whether that content is
			// already in the collection. Nothing is copied.
			hash, hashErr := hashFile(path)
			if hashErr != nil {
				res.Failed++
				continue
			}
			existing, findErr := st.FindDocumentByHash(collectionID, hash)
			if findErr != nil {
				res.Failed++
				continue
			}
			if existing != nil {
				res.Duplicates++
			} else {
				res.Added++
			}
			continue
		}
		if _, addErr := AddDocumentFromPath(collectionID, path); addErr != nil {
			switch {
			case errors.Is(addErr, ErrDuplicateDocument):
				res.Duplicates++
			case errors.Is(addErr, ErrUnsupportedFile),
				errors.Is(addErr, ErrFileTooLarge),
				errors.Is(addErr, ErrNoText):
				res.Skipped++
			default:
				res.Failed++
			}
			continue
		}
		res.Added++
	}
	return res
}
