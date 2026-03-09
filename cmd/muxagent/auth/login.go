package auth

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a mobile device via QR code",
		Long: `Initiates an authentication flow with the relay server.

A QR code will be displayed in the terminal. Scan it with the muxagent
mobile app to complete authentication.

This command will:
1. Generate a secure keypair for this machine
2. Display a QR code linking to the relay server
3. Wait for you to scan and approve on the mobile app
4. Save encrypted credentials locally`,
		RunE: runLogin,
	}

	cmd.Flags().Bool("force", false, "Force re-authentication even if already logged in")

	return cmd
}

func runLogin(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	// Check if already authenticated
	if auth.HasCredentials() && !force {
		fmt.Println("Already authenticated. Use --force to re-authenticate.")
		return nil
	}

	// Load config to get relay URL
	cfg, err := config.LoadEffective()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Convert ws:// to http:// for REST API
	relayURL := cfg.RelayURL
	relayURL = strings.Replace(relayURL, "ws://", "http://", 1)
	relayURL = strings.Replace(relayURL, "wss://", "https://", 1)
	relayURL = strings.TrimSuffix(relayURL, "/ws")

	relaySignPub, err := config.ResolveRelaySigningPublicKey(relayURL, cfg.RelaySigningPublicKey)
	if err != nil {
		return err
	}

	fmt.Println("Starting authentication flow...")
	fmt.Printf("Relay server: %s\n\n", relayURL)

	// Create auth flow handler
	flow := auth.NewAuthFlow(relayURL, relaySignPub)

	// Run the auth flow
	creds, err := flow.RunAuthFlow(cmd.Context(), func(qrURL string) error {
		fmt.Println("Scan this QR code with the muxagent mobile app:")
		fmt.Println()

		// Display the QR code
		if err := auth.QRTerminalOutput(os.Stdout, qrURL); err != nil {
			// Fall back to printing the URL
			fmt.Println("(Could not display QR code)")
		}

		fmt.Printf("Or open this URL manually:\n%s\n\n", qrURL)
		if err := auth.CopyToClipboard(qrURL); err == nil {
			fmt.Println("(Copied to clipboard)")
		}
		fmt.Println("Waiting for approval...")

		return nil
	})

	if err != nil {
		if err == context.Canceled {
			fmt.Println("\nAuthentication cancelled.")
			return nil
		}
		return fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println("\nAuthentication successful!")
	fmt.Printf("Machine ID: %s\n", creds.MachineID)

	return nil
}
