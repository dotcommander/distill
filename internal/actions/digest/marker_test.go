package digest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	if _, err := ValidateArtifactBinding(dir, "s", 6000, 4000); err == nil {
		t.Fatal("expected corrupt marker validation failure")
	}
}

func TestPrepareArtifactBindingCreatesAndStampsAbsentOrEmptyDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		dir  string
	}{
		{name: "absent", dir: filepath.Join(root, "absent")},
		{name: "empty", dir: filepath.Join(root, "empty")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "empty" {
				if err := os.MkdirAll(tc.dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			reuseOK, err := PrepareArtifactBinding(tc.dir, "source", 6000, 4000)
			if err != nil {
				t.Fatalf("PrepareArtifactBinding: %v", err)
			}
			if reuseOK {
				t.Fatal("new artifact directory must not report reusable artifacts")
			}
			if !ArtifactsMatchSource(tc.dir, "source", 6000, 4000) {
				t.Fatal("prepared directory does not carry the current marker")
			}
		})
	}
}

type invalidArtifactBindingCase struct {
	name  string
	setup func(t *testing.T, dir string)
	want  string
}

func TestArtifactBindingRejectsInvalidNonEmptyDirectoryWithoutMutation(t *testing.T) {
	t.Parallel()
	for _, tc := range invalidArtifactBindingCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			assertInvalidArtifactBindingUnchanged(t, dir, tc.want)
		})
	}
}

func invalidArtifactBindingCases() []invalidArtifactBindingCase {
	return []invalidArtifactBindingCase{
		{name: "missing marker", setup: setupMissingArtifactMarker, want: "source marker missing"},
		{name: "corrupt marker", setup: setupCorruptArtifactMarker, want: "source marker corrupt"},
		{name: "mismatched marker", setup: setupMismatchedArtifactMarker, want: "source marker mismatch"},
	}
}

func setupMissingArtifactMarker(t *testing.T, dir string) {
	t.Helper()
	writeArtifactSentinel(t, dir)
}

func setupCorruptArtifactMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, markerName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeArtifactSentinel(t, dir)
}

func setupMismatchedArtifactMarker(t *testing.T, dir string) {
	t.Helper()
	if err := writeRunMarker(dir, markerFor("other source", 6000, 4000)); err != nil {
		t.Fatal(err)
	}
	writeArtifactSentinel(t, dir)
}

func writeArtifactSentinel(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "sentinel"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertInvalidArtifactBindingUnchanged(t *testing.T, dir, want string) {
	t.Helper()
	beforeMarker, markerErr := os.ReadFile(filepath.Join(dir, markerName))
	beforeSentinel, sentinelErr := os.ReadFile(filepath.Join(dir, "sentinel"))
	_, validateErr := ValidateArtifactBinding(dir, "source", 6000, 4000)
	assertArtifactBindingError(t, validateErr, "ValidateArtifactBinding", want)
	_, prepareErr := PrepareArtifactBinding(dir, "source", 6000, 4000)
	assertArtifactBindingError(t, prepareErr, "PrepareArtifactBinding", want)
	assertArtifactFileUnchanged(t, "marker", beforeMarker, markerErr, filepath.Join(dir, markerName))
	assertArtifactFileUnchanged(t, "sentinel", beforeSentinel, sentinelErr, filepath.Join(dir, "sentinel"))
}

func assertArtifactBindingError(t *testing.T, err error, operation, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want %q", operation, err, want)
	}
}

func assertArtifactFileUnchanged(t *testing.T, name string, before []byte, beforeErr error, path string) {
	t.Helper()
	after, afterErr := os.ReadFile(path)
	if os.IsNotExist(beforeErr) {
		if !os.IsNotExist(afterErr) {
			t.Fatalf("missing %s was created: %q", name, after)
		}
		return
	}
	if afterErr != nil || string(after) != string(before) {
		t.Fatalf("%s changed: before=%q after=%q err=%v", name, before, after, afterErr)
	}
}

func TestPrepareArtifactBindingConcurrentClaimsDoNotRebind(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "artifacts")
	sources := []string{"source alpha", "source beta"}
	start := make(chan struct{})
	errs := make([]error, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Go(func() {
			<-start
			_, errs[i] = PrepareArtifactBinding(dir, source, 6000, 4000)
		})
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful claims = %d, want exactly 1; errors=%v", successes, errs)
	}
	matches := 0
	for _, source := range sources {
		if ArtifactsMatchSource(dir, source, 6000, 4000) {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("matching source bindings = %d, want exactly 1", matches)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read claim root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "artifacts" {
		t.Fatalf("marker claims left temporary files: %v", entries)
	}
}

func TestClaimRunMarkerTemporaryCreationFailureLeavesNoMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions are not deterministic on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "artifacts")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := claimRunMarker(dir, markerFor("source", 6000, 4000)); err == nil {
		t.Fatal("claimRunMarker succeeded in a non-writable directory")
	}
	if _, err := os.Stat(filepath.Join(dir, markerName)); !os.IsNotExist(err) {
		t.Fatalf("failed claim left final marker: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read claim root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "artifacts" {
		t.Fatalf("failed claim left temporary files: %v", entries)
	}
}

func TestClaimRunMarkerExistingClaimPreservesWinnerAndCleansTemporaryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := claimRunMarker(dir, markerFor("winner", 6000, 4000)); err != nil {
		t.Fatalf("install winner marker: %v", err)
	}
	path := filepath.Join(dir, markerName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read winner marker: %v", err)
	}

	err = claimRunMarker(dir, markerFor("loser", 6000, 4000))
	if !os.IsExist(err) {
		t.Fatalf("loser claim error = %v, want os.ErrExist", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read winner marker after collision: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("winner marker changed: before=%q after=%q", before, after)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read artifact directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != markerName {
		t.Fatalf("loser claim left temporary files: %v", entries)
	}
}

func TestPrepareArtifactBindingWritableDirectoryUnderReadOnlyParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions are not deterministic on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "artifacts")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	if _, err := PrepareArtifactBinding(dir, "source", 6000, 4000); err != nil {
		t.Fatalf("prepare binding in writable artifact directory: %v", err)
	}
	if !ArtifactsMatchSource(dir, "source", 6000, 4000) {
		t.Fatal("prepared marker does not match source")
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
