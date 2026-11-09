package handler

import (
	"net/http"

	"clawbench/internal/service"
)

// maxMailAttachPaths bounds one staging request. The composer is a single
// message, so this only has to stop a runaway list, not describe a limit users
// could legitimately hit.
const maxMailAttachPaths = 20

// ServeMailAttachments handles POST /api/mail/attachments.
//
// It turns paths the user picked in the file manager / file header into
// composer attachments: a file is used in place, a directory is packed into one
// zip. Doing this on the server keeps a folder out of the browser entirely —
// the alternative (download the zip, upload it back) would push the whole tree
// through the client and its memory.
func ServeMailAttachments(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}
	if len(req.Paths) > maxMailAttachPaths {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TooManyPaths")
		return
	}

	// Same resolution as every other file endpoint: project-relative paths are
	// resolved against the project cookie, absolute ones are checked against
	// the allowed roots.
	absPaths := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		abs, ok := resolveAbsPath(w, r, p)
		if !ok {
			return
		}
		absPaths = append(absPaths, abs)
	}

	atts, err := service.StageMailAttachments(absPaths)
	if err != nil {
		writeMailError(w, r, err)
		return
	}
	if atts == nil {
		atts = []service.MailStagedAttachment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "attachments": atts})
}
