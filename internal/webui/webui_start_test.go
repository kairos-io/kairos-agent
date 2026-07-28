package webui

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/labstack/echo/v5"
)

// The tests below reach into the package-local seams to exercise Start's
// branches without opening a socket or reading /etc/kairos/agent.yaml.

func TestStart_LoadConfigError(t *testing.T) {
	prev := loadAgentConfig
	defer func() { loadAgentConfig = prev }()
	loadAgentConfig = func() (*agent.Config, error) { return nil, errors.New("nope") }

	if err := Start(context.Background()); err == nil {
		t.Fatal("expected error from LoadConfig")
	}
}

func TestStart_DisabledReturnsNil(t *testing.T) {
	prev := loadAgentConfig
	defer func() { loadAgentConfig = prev }()
	loadAgentConfig = func() (*agent.Config, error) {
		return &agent.Config{WebUI: agent.WebUI{Disable: true}}, nil
	}

	// Disable=true short-circuits before the listener would open — nil error,
	// no server started.
	if err := Start(context.Background()); err != nil {
		t.Fatalf("Start(disabled): %v", err)
	}
}

func TestStart_UsesCustomListenAddress(t *testing.T) {
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

	if err := Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if gotAddr != "127.0.0.1:9" {
		t.Errorf("listen addr not propagated: %q", gotAddr)
	}
}

func TestStart_SwallowsErrServerClosed(t *testing.T) {
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
	if err := Start(context.Background()); err != nil {
		t.Fatalf("expected ErrServerClosed to be swallowed, got %v", err)
	}
}

func TestStart_ListenerErrorSurfaces(t *testing.T) {
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
	if err := Start(context.Background()); err == nil {
		t.Fatal("expected error to bubble up")
	}
}
