// Package digestcache is a content-addressed disk cache for finished digest
// articles. A run whose every output-affecting input (source bytes, per-role
// models, prompt text, style, chunk/token budget, and the fuse/edit/appendix/
// context toggles) is unchanged hashes to the same key and can reuse the prior
// article instead of re-firing the four LLM stages. Modeled on internal/embedcache.
package digestcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dotcommander/distill/internal/actions/digest"
	"github.com/dotcommander/distill/internal/fsutil"
)

// KeyInputs holds every input that affects a digest's final article. Build it
// from the resolved run configuration and pass it to Key. Anything that does NOT
// change the article (output paths, concurrency, timeout, retries, resume/reuse
// toggles) is intentionally absent — including it would cause spurious misses.
type KeyInputs struct {
	Source  string // exact post-clean source bytes fed to research
	Profile string // routing profile (default/local/deepseek) — disambiguates provider
	BaseURL string // --base-url override (local-only); part of provider routing

	ResearchModel           string
	ResearchEscalationModel string // only folded in when Cascade is true
	EmbeddingModel          string // folded in when MergeFacts or TargetFacts are true
	OutlineModel            string
	WriteModel              string // the "section" writer model
	FuseModel               string // only folded in when Fuse is true
	EditModel               string // only folded in when Edit is true

	Style               string
	ChunkSize           int
	MaxTokens           int
	Fuse                bool
	Edit                bool
	Appendix            bool
	Repair              bool // when true, the verify→repair reinsert pass ran (changes the article)
	DocContext          bool // when true, research prompts include a generated document header
	Cite                bool // when true, generation uses temporary fact-id citation markers
	Cascade             bool // when true, weak fresh research chunks may get one escalation pass
	CascadeThreshold    float64
	MergeFacts          bool // when true, similar extracted facts may be merged before writing
	MergeThreshold      float64
	OutlineFromClusters bool   // when true, outline is synthesized from merge clusters
	TargetFacts         int    // when >0, only the selected fact budget feeds writing
	Context             string // resolved steering text (empty = no preamble)

	ResearchPrompt          string // resolved prompt template text (user-editable)
	OutlinePrompt           string
	SectionPrompt           string
	FusePrompt              string // folded in only when Fuse
	EditPrompt              string // EditSection template; folded in only when Edit
	RepairPrompt            string // Repair template; folded in only when Repair
	DocContextPrompt        string // folded in only when DocContext
	DocHeaderPreamblePrompt string // folded in only when DocContext
	CiteSectionPrompt       string // folded in only when Cite
	CiteEditPrompt          string // folded in only when Cite
	CiteRepairPrompt        string // folded in only when Cite && Repair
	MergeFactsPrompt        string // folded in only when MergeFacts
	ClusterLabelsPrompt     string // folded in only when OutlineFromClusters
	ContextPrompt           string // ContextPreamble template; folded in only when Context != ""
}

// keyFormatVersion is bumped if the key pre-image layout changes, so old cache
// entries can never be misread as current.
const keyFormatVersion = "digestcache/v1"

