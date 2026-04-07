package appserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	internalappserver "github.com/LaLanMo/muxagent-cli/internal/appserver"
	"github.com/LaLanMo/muxagent-cli/internal/filelock"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/spf13/cobra"
)

type ensureResult struct {
	Address    string `json:"address"`
	Token      string `json:"token"`
	InstanceID string `json:"instance_id"`
}

const daemonProbeTimeout = 750 * time.Millisecond

func newEnsureCmd(stateDir *string) *cobra.Command {
	return &cobra.Command{
		Use:    "ensure",
		Short:  "Ensure the app-server daemon is running",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := newServer(strings.TrimSpace(*stateDir))
			if err != nil {
				return err
			}
			resolvedStateDir := server.StateDir()

			lock, err := filelock.Acquire(
				internalappserver.EnsureLockPath(resolvedStateDir),
				"muxagent app-server ensure is already running",
			)
			if err != nil {
				return err
			}
			defer func() { _ = lock.Release() }()

			if endpoint, reuse, err := resolveLiveEndpoint(resolvedStateDir, probeDaemonEndpoint, isPIDAlive); err != nil {
				return fmt.Errorf("existing app-server daemon unavailable: %w", err)
			} else if reuse {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(ensureResult{
					Address:    endpoint.Address,
					Token:      mustToken(endpoint),
					InstanceID: endpoint.InstanceID,
				})
			}

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}

			logPath, logFile, err := openAppServerLogFile(resolvedStateDir)
			if err != nil {
				return err
			}
			defer logFile.Close()

			args = []string{"app-server", "serve-daemon"}
			if strings.TrimSpace(*stateDir) != "" {
				args = append(args, "--state-dir", strings.TrimSpace(*stateDir))
			}
			child := exec.Command(exe, args...)
			child.Stdout = logFile
			child.Stderr = logFile
			child.Stdin = nil
			child.SysProcAttr = daemonSysProcAttr()

			if err := child.Start(); err != nil {
				return fmt.Errorf("start app-server daemon: %w", err)
			}
			childPID := child.Process.Pid
			_ = child.Process.Release()

			endpoint, err := waitForAppServerReady(resolvedStateDir, childPID, 10*time.Second)
			if err != nil {
				return fmt.Errorf("app-server daemon failed to start: %w (see log: %s)", err, logPath)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(ensureResult{
				Address:    endpoint.Address,
				Token:      mustToken(endpoint),
				InstanceID: endpoint.InstanceID,
			})
		},
	}
}

func openAppServerLogFile(stateDir string) (string, *os.File, error) {
	logPath := internalappserver.DaemonLogPath(stateDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create app-server state dir: %w", err)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("open app-server log file: %w", err)
	}
	return logPath, f, nil
}

func waitForAppServerReady(stateDir string, expectedPID int, timeout time.Duration) (internalappserver.DaemonEndpoint, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isPIDAlive(expectedPID) {
			return internalappserver.DaemonEndpoint{}, fmt.Errorf("app-server daemon process %d exited during startup", expectedPID)
		}
		endpoint, reuse, err := resolveLiveEndpoint(stateDir, probeDaemonEndpoint, isPIDAlive)
		if err == nil && reuse && endpoint.PID == expectedPID {
			return endpoint, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return internalappserver.DaemonEndpoint{}, errors.New("timeout waiting for app-server daemon readiness")
}

func resolveLiveEndpoint(
	stateDir string,
	probe func(internalappserver.DaemonEndpoint) error,
	pidAlive func(int) bool,
) (internalappserver.DaemonEndpoint, bool, error) {
	endpoint, err := internalappserver.LoadDaemonEndpoint(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return internalappserver.DaemonEndpoint{}, false, nil
		}
		return internalappserver.DaemonEndpoint{}, false, err
	}
	if probe == nil {
		probe = probeDaemonEndpoint
	}
	if pidAlive == nil {
		pidAlive = isPIDAlive
	}
	if err := probe(endpoint); err != nil {
		if endpoint.PID > 0 && pidAlive(endpoint.PID) {
			return internalappserver.DaemonEndpoint{}, false, err
		}
		if clearErr := internalappserver.ClearDaemonEndpoint(stateDir, endpoint.InstanceID); clearErr != nil {
			return internalappserver.DaemonEndpoint{}, false, clearErr
		}
		return internalappserver.DaemonEndpoint{}, false, nil
	}
	return endpoint, true, nil
}

func probeDaemonEndpoint(endpoint internalappserver.DaemonEndpoint) error {
	token, err := endpoint.GetToken()
	if err != nil {
		return err
	}
	return probeEndpoint(endpoint.Address, token, endpoint.InstanceID)
}

func mustToken(endpoint internalappserver.DaemonEndpoint) string {
	token, err := endpoint.GetToken()
	if err != nil {
		return ""
	}
	return token
}

func probeEndpoint(address, token, expectedInstanceID string) error {
	conn, err := net.DialTimeout("tcp", address, daemonProbeTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(daemonProbeTimeout)); err != nil {
		return err
	}

	reader := newCommandFrameReader(conn)
	if err := writeCommandFrame(conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"client_name":      "muxagent-app-server-ensure",
			"client_version":   cliversion.CLIString(),
			"protocol_version": 1,
			"auth_token":       token,
			"passive":          true,
		},
	}); err != nil {
		return err
	}
	initResp, err := readCommandJSON(reader)
	if err != nil {
		return err
	}
	if rpcErr, ok := initResp["error"].(map[string]any); ok {
		return fmt.Errorf("%v", rpcErr["message"])
	}
	result, _ := initResp["result"].(map[string]any)
	if expectedInstanceID != "" && stringField(result, "instance_id") != expectedInstanceID {
		return errors.New("app-server instance mismatch")
	}

	if err := writeCommandFrame(conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "service.status",
		"params":  map[string]any{},
	}); err != nil {
		return err
	}
	statusResp, err := readCommandJSON(reader)
	if err != nil {
		return err
	}
	if rpcErr, ok := statusResp["error"].(map[string]any); ok {
		return fmt.Errorf("%v", rpcErr["message"])
	}
	statusResult, _ := statusResp["result"].(map[string]any)
	if expectedInstanceID != "" && stringField(statusResult, "instance_id") != expectedInstanceID {
		return errors.New("app-server status instance mismatch")
	}
	return nil
}

type commandFrameReader struct {
	reader *bufio.Reader
}

func newCommandFrameReader(reader io.Reader) *commandFrameReader {
	return &commandFrameReader{reader: bufio.NewReader(reader)}
}

func (r *commandFrameReader) readFrame() ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		name, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &contentLength); err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length")
	}
	payload := make([]byte, contentLength)
	_, err := io.ReadFull(r.reader, payload)
	return payload, err
}

func readCommandJSON(reader *commandFrameReader) (map[string]any, error) {
	frame, err := reader.readFrame()
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeCommandFrame(writer io.Writer, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(writer, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(encoded))); err != nil {
		return err
	}
	_, err = writer.Write(encoded)
	return err
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return value
}

func isPIDAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
