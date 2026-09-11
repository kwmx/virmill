package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

func consoleCommand(client ui.Client, options *Options) *cobra.Command {
	var choice string
	cmd := &cobra.Command{Use: "open VM_UUID", Short: "Open the guest display or a configured serial console", Long: "Open an explicitly selected local console. Start the VM first. A serial device does not guarantee a guest login. Ctrl+] leaves serial access; closing a viewer does not stop the VM. Use vm console show for scriptable discovery.", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&choice, "choice", "", "Console choice ID from vm console show; optional when exactly one is available")
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if options.NonInteractive || options.Output != "table" || !term.IsTerminal(int(os.Stdin.Fd())) {
			return domain.Fail("INVALID_INPUT", "Console access needs an interactive terminal; use vm console show for inspection.")
		}
		if client == nil {
			return domain.Fail("OPERATION_FAILED", "The local coordinator is unavailable.")
		}
		ctx, cancel := context.WithTimeout(c.Context(), min(options.Timeout, 15*time.Second))
		defer cancel()
		response, err := client.Call(ctx, "vm.console.show", app.Request{Connection: options.Connection, ID: args[0]})
		if err != nil {
			return err
		}
		if response.Error != nil {
			return response.Error
		}
		var info domain.ConsoleInfo
		b, err := json.Marshal(response.Data)
		if err != nil || json.Unmarshal(b, &info) != nil {
			return domain.Fail("OPERATION_FAILED", "Could not read console choices.")
		}
		selected := choice
		if selected == "" {
			for _, item := range info.Choices {
				if item.Available {
					if selected != "" {
						return domain.Fail("INVALID_INPUT", "Several consoles are available; use --choice with an ID from vm console show, or choose Console in the TUI.")
					}
					selected = item.ID
				}
			}
		}
		if selected == "" {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "No console is available. Start the VM and check vm console show for prerequisites.")
		}
		session, err := ui.PrepareConsole(ctx, client, options.Connection, args[0], selected, info.ConfigFingerprint)
		if err != nil {
			return err
		}
		defer session.Close()
		fmt.Fprintln(c.ErrOrStderr(), "Opening console. Ctrl+] leaves serial access. Closing the console keeps the VM running.")
		session.Command.Stdin, session.Command.Stdout, session.Command.Stderr = os.Stdin, c.OutOrStdout(), c.ErrOrStderr()
		if err := session.Command.Run(); err != nil {
			return domain.Fail("OPERATION_FAILED", "Console ended: "+validation.SafeText(err.Error())+". The VM was not stopped by Virmill.")
		}
		return session.Close()
	}
	return cmd
}
