package gradecal

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
)

// RecognizeRenderer builds the self-recognition prompt from an ordered set of
// anonymized digest texts.
type RecognizeRenderer interface {
	RenderRecognize(digests []string) string
}

var (
	headingMarkerRe = regexp.MustCompile(`(?m)^#+\s+`)
	bulletMarkerRe  = regexp.MustCompile(`(?m)^[-*]\s+`)
	emphasisRe      = regexp.MustCompile(`[*_]`)
	integerRe       = regexp.MustCompile(`\d+`)
)

// Canonicalize strips lightweight Markdown so recognition keys on voice rather
// than formatting.
func Canonicalize(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = headingMarkerRe.ReplaceAllString(text, "")
	text = bulletMarkerRe.ReplaceAllString(text, "")
	text = emphasisRe.ReplaceAllString(text, "")

	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	droppedTitle := false
	for _, line := range lines {
		if !droppedTitle && strings.TrimSpace(line) != "" {
			droppedTitle = true
			continue
		}
		kept = append(kept, line)
	}
	return normWS(strings.Join(kept, "\n"))
}

func parseIndex(raw string, setSize int) (int, error) {
	m := integerRe.FindString(raw)
	if m == "" {
		return 0, fmt.Errorf("no integer in judge reply: %q", trunc(raw))
	}
	i, err := strconv.Atoi(m)
	if err != nil {
		return 0, fmt.Errorf("parsing judge reply %q: %w", trunc(m), err)
	}
	if i < 0 || i >= setSize {
		return 0, fmt.Errorf("judge index out of range: %d", i)
	}
	return i, nil
}

type Trial struct {
	JudgeModel string
	OwnIndex   int
	Picked     int
	Correct    bool
	Err        string
}

type JudgeRecognition struct {
	Model   string
	Trials  int
	Hits    int
	SetSize int
}

func (j JudgeRecognition) Accuracy() float64 {
	if j.Trials == 0 {
		return 0
	}
	return float64(j.Hits) / float64(j.Trials)
}

func (j JudgeRecognition) Chance() float64 {
	if j.SetSize == 0 {
		return 0
	}
	return 1 / float64(j.SetSize)
}

func (j JudgeRecognition) AboveChance() bool { return j.Accuracy() > j.Chance() }

func (j JudgeRecognition) Verdict() string {
	if j.AboveChance() {
		return "RECOGNITION PRESENT — self-preference bias plausibly real"
	}
	return "AT/BELOW CHANCE — self-preference bias moot"
}

func RunRecognition(ctx context.Context, judges map[string]Completer, r RecognizeRenderer, field []Digest, judgeNames []string, trials, setSize int, seed int64) []JudgeRecognition {
	results := make([]JudgeRecognition, 0, len(judgeNames))
	for judgeIdx, name := range judgeNames {
		judge, ok := judges[name]
		own, found := findDigest(field, name)
		if !ok || !found {
			results = append(results, JudgeRecognition{Model: name, Trials: 0, Hits: 0, SetSize: setSize})
			continue
		}

		hits := 0
		for trialNum := 0; trialNum < trials; trialNum++ {
			if runRecognitionTrial(ctx, judge, r, field, own, judgeIdx, trialNum, trials, setSize, seed) {
				hits++
			}
		}
		results = append(results, JudgeRecognition{Model: name, Trials: trials, Hits: hits, SetSize: setSize})
	}
	return results
}

func runRecognitionTrial(ctx context.Context, judge Completer, r RecognizeRenderer, field []Digest, own Digest, judgeIdx, trialNum, trials, setSize int, seed int64) bool {
	if setSize <= 0 {
		return false
	}
	decoys := decoyDigests(field, own.Name)
	if len(decoys) < setSize-1 {
		return false
	}

	trialOffset := judgeIdx*trials + trialNum
	rng := rand.New(rand.NewSource(seed + int64(trialOffset))) //nolint:gosec // non-crypto: grading-tournament shuffle; cryptographic randomness not required
	rng.Shuffle(len(decoys), func(i, j int) { decoys[i], decoys[j] = decoys[j], decoys[i] })
	ownIndex := rng.Intn(setSize)
	set := make([]string, setSize)
	decoyIdx := 0
	for i := range set {
		if i == ownIndex {
			set[i] = Canonicalize(own.Text)
			continue
		}
		set[i] = Canonicalize(decoys[decoyIdx].Text)
		decoyIdx++
	}

	raw, err := judge.Complete(ctx, r.RenderRecognize(set))
	if err != nil {
		return false
	}
	picked, err := parseIndex(raw, setSize)
	return err == nil && picked == ownIndex
}

func findDigest(field []Digest, name string) (Digest, bool) {
	for _, d := range field {
		if d.Name == name {
			return d, true
		}
	}
	return Digest{}, false
}

func decoyDigests(field []Digest, ownName string) []Digest {
	decoys := make([]Digest, 0, len(field))
	for _, d := range field {
		if d.Name != ownName {
			decoys = append(decoys, d)
		}
	}
	return decoys
}
