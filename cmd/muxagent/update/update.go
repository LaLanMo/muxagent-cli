package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpbin"
	"github.com/LaLanMo/muxagent-cli/internal/codexbin"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

const (
	cliRepo                    = "LaLanMo/muxagent-cli"
	releaseManifestName        = "SHA256SUMS"
	releaseManifestSigName     = "SHA256SUMS.sig"
	releaseLatestMaxBytes      = 1 << 20
	releaseManifestMaxBytes    = 1 << 20
	releaseSignatureMaxBytes   = 64 << 10
	releaseBundleMaxBytes      = 500 << 20
	updateHTTPTimeout          = 5 * time.Minute
	maxRedirects               = 10
	updatedBackupEnvVar        = "MUXAGENT_UPDATED_BACKUP"
	updatedClaudeRuntimeBakEnv = "MUXAGENT_UPDATED_CLAUDE_RUNTIME_BACKUP"
	updatedCodexRuntimeBakEnv  = "MUXAGENT_UPDATED_CODEX_RUNTIME_BACKUP"
	updatedLockEnvVar          = "MUXAGENT_UPDATED_LOCK_FILE"
	updatedStageDirEnvVar      = "MUXAGENT_UPDATED_STAGE_DIR"
	releaseManifestHeaderBase  = "# muxagent "
)

var releaseSigningPublicKeyStrings = []string{
	"mHLat/iu3bV0z9fCcephlbMKrtCnAXiqz+UHHSkoBbQ=",
}

type releaseManifest struct {
	Version string
	Entries map[string]string
}

type updater struct {
	client                 *http.Client
	latestReleaseURL       string
	releaseDownloadBaseURL string
	releaseSigningKeys     []ed25519.PublicKey
	resolveExecutablePath  func() (string, error)
	exec                   func(string, []string, []string) error
	environ                func() []string
	runtimePlatform        func() (string, error)
	goos                   string
	goarch                 string
}

