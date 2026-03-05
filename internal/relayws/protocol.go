package relayws

type MessageType string

const (
	MessageTypeRegister          MessageType = "register"
	MessageTypeChallenge         MessageType = "challenge"
	MessageTypeChallengeResponse MessageType = "challengeResponse"
	MessageTypeRegistered        MessageType = "registered"
	MessageTypeSessionInit       MessageType = "session-init"
	MessageTypeSessionAck        MessageType = "session-ack"
	MessageTypeSessionEnd        MessageType = "session-end"
	MessageTypeRPC               MessageType = "rpc"
	MessageTypeResponse          MessageType = "response"
	MessageTypeEvent             MessageType = "event"
	MessageTypeError             MessageType = "error"
)

type Role string

const (
	RoleClient  Role = "client"
	RoleMachine Role = "machine"
)

type MessageEnvelope struct {
	Type MessageType `json:"type"`
}

type RegisterMessage struct {
	Type         MessageType `json:"type"`
	Role         Role        `json:"role"`
	MachineID    string      `json:"machine_id,omitempty"`
	Hostname     string      `json:"hostname,omitempty"`
	ConnectToken string      `json:"connect_token,omitempty"`
}

type ChallengeMessage struct {
	Type  MessageType `json:"type"`
	Nonce string      `json:"nonce"`
}

type ChallengeResponseMessage struct {
	Type      MessageType `json:"type"`
	Signature string      `json:"signature"`
}

type RegisteredMessage struct {
	Type      MessageType `json:"type"`
	MasterID  string      `json:"master_id,omitempty"`
	MachineID string      `json:"machine_id,omitempty"`
}

type SessionInitMessage struct {
	Type               MessageType `json:"type"`
	MachineID          string      `json:"machine_id"`
	MachineToken       string      `json:"machine_token"`
	ClientEphemeralPub string      `json:"client_ephemeral_pub"`
	Signature          string      `json:"signature"`
}

type SessionAckMessage struct {
	Type                MessageType `json:"type"`
	MachineID           string      `json:"machine_id"`
	MachineEphemeralPub string      `json:"machine_ephemeral_pub"`
	Signature           string      `json:"signature"`
}

type SessionEndMessage struct {
	Type      MessageType `json:"type"`
	MachineID string      `json:"machine_id"`
}

type EventHint struct {
	Event string `json:"event"`
}

type EncryptedMessage struct {
	Type       MessageType `json:"type"`
	MachineID  string      `json:"machine_id"`
	MsgID      string      `json:"msg_id"`
	Nonce      string      `json:"nonce"`
	Ciphertext string      `json:"ciphertext"`
	Hint       *EventHint  `json:"hint,omitempty"`
}

type ErrorMessage struct {
	Type  MessageType `json:"type"`
	Error string      `json:"error"`
}

type RPCPayload struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}
