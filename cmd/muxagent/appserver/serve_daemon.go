package appserver

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	internalappserver "github.com/LaLanMo/muxagent-cli/internal/appserver"
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/filelock"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newServeDaemonCmd(stateDir *string) *cobra.Command {
	return &cobra.Command{
		Use:    "serve-daemon",
		Short:  "Run the app-server daemon in the foreground",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			server, err := newServer(*stateDir)
			if err != nil {
				return err
			}
			resolvedStateDir := server.StateDir()

			lock, err := filelock.Acquire(
				internalappserver.SingletonLockPath(resolvedStateDir),
				"muxagent app-server is already running",
			)
			if err != nil {
				return err
			}
			defer func() {
				_ = lock.Release()
			}()

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return err
			}
			defer listener.Close()

			endpoint := internalappserver.DaemonEndpoint{
				DaemonState: appconfig.DaemonState{
					Address:               listener.Addr().String(),
					PID:                   os.Getpid(),
					StartTime:             time.Now().UTC().Format(time.RFC3339),
					StartedWithCLIVersion: cliversion.CLIString(),
					LogPath:               internalappserver.DaemonLogPath(resolvedStateDir),
				},
				InstanceID: server.InstanceID(),
			}
			token := uuid.NewString()
			if err := endpoint.SetToken(token); err != nil {
				return err
			}
			if err := internalappserver.SaveDaemonEndpoint(resolvedStateDir, endpoint); err != nil {
				return err
			}
			defer func() {
				_ = internalappserver.ClearDaemonEndpoint(resolvedStateDir, endpoint.InstanceID)
			}()

			signalCtx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopSignals()
			ctx, cancel := context.WithCancel(signalCtx)
			defer cancel()

			var connMu sync.Mutex
			conns := map[net.Conn]struct{}{}
			closeAllConns := func() {
				connMu.Lock()
				defer connMu.Unlock()
				for conn := range conns {
					_ = conn.Close()
				}
			}
			go func() {
				select {
				case <-signalCtx.Done():
				case <-server.ShutdownRequested():
				}
				cancel()
				_ = listener.Close()
				closeAllConns()
			}()

			var wg sync.WaitGroup
			for {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
						break
					}
					return acceptErr
				}
				connMu.Lock()
				conns[conn] = struct{}{}
				connMu.Unlock()

				wg.Add(1)
				go func(conn net.Conn) {
					defer wg.Done()
					defer func() {
						connMu.Lock()
						delete(conns, conn)
						connMu.Unlock()
						_ = conn.Close()
					}()
					_ = server.ServeConn(ctx, conn, conn, internalappserver.ConnectionOptions{
						RequireAuth: true,
						AuthToken:   token,
					})
				}(conn)
			}
			wg.Wait()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			return server.Shutdown(shutdownCtx, server.GracefulShutdownRequested())
		},
	}
}
