package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/control"
	"github.com/LaLanMo/muxagent-cli/internal/keyring"
	"github.com/LaLanMo/muxagent-cli/internal/relayws"
	"github.com/LaLanMo/muxagent-cli/internal/runtime"
	"github.com/LaLanMo/muxagent-cli/internal/runtime/acp"
)

type Daemon struct {
	control  *control.Server
	addr     string
	token    string
	relay    *relayws.Client
	relayURL string
	rt       runtime.Client
	eventBuf *relayws.EventBuffer
}

func New(relayURL string) *Daemon {
	return &Daemon{relayURL: relayURL}
}

func (d *Daemon) Start() error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	d.token = token

	// Initialize runtime client
	cfg, err := config.LoadEffective()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	runtimeSettings, err := cfg.ActiveRuntimeSettings()
	if err != nil {
		return fmt.Errorf("failed to get runtime settings: %w", err)
	}

	rtClient := acp.NewClient(acp.Config{
		Command: runtimeSettings.Command,
		Args:    runtimeSettings.Args,
		CWD:     runtimeSettings.CWD,
		Env:     runtimeSettings.Env,
	})

	if err := rtClient.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start ACP runtime: %w", err)
	}
	d.rt = rtClient
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

	keyringMgr := keyring.NewManager(creds.Keyring)
	relayHTTPURL := relayws.HTTPURLFromWS(d.relayURL)
	if err := keyringMgr.Sync(context.Background(), relayHTTPURL); err != nil {
		return fmt.Errorf("failed to sync keyring: %w", err)
	}

	relayClient, err := relayws.NewMachineClient(d.relayURL, hostname, creds, machineSignPriv, keyringMgr, rtClient, d.eventBuf)
	if err != nil {
		return fmt.Errorf("failed to create relay client: %w", err)
	}

	// Start event bridge: runtime events → ring buffer → relay (to mobile)
	go d.runEventBridge(context.Background(), relayClient)

	go func() {
		if err := relayClient.Connect(context.Background()); err != nil {
			log.Printf("Relay connect failed: %v", err)
			return
		}

		if err := relayClient.Run(context.Background()); err != nil {
			log.Printf("Relay run error: %v", err)
		}
	}()

	d.relay = relayClient
	return nil
}

// runEventBridge reads events from the ACP runtime, pushes them to the ring buffer,
// and forwards them to the mobile client if a session is active.
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
			ev = d.eventBuf.Push(ev)
			if relay.HasSession() {
				if err := relay.SendEvent(ev); err != nil {
					log.Printf("event forward error: %v", err)
				}
			}
		}
	}
}

func (d *Daemon) Stop(ctx context.Context) error {
	if d.rt != nil {
		d.rt.Stop()
	}
	if d.relay != nil {
		d.relay.Close()
	}
	if d.control == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return d.control.Shutdown(ctx)
}

func (d *Daemon) Address() string {
	return d.addr
}

func (d *Daemon) Token() string {
	return d.token
}

func randomToken() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
