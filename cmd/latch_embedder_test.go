package cmd

import (
	"context"
	"errors"
	"testing"
)

type flakyEmbedder struct {
	calls int
	fail  map[int]error
}

func (f *flakyEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if err := f.fail[f.calls]; err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}

func TestLatchEmbedderRecordsFirstError(t *testing.T) {
	t.Parallel()
	first := errors.New("rate limited")
	second := errors.New("later failure")
	latch := &latchEmbedder{inner: &flakyEmbedder{fail: map[int]error{2: first, 3: second}}}

	if _, err := latch.EmbedBatch(context.Background(), []string{"a"}); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	if latch.Err() != nil {
		t.Fatalf("no error should be latched yet, got %v", latch.Err())
	}
	if _, err := latch.EmbedBatch(context.Background(), []string{"b"}); !errors.Is(err, first) {
		t.Fatalf("second batch should fail with the injected error, got %v", err)
	}
	if _, err := latch.EmbedBatch(context.Background(), []string{"c"}); !errors.Is(err, second) {
		t.Fatalf("third batch error passthrough, got %v", err)
	}
	if !errors.Is(latch.Err(), first) {
		t.Fatalf("latch must keep the FIRST error, got %v", latch.Err())
	}
	vecs, err := latch.EmbedBatch(context.Background(), []string{"d", "e"})
	if err != nil || len(vecs) != 2 {
		t.Fatalf("vectors must pass through after errors: %v, %d", err, len(vecs))
	}
	if !errors.Is(latch.Err(), first) {
		t.Fatal("latched error must persist across later successes")
	}
}
