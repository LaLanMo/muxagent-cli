package crypto

import (
	"os"
	"runtime"
)

// CollectSystemEntropy gathers machine-specific data for local key derivation.
func CollectSystemEntropy() []byte {
	var parts [][]byte

	hostname, _ := os.Hostname()
	parts = append(parts, []byte(hostname))

	home, _ := os.UserHomeDir()
	parts = append(parts, []byte(home))

	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, data)
	}

	if runtime.GOOS == "darwin" {
		parts = append(parts, []byte("darwin"))
	}

	parts = append(parts, []byte(os.Getenv("USER")))

	var combined []byte
	for _, p := range parts {
		combined = append(combined, p...)
		combined = append(combined, 0)
	}

	return combined
}
