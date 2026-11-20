// Package filex 是文件读取的公共工具：MIME 判定、二进制嗅探、文本截断、
// 符号链接解析，以及文件接口的响应 DTO。
//
// 它从 internal/handler/file.go 抽出来，因为分享功能（internal/plugin/share）
// 要复用同一套「读取文件并返回内容」的规则，而插件不能 import handler。
// 抽出来之后两边用同一份实现，不会出现「分享页和文件页对同一个文件判断
// 不一致」这类问题。
package filex

import (
	"os"
	"path/filepath"
	"unicode/utf8"
)

// MimeOctetStream is the generic binary MIME type used when a file extension
// is not recognized.
const MimeOctetStream = "application/octet-stream"

// MimeTypes maps file extensions to MIME types.
//
// Deliberately contains NO active types (text/html, text/css, text/javascript):
// the public share endpoints serve from this table, and an unsandboxed text/html
// would let a shared .html run same-origin script the moment a logged-in reader
// opens the link.
var MimeTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".bmp":  "image/bmp",
	".pdf":  "application/pdf",
	".mp3":  "audio/mpeg",
	".pcm":  "audio/L16",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".wma":  "audio/x-ms-wma",
	".opus": "audio/opus",
	".mp4":  "video/mp4",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".flv":  "video/x-flv",
	".wmv":  "video/x-ms-wmv",
	".m4v":  "video/mp4",
	".3gp":  "video/3gpp",
	".m3u8": "application/vnd.apple.mpegurl",
	// Office documents
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
}

// WebAssetMimeTypes maps the additional extensions the HTML file preview needs,
// on top of MimeTypes.
//
// Kept SEPARATE from MimeTypes on purpose: MimeTypes is the generic table that
// the unauthenticated /local/ and /download/ endpoints also use, and it must
// never contain an ACTIVE type. Two callers grant these, each with its own
// safety argument: the authenticated raw-file endpoint (the document already
// runs inside a sandboxed iframe the user opened deliberately), and the public
// /doc/ endpoint, which grants them ONLY together with a sandbox CSP (no
// allow-same-origin).
//
//nolint:goconst // font extension literals repeat across the MIME / hashed-asset / custom-font maps
var WebAssetMimeTypes = map[string]string{
	".html":  "text/html",
	".htm":   "text/html",
	".xhtml": "application/xhtml+xml",
	".css":   "text/css",
	".js":    "text/javascript",
	".mjs":   "text/javascript",
	".json":  "application/json",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

// MaxGetFileBytes bounds how many bytes are worth inlining into a FileContent
// response.
const MaxGetFileBytes = 10 * 1024 * 1024

// DirEntry represents a directory entry in API responses.
type DirEntry struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Modified  string `json:"modified,omitempty"`
	Size      int64  `json:"size"`
	Supported bool   `json:"supported"`
	Symlink   bool   `json:"symlink,omitempty"`
	Broken    bool   `json:"broken,omitempty"`
	// Target is the absolute directory a symlink-to-directory must be entered
	// THROUGH when the link escapes the browsed base. The link itself resolves
	// outside the base, so /api/dir refuses to follow it; the client instead
	// navigates to this path, which is served by the external-browsing endpoint
	// (/api/projects). Absent for ordinary entries and for symlinks that stay
	// inside the base — those are entered via their own (relative) path.
	Target string `json:"target,omitempty"`
	// Ignored marks an entry git would not track (matched by .gitignore, or
	// sitting under an excluded directory). It is advisory only: ignored entries
	// stay listed and fully operable, the UI just dims them.
	Ignored bool `json:"ignored,omitempty"`
}

