// Package prompts loads the LLM prompt templates used by the digest pipeline.
// The default prompt text lives in embedded .md files (config data, not Go
// string literals); on first run the defaults are materialized under the user
// config dir so they can be edited, and are always read back from there.
package prompts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/distill/internal/fsutil"
)

//go:embed defaults/research.md defaults/fuse.md defaults/judge.md defaults/merit-judge.md defaults/recognize.md defaults/label-classification.md defaults/label-sentiment.md defaults/comedy-judge.md defaults/code.md defaults/trace-go.md defaults/publish-judge.md defaults/outline.md defaults/section.md defaults/edit-section.md defaults/context-preamble.md defaults/repair.md defaults/doc-context.md defaults/doc-header-preamble.md defaults/cite-section.md defaults/cite-edit.md defaults/cite-repair.md defaults/precision.md defaults/precision-repair.md defaults/merge-facts.md defaults/cluster-labels.md defaults/optimize-mutate.md
var defaults embed.FS

// Set holds the resolved prompt templates for one digest run.
type Set struct {
	Research            string
	Fuse                string
	Outline             string
	Section             string
	EditSection         string
	Repair              string
	ContextPreamble     string
	DocContext          string
	DocHeaderPreamble   string
	CiteSection         string
	CiteEdit            string
	CiteRepair          string
	Precision           string
	PrecisionRepair     string
	MergeFacts          string
	ClusterLabels       string
	OptimizeMutate      string
	Judge               string
	MeritJudge          string
	Recognize           string
	LabelClassification string
	LabelSentiment      string
	ComedyJudge         string
	Code                string
	TraceGo             string
	PublishJudge        string
}

// Load materializes the default prompt files under <configDir>/distill/prompts
// on first run, then reads the (possibly user-edited) prompts back.
func Load() (*Set, error) {
	dir, err := promptsDir()
	if err != nil {
		return nil, err
	}
	return loadFrom(dir)
}

// loadFrom resolves both templates from dir, writing embedded defaults first
// where absent. Separated from Load so tests can target a temp dir.
func loadFrom(dir string) (*Set, error) {
	meta, err := loadPromptMeta(dir)
	if err != nil {
		return nil, err
	}
	dirty := false
	load := func(name string) (string, error) {
		return loadOne(dir, meta, &dirty, name)
	}
	research, err := load("research.md")
	if err != nil {
		return nil, err
	}
	fuse, err := load("fuse.md")
	if err != nil {
		return nil, err
	}
	outline, err := load("outline.md")
	if err != nil {
		return nil, err
	}
	section, err := load("section.md")
	if err != nil {
		return nil, err
	}
	editSection, err := load("edit-section.md")
	if err != nil {
		return nil, err
	}
	repair, err := load("repair.md")
	if err != nil {
		return nil, err
	}
	contextPreamble, err := load("context-preamble.md")
	if err != nil {
		return nil, err
	}
	docContext, err := load("doc-context.md")
	if err != nil {
		return nil, err
	}
	docHeaderPreamble, err := load("doc-header-preamble.md")
	if err != nil {
		return nil, err
	}
	citeSection, err := load("cite-section.md")
	if err != nil {
		return nil, err
	}
	citeEdit, err := load("cite-edit.md")
	if err != nil {
		return nil, err
	}
	citeRepair, err := load("cite-repair.md")
	if err != nil {
		return nil, err
	}
	precision, err := load("precision.md")
	if err != nil {
		return nil, err
	}
	precisionRepair, err := load("precision-repair.md")
	if err != nil {
		return nil, err
	}
	mergeFacts, err := load("merge-facts.md")
	if err != nil {
		return nil, err
	}
	clusterLabels, err := load("cluster-labels.md")
	if err != nil {
		return nil, err
	}
	optimizeMutate, err := load("optimize-mutate.md")
	if err != nil {
		return nil, err
	}
	judge, err := load("judge.md")
	if err != nil {
		return nil, err
	}
	meritJudge, err := load("merit-judge.md")
	if err != nil {
		return nil, err
	}
	recognize, err := load("recognize.md")
	if err != nil {
		return nil, err
	}
	labelClass, err := load("label-classification.md")
	if err != nil {
		return nil, err
	}
	labelSent, err := load("label-sentiment.md")
	if err != nil {
		return nil, err
	}
	comedyJudge, err := load("comedy-judge.md")
	if err != nil {
		return nil, err
	}
	publishJudge, err := load("publish-judge.md")
	if err != nil {
		return nil, err
	}
	code, err := load("code.md")
	if err != nil {
		return nil, err
	}
	traceGo, err := load("trace-go.md")
	if err != nil {
		return nil, err
	}
	if dirty {
		if err := writePromptMeta(dir, meta); err != nil {
			return nil, err
		}
	}
	return &Set{
		Research:            research,
		Fuse:                fuse,
		Outline:             outline,
		Section:             section,
		EditSection:         editSection,
		Repair:              repair,
		ContextPreamble:     contextPreamble,
		DocContext:          docContext,
		DocHeaderPreamble:   docHeaderPreamble,
		CiteSection:         citeSection,
		CiteEdit:            citeEdit,
		CiteRepair:          citeRepair,
		Precision:           precision,
		PrecisionRepair:     precisionRepair,
		MergeFacts:          mergeFacts,
		ClusterLabels:       clusterLabels,
		OptimizeMutate:      optimizeMutate,
		Judge:               judge,
		MeritJudge:          meritJudge,
		Recognize:           recognize,
		LabelClassification: labelClass,
		LabelSentiment:      labelSent,
		ComedyJudge:         comedyJudge,
		PublishJudge:        publishJudge,
		Code:                code,
		TraceGo:             traceGo,
	}, nil
}

