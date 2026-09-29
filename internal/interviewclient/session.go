package interviewclient

import "encoding/json"

// sessionState is serialized only by the private state store. The recovery
// token never appears in a URL, command line, or log.
type sessionState struct {
	SchemaVersion      int             `json:"schema_version"`
	Name               string          `json:"name"`
	Token              string          `json:"token"`
	Version            uint64          `json:"version"`
	PendingRequestID   string          `json:"pending_request_id,omitempty"`
	PendingKind        string          `json:"pending_kind,omitempty"`
	PendingBody        json.RawMessage `json:"pending_body,omitempty"`
	PendingBaseVersion uint64          `json:"pending_base_version,omitempty"`
}

// Session binds the locally persisted recovery identity to one chat
// conversation. It deliberately does not expose the token as an accessor.
type Session struct {
	client *Client
	key    string
	state  sessionState
}
