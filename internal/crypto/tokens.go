package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"
)

// BuildMachineAccessToken creates a signed machine access token for keyring
// endpoints. Format: base64(payload).base64(signature)
// Payload: muxagent-machine-access-v1|{masterID}|{machineID}|{fingerprint}|{expiresAt}
func BuildMachineAccessToken(masterID, machineID, fingerprint string, machineSignPriv ed25519.PrivateKey, ttl time.Duration) string {
	expiresAt := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("muxagent-machine-access-v1|%s|%s|%s|%d", masterID, machineID, fingerprint, expiresAt)
	sig := ed25519.Sign(machineSignPriv, []byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig)
}
