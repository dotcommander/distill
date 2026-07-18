package tokenizer

import "errors"

// TokenCounter adapts Distill's preflight tokenizer to chunking.TokenCounter.
type TokenCounter struct {
	tok       *openAITokenizer
	maxTokens int
}

// NewTokenCounter constructs a TokenCounter using Distill's tokenizer.
// maxTokens must be > 0; returns an error if it is not or if the tokenizer
// cannot be initialised.
func NewTokenCounter(maxTokens int) (*TokenCounter, error) {
	return newTokenCounter(maxTokens, newOpenAITokenizer)
}

func newTokenCounter(maxTokens int, newTokenizer func() (*openAITokenizer, error)) (*TokenCounter, error) {
	if maxTokens <= 0 {
		return nil, errors.New("tokenizer: maxTokens must be > 0")
	}
	tok, err := newTokenizer()
	if err != nil {
		return nil, err
	}
	return &TokenCounter{tok: tok, maxTokens: maxTokens}, nil
}

// CountTokens returns the cl100k_base estimate required by Reliquary's
// non-error TokenCounter contract. Encoding initialization has already
// succeeded, and EncodeOrdinary itself has no error path.
func (c *TokenCounter) CountTokens(text string) int {
	return len(c.tok.enc.EncodeOrdinary(text))
}

// MaxTokens returns the maximum allowed tokens per chunk.
func (c *TokenCounter) MaxTokens() int {
	return c.maxTokens
}
