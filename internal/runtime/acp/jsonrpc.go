package acp

import "encoding/json"

// Request is a JSON-RPC 2.0 request sent from client to agent.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response received from agent.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no ID) from agent to client.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return e.Message
}

// IncomingMessage is a union type for parsing any incoming JSON-RPC message.
// Discriminate by checking: has ID+Result/Error → Response; has Method+no ID → Notification; has Method+ID → Request.
type IncomingMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// IsResponse returns true if this message is a response (has ID, has result or error, no method).
func (m *IncomingMessage) IsResponse() bool {
	return m.ID != nil && m.Method == ""
}

// IsNotification returns true if this message is a notification (has method, no ID).
func (m *IncomingMessage) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// IsRequest returns true if this message is a request from agent (has method and ID).
func (m *IncomingMessage) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}
