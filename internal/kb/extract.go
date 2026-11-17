package kb

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Location is a structured position inside the source file. The label is
// deliberately language-neutral: the panel formats it for the current UI
// language, while the API and the AI prompt get a stable short form ("p.12",
// "slide 3", "sheet 报价 · rows 1-50", "L1-L80", "¶1-¶6").
type Location struct {
	Kind  string // page | slide | sheet | line | paragraph | title
	Start int
	End   int
	Name  string // sheet name
}

// Label renders the neutral short form.
func (l Location) Label() string {
	switch l.Kind {
	case "page":
		return "p." + strconv.Itoa(l.Start)
	case "slide":
		return "slide " + strconv.Itoa(l.Start)
	case "sheet":
		if l.Start == l.End {
			return fmt.Sprintf("sheet %s · row %d", l.Name, l.Start)
		}
		return fmt.Sprintf("sheet %s · rows %d-%d", l.Name, l.Start, l.End)
	case "line":
		if l.Start == l.End {
			return "L" + strconv.Itoa(l.Start)
		}
		return fmt.Sprintf("L%d-L%d", l.Start, l.End)
	case "paragraph":
		if l.Start == l.End {
			return "¶" + strconv.Itoa(l.Start)
		}
		return fmt.Sprintf("¶%d-¶%d", l.Start, l.End)
	case locTitle:
		return locTitle
	}
	return ""
}

// Block is one extractor-produced unit of text with a position. Blocks are the
// granularity at which a location stays accurate: one PDF page, one slide, one
// sheet row range, one paragraph range.
type Block struct {
	Text     string
	Location Location
}

// Reasons carried by FileError. They are stable codes, not display text: the
// HTTP layer maps each to an i18n key so the message follows the UI language.
const (
	ReasonLegacyDoc     = "legacy_doc"
	ReasonLegacyXls     = "legacy_xls"
	ReasonLegacyPpt     = "legacy_ppt"
	ReasonImage         = "image"
	ReasonSVG           = "svg"
	ReasonAudioVideo    = "audio_video"
	ReasonArchive       = "archive"
	ReasonExecutable    = "executable"
	ReasonBinary        = "binary"
	ReasonFont          = "font"
	ReasonPDFEncrypted  = "pdf_encrypted"
	ReasonPDFMalformed  = "pdf_malformed"
	ReasonPDFNoText     = "pdf_no_text"
	ReasonNoText        = "no_text"
	ReasonFileTooLarge  = "file_too_large"
	ReasonNotRegular    = "not_regular_file"
	ReasonUnknownFormat = "unknown_format"
)

// WarningTruncated is stored in kb_documents.warning when the extracted text
// exceeded the configured cap. The panel translates it.
const WarningTruncated = "truncated"

const locTitle = "title"

// FileError carries a machine-readable reason plus an optional non-localized
// detail (the extension, the size). Errors.Is still sees the sentinel in Err.
type FileError struct {
	Reason string
	Detail string
	Err    error
}

func (e *FileError) Error() string {
	if e.Detail != "" {
		return e.Reason + ": " + e.Detail
	}
	return e.Reason
}

func (e *FileError) Unwrap() error { return e.Err }

// textExtensions are indexed by content as text even when the extension is
// unfamiliar; this list only lets us skip the binary sniff for common ones.
var textExtensions = map[string]bool{
	".txt": true, ".text": true, ".md": true, ".markdown": true, ".mdx": true,
	".json": true, ".jsonl": true, ".csv": true, ".tsv": true, ".yaml": true, ".yml": true,
	".xml": true, ".html": true, ".htm": true, ".xhtml": true, ".log": true,
	".go": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".jsx": true, ".vue": true, ".svelte": true, ".py": true, ".rb": true, ".php": true,
	".java": true, ".kt": true, ".kts": true, ".scala": true, ".groovy": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true, ".m": true, ".mm": true,
	".rs": true, ".swift": true, ".dart": true, ".lua": true, ".pl": true, ".r": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".ps1": true, ".bat": true,
	".sql": true, ".toml": true, ".ini": true, ".conf": true, ".cfg": true,
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".gradle": true, ".properties": true, ".env": true, ".gitignore": true,
	".dockerfile": true, ".makefile": true, ".cmake": true, ".proto": true,
	".graphql": true, ".gql": true, ".ex": true, ".exs": true, ".erl": true, ".hs": true,
	".clj": true, ".cljs": true, ".sol": true, ".tf": true, ".hcl": true, ".tex": true,
	".rtf": true,
}

// unsupportedExtensions maps a format with no extractable text (or a legacy
// binary Office format we deliberately do not parse) to its reason code.
var unsupportedExtensions = map[string]string{
	".doc":   ReasonLegacyDoc,
	".xls":   ReasonLegacyXls,
	".ppt":   ReasonLegacyPpt,
	".png":   ReasonImage,
	".jpg":   ReasonImage,
	".jpeg":  ReasonImage,
	".gif":   ReasonImage,
	".bmp":   ReasonImage,
	".webp":  ReasonImage,
	".svg":   ReasonSVG,
	".mp3":   ReasonAudioVideo,
	".wav":   ReasonAudioVideo,
	".m4a":   ReasonAudioVideo,
	".mp4":   ReasonAudioVideo,
	".mov":   ReasonAudioVideo,
	".mkv":   ReasonAudioVideo,
	".zip":   ReasonArchive,
	".rar":   ReasonArchive,
	".7z":    ReasonArchive,
	".tar":   ReasonArchive,
	".gz":    ReasonArchive,
	".exe":   ReasonExecutable,
	".dll":   ReasonExecutable,
	".so":    ReasonExecutable,
	".dylib": ReasonExecutable,
	".bin":   ReasonBinary,
	".ttf":   ReasonFont,
	".otf":   ReasonFont,
	".woff":  ReasonFont,
	".woff2": ReasonFont,
}

