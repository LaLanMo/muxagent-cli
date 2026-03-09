package acpbin

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestFileSHA256(t *testing.T) {
	f, err := os.CreateTemp("", "sha256test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	content := []byte("hello world")
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := fileSHA256(f.Name())
	if err != nil {
		t.Fatalf("fileSHA256() error: %v", err)
	}

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("fileSHA256() = %q, want %q", got, want)
	}
}
