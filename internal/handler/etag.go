package handler

// Response validators for assembled (non-file) bodies.
//
// The repo has exactly one other conditional-request implementation
// (file_thumb.go), which compares If-None-Match with plain string equality. That
// is enough for a value it generates itself and always receives back verbatim,
// but not for a public endpoint: browsers and proxies legitimately send weak
// validators (W/"x"), comma-separated lists, and "*". A plain == silently fails
// all three, turning every revalidation into a full re-download.
