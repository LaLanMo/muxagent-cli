package auth

import (
	"encoding/base64"
	"fmt"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		Long: `Displays the current authentication status including:
- Whether credentials are stored
- Machine ID
- Public keys (for verification)`,
		RunE: runStatus,
	}

	cmd.Flags().Bool("verbose", false, "Show detailed key information")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")

	if !auth.HasCredentials() {
		fmt.Println("Status: Not authenticated")
		fmt.Println("\nRun 'muxagent auth login' to authenticate.")
		return nil
	}

	creds, _, _, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	fmt.Println("Status: Authenticated")
	fmt.Printf("Machine ID: %s\n", creds.MachineID)
	fmt.Printf("Master ID: %s\n", creds.MasterID)
	if creds.Keyring.MasterID != "" {
		fmt.Printf("Keyring Seq: %d\n", creds.Keyring.Seq)
		fmt.Printf("Keyring Head: %s\n", creds.Keyring.HeadHash)
	}

	if verbose {
		fmt.Printf("\nMachine Sign Public Key (this device):\n  %s\n", creds.Keys.MachineSignPublicKey)
		fmt.Printf("Machine Enc Public Key (this device):\n  %s\n", creds.Keys.MachineEncPublicKey)

		// Show key fingerprints (first 8 bytes as hex)
		if machinePub, err := base64.StdEncoding.DecodeString(creds.Keys.MachineSignPublicKey); err == nil && len(machinePub) >= 8 {
			fmt.Printf("\nMachine Sign Key Fingerprint: %x\n", machinePub[:8])
		}
		if machineEncPub, err := base64.StdEncoding.DecodeString(creds.Keys.MachineEncPublicKey); err == nil && len(machineEncPub) >= 8 {
			fmt.Printf("Machine Enc Key Fingerprint:  %x\n", machineEncPub[:8])
		}

		if len(creds.Keyring.Keys) > 0 {
			fmt.Printf("\nMaster Keys (%d):\n", len(creds.Keyring.Keys))
			for _, key := range creds.Keyring.Keys {
				fmt.Printf("- %s\n", key.MasterSignKeyFingerprint)
				if masterPub, err := base64.StdEncoding.DecodeString(key.MasterSignPub); err == nil && len(masterPub) >= 8 {
					fmt.Printf("  Sign Pub Fingerprint: %x\n", masterPub[:8])
				}
				if masterEncPub, err := base64.StdEncoding.DecodeString(key.MasterEncPub); err == nil && len(masterEncPub) >= 8 {
					fmt.Printf("  Enc Pub Fingerprint:  %x\n", masterEncPub[:8])
				}
			}
		}
	}

	return nil
}
