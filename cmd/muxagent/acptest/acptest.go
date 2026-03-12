package acptest

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/acpbin"
	"github.com/LaLanMo/muxagent-cli/internal/codexbin"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/runtime/acp"
	"github.com/spf13/cobra"
)

func NewCmd() *cobra.Command {
	var prompt string
	var cwd string
	var runtimeID string

	cmd := &cobra.Command{
		Use:   "acp-test",
		Short: "Test ACP runtime link directly (bypasses relay/auth)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" {
				prompt = "say hello"
			}
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			return run(cmd, prompt, cwd, runtimeID)
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text to send (default: \"say hello\")")
	cmd.Flags().StringVar(&cwd, "cwd", "", "Working directory for agent (default: current dir)")
	cmd.Flags().StringVar(&runtimeID, "runtime", "", "Runtime ID to test (required when multiple runtimes are configured)")
	return cmd
}

func run(cmd *cobra.Command, promptText, cwd, requestedRuntime string) error {
	cfg, err := config.LoadEffective()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	runtimeID, err := selectRuntime(cfg, requestedRuntime)
	if err != nil {
		return err
	}

	rtSettings, err := cfg.RuntimeSettingsFor(runtimeID)
	if err != nil {
		return fmt.Errorf("runtime settings: %w", err)
	}
	if runtimeID == config.RuntimeClaudeCode {
		resolved, err := acpbin.Resolve(cfg, nil)
		if err != nil {
			return fmt.Errorf("resolve Claude Code runtime: %w", err)
		}
		rtSettings.Command = resolved
		rtSettings, err = acpbin.InjectClaudeCodeExecutable(rtSettings)
		if err != nil {
			return fmt.Errorf("configure Claude Code executable wrapper: %w", err)
		}
	} else if runtimeID == config.RuntimeCodex {
		if !(config.IsRuntimeCommandOverridden(runtimeID) && rtSettings.Command != "") {
			resolved, err := codexbin.Resolve(cfg, nil)
			if err != nil {
				return fmt.Errorf("resolve Codex runtime: %w", err)
			}
			rtSettings.Command = resolved
		}
	}

	client := acp.NewClient(acp.Config{
		Command: rtSettings.Command,
		Args:    rtSettings.Args,
		CWD:     rtSettings.CWD,
		Env:     rtSettings.Env,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintln(cmd.OutOrStdout(), "[init] Starting ACP...")
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer client.Stop()
	fmt.Fprintln(cmd.OutOrStdout(), "[init] ACP initialized")

	sessionID, _, err := client.NewSession(ctx, cwd, "")
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[session] created: %s\n", sessionID)

	// Consume events in background
	go func() {
		for ev := range client.Events() {
			printEvent(cmd, ev, client, ctx, sessionID)
		}
	}()

	content := []domain.ContentBlock{{Type: "text", Text: promptText}}
	fmt.Fprintf(cmd.OutOrStdout(), "[prompt] sending: %q\n", promptText)

	stopReason, _, err := client.Prompt(ctx, sessionID, content)
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[done] stopReason: %s\n", stopReason)
	return nil
}

func selectRuntime(cfg config.Config, requestedRuntime string) (config.RuntimeID, error) {
	if requestedRuntime != "" {
		id := config.RuntimeID(requestedRuntime)
		if !config.IsSupportedRuntime(id) {
			return "", fmt.Errorf("runtime %q is not supported", requestedRuntime)
		}
		if _, ok := cfg.Runtimes[id]; !ok {
			return "", fmt.Errorf("runtime %q is not configured", requestedRuntime)
		}
		return id, nil
	}

	ids := cfg.ConfiguredRuntimeIDs()
	if len(ids) == 1 {
		return ids[0], nil
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, string(id))
	}
	return "", fmt.Errorf("multiple runtimes configured; pass --runtime (%s)", strings.Join(names, ", "))
}

func printEvent(cmd *cobra.Command, ev domain.Event, client *acp.Client, ctx context.Context, sessionID string) {
	out := cmd.OutOrStdout()
	switch ev.Type {
	case domain.EventMessageDelta:
		if ev.MessagePart != nil {
			text := ev.MessagePart.Delta
			if len(text) > 80 {
				text = text[:80] + "..."
			}
			fmt.Fprintf(out, "[event] message.delta: %q\n", text)
		}
	case domain.EventReasoning:
		if ev.MessagePart != nil {
			text := ev.MessagePart.Delta
			if len(text) > 80 {
				text = text[:80] + "..."
			}
			fmt.Fprintf(out, "[event] reasoning: %q\n", text)
		}
	case domain.EventToolStarted:
		if ev.Tool != nil {
			fmt.Fprintf(out, "[event] tool.started: %s (call: %s)\n", ev.Tool.Name, ev.Tool.CallID)
		}
	case domain.EventToolUpdated:
		if ev.Tool != nil {
			fmt.Fprintf(out, "[event] tool.updated: %s → %s\n", ev.Tool.Name, ev.Tool.Status)
		}
	case domain.EventToolCompleted:
		if ev.Tool != nil {
			output := ev.Tool.Output
			if len(output) > 80 {
				output = output[:80] + "..."
			}
			output = strings.ReplaceAll(output, "\n", "\\n")
			fmt.Fprintf(out, "[event] tool.completed: %s → %q\n", ev.Tool.Name, output)
		}
	case domain.EventToolFailed:
		if ev.Tool != nil {
			fmt.Fprintf(out, "[event] tool.failed: %s → %q\n", ev.Tool.Name, ev.Tool.Error)
		}
	case domain.EventApprovalRequested:
		if ev.Approval != nil {
			fmt.Fprintf(out, "[event] approval.requested: %s → auto-approving (once)\n", ev.Approval.ToolName)
			// Auto-approve with "once"
			optionID := "once"
			if len(ev.Approval.Options) > 0 {
				optionID = ev.Approval.Options[0].OptionID
			}
			if err := client.ReplyPermission(ctx, sessionID, ev.Approval.ID, optionID); err != nil {
				fmt.Fprintf(out, "[error] reply permission: %v\n", err)
			}
		}
	case domain.EventPlanUpdated:
		fmt.Fprintln(out, "[event] plan.updated")
	default:
		fmt.Fprintf(out, "[event] %s\n", ev.Type)
	}
}
