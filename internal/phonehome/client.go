package phonehome

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

// CommandHandler is called when a command is received from daedalus.
// It should execute the command and return a result string and any error.
type CommandHandler func(cmd CommandData) (result string, err error)

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithCredentialsPath overrides the default credentials file path.
func WithCredentialsPath(path string) ClientOption {
	return func(c *Client) { c.credPath = path }
}

// WithMachineIDFunc overrides the function that returns the machine ID.
func WithMachineIDFunc(fn func() string) ClientOption {
	return func(c *Client) { c.machineIDFn = fn }
}

// WithCommandHandler sets the handler for incoming commands.
func WithCommandHandler(h CommandHandler) ClientOption {
	return func(c *Client) { c.cmdHandler = h }
}

// WithLogger sets a custom logger.
func WithLogger(l *log.Logger) ClientOption {
	return func(c *Client) { c.logger = l }
}

// Client manages the connection to a daedalus server.
type Client struct {
	cfg         *Config
	credentials *Credentials
	httpClient  *http.Client
	credPath    string
	machineIDFn func() string
	cmdHandler  CommandHandler
	logger      *log.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewClient creates a new phone-home client.
func NewClient(cfg *Config, opts ...ClientOption) *Client {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if cfg.ReconnectBackoff == 0 {
		cfg.ReconnectBackoff = DefaultReconnectBackoff
	}

	c := &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		credPath:   DefaultCredentialsPath,
		machineIDFn: func() string {
			data, err := os.ReadFile("/etc/machine-id")
			if err != nil {
				return "unknown"
			}
			return strings.TrimSpace(string(data))
		},
		logger: log.New(os.Stderr, "[phonehome] ", log.LstdFlags),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Register contacts daedalus to register this node. Stores credentials locally.
// If credentials already exist on disk, they are loaded and registration is skipped.
func (c *Client) Register(ctx context.Context) error {
	// Try loading existing credentials
	if creds, err := c.loadCredentials(); err == nil {
		c.credentials = creds
		c.logger.Printf("loaded existing credentials for node %s", creds.NodeID)
		return nil
	}

	hostname, _ := os.Hostname()
	reqBody := RegisterRequest{
		MachineID:         c.machineIDFn(),
		Hostname:          hostname,
		RegistrationToken: c.cfg.RegistrationToken,
		Group:             c.cfg.Group,
		Labels:            c.cfg.Labels,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal register request: %w", err)
	}

	reqURL := strings.TrimRight(c.cfg.URL, "/") + "/api/v1/nodes/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("registration failed: invalid registration token")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed: %d %s", resp.StatusCode, string(respBody))
	}

	var creds Credentials
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}

	c.credentials = &creds
	if err := c.saveCredentials(&creds); err != nil {
		c.logger.Printf("warning: could not save credentials: %v", err)
	}

	c.logger.Printf("registered as node %s", creds.NodeID)
	return nil
}

// Connect establishes a WebSocket connection to daedalus and handles messages.
// It blocks until the connection is closed or the context is cancelled.
func (c *Client) Connect(ctx context.Context) error {
	if c.credentials == nil {
		return fmt.Errorf("not registered")
	}

	wsURL, err := c.buildWSURL()
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		conn.Close()
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	c.logger.Printf("connected to %s", wsURL)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Close connection when context is cancelled (unblocks ReadMessage)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}()

	// Start heartbeat writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.heartbeatLoop(ctx, conn)
	}()

	// Read loop — blocks on ReadMessage, unblocked by conn.Close() above
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			cancel()
			wg.Wait()
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return fmt.Errorf("websocket read: %w", err)
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.logger.Printf("invalid message: %v", err)
			continue
		}

		switch msg.Type {
		case "command":
			var cmd CommandData
			if err := json.Unmarshal(msg.Data, &cmd); err != nil {
				c.logger.Printf("invalid command data: %v", err)
				continue
			}
			go c.handleCommand(ctx, conn, cmd)
		default:
			c.logger.Printf("unknown message type: %s", msg.Type)
		}
	}
}

// Run is the main loop: register, then connect with auto-reconnect.
func (c *Client) Run(ctx context.Context) error {
	if err := c.Register(ctx); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	backoff := c.cfg.ReconnectBackoff
	for {
		err := c.Connect(ctx)
		if ctx.Err() != nil {
			return nil // context cancelled, clean shutdown
		}
		if err != nil {
			c.logger.Printf("disconnected: %v, reconnecting in %s", err, backoff)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		// Exponential backoff
		backoff = backoff * 2
		if backoff > MaxReconnectBackoff {
			backoff = MaxReconnectBackoff
		}
	}
}

func (c *Client) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	// Send initial heartbeat immediately
	c.sendHeartbeat(conn)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.sendHeartbeat(conn); err != nil {
				c.logger.Printf("heartbeat error: %v", err)
				return
			}
		}
	}
}

func (c *Client) sendHeartbeat(conn *websocket.Conn) error {
	hb := HeartbeatData{
		AgentVersion: "dev", // TODO: inject from build
		Labels:       c.cfg.Labels,
	}
	data, _ := json.Marshal(hb)
	msg := WSMessage{Type: "heartbeat", Data: data}
	msgBytes, _ := json.Marshal(msg)

	c.mu.Lock()
	defer c.mu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, msgBytes)
}

func (c *Client) handleCommand(ctx context.Context, conn *websocket.Conn, cmd CommandData) {
	c.logger.Printf("executing command %s: %s", cmd.ID, cmd.Command)

	// Report running
	c.sendCommandStatus(conn, cmd.ID, "Running", "")

	var result string
	var err error

	if c.cmdHandler != nil {
		result, err = c.cmdHandler(cmd)
	} else {
		result = "no command handler configured"
		err = fmt.Errorf("no command handler")
	}

	if err != nil {
		c.sendCommandStatus(conn, cmd.ID, "Failed", err.Error())
	} else {
		c.sendCommandStatus(conn, cmd.ID, "Completed", result)
	}
}


func (c *Client) sendCommandStatus(conn *websocket.Conn, id, phase, result string) {
	status := CommandStatusData{ID: id, Phase: phase, Result: result}
	data, _ := json.Marshal(status)
	msg := WSMessage{Type: "command_status", Data: data}
	msgBytes, _ := json.Marshal(msg)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		c.logger.Printf("failed to send command status: %v", err)
	}
}

func (c *Client) buildWSURL() (string, error) {
	u, err := url.Parse(c.cfg.URL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}

	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/ws"
	q := u.Query()
	q.Set("token", c.credentials.APIKey)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (c *Client) loadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(c.credPath)
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.NodeID == "" || creds.APIKey == "" {
		return nil, fmt.Errorf("incomplete credentials")
	}
	return &creds, nil
}

func (c *Client) saveCredentials(creds *Credentials) error {
	dir := filepath.Dir(c.credPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(creds)
	if err != nil {
		return err
	}
	return os.WriteFile(c.credPath, data, 0600)
}