// FileContent represents file content in API responses.
type FileContent struct {
	Content    string `json:"content"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Supported  bool   `json:"supported"`
	IsBinary   bool   `json:"isBinary,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Size       int64  `json:"size"`
	Subtype    string `json:"subtype,omitempty"`
	SpecJSON   string `json:"specJson,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
	IsSymlink  bool   `json:"isSymlink,omitempty"`

	// TooLarge marks a metadata-only response: the file exceeds the inline cap
	// so Content is empty and the caller must fall back to a raw/download
	// endpoint. Currently emitted only by the public share file endpoint, which
	// has no line-window path to fall back on. (GetFile instead rejects with
	// FileTooLarge, because it has the line-window preview as an alternative.)
	TooLarge bool `json:"tooLarge,omitempty"`

	// Line-window metadata, present only on ?lineStart/?lineEnd responses.
	// TotalLines is the file's full line count (so the preview can compute how
	// much context remains above/below), while WindowStart/WindowEnd bound the
	// returned Content (1-based inclusive; WindowEnd < WindowStart means no lines
	// were captured, e.g. the requested range starts past EOF).
	TotalLines  int `json:"totalLines,omitempty"`
	WindowStart int `json:"windowStart,omitempty"`
	// WindowEnd is deliberately NOT omitempty: "no lines captured" is encoded as
	// WindowEnd == WindowStart-1, which is 0 when the window starts at line 1.
	// Omitting that 0 would make an empty window indistinguishable from a
	// response that carried no window metadata at all.
	WindowEnd       int  `json:"windowEnd"`
	WindowTruncated bool `json:"windowTruncated,omitempty"`
}

const (
	// BinarySniffSize is how many leading bytes are checked for null bytes.
	BinarySniffSize = 8192
	// MaxForceTextSize bounds how much of a large text file is returned.
	MaxForceTextSize = 512 * 1024 // 512KB
	// MaxBinaryTextSize bounds how much of a binary file is returned as text.
	MaxBinaryTextSize = 64 * 1024 // 64KB
)

// MimeForLocalFile returns the MIME type for an extension, falling back to
// octet-stream.
func MimeForLocalFile(ext string) string {
	if mime := MimeTypes[ext]; mime != "" {
		return mime
	}
	if mime := WebAssetMimeTypes[ext]; mime != "" {
		return mime
	}
	return MimeOctetStream
}

// SniffBinaryContent reads the beginning of a file and returns true if it
// contains null bytes (indicating binary content).
func SniffBinaryContent(absPath string) (bool, error) {
	sniffBuf := make([]byte, BinarySniffSize)
	f, err := os.Open(absPath)
	if err != nil {
		return false, err
	}
	n, _ := f.Read(sniffBuf)
	_ = f.Close()

	for i := range n {
		if sniffBuf[i] == 0 {
			return true, nil
		}
	}
	return false, nil
}

// ResponsePath returns the path to include in API responses: relative for
// project files, absolute for external files.
func ResponsePath(absPath, projectPath string, isExternal bool) string {
	if isExternal {
		return absPath
	}
	relPath, err := filepath.Rel(projectPath, absPath)
	if err != nil {
		return absPath
	}
	return relPath
}

// ResolveLinkTarget returns the symlink target path (relative to projectPath when
// the link is project-internal, absolute otherwise) for a symlink at absPath,
// plus whether the entry is a symlink. Returns ("", false) for non-symlinks and
// when the target cannot be read.
func ResolveLinkTarget(absPath, projectPath string, isExternal bool) (string, bool) {
	linfo, err := os.Lstat(absPath)
	if err != nil || linfo.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(absPath)
	if err != nil {
		return "", true
	}
	// Resolve a relative link target against the link's directory, then present
	// it project-relative (or absolute for external files), matching respPath.
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(absPath), target)
	}
	resp := ResponsePath(target, projectPath, isExternal)
	if resp == "" {
		resp = target
	}
	return resp, true
}

// SanitizeTextContent normalizes file content for text display.
//
// For binary files (detected via null-byte sniffing in the first 8KB):
//   - Truncates to MaxBinaryTextSize
//   - Replaces non-printable characters with '.'
//
// For large text files:
//   - Truncates to MaxForceTextSize at a UTF-8 boundary
//
// Returns the sanitized content and whether truncation occurred.
func SanitizeTextContent(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}

	if HasBinaryContent(data) {
		return SanitizeBinaryContent(data)
	}

	if len(data) > MaxForceTextSize {
		return TruncateAtUTF8Boundary(data), true
	}

	return data, false
}

// HasBinaryContent checks the first 8KB for null bytes to detect binary content.
func HasBinaryContent(data []byte) bool {
	sniffEnd := len(data)
	if sniffEnd > BinarySniffSize {
		sniffEnd = BinarySniffSize
	}
	for i := range sniffEnd {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// SanitizeBinaryContent truncates binary content and replaces non-printable
// characters with '.', keeping \n, \r, \t, printable ASCII, and high bytes.
func SanitizeBinaryContent(data []byte) ([]byte, bool) {
	truncated := false
	if len(data) > MaxBinaryTextSize {
		data = data[:MaxBinaryTextSize]
		truncated = true
	}
	out := make([]byte, len(data))
	for i, b := range data {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b < 0x7F) || b >= 0x80 {
			out[i] = b
		} else {
			out[i] = '.'
		}
	}
	return out, truncated
}

// TruncateAtUTF8Boundary truncates data at MaxForceTextSize, stepping back to
// avoid splitting a multi-byte UTF-8 character.
func TruncateAtUTF8Boundary(data []byte) []byte {
	cut := MaxForceTextSize
	for cut > 0 && cut < len(data) && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return data[:cut]
}

// CopyFile copies src to dst, truncating dst if it exists.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = dstFile.ReadFrom(srcFile)
	return err
}
