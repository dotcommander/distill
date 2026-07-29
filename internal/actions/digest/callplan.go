package digest

import "fmt"

// CallReuse records verified checkpoint work that does not need a first
// provider attempt. Counts are units, not provider requests.
type CallReuse struct {
	Research int `json:"research"`
	Outline  int `json:"outline"`
	Sections int `json:"sections"`
	Edits    int `json:"edits"`
}

// CallPlan is the authoritative per-run request plan. MandatoryCalls counts
// first attempts after verified reuse; recovery permits are intentionally kept
// outside that count so preflight can reserve the configured headroom.
type CallPlan struct {
	PackedPlan          PackedPlan `json:"packed_plan"`
	SectionCap          int        `json:"section_cap"`
	MandatoryCalls      int        `json:"mandatory_calls"`
	ConfiguredWorstCase int        `json:"configured_worst_case"`
	HardLimit           int        `json:"hard_limit"`
	RecoveryHeadroom    int        `json:"recovery_headroom"`
	Reused              CallReuse  `json:"reused"`
	Edit                bool       `json:"edit"`
	// StageCalls are additional mandatory first attempts for enabled optional
	// stages (doc-context, fuse, merge, cascade, repair, precision, etc.).
	// Keys are stable stage names; values must be non-negative.
	StageCalls map[string]int `json:"stage_calls,omitempty"`
}

// WithStageCalls returns a copy including explicit optional mandatory stages.
// It is deliberately data-driven so CLI policy remains the single owner of
// which polished-mode features are enabled.
func (p CallPlan) WithStageCalls(calls map[string]int, primaryAttempts int) (CallPlan, error) {
	p.StageCalls = make(map[string]int, len(calls))
	for stage, n := range calls {
		if n < 0 {
			return CallPlan{}, fmt.Errorf("digest: call plan stage %q has negative calls", stage)
		}
		if n > 0 {
			p.StageCalls[stage] = n
		}
	}
	p.recalculate(primaryAttempts)
	return p, nil
}

// NewCallPlan builds the conservative plan before an outline is available.
// primaryAttempts includes the initial request; recovery headroom reserves one
// primary retry and one fallback for finite runs.
func NewCallPlan(packed PackedPlan, sectionCap int, edit bool, maxCalls, primaryAttempts int) (CallPlan, error) {
	if sectionCap < 1 {
		sectionCap = max(3, len(packed.Chunks))
	}
	if primaryAttempts < 1 {
		primaryAttempts = 1
	}
	p := CallPlan{PackedPlan: packed, SectionCap: sectionCap, HardLimit: maxCalls, Edit: edit}
	p.recalculate(primaryAttempts)
	return p, nil
}

// WithReuse returns a copy whose mandatory requests exclude verified reusable
// units. Values are clamped to the known plan geometry.
func (p CallPlan) WithReuse(reuse CallReuse, primaryAttempts int) CallPlan {
	reuse.Research = min(max(reuse.Research, 0), len(p.PackedPlan.Chunks))
	reuse.Outline = min(max(reuse.Outline, 0), 1)
	reuse.Sections = min(max(reuse.Sections, 0), p.SectionCap)
	if !p.Edit {
		reuse.Edits = 0
	} else {
		reuse.Edits = min(max(reuse.Edits, 0), p.SectionCap)
	}
	p.Reused = reuse
	p.recalculate(primaryAttempts)
	return p
}

// ReleaseUnusedSections updates the plan once a valid outline has established
// its actual section count, releasing reserved write/edit first attempts.
func (p *CallPlan) ReleaseUnusedSections(actual, primaryAttempts int) error {
	if actual < 1 || actual > p.SectionCap {
		return fmt.Errorf("digest: outline sections %d outside planned cap %d", actual, p.SectionCap)
	}
	p.SectionCap = actual
	p.Reused.Sections = min(p.Reused.Sections, actual)
	p.Reused.Edits = min(p.Reused.Edits, actual)
	p.recalculate(primaryAttempts)
	return nil
}

func (p *CallPlan) recalculate(primaryAttempts int) {
	research := len(p.PackedPlan.Chunks) - p.Reused.Research
	sections := p.SectionCap - p.Reused.Sections
	edits := 0
	if p.Edit {
		edits = p.SectionCap - p.Reused.Edits
	}
	extra := 0
	for _, n := range p.StageCalls {
		extra += n
	}
	p.MandatoryCalls = research + (1 - p.Reused.Outline) + sections + edits + extra
	if p.MandatoryCalls < 0 {
		p.MandatoryCalls = 0
	}
	// Unlimited executions preserve all configured primary retries. Finite
	// executions only promise the one primary retry plus one fallback reserve.
	if p.HardLimit == 0 {
		p.RecoveryHeadroom = p.MandatoryCalls * max(primaryAttempts-1, 0)
	} else {
		p.RecoveryHeadroom = min(2, max(0, p.HardLimit-p.MandatoryCalls))
	}
	p.ConfiguredWorstCase = p.MandatoryCalls + p.RecoveryHeadroom
}

// Preflight rejects a finite max-calls ceiling before artifact creation or
// provider construction. Mandatory first attempts must always fit; recovery is
// opportunistic but shown in the plan.
func (p CallPlan) Preflight() error {
	if p.HardLimit > 0 && p.MandatoryCalls > p.HardLimit {
		return fmt.Errorf("digest: --max-calls %d is below mandatory call plan %d (research=%d, outline=%d, sections=%d, edits=%d)", p.HardLimit, p.MandatoryCalls, len(p.PackedPlan.Chunks)-p.Reused.Research, 1-p.Reused.Outline, p.SectionCap-p.Reused.Sections, boolCount(p.Edit)*(p.SectionCap-p.Reused.Edits))
	}
	return nil
}

func boolCount(v bool) int {
	if v {
		return 1
	}
	return 0
}

// RemainingMandatory is the pre-runtime first-attempt requirement after
// checkpoint reuse. Runtime dispatchers can display it with their used count.
func (p CallPlan) RemainingMandatory() int { return p.MandatoryCalls }