const promptMetaName = ".embedded-hashes.json"

// loadOne reads <dir>/<name>, writing the embedded default first if it is
// absent. If the file still matches the last embedded default materialized by
// distill, it is refreshed when the embedded default changes. User-edited files
// are preserved because their content hash no longer matches the tracked hash.
func loadOne(dir string, meta map[string]string, dirty *bool, name string) (string, error) {
	path := filepath.Join(dir, name)
	def, derr := defaults.ReadFile("defaults/" + name)
	if derr != nil {
		return "", fmt.Errorf("prompts: reading embedded default %s: %w", name, derr)
	}
	currentHash := hashPromptBytes(def)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if werr := fsutil.WriteFile(path, def, 0o644); werr != nil {
			return "", fmt.Errorf("prompts: materializing default %s: %w", name, werr)
		}
		meta[name] = currentHash
		*dirty = true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompts: reading %s: %w", name, err)
	}
	if oldHash := meta[name]; oldHash != "" && oldHash != currentHash && hashPromptBytes(data) == oldHash {
		if werr := fsutil.WriteFile(path, def, 0o644); werr != nil {
			return "", fmt.Errorf("prompts: refreshing default %s: %w", name, werr)
		}
		data = def
		meta[name] = currentHash
		*dirty = true
	} else if meta[name] == "" {
		meta[name] = currentHash
		*dirty = true
	}
	return string(data), nil
}

func loadPromptMeta(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, promptMetaName))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prompts: reading %s: %w", promptMetaName, err)
	}
	meta := map[string]string{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("prompts: parsing %s: %w", promptMetaName, err)
	}
	return meta, nil
}

func writePromptMeta(dir string, meta map[string]string) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("prompts: encoding %s: %w", promptMetaName, err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFile(filepath.Join(dir, promptMetaName), data, 0o644); err != nil {
		return fmt.Errorf("prompts: writing %s: %w", promptMetaName, err)
	}
	return nil
}

func hashPromptBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// promptsDir returns <XDG_CONFIG_HOME or ~/.config>/distill/prompts.
func promptsDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("prompts: resolving home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "distill", "prompts"), nil
}

