package privdir

import (
	"os"
	"path/filepath"
)

// Ensure creates path (and any missing parents) with owner-only permissions and
// tightens the final directory if it already exists.
func Ensure(path string) error {
	clean := filepath.Clean(path)
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return err
	}
	return os.Chmod(clean, 0o700)
}

// EnsureWithin tightens path and every ancestor up to and including root.
func EnsureWithin(path, root string) error {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if err := os.MkdirAll(cleanPath, 0o700); err != nil {
		return err
	}
	for dir := cleanPath; ; dir = filepath.Dir(dir) {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
		if dir == cleanRoot {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
	}
}