func NewCmd() *cobra.Command {
	var checkOnly bool
	var ensureRuntimeOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update muxagent to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ensureRuntimeOnly {
				return ensureRuntime(true)
			}
			return run(checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
	cmd.Flags().BoolVar(&ensureRuntimeOnly, "ensure-runtime", false, "Only ensure the agent runtime binary is downloaded")
	cmd.Flags().MarkHidden("ensure-runtime")

	return cmd
}

func run(checkOnly bool) error {
	u, err := newDefaultUpdater()
	if err != nil {
		return err
	}
	return runWithUpdater(u, checkOnly, version.Version)
}

func runWithUpdater(u *updater, checkOnly bool, currentVersion string) error {
	if currentVersion == "dev" {
		fmt.Println("Running development build. Skipping update.")
		return nil
	}
	if u.goos == "windows" {
		return fmt.Errorf("self-update is not supported on windows")
	}

	current, err := normalizeVersion(currentVersion)
	if err != nil {
		return fmt.Errorf("invalid current version: %w", err)
	}

	latest, err := u.latestRelease(context.Background())
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	currentClean := displayVersion(current)
	latestClean := displayVersion(latest)
	cmp := semver.Compare(latest, current)

	if checkOnly {
		if cmp <= 0 {
			fmt.Printf("muxagent is up to date (v%s)\n", currentClean)
		} else {
			fmt.Printf("Update available: v%s → v%s\nRun \"muxagent update\" to install.\n", currentClean, latestClean)
		}
		return nil
	}

	if cmp <= 0 {
		fmt.Printf("muxagent is up to date (v%s)\n", currentClean)
		return ensureRuntime(false)
	}

	fmt.Printf("Updating muxagent... (v%s → v%s)\n", currentClean, latestClean)
	if err := u.install(context.Background(), latest); err != nil {
		return err
	}
	return nil
}

func ensureRuntime(forceBundleInstall bool) error {
	cfg, err := config.LoadEffective()
	if err != nil {
		return err
	}

	runtimeIDs := cfg.ConfiguredRuntimeIDs()
	if len(runtimeIDs) == 0 {
		fmt.Printf("Updated muxagent to v%s\n", strings.TrimPrefix(version.Version, "v"))
		return nil
	}

	var updaterInstance *updater
	var currentTag string
	if version.Version != "dev" {
		updaterInstance, err = newDefaultUpdater()
		if err != nil {
			return err
		}
		currentTag, _ = normalizeVersion(version.Version)
	}

	for _, runtimeID := range runtimeIDs {
		if err := ensureRuntimeFor(cfg, runtimeID, updaterInstance, currentTag, forceBundleInstall); err != nil {
			return err
		}
	}
	fmt.Printf("Updated muxagent to v%s\n", strings.TrimPrefix(version.Version, "v"))
	return nil
}

func CleanupUpdatedBackup() {
	exePath, err := currentExecutablePath()
	if err != nil {
		return
	}

	cleanupBackupFile(updatedBackupEnvVar, exePath+".bak")
	cleanupBackupFile(updatedClaudeRuntimeBakEnv, filepath.Join(filepath.Dir(exePath), runtimeBinaryName(config.RuntimeClaudeCode, runtime.GOOS)+".bak"))
	cleanupBackupFile(updatedCodexRuntimeBakEnv, filepath.Join(filepath.Dir(exePath), runtimeBinaryName(config.RuntimeCodex, runtime.GOOS)+".bak"))
	cleanupBackupFile(updatedLockEnvVar, exePath+".lock")
	cleanupStageDir(updatedStageDirEnvVar, filepath.Dir(exePath))
}

func cleanupBackupFile(envVar, expectedPath string) {
	backupPath := os.Getenv(envVar)
	if backupPath == "" {
		return
	}

	_ = os.Unsetenv(envVar)
	if backupPath != expectedPath {
		return
	}
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}

func ensureRuntimeFor(cfg config.Config, runtimeID config.RuntimeID, u *updater, currentTag string, forceBundleInstall bool) error {
	switch runtimeID {
	case config.RuntimeClaudeCode:
		if config.IsRuntimeCommandOverridden(runtimeID) {
			if _, err := acpbin.Resolve(cfg, nil); err != nil {
				return fmt.Errorf("failed to set up %s runtime: %w", runtimeID, err)
			}
			return nil
		}
		if u != nil && currentTag != "" {
			if _, err := u.ensureBundledRuntime(context.Background(), currentTag, forceBundleInstall, runtimeID); err == nil {
				return nil
			}
		}
		if forceBundleInstall {
			if runtimePath, err := acpbin.RelativePath(); err == nil {
				_ = os.Remove(runtimePath)
			}
		}
		if _, err := acpbin.ResolveManaged(cfg, func(ev acpbin.ProgressEvent) {
			if ev.Phase == "downloading" && ev.TotalBytes > 0 {
				pct := float64(ev.BytesRead) / float64(ev.TotalBytes) * 100
				fmt.Printf("\rDownloading %s runtime... %.0f%%", runtimeID, pct)
			}
		}); err != nil {
			return fmt.Errorf("failed to set up %s runtime: %w", runtimeID, err)
		}
		fmt.Println()
		return nil
	case config.RuntimeCodex:
		if config.IsRuntimeCommandOverridden(runtimeID) {
			if _, err := codexbin.Resolve(cfg, nil); err != nil {
				return fmt.Errorf("failed to set up %s runtime: %w", runtimeID, err)
			}
			return nil
		}
		if u != nil && currentTag != "" {
			if _, err := u.ensureBundledRuntime(context.Background(), currentTag, forceBundleInstall, runtimeID); err == nil {
				return nil
			}
		}
		if forceBundleInstall {
			if runtimePath, err := codexbin.RelativePath(); err == nil {
				_ = os.Remove(runtimePath)
			}
		}
		if _, err := codexbin.ResolveManaged(cfg, func(ev codexbin.ProgressEvent) {
			if ev.Phase == "downloading" && ev.TotalBytes > 0 {
				pct := float64(ev.BytesRead) / float64(ev.TotalBytes) * 100
				fmt.Printf("\rDownloading %s runtime... %.0f%%", runtimeID, pct)
			}
		}); err != nil {
			return fmt.Errorf("failed to set up %s runtime: %w", runtimeID, err)
		}
		fmt.Println()
		return nil
	default:
		return fmt.Errorf("runtime %q is not supported", runtimeID)
	}
}

func cleanupStageDir(envVar, parentDir string) {
	stageDir := os.Getenv(envVar)
	if stageDir == "" {
		return
	}

	_ = os.Unsetenv(envVar)

	cleanStageDir := filepath.Clean(stageDir)
	cleanParent := filepath.Clean(parentDir)
	if filepath.Dir(cleanStageDir) != cleanParent {
		return
	}
	if !strings.HasPrefix(filepath.Base(cleanStageDir), "muxagent-update-") {
		return
	}
	_ = os.RemoveAll(cleanStageDir)
}

func newDefaultUpdater() (*updater, error) {
	signingKeys, err := decodeSigningPublicKeys(releaseSigningPublicKeyStrings)
	if err != nil {
		return nil, fmt.Errorf("invalid embedded release signing keys: %w", err)
	}

	return &updater{
		client:                 newUpdateHTTPClient(),
		latestReleaseURL:       "",
		releaseDownloadBaseURL: fmt.Sprintf("https://github.com/%s/releases/download", cliRepo),
		releaseSigningKeys:     signingKeys,
		resolveExecutablePath:  currentExecutablePath,
		exec:                   syscall.Exec,
		environ:                os.Environ,
		runtimePlatform:        acpbin.Platform,
		goos:                   runtime.GOOS,
		goarch:                 runtime.GOARCH,
	}, nil
}

func newUpdateHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       updateHTTPTimeout,
		CheckRedirect: httpsOnlyRedirectPolicy,
	}
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

