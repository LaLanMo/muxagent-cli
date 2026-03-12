package localkey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

type fileBackend struct {
	path string
}

func (f *fileBackend) Get() (string, error) {
	payload, err := os.ReadFile(f.path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(payload)), nil
}

func (f *fileBackend) Set(value string) error {
	if err := privdir.Ensure(filepath.Dir(f.path)); err != nil {
		return err
	}
	return os.WriteFile(f.path, []byte(value), 0o600)
}

func (f *fileBackend) Delete() error {
	err := os.Remove(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f *fileBackend) IsNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