// RenderJudge fills the judge template with the chunk id, candidate name, and the
// source / reference / candidate extraction texts.
func (s *Set) RenderJudge(chunkID, candidate, source, reference, extraction string) string {
	return strings.NewReplacer(
		"{{CHUNK_ID}}", chunkID,
		"{{CANDIDATE}}", candidate,
		"{{SOURCE}}", source,
		"{{REFERENCE}}", reference,
		"{{CANDIDATE_EXTRACTION}}", extraction,
	).Replace(s.Judge)
}

// RenderMeritJudge fills the pairwise merit-judge template with the source and
// the two anonymized versions (A and B) under comparison.
func (s *Set) RenderMeritJudge(source, versionA, versionB string) string {
	return strings.NewReplacer(
		"{{SOURCE}}", source,
		"{{VERSION_A}}", versionA,
		"{{VERSION_B}}", versionB,
	).Replace(s.MeritJudge)
}

func (s *Set) RenderRecognize(digests []string) string {
	var block strings.Builder
	for i, d := range digests {
		fmt.Fprintf(&block, "=== [%d] ===\n%s\n\n", i, d)
	}
	return strings.NewReplacer("{{DIGESTS}}", block.String()).Replace(s.Recognize)
}

// RenderResearch fills the research template with the chunk id and source text.
func (s *Set) RenderResearch(chunkID, chunk string) string {
	return strings.NewReplacer("{{CHUNK_ID}}", chunkID, "{{CHUNK}}", chunk).Replace(s.Research)
}

// RenderFuse fills the fuse template with the gathered per-chunk notes.
func (s *Set) RenderFuse(notes string) string {
	return strings.NewReplacer("{{NOTES}}", notes).Replace(s.Fuse)
}

// RenderOutline fills the outline template with the target style and the facts.
func (s *Set) RenderOutline(style, facts string) string {
	return strings.NewReplacer("{{STYLE}}", style, "{{FACTS}}", facts).Replace(s.Outline)
}

// RenderSection fills the section-writer template: target style, the full outline,
// all facts, the sections already written (prior), and this section's heading and intent.
func (s *Set) RenderSection(style, outline, facts, prior, heading, intent string) string {
	return strings.NewReplacer(
		"{{STYLE}}", style,
		"{{OUTLINE}}", outline,
		"{{FACTS}}", facts,
		"{{PRIOR}}", prior,
		"{{HEADING}}", heading,
		"{{INTENT}}", intent,
	).Replace(s.Section)
}

// RenderEditSection fills the windowed-editor template: target style, the
// stable full draft article, already accepted prior edited sections, the facts,
// and the heading of the one section to rewrite.
func (s *Set) RenderEditSection(style, article, prior, facts, heading string) string {
	return strings.NewReplacer(
		"{{STYLE}}", style,
		"{{ARTICLE}}", article,
		"{{PRIOR_ACCEPTED}}", prior,
		"{{FACTS}}", facts,
		"{{HEADING}}", heading,
	).Replace(s.EditSection)
}

// RenderRepair fills the reinsert-only repair template with the current article
// and the newline-joined specifics that did not survive into it.
func (s *Set) RenderRepair(article, missing string) string {
	return strings.NewReplacer("{{ARTICLE}}", article, "{{MISSING}}", missing).Replace(s.Repair)
}

// RenderContextPreamble fills the user-guidance preamble template with the
// caller-supplied steering context. Prepended to the writing-stage prompts when
// the user passes --context; empty context means it is never invoked.
func (s *Set) RenderContextPreamble(ctx string) string {
	return strings.NewReplacer("{{CONTEXT}}", ctx).Replace(s.ContextPreamble)
}

// RenderLabel fills the task template (classification or sentiment) with the
// allowed label set and the item text. task selects the template; allowed is the
// comma-joined taxonomy the model must choose from.
func (s *Set) RenderLabel(task, allowed, text string) string {
	tmpl := s.LabelClassification
	if task == "sentiment" {
		tmpl = s.LabelSentiment
	}
	return strings.NewReplacer("{{ALLOWED}}", allowed, "{{TEXT}}", text).Replace(tmpl)
}

