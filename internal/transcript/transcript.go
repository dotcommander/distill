// Package transcript detects and cleans auto-caption transcript input (YouTube
// VTT/SRT) into paragraph-reflowed prose, deterministically and offline. Raw
// auto-captions arrive as ~5-word fragments wrapped in a WEBVTT header or
// numbered SRT cues, with timing lines, inline <HH:MM:SS.mmm> timestamps,
// <c> styling tags, [bracket] labels, AND a YouTube rolling-overlap bug that
// triplicates lines. Feeding that raw to an LLM extractor fragments context and
// conflates entities; cleaning fixes that class of error. No LLM is used — only
// stdlib regexp/strings/bufio. Format-parsing structure is logic (Go); there is
// no user-tunable behavioral content, so thresholds are documented Go constants.
package transcript

import "strings"

// Kind identifies a detected transcript format.
type Kind int

const (
	// KindNone means the input is not a recognized transcript format.
	KindNone Kind = iota
	// KindVTT is WebVTT (a "WEBVTT" header line).
	KindVTT
	// KindSRT is SubRip (numbered cue blocks with "-->" timing lines).
	KindSRT
	// KindYTDLPLog is a yt-dlp console-log preamble prefixing a transcript body.
	// Detect returns it only as a gate signal (so IsTranscript opens and callers
	// invoke Clean); Clean strips the preamble, then re-detects the body format.
	KindYTDLPLog
)

// Detect classifies data as a transcript format. It must NOT false-positive on
// ordinary markdown/prose: VTT requires the WEBVTT signature on the first
// non-blank line; SRT requires at least one numbered-cue + timing-line pair.
func Detect(data string) Kind {
	if isVTT(data) {
		return KindVTT
	}
	if isSRT(data) {
		return KindSRT
	}
	if hasYTDLPPreamble(data) {
		return KindYTDLPLog
	}
	return KindNone
}

// IsTranscript reports whether Detect classifies data as a known transcript.
func IsTranscript(data string) bool { return Detect(data) != KindNone }

// Clean parses cues (VTT or SRT), strips per-cue noise, deduplicates the
// auto-caption triplication / rolling overlap, and reflows fragments into
// paragraph blocks. For non-transcript input it returns data unchanged so
// callers can clean unconditionally without re-detecting. The error return is
// reserved for future parse failures; today Clean is total and returns nil.
func Clean(data string) (string, error) {
	kind := Detect(data)
	if kind == KindNone {
		return data, nil
	}
	if kind == KindYTDLPLog {
		body := stripYTDLPPreamble(data)
		if strings.TrimSpace(body) == "" {
			return data, nil
		}
		return Clean(body)
	}
	cues := parseCues(data, kind)
	if len(cues) == 0 {
		return data, nil
	}
	cleaned := reflow(cues)
	// If stripping/reflow emptied the content, the detection was a
	// false-positive or the cues were pure noise — return the original rather
	// than handing downstream a blank document (mirrors the yt-dlp guard above).
	if strings.TrimSpace(cleaned) == "" {
		return data, nil
	}
	return cleaned, nil
}

// isVTT reports whether the first non-blank line is the WEBVTT signature.
func isVTT(data string) bool {
	for _, line := range strings.Split(data, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		return strings.HasPrefix(t, "WEBVTT")
	}
	return false
}
