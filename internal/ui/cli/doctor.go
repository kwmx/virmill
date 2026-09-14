package cli

import (
	"encoding/json"
	"io"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

// ShownError wraps an error whose message the command already printed.
type ShownError struct{ Err error }

func (s ShownError) Error() string { return s.Err.Error() }
func (s ShownError) Unwrap() error { return s.Err }

// writeDoctor shows host checks as a readable report with install commands.
// JSON and NDJSON output keep the full machine-readable form.
func writeDoctor(out io.Writer, response app.Response) error {
	if response.Error != nil {
		if response.Error.Code == "COORDINATOR_UNAVAILABLE" {
			_, err := io.WriteString(out, "The Virmill coordinator isn't running, so the host check could not run.\n\n"+
				"Start it:\n  systemctl --user start virmilld.service\n\nThen run virmill doctor again.\n")
			if err != nil {
				return err
			}
			return ShownError{response.Error}
		}
		return writeResponse(out, "table", false, response)
	}
	var checks []domain.Capability
	data, err := json.Marshal(response.Data)
	if err == nil {
		err = json.Unmarshal(data, &checks)
	}
	if err != nil {
		return writeResponse(out, "table", false, response)
	}
	_, err = io.WriteString(out, validation.SafeText(ui.DoctorReport(checks, false)))
	return err
}
