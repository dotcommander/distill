// Package mdclean strips embedded binary blobs (base64 data URIs and long bare
// base64 runs) from markdown before it is chunked. Such blobs — typically images
// from a PDF-to-markdown conversion — carry no digestible content and otherwise
// explode the chunk count: a single multi-megabyte base64 line can yield hundreds
// of useless chunks. Deterministic, offline, stdlib only.
package mdclean

import "regexp"

var (
	// Markdown image whose target is a data: URI — ![alt](data:image/png;base64,…).
	mdDataImage = regexp.MustCompile(`!\[[^\]]*\]\(\s*data:[^)]*\)`)
	// HTML <img> with a data: URI source.
	htmlDataImage = regexp.MustCompile(`(?i)<img[^>]*\bsrc\s*=\s*["']data:[^"']*["'][^>]*>`)
	// A long contiguous run of base64 characters (>=1024) — a raw embedded blob.
	// The floor sits far above any real word, URL, hash, or token, so prose is
	// left untouched.
	bareBase64 = regexp.MustCompile(`[A-Za-z0-9+/]{512}[A-Za-z0-9+/]{512}[A-Za-z0-9+/]*={0,2}`)
)

// StripBinaryBlobs removes embedded base64 / data-URI blobs from s and returns
// the cleaned text together with the number of bytes removed (0 when s is clean).
func StripBinaryBlobs(s string) (string, int) {
	before := len(s)
	s = mdDataImage.ReplaceAllString(s, "")
	s = htmlDataImage.ReplaceAllString(s, "")
	s = bareBase64.ReplaceAllString(s, "")
	return s, before - len(s)
}
