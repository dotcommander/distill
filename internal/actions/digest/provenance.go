package digest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dotcommander/distill/internal/fsutil"
)

// RunProvenance supplies the stable inputs that decide whether a response can
// be reused. PromptHashes and UpstreamHash are keyed by stage/unit (for
// example "research/chunk-003" or "section/section-002"), never just role.
// Values are hashes or non-sensitive identifiers, never raw prompts or
// provider error bodies.
type RunProvenance struct {
	PromptHashes map[string]string `json:"prompt_hashes"`
	UpstreamHash map[string]string `json:"upstream_hashes"`
	Routes       map[string]Route  `json:"routes"`
	Parameters   map[string]string `json:"parameters"`
}

// ResponseProvenance is stored beside every reusable provider response.
type ResponseProvenance struct {
	Version      int               `json:"version"`
	Stage        string            `json:"stage"`
	ContentHash  string            `json:"content_sha256"`
	PromptHash   string            `json:"rendered_prompt_sha256"`
	UpstreamHash string            `json:"upstream_input_sha256"`
	Route        Route             `json:"route"`
	Parameters   map[string]string `json:"parameters"`
}

const responseProvenanceVersion = 1

func responseSidecarPath(responsePath string) string { return responsePath + ".meta.json" }

// BuildProvenanceParameters returns a stable, redacted description of every
// request-affecting recovery route. It binds reusable responses to the primary
// model, explicit fallback policy, retry count, and per-call timeout.
func BuildProvenanceParameters(routes RoleRoutes, attempts int, timeout time.Duration) map[string]string {
	roles := make([]string, 0, len(routes))
	for role := range routes {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	var policy strings.Builder
	for _, role := range roles {
		pair := routes[role]
		fmt.Fprintf(&policy, "%s=%s/%s", role, pair.Primary.Provider, pair.Primary.Model)
		if pair.Fallback.Available {
			fmt.Fprintf(&policy, "->%s/%s", pair.Fallback.Provider, pair.Fallback.Model)
		} else {
			policy.WriteString("->disabled")
		}
		policy.WriteByte('\n')
	}
	return map[string]string{
		"route_policy_sha256": digestHash(policy.String()),
		"primary_attempts":    strconv.Itoa(max(1, attempts)),
		"timeout":             timeout.String(),
	}
}

// WriteResponseProvenance atomically writes the metadata sidecar after the
// corresponding response checkpoint has been durably written.
func WriteResponseProvenance(responsePath string, meta ResponseProvenance) error {
	meta.Version = responseProvenanceVersion
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("digest: encode response provenance: %w", err)
	}
	if err := fsutil.WriteFile(responseSidecarPath(responsePath), data, 0o644); err != nil {
		return fmt.Errorf("digest: write response provenance %q: %w", responsePath, err)
	}
	return nil
}

// ReadResponseProvenance verifies the response and sidecar agree, including
// every request-affecting input in expected. Missing/corrupt sidecars are not
// reusable rather than silently falling back to weak file-presence reuse.
func ReadResponseProvenance(responsePath string, expected ResponseProvenance) (ResponseProvenance, bool) {
	data, err := os.ReadFile(responsePath)
	if err != nil || !artifactReusable(expected.Stage, string(data)) {
		return ResponseProvenance{}, false
	}
	metaData, err := os.ReadFile(responseSidecarPath(responsePath))
	if err != nil {
		return ResponseProvenance{}, false
	}
	var got ResponseProvenance
	if json.Unmarshal(metaData, &got) != nil || got.Version != responseProvenanceVersion {
		return ResponseProvenance{}, false
	}
	if got.ContentHash != digestHash(string(data)) || got.Stage != expected.Stage || got.PromptHash != expected.PromptHash || got.UpstreamHash != expected.UpstreamHash || got.Route.Provider == "" || got.Route.Model == "" || !routeMatchesExpected(got.Route, expected.Route) || !sameStringMap(got.Parameters, expected.Parameters) {
		return ResponseProvenance{}, false
	}
	return got, true
}

