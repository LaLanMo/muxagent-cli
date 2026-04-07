package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

type RuntimeID string

const (
	RuntimeClaudeCode RuntimeID = "claude-code"
	RuntimeCodex      RuntimeID = "codex"
	RuntimeOpenCode   RuntimeID = "opencode"

	defaultRelayURL              = "wss://relay.muxagent.com/ws"
	defaultRelaySigningPublicKey = "xpUiBnvnwOKe8tsXL7LgLmeTcog7hJXA+RrVERC+QqU="
)

type Config struct {
	Runtimes              map[RuntimeID]RuntimeSettings `json:"runtimes"`
	RelayURL              string                        `json:"relay_url,omitempty"`
	RelaySigningPublicKey string                        `json:"relay_signing_public_key,omitempty"`
}

type RuntimeSettings struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Default returns the configuration that should be used when no config file
// exists or when callers want to seed a new config.
func Default() Config {
	return Config{
		RelayURL:              defaultRelayURL,
		RelaySigningPublicKey: defaultRelaySigningPublicKey,
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeClaudeCode: {
				Env: map[string]string{"CLAUDECODE": ""},
			},
			RuntimeCodex:    {},
			RuntimeOpenCode: {},
		},
	}
}

func DetectPreferredRuntime(lookPath func(string) (string, error)) (RuntimeID, bool) {
	if lookPath != nil {
		if _, err := lookPath("codex"); err == nil {
			return RuntimeCodex, true
		}
		if _, err := lookPath("claude"); err == nil {
			return RuntimeClaudeCode, true
		}
	}
	return RuntimeCodex, false
}

func PreferredRuntimeFromPATH() RuntimeID {
	runtime, _ := DetectPreferredRuntime(exec.LookPath)
	return runtime
}

// UserConfigPath returns the path to the user-level config file at ~/.muxagent/config.json.
func UserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "config.json"), nil
}

// ProjectConfigPath returns the path to the project-level config file at ./.muxagent/config.json.
func ProjectConfigPath() string {
	return filepath.Join(".muxagent", "config.json")
}

