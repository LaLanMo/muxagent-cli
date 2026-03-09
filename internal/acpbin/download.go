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
)

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
	if err := httpDownload(url, tmpArchive, progressFn); err != nil {
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
		err = extractTarGz(tmpArchive, tmpBin)
	} else {
		err = extractZip(tmpArchive, tmpBin)
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

func httpDownload(url, dest string, progressFn func(ProgressEvent)) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download agent runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download agent runtime (HTTP %d). Run \"muxagent update\" to retry", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = resp.Body
	if progressFn != nil {
		reader = &progressReader{
			r:          resp.Body,
			total:      resp.ContentLength,
			progressFn: progressFn,
		}
	}

	if _, err := io.Copy(f, reader); err != nil {
		os.Remove(dest)
		return fmt.Errorf("failed to download agent runtime: %w", err)
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

func extractTarGz(archivePath, destPath string) error {
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
			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("claude-agent-acp not found in archive")
}

func extractZip(archivePath, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == "claude-agent-acp" || filepath.Base(f.Name) == "claude-agent-acp.exe" {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("claude-agent-acp not found in archive")
}
