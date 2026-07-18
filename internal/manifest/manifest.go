package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ChunkInfo struct {
	File      string `json:"file"`
	Tokens    *int   `json:"tokens,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Manifest struct {
	Source               string      `json:"source"`
	Mode                 string      `json:"mode"`
	Tokenizer            string      `json:"tokenizer,omitempty"`
	TokenCountsAvailable bool        `json:"token_counts_available"`
	Chunks               []ChunkInfo `json:"chunks"`
	TotalTokens          *int        `json:"total_tokens,omitempty"`
	TotalChunks          int         `json:"total_chunks"`
}

func WriteManifest(m *Manifest, dir string) error {
	data, err := ToJSON(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600)
}

func ToJSON(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
