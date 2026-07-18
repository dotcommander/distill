package transcript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// triplicatedVTT reproduces the YouTube rolling-overlap auto-caption bug: a
// WEBVTT header, timing lines, inline <ts> tags, <c> styling, a [Music] label,
// and each phrase repeated across overlapping cues.
const triplicatedVTT = `WEBVTT
Kind: captions
Language: en

00:00:00.000 --> 00:00:02.000 align:start position:0%
[Music]
the quick brown<00:00:01.000><c> fox</c>

00:00:01.000 --> 00:00:03.000 align:start position:0%
the quick brown fox

00:00:02.000 --> 00:00:04.000 align:start position:0%
brown fox jumps over

00:00:08.000 --> 00:00:10.000 align:start position:0%
the lazy dog sleeps`

const sampleSRT = `1
00:00:00,000 --> 00:00:02,000
Hello there friends

2
00:00:01,000 --> 00:00:03,000
there friends welcome back

3
00:00:02,000 --> 00:00:04,000
welcome back to the show`

const plainMarkdown = `# A Heading

This is an ordinary paragraph of prose. It mentions 42 things and has a
timestamp-looking phrase but no cue grammar at all.

- a list item
- another item`

func TestDetect(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"vtt", triplicatedVTT, KindVTT},
		{"srt", sampleSRT, KindSRT},
		{"markdown", plainMarkdown, KindNone},
		{"empty", "", KindNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Detect(tc.in))
		})
	}
}

func TestCleanVTTDedupAndReflow(t *testing.T) {
	t.Parallel()
	got, err := Clean(triplicatedVTT)
	require.NoError(t, err)

	assert.NotContains(t, got, "WEBVTT", "header must be dropped")
	assert.NotContains(t, got, "-->", "timing lines must be dropped")
	assert.NotContains(t, got, "<c>", "styling tags must be stripped")
	assert.NotContains(t, got, "<00:00", "inline timestamps must be stripped")
	assert.NotContains(t, got, "[Music]", "bracket labels must be stripped")
	// Rolling-overlap dedup: "the quick brown fox" appears once, not repeated.
	assert.Equal(t, 1, strings.Count(got, "the quick brown fox"),
		"overlapping duplicate phrase must collapse to one occurrence")
	assert.Contains(t, got, "jumps over")
	assert.Contains(t, got, "the lazy dog sleeps")
	// The 4s -> 8s gap (>3s threshold) must start a new paragraph.
	assert.Contains(t, got, "\n\n", "timing gap must produce a paragraph break")
}

func TestCleanSRTDedup(t *testing.T) {
	t.Parallel()
	got, err := Clean(sampleSRT)
	require.NoError(t, err)
	assert.NotContains(t, got, "-->")
	assert.NotContains(t, got, "00:00:00")
	assert.Equal(t, 1, strings.Count(got, "Hello there friends"))
	assert.Equal(t, 1, strings.Count(got, "welcome back"),
		"rolling overlap across SRT cues must collapse")
	assert.Contains(t, got, "to the show")
}

func TestCleanPassesThroughProse(t *testing.T) {
	t.Parallel()
	got, err := Clean(plainMarkdown)
	require.NoError(t, err)
	assert.Equal(t, plainMarkdown, got, "non-transcript input must be unchanged")
}

func TestStripNoise(t *testing.T) {
	t.Parallel()
	in := `the quick<00:00:01.000><c.color> brown</c> [Applause] fox`
	assert.Equal(t, "the quick brown fox", stripNoise(in))
}

// ytdlpPreamblePlain is a yt-dlp console-log preamble (URL extraction, a
// subtitle-language dump, a download progress line with prose concatenated onto
// the terminal 100% segment) followed by plain prose.
const ytdlpPreamblePlain = `[youtube] Extracting URL: https://www.youtube.com/watch?v=DzbqeO_diOQ
[youtube] DzbqeO_diOQ: Downloading webpage
[youtube] [jsc:deno] Solving JS challenges using deno
[info] DzbqeO_diOQ: Downloading subtitles: ab, aa, af, ak, sq, am, ar, hy, en
[info] DzbqeO_diOQ: Downloading 1 format(s): 401+251
[download] 100% of   57.05MiB in 00:00:02 at 23.59MiB/s  What's up, engineers? Andy Devdan here.
This is the real spoken transcript body. It has ordinary prose with no cue grammar.`