// loadFile reads and parses a config file at the given path. Returns empty
// Config and os.ErrNotExist if file doesn't exist. Other errors are returned
// as-is for callers to handle.
func loadFile(path string) (Config, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// load reads config from $HOME/.muxagent/config.json and unmarshals it into a
// Config value. It does not apply defaults or synthesize missing values, so any
// absent fields remain zero values and missing runtime entries remain missing.
// A missing file is returned as an error (typically os.ErrNotExist) so the
// caller can explicitly decide whether to fall back to defaults or fail fast.
func load() (Config, error) {
	path, err := UserConfigPath()
	if err != nil {
		return Config{}, err
	}
	return loadFile(path)
}

// LoadEffective loads config using layered priority:
// 1. Start with built-in defaults
// 2. Merge user config (~/.muxagent/config.json)
// 3. Merge project config (./.muxagent/config.json)
// 4. Apply environment variable overrides (MUXAGENT_*)
//
// Each layer overrides the previous. Scalar fields use non-empty overwrite
// semantics; an explicit runtimes map replaces the previous runtime set.
// Missing files are silently skipped; parse errors are returned.
func LoadEffective() (Config, error) {
	cfg := Default()

	// Layer 2: User config
	userPath, err := UserConfigPath()
	if err != nil {
		return Config{}, err
	}
	if userCfg, err := loadFile(userPath); err == nil {
		cfg = mergeConfig(cfg, userCfg)
	} else if !os.IsNotExist(err) {
		return Config{}, err
	}

	// Layer 3: Project config
	projectPath := ProjectConfigPath()
	if projectCfg, err := loadFile(projectPath); err == nil {
		cfg = mergeConfig(cfg, projectCfg)
	} else if !os.IsNotExist(err) {
		return Config{}, err
	}

	// Layer 4: Environment overrides
	cfg = applyEnvOverrides(cfg)

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Save writes the full config to $HOME/.muxagent/config.json as JSON and
// returns the resolved file path. It creates parent directories as needed and
// writes with owner-only permissions. Save always overwrites the entire file
// rather than merging fields, so the on-disk contents match the provided cfg.
func Save(cfg Config) (string, error) {
	path, err := UserConfigPath()
	if err != nil {
		return "", err
	}
	return SaveTo(cfg, path)
}

// SaveTo writes the full config to the specified path as JSON and returns the
// resolved file path. It creates parent directories as needed and writes with
// owner-only permissions. SaveTo always overwrites the entire file rather than
// merging fields, so the on-disk contents match the provided cfg.
func SaveTo(cfg Config, path string) (string, error) {
	if err := validateConfig(cfg); err != nil {
		return "", err
	}

	if err := privdir.Ensure(filepath.Dir(path)); err != nil {
		return "", err
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", err
	}

	return path, nil
}

// IsRuntimeCommandOverridden returns true if the user has explicitly set
// the Command for the given runtime via config files or environment variable.
// When true, acpbin.Resolve skips auto-download and uses the user's value.
func IsRuntimeCommandOverridden(id RuntimeID) bool {
	// Check environment variable first (highest priority)
	envKey := "MUXAGENT_RUNTIMES_" + strings.ToUpper(string(id)) + "_COMMAND"
	if os.Getenv(envKey) != "" {
		return true
	}

	// Check user config
	if userPath, err := UserConfigPath(); err == nil {
		if userCfg, err := loadFile(userPath); err == nil {
			if s, ok := userCfg.Runtimes[id]; ok && s.Command != "" {
				return true
			}
		}
	}

	// Check project config
	if projectCfg, err := loadFile(ProjectConfigPath()); err == nil {
		if s, ok := projectCfg.Runtimes[id]; ok && s.Command != "" {
			return true
		}
	}

	return false
}

// Exists checks if a config file exists at the given path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RuntimeSettingsFor returns the configured settings for the given runtime.
// Returns an error if the runtime is not configured.
func (c Config) RuntimeSettingsFor(id RuntimeID) (RuntimeSettings, error) {
	settings, ok := c.Runtimes[id]
	if !ok {
		return RuntimeSettings{}, fmt.Errorf("runtime %q not configured", id)
	}
	return settings, nil
}

// ConfiguredRuntimeIDs returns configured runtimes in a stable order.
func (c Config) ConfiguredRuntimeIDs() []RuntimeID {
	ids := make([]RuntimeID, 0, len(c.Runtimes))
	for id := range c.Runtimes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return string(ids[i]) < string(ids[j])
	})
	return ids
}

// mergeConfig merges overlay into base. Scalar fields use non-empty overwrite
// semantics; an explicit runtimes map replaces the previous runtime set while
// still preserving built-in defaults for each selected runtime entry.
func mergeConfig(base, overlay Config) Config {
	result := base

	if overlay.RelayURL != "" {
		result.RelayURL = overlay.RelayURL
	}
	if overlay.RelaySigningPublicKey != "" {
		result.RelaySigningPublicKey = overlay.RelaySigningPublicKey
	}

	if overlay.Runtimes != nil {
		result.Runtimes = make(map[RuntimeID]RuntimeSettings, len(overlay.Runtimes))
		for name, settings := range overlay.Runtimes {
			seed := defaultRuntimeSettings(name)
			if baseSettings, ok := base.Runtimes[name]; ok {
				seed = mergeRuntimeSettings(seed, baseSettings)
			}
			result.Runtimes[name] = mergeRuntimeSettings(seed, settings)
		}
	}

	return result
}

func defaultRuntimeSettings(id RuntimeID) RuntimeSettings {
	return Default().Runtimes[id]
}

func mergeRuntimeSettings(base, overlay RuntimeSettings) RuntimeSettings {
	result := base

	if overlay.Command != "" {
		result.Command = overlay.Command
	}
	if len(overlay.Args) > 0 {
		result.Args = overlay.Args
	}
	if overlay.CWD != "" {
		result.CWD = overlay.CWD
	}
	if len(overlay.Env) > 0 {
		if result.Env == nil {
			result.Env = make(map[string]string)
		}
		for k, v := range overlay.Env {
			result.Env[k] = v
		}
	}

	return result
}