func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func decodeSigningPublicKeys(keys []string) ([]ed25519.PublicKey, error) {
	decoded := make([]ed25519.PublicKey, 0, len(keys))
	for _, key := range keys {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return nil, err
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key length")
		}
		decoded = append(decoded, ed25519.PublicKey(raw))
	}
	return decoded, nil
}

func normalizeVersion(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty version")
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

func displayVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

func (u *updater) latestRelease(ctx context.Context) (string, error) {
	if u.latestReleaseURL != "" {
		body, err := u.fetchBytes(ctx, u.latestReleaseURL, releaseLatestMaxBytes, "latest release metadata")
		if err != nil {
			return "", err
		}

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return "", fmt.Errorf("decode latest release metadata: %w", err)
		}
		return normalizeVersion(release.TagName)
	}

	manifest, err := u.fetchAndVerifyManifestFromURLs(
		ctx,
		u.releaseLatestAssetURL(releaseManifestName),
		u.releaseLatestAssetURL(releaseManifestSigName),
		"latest release manifest",
		"latest release manifest signature",
	)
	if err != nil {
		return "", err
	}
	return manifest.Version, nil
}

func (u *updater) install(ctx context.Context, latest string) error {
	exePath, err := u.resolveExecutablePath()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	lock, err := acquireUpdateLock(exePath + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()

	manifest, err := u.fetchAndVerifyManifest(ctx, latest)
	if err != nil {
		return err
	}

	assetName, err := u.bundleAssetName()
	if err != nil {
		return err
	}
	expectedHash, ok := manifest.Entries[assetName]
	if !ok {
		return fmt.Errorf("release manifest missing asset %q", assetName)
	}

	bakPath := exePath + ".bak"
	runtimeNames := bundledRuntimeBinaryNames(u.goos)
	runtimePaths := make(map[config.RuntimeID]string, len(runtimeNames))
	runtimeBakPaths := make(map[config.RuntimeID]string, len(runtimeNames))
	hadRuntime := make(map[config.RuntimeID]bool, len(runtimeNames))
	stageDir, err := os.MkdirTemp(filepath.Dir(exePath), "muxagent-update-*")
	if err != nil {
		return fmt.Errorf("create update staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	archivePath := filepath.Join(stageDir, assetName)
	_ = os.Remove(archivePath)
	defer os.Remove(archivePath)

	if err := u.downloadVerifiedAsset(ctx, u.releaseAssetURL(latest, assetName), archivePath, expectedHash, releaseBundleMaxBytes, "release bundle"); err != nil {
		return err
	}

	bundleFiles, err := extractBundleArchive(archivePath, stageDir, u.goos)
	if err != nil {
		return fmt.Errorf("extract release bundle: %w", err)
	}

	if err := copyFile(exePath, bakPath); err != nil {
		_ = os.Remove(bakPath)
		return fmt.Errorf("create rollback backup: %w", err)
	}

	if err := os.Rename(bundleFiles.CLIPath, exePath); err != nil {
		_ = os.Remove(bakPath)
		return fmt.Errorf("replace executable: %w", err)
	}

	for runtimeID, stagedPath := range bundleFiles.RuntimePaths {
		runtimePath := u.runtimeInstallPath(runtimeID, exePath)
		runtimeBakPath := runtimePath + ".bak"
		runtimePaths[runtimeID] = runtimePath
		runtimeBakPaths[runtimeID] = runtimeBakPath

		if _, err := os.Stat(runtimePath); err == nil {
			hadRuntime[runtimeID] = true
			_ = os.Remove(runtimeBakPath)
			if err := os.Rename(runtimePath, runtimeBakPath); err != nil {
				if restoreErr := restoreExecutable(exePath, bakPath); restoreErr != nil {
					return fmt.Errorf("backup %s runtime: %v (rollback failed: %w)", runtimeID, err, restoreErr)
				}
				return fmt.Errorf("backup %s runtime: %w", runtimeID, err)
			}
		}

		if err := os.Rename(stagedPath, runtimePath); err != nil {
			for restoredID, destPath := range runtimePaths {
				bak := runtimeBakPaths[restoredID]
				if hadRuntime[restoredID] {
					_ = restoreFile(destPath, bak)
				} else {
					_ = os.Remove(destPath)
				}
			}
			if restoreErr := restoreExecutable(exePath, bakPath); restoreErr != nil {
				return fmt.Errorf("install bundled %s runtime: %v (rollback failed: %w)", runtimeID, err, restoreErr)
			}
			return fmt.Errorf("install bundled %s runtime: %w", runtimeID, err)
		}
	}

	env := setEnv(u.environ(), updatedBackupEnvVar, bakPath)
	for runtimeID, backupPath := range runtimeBakPaths {
		if !hadRuntime[runtimeID] {
			continue
		}
		env = setEnv(env, runtimeBackupEnvVar(runtimeID), backupPath)
	}
	env = setEnv(env, updatedLockEnvVar, exePath+".lock")
	env = setEnv(env, updatedStageDirEnvVar, stageDir)
	if err := u.exec(exePath, []string{exePath, "update", "--ensure-runtime"}, env); err != nil {
		for runtimeID, runtimePath := range runtimePaths {
			runtimeBakPath := runtimeBakPaths[runtimeID]
			if hadRuntime[runtimeID] {
				if restoreErr := restoreFile(runtimePath, runtimeBakPath); restoreErr != nil {
					return fmt.Errorf("re-exec failed: %v (%s runtime rollback failed: %w)", err, runtimeID, restoreErr)
				}
			} else {
				_ = os.Remove(runtimePath)
			}
		}
		if restoreErr := restoreExecutable(exePath, bakPath); restoreErr != nil {
			return fmt.Errorf("re-exec failed: %v (rollback failed: %w)", err, restoreErr)
		}
		return fmt.Errorf("re-exec updated binary: %w", err)
	}
	return nil
}

func (u *updater) fetchAndVerifyManifest(ctx context.Context, latest string) (*releaseManifest, error) {
	manifest, err := u.fetchAndVerifyManifestFromURLs(
		ctx,
		u.releaseAssetURL(latest, releaseManifestName),
		u.releaseAssetURL(latest, releaseManifestSigName),
		"release manifest",
		"release manifest signature",
	)
	if err != nil {
		return nil, err
	}
	if manifest.Version != latest {
		return nil, fmt.Errorf("release manifest version %q does not match latest release %q", manifest.Version, latest)
	}
	return manifest, nil
}

func (u *updater) fetchAndVerifyManifestFromURLs(ctx context.Context, manifestURL, signatureURL, manifestLabel, signatureLabel string) (*releaseManifest, error) {
	manifestBytes, err := u.fetchBytes(ctx, manifestURL, releaseManifestMaxBytes, manifestLabel)
	if err != nil {
		return nil, err
	}
	signatureBytes, err := u.fetchBytes(ctx, signatureURL, releaseSignatureMaxBytes, signatureLabel)
	if err != nil {
		return nil, err
	}

	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureBytes)))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", signatureLabel, err)
	}
	if !verifyManifestSignature(manifestBytes, signature, u.releaseSigningKeys) {
		return nil, fmt.Errorf("%s verification failed", signatureLabel)
	}

	manifest, err := parseReleaseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func (u *updater) fetchBytes(ctx context.Context, url string, maxBytes int64, label string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected HTTP %d", label, resp.StatusCode)
	}
	body, err := readAllLimited(resp.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	return body, nil
}

func readAllLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(r, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func verifyManifestSignature(manifest, signature []byte, keys []ed25519.PublicKey) bool {
	for _, key := range keys {
		if ed25519.Verify(key, manifest, signature) {
			return true
		}
	}
	return false
}

func parseReleaseManifest(manifestBytes []byte) (*releaseManifest, error) {
	normalized := strings.ReplaceAll(string(manifestBytes), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, fmt.Errorf("release manifest missing version header")
	}

	header := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(header, releaseManifestHeaderBase) {
		return nil, fmt.Errorf("release manifest missing version header")
	}
	versionText := strings.TrimSpace(strings.TrimPrefix(header, releaseManifestHeaderBase))
	versionValue, err := normalizeVersion(versionText)
	if err != nil {
		return nil, fmt.Errorf("release manifest has invalid version header: %w", err)
	}

	entries := make(map[string]string)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid release manifest line %q", line)
		}
		hashValue := strings.ToLower(fields[0])
		if len(hashValue) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid release manifest hash %q", fields[0])
		}
		if _, err := hex.DecodeString(hashValue); err != nil {
			return nil, fmt.Errorf("invalid release manifest hash %q", fields[0])
		}
		if _, exists := entries[fields[1]]; exists {
			return nil, fmt.Errorf("duplicate release manifest entry for %q", fields[1])
		}
		entries[fields[1]] = hashValue
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("release manifest has no asset entries")
	}

	return &releaseManifest{
		Version: versionValue,
		Entries: entries,
	}, nil
}

