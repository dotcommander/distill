package cmd

import "testing"

func TestEmbeddingProviderResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		flagProvider string
		baseURL      string
		local        bool
		want         string
	}{
		{name: "local wins", baseURL: "http://127.0.0.1:8000/v1", local: true, want: "local"},
		{name: "openrouter endpoint", baseURL: "https://openrouter.ai/api/v1", want: "openrouter"},
		{name: "gemini endpoint", baseURL: "https://generativelanguage.googleapis.com/v1beta", want: "gemini"},
		{name: "openai endpoint", baseURL: "https://api.openai.com/v1", want: "openai"},
		{name: "explicit gemini", flagProvider: "gemini", baseURL: "https://openrouter.ai/api/v1", want: "gemini"},
		{name: "unknown explicit", flagProvider: "custom", baseURL: "https://openrouter.ai/api/v1", want: ""},
		{name: "unknown endpoint", baseURL: "https://api.example.com/v1", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := embeddingProvider(tt.flagProvider, tt.baseURL, tt.local); got != tt.want {
				t.Fatalf("embeddingProvider = %q, want %q", got, tt.want)
			}
		})
	}
}
