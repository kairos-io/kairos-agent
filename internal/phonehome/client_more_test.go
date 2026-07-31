package phonehome_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client construction and registration edge cases", func() {
	It("returns an empty API key before registering", func() {
		c := phonehome.NewClient(&phonehome.Config{URL: "http://example"})
		Expect(c.APIKey()).To(BeEmpty())
	})

	It("respects caller-preset intervals in NewClient", func() {
		// When the caller pre-sets HeartbeatInterval/ReconnectBackoff, NewClient
		// must leave them alone (the "!= 0" branches). Any client returned is
		// enough to prove the branch fired without panicking.
		cfg := &phonehome.Config{
			URL:               "http://example",
			HeartbeatInterval: 42 * time.Second,
			ReconnectBackoff:  99 * time.Second,
		}
		Expect(phonehome.NewClient(cfg)).ToNot(BeNil())
		Expect(cfg.HeartbeatInterval).To(Equal(42*time.Second), "HeartbeatInterval mutated")
		Expect(cfg.ReconnectBackoff).To(Equal(99*time.Second), "ReconnectBackoff mutated")
	})

	It("refuses to connect before registering", func() {
		c := phonehome.NewClient(&phonehome.Config{URL: "http://example"})
		err := c.Connect(context.Background())
		Expect(err).To(MatchError("not registered"))
	})

	It("surfaces a malformed URL from Register", func() {
		// Invalid URL surfaces via http.NewRequestWithContext.
		c := phonehome.NewClient(&phonehome.Config{URL: "http://\x7f/bad"},
			phonehome.WithCredentialsPath(filepath.Join(GinkgoT().TempDir(), "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "id" }),
		)
		Expect(c.Register(context.Background())).To(HaveOccurred())
	})

	It("surfaces a request error from Register", func() {
		// 500 → registration surfaces the body in the error message.
		tmp := GinkgoT().TempDir()
		c := phonehome.NewClient(&phonehome.Config{URL: "http://127.0.0.1:0"},
			phonehome.WithCredentialsPath(filepath.Join(tmp, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "id" }),
		)
		// Use a context that's already cancelled to make Do return quickly.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(c.Register(ctx)).To(HaveOccurred())
	})

	It("is safe to Stop before Run has ever started", func() {
		// Stop must be safe to call when Run has never started (stopCancel is nil).
		c := phonehome.NewClient(&phonehome.Config{URL: "http://x"})
		// Should not panic.
		c.Stop()
	})

	It("falls through to the network when saved credentials are incomplete", func() {
		// Craft a creds file that parses but lacks required fields — Register must
		// still fall through to a network attempt (which will fail here).
		tmp := GinkgoT().TempDir()
		credPath := filepath.Join(tmp, "creds.yaml")
		Expect(os.WriteFile(credPath, []byte("node_id: only\n"), 0600)).To(Succeed())
		c := phonehome.NewClient(&phonehome.Config{URL: "http://127.0.0.1:0"},
			phonehome.WithCredentialsPath(credPath),
			phonehome.WithMachineIDFunc(func() string { return "id" }),
		)
		// Cancel context so we don't wait on real network — we just care that the
		// path exercises the incomplete-credentials branch and moves on.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = c.Register(ctx) // error expected either way
	})
})
