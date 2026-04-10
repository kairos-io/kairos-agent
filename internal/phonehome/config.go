package phonehome

import (
	"encoding/json"
	"time"
)

const (
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultReconnectBackoff  = 5 * time.Second
	MaxReconnectBackoff      = 60 * time.Second
	DefaultCredentialsPath   = "/usr/local/.kairos/daedalus-credentials.yaml"
)

// Config holds the phone-home configuration, typically read from cloud-config.
type Config struct {
	URL               string            `yaml:"url"`
	RegistrationToken string            `yaml:"registration_token"`
	Group             string            `yaml:"group"`
	Labels            map[string]string `yaml:"labels"`
	HeartbeatInterval time.Duration     `yaml:"heartbeat_interval"`
	ReconnectBackoff  time.Duration     `yaml:"reconnect_backoff"`
}

// Credentials stores the node's registration result.
type Credentials struct {
	NodeID string `yaml:"node_id" json:"id"`
	APIKey string `yaml:"api_key" json:"apiKey"`
}

// RegisterRequest is the body sent to POST /api/v1/nodes/register.
type RegisterRequest struct {
	MachineID         string            `json:"machineID"`
	Hostname          string            `json:"hostname"`
	RegistrationToken string            `json:"registrationToken"`
	Group             string            `json:"group,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	OSRelease         map[string]string `json:"osRelease,omitempty"`
}

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HeartbeatData is sent by the agent periodically.
type HeartbeatData struct {
	AgentVersion string            `json:"agentVersion"`
	OSRelease    map[string]string `json:"osRelease,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// CommandData is received from daedalus.
type CommandData struct {
	ID      string            `json:"id"`
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// CommandStatusData is sent by the agent to report command execution result.
type CommandStatusData struct {
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Result string `json:"result,omitempty"`
}
