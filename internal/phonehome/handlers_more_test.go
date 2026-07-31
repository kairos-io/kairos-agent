package phonehome

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isSafeArtifactID", func() {
	DescribeTable("validates artifact identifiers",
		func(id string, want bool) {
			Expect(isSafeArtifactID(id)).To(Equal(want))
		},
		Entry("empty", "", false),
		Entry("plain", "abc", true),
		Entry("separators", "a-b_c.d", true),
		Entry("alphanumeric", "ABC123", true),
		Entry("space", "has space", false),
		Entry("path traversal", "path/traversal", false),
		Entry("shell metacharacter", "cmd$injection", false),
		Entry("mixed separators", "a.b.c-42_43", true),
	)
})

var _ = Describe("isTransientDownloadError", func() {
	It("classifies unexpected EOF as transient", func() {
		Expect(isTransientDownloadError(io.ErrUnexpectedEOF)).To(BeTrue(), "ErrUnexpectedEOF should be transient")
	})

	It("classifies plain errors as non-transient", func() {
		Expect(isTransientDownloadError(errors.New("plain error"))).To(BeFalse(), "plain error should not be transient")
	})
})

var _ = Describe("handleUpgrade argument validation", func() {
	It("fails when the source argument is missing", func() {
		_, err := handleUpgrade(context.Background(), CommandData{Args: map[string]string{}}, "", "", nil, 1, time.Millisecond)
		Expect(err).To(HaveOccurred(), "expected error for missing source")
	})

	It("fails when the artifact id is invalid", func() {
		_, err := handleUpgrade(context.Background(), CommandData{Args: map[string]string{"source": "artifact:bad id"}}, "", "", nil, 1, time.Millisecond)
		Expect(err).To(HaveOccurred(), "expected error for invalid artifact id")
	})
})

var _ = Describe("handleReboot", func() {
	It("returns a status message immediately", func() {
		// scheduleReboot spawns a goroutine that sleeps 10s before firing sync/reboot.
		// The handler itself returns immediately with a status message — we don't
		// wait for the goroutine to complete.
		msg, err := handleReboot()
		Expect(err).ToNot(HaveOccurred())
		Expect(msg).ToNot(BeEmpty())
	})
})

var _ = Describe("handleApplyCloudConfig argument validation", func() {
	It("fails when the config argument is missing", func() {
		_, err := handleApplyCloudConfig(CommandData{Args: map[string]string{}})
		Expect(err).To(HaveOccurred(), "expected error for missing config arg")
	})
})

var _ = Describe("downloadArtifact error propagation", func() {
	It("fails on a non-transient 404", func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()
		cfg := &sdkConfig.Config{}
		_, err := downloadArtifact(context.Background(), ts.URL, "key", "artifact-1", cfg, 0, time.Millisecond)
		Expect(err).To(HaveOccurred(), "expected error for 404")
	})

	It("fails after transient errors exhaust the retries", func() {
		// Always return 500 (transient) → each attempt fails, exhausts retries.
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()
		cfg := &sdkConfig.Config{}
		_, err := downloadArtifact(context.Background(), ts.URL, "key", "artifact-1", cfg, 2, time.Millisecond)
		Expect(err).To(HaveOccurred(), "expected error after retries")
	})
})

var _ = Describe("artifactDownloadRetrySettings", func() {
	It("falls back to defaults for a nil config", func() {
		r, i := artifactDownloadRetrySettings(nil)
		Expect(r).To(Equal(DefaultArtifactDownloadRetries))
		Expect(i).To(Equal(DefaultArtifactDownloadRetryInterval))
	})

	It("clamps an oversized interval to the maximum", func() {
		// A ridiculously large interval must clamp to Max.
		c := &Config{ArtifactDownloadRetries: 5, ArtifactDownloadRetryInterval: 10 * time.Hour}
		r, i := artifactDownloadRetrySettings(c)
		Expect(r).To(Equal(5))
		Expect(i).To(Equal(MaxArtifactDownloadRetryInterval), "interval not clamped")
	})

	It("applies defaults when the config leaves the settings zeroed", func() {
		c := &Config{}
		r, i := artifactDownloadRetrySettings(c)
		Expect(r).To(Equal(DefaultArtifactDownloadRetries))
		Expect(i).To(Equal(DefaultArtifactDownloadRetryInterval))
	})
})
