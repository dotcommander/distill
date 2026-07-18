package mdclean

import (
	"strings"
	"testing"
)

func TestStripBinaryBlobs(t *testing.T) {
	t.Parallel()
	blob := strings.Repeat("AB/c+9", 300) // 1800 base64 chars, above the 1024 floor
	tests := []struct {
		name     string
		in       string
		wantKeep string
		wantGone string
		removed  bool
	}{
		{"markdown data image", "intro ![logo](data:image/png;base64," + blob + ") outro", "intro", "data:image", true},
		{"html data image", `text <img src="data:image/gif;base64,` + blob + `"> more`, "text", "data:image", true},
		{"bare base64 run", "before " + blob + " after", "before", blob, true},
		{"prose untouched", "The quick brown fox jumps over https://example.com/a?b=1.", "quick brown fox", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, removed := StripBinaryBlobs(tt.in)
			if (removed > 0) != tt.removed {
				t.Fatalf("removed=%d, wantRemoved=%v", removed, tt.removed)
			}
			if !strings.Contains(got, tt.wantKeep) {
				t.Fatalf("expected %q to survive, got %q", tt.wantKeep, got)
			}
			if tt.wantGone != "" && strings.Contains(got, tt.wantGone) {
				t.Fatalf("expected %q removed, still present in %q", tt.wantGone, got)
			}
		})
	}
}