func (u *updater) bundleAssetName() (string, error) {
	platformFn := u.runtimePlatform
	if platformFn == nil {
		platformFn = acpbin.Platform
	}

	platform, err := platformFn()
	if err != nil {
		return "", err
	}

	suffix := ""
	if strings.HasSuffix(platform, "-musl") {
		suffix = "-musl"
	}

	assetName := fmt.Sprintf("muxagent-%s-%s%s", u.goos, u.goarch, suffix)
	if u.goos == "windows" {
		assetName += ".zip"
		return assetName, nil
	}
	assetName += ".tar.gz"
	return assetName, nil
}

func runtimeBackupEnvVar(runtimeID config.RuntimeID) string {
	switch runtimeID {
	case config.RuntimeClaudeCode:
		return updatedClaudeRuntimeBakEnv
	case config.RuntimeCodex:
		return updatedCodexRuntimeBakEnv
	default:
		return ""
	}
}

func (u *updater) runtimeInstallPath(runtimeID config.RuntimeID, exePath string) string {
	return filepath.Join(filepath.Dir(exePath), runtimeBinaryName(runtimeID, u.goos))
}

func (u *updater) releaseAssetURL(tag, assetName string) string {
	return strings.TrimRight(u.releaseDownloadBaseURL, "/") + "/" + tag + "/" + assetName
}

