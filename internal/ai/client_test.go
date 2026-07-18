package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCompleteSendsProviderOptions(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"model":"test-model",
			"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	client, err := New(Config{
		Provider:  "openrouter",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		TextModel: "test-model",
		ProviderOptions: map[string]any{
			"session_id": "distill-digest-test",
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	got, err := client.Complete(context.Background(), "hello")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got != "ok" {
		t.Fatalf("complete = %q, want ok", got)
	}
	if payload["session_id"] != "distill-digest-test" {
		t.Fatalf("session_id = %v, want distill-digest-test", payload["session_id"])
	}
}

func TestOpenRouterUsesConfiguredEndpointAndKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/openrouter.ai/api/v1/chat/completions" {
			t.Fatalf("path = %q, want /openrouter.ai/api/v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"model":"test-model",
			"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	client, err := New(Config{
		Provider:  "openrouter",
		BaseURL:   server.URL + "/openrouter.ai/api/v1",
		APIKey:    "sk-or-explicit-router-key",
		TextModel: "test-model",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	got, err := client.Complete(context.Background(), "hello")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got != "ok" {
		t.Fatalf("complete = %q, want ok", got)
	}
	if gotAuth != "Bearer sk-or-explicit-router-key" {
		t.Fatalf("authorization = %q, want bearer token from Config.APIKey", gotAuth)
	}
}

func TestOpenRouterRetriesRateLimitInsideClientCall(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := attempts.Add(1); n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"model":"test-model",
			"choices":[{"message":{"role":"assistant","content":"ok after retry"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	endpoint, err := NewEndpoint(Config{
		Provider: "openrouter",
		BaseURL:  server.URL + "/openrouter.ai/api/v1",
		APIKey:   "sk-or-explicit-router-key",
	})
	if err != nil {
		t.Fatalf("new endpoint: %v", err)
	}
	client := endpoint.Client(Config{TextModel: "test-model"})

	got, err := client.Complete(context.Background(), "hello")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got != "ok after retry" {
		t.Fatalf("complete = %q, want retry response", got)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestDeepSeekDisablesThinkingByDefault(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"model":"deepseek-v4-pro",
			"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	client, err := New(Config{
		Provider:  "deepseek",
		BaseURL:   server.URL + "/api.deepseek.com",
		APIKey:    "deepseek-key",
		TextModel: "deepseek-v4-pro",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Complete(context.Background(), "hello"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking payload = %#v, want object", payload["thinking"])
	}
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking.type = %#v, want disabled", thinking["type"])
	}
}

func TestDeepSeekKeepsExplicitThinkingOption(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"model":"deepseek-v4-pro",
			"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	client, err := New(Config{
		Provider:  "deepseek",
		BaseURL:   server.URL + "/api.deepseek.com",
		APIKey:    "deepseek-key",
		TextModel: "deepseek-v4-pro",
		ProviderOptions: map[string]any{
			"thinking": map[string]any{"type": "enabled"},
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Complete(context.Background(), "hello"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking payload = %#v, want object", payload["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking.type = %#v, want explicit enabled", thinking["type"])
	}
}

func TestAPIKeyForProviderLocalNeverUsesOpenAIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-real-openai-key")
	t.Setenv("DISTILL_LOCAL_API_KEY", "")
	got := APIKeyForProvider("local")
	if got == "sk-real-openai-key" {
		t.Fatal("local provider must never default to OPENAI_API_KEY")
	}
	if got == "" {
		t.Fatal("local provider key must be non-empty (some servers reject empty bearer tokens)")
	}
	t.Setenv("DISTILL_LOCAL_API_KEY", "proxy-key")
	if got := APIKeyForProvider("local"); got != "proxy-key" {
		t.Fatalf("DISTILL_LOCAL_API_KEY must win, got %q", got)
	}
}
