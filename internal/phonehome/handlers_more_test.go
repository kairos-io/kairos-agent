package phonehome

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
)

func TestIsSafeArtifactID(t *testing.T) {
	cases := map[string]bool{
		"":                    false,
		"abc":                 true,
		"a-b_c.d":             true,
		"ABC123":              true,
		"has space":           false,
		"path/traversal":      false,
		"cmd$injection":       false,
		"a.b.c-42_43":         true,
	}
	for id, want := range cases {
		if got := isSafeArtifactID(id); got != want {
			t.Errorf("isSafeArtifactID(%q)=%v want %v", id, got, want)
		}
	}
}

func TestIsTransientDownloadError(t *testing.T) {
	if !isTransientDownloadError(io.ErrUnexpectedEOF) {
		t.Error("ErrUnexpectedEOF should be transient")
	}
	if isTransientDownloadError(errors.New("plain error")) {
		t.Error("plain error should not be transient")
	}
}

func TestHandleUpgrade_MissingSource(t *testing.T) {
	_, err := handleUpgrade(context.Background(), CommandData{Args: map[string]string{}}, "", "", nil, 1, time.Millisecond)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestHandleUpgrade_InvalidArtifactID(t *testing.T) {
	_, err := handleUpgrade(context.Background(), CommandData{Args: map[string]string{"source": "artifact:bad id"}}, "", "", nil, 1, time.Millisecond)
	if err == nil {
		t.Fatal("expected error for invalid artifact id")
	}
}

func TestHandleReboot(t *testing.T) {
	// scheduleReboot spawns a goroutine that sleeps 10s before firing sync/reboot.
	// The handler itself returns immediately with a status message — we don't
	// wait for the goroutine to complete.
	msg, err := handleReboot()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestHandleApplyCloudConfig_MissingArg(t *testing.T) {
	_, err := handleApplyCloudConfig(CommandData{Args: map[string]string{}})
	if err == nil {
		t.Fatal("expected error for missing config arg")
	}
}

func TestDownloadArtifact_NonTransient404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	cfg := &sdkConfig.Config{}
	_, err := downloadArtifact(context.Background(), ts.URL, "key", "artifact-1", cfg, 0, time.Millisecond)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestDownloadArtifact_TransientRetriedThenFails(t *testing.T) {
	// Always return 500 (transient) → each attempt fails, exhausts retries.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	cfg := &sdkConfig.Config{}
	_, err := downloadArtifact(context.Background(), ts.URL, "key", "artifact-1", cfg, 2, time.Millisecond)
	if err == nil {
		t.Fatal("expected error after retries")
	}
}

func TestArtifactDownloadRetrySettings_Nil(t *testing.T) {
	r, i := artifactDownloadRetrySettings(nil)
	if r != DefaultArtifactDownloadRetries || i != DefaultArtifactDownloadRetryInterval {
		t.Fatalf("got %d/%s", r, i)
	}
}

func TestArtifactDownloadRetrySettings_Clamp(t *testing.T) {
	// A ridiculously large interval must clamp to Max.
	c := &Config{ArtifactDownloadRetries: 5, ArtifactDownloadRetryInterval: 10 * time.Hour}
	r, i := artifactDownloadRetrySettings(c)
	if r != 5 {
		t.Errorf("retries=%d", r)
	}
	if i != MaxArtifactDownloadRetryInterval {
		t.Errorf("interval not clamped: %s", i)
	}
}

func TestArtifactDownloadRetrySettings_Defaults(t *testing.T) {
	c := &Config{}
	r, i := artifactDownloadRetrySettings(c)
	if r != DefaultArtifactDownloadRetries || i != DefaultArtifactDownloadRetryInterval {
		t.Fatalf("got %d/%s", r, i)
	}
}
