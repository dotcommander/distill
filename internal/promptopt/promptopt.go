package promptopt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type LogRecord struct {
	Iter          int     `json:"iter"`
	PromptSHA256  string  `json:"prompt_sha256"`
	Operator      string  `json:"operator"`
	Recall        float64 `json:"recall"`
	HoldoutRecall float64 `json:"holdout_recall,omitempty"`
	CallsUsed     int     `json:"calls_used"`
	Accepted      bool    `json:"accepted"`
}

func HashPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

func ValidatePrompt(prompt string) error {
	if !strings.Contains(prompt, "{{CHUNK}}") {
		return errors.New("prompt missing {{CHUNK}} placeholder")
	}
	if !strings.Contains(prompt, "{{CHUNK_ID}}") {
		return errors.New("prompt missing {{CHUNK_ID}} placeholder")
	}
	return nil
}

func ShouldAccept(candidateRecall float64, candidatePrompt string, bestRecall float64, bestPrompt string) bool {
	if candidateRecall > bestRecall {
		return true
	}
	return candidateRecall == bestRecall && len(candidatePrompt) < len(bestPrompt)
}

func AppendLog(path string, rec LogRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("promptopt: marshal log: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("promptopt: open log: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("promptopt: write log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("promptopt: close log: %w", err)
	}
	return nil
}
