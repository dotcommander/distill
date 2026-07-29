package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dotcommander/distill/internal/tokenizer"
	"github.com/dotcommander/reliquary/chunking"
)

// SourcePart is one ordered source supplied to digest. Text, rather than a
// path, is the provider-bound input: callers must never put local paths into a
// prompt. Ordinal is stable within a run and is rendered only as Source NN.
type SourcePart struct {
	Ordinal int    `json:"ordinal"`
	Text    string `json:"-"`
	Hash    string `json:"sha256"`
}

// SourceLabel returns the only source identifier that may be sent to a
// provider. It deliberately contains no filename or directory information.
func SourceLabel(ordinal int) string { return fmt.Sprintf("Source %02d", ordinal) }

// PackedChunk is a source-isolated research unit. Text is exactly the ordered
// concatenation of the Reliquary fragments packed into this unit.
type PackedChunk struct {
	ID            string `json:"id"`
	SourceOrdinal int    `json:"source_ordinal"`
	Text          string `json:"-"`
	Characters    int    `json:"characters"`
	Tokens        int    `json:"tokens"`
	Hash          string `json:"sha256"`
}

// PackedPlan is the deterministic source/chunk geometry used by both dry-run
// and execution, and persisted by source.json v2.
type PackedPlan struct {
	Sources               []SourcePart  `json:"sources"`
	Chunks                []PackedChunk `json:"chunks"`
	ChunkSize             int           `json:"chunk_size"`
	MaxTokens             int           `json:"max_tokens"`
	NormalizationVersion  string        `json:"normalization_version"`
	ChunkAlgorithmVersion string        `json:"chunk_algorithm_version"`
}

const (
	packingNormalizationVersion = "source-parts-v1"
	packingAlgorithmVersion     = "heading-aware-greedy-v1"
)

// PlanPackedSources chunks each source independently with the normal
// heading-aware algorithm, then greedily packs only adjacent fragments from
// that source. It never crosses source boundaries.
//
//nolint:funlen,gocognit,gocyclo,revive // Packing must preserve source boundaries and exact input bytes.
func PlanPackedSources(parts []SourcePart, chunkSize, maxTokens int) (PackedPlan, error) {
	if chunkSize < 1 {
		return PackedPlan{}, errors.New("digest: chunk size must be positive")
	}
	plan := PackedPlan{
		Sources:               make([]SourcePart, len(parts)),
		ChunkSize:             chunkSize,
		MaxTokens:             maxTokens,
		NormalizationVersion:  packingNormalizationVersion,
		ChunkAlgorithmVersion: packingAlgorithmVersion,
	}
	copy(plan.Sources, parts)
	var counter tokenizer.Tokenizer
	var err error
	if maxTokens > 0 {
		counter, err = tokenizer.New()
		if err != nil {
			return PackedPlan{}, fmt.Errorf("digest: creating token counter: %w", err)
		}
	}
	for sourceIndex := range plan.Sources {
		part := &plan.Sources[sourceIndex]
		if part.Ordinal < 1 {
			part.Ordinal = sourceIndex + 1
		}
		part.Hash = digestHash(part.Text)
		fragments, err := ChunkSource(part.Text, chunkSize, maxTokens)
		if err != nil {
			return PackedPlan{}, fmt.Errorf("digest: chunking %s: %w", SourceLabel(part.Ordinal), err)
		}
		fragmentTexts, err := exactSourceFragments(part.Text, fragments)
		if err != nil {
			// Some Reliquary hard-limit post-processing deliberately clears
			// spans. Fall back to a raw, lossless bounded partition rather
			// than silently sending normalized/lossy chunk text.
			fragmentTexts, err = losslessBoundedFragments(part.Text, chunkSize, maxTokens, counter)
			if err != nil {
				return PackedPlan{}, fmt.Errorf("digest: preserving %s: %w", SourceLabel(part.Ordinal), err)
			}
		}
		var current strings.Builder
		flush := func() error {
			if current.Len() == 0 {
				return nil
			}
			text := current.String()
			tokens := 0
			if counter != nil {
				tokens, err = counter.Count(text)
				if err != nil {
					return fmt.Errorf("digest: counting packed %s: %w", SourceLabel(part.Ordinal), err)
				}
			}
			plan.Chunks = append(plan.Chunks, PackedChunk{
				ID:            fmt.Sprintf("chunk-%03d", len(plan.Chunks)+1),
				SourceOrdinal: part.Ordinal,
				Text:          text,
				Characters:    utf8.RuneCountInString(text),
				Tokens:        tokens,
				Hash:          digestHash(text),
			})
			current.Reset()
			return nil
		}
		for _, fragment := range fragmentTexts {
			candidate := current.String() + fragment
			candidateTokens := 0
			if counter != nil {
				candidateTokens, err = counter.Count(candidate)
				if err != nil {
					return PackedPlan{}, fmt.Errorf("digest: counting packed %s: %w", SourceLabel(part.Ordinal), err)
				}
			}
			if current.Len() > 0 && (utf8.RuneCountInString(candidate) > chunkSize || (maxTokens > 0 && candidateTokens > maxTokens)) {
				if err := flush(); err != nil {
					return PackedPlan{}, err
				}
			}
			current.WriteString(fragment)
		}
		if err := flush(); err != nil {
			return PackedPlan{}, err
		}
	}
	return plan, nil
}

