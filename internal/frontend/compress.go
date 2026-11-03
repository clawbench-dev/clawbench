package frontend

import (
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// precompressedSuffix is appended to a file name to find the gzip-encoded copy
// emitted at build time (see scripts/vite-precompress.ts). Compressing during
// the build instead of per request keeps the serving path free of CPU work and
// memory caches, and lets the same files be embedded in the binary unchanged.
const precompressedSuffix = ".gz"

// compressibleExts are the text formats that shrink under gzip. Fonts, images
// and archives are already compressed: re-encoding them costs CPU and usually
// makes the payload larger, so no `.gz` sibling is ever produced for them.
var compressibleExts = map[string]bool{
	".js":   true,
	".mjs":  true,
	".css":  true,
	".html": true,
	".json": true,
	".svg":  true,
	".txt":  true,
}

// isCompressible reports whether name has an extension worth gzip-encoding.
func isCompressible(name string) bool {
	return compressibleExts[strings.ToLower(path.Ext(name))]
}

// acceptsGzip reports whether the request's Accept-Encoding header allows a
// gzip response. Quality values are honored: `gzip;q=0` is an explicit refusal,
// and a bare `*` with a non-zero quality accepts gzip too.
func acceptsGzip(header string) bool {
	star := false
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(part, ";")
		switch strings.ToLower(strings.TrimSpace(token)) {
		case "gzip":
			return !zeroQuality(params)
		case "*":
			star = !zeroQuality(params)
		}
	}
	return star
}

// zeroQuality reports whether an Accept-Encoding parameter list carries `q=0`.
func zeroQuality(params string) bool {
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(p, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return err == nil && q <= 0
	}
	return false
}

// openPrecompressed opens the gzip sibling of name when this request can use
// it. The caller owns closing the returned file.
//
// Range requests deliberately fall back to the identity representation: byte
// offsets in a Range header refer to the selected representation, and mixing
// encoded and unencoded responses for the same URL confuses clients that resume
// a download. Browsers never range-request JS/CSS, so nothing is lost.
//
// A sibling older than the file it encodes is ignored. With emptyOutDir=false
// the build directory keeps old files, and a non-hashed name (index.html,
// manifest.json) can be rewritten in place — serving its stale `.gz` would
// hand the browser yesterday's content.
func openPrecompressed(fsys fs.FS, r *http.Request, name string, srcInfo fs.FileInfo) (fs.File, fs.FileInfo, bool) {
	if r.Header.Get("Range") != "" {
		return nil, nil, false
	}
	if !isCompressible(name) || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return nil, nil, false
	}

	f, err := fsys.Open(name + precompressedSuffix)
	if err != nil {
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.ModTime().Before(srcInfo.ModTime()) {
		_ = f.Close()
		return nil, nil, false
	}
	return f, info, true
}
