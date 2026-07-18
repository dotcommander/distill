package tokenizer

import (
	"strings"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer is the code-owned boundary for replaceable token estimators.
type Tokenizer interface {
	Encode(text string) ([]int, error)
	Count(text string) (int, error)
}

type openAITokenizer struct {
	enc *tiktoken.Tiktoken
}

// New constructs the canonical tiktoken estimator used for preflight sizing.
// Claude does not publish a compatible tokenizer through tiktoken, so Distill
// uses cl100k_base consistently as an estimate rather than claiming exactness.
func New() (Tokenizer, error) {
	return newOpenAITokenizer()
}

func newOpenAITokenizer() (*openAITokenizer, error) {
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	if err != nil {
		return nil, err
	}

	return &openAITokenizer{enc: enc}, nil
}

func (t *openAITokenizer) Encode(text string) ([]int, error) {
	return t.enc.EncodeOrdinary(text), nil
}

func (t *openAITokenizer) Count(text string) (int, error) {
	tokens, err := t.Encode(text)
	return len(tokens), err
}

func CountLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}
