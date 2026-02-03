package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/crypto"
	"github.com/google/uuid"
)

const (
	pollInterval = 2 * time.Second
	pollTimeout  = 5 * time.Minute
)

// AuthState represents the current state of an auth request.
type AuthState string

const (
	AuthStatePending  AuthState = "pending"
	AuthStateApproved AuthState = "approved"
	AuthStateExpired  AuthState = "expired"
	AuthStateRejected AuthState = "rejected"
)

// AuthRequestOutput is returned from POST /v1/auth/request
type AuthRequestOutput struct {
	RequestID string `json:"request_id"`
	QRURL     string `json:"qr_url"`
	ExpiresAt int64  `json:"expires_at"`
}

// AuthStatusOutput is returned from GET /v1/auth/{id}
type AuthStatusOutput struct {
	State                              AuthState     `json:"state"`
	RequestID                          string        `json:"request_id,omitempty"`
	MachineID                          string        `json:"machine_id,omitempty"`
	MachineSignPub                     string        `json:"machine_sign_pub,omitempty"`
	MachineEncPub                      string        `json:"machine_enc_pub,omitempty"`
	RelayChallenge                     string        `json:"relay_challenge,omitempty"`
	ExpiresAt                          int64         `json:"expires_at,omitempty"`
	ApprovedAt                         int64         `json:"approved_at,omitempty"`
	ApprovedByMasterSignKeyFingerprint string        `json:"approved_by_master_sign_key_fingerprint,omitempty"`
	ApprovalSignature                  string        `json:"approval_signature,omitempty"`
	MasterID                           string        `json:"master_id,omitempty"`
	Keyring                            *KeyringState `json:"keyring,omitempty"`
	RelayPubKey                        string        `json:"relay_pub_key,omitempty"`
	RelaySignature                     string        `json:"relay_signature,omitempty"`
}

// ApiEnvelope wraps all API responses from the relay server
type ApiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// parseEnvelopeResponse is a helper to unwrap envelope responses
func parseEnvelopeResponse(body io.Reader, target interface{}) error {
	var envelope ApiEnvelope
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return fmt.Errorf("failed to decode envelope: %w", err)
	}

	if envelope.Code != 0 {
		return fmt.Errorf("API error (code %d): %s", envelope.Code, envelope.Message)
	}

	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			return fmt.Errorf("failed to decode data field: %w", err)
		}
	}

	return nil
}

var (
	ErrAuthExpired  = errors.New("auth: request expired")
	ErrAuthRejected = errors.New("auth: request rejected")
	ErrAuthTimeout  = errors.New("auth: timeout waiting for approval")
)

// AuthFlow handles the authentication request/poll flow.
type AuthFlow struct {
	relayURL string
	client   *http.Client

	machineID       string
	machineSignPub  ed25519.PublicKey
	machineSignPriv ed25519.PrivateKey
	machineEncPub   *[32]byte
	machineEncPriv  *[32]byte
}

// NewAuthFlow creates a new auth flow handler.
func NewAuthFlow(relayURL string) *AuthFlow {
	return &AuthFlow{
		relayURL: relayURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// StartAuthRequest initiates an auth request with the relay server.
func (f *AuthFlow) StartAuthRequest(ctx context.Context) (*AuthRequestOutput, error) {
	machineID := uuid.New().String()
	machineSignPub, machineSignPriv, err := crypto.GenerateEd25519KeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate machine signing key: %w", err)
	}
	machineEncPub, machineEncPriv, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate machine encryption key: %w", err)
	}

	f.machineID = machineID
	f.machineSignPub = machineSignPub
	f.machineSignPriv = machineSignPriv
	f.machineEncPub = machineEncPub
	f.machineEncPriv = machineEncPriv

	hostname, _ := os.Hostname()

	payload := map[string]any{
		"machine_id":       machineID,
		"machine_sign_pub": base64.StdEncoding.EncodeToString(machineSignPub),
		"machine_enc_pub":  base64.StdEncoding.EncodeToString(machineEncPub[:]),
		"hostname":         hostname,
	}
	body, _ := json.Marshal(payload)

	url := f.relayURL + "/v1/auth/request"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to start auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("auth request failed: %s - %s", resp.Status, string(respBody))
	}

	var output AuthRequestOutput
	if err := parseEnvelopeResponse(resp.Body, &output); err != nil {
		return nil, fmt.Errorf("failed to decode auth response: %w", err)
	}

	return &output, nil
}

// PollForApproval polls the relay server until the auth request is approved or expires.
func (f *AuthFlow) PollForApproval(ctx context.Context, requestID string) (*Credentials, error) {
	timeout := time.After(pollTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, ErrAuthTimeout
		case <-ticker.C:
			status, err := f.checkAuthStatus(ctx, requestID)
			if err != nil {
				return nil, err
			}

			switch status.State {
			case AuthStatePending:
				continue
			case AuthStateExpired:
				return nil, ErrAuthExpired
			case AuthStateRejected:
				return nil, ErrAuthRejected
			case AuthStateApproved:
				return f.processApproval(requestID, status)
			}
		}
	}
}

// checkAuthStatus polls the auth status endpoint.
func (f *AuthFlow) checkAuthStatus(ctx context.Context, requestID string) (*AuthStatusOutput, error) {
	url := fmt.Sprintf("%s/v1/auth/%s", f.relayURL, requestID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check auth status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrAuthExpired
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("auth status check failed: %s - %s", resp.Status, string(respBody))
	}

	var status AuthStatusOutput
	if err := parseEnvelopeResponse(resp.Body, &status); err != nil {
		return nil, fmt.Errorf("failed to decode auth status: %w", err)
	}

	return &status, nil
}

