package model

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fontExtensions is the whitelist of font file extensions recognized by the
// custom font directory scanner. Lower-case keys; matching is case-insensitive.
var fontExtensions = map[string]bool{
	".ttf":   true,
	".otf":   true,
	".woff":  true,
	".woff2": true,
	".eot":   true,
}

// FontFile describes a single discovered font file inside the configured
// custom font directory. Family is the file stem (name without extension) and
// is the CSS font-family used by the frontend @font-face loader.
type FontFile struct {
	Family  string    `json:"family"`
	File    string    `json:"file"`
	Ext     string    `json:"ext"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// DefaultFontsDir returns the default custom font directory:
// <DataDir>/fonts. Returns an empty string when DataDir is unset (not yet
// resolved).
func DefaultFontsDir() string {
	if DataDir == "" {
		return ""
	}
	return filepath.Join(DataDir, "fonts")
}

// ResolveFontsDir returns the configured custom font directory, falling back
// to the default when empty. Always call this at request time rather than
// reading Fonts.Dir directly, so an explicitly-cleared value (stored as "")
// resolves back to the default.
func (c *Config) ResolveFontsDir() string {
	if c.Fonts.Dir == "" {
		return DefaultFontsDir()
	}
	return c.Fonts.Dir
}

// IsFontFile reports whether name has a supported font file extension
// (case-insensitive).
func IsFontFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return fontExtensions[ext]
}

// fontExtPriority ranks file extensions for same-stem deduplication, so a
// single family keeps the most broadly-supported (and smallest) format.
var fontExtPriority = map[string]int{
	".woff2": 4,
	".woff":  3,
	".ttf":   2,
	".otf":   1,
	".eot":   0,
}

// ListFontFiles scans dir (non-recursively) for supported font files and
// returns one entry per unique family (file stem), sorted by family name.
// When several files share a stem (e.g. Foo.woff2 + Foo.ttf), only the best
// format wins (woff2 > woff > ttf > otf > eot). Dot-files are skipped.
// Returns an empty slice when the directory does not exist or contains no
// matching files.
func ListFontFiles(dir string) []FontFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []FontFile{}
	}

	// best per stem: prefer the highest-priority extension.
	best := make(map[string]FontFile)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !IsFontFile(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		cur, exists := best[stem]
		if !exists || fontExtPriority[ext] > fontExtPriority[cur.Ext] {
			best[stem] = FontFile{
				Family:  stem,
				File:    name,
				Ext:     ext,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			}
		}
	}

	files := make([]FontFile, 0, len(best))
	for _, f := range best {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Family < files[j].Family
	})
	return files
}
