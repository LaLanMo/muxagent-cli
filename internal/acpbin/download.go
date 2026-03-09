package acpbin

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	httpTimeout             = 5 * time.Minute
	maxRedirects            = 10
	maxArchiveDownloadBytes = 500 << 20
	maxExtractedBinaryBytes = 500 << 20
)

var downloadHTTPClient = &http.Client{
	Timeout:       httpTimeout,
	CheckRedirect: httpsOnlyRedirectPolicy,
}

// ProgressEvent reports download/verify/extract progress.
type ProgressEvent struct {
	Phase      string // "downloading", "verifying", "extracting"
	BytesRead  int64
	TotalBytes int64 // -1 if unknown
}

// Download fetches the ACP binary for the current platform, verifies its
// checksum, extracts it, and places it at the managed path.
// Returns the final binary path.
func Download(progressFn func(ProgressEvent)) (string, error) {
	platform, err := Platform()
	if err != nil {
		return "", err
	}

	expectedHash, ok := Checksums[platform]
	if !ok {
		return "", fmt.Errorf("unsupported platform: %s", platform)
	}

	url := DownloadURL(platform)
	dest, err := ManagedPath()
	if err != nil {
		return "", fmt.Errorf("cannot write to ~/.muxagent/bin/: %w", err)
	}

	dir := filepath.Dir(dest)
	tmpArchive := filepath.Join(dir, fmt.Sprintf(".tmp-archive-%d", rand.Int64()))
	defer os.Remove(tmpArchive)

	// Download
	if err := httpDownload(downloadHTTPClient, url, tmpArchive, maxArchiveDownloadBytes, progressFn); err != nil {
		return "", err
	}

	// Verify checksum
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "verifying"})
	}
	actualHash, err := fileSHA256(tmpArchive)
	if err != nil {
		return "", fmt.Errorf("agent runtime integrity check failed: %w", err)
	}
	if actualHash != expectedHash {
		return "", fmt.Errorf("agent runtime integrity check failed. Run \"muxagent update\" to re-download")
	}

	// Extract
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "extracting"})
	}
	tmpBin := filepath.Join(dir, fmt.Sprintf(".tmp-bin-%d", rand.Int64()))
	defer os.Remove(tmpBin)

	if strings.Contains(platform, "linux") {
		err = extractTarGz(tmpArchive, tmpBin, maxExtractedBinaryBytes)
	} else {
		err = extractZip(tmpArchive, tmpBin, maxExtractedBinaryBytes)
	}
	if err != nil {
		return "", fmt.Errorf("failed to extract agent runtime: %w", err)
	}

	if err := os.Chmod(tmpBin, 0o755); err != nil {
		return "", err
	}

	// Atomic rename
	if err := os.Rename(tmpBin, dest); err != nil {
		return "", err
	}

	return dest, nil
}

func httpsOnlyRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect to non-https URL %q", req.URL.String())
	}
	return nil
}

func httpDownload(client *http.Client, url, dest string, maxBytes int64, progressFn func(ProgressEvent)) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download agent runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download agent runtime (HTTP %d). Run \"muxagent update\" to retry", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("agent runtime download exceeds %d bytes", maxBytes)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}

	var reader io.Reader = resp.Body
	if progressFn != nil {
		reader = &progressReader{
			r:          resp.Body,
			total:      resp.ContentLength,
			progressFn: progressFn,
		}
	}
	reader = io.LimitReader(reader, maxBytes+1)

	written, err := io.Copy(f, reader)
	if err != nil {
		_ = f.Close()
		os.Remove(dest)
		return fmt.Errorf("failed to download agent runtime: %w", err)
	}
	if written > maxBytes {
		_ = f.Close()
		os.Remove(dest)
		return fmt.Errorf("agent runtime download exceeds %d bytes", maxBytes)
	}
	if err := f.Close(); err != nil {
		os.Remove(dest)
		return err
	}
	return nil
}

type progressReader struct {
	r          io.Reader
	read       int64
	total      int64
	progressFn func(ProgressEvent)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	total := pr.total
	if total <= 0 {
		total = -1
	}
	pr.progressFn(ProgressEvent{
		Phase:      "downloading",
		BytesRead:  pr.read,
		TotalBytes: total,
	})
	return n, err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractTarGz(archivePath, destPath string, maxBytes int64) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == "claude-agent-acp" && hdr.Typeflag == tar.TypeReg {
			if hdr.Size > maxBytes {
				return fmt.Errorf("extracted agent runtime exceeds %d bytes", maxBytes)
			}
			return copyReaderToFile(destPath, tr, maxBytes)
		}
	}
	return fmt.Errorf("claude-agent-acp not found in archive")
}

func extractZip(archivePath, destPath string, maxBytes int64) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == "claude-agent-acp" || filepath.Base(f.Name) == "claude-agent-acp.exe" {
			if int64(f.UncompressedSize64) > maxBytes {
				return fmt.Errorf("extracted agent runtime exceeds %d bytes", maxBytes)
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			return copyReaderToFile(destPath, rc, maxBytes)
		}
	}
	return fmt.Errorf("claude-agent-acp not found in archive")
}

func copyReaderToFile(destPath string, src io.Reader, maxBytes int64) error {
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	written, copyErr := io.Copy(out, io.LimitReader(src, maxBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return copyErr
	}
	if written > maxBytes {
		os.Remove(destPath)
		return fmt.Errorf("extracted agent runtime exceeds %d bytes", maxBytes)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return closeErr
	}
	return nil
}
