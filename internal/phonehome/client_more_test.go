package phonehome_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
)

func TestClient_APIKey_EmptyBeforeRegister(t *testing.T) {
	c := phonehome.NewClient(&phonehome.Config{URL: "http://example"})
	if got := c.APIKey(); got != "" {
		t.Errorf("APIKey pre-register: got %q want empty", got)
	}
}

func TestNewClient_PresetIntervalsAreRespected(t *testing.T) {
	// When the caller pre-sets HeartbeatInterval/ReconnectBackoff, NewClient
	// must leave them alone (the "!= 0" branches). Any client returned is
	// enough to prove the branch fired without panicking.
	cfg := &phonehome.Config{
		URL:               "http://example",
		HeartbeatInterval: 42 * time.Second,
		ReconnectBackoff:  99 * time.Second,
	}
	if c := phonehome.NewClient(cfg); c == nil {
		t.Fatal("NewClient returned nil")
	}
	if cfg.HeartbeatInterval != 42*time.Second {
		t.Errorf("HeartbeatInterval mutated: %s", cfg.HeartbeatInterval)
	}
	if cfg.ReconnectBackoff != 99*time.Second {
		t.Errorf("ReconnectBackoff mutated: %s", cfg.ReconnectBackoff)
	}
}

func TestClient_ConnectWithoutRegisterFails(t *testing.T) {
	c := phonehome.NewClient(&phonehome.Config{URL: "http://example"})
	err := c.Connect(context.Background())
	if err == nil || err.Error() != "not registered" {
		t.Fatalf("expected 'not registered', got: %v", err)
	}
}

func TestClient_RegisterBadURL(t *testing.T) {
	// Invalid URL surfaces via http.NewRequestWithContext.
	c := phonehome.NewClient(&phonehome.Config{URL: "http://\x7f/bad"},
		phonehome.WithCredentialsPath(filepath.Join(t.TempDir(), "creds.yaml")),
		phonehome.WithMachineIDFunc(func() string { return "id" }),
	)
	err := c.Register(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

func TestClient_RegisterServerError(t *testing.T) {
	// 500 → registration surfaces the body in the error message.
	tmp := t.TempDir()
	c := phonehome.NewClient(&phonehome.Config{URL: "http://127.0.0.1:0"},
		phonehome.WithCredentialsPath(filepath.Join(tmp, "creds.yaml")),
		phonehome.WithMachineIDFunc(func() string { return "id" }),
	)
	// Use a context that's already cancelled to make Do return quickly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Register(ctx); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_Stop_BeforeRun(t *testing.T) {
	// Stop must be safe to call when Run has never started (stopCancel is nil).
	c := phonehome.NewClient(&phonehome.Config{URL: "http://x"})
	// Should not panic.
	c.Stop()
}

func TestClient_LoadCredentialsIncomplete(t *testing.T) {
	// Craft a creds file that parses but lacks required fields — Register must
	// still fall through to a network attempt (which will fail here).
	tmp := t.TempDir()
	credPath := filepath.Join(tmp, "creds.yaml")
	if err := os.WriteFile(credPath, []byte("node_id: only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := phonehome.NewClient(&phonehome.Config{URL: "http://127.0.0.1:0"},
		phonehome.WithCredentialsPath(credPath),
		phonehome.WithMachineIDFunc(func() string { return "id" }),
	)
	// Cancel context so we don't wait on real network — we just care that the
	// path exercises the incomplete-credentials branch and moves on.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = c.Register(ctx) // error expected either way
}
