package digest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkerRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if ArtifactsMatchSource(dir, "source text", 6000, 4000) {
		t.Fatal("expected no match before a marker is written")
	}
	if err := writeRunMarker(dir, markerFor("source text", 6000, 4000)); err != nil {
		t.Fatalf("writeRunMarker: %v", err)
	}
	if !ArtifactsMatchSource(dir, "source text", 6000, 4000) {
		t.Fatal("expected match for identical inputs")
	}
	cases := []struct {
		name              string
		source            string
		chunkSize, maxTok int
	}{
		{"source changed", "other text", 6000, 4000},
		{"chunk size changed", "source text", 5000, 4000},
		{"max tokens changed", "source text", 6000, 2000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if ArtifactsMatchSource(dir, tc.source, tc.chunkSize, tc.maxTok) {
				t.Fatal("expected mismatch")
			}
		})
	}
}

func TestMarkerAbsentDirAndCorruptJSON(t *testing.T) {
	t.Parallel()
	if ArtifactsMatchSource(filepath.Join(t.TempDir(), "missing"), "s", 6000, 4000) {
		t.Fatal("expected no match for absent dir")
	}
	if ArtifactsMatchSource("", "s", 6000, 4000) {
		t.Fatal("expected no match for empty dir")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, markerName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ArtifactsMatchSource(dir, "s", 6000, 4000) {
		t.Fatal("expected no match for corrupt marker")
	}
}

func TestPathWithin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		dir  string
		path string
		want bool
	}{
		{"inside", "/a/b", "/a/b/c.md", true},
		{"nested inside", "/a/b", "/a/b/c/d.md", true},
		{"equal", "/a/b", "/a/b", true},
		{"sibling", "/a/b", "/a/x/c.md", false},
		{"parent escape", "/a/b", "/a/b/../c.md", false},
		{"empty dir", "", "/a/b", false},
		{"empty path", "/a/b", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pathWithin(tc.dir, tc.path); got != tc.want {
				t.Fatalf("pathWithin(%q, %q) = %v, want %v", tc.dir, tc.path, got, tc.want)
			}
		})
	}
}