// Key returns the hex sha256 cache key for in. The pre-image is a labeled,
// newline-joined record; unbounded blobs (source, context, prompts) are folded
// in as their own sha256 so the pre-image stays small and unambiguous. Stages
// that do not run (fuse, edit, context preamble) contribute nothing, so toggling
// an unused model/prompt never changes the key.
func Key(in KeyInputs) string {
	var b strings.Builder
	add := func(label, val string) {
		b.WriteString(label)
		b.WriteByte('=')
		b.WriteString(val)
		b.WriteByte('\n')
	}
	b.WriteString(keyFormatVersion)
	b.WriteByte('\n')
	add("source", hashKey(in.Source))
	add("profile", in.Profile)
	add("baseurl", in.BaseURL)
	add("model.research", in.ResearchModel)
	add("model.outline", in.OutlineModel)
	add("model.section", in.WriteModel)
	add("style", in.Style)
	add("chunk_size", strconv.Itoa(in.ChunkSize))
	add("max_tokens", strconv.Itoa(in.MaxTokens))
	add("fuse", strconv.FormatBool(in.Fuse))
	add("edit", strconv.FormatBool(in.Edit))
	add("appendix", strconv.FormatBool(in.Appendix))
	add("repair", strconv.FormatBool(in.Repair))
	add("prompt.research", hashKey(in.ResearchPrompt))
	add("prompt.outline", hashKey(in.OutlinePrompt))
	add("prompt.section", hashKey(in.SectionPrompt))
	if in.Cascade {
		add("cascade", strconv.FormatBool(in.Cascade))
		add("cascade_threshold", strconv.FormatFloat(in.CascadeThreshold, 'g', -1, 64))
		add("model.research_escalation", in.ResearchEscalationModel)
	}
	if in.MergeFacts {
		add("merge_facts", strconv.FormatBool(in.MergeFacts))
		add("merge_threshold", strconv.FormatFloat(in.MergeThreshold, 'g', -1, 64))
		add("model.embedding", in.EmbeddingModel)
		add("prompt.merge_facts", hashKey(in.MergeFactsPrompt))
		add("outline_from_clusters", strconv.FormatBool(in.OutlineFromClusters))
		if in.OutlineFromClusters {
			add("prompt.cluster_labels", hashKey(in.ClusterLabelsPrompt))
		}
	}
	if in.TargetFacts > 0 {
		add("target_facts", strconv.Itoa(in.TargetFacts))
		add("model.embedding", in.EmbeddingModel)
	}
	if in.Fuse {
		add("model.fuse", in.FuseModel)
		add("prompt.fuse", hashKey(in.FusePrompt))
	}
	if in.Edit {
		add("model.edit", in.EditModel)
		add("prompt.edit", hashKey(in.EditPrompt))
	}
	if in.Repair {
		add("prompt.repair", hashKey(in.RepairPrompt))
	}
	if in.DocContext {
		add("doc_context", strconv.FormatBool(in.DocContext))
		add("prompt.doc_context", hashKey(in.DocContextPrompt))
		add("prompt.doc_header_preamble", hashKey(in.DocHeaderPreamblePrompt))
	}
	if in.Cite {
		add("cite", strconv.FormatBool(in.Cite))
		add("prompt.cite_section", hashKey(in.CiteSectionPrompt))
		add("prompt.cite_edit", hashKey(in.CiteEditPrompt))
		if in.Repair {
			add("prompt.cite_repair", hashKey(in.CiteRepairPrompt))
		}
	}
	if in.Context != "" {
		add("context", hashKey(in.Context))
		add("prompt.context", hashKey(in.ContextPrompt))
	}
	return hashKey(b.String())
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Cache is a disk cache of finished digest articles under a single directory.
// Entries are content-addressed: the filename IS the Key, so there is no
// eviction in v1 (a follow-up could prune by age/size).
type Cache struct {
	dir string
}

// New returns a Cache rooted at <os.UserCacheDir>/distill/digests/.
func New() (*Cache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("digestcache: resolving user cache dir: %w", err)
	}
	return newDir(filepath.Join(base, "distill", "digests"))
}

// newDir is the directory-explicit constructor used by tests.
func newDir(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("digestcache: creating dir: %w", err)
	}
	return &Cache{dir: dir}, nil
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, key+".md")
}

// sidecarVersion is bumped if the sidecar layout changes, so old metadata can
// never be misread as current.
const sidecarVersion = 1

// sidecar is the on-disk metadata stored beside each cached article.
type sidecar struct {
	Version int              `json:"version"`
	Meta    digest.CacheMeta `json:"meta"`
}

func (c *Cache) metaPath(key string) string {
	return filepath.Join(c.dir, key+".json")
}

// Load returns the cached article and its metrics for key; ok is false on any
// miss, unreadable or empty article, or missing/unparsable sidecar — a
// pre-sidecar entry is a miss, so it self-heals on the next store.
func (c *Cache) Load(key string) (article string, meta digest.CacheMeta, ok bool) {
	data, err := os.ReadFile(c.path(key))
	if err != nil || len(data) == 0 {
		return "", digest.CacheMeta{}, false
	}
	mdata, err := os.ReadFile(c.metaPath(key))
	if err != nil {
		return "", digest.CacheMeta{}, false
	}
	var sc sidecar
	if jerr := json.Unmarshal(mdata, &sc); jerr != nil || sc.Version != sidecarVersion {
		return "", digest.CacheMeta{}, false
	}
	return string(data), sc.Meta, true
}

// Store writes article and its metrics under key atomically. Errors are
// swallowed: the cache is an optimization, never a correctness dependency. A
// crash between the two writes leaves a pair Load rejects (self-healing miss).
func (c *Cache) Store(key, article string, meta digest.CacheMeta) {
	_ = fsutil.WriteFile(c.path(key), []byte(article), 0o600)
	data, err := json.Marshal(sidecar{Version: sidecarVersion, Meta: meta})
	if err != nil {
		return
	}
	_ = fsutil.WriteFile(c.metaPath(key), data, 0o600)
}
