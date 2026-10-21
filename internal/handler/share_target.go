//nolint:goconst // JSON response field names are domain strings
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
)

// shareTargetPath is the URL the PWA share_target POSTs to (see
// assets/manifest.json). It must stay in sync with the manifest, which is the
// only thing that ever tells the browser to use it.
const shareTargetPath = "/share-target"

// maxShareTargetFiles bounds how many files one share may carry. A phone
// "share to app" gesture is one to a handful of images; anything larger is not
// a share, and parsing it would only hold the request open.
const maxShareTargetFiles = 20

// ShareTarget receives a Web Share Target POST from an installed PWA.
//
// How the browser calls this: when the user shares a photo from the system
// gallery to an installed ClawBench, the browser sends a multipart POST here
// (method: POST, enctype: multipart/form-data in the manifest) and then
// navigates the app window to whatever URL we redirect to. So this handler has
// two jobs: store the files, and answer with a redirect the app can act on.
//
// The files land in the SAME place a manual upload does
// (.clawbench/uploads/) so the rest of the pipeline — attachment cards, prompt
// injection, AI file reading — needs no knowledge of share-target at all. The
// redirect carries the stored paths so the app can pre-attach them; putting
// them in the URL rather than a server-side session keeps this stateless and
// survives the browser navigating a fresh document.
//
// This route is AUTHENTICATED (registered through `register`): sharing into a
// ClawBench that is not logged in must not write files, and the cookie is
// available here because the share target is same-origin.
func ShareTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// A GET here means the browser opened the URL directly (or the manifest
		// is wrong). Send the user into the app rather than showing a 405.
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// This route is registered OUTSIDE middleware.Auth (see RegisterRoutes), so
	// authentication is enforced here. The reason it is not wrapped: Auth
	// answers a JSON 401, and this URL is NAVIGATED to by the browser after the
	// OS share sheet — the user would see a raw JSON body instead of the login
	// page. Bouncing to "/" lands them in the app, which shows login itself.
	if !middleware.IsAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Read the project directly rather than through requireProject: that helper
	// writes a 403 JSON body, which the browser would render as an unreachable
	// error page (it navigated here). Bounce into the app so the user sees the
	// normal project picker instead.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize()*maxShareTargetFiles)
	// maxMemory bounds the IN-MEMORY part; anything beyond it spills to a temp
	// file, and the total is already capped by MaxBytesReader above.
	if err := r.ParseMultipartForm(maxUploadSize()); err != nil { //nolint:gosec // total capped by MaxBytesReader
		// The error page would be unreachable in practice (the browser navigated
		// here), so bounce back into the app with a flag the UI can surface.
		redirectShareTarget(w, r, shareOutcome{reason: "invalid"})
		return
	}

	files := shareTargetFiles(r)
	if len(files) == 0 {
		redirectShareTarget(w, r, shareOutcome{reason: "empty"})
		return
	}
	// Anything beyond the cap is a file the user shared and did not get, so it
	// counts as a failure — silently truncating would report a partial batch as
	// a complete one.
	overflow := 0
	if len(files) > maxShareTargetFiles {
		overflow = len(files) - maxShareTargetFiles
		files = files[:maxShareTargetFiles]
	}

	targetDir := filepath.Join(projectPath, ".clawbench", "uploads")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		redirectShareTarget(w, r, shareOutcome{reason: "save"})
		return
	}

	maxPerFile := maxUploadSize()
	stored := make([]string, 0, len(files))
	failed := overflow
	for _, fh := range files {
		// Enforce the same per-file cap every other upload path honors. The
		// body limit above is per-BATCH (maxUploadSize()*maxShareTargetFiles),
		// so without this a single oversized file would slip through.
		if fh.Size > maxPerFile {
			failed++
			continue
		}
		rel, err := saveSharedFile(targetDir, fh)
		if err != nil {
			// One bad file must not discard the ones already saved — the user
			// shared a batch and losing all of it is worse than losing one.
			failed++
			continue
		}
		stored = append(stored, rel)
	}
	if len(stored) == 0 {
		redirectShareTarget(w, r, shareOutcome{reason: "save"})
		return
	}

	// Report a partial batch as such. Saying "added N" while silently dropping
	// the rest makes the user believe every shared file is attached.
	redirectShareTarget(w, r, shareOutcome{paths: stored, failed: failed})
}

// shareTargetFiles collects the uploaded parts.
//
// The manifest's share_target params use the field name `files`, but a browser
// is free to send the same file under a different key when the share comes from
// a source that does not follow the manifest (and some Android share sheets
// send a single `file`). Both are accepted so a share never silently no-ops.
func shareTargetFiles(r *http.Request) []*multipart.FileHeader {
	var out []*multipart.FileHeader
	for _, key := range []string{"files", "file"} {
		if r.MultipartForm == nil {
			return out
		}
		out = append(out, r.MultipartForm.File[key]...)
	}
	return out
}

