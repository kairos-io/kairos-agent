package phonehome

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/mount-utils"
)

// These specs live in the internal package so they can hit
// downloadArtifactAttempt directly — the higher-level downloadArtifact loop
// swallows the (transient, err) tuple and rebuilds a summary error, which
// makes individual branches hard to assert on from the public surface.

var _ = Describe("downloadArtifactAttempt", func() {
	It("treats a malformed URL as a non-transient error", func() {
		// Invalid URL surfaces from http.NewRequestWithContext (not transient).
		_, transient, err := downloadArtifactAttempt(context.Background(), "http://\x7f/", "id", &sdkConfig.Config{})
		Expect(err).To(HaveOccurred())
		Expect(transient).To(BeFalse(), "malformed URL is not transient")
	})

	It("fails when the context is already cancelled", func() {
		// Cancel the ctx before dispatching → Do returns error and ctx.Err() != nil.
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte("ok"))
		}))
		defer ts.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := downloadArtifactAttempt(ctx, ts.URL, "id", &sdkConfig.Config{})
		Expect(err).To(HaveOccurred(), "expected ctx-cancel error")
	})

	It("treats a temp-file creation failure as non-transient", func() {
		// Server returns success but createArtifactTempFile fails (nil config).
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("payload"))
		}))
		defer ts.Close()

		_, transient, err := downloadArtifactAttempt(context.Background(), ts.URL, "id", nil)
		Expect(err).To(HaveOccurred())
		Expect(transient).To(BeFalse(), "temp-file error must not be transient")
	})
})

var _ = Describe("downloadArtifact backoff cancellation", func() {
	originalPersistentDir := persistentDir

	AfterEach(func() {
		persistentDir = originalPersistentDir
	})

	It("aborts when the context is cancelled during the retry backoff", func() {
		// Server always returns 500 (transient). Cancel the context after the first
		// attempt lands so the retry sleep unblocks with ctx.Done — this hits the
		// ctx-cancel branch inside downloadArtifact.
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		dir := GinkgoT().TempDir()
		// Directory must exist for the mounter to consider it a mount point.
		persistentDir = filepath.Join(dir, "persistent")
		cfg := &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
			{Device: "/dev/x", Path: persistentDir},
		})}

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		_, err := downloadArtifact(ctx, ts.URL, "k", "art-1", cfg, 5, 100*time.Millisecond)
		Expect(err).To(HaveOccurred(), "expected error from cancelled ctx")
	})
})
