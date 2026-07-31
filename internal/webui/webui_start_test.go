package webui

import (
	"context"
	"errors"
	"net/http"

	"github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/labstack/echo/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The specs below reach into the package-local seams to exercise Start's
// branches without opening a socket or reading /etc/kairos/agent.yaml.
var _ = Describe("Start", func() {
	It("returns an error when LoadConfig fails", func() {
		prev := loadAgentConfig
		defer func() { loadAgentConfig = prev }()
		loadAgentConfig = func() (*agent.Config, error) { return nil, errors.New("nope") }

		Expect(Start(context.Background())).To(HaveOccurred(), "expected error from LoadConfig")
	})

	It("returns nil when the WebUI is disabled", func() {
		prev := loadAgentConfig
		defer func() { loadAgentConfig = prev }()
		loadAgentConfig = func() (*agent.Config, error) {
			return &agent.Config{WebUI: agent.WebUI{Disable: true}}, nil
		}

		// Disable=true short-circuits before the listener would open — nil error,
		// no server started.
		Expect(Start(context.Background())).To(Succeed())
	})

	It("uses the custom listen address from the config", func() {
		prevCfg := loadAgentConfig
		prevListen := listenAndServe
		defer func() {
			loadAgentConfig = prevCfg
			listenAndServe = prevListen
		}()

		loadAgentConfig = func() (*agent.Config, error) {
			return &agent.Config{WebUI: agent.WebUI{ListenAddress: "127.0.0.1:9"}}, nil
		}
		var gotAddr string
		listenAndServe = func(_ context.Context, addr string, _ *echo.Echo) error {
			gotAddr = addr
			return nil
		}

		Expect(Start(context.Background())).To(Succeed())
		Expect(gotAddr).To(Equal("127.0.0.1:9"), "listen addr not propagated")
	})

	It("swallows http.ErrServerClosed", func() {
		prevCfg := loadAgentConfig
		prevListen := listenAndServe
		defer func() {
			loadAgentConfig = prevCfg
			listenAndServe = prevListen
		}()

		loadAgentConfig = func() (*agent.Config, error) { return &agent.Config{}, nil }
		listenAndServe = func(_ context.Context, _ string, _ *echo.Echo) error {
			// mimic the graceful-shutdown error surfaced by echo — Start must not
			// forward it to the caller.
			return http.ErrServerClosed
		}
		Expect(Start(context.Background())).To(Succeed(), "expected ErrServerClosed to be swallowed")
	})

	It("surfaces listener errors", func() {
		prevCfg := loadAgentConfig
		prevListen := listenAndServe
		defer func() {
			loadAgentConfig = prevCfg
			listenAndServe = prevListen
		}()

		loadAgentConfig = func() (*agent.Config, error) { return &agent.Config{}, nil }
		listenAndServe = func(_ context.Context, _ string, _ *echo.Echo) error {
			return errors.New("bind failed")
		}
		Expect(Start(context.Background())).To(HaveOccurred(), "expected error to bubble up")
	})
})
