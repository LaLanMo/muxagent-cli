package acpbin

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPDownloadTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("slow"))
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 50 * time.Millisecond

	dest := filepath.Join(t.TempDir(), "download.bin")
	err := httpDownload(client, srv.URL, dest, 1024, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}

func TestHTTPDownloadRejectsOversizedArchive(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "32")
		_, _ = w.Write(make([]byte, 32))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "download.bin")
	err := httpDownload(srv.Client(), srv.URL, dest, 8, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	_, statErr := os.Stat(dest)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestHTTPDownloadRejectsStreamingOversizedArchive(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		_, _ = w.Write([]byte("12345"))
		flusher.Flush()
		_, _ = w.Write([]byte("67890"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "download.bin")
	err := httpDownload(srv.Client(), srv.URL, dest, 8, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	_, statErr := os.Stat(dest)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestExtractTarGzRejectsOversizedBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "runtime.tar.gz")
	destPath := filepath.Join(dir, "claude-agent-acp")
	require.NoError(t, writeTarGz(archivePath, "claude-agent-acp", make([]byte, 32)))

	err := extractTarGz(archivePath, destPath, 8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestExtractZipRejectsOversizedBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "runtime.zip")
	destPath := filepath.Join(dir, "claude-agent-acp")
	require.NoError(t, writeZip(archivePath, "claude-agent-acp", make([]byte, 32)))

	err := extractZip(archivePath, destPath, 8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func writeTarGz(path, name string, content []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(content)),
	}); err != nil {
		return err
	}
	_, err = tw.Write(content)
	return err
}

func writeZip(path, name string, content []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(content)
	return err
}
