package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/update"
)

// The updater's network, package queries and commands, replaced in tests.
var (
	updateClient               = update.Default
	updatePaths                = update.UserPaths
	updateQuery  update.Runner = update.ExecRunner
	updateExec                 = func(argv []string) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	updateBinary = func() (string, error) {
		path, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(path)
	}
)

// activeJobs counts jobs the coordinator has not finished. A coordinator that
// cannot be asked has no jobs running under it.
func activeJobs(client ui.Client, o *Options) func(*cobra.Command) int {
	return func(c *cobra.Command) int {
		r, err := ui.NormalizeRequest("operation.list", app.Request{Connection: o.Connection})
		if err != nil {
			return 0
		}
		ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
		defer cancel()
		resp, err := client.Call(ctx, "operation.list", r)
		if err != nil || resp.Error != nil {
			return 0
		}
		var jobs []domain.Job
		if b, err := json.Marshal(resp.Data); err != nil || json.Unmarshal(b, &jobs) != nil {
			return 0
		}
		n := 0
		for _, j := range jobs {
			if !domain.Terminal(j.State) {
				n++
			}
		}
		return n
	}
}

func freshCheck(c *cobra.Command, p update.Paths) (update.Result, error) {
	ctx, cancel := context.WithTimeout(c.Context(), 20*time.Second)
	defer cancel()
	res, err := updateClient().Check(ctx, buildinfo.Version)
	if err != nil {
		return res, domain.Fail("OPERATION_FAILED", "Could not check GitHub for updates: "+update.Explain(err))
	}
	_ = p.Save(res)
	return res, nil
}

