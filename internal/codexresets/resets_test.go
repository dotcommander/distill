package codexresets

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunFetchesAndPrintsResetCredits(t *testing.T) {
	t.Parallel()

	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"test-token","chatgpt_account_id":"acct-123"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-ID = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"available_count":2,"credits":[{"expires_at":"2026-07-17T20:31:00Z"},{"expires":1780000000000}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	now := time.Date(2026, 6, 27, 19, 57, 0, 0, time.UTC)
	var out bytes.Buffer
	err := Run(context.Background(), &out, Options{
		AuthPath: authPath,
		Endpoint: server.URL,
		Now:      func() time.Time { return now },
		Client:   server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"Available: 2 resets",
		"Jul 17",
		"20d 0h",
		"expired",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunSupportsTopLevelAuthToken(t *testing.T) {
	t.Parallel()

	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"access_token":"top-token","account_id":"acct-top"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer top-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acct-top" {
			t.Fatalf("ChatGPT-Account-ID = %q", got)
		}
		_, err := w.Write([]byte(`{"data":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	var out bytes.Buffer
	err := Run(context.Background(), &out, Options{
		AuthPath: authPath,
		Endpoint: server.URL,
		Now:      func() time.Time { return time.Date(2026, 6, 27, 19, 57, 0, 0, time.UTC) },
		Client:   server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "No reset credits found.") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}
