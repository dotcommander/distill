// Package comedyeval loads comedy-topic fixtures and orchestrates per-model bit
// generation for the comedy tournament (the subjective counterpart to the
// deterministic label eval).
package comedyeval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Topic is one comedy prompt: a stable id and the brief framing the topic.
type Topic struct {
	ID    string `json:"id"`
	Brief string `json:"brief"`
}

// TopicSet is the comedy fixture: a shared style directive plus the topics each
// model writes a bit for.
type TopicSet struct {
	Style  string  `json:"style"`
	Topics []Topic `json:"topics"`
}

// LoadTopics reads and validates a comedy fixture: non-empty style, at least one
// topic, unique non-empty ids and non-empty briefs.
func LoadTopics(path string) (TopicSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TopicSet{}, err
	}
	var ts TopicSet
	if err := json.Unmarshal(data, &ts); err != nil {
		return TopicSet{}, fmt.Errorf("comedyeval: parsing %s: %w", path, err)
	}
	if strings.TrimSpace(ts.Style) == "" {
		return TopicSet{}, fmt.Errorf("comedyeval: %s: empty style", path)
	}
	if len(ts.Topics) == 0 {
		return TopicSet{}, fmt.Errorf("comedyeval: %s: no topics", path)
	}
	seen := map[string]bool{}
	for _, t := range ts.Topics {
		if strings.TrimSpace(t.ID) == "" {
			return TopicSet{}, fmt.Errorf("comedyeval: %s: topic with empty id", path)
		}
		if seen[t.ID] {
			return TopicSet{}, fmt.Errorf("comedyeval: %s: duplicate topic id %q", path, t.ID)
		}
		seen[t.ID] = true
		if strings.TrimSpace(t.Brief) == "" {
			return TopicSet{}, fmt.Errorf("comedyeval: %s: topic %q empty brief", path, t.ID)
		}
	}
	return ts, nil
}