func updateCommands(o *Options, out io.Writer, emit func(app.Response) error, jobs func(*cobra.Command) int) *cobra.Command {
	var yes, downloadOnly bool
	cmd := &cobra.Command{Use: "update", Short: "Install the newest Virmill release from GitHub",
		Long: "Checks GitHub for a newer release, downloads the RPM or DEB packages this host uses, checks them against the release's " +
			"SHA256SUMS, the digest GitHub recorded for each upload and their own package name and version, then installs them with " +
			"dnf or apt, which asks for your password. " +
			"It refuses while jobs are running and restarts your coordinator afterwards. Checksums catch damaged downloads, not a " +
			"tampered release.",
		Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error {
			if o.Output != "table" {
				return domain.Fail("INVALID_INPUT", "update shows its progress as text; use update check --output json for machine-readable status")
			}
			p, err := updatePaths()
			if err != nil {
				return err
			}
			res, err := freshCheck(c, p)
			if err != nil {
				return err
			}
			if !res.Available() {
				fmt.Fprintf(out, "Virmill %s is the newest release.\n", res.Current)
				return nil
			}
			fmt.Fprintf(out, "Virmill %s is available (you have %s).\n", res.Latest.Version, res.Current)
			if res.Latest.Page != "" {
				fmt.Fprintf(out, "Release notes: %s\n", res.Latest.Page)
			}
			binary, err := updateBinary()
			if err != nil {
				return err
			}
			inst, err := update.DetectInstall(c.Context(), updateQuery, binary)
			if err != nil {
				return domain.Fail("UNSUPPORTED_CAPABILITY", err.Error())
			}
			if n := jobs(c); n > 0 && !downloadOnly {
				return domain.Fail("RESOURCE_BUSY", fmt.Sprintf("%d Virmill job(s) are not finished. Let them finish, or cancel or reconcile them in Jobs, then run virmill update again.", n))
			}
			if !yes {
				if o.NonInteractive {
					return domain.Fail("INVALID_INPUT", "add --yes to update without a prompt")
				}
				question := "Download, verify and install it now? dnf or apt will ask for your password. [y/N] "
				if downloadOnly {
					question = "Download and verify it now? [y/N] "
				}
				fmt.Fprint(out, question)
				answer, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
					fmt.Fprintln(out, "Nothing was changed.")
					return nil
				}
			}
			fmt.Fprintln(out, "Downloading and checking the packages...")
			ctx, cancel := context.WithTimeout(c.Context(), 30*time.Minute)
			defer cancel()
			files, err := updateClient().Prepare(ctx, *res.Latest, inst, p.Cache, updateQuery)
			if err != nil {
				return domain.Fail("OPERATION_FAILED", "The update was not installed: "+err.Error())
			}
			fmt.Fprintln(out, "Verified: each file matches SHA256SUMS and the digest GitHub recorded, and is the expected package and version.")
			argv := update.InstallCommand(inst.Format, files)
			if downloadOnly {
				fmt.Fprintf(out, "Install it with:\n  %s\n", strings.Join(argv, " "))
				return nil
			}
			fmt.Fprintf(out, "Installing with: %s\n", strings.Join(argv, " "))
			if err := updateExec(argv); err != nil {
				return domain.Fail("OPERATION_FAILED", fmt.Sprintf("The package manager stopped (%v), so Virmill %s is still installed. The verified packages stay in %s.",
					err, res.Current, filepath.Dir(files[0])))
			}
			if err := updateExec([]string{"systemctl", "--user", "try-restart", "virmilld.service"}); err != nil {
				fmt.Fprintln(out, "The coordinator did not restart. Restart it with: systemctl --user restart virmilld.service")
			}
			_ = p.Save(update.Result{Current: res.Latest.Version, CheckedAt: time.Now().UTC()})
			fmt.Fprintf(out, "Virmill %s is installed. Restart any open Virmill TUI to use it.\n", res.Latest.Version)
			return nil
		}}
	cmd.Flags().BoolVar(&yes, "yes", false, "Do not ask before downloading and installing (dnf or apt may still ask for your password)")
	cmd.Flags().BoolVar(&downloadOnly, "download-only", false, "Download and verify the packages, then print the install command instead of running it")

	check := &cobra.Command{Use: "check", Short: "Check GitHub for a newer Virmill release (read-only)", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			p, err := updatePaths()
			if err != nil {
				return err
			}
			res, err := freshCheck(c, p)
			if o.Output != "table" || o.Quiet {
				var failure *domain.Error
				if errors.As(err, &failure) {
					return emit(app.Response{APIVersion: domain.APIVersion, Warnings: []string{}, Error: failure})
				}
				return emit(app.Response{APIVersion: domain.APIVersion, Data: res, Warnings: []string{}})
			}
			if err != nil {
				return err
			}
			if res.Available() {
				fmt.Fprintf(out, "Virmill %s is available (you have %s).\n", res.Latest.Version, res.Current)
				if res.Latest.Page != "" {
					fmt.Fprintf(out, "Release notes: %s\n", res.Latest.Page)
				}
				fmt.Fprintln(out, "Install it with: virmill update")
			} else {
				fmt.Fprintf(out, "Virmill %s is the newest release.\n", res.Current)
			}
			if !p.ChecksEnabled() {
				fmt.Fprintln(out, "Daily checks are off. Turn them on with: virmill update checks on")
			}
			return nil
		}}

	checks := &cobra.Command{Use: "checks [on|off]", Short: "Show or change the daily update check", Args: cobra.MaximumNArgs(1), ValidArgs: []string{"on", "off"},
		RunE: func(c *cobra.Command, args []string) error {
			p, err := updatePaths()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if args[0] != "on" && args[0] != "off" {
					return domain.Fail("INVALID_INPUT", "use on or off")
				}
				if err := p.SetChecks(args[0] == "on"); err != nil {
					return err
				}
			}
			on := p.ChecksEnabled()
			if o.Output != "table" {
				return emit(app.Response{APIVersion: domain.APIVersion, Data: map[string]bool{"checks": on}, Warnings: []string{}})
			}
			state := "off"
			if on {
				state = "on"
			}
			fmt.Fprintf(out, "Daily update checks are %s.\n", state)
			if env := os.Getenv("VIRMILL_UPDATE_CHECK"); env != "" {
				fmt.Fprintf(out, "VIRMILL_UPDATE_CHECK=%s is set and takes priority over this setting.\n", env)
			}
			return nil
		}}
	cmd.AddCommand(check, checks)
	return cmd
}

// notifyUpdate prints one line after an interactive command when the daily
// check finds a newer release. It checks at most once a day, for three seconds.
func notifyUpdate(c *cobra.Command, o *Options, out, errOut io.Writer) {
	f, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) || o.Output != "table" || o.Quiet || o.NonInteractive || !c.HasParent() {
		return
	}
	for p := c; p != nil; p = p.Parent() {
		switch p.Name() {
		case "update", "tui", "completion", "reference", "help", "__complete":
			return
		}
	}
	paths, err := updatePaths()
	if err != nil || !paths.ChecksEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()
	if res := update.CheckIfDue(ctx, updateClient(), paths, buildinfo.Version, time.Now()); res.Available() {
		fmt.Fprintf(errOut, "\nVirmill %s is available (you have %s). Run: virmill update\n", res.Latest.Version, buildinfo.Version)
	}
}
