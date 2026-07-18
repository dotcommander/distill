package promptopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePromptRequiresPlaceholders(t *testing.T) {
	t.Parallel()
	if err := ValidatePrompt("extract {{CHUNK_ID}} {{CHUNK}}"); err != nil {
		t.Fatalf("valid prompt rejected: %v", err)
	}
	if err := ValidatePrompt("extract {{CHUNK_ID}}"); err == nil {
		t.Fatal("expected missing CHUNK error")
	}
}

func TestShouldAccept(t *testing.T) {
	t.Parallel()
	if !ShouldAccept(0.8, "long prompt", 0.7, "short") {
		t.Fatal("higher recall should be accepted")
	}
	if !ShouldAccept(0.7, "short", 0.7, "much longer") {
		t.Fatal("shorter prompt should win recall ties")
	}
	if ShouldAccept(0.6, "short", 0.7, "long") {
		t.Fatal("lower recall should not be accepted")
	}
}

func TestAppendLog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "score-log.jsonl")
	if err := AppendLog(path, LogRecord{Iter: 1, PromptSHA256: HashPrompt("p"), Recall: 0.5, CallsUsed: 2}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), `"iter":1`) || !strings.Contains(string(data), `"calls_used":2`) {
		t.Fatalf("unexpected log: %s", data)
	}
}