// RenderComedyJudge fills the pairwise comedy-judge template with the topic brief
// and the two anonymized bits (A and B) under comparison. Mirrors RenderMeritJudge
// so it slots into the same judge call path.
func (s *Set) RenderComedyJudge(source, versionA, versionB string) string {
	return strings.NewReplacer(
		"{{SOURCE}}", source,
		"{{VERSION_A}}", versionA,
		"{{VERSION_B}}", versionB,
	).Replace(s.ComedyJudge)
}

// RenderPublishJudge fills the publication-editor template (same placeholders as
// the merit/comedy judges so it slots into judgeOnce + parseVerdict).
func (s *Set) RenderPublishJudge(source, versionA, versionB string) string {
	return strings.NewReplacer(
		"{{SOURCE}}", source,
		"{{VERSION_A}}", versionA,
		"{{VERSION_B}}", versionB,
	).Replace(s.PublishJudge)
}

// RenderCode fills the code-writing template with the required signature and the
// problem spec. Mirrors RenderLabel so it slots into the same Complete path.
func (s *Set) RenderCode(signature, prompt string) string {
	return strings.NewReplacer("{{SIGNATURE}}", signature, "{{PROMPT}}", prompt).Replace(s.Code)
}

// RenderTraceGo fills the Go program-tracing prompt with the program the model
// must simulate.
func (s *Set) RenderTraceGo(program string) string {
	return strings.NewReplacer("{{PROGRAM}}", program).Replace(s.TraceGo)
}

// RenderDocContext fills the doc-context template with the excerpt text and
// the headings list.
func (s *Set) RenderDocContext(excerpt, headings string) string {
	return strings.NewReplacer("{{EXCERPT}}", excerpt, "{{HEADINGS}}", headings).Replace(s.DocContext)
}

// RenderDocHeaderPreamble fills the doc-header-preamble template with the
// primary header text.
func (s *Set) RenderDocHeaderPreamble(header string) string {
	return strings.NewReplacer("{{HEADER}}", header).Replace(s.DocHeaderPreamble)
}

// RenderPrecision fills the sentence-level precision judge template. The facts
// block is first in the template so it stays stable across sentence batches.
func (s *Set) RenderPrecision(facts, sentences string) string {
	return strings.NewReplacer("{{FACTS}}", facts, "{{SENTENCES}}", sentences).Replace(s.Precision)
}

// RenderPrecisionRepair fills the precision-repair template. Facts are first so
// they stay stable across repair calls within the same document.
func (s *Set) RenderPrecisionRepair(facts, flagged, article string) string {
	return strings.NewReplacer("{{FACTS}}", facts, "{{FLAGGED}}", flagged, "{{ARTICLE}}", article).Replace(s.PrecisionRepair)
}

// RenderMergeFacts fills the duplicate-fact merge prompt for one clustered set
// of similar fact bullets.
func (s *Set) RenderMergeFacts(clusterID, facts string) string {
	return strings.NewReplacer("{{CLUSTER_ID}}", clusterID, "{{FACTS}}", facts).Replace(s.MergeFacts)
}

// RenderClusterLabels fills the outline-cluster label prompt with all clusters
// in one stable, numbered block.
func (s *Set) RenderClusterLabels(clusters string) string {
	return strings.NewReplacer("{{CLUSTERS}}", clusters).Replace(s.ClusterLabels)
}

// RenderOptimizeMutate fills the prompt-optimizer mutation template.
func (s *Set) RenderOptimizeMutate(operator, prompt, scoreReport string) string {
	return strings.NewReplacer(
		"{{OPERATOR}}", operator,
		"{{PROMPT}}", prompt,
		"{{SCORE_REPORT}}", scoreReport,
	).Replace(s.OptimizeMutate)
}
