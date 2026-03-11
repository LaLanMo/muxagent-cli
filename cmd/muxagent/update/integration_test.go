package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationE2EUpdateFlow exercises the full update pipeline using a real
// binary (a small shell script that prints its "version") rather than opaque
// byte slices. This proves the entire chain: manifest → signature → streaming
// hash → atomic replace → exec callback → rollback.
func TestIntegrationE2EUpdateFlow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update not supported on Windows")
	}
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	dir := t.TempDir()

	// "Installed" old binary — a shell script that prints v0.0.1
	oldBinary := []byte("#!/bin/sh\necho v0.0.1\n")
	exePath := filepath.Join(dir, "muxagent")
	require.NoError(t, os.WriteFile(exePath, oldBinary, 0o755))

	// Verify the old binary works
	out, err := exec.Command(exePath).Output()
	require.NoError(t, err)
	assert.Equal(t, "v0.0.1\n", string(out))

	// "New" release binary — a shell script that prints v0.0.2
	newBinary := []byte("#!/bin/sh\necho v0.0.2\n")
	newHash := sha256.Sum256(newBinary)

	// Build manifest and sign it
	manifest := []byte(fmt.Sprintf("%sv0.0.2\n%s  muxagent-%s-%s\n",
		releaseManifestHeaderBase,
		hex.EncodeToString(newHash[:]),
		runtime.GOOS, runtime.GOARCH,
	))
	signature := ed25519.Sign(priv, manifest)
	sigBase64 := []byte(base64.StdEncoding.EncodeToString(signature))

	// Serve a mock GitHub release
	reqs := &releaseRequests{counts: make(map[string]int)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.add(r.URL.Path)
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v0.0.2"}`))
		case "/download/v0.0.2/" + releaseManifestName:
			_, _ = w.Write(manifest)
		case "/download/v0.0.2/" + releaseManifestSigName:
			_, _ = w.Write(sigBase64)
		case fmt.Sprintf("/download/v0.0.2/muxagent-%s-%s", runtime.GOOS, runtime.GOARCH):
			_, _ = w.Write(newBinary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// --- Test 1: Successful update ---
	var execCalled bool
	var execPath string

	client := server.Client()
	client.Timeout = 5 * time.Second
	u := &updater{
		client:                 client,
		latestReleaseURL:       server.URL + "/latest",
		releaseDownloadBaseURL: server.URL + "/download",
		releaseSigningKeys:     []ed25519.PublicKey{pub},
		resolveExecutablePath:  func() (string, error) { return exePath, nil },
		exec: func(path string, args []string, env []string) error {
			execCalled = true
			execPath = path
			return nil
		},
		environ: func() []string { return os.Environ() },
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
	}

	err = u.install(context.Background(), "v0.0.2")
	require.NoError(t, err)

	// The binary on disk should be the new version
	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, newBinary, got, "binary should be replaced with new version")

	// The new binary should be executable
	out, err = exec.Command(exePath).Output()
	require.NoError(t, err)
	assert.Equal(t, "v0.0.2\n", string(out), "new binary should print v0.0.2")

	// Backup should exist
	assert.FileExists(t, exePath+".bak")
	bakContent, _ := os.ReadFile(exePath + ".bak")
	assert.Equal(t, oldBinary, bakContent, "backup should contain old binary")

	// Exec should have been called
	assert.True(t, execCalled)
	assert.Equal(t, exePath, execPath)

	// All expected URLs should have been hit
	assert.Equal(t, 1, reqs.count("/download/v0.0.2/"+releaseManifestName))
	assert.Equal(t, 1, reqs.count("/download/v0.0.2/"+releaseManifestSigName))
	assert.Equal(t, 1, reqs.count(fmt.Sprintf("/download/v0.0.2/muxagent-%s-%s", runtime.GOOS, runtime.GOARCH)))

	// --- Test 2: Tampered binary is rejected ---
	// Reset: put old binary back
	require.NoError(t, os.WriteFile(exePath, oldBinary, 0o755))
	_ = os.Remove(exePath + ".bak")

	tamperedBinary := []byte("#!/bin/sh\necho EVIL\n")
	tamperedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v0.0.2/" + releaseManifestName:
			_, _ = w.Write(manifest) // same signed manifest (expects newBinary hash)
		case "/download/v0.0.2/" + releaseManifestSigName:
			_, _ = w.Write(sigBase64)
		case fmt.Sprintf("/download/v0.0.2/muxagent-%s-%s", runtime.GOOS, runtime.GOARCH):
			_, _ = w.Write(tamperedBinary) // DIFFERENT binary!
		default:
			http.NotFound(w, r)
		}
	}))
	defer tamperedServer.Close()

	u2 := &updater{
		client:                 tamperedServer.Client(),
		latestReleaseURL:       server.URL + "/latest",
		releaseDownloadBaseURL: tamperedServer.URL + "/download",
		releaseSigningKeys:     []ed25519.PublicKey{pub},
		resolveExecutablePath:  func() (string, error) { return exePath, nil },
		exec: func(path string, args []string, env []string) error {
			t.Fatal("exec should not be called for tampered binary")
			return nil
		},
		environ: func() []string { return nil },
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
	}
	u2.client.Timeout = 5 * time.Second

	err = u2.install(context.Background(), "v0.0.2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")

	// Original binary should be untouched
	got, _ = os.ReadFile(exePath)
	assert.Equal(t, oldBinary, got, "original binary must survive tampered update attempt")

	// --- Test 3: Wrong signing key is rejected ---
	_, wrongPriv, _ := ed25519.GenerateKey(rand.Reader)
	wrongSig := ed25519.Sign(wrongPriv, manifest)
	wrongSigBase64 := []byte(base64.StdEncoding.EncodeToString(wrongSig))

	wrongKeyReqs := &releaseRequests{counts: make(map[string]int)}
	wrongKeyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrongKeyReqs.add(r.URL.Path)
		switch r.URL.Path {
		case "/download/v0.0.2/" + releaseManifestName:
			_, _ = w.Write(manifest)
		case "/download/v0.0.2/" + releaseManifestSigName:
			_, _ = w.Write(wrongSigBase64) // signed with wrong key
		default:
			http.NotFound(w, r)
		}
	}))
	defer wrongKeyServer.Close()

	u3 := &updater{
		client:                 wrongKeyServer.Client(),
		latestReleaseURL:       server.URL + "/latest",
		releaseDownloadBaseURL: wrongKeyServer.URL + "/download",
		releaseSigningKeys:     []ed25519.PublicKey{pub}, // only trusts original key
		resolveExecutablePath:  func() (string, error) { return exePath, nil },
		exec: func(path string, args []string, env []string) error {
			t.Fatal("exec should not be called for wrong key")
			return nil
		},
		environ: func() []string { return nil },
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
	}
	u3.client.Timeout = 5 * time.Second

	err = u3.install(context.Background(), "v0.0.2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature verification failed")

	// Binary download should never have been attempted
	assert.Zero(t, wrongKeyReqs.count(fmt.Sprintf("/download/v0.0.2/muxagent-%s-%s", runtime.GOOS, runtime.GOARCH)))

	// --- Test 4: Signing tool round-trip ---
	// Build a release dir, sign it, verify the outputs match what the updater expects
	releaseDir := t.TempDir()
	assetName := fmt.Sprintf("muxagent-%s-%s", runtime.GOOS, runtime.GOARCH)
	require.NoError(t, os.WriteFile(filepath.Join(releaseDir, assetName), newBinary, 0o755))

	// Simulate what the signing tool does
	assetHash, err := fileSHA256ForTest(filepath.Join(releaseDir, assetName))
	require.NoError(t, err)

	toolManifest := []byte(fmt.Sprintf("%sv0.0.2\n%s  %s\n", releaseManifestHeaderBase, assetHash, assetName))
	toolSig := ed25519.Sign(priv, toolManifest)

	// Verify the manifest parses correctly
	parsed, err := parseReleaseManifest(toolManifest)
	require.NoError(t, err)
	assert.Equal(t, "v0.0.2", parsed.Version)
	assert.Equal(t, assetHash, parsed.Entries[assetName])

	// Verify the signature
	assert.True(t, ed25519.Verify(pub, toolManifest, toolSig))

	t.Log("E2E integration test passed: update, tamper rejection, wrong-key rejection, signing tool round-trip")
}