// applyEnvOverrides reads MUXAGENT_* environment variables and applies them to
// the config. Env vars have highest priority and override all file-based config
// values.
//
// Supported env vars:
//   - MUXAGENT_RELAY_URL
//   - MUXAGENT_RELAY_SIGNING_PUBLIC_KEY
//   - MUXAGENT_RUNTIMES_<RUNTIME>_COMMAND
//   - MUXAGENT_RUNTIMES_<RUNTIME>_CWD
func applyEnvOverrides(cfg Config) Config {
	if val := os.Getenv("MUXAGENT_RELAY_URL"); val != "" {
		cfg.RelayURL = val
	}
	if val := os.Getenv("MUXAGENT_RELAY_SIGNING_PUBLIC_KEY"); val != "" {
		cfg.RelaySigningPublicKey = val
	}

	for name, settings := range cfg.Runtimes {
		prefix := "MUXAGENT_RUNTIMES_" + strings.ToUpper(string(name)) + "_"

		if val := os.Getenv(prefix + "COMMAND"); val != "" {
			settings.Command = val
		}
		if val := os.Getenv(prefix + "CWD"); val != "" {
			settings.CWD = val
		}

		cfg.Runtimes[name] = settings
	}

	return cfg
}

// ResolveRelaySigningPublicKey enforces the relay trust policy for auth flows.
// Remote relays require a configured Ed25519 relay signing public key. Loopback
// relays may omit it for local development.
func ResolveRelaySigningPublicKey(relayURL, relaySigningPublicKey string) (ed25519.PublicKey, error) {
	loopback, err := IsLoopbackRelayURL(relayURL)
	if err != nil {
		return nil, err
	}
	if relaySigningPublicKey == "" {
		if loopback {
			return nil, nil
		}
		return nil, fmt.Errorf("pairing with remote relay requires relay_signing_public_key")
	}

	return decodeRelaySigningPublicKey(relaySigningPublicKey)
}

// IsLoopbackRelayURL reports whether relayURL points to a local loopback host.
func IsLoopbackRelayURL(relayURL string) (bool, error) {
	parsed, err := url.Parse(relayURL)
	if err != nil {
		return false, fmt.Errorf("invalid relay URL %q: %w", relayURL, err)
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true, nil
	default:
		return false, nil
	}
}

func validateConfig(cfg Config) error {
	if len(cfg.Runtimes) == 0 {
		return fmt.Errorf("at least one runtime must be configured")
	}
	for id := range cfg.Runtimes {
		if !IsSupportedRuntime(id) {
			return fmt.Errorf("runtime %q is not supported", id)
		}
	}

	if cfg.RelayURL == "" {
		if cfg.RelaySigningPublicKey == "" {
			return nil
		}
		_, err := decodeRelaySigningPublicKey(cfg.RelaySigningPublicKey)
		return err
	}

	if err := validateRelayURL(cfg.RelayURL); err != nil {
		return err
	}

	if _, err := ResolveRelaySigningPublicKey(cfg.RelayURL, cfg.RelaySigningPublicKey); err != nil {
		return err
	}

	return nil
}

func validateRelayURL(relayURL string) error {
	parsed, err := url.Parse(relayURL)
	if err != nil {
		return fmt.Errorf("invalid relay_url %q: %w", relayURL, err)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("relay_url must include a host")
	}
	switch parsed.Scheme {
	case "ws", "wss":
	default:
		return fmt.Errorf("relay_url must use ws:// or wss://")
	}

	loopback, err := IsLoopbackRelayURL(relayURL)
	if err != nil {
		return err
	}
	if !loopback && parsed.Scheme != "wss" {
		return fmt.Errorf("non-loopback relay_url must use wss://")
	}

	return nil
}

func decodeRelaySigningPublicKey(relaySigningPublicKey string) (ed25519.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(relaySigningPublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid relay_signing_public_key: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid relay_signing_public_key length")
	}
	return ed25519.PublicKey(decoded), nil
}

func IsSupportedRuntime(id RuntimeID) bool {
	switch id {
	case RuntimeClaudeCode, RuntimeCodex, RuntimeOpenCode:
		return true
	default:
		return false
	}
}