// processApproval processes an approved auth request and creates credentials.
func (f *AuthFlow) processApproval(requestID string, status *AuthStatusOutput) (*Credentials, error) {
	if status.MachineID != f.machineID {
		return nil, fmt.Errorf("auth response machine_id mismatch")
	}

	machineSignPubB64 := base64.StdEncoding.EncodeToString(f.machineSignPub)
	machineEncPubB64 := base64.StdEncoding.EncodeToString(f.machineEncPub[:])
	if status.MachineSignPub != machineSignPubB64 || status.MachineEncPub != machineEncPubB64 {
		return nil, fmt.Errorf("auth response machine keys mismatch")
	}

	if status.Keyring == nil {
		return nil, fmt.Errorf("missing keyring in approval response")
	}
	if status.MasterID == "" {
		return nil, fmt.Errorf("missing master_id in approval response")
	}
	if status.Keyring.MasterID != "" && status.Keyring.MasterID != status.MasterID {
		return nil, fmt.Errorf("keyring master_id mismatch")
	}
	if status.ApprovedByMasterSignKeyFingerprint == "" || status.ApprovalSignature == "" {
		return nil, fmt.Errorf("missing approval signature")
	}

	masterKey := findMasterKey(status.Keyring.Keys, status.ApprovedByMasterSignKeyFingerprint)
	if masterKey == nil {
		return nil, fmt.Errorf("approved master key not found in keyring")
	}
	if masterKey.RevokedAt != nil {
		return nil, fmt.Errorf("approved master key is revoked")
	}

	approvalSig, err := base64.StdEncoding.DecodeString(status.ApprovalSignature)
	if err != nil {
		return nil, fmt.Errorf("invalid approval signature")
	}

	masterSignPubBytes, err := base64.StdEncoding.DecodeString(masterKey.MasterSignPub)
	if err != nil || len(masterSignPubBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid master sign pub")
	}

	approvalMsg := crypto.BuildApprovalMessage(
		requestID,
		status.MachineSignPub,
		status.MachineEncPub,
		status.RelayChallenge,
		status.ExpiresAt,
	)
	if !crypto.Verify(ed25519.PublicKey(masterSignPubBytes), []byte(approvalMsg), approvalSig) {
		return nil, fmt.Errorf("invalid master approval signature")
	}

	if status.RelaySignature == "" || status.RelayPubKey == "" {
		return nil, fmt.Errorf("missing relay signature")
	}
	payload := buildAuthStatusSignaturePayload(status, requestID)
	relaySig, err := base64.StdEncoding.DecodeString(status.RelaySignature)
	if err != nil {
		return nil, fmt.Errorf("invalid relay signature")
	}
	relayPub, err := base64.StdEncoding.DecodeString(status.RelayPubKey)
	if err != nil || len(relayPub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid relay public key")
	}
	if !crypto.Verify(ed25519.PublicKey(relayPub), payload, relaySig) {
		return nil, fmt.Errorf("invalid relay signature")
	}

	creds, err := NewCredentials(status.MachineID, status.MasterID, *status.Keyring, f.machineSignPub, f.machineEncPub)
	if err != nil {
		return nil, fmt.Errorf("failed to build credentials: %w", err)
	}

	return creds, nil
}

// RunAuthFlow executes the complete auth flow: request, display QR, poll, save.
func (f *AuthFlow) RunAuthFlow(ctx context.Context, onQRReady func(qrURL string) error) (*Credentials, error) {
	output, err := f.StartAuthRequest(ctx)
	if err != nil {
		return nil, err
	}

	if err := onQRReady(output.QRURL); err != nil {
		return nil, err
	}

	creds, err := f.PollForApproval(ctx, output.RequestID)
	if err != nil {
		return nil, err
	}

	signSeed := f.machineSignPriv.Seed()
	encPriv := f.machineEncPriv[:]
	if err := SaveCredentials(creds, signSeed, encPriv); err != nil {
		return nil, fmt.Errorf("failed to save credentials: %w", err)
	}

	return creds, nil
}

func findMasterKey(keys []MasterKeyInfo, masterSignKeyFingerprint string) *MasterKeyInfo {
	for _, key := range keys {
		if key.MasterSignKeyFingerprint == masterSignKeyFingerprint {
			return &key
		}
	}
	return nil
}

func buildAuthStatusSignaturePayload(status *AuthStatusOutput, requestID string) []byte {
	keys := append([]MasterKeyInfo{}, status.Keyring.Keys...)
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].MasterSignKeyFingerprint < keys[j].MasterSignKeyFingerprint
	})

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		revoked := int64(0)
		if key.RevokedAt != nil {
			revoked = *key.RevokedAt
		}
		parts = append(parts, fmt.Sprintf("%s,%s,%s,%d", key.MasterSignKeyFingerprint, key.MasterSignPub, key.MasterEncPub, revoked))
	}
	keysField := strings.Join(parts, ";")

	payload := strings.Join([]string{
		"muxagent-auth-status-v1",
		requestID,
		string(status.State),
		status.MachineID,
		status.MachineSignPub,
		status.MachineEncPub,
		status.RelayChallenge,
		fmt.Sprintf("%d", status.ExpiresAt),
		status.ApprovedByMasterSignKeyFingerprint,
		status.ApprovalSignature,
		status.MasterID,
		fmt.Sprintf("%d", keyringSeq(status.Keyring)),
		keyringHead(status.Keyring),
		keysField,
	}, "|")

	return []byte(payload)
}

func keyringSeq(state *KeyringState) int {
	if state == nil {
		return 0
	}
	return state.Seq
}

func keyringHead(state *KeyringState) string {
	if state == nil {
		return ""
	}
	return state.HeadHash
}