//nolint:gocognit,revive // The nested bounds enforce character and token limits without loss.
func losslessBoundedFragments(source string, chunkSize, maxTokens int, counter tokenizer.Tokenizer) ([]string, error) {
	var fragments []string
	for len(source) > 0 {
		end, runes := 0, 0
		for offset := range source {
			if runes == chunkSize {
				break
			}
			end = offset
			runes++
		}
		if runes < chunkSize {
			end = len(source)
		} else {
			_, size := utf8.DecodeRuneInString(source[end:])
			end += size
		}
		if end == 0 {
			_, size := utf8.DecodeRuneInString(source)
			end = size
		}
		if maxTokens > 0 && counter != nil {
			for end > 0 {
				tokens, err := counter.Count(source[:end])
				if err != nil {
					return nil, err
				}
				if tokens <= maxTokens {
					break
				}
				_, size := utf8.DecodeLastRuneInString(source[:end])
				end -= size
			}
			if end == 0 {
				return nil, fmt.Errorf("one rune exceeds token limit %d", maxTokens)
			}
		}
		fragments = append(fragments, source[:end])
		source = source[end:]
	}
	return fragments, nil
}

// exactSourceFragments refuses an ambiguous chunk plan rather than allowing a
// library normalization or dropped gap to silently change provider input.
func exactSourceFragments(source string, chunks []chunking.Chunk) ([]string, error) {
	if source == "" {
		return nil, nil
	}
	byText := make([]string, len(chunks))
	var joined strings.Builder
	for i, chunk := range chunks {
		byText[i] = chunk.Text
		joined.WriteString(chunk.Text)
	}
	if joined.String() == source {
		return byText, nil
	}
	bySpan := make([]string, len(chunks))
	pos := 0
	for i, chunk := range chunks {
		if chunk.StartChar != pos || chunk.EndChar < chunk.StartChar || chunk.EndChar > len(source) {
			return nil, errors.New("chunk geometry does not cover source byte-for-byte")
		}
		end := len(source)
		if i+1 < len(chunks) {
			end = chunks[i+1].StartChar
		}
		if end < chunk.StartChar || end > len(source) {
			return nil, errors.New("chunk geometry has an invalid next boundary")
		}
		bySpan[i] = source[chunk.StartChar:end]
		pos = end
	}
	if pos != len(source) {
		return nil, fmt.Errorf("chunk geometry ends at byte %d of %d", pos, len(source))
	}
	return bySpan, nil
}

func digestHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
