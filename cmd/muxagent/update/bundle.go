package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

type bundleFiles struct {
	CLIPath      string
	RuntimePaths map[config.RuntimeID]string
}

func extractBundleArchive(archivePath, stageDir, goos string) (bundleFiles, error) {
	files := bundleFiles{RuntimePaths: make(map[config.RuntimeID]string)}
	cliName := cliBinaryName(goos)
	runtimeNames := bundledRuntimeBinaryNames(goos)

	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"):
		if err := extractTarGzBundle(archivePath, stageDir, cliName, runtimeNames, &files); err != nil {
			return bundleFiles{}, err
		}
	case strings.HasSuffix(archivePath, ".zip"):
		if err := extractZipBundle(archivePath, stageDir, cliName, runtimeNames, &files); err != nil {
			return bundleFiles{}, err
		}
	default:
		return bundleFiles{}, fmt.Errorf("unsupported bundle format: %s", archivePath)
	}

	if files.CLIPath == "" {
		return bundleFiles{}, fmt.Errorf("bundle missing %s", cliName)
	}
	for runtimeID, runtimeName := range runtimeNames {
		if files.RuntimePaths[runtimeID] == "" {
			return bundleFiles{}, fmt.Errorf("bundle missing %s", runtimeName)
		}
	}
	return files, nil
}

func extractTarGzBundle(archivePath, stageDir, cliName string, runtimeNames map[config.RuntimeID]string, files *bundleFiles) error {
	src, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer src.Close()

	gzReader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	reader := tar.NewReader(gzReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(header.Name)
		switch name {
		case cliName:
			path, err := extractBundleFile(reader, stageDir, cliName, 0o755)
			if err != nil {
				return err
			}
			files.CLIPath = path
		default:
			for runtimeID, runtimeName := range runtimeNames {
				if name != runtimeName {
					continue
				}
				path, err := extractBundleFile(reader, stageDir, runtimeName, 0o755)
				if err != nil {
					return err
				}
				files.RuntimePaths[runtimeID] = path
				break
			}
		}
	}
}

func extractZipBundle(archivePath, stageDir, cliName string, runtimeNames map[config.RuntimeID]string, files *bundleFiles) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		name := filepath.Base(file.Name)
		switch name {
		case cliName:
			rc, err := file.Open()
			if err != nil {
				return err
			}
			path, extractErr := extractBundleFile(rc, stageDir, cliName, file.Mode())
			_ = rc.Close()
			if extractErr != nil {
				return extractErr
			}
			files.CLIPath = path
		default:
			for runtimeID, runtimeName := range runtimeNames {
				if name != runtimeName {
					continue
				}
				rc, err := file.Open()
				if err != nil {
					return err
				}
				path, extractErr := extractBundleFile(rc, stageDir, runtimeName, file.Mode())
				_ = rc.Close()
				if extractErr != nil {
					return extractErr
				}
				files.RuntimePaths[runtimeID] = path
				break
			}
		}
	}
	return nil
}

func extractBundleFile(src io.Reader, stageDir, name string, mode os.FileMode) (string, error) {
	path := filepath.Join(stageDir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, src); err != nil {
		file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func cliBinaryName(goos string) string {
	name := "muxagent"
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func runtimeBinaryName(id config.RuntimeID, goos string) string {
	name := ""
	switch id {
	case config.RuntimeClaudeCode:
		name = "claude-agent-acp"
	case config.RuntimeCodex:
		name = "codex-acp"
	default:
		return ""
	}
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func bundledRuntimeBinaryNames(goos string) map[config.RuntimeID]string {
	return map[config.RuntimeID]string{
		config.RuntimeClaudeCode: runtimeBinaryName(config.RuntimeClaudeCode, goos),
		config.RuntimeCodex:      runtimeBinaryName(config.RuntimeCodex, goos),
	}
}

func restoreFile(destPath, backupPath string) error {
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(backupPath, destPath)
}
