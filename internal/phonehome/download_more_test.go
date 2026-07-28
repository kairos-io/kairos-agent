package phonehome

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"k8s.io/mount-utils"
)

// These tests live in the internal package so they can hit
// downloadArtifactAttempt directly — the higher-level downloadArtifact loop
// swallows the (transient, err) tuple and rebuilds a summary error, which
// makes individual branches hard to assert on from the public surface.

func TestDownloadArtifactAttempt_MalformedURL(t *testing.T) {
	// Invalid URL surfaces from http.NewRequestWithContext (not transient).
	_, transient, err := downloadArtifactAttempt(context.Background(), "http://\x7f/", "id", &sdkConfig.Config{})
	if err == nil {
		t.Fatal("expected error")
	}
	if transient {
		t.Error("malformed URL is not transient")
	}
}

func TestDownloadArtifactAttempt_CancelledCtx(t *testing.T) {
	// Cancel the ctx before dispatching → Do returns error and ctx.Err() != nil.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := downloadArtifactAttempt(ctx, ts.URL, "id", &sdkConfig.Config{})
	if err == nil {
		t.Fatal("expected ctx-cancel error")
	}
}

func TestDownloadArtifactAttempt_TempFileError(t *testing.T) {
	// Server returns success but createArtifactTempFile fails (nil config).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer ts.Close()

	_, transient, err := downloadArtifactAttempt(context.Background(), ts.URL, "id", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if transient {
		t.Error("temp-file error must not be transient")
	}
}

func TestDownloadArtifact_ContextCancelledDuringBackoff(t *testing.T) {
	// Server always returns 500 (transient). Cancel the context after the first
	// attempt lands so the retry sleep unblocks with ctx.Done — this hits the
	// ctx-cancel branch inside downloadArtifact.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	dir := t.TempDir()
	origPersistent := persistentDir
	persistentDir = filepath.Join(dir, "persistent")
	defer func() { persistentDir = origPersistent }()
	if err := (func() error {
		// Directory must exist for the mounter to consider it a mount point.
		return nil
	})(); err != nil {
		t.Fatal(err)
	}
	cfg := &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
		{Device: "/dev/x", Path: persistentDir},
	})}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := downloadArtifact(ctx, ts.URL, "k", "art-1", cfg, 5, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
}
