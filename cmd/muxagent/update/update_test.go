package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type releaseRequests struct {
	mu     sync.Mutex
	counts map[string]int
}

func (r *releaseRequests) add(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[path]++
}

func (r *releaseRequests) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[path]
}

func TestParseReleaseManifest(t *testing.T) {
	t.Parallel()

	valid := "# muxagent v1.2.3\r\n0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  muxagent-darwin-arm64\r\n"
	manifest, err := parseReleaseManifest([]byte(valid))
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", manifest.Version)
	assert.Equal(t, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", manifest.Entries["muxagent-darwin-arm64"])

	tests := []struct {
		name     string
		manifest string
	}{
		{name: "empty", manifest: ""},
		{name: "missing header", manifest: "0123  muxagent-darwin-arm64\n"},
		{name: "invalid hash", manifest: "# muxagent v1.2.3\nbad  muxagent-darwin-arm64\n"},
		{name: "duplicate", manifest: "# muxagent v1.2.3\naaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  muxagent-darwin-arm64\naaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  muxagent-darwin-arm64\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseReleaseManifest([]byte(tt.manifest))
			require.Error(t, err)
		})
	}
}

func TestRunWithUpdaterRejectsWindows(t *testing.T) {
	t.Parallel()

	u := &updater{goos: "windows"}
	err := runWithUpdater(u, true, "1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestLatestReleaseRejectsNonHTTPSRedirect(t *testing.T) {
	t.Parallel()

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer httpSrv.Close()

	httpsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpSrv.URL+"/latest", http.StatusFound)
	}))
	defer httpsSrv.Close()

	client := httpsSrv.Client()
	client.CheckRedirect = httpsOnlyRedirectPolicy
	client.Timeout = time.Second

	u := &updater{
		client:           client,
		latestReleaseURL: httpsSrv.URL + "/latest",
	}
	_, err := u.latestRelease(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-https")
}

func TestLatestReleaseTimesOut(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 50 * time.Millisecond

	u := &updater{
		client:           client,
		latestReleaseURL: srv.URL + "/latest",
	}
	_, err := u.latestRelease(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}

func TestInstallSuccessReplacesBinary(t *testing.T) {
	t.Parallel()

	pub, priv := generateSigningKeypair(t)
	exePath, oldContent := writeExecutable(t, "old-binary")
	newContent := []byte("#!/bin/sh\necho new\n")
	server, reqs := startReleaseServer(t, "v1.2.3", "v1.2.3", "muxagent-darwin-arm64", newContent, priv, false, nil)
	defer server.Close()

	var execPath string
	var execArgs []string
	var execEnv []string
	u := newTestUpdater(server, pub, exePath)
	u.exec = func(path string, args []string, env []string) error {
		execPath = path
		execArgs = append([]string(nil), args...)
		execEnv = append([]string(nil), env...)
		return nil
	}

	err := u.install(context.Background(), "v1.2.3")
	require.NoError(t, err)

	got, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, newContent, got)
	assert.FileExists(t, exePath+".bak")
	assert.Equal(t, exePath, execPath)
	assert.Equal(t, []string{exePath, "update", "--ensure-runtime"}, execArgs)
	assert.Contains(t, strings.Join(execEnv, "\n"), updatedBackupEnvVar+"="+exePath+".bak")
	assert.Equal(t, 1, reqs.count("/download/v1.2.3/muxagent-darwin-arm64"))
	assert.NotEqual(t, newContent, oldContent)
}

