package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEmbedBatchReservesEverySubBatch(t *testing.T) {
	for _, count := range []int{64, 65, 129} {
		t.Run(fmt.Sprintf("inputs_%d", count), func(t *testing.T) {
			testEmbedBatchReservation(t, count)
		})
	}
}

func testEmbedBatchReservation(t *testing.T, count int) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(embedBatchHandler(t, &calls))
	defer server.Close()

	budget, _ := NewRequestBudget(0)
	client, err := New(Config{Provider: "local", BaseURL: server.URL, APIKey: "test-key", EmbeddingModel: "embed-test", RequestBudget: budget})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.EmbedBatch(context.Background(), make([]string, count)); err != nil {
		t.Fatalf("embed: %v", err)
	}
	want := (count + embedBatchSize - 1) / embedBatchSize
	if got := int(calls.Load()); got != want {
		t.Fatalf("provider calls = %d, want %d", got, want)
	}
	if got := budget.Used(); got != want {
		t.Fatalf("budget used = %d, want %d", got, want)
	}
}

func embedBatchHandler(t *testing.T, calls *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("path = %s, want /embeddings", r.URL.Path)
		}
		calls.Add(1)
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": []float64{float64(i)}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "embed-test"})
	}
}

func TestEmbedBatchStopsBeforeOverLimitSend(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": make([]any, embedBatchSize), "model": "embed-test"})
	}))
	defer server.Close()

	budget, _ := NewRequestBudget(1)
	client, err := New(Config{Provider: "local", BaseURL: server.URL, APIKey: "test-key", EmbeddingModel: "embed-test", RequestBudget: budget, NoRetries: true})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.EmbedBatch(context.Background(), make([]string, 65)); !errors.Is(err, ErrRequestBudgetExhausted) {
		t.Fatalf("embed error = %v, want exhaustion", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}