// ytdlpPreambleVTT is a yt-dlp preamble followed by a WEBVTT body.
const ytdlpPreambleVTT = `[youtube] Extracting URL: https://www.youtube.com/watch?v=abc
[info] abc: Downloading subtitles: en, fr, de
[download] Destination: /T/transcribe-1/subtitle.en.vtt
WEBVTT
Kind: captions

00:00:00.000 --> 00:00:02.000
hello there friends

00:00:01.000 --> 00:00:03.000
there friends welcome back`

// ytdlpOnlyNoBody is a preamble with no prose boundary.
const ytdlpOnlyNoBody = `[youtube] Extracting URL: https://www.youtube.com/watch?v=xyz
[youtube] xyz: Downloading webpage
[info] xyz: Downloading 1 format(s): 251
[download] 100% of   12.00MiB in 00:00:01 at 12.00MiB/s`

func TestDetectYTDLPLog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"preamble_plain", ytdlpPreamblePlain, KindYTDLPLog},
		{"preamble_vtt", ytdlpPreambleVTT, KindYTDLPLog},
		{"ytdlp_only", ytdlpOnlyNoBody, KindYTDLPLog},
		{"prose_no_preamble", plainMarkdown, KindNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Detect(tc.in))
		})
	}
}

func TestCleanStripsYTDLPPreamblePlain(t *testing.T) {
	t.Parallel()
	got, err := Clean(ytdlpPreamblePlain)
	require.NoError(t, err)
	assert.NotContains(t, got, "[youtube]")
	assert.NotContains(t, got, "[download]")
	assert.NotContains(t, got, "Downloading subtitles")
	assert.NotContains(t, got, "57.05MiB")
	assert.Contains(t, got, "What's up, engineers? Andy Devdan here.")
	assert.Contains(t, got, "the real spoken transcript body")
}

func TestCleanStripsYTDLPPreambleVTT(t *testing.T) {
	t.Parallel()
	got, err := Clean(ytdlpPreambleVTT)
	require.NoError(t, err)
	assert.NotContains(t, got, "[youtube]")
	assert.NotContains(t, got, "subtitle.en.vtt")
	assert.NotContains(t, got, "WEBVTT")
	assert.NotContains(t, got, "-->")
	assert.Equal(t, 1, strings.Count(got, "welcome back"))
	assert.Contains(t, got, "hello there friends")
}

func TestCleanYTDLPOnlyReturnsInputUnchanged(t *testing.T) {
	t.Parallel()
	got, err := Clean(ytdlpOnlyNoBody)
	require.NoError(t, err)
	assert.Equal(t, ytdlpOnlyNoBody, got)
}

func TestCleanProseWithoutPreambleUnchanged(t *testing.T) {
	t.Parallel()
	got, err := Clean(plainMarkdown)
	require.NoError(t, err)
	assert.Equal(t, plainMarkdown, got)
}

// noiseOnlyVTT detects as VTT but every cue is a bracket label that stripping
// removes — reflow would yield "". Clean must return the original unchanged
// rather than hand downstream a blank document.
const noiseOnlyVTT = `WEBVTT

00:00:00.000 --> 00:00:02.000
[Music]

00:00:02.000 --> 00:00:04.000
[Applause]`

func TestCleanEmptiedOutputReturnsOriginal(t *testing.T) {
	t.Parallel()
	got, err := Clean(noiseOnlyVTT)
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(got), "must not return a blank document")
	assert.Equal(t, noiseOnlyVTT, got, "emptied clean must fall back to the original input")
}

func TestSalvageProseTail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"full_rate_eta", `[download] 100% of   57.05MiB in 00:00:02 at 23.59MiB/s  What's up, engineers?`, "What's up, engineers?"},
		{"no_rate", `[download] 100% of 57.05MiB  Prose starts here.`, "Prose starts here."},
		{"unknown_rate", `[download] 100% of 12MiB at Unknown B/s Hello there.`, "Hello there."},
		{"frag", `[download] 100% of 5MiB in 00:00:01 at 5MiB/s (frag 10/10) Final words.`, "Final words."},
		{"pure_telemetry", `[download] 100% of 12MiB in 00:00:01 at 12MiB/s`, ""},
		{"prose_starting_in", `[download] 100% of 57MiB in the beginning there was a plan.`, "in the beginning there was a plan."},
		{"not_a_download_line", `[info] abc: Downloading 1 format(s): 251`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, salvageProseTail(tc.in))
		})
	}
}
