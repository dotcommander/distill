package researchcache

import (
	"path/filepath"
	"testing"
)

func TestLoadStoreRoundTrip(t *testing.T) {
	t.Parallel()
	c, err := newDir(filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("newDir: %v", err)
	}
	if _, ok := c.Load("chunk"); ok {
		t.Fatal("expected miss before store")
	}
	c.Store("chunk", "- fact")
	got, ok := c.Load("chunk")
	if !ok {
		t.Fatal("expected hit after store")
	}
	if got != "- fact" {
		t.Fatalf("Load = %q, want %q", got, "- fact")
	}
}

func TestEmptyResponseNotStored(t *testing.T) {
	t.Parallel()
	c, err := newDir(filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("newDir: %v", err)
	}
	c.Store("chunk", "")
	if _, ok := c.Load("chunk"); ok {
		t.Fatal("empty response should not be cached")
	}
}

func TestNamespaceChangesWithPromptModelProviderEndpoint(t *testing.T) {
	t.Parallel()
	base := hashKey(keyVersion + "provider" + "\x00" + "endpoint" + "\x00" + "model" + "\x00" + hashKey("prompt"))
	cases := map[string]string{
		"provider": hashKey(keyVersion + "other" + "\x00" + "endpoint" + "\x00" + "model" + "\x00" + hashKey("prompt")),
		"endpoint": hashKey(keyVersion + "provider" + "\x00" + "other" + "\x00" + "model" + "\x00" + hashKey("prompt")),
		"model":    hashKey(keyVersion + "provider" + "\x00" + "endpoint" + "\x00" + "other" + "\x00" + hashKey("prompt")),
		"prompt":   hashKey(keyVersion + "provider" + "\x00" + "endpoint" + "\x00" + "model" + "\x00" + hashKey("other")),
	}
	for name, got := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got == base {
				t.Fatalf("%s namespace did not change", name)
			}
		})
	}
}
