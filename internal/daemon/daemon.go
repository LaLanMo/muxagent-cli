package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/control"
	"github.com/LaLanMo/muxagent-cli/internal/crypto"
	"github.com/LaLanMo/muxagent-cli/internal/keyring"
	"github.com/LaLanMo/muxagent-cli/internal/relayws"
	runtimemanager "github.com/LaLanMo/muxagent-cli/internal/runtime/manager"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
)

type Daemon struct {
	control  *control.Server
	addr     string
	token    string
	relay    *relayws.Client
	relayURL string
	rt       *runtimemanager.Manager
	eventBuf *relayws.EventBuffer
	stopOnce sync.Once
	stopErr  error
	done     chan struct{}
}

func New(relayURL string) *Daemon {
	return &Daemon{
		relayURL: relayURL,
		done:     make(chan struct{}),
	}
}

func (d *Daemon) Start() error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	d.token = token

	// Initialize runtime manager
	cfg, err := config.LoadEffective()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	d.rt = runtimemanager.New(cfg)
	d.eventBuf = relayws.NewEventBuffer(4096)

	mux := http.NewServeMux()

	// HTTP endpoints for daemon control only
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		control.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		control.WriteJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
		go func() {
			_ = d.Stop(context.Background())
		}()
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			control.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if payload.Message == "" {
			payload.Message = "echo from daemon"
		}
		if d.relay == nil {
			control.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "relay not connected"})
			return
		}
		if err := d.relay.SendEcho(map[string]any{
			"from":    "daemon",
			"message": payload.Message,
			"ts":      time.Now().Unix(),
		}); err != nil {
			control.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		control.WriteOK(w)
	})

	server := control.NewServer(token, control.WithAuth(token, mux))
	addr, err := server.Listen()
	if err != nil {
		return err
	}
	d.control = server
	d.addr = addr

	// Connect to relay server
	hostname, _ := os.Hostname()

	creds, machineSignPriv, _, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials (run 'muxagent auth login' first): %w", err)
	}

	machineSignPub, err := creds.GetMachineSignPublicKey()
	if err != nil {
		return fmt.Errorf("failed to decode machine sign public key: %w", err)
	}
	fingerprint := crypto.HashKeyFingerprint(machineSignPub)
	accessToken := crypto.BuildMachineAccessToken(creds.MasterID, creds.MachineID, fingerprint, machineSignPriv, 5*time.Minute)

	keyringMgr := keyring.NewManager(creds.Keyring)
	relayHTTPURL := relayws.HTTPURLFromWS(d.relayURL)
	if err := keyringMgr.Sync(context.Background(), relayHTTPURL, accessToken); err != nil {
		return fmt.Errorf("failed to sync keyring: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home dir: %w", err)
	}
	wtStore := worktree.NewStore(filepath.Join(home, ".muxagent", "worktrees.json"))
	if err := wtStore.Load(); err != nil {
		return fmt.Errorf("failed to load worktree store: %w", err)
	}

	relayClient, err := relayws.NewMachineClient(d.relayURL, hostname, creds, machineSignPriv, keyringMgr, d.rt, d.eventBuf, wtStore)
	if err != nil {
		return fmt.Errorf("failed to create relay client: %w", err)
	}

	// Start event bridge: runtime events → ring buffer → relay (to mobile)
	go d.runEventBridge(context.Background(), relayClient)

	go func() {
		backoff := time.Second
		const maxBackoff = 30 * time.Second
		for {
			if err := relayClient.Connect(context.Background()); err != nil {
				log.Printf("Relay connect failed: %v (retry in %v)", err, backoff)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			backoff = time.Second // reset on successful connect

			if err := relayClient.Run(context.Background()); err != nil {
				log.Printf("Relay connection lost: %v (reconnecting...)", err)
			}
		}
	}()

	d.relay = relayClient
	return nil
}

// runEventBridge reads events from the ACP runtime and hands them to the relay,
// which owns status tracking, buffering, and best-effort WS delivery.
func (d *Daemon) runEventBridge(ctx context.Context, relay *relayws.Client) {
	events := d.rt.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := relay.SendEvent(ev); err != nil &&
				!errors.Is(err, relayws.ErrRelayNotConnected) &&
				!errors.Is(err, relayws.ErrNoActiveSession) &&
				!errors.Is(err, relayws.ErrStaleRelaySession) {
				log.Printf("event forward error: %v", err)
			}
		}
	}
}

func (d *Daemon) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.stopOnce.Do(func() {
		if d.rt != nil {
			d.rt.Stop()
		}
		if d.relay != nil {
			d.relay.Close()
		}
		if d.control != nil {
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			d.stopErr = d.control.Shutdown(shutdownCtx)
		}
		close(d.done)
	})

	return d.stopErr
}

func (d *Daemon) Address() string {
	return d.addr
}

func (d *Daemon) Token() string {
	return d.token
}

func (d *Daemon) Done() <-chan struct{} {
	return d.done
}

func randomToken() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
