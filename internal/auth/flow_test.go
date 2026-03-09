package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	cryptoutil "github.com/LaLanMo/muxagent-cli/internal/crypto"
)

func TestProcessApproval_VerifiesPinnedRelaySignature(t *testing.T) {
	status, flow, relayPriv := newApprovedStatusFixture(t)
	signRelayStatus(t, status, status.RequestID, relayPriv)

	creds, err := flow.processApproval(status.RequestID, status)
	if err != nil {
		t.Fatalf("processApproval: %v", err)
	}
	if creds.MasterID != status.MasterID {
		t.Fatalf("MasterID = %q, want %q", creds.MasterID, status.MasterID)
	}
}

func TestProcessApproval_RejectsRelaySignatureFromUnexpectedKey(t *testing.T) {
	status, flow, _ := newApprovedStatusFixture(t)
	_, attackerRelayPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signRelayStatus(t, status, status.RequestID, attackerRelayPriv)
	status.RelayPubKey = flow.relaySignPubB64

	if _, err := flow.processApproval(status.RequestID, status); err == nil || err.Error() != "invalid relay signature" {
		t.Fatalf("error = %v, want invalid relay signature", err)
	}
}

func TestProcessApproval_RejectsRelayPublicKeyMismatch(t *testing.T) {
	status, flow, relayPriv := newApprovedStatusFixture(t)
	signRelayStatus(t, status, status.RequestID, relayPriv)

	attackerRelayPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	status.RelayPubKey = base64.StdEncoding.EncodeToString(attackerRelayPub)

	if _, err := flow.processApproval(status.RequestID, status); err == nil || err.Error() != "relay public key mismatch" {
		t.Fatalf("error = %v, want relay public key mismatch", err)
	}
}

func TestProcessApproval_RejectsForgedMasterApprovalWithPinnedRelay(t *testing.T) {
	status, flow, relayPriv := newApprovedStatusFixture(t)

	attackerMasterPub, attackerMasterPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	status.Keyring.Keys[0].MasterSignKeyFingerprint = cryptoutil.HashKeyFingerprint(attackerMasterPub)
	status.Keyring.Keys[0].MasterSignPub = base64.StdEncoding.EncodeToString(attackerMasterPub)
	status.ApprovedByMasterSignKeyFingerprint = status.Keyring.Keys[0].MasterSignKeyFingerprint

	approvalMsg := cryptoutil.BuildApprovalMessage(
		status.RequestID,
		status.MachineSignPub,
		status.MachineEncPub,
		status.RelayChallenge,
		status.ExpiresAt,
	)
	// Sign a different payload so the approval remains invalid even though the relay signed the bundle.
	status.ApprovalSignature = base64.StdEncoding.EncodeToString(cryptoutil.Sign([]byte(approvalMsg+"-tampered"), attackerMasterPriv))

	signRelayStatus(t, status, status.RequestID, relayPriv)

	if _, err := flow.processApproval(status.RequestID, status); err == nil || err.Error() != "invalid master approval signature" {
		t.Fatalf("error = %v, want invalid master approval signature", err)
	}
}

func TestProcessApproval_AllowsLoopbackWithoutPinnedKey(t *testing.T) {
	status, _, relayPriv := newApprovedStatusFixture(t)
	relayPub := relayPriv.Public().(ed25519.PublicKey)

	flow := newFlowForStatus("http://localhost:8080", nil, status)
	status.RelayPubKey = base64.StdEncoding.EncodeToString(relayPub)
	signRelayStatus(t, status, status.RequestID, relayPriv)

	if _, err := flow.processApproval(status.RequestID, status); err != nil {
		t.Fatalf("processApproval: %v", err)
	}
}

func newApprovedStatusFixture(t *testing.T) (*AuthStatusOutput, *AuthFlow, ed25519.PrivateKey) {
	t.Helper()

	requestID := "request-123"
	machineID := requestID
	machineSignPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	var machineEncPub [32]byte
	if _, err := rand.Read(machineEncPub[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	masterSignPub, masterSignPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	masterEncPub := make([]byte, 32)
	if _, err := rand.Read(masterEncPub); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	relayPub, relayPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	status := &AuthStatusOutput{
		State:                              AuthStateApproved,
		RequestID:                          requestID,
		MachineID:                          machineID,
		MachineSignPub:                     base64.StdEncoding.EncodeToString(machineSignPub),
		MachineEncPub:                      base64.StdEncoding.EncodeToString(machineEncPub[:]),
		MachineHostname:                    "test-host",
		RelayChallenge:                     base64.StdEncoding.EncodeToString([]byte("relay-challenge")),
		ExpiresAt:                          1_700_000_000,
		ApprovedByMasterSignKeyFingerprint: cryptoutil.HashKeyFingerprint(masterSignPub),
		MasterID:                           "master-123",
		Keyring: &KeyringState{
			MasterID: "master-123",
			Seq:      1,
			HeadHash: "head-hash",
			Keys: []MasterKeyInfo{
				{
					MasterSignKeyFingerprint: cryptoutil.HashKeyFingerprint(masterSignPub),
					MasterSignPub:            base64.StdEncoding.EncodeToString(masterSignPub),
					MasterEncPub:             base64.StdEncoding.EncodeToString(masterEncPub),
				},
			},
		},
		RelayPubKey: base64.StdEncoding.EncodeToString(relayPub),
	}

	approvalMsg := cryptoutil.BuildApprovalMessage(
		requestID,
		status.MachineSignPub,
		status.MachineEncPub,
		status.RelayChallenge,
		status.ExpiresAt,
	)
	status.ApprovalSignature = base64.StdEncoding.EncodeToString(cryptoutil.Sign([]byte(approvalMsg), masterSignPriv))

	flow := newFlowForStatus("https://relay.example", relayPub, status)
	return status, flow, relayPriv
}

func newFlowForStatus(relayURL string, relaySignPub ed25519.PublicKey, status *AuthStatusOutput) *AuthFlow {
	flow := NewAuthFlow(relayURL, relaySignPub)
	flow.machineID = status.MachineID
	machineSignPub, _ := base64.StdEncoding.DecodeString(status.MachineSignPub)
	flow.machineSignPub = ed25519.PublicKey(machineSignPub)
	var machineEncPub [32]byte
	machineEncPubBytes, _ := base64.StdEncoding.DecodeString(status.MachineEncPub)
	copy(machineEncPub[:], machineEncPubBytes)
	flow.machineEncPub = &machineEncPub
	return flow
}

func signRelayStatus(t *testing.T, status *AuthStatusOutput, requestID string, relayPriv ed25519.PrivateKey) {
	t.Helper()
	payload := buildAuthStatusSignaturePayload(status, requestID)
	status.RelaySignature = base64.StdEncoding.EncodeToString(cryptoutil.Sign(payload, relayPriv))
}