// ValidateFile performs the cheap up-front checks so an obviously unusable
// file is rejected at add time rather than after being copied.
func ValidateFile(path, ext string, size, maxFileBytes int64) error {
	if maxFileBytes > 0 && size > maxFileBytes {
		return &FileError{
			Reason: ReasonFileTooLarge,
			Detail: strconv.FormatInt(size/(1024*1024), 10),
			Err:    ErrFileTooLarge,
		}
	}
	ext = strings.ToLower(ext)
	if reason, bad := unsupportedExtensions[ext]; bad {
		return &FileError{Reason: reason, Detail: ext, Err: ErrUnsupportedFile}
	}
	if textExtensions[ext] || isOfficeExt(ext) || ext == ".pdf" {
		return nil
	}
	// Unknown extension: let the content decide. A NUL byte in the head is the
	// same binary signal the file viewer uses.
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	for i := range n {
		if buf[i] == 0 {
			return &FileError{Reason: ReasonBinary, Err: ErrUnsupportedFile}
		}
	}
	return nil
}

func isOfficeExt(ext string) bool {
	return ext == ".docx" || ext == ".xlsx" || ext == ".pptx"
}

// ExtractFile turns a file into text blocks. maxTextBytes caps the retained
// text; longer output is truncated and the returned warning code says so.
func ExtractFile(path, ext string, maxTextBytes int64) ([]Block, string, error) {
	ext = strings.ToLower(ext)
	var blocks []Block
	var err error
	var warning string
	switch ext {
	case ".pdf":
		blocks, err = extractPDF(path)
	case ".docx":
		blocks, err = extractDocx(path)
	case ".xlsx":
		blocks, err = extractXlsx(path)
		if errors.Is(err, ErrOfficeBudgetSpent) {
			// A workbook that decompressed more than the budget is not a parse
			// failure: keep what was already read and mark the text as
			// truncated, so the document stays searchable.
			err, warning = nil, WarningTruncated
		}
	case ".pptx":
		blocks, err = extractPptx(path)
		if errors.Is(err, ErrOfficeBudgetSpent) {
			err, warning = nil, WarningTruncated
		}
	case ".html", ".htm", ".xhtml":
		blocks, err = extractHTML(path)
	default:
		blocks, err = extractPlainText(path)
	}
	if err != nil {
		return nil, "", err
	}
	blocks, capWarning := capBlocks(blocks, maxTextBytes)
	if capWarning != "" {
		warning = capWarning
	}
	if len(blocks) == 0 {
		return nil, warning, &FileError{Reason: ReasonNoText, Err: ErrNoText}
	}
	return blocks, warning, nil
}

// extractPlainText reads a text file, detects its encoding and emits blocks of
// roughly 80 lines so the location stays useful.
func extractPlainText(path string) ([]Block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	text, err := decodeText(data)
	if err != nil {
		return nil, err
	}
	return lineBlocks(text, 80), nil
}

// decodeText handles the encodings users actually have: UTF-8 (with or without
// BOM), UTF-16 with BOM, and GBK/GB18030. Anything else is treated as UTF-8 with
// invalid bytes replaced rather than failing the whole document.
func decodeText(data []byte) (string, error) { //nolint:gocyclo // encoding detection: BOM, UTF-16, GB18030, fallback
	if len(data) == 0 {
		return "", nil
	}
	// Reject binary content early.
	for _, b := range data {
		if b == 0 {
			// A UTF-16 BOM also contains NUL bytes, so check that first.
			if len(data) >= 2 && ((data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF)) {
				break
			}
			return "", &FileError{Reason: ReasonBinary, Err: ErrUnsupportedFile}
		}
	}

	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return string(data[3:]), nil
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16(data[2:], false), nil
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16(data[2:], true), nil
	}

	if utf8.Valid(data) {
		return string(data), nil
	}
	// GBK/GB18030 covers most Chinese text produced on Windows.
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil && utf8.Valid(decoded) {
		return string(decoded), nil
	}
	return strings.ToValidUTF8(string(data), "\uFFFD"), nil
}

func decodeUTF16(data []byte, bigEndian bool) string {
	n := len(data) / 2
	units := make([]uint16, n)
	for i := range n {
		if bigEndian {
			units[i] = uint16(data[i*2])<<8 | uint16(data[i*2+1])
		} else {
			units[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
		}
	}
	return string(utf16.Decode(units))
}

// lineBlocks splits text into blocks of at most linesPerBlock lines.
func lineBlocks(text string, linesPerBlock int) []Block {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	var out []Block
	for start := 0; start < len(lines); start += linesPerBlock {
		end := start + linesPerBlock
		if end > len(lines) {
			end = len(lines)
		}
		chunk := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if chunk == "" {
			continue
		}
		out = append(out, Block{
			Text:     chunk,
			Location: Location{Kind: "line", Start: start + 1, End: end},
		})
	}
	return out
}

func paraRange(start, end int) Location {
	return Location{Kind: "paragraph", Start: start, End: end}
}

// capBlocks truncates the extracted text to maxBytes and reports whether it did.
func capBlocks(blocks []Block, maxBytes int64) ([]Block, string) {
	if maxBytes <= 0 {
		return blocks, ""
	}
	var total int64
	var out []Block
	for _, b := range blocks {
		size := int64(len(b.Text))
		if total+size <= maxBytes {
			out = append(out, b)
			total += size
			continue
		}
		remaining := maxBytes - total
		if remaining > 0 {
			out = append(out, Block{Text: truncateUTF8(b.Text, int(remaining)), Location: b.Location})
		}
		return out, WarningTruncated
	}
	return out, ""
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune.
func truncateUTF8(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}