func TestInstallRollsBackWhenExecFails(t *testing.T) {
	t.Parallel()

	pub, priv := generateSigningKeypair(t)
	exePath, oldContent := writeExecutable(t, "old-binary")
	newContent := []byte("#!/bin/sh\necho new\n")
	server, _ := startReleaseServer(t, "v1.2.3", "v1.2.3", "muxagent-darwin-arm64", newContent, priv, false, nil)
	defer server.Close()

	u := newTestUpdater(server, pub, exePath)
	u.exec = func(path string, args []string, env []string) error {
		return errors.New("exec failed")
	}

	err := u.install(context.Background(), "v1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-exec updated binary")

	got, readErr := os.ReadFile(exePath)
	require.NoError(t, readErr)
	assert.Equal(t, oldContent, got)
	_, statErr := os.Stat(exePath + ".bak")
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestInstallRejectsManifestVersionMismatchWithoutDownloadingBinary(t *testing.T) {
	t.Parallel()

	pub, priv := generateSigningKeypair(t)
	exePath, _ := writeExecutable(t, "old-binary")
	server, reqs := startReleaseServer(t, "v1.2.3", "v1.2.2", "muxagent-darwin-arm64", []byte("new-binary"), priv, false, nil)
	defer server.Close()

	u := newTestUpdater(server, pub, exePath)
	err := u.install(context.Background(), "v1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match latest release")
	assert.Zero(t, reqs.count("/download/v1.2.3/muxagent-darwin-arm64"))
}

func TestInstallRejectsInvalidSignatureBeforeBinaryDownload(t *testing.T) {
	t.Parallel()

	pub, priv := generateSigningKeypair(t)
	exePath, _ := writeExecutable(t, "old-binary")
	server, reqs := startReleaseServer(t, "v1.2.3", "v1.2.3", "muxagent-darwin-arm64", []byte("new-binary"), priv, true, nil)
	defer server.Close()

	u := newTestUpdater(server, pub, exePath)
	err := u.install(context.Background(), "v1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature verification failed")
	assert.Zero(t, reqs.count("/download/v1.2.3/muxagent-darwin-arm64"))
}

func TestInstallRejectsBinaryHashMismatch(t *testing.T) {
	t.Parallel()

	pub, priv := generateSigningKeypair(t)
	exePath, oldContent := writeExecutable(t, "old-binary")
	expectedBinary := []byte("#!/bin/sh\necho expected\n")
	actualBinary := []byte("#!/bin/sh\necho tampered\n")
	hash := sha256.Sum256(expectedBinary)
	manifest := []byte(fmt.Sprintf("%s%s\n%s  %s\n", releaseManifestHeaderBase, "v1.2.3", hex.EncodeToString(hash[:]), "muxagent-darwin-arm64"))
	signature := ed25519.Sign(priv, manifest)

	reqs := &releaseRequests{counts: make(map[string]int)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.add(r.URL.Path)
		switch r.URL.Path {
		case "/download/v1.2.3/" + releaseManifestName:
			_, _ = w.Write(manifest)
		case "/download/v1.2.3/" + releaseManifestSigName:
			_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(signature)))
		case "/download/v1.2.3/muxagent-darwin-arm64":
			_, _ = w.Write(actualBinary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u := newTestUpdater(server, pub, exePath)
	err := u.install(context.Background(), "v1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Equal(t, 1, reqs.count("/download/v1.2.3/muxagent-darwin-arm64"))

	got, readErr := os.ReadFile(exePath)
	require.NoError(t, readErr)
	assert.Equal(t, oldContent, got)
}

func TestDownloadVerifiedBinaryRejectsOversizedBinary(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", releaseBinaryMaxBytes+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "muxagent.tmp")
	u := &updater{client: srv.Client()}
	err := u.downloadVerifiedBinary(context.Background(), srv.URL, destPath, strings.Repeat("0", 64))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	_, statErr := os.Stat(destPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestInstallFailsWhenLockHeld(t *testing.T) {
	t.Parallel()

	pub, _ := generateSigningKeypair(t)
	exePath, _ := writeExecutable(t, "old-binary")
	lock, err := acquireUpdateLock(exePath + ".lock")
	require.NoError(t, err)
	defer lock.Close()

	u := &updater{
		client:                 http.DefaultClient,
		latestReleaseURL:       "http://example.invalid/latest",
		releaseDownloadBaseURL: "http://example.invalid/download",
		releaseSigningKeys:     []ed25519.PublicKey{pub},
		resolveExecutablePath: func() (string, error) {
			return exePath, nil
		},
		exec:    func(path string, args []string, env []string) error { return nil },
		environ: func() []string { return nil },
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
	}

	err = u.install(context.Background(), "v1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestCleanupUpdatedBackup(t *testing.T) {
	exePath, err := currentExecutablePath()
	require.NoError(t, err)
	backupPath := exePath + ".bak"
	t.Cleanup(func() { _ = os.Remove(backupPath) })
	require.NoError(t, os.WriteFile(backupPath, []byte("backup"), 0o600))
	t.Setenv(updatedBackupEnvVar, backupPath)

	CleanupUpdatedBackup()

	_, err = os.Stat(backupPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, os.Getenv(updatedBackupEnvVar))
}

func TestCleanupUpdatedBackupIgnoresUnexpectedPath(t *testing.T) {
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "unexpected.bak")
	require.NoError(t, os.WriteFile(backupPath, []byte("backup"), 0o600))
	t.Setenv(updatedBackupEnvVar, backupPath)

	CleanupUpdatedBackup()

	_, err := os.Stat(backupPath)
	require.NoError(t, err)
	assert.Empty(t, os.Getenv(updatedBackupEnvVar))
}

func TestRunWithUpdaterCheckOnlyDoesNotDowngrade(t *testing.T) {
	t.Parallel()

	pub, priv := generateSigningKeypair(t)
	exePath, _ := writeExecutable(t, "old-binary")
	server, _ := startReleaseServer(t, "v1.1.0", "v1.1.0", "muxagent-darwin-arm64", []byte("new-binary"), priv, false, nil)
	defer server.Close()

	u := newTestUpdater(server, pub, exePath)
	err := runWithUpdater(u, true, "1.2.0")
	require.NoError(t, err)
}

func generateSigningKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pub, priv
}

func writeExecutable(t *testing.T, content string) (string, []byte) {
	t.Helper()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "muxagent")
	oldContent := []byte(content)
	require.NoError(t, os.WriteFile(exePath, oldContent, 0o755))
	return exePath, oldContent
}

func newTestUpdater(server *httptest.Server, pub ed25519.PublicKey, exePath string) *updater {
	client := server.Client()
	client.Timeout = time.Second
	client.CheckRedirect = httpsOnlyRedirectPolicy

	return &updater{
		client:                 client,
		latestReleaseURL:       server.URL + "/latest",
		releaseDownloadBaseURL: server.URL + "/download",
		releaseSigningKeys:     []ed25519.PublicKey{pub},
		resolveExecutablePath: func() (string, error) {
			return exePath, nil
		},
		exec: func(path string, args []string, env []string) error {
			return nil
		},
		environ: func() []string { return nil },
		goos:    "darwin",
		goarch:  "arm64",
	}
}

func startReleaseServer(t *testing.T, latestTag, manifestTag, assetName string, binary []byte, signer ed25519.PrivateKey, corruptSignature bool, latestDelay func(http.ResponseWriter, *http.Request)) (*httptest.Server, *releaseRequests) {
	t.Helper()

	hash := sha256.Sum256(binary)
	manifest := []byte(fmt.Sprintf("%s%s\n%s  %s\n", releaseManifestHeaderBase, manifestTag, hex.EncodeToString(hash[:]), assetName))
	signature := ed25519.Sign(signer, manifest)
	if corruptSignature {
		signature[0] ^= 0xff
	}
	signatureBody := []byte(base64.StdEncoding.EncodeToString(signature))

	reqs := &releaseRequests{counts: make(map[string]int)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.add(r.URL.Path)
		switch r.URL.Path {
		case "/latest":
			if latestDelay != nil {
				latestDelay(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"tag_name":%q}`, latestTag)))
		case "/download/" + latestTag + "/" + releaseManifestName:
			_, _ = w.Write(manifest)
		case "/download/" + latestTag + "/" + releaseManifestSigName:
			_, _ = w.Write(signatureBody)
		case "/download/" + latestTag + "/" + assetName:
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, reqs
}
