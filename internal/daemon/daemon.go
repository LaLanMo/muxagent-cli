package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/control"
	"github.com/LaLanMo/muxagent-cli/internal/keyring"
	"github.com/LaLanMo/muxagent-cli/internal/relayws"
)

type Daemon struct {
	control  *control.Server
	addr     string
	token    string
	relay    *relayws.Client
	relayURL string
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

	// Session/approval/events logic will be handled via WebSocket RPC

	server := control.NewServer(token, control.WithAuth(token, mux))
	addr, err := server.Listen()
	if err != nil {
		return err
	}
	d.control = server
	d.addr = addr

	// Connect to relay server
	hostname, _ := os.Hostname()

	// Load credentials (required for authenticated connection)
	creds, machineSignPriv, _, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials (run 'muxagent auth login' first): %w", err)
	}

	keyringMgr := keyring.NewManager(creds.Keyring)
	relayHTTPURL := relayws.HTTPURLFromWS(d.relayURL)
	if err := keyringMgr.Sync(context.Background(), relayHTTPURL); err != nil {
		return fmt.Errorf("failed to sync keyring: %w", err)
	}

	relayClient, err := relayws.NewMachineClient(d.relayURL, hostname, creds, machineSignPriv, keyringMgr)
	if err != nil {
		return fmt.Errorf("failed to create relay client: %w", err)
	}

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

func (d *Daemon) Stop(ctx context.Context) error {
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
