package acpbin

import (
	"runtime"
	"testing"
)

func TestPlatform(t *testing.T) {
	plat, err := Platform()
	if err != nil {
		t.Fatalf("Platform() error: %v", err)
	}

	// Must start with the current GOOS
	if plat[:len(runtime.GOOS)] != runtime.GOOS {
		t.Errorf("Platform() = %q, expected prefix %q", plat, runtime.GOOS)
	}

	// Must contain a known arch suffix
	validArch := false
	for _, a := range []string{"x64", "arm64"} {
		if len(plat) >= len(a) && plat[len(plat)-len(a):] == a ||
			// musl case: ends with -musl but contains the arch
			contains(plat, a) {
			validArch = true
			break
		}
	}
	if !validArch {
		t.Errorf("Platform() = %q, doesn't contain x64 or arm64", plat)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPlatformHasChecksum(t *testing.T) {
	plat, err := Platform()
	if err != nil {
		t.Skipf("unsupported platform: %v", err)
	}
	if _, ok := Checksums[plat]; !ok {
		t.Errorf("no checksum entry for platform %q", plat)
	}
}

func TestDownloadURL(t *testing.T) {
	tests := []struct {
		platform string
		wantExt  string
	}{
		{"darwin-arm64", ".zip"},
		{"linux-x64", ".tar.gz"},
		{"linux-arm64-musl", ".tar.gz"},
		{"windows-x64", ".zip"},
	}
	for _, tt := range tests {
		url := DownloadURL(tt.platform)
		wantSuffix := "claude-agent-acp-" + tt.platform + tt.wantExt
		if url[len(url)-len(wantSuffix):] != wantSuffix {
			t.Errorf("DownloadURL(%q) = %q, want suffix %q", tt.platform, url, wantSuffix)
		}
		wantPrefix := "https://github.com/zed-industries/claude-agent-acp/releases/download/v" + ACPVersion + "/"
		if url[:len(wantPrefix)] != wantPrefix {
			t.Errorf("DownloadURL(%q) = %q, want prefix %q", tt.platform, url, wantPrefix)
		}
	}
}

func TestArchiveExt(t *testing.T) {
	ext := ArchiveExt()
	switch runtime.GOOS {
	case "linux":
		if ext != ".tar.gz" {
			t.Errorf("ArchiveExt() = %q on linux, want .tar.gz", ext)
		}
	default:
		if ext != ".zip" {
			t.Errorf("ArchiveExt() = %q on %s, want .zip", ext, runtime.GOOS)
		}
	}
}
