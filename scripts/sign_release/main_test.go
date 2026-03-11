package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWritesManifestAndSignature(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "muxagent-darwin-arm64.tar.gz"), []byte("darwin"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "muxagent-linux-amd64.tar.gz"), []byte("linux"), 0o644))

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	t.Setenv(signingKeyEnv, base64.StdEncoding.EncodeToString(privateKey))

	require.NoError(t, run(dir, "v1.2.3"))

	manifestPath := filepath.Join(dir, manifestName)
	signaturePath := filepath.Join(dir, signatureName)
	manifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	signatureBody, err := os.ReadFile(signaturePath)
	require.NoError(t, err)

	signature, err := base64.StdEncoding.DecodeString(string(signatureBody))
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "# muxagent v1.2.3")
	assert.Contains(t, string(manifest), "muxagent-darwin-arm64.tar.gz")
	assert.Contains(t, string(manifest), "muxagent-linux-amd64.tar.gz")
	assert.True(t, ed25519.Verify(privateKey.Public().(ed25519.PublicKey), manifest, signature))
}