func routeMatchesExpected(got, expected Route) bool {
	if expected.Provider != "" && got.Provider != expected.Provider {
		return false
	}
	if expected.Model != "" && got.Model != expected.Model {
		return false
	}
	if expected.Kind != "" && got.Kind != expected.Kind {
		return false
	}
	return true
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func provenanceExpected(stage, upstream string, p RunProvenance) (ResponseProvenance, bool) {
	unitKey := stage + "/" + upstream
	promptHash, ok := p.PromptHashes[unitKey]
	if !ok || promptHash == "" || p.UpstreamHash[unitKey] == "" {
		return ResponseProvenance{}, false
	}
	route, ok := p.Routes[stage]
	if !ok || route.Provider == "" || route.Model == "" {
		return ResponseProvenance{}, false
	}
	return ResponseProvenance{Stage: stage, PromptHash: promptHash, UpstreamHash: p.UpstreamHash[unitKey], Route: route, Parameters: p.Parameters}, true
}

func requestProvenance(stage, prompt, upstream string, route Route, parameters map[string]string) ResponseProvenance {
	return ResponseProvenance{
		Stage:        stage,
		PromptHash:   digestHash(prompt),
		UpstreamHash: digestHash(upstream),
		Route:        route,
		Parameters:   parameters,
	}
}

func readProvenancedResponse(opts Options, path, stage, prompt, upstream string) (string, bool) {
	if opts.ProvenanceParameters == nil {
		return readReusableArtifact(path, stage)
	}
	expected := requestProvenance(stage, prompt, upstream, Route{}, opts.ProvenanceParameters)
	if _, ok := ReadResponseProvenance(path, expected); !ok {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

//nolint:revive // Checkpoint, request identity, route, and payload form one durable write.
func writeProvenancedResponse(opts Options, checkpoint, path, stage, prompt, upstream string, route Route, data []byte) error {
	if err := writeCheckpoint(opts, checkpoint, path, data); err != nil {
		return err
	}
	if opts.ProvenanceParameters == nil {
		return nil
	}
	meta := requestProvenance(stage, prompt, upstream, route, opts.ProvenanceParameters)
	meta.ContentHash = digestHash(string(data))
	return WriteResponseProvenance(path, meta)
}

// VerifiedCallReuse inspects only marker-bound response files with exact
// sidecars. An absent/new artifact dir returns zero reuse; a v1 or mismatched
// directory returns the actionable marker error without modifying it.
//
//nolint:gocognit,revive // Each response family needs strict, independent reuse checks.
func VerifiedCallReuse(artifactDir string, packed PackedPlan, provenance RunProvenance, sectionCap int, edit bool) (CallReuse, error) {
	if artifactDir == "" {
		return CallReuse{}, nil
	}
	reusable, err := ValidateArtifactBindingPlan(artifactDir, packed)
	if err != nil || !reusable {
		return CallReuse{}, err
	}
	var reuse CallReuse
	for _, chunk := range packed.Chunks {
		if expected, ok := provenanceExpected("research", chunk.ID, provenance); ok {
			if _, ok := ReadResponseProvenance(filepath.Join(artifactDir, "responses", chunk.ID+".md"), expected); ok {
				reuse.Research++
			}
		}
	}
	if expected, ok := provenanceExpected("outline", "outline", provenance); ok {
		if _, ok := ReadResponseProvenance(filepath.Join(artifactDir, "responses", "outline.md"), expected); ok {
			reuse.Outline = 1
		}
	}
	for i := 1; i <= sectionCap; i++ {
		unit := fmt.Sprintf("section-%03d", i)
		if expected, ok := provenanceExpected("section", unit, provenance); ok {
			if _, ok := ReadResponseProvenance(filepath.Join(artifactDir, "responses", unit+".md"), expected); ok {
				reuse.Sections++
			}
		}
		if edit {
			if expected, ok := provenanceExpected("edit", unit, provenance); ok {
				if _, ok := ReadResponseProvenance(filepath.Join(artifactDir, "responses", unit+".edited.md"), expected); ok {
					reuse.Edits++
				}
			}
		}
	}
	return reuse, nil
}
