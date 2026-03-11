package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	signingKeyEnv  = "MUXAGENT_RELEASE_SIGNING_PRIVATE_KEY"
	manifestName   = "SHA256SUMS"
	signatureName  = "SHA256SUMS.sig"
	manifestPrefix = "# muxagent "
)

var assetNamePrefixes = []string{"muxagent-"}

func main() {
	var dir string
	var version string

	flag.StringVar(&dir, "dir", ".", "release directory containing muxagent-* bundle assets")
	flag.StringVar(&version, "version", "", "release version (for example v1.2.3)")
	flag.Parse()

	if err := run(dir, version); err != nil {
		fmt.Fprintf(os.Stderr, "sign release: %v\n", err)
		os.Exit(1)
	}
}

func run(dir, version string) error {
	normalizedVersion, err := normalizeVersion(version)
	if err != nil {
		return err
	}

	privateKey, err := loadSigningPrivateKey()
	if err != nil {
		return err
	}

	manifest, err := buildManifest(dir, normalizedVersion)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(privateKey, manifest)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(publicKey, manifest, signature) {
		return fmt.Errorf("generated signature failed self-verification")
	}

	if err := os.WriteFile(filepath.Join(dir, manifestName), manifest, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, signatureName), []byte(base64.StdEncoding.EncodeToString(signature)), 0o644); err != nil {
		return err
	}

	fmt.Printf("wrote %s and %s\n", manifestName, signatureName)
	fmt.Printf("release signing public key: %s\n", base64.StdEncoding.EncodeToString(publicKey))
	return nil
}

func normalizeVersion(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("version is required")
	}
	normalized := raw
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	if !semver.IsValid(normalized) {
		return "", fmt.Errorf("invalid semver %q", raw)
	}
	return normalized, nil
}

func loadSigningPrivateKey() (ed25519.PrivateKey, error) {
	value := os.Getenv(signingKeyEnv)
	if value == "" {
		return nil, fmt.Errorf("%s is required", signingKeyEnv)
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", signingKeyEnv, err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("invalid %s length", signingKeyEnv)
	}
}

func buildManifest(dir, version string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type asset struct {
		name string
		hash string
	}
	assets := make([]asset, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !matchesAssetPrefix(name) {
			continue
		}
		hash, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset{name: name, hash: hash})
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("no signed release assets found in %s", dir)
	}

	sort.Slice(assets, func(i, j int) bool {
		return assets[i].name < assets[j].name
	})

	var builder strings.Builder
	builder.WriteString(manifestPrefix)
	builder.WriteString(version)
	builder.WriteByte('\n')
	for _, asset := range assets {
		builder.WriteString(asset.hash)
		builder.WriteString("  ")
		builder.WriteString(asset.name)
		builder.WriteByte('\n')
	}
	return []byte(builder.String()), nil
}

func matchesAssetPrefix(name string) bool {
	for _, prefix := range assetNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
