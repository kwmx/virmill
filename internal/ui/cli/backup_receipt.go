package cli

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/spf13/cobra"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

func backupReceiptExportCommand(client ui.Client, options *Options) *cobra.Command {
	return &cobra.Command{Use: "export OPERATION_ID PATH", Short: "Save a portable backup recovery receipt without credentials", Long: "Save recovery identifiers for one verified backup. Keep the receipt with your recovery records and the repository password separately. Existing files are never replaced. Recovery still verifies repository contents.", Args: cobra.ExactArgs(2), RunE: func(c *cobra.Command, args []string) error {
		if filepath.Clean(args[1]) != args[1] {
			return domain.Fail("INVALID_INPUT", "Choose a path without . or .. components")
		}
		path, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(c.Context(), options.Timeout)
		defer cancel()
		response, err := client.Call(ctx, "backup.receipt.show", app.Request{Connection: options.Connection, ID: args[0]})
		if err != nil {
			return err
		}
		if response.Error != nil {
			return response.Error
		}
		b, err := json.Marshal(response.Data)
		if err != nil {
			return err
		}
		if err = validation.Schema("backup-receipt", b); err != nil {
			return domain.Fail("INVALID_INPUT", "The coordinator did not return a valid backup receipt")
		}
		var receipt map[string]any
		if err = wire.Decode(b, &receipt); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = ui.ExportJSONDocument(path, receipt); err != nil {
			return err
		}
		return writeResponse(c.OutOrStdout(), options.Output, options.Quiet, app.Response{APIVersion: domain.APIVersion, Data: map[string]any{"path": path, "saved": true}, Warnings: []string{}})
	}}
}