// saveSharedFile writes one shared file into targetDir and returns its
// project-relative path.
//
// The name is sanitized the same way UploadFile does it (basename, spaces to
// underscores, sequential suffix on collision) so a shared file behaves exactly
// like an uploaded one — including for the retention worker, which matches
// flat names under .clawbench/uploads/.
func saveSharedFile(targetDir string, fh *multipart.FileHeader) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()

	baseName := filepath.Base(fh.Filename)
	// Reject names that are not usable as a file name. Go's multipart parser
	// never yields an empty FileName (a part without `filename=` is a form
	// value, not a file), but "." and ".." both arrive intact and filepath.Base
	// returns them unchanged — writing to either would target the directory
	// itself. A share from a gallery may also carry a bare name with no
	// extension, which the AI could not classify, so derive one from the type.
	if baseName == "" || baseName == "." || baseName == ".." || baseName == string(filepath.Separator) {
		baseName = "shared_" + extForContentType(fh.Header.Get("Content-Type"))
	}
	ext := strings.ToLower(filepath.Ext(baseName))
	nameWithoutExt := strings.ReplaceAll(strings.TrimSuffix(baseName, ext), " ", "_")
	if nameWithoutExt == "" {
		nameWithoutExt = "shared"
	}

	// Create with O_EXCL rather than stat-then-create: shares can arrive
	// concurrently (a multi-photo share from two apps, or the same gallery name
	// twice) and a stat/create pair would let the second create truncate the
	// first — destroying an upload an earlier message may already reference.
	// Same reasoning as internal/push/common/attachment.go.
	var (
		dst      *os.File
		dstPath  string
		filename string
	)
	for i := range 10000 {
		if i == 0 {
			filename = nameWithoutExt + ext
		} else {
			filename = fmt.Sprintf("%s_%d%s", nameWithoutExt, i, ext)
		}
		dstPath = filepath.Join(targetDir, filename)
		if !isPathUnderAnyRoot(dstPath) {
			return "", model.Forbidden(nil, "AccessDenied")
		}
		//nolint:gosec // G302: uploads are user-visible project files, same mode as the web upload endpoint
		created, openErr := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if openErr == nil {
			dst = created
			break
		}
		if !errors.Is(openErr, fs.ErrExist) {
			return "", openErr
		}
	}
	if dst == nil {
		return "", fmt.Errorf("share target: no free filename for %q", nameWithoutExt+ext)
	}
	if _, err := io.Copy(dst, src); err != nil {
		// Close BEFORE removing: on Windows a delete of an open handle fails,
		// which would leave a truncated file behind under a name that then shows
		// up in "recent uploads". (Same trap documented in
		// internal/push/common/attachment.go.)
		_ = dst.Close()
		_ = os.Remove(dstPath)
		return "", err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dstPath)
		return "", err
	}
	return ".clawbench/uploads/" + filename, nil
}

// extForContentType maps an image content type to a file extension for shares
// that arrive without a filename.
func extForContentType(ct string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0])) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "image/heic", "image/heif":
		return "heic"
	case "image/bmp":
		return "bmp"
	case "image/svg+xml":
		return "svg"
	case "application/pdf":
		return "pdf"
	default:
		return "bin"
	}
}

// shareOutcome is what the browser is told after a share POST.
type shareOutcome struct {
	// paths are the project-relative paths that were stored.
	paths []string
	// failed is how many files in the batch could not be stored. Reported so a
	// partial batch is not presented as full success.
	failed int
	// reason is the whole message when nothing was stored at all
	// ("empty" / "save" / "invalid").
	reason string
}

// redirectShareTarget sends the browser back into the app with the outcome.
//
// The app reads `share_target` from the query string: on success it is the JSON
// array of stored paths, which the composer pre-attaches, plus an optional
// `share_target_failed` count. On failure it is the bare reason, which the UI
// turns into a toast — the files are gone at that point, and a silent return
// would look like the share simply did nothing.
//
// 303 (not 302) so the browser follows with GET: the POST body has been
// consumed and replaying it would re-upload every file.
func redirectShareTarget(w http.ResponseWriter, r *http.Request, outcome shareOutcome) {
	value := outcome.reason
	if len(outcome.paths) > 0 {
		encoded, err := json.Marshal(outcome.paths)
		if err != nil {
			value = "save"
		} else {
			value = string(encoded)
		}
	}

	// url.QueryEscape would encode the JSON's brackets/quotes; the app only
	// needs it to survive as one query parameter value.
	target := "/?share_target=" + queryEscape(value)
	if len(outcome.paths) > 0 && outcome.failed > 0 {
		target += "&share_target_failed=" + strconv.Itoa(outcome.failed)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// queryEscape is a tiny wrapper so this file does not pull net/url into the
// handler's import set just for one call site.
func queryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