func (u *updater) releaseLatestAssetURL(assetName string) string {
	base := strings.TrimRight(u.releaseDownloadBaseURL, "/")
	if strings.HasSuffix(base, "/download") {
		base = strings.TrimSuffix(base, "/download")
	}
	return base + "/latest/download/" + assetName
}

func (u *updater) ensureBundledRuntime(ctx context.Context, tag string, forceInstall bool, runtimeID config.RuntimeID) (string, error) {
	exePath, err := u.resolveExecutablePath()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	destPath := u.runtimeInstallPath(runtimeID, exePath)
	if !forceInstall {
		if _, err := os.Stat(destPath); err == nil {
			return destPath, nil
		}
	}

	manifest, err := u.fetchAndVerifyManifest(ctx, tag)
	if err != nil {
		return "", err
	}

	assetName, err := u.bundleAssetName()
	if err != nil {
		return "", err
	}

	expectedHash, ok := manifest.Entries[assetName]
	if !ok {
		return "", fmt.Errorf("release manifest missing asset %q", assetName)
	}

	stageDir, err := os.MkdirTemp(filepath.Dir(destPath), "muxagent-runtime-*")
	if err != nil {
		return "", fmt.Errorf("create runtime staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	archivePath := filepath.Join(stageDir, assetName)
	_ = os.Remove(archivePath)
	defer os.Remove(archivePath)

	if err := u.downloadVerifiedAsset(ctx, u.releaseAssetURL(tag, assetName), archivePath, expectedHash, releaseBundleMaxBytes, "release bundle"); err != nil {
		return "", err
	}

	bundleFiles, err := extractBundleArchive(archivePath, stageDir, u.goos)
	if err != nil {
		return "", fmt.Errorf("extract release bundle: %w", err)
	}

	if forceInstall {
		_ = os.Remove(destPath)
	}
	runtimePath := bundleFiles.RuntimePaths[runtimeID]
	if runtimePath == "" {
		return "", fmt.Errorf("release bundle missing %s runtime", runtimeID)
	}
	if err := os.Rename(runtimePath, destPath); err != nil {
		return "", fmt.Errorf("install %s runtime: %w", runtimeID, err)
	}

	return destPath, nil
}

func (u *updater) downloadVerifiedBinary(ctx context.Context, url, destPath, expectedHash string) error {
	return u.downloadVerifiedAsset(ctx, url, destPath, expectedHash, releaseBundleMaxBytes, "update")
}

func (u *updater) downloadVerifiedAsset(ctx context.Context, url, destPath, expectedHash string, maxBytes int64, label string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s (HTTP %d)", label, resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, maxBytes)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", label, err)
	}

	hasher := sha256.New()
	reader := io.LimitReader(resp.Body, maxBytes+1)
	written, err := io.Copy(io.MultiWriter(f, hasher), reader)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("failed to download %s: %w", label, err)
	}
	if written == 0 {
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("%s is empty", label)
	}
	if written > maxBytes {
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("%s exceeds %d bytes", label, maxBytes)
	}
	expectedHashBytes, err := hex.DecodeString(expectedHash)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("invalid expected %s checksum: %w", label, err)
	}
	actualHashBytes := hasher.Sum(nil)
	if subtle.ConstantTimeCompare(actualHashBytes, expectedHashBytes) != 1 {
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("%s checksum mismatch", label)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("failed to close %s: %w", label, err)
	}
	if err := os.Chmod(destPath, 0o755); err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("chmod %s: %w", label, err)
	}
	return nil
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

func copyFile(srcPath, dstPath string) error {
	_ = os.Remove(dstPath)

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	return nil
}

func restoreExecutable(exePath, bakPath string) error {
	if err := os.Rename(bakPath, exePath); err != nil {
		return err
	}
	return nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	replaced := false
	updated := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				updated = append(updated, prefix+value)
				replaced = true
			}
			continue
		}
		updated = append(updated, entry)
	}
	if !replaced {
		updated = append(updated, prefix+value)
	}
	return updated
}
