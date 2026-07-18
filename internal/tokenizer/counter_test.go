package tokenizer

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTokenCounter_InvalidMaxTokens(t *testing.T) {
	t.Parallel()
	_, err := NewTokenCounter(0)
	require.Error(t, err)
}

func TestNewTokenCounter_PropagatesInitializationError(t *testing.T) {
	t.Parallel()
	want := errors.New("load encoding")
	_, err := newTokenCounter(512, func() (*openAITokenizer, error) {
		return nil, want
	})
	require.ErrorIs(t, err, want)
}

func TestTokenCounter_CountTokens(t *testing.T) {
	t.Parallel()
	tc, err := NewTokenCounter(512)
	require.NoError(t, err)

	require.Equal(t, 10, tc.CountTokens("The quick brown fox jumps over the lazy dog."))
}

func TestTokenCounter_MaxTokens(t *testing.T) {
	t.Parallel()
	tc, err := NewTokenCounter(1024)
	require.NoError(t, err)
	require.Equal(t, 1024, tc.MaxTokens())
}

func TestTokenizerEncodeAndCountAgree(t *testing.T) {
	t.Parallel()
	tok, err := New()
	require.NoError(t, err)

	tokens, err := tok.Encode("Hello, world!")
	require.NoError(t, err)
	count, err := tok.Count("Hello, world!")
	require.NoError(t, err)
	require.Equal(t, tokens, []int{9906, 11, 1917, 0})
	require.Len(t, tokens, count)
}

func TestCountLines(t *testing.T) {
	t.Parallel()
	require.Zero(t, CountLines(""))
	require.Equal(t, 2, CountLines("first\nsecond"))
}