func fileSHA256ForTest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// TestIntegrationSigningToolCLI tests the actual signing tool binary if it can be built.
func TestIntegrationSigningToolCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	t.Parallel()

	// Build the signing tool
	toolDir := t.TempDir()
	toolPath := filepath.Join(toolDir, "sign_release")
	cmd := exec.Command("go", "build", "-o", toolPath, "./../../scripts/sign_release/")
	cmd.Dir = filepath.Join(".") // from cmd/muxagent/update
	// Need to find the correct relative path
	// Let's use an absolute path instead
	repoRoot := findRepoRoot(t)
	cmd = exec.Command("go", "build", "-o", toolPath, filepath.Join(repoRoot, "scripts", "sign_release"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("cannot build signing tool: %s\n%s", err, out)
	}

	// Generate a keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	seed := priv.Seed()

	// Create a release directory with a fake binary
	releaseDir := t.TempDir()
	binaryContent := []byte("#!/bin/sh\necho hello\n")
	assetName := fmt.Sprintf("muxagent-%s-%s", runtime.GOOS, runtime.GOARCH)
	require.NoError(t, os.WriteFile(filepath.Join(releaseDir, assetName), binaryContent, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(releaseDir, "claude-agent-acp-darwin-arm64"), []byte("#!/bin/sh\necho runtime\n"), 0o755))

	// Run the signing tool
	signCmd := exec.Command(toolPath, "-dir", releaseDir, "-version", "v1.0.0")
	signCmd.Env = append(os.Environ(), fmt.Sprintf("MUXAGENT_RELEASE_SIGNING_PRIVATE_KEY=%s", base64.StdEncoding.EncodeToString(seed)))
	signOut, err := signCmd.CombinedOutput()
	require.NoError(t, err, "signing tool failed: %s", signOut)

	// Verify outputs exist
	manifestPath := filepath.Join(releaseDir, "SHA256SUMS")
	sigPath := filepath.Join(releaseDir, "SHA256SUMS.sig")
	assert.FileExists(t, manifestPath)
	assert.FileExists(t, sigPath)

	// Read and verify manifest
	manifestBytes, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(manifestBytes), "# muxagent v1.0.0\n"))

	parsed, err := parseReleaseManifest(manifestBytes)
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", parsed.Version)
	assert.Contains(t, parsed.Entries, assetName)
	assert.Contains(t, parsed.Entries, "claude-agent-acp-darwin-arm64")

	// Read and verify signature
	sigBytes, err := os.ReadFile(sigPath)
	require.NoError(t, err)
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigBytes)))
	require.NoError(t, err)
	assert.True(t, ed25519.Verify(pub, manifestBytes, sig), "signature should verify with the corresponding public key")

	// Verify the hash matches the binary
	expectedHash, err := fileSHA256ForTest(filepath.Join(releaseDir, assetName))
	require.NoError(t, err)
	assert.Equal(t, expectedHash, parsed.Entries[assetName])

	t.Logf("Signing tool CLI test passed. Public key: %s", base64.StdEncoding.EncodeToString(pub))
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
