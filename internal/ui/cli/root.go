package cli

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/ui/tui"
	"virmill.local/core/internal/wire"
)

type Options struct {
	Connection              string
	Output                  string
	NonInteractive          bool
	Timeout                 time.Duration
	Quiet, Verbose, NoColor bool
	Config                  string
}

func New(client ui.Client, out, errOut io.Writer) *cobra.Command {
	o := &Options{}
	root := &cobra.Command{Use: "virmill", Short: "Virmill local Linux virtualization suite (development build)", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(out)
	root.SetErr(errOut)
	f := root.PersistentFlags()
	f.StringVar(&o.Connection, "connection", "qemu:///system", "Explicit local libvirt connection")
	f.StringVar(&o.Output, "output", "table", "table, json or ndjson")
	f.BoolVar(&o.NonInteractive, "non-interactive", false, "Never prompt")
	f.DurationVar(&o.Timeout, "timeout", 30*time.Second, "Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach")
	f.BoolVar(&o.Quiet, "quiet", false, "Suppress human output")
	f.BoolVar(&o.Verbose, "verbose", false, "Verbose diagnostics")
	f.BoolVar(&o.NoColor, "no-color", false, "Disable color (output is plain by default)")
	f.StringVar(&o.Config, "config", "", "Configuration path (reserved; nonempty input is rejected)")
	root.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if o.Config != "" {
			return domain.Fail("NOT_IMPLEMENTED", "configuration file loading is not yet implemented")
		}
		if o.Output != "table" && o.Output != "json" && o.Output != "ndjson" {
			return domain.Fail("INVALID_INPUT", "output must be table, json or ndjson")
		}
		if o.Timeout <= 0 {
			return domain.Fail("INVALID_INPUT", "timeout must be positive")
		}
		return nil
	}
	root.RunE = func(c *cobra.Command, args []string) error {
		if term.IsTerminal(int(os.Stdin.Fd())) && o.Output == "table" && !o.NonInteractive {
			return tui.RunOptions(client, o.Connection, o.NoColor)
		}
		return c.Help()
	}
	emit := func(response app.Response) error {
		return writeResponse(out, o.Output, o.Quiet, response)
	}
	send := func(c *cobra.Command, method string, r app.Request, write func(app.Response) error) error {
		r.Connection = o.Connection
		var err error
		r, err = ui.NormalizeRequest(method, r)
		if err != nil {
			return err
		}
		wait := o.Timeout
		if ui.ImportRead(method) && !root.PersistentFlags().Changed("timeout") {
			wait = ui.ImportWait
		}
		ctx, cancel := context.WithTimeout(c.Context(), wait)
		defer cancel()
		resp, e := client.Call(ctx, method, r)
		if e != nil {
			d, ok := e.(*domain.Error)
			if !ok {
				d = domain.Fail("OPERATION_FAILED", e.Error())
			}
			return write(app.Response{APIVersion: domain.APIVersion, Warnings: []string{}, Error: d})
		}
		return write(resp)
	}
	call := func(c *cobra.Command, method string, r app.Request) error { return send(c, method, r, emit) }
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show application and build contracts", RunE: func(c *cobra.Command, args []string) error {
		return emit(app.Response{APIVersion: domain.APIVersion, Data: buildinfo.Info(), Warnings: []string{}})
	}})
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Check host prerequisites and show how to install what's missing (read-only)", RunE: func(c *cobra.Command, args []string) error {
		if o.Output != "table" || o.Quiet {
			return call(c, "host.doctor", app.Request{})
		}
		return send(c, "host.doctor", app.Request{}, func(r app.Response) error { return writeDoctor(out, r) })
	}})
	root.AddCommand(&cobra.Command{Use: "tui", Short: "Open the keyboard interface", RunE: func(c *cobra.Command, args []string) error {
		if o.NonInteractive {
			return domain.Fail("INVALID_INPUT", "TUI requires an interactive terminal")
		}
		return tui.RunOptions(client, o.Connection, o.NoColor)
	}})
	root.AddCommand(updateCommands(o, out, emit, activeJobs(client, o)))
	root.PersistentPostRunE = func(c *cobra.Command, args []string) error {
		notifyUpdate(c, o, out, errOut)
		return nil
	}
	parents := map[string]*cobra.Command{"": root}
	add := func(path string, leaf *cobra.Command) {
		parts := strings.Split(path, " ")
		current := root
		prefix := ""
		for _, p := range parts[:len(parts)-1] {
			if prefix != "" {
				prefix += " "
			}
			prefix += p
			if parents[prefix] == nil {
				parent := &cobra.Command{Use: p, Short: "Manage " + prefix}
				current.AddCommand(parent)
				parents[prefix] = parent
			}
			current = parents[prefix]
		}
		current.AddCommand(leaf)
	}
	for _, item := range ui.Actions {
		a := item
		parts := strings.Split(a.Command, " ")
		use := parts[len(parts)-1]
		if a.Argument != "" && a.Argument != "parameters" {
			use += " " + strings.ToUpper(a.Argument)
		}
		var input string
		var deleteDisks []string
		var after int64
		var hard, planOnly, follow bool
		pluginFlags := map[string]*string{}
		cmd := &cobra.Command{Use: use, Short: a.Summary, Long: a.Summary + ". Calls the shared coordinator service. Mutations return an immutable preview; apply it with plan apply and exact acknowledgements. No privilege is implied by --yes.", Args: cobra.NoArgs}
		if a.Argument != "" && a.Argument != "parameters" {
			cmd.Args = cobra.ExactArgs(1)
		}
		cmd.Flags().StringVar(&input, "input", "{}", "JSON parameters; secrets must be references")
		cmd.Flags().Int64Var(&after, "after", 0, "Event cursor")
		if a.Command == "operation watch" {
			cmd.Flags().BoolVar(&follow, "follow", false, "Follow ordered events and terminal state with --output ndjson; timeout detaches")
		}
		if a.Mutation != "" {
			cmd.Flags().BoolVar(&planOnly, "plan", true, "Return preview (apply separately after review)")
		}
		if a.Command == "vm stop" {
			cmd.Flags().BoolVar(&hard, "hard", false, "Plan abrupt power-off, requiring data-loss acknowledgement")
		}
		if a.Command == "vm remove" {
			cmd.Flags().StringArrayVar(&deleteDisks, "delete-disk", nil, "Permanently delete selected guest disk targets (vda,vdb); repeatable; omitted keeps all disks")
			cmd.Long += " Disks and backups are kept by default. --delete-disk accepts guest target IDs, not host paths; selected disk deletion is irreversible and requires its own plan acknowledgement. Backups are retained."
		}
		if strings.HasPrefix(a.Command, "plugin ") {
			for _, spec := range []struct{ flag, key, help string }{
				{"id", "id", "Stable plugin ID for a new scaffold"},
				{"language", "language", "Scaffold source language (go)"},
				{"type", "type", "Scaffold extension type (action)"},
				{"sdk-directory", "sdkDirectory", "Reviewed local SDK source directory"},
				{"key-id", "keyID", "Signing-key identifier"},
				{"public-key", "publicKey", "Reviewed hexadecimal Ed25519 public key"},
				{"signing-key-file", "signingKeyPath", "Private signing-key file outside package source"},
				{"destination", "output", "New output archive path for pack"},
			} {
				allowed := (a.Mutation == "new" && (spec.key == "id" || spec.key == "language" || spec.key == "type" || spec.key == "sdkDirectory")) || ((a.Mutation == "install" || a.Mutation == "update") && (spec.key == "keyID" || spec.key == "publicKey")) || (a.Mutation == "pack" && (spec.key == "keyID" || spec.key == "signingKeyPath" || spec.key == "output"))
				if allowed {
					pluginFlags[spec.key] = cmd.Flags().String(spec.flag, "", spec.help)
				}
			}
		}
		if a.Command == "plugin call" {
			cmd.Use = "call PLUGIN_ID ACTION_ID"
			cmd.Args = cobra.ExactArgs(2)
		}
		if a.Command == "guest recipe run" {
			cmd.Use = "run VM_UUID RECIPE_PATH"
			cmd.Args = cobra.ExactArgs(2)
		}
		if a.Command == "network create" {
			cmd.Use = "create [PATH]"
			cmd.Args = cobra.MaximumNArgs(1)
			cmd.Long += " Supply either PATH or --input containing a complete Network object under document."
		}
		if a.Command == "plugin new" {
			cmd.Use = "new [PATH]"
			cmd.Args = cobra.MaximumNArgs(1)
		}
		cmd.RunE = func(c *cobra.Command, args []string) error {
			r := app.Request{Action: a.Mutation, After: after}
			if a.Argument == "id" {
				r.ID = args[0]
			}
			if a.Command == "guest recipe run" {
				r.Path = args[1]
			}
			if a.Argument == "path" && len(args) > 0 {
				r.Path = args[0]
			}
			if hard {
				r.Action = "hard-stop"
			}
			if e := wire.Decode([]byte(input), &r.Input); e != nil {
				return domain.Fail("INVALID_INPUT", "invalid input JSON")
			}
			if r.Input == nil {
				r.Input = map[string]any{}
			}
			if a.Command == "vm remove" && c.Flags().Changed("delete-disk") {
				if _, exists := r.Input["deleteDisks"]; exists {
					return domain.Fail("INVALID_INPUT", "deleteDisks was supplied by both --delete-disk and JSON; choose one")
				}
				selected := []string{}
				seen := map[string]bool{}
				for _, value := range deleteDisks {
					for _, target := range strings.Split(value, ",") {
						if target == "" || strings.TrimSpace(target) != target || strings.ContainsAny(target, "/\\ \t\r\n") || len(target) > 32 {
							return domain.Fail("INVALID_INPUT", "--delete-disk needs a nonempty guest disk target such as vda, not a host path")
						}
						if seen[target] {
							return domain.Fail("INVALID_INPUT", "duplicate --delete-disk target: "+target)
						}
						seen[target] = true
						selected = append(selected, target)
					}
				}
				if len(selected) == 0 {
					return domain.Fail("INVALID_INPUT", "--delete-disk requires at least one explicit guest disk target")
				}
				r.Input["deleteDisks"] = selected
			}
			if a.Command == "vm guest-agent enable" {
				if _, exists := r.Input["enableGuestAgent"]; exists {
					return domain.Fail("INVALID_INPUT", "enableGuestAgent is supplied by this command")
				}
				r.Input["enableGuestAgent"] = true
			}
			for key, value := range pluginFlags {
				if *value != "" {
					if _, exists := r.Input[key]; exists {
						return domain.Fail("INVALID_INPUT", "parameter supplied by both flag and JSON: "+key)
					}
					r.Input[key] = *value
				}
			}
			if a.Command == "plugin call" {
				if _, exists := r.Input["action"]; exists {
					return domain.Fail("INVALID_INPUT", "action is provided as the second positional argument")
				}
				r.Input["action"] = args[1]
			}
			if a.Mutation != "" && !planOnly {
				return domain.Fail("INVALID_INPUT", "review and apply the generated plan using plan apply")
			}
			if follow {
				if o.Output != "ndjson" || len(r.Input) != 0 {
					return domain.Fail("INVALID_INPUT", "--follow requires --output ndjson and no input parameters")
				}
				interruptible, stop := signal.NotifyContext(c.Context(), os.Interrupt)
				defer stop()
				ctx, cancel := context.WithTimeout(interruptible, o.Timeout)
				defer cancel()
				return followOperation(ctx, client, o.Connection, r.ID, after, nil, true, emit)
			}
			return call(c, a.Method, r)
		}
		add(a.Command, cmd)
	}
	var digest, key string
	var acks []string
	var wait, detach bool
	apply := &cobra.Command{Use: "apply PLAN_ID", Short: "Apply an immutable plan with explicit digest and acknowledgements", Args: cobra.ExactArgs(1)}
	apply.Flags().StringVar(&digest, "digest", "", "Exact reviewed plan digest (required)")
	apply.Flags().StringVar(&key, "idempotency-key", "", "Stable request key (required)")
	apply.Flags().StringSliceVar(&acks, "ack", nil, "Explicit plan acknowledgement IDs")
	apply.Flags().BoolVar(&wait, "wait", false, "Wait for terminal operation state")
	apply.Flags().BoolVar(&detach, "detach", false, "Return after durable submission")
	apply.RunE = func(c *cobra.Command, args []string) error {
		if digest == "" || key == "" {
			return domain.Fail("INVALID_INPUT", "--digest and --idempotency-key required")
		}
		if wait && detach {
			return domain.Fail("INVALID_INPUT", "choose --wait or --detach")
		}
		parent := c.Context()
		if wait {
			var stop context.CancelFunc
			parent, stop = signal.NotifyContext(parent, os.Interrupt)
			defer stop()
		}
		limit := o.Timeout
		if !root.PersistentFlags().Changed("timeout") {
			limit = ui.ImportWait
		}
		ctx, cancel := context.WithTimeout(parent, limit)
		defer cancel()
		if e := ctx.Err(); e != nil {
			return emit(streamFailure(ctx, e, "", 0, nil))
		}
		resp, e := client.Call(ctx, "operation.apply", app.Request{Connection: o.Connection, Apply: &operations.ApplyRequest{PlanID: args[0], PlanDigest: digest, IdempotencyKey: key, Acknowledgements: acks}})
		if e != nil {
			if wait {
				return emit(streamFailure(ctx, e, "", 0, nil))
			}
			return emit(app.Response{APIVersion: domain.APIVersion, Warnings: []string{}, Error: responseFailure(e)})
		}
		if resp.Error != nil || !wait {
			if wait && ctx.Err() != nil {
				return emit(streamFailure(ctx, ctx.Err(), "", 0, nil))
			}
			return emit(resp)
		}
		job, e := decodeStreamJob(resp, "")
		if e != nil {
			return emit(streamFailure(ctx, e, "", 0, nil))
		}
		if job.PlanID != args[0] {
			return emit(streamFailure(ctx, domain.Fail("INVALID_STATE", "accepted operation refers to a different plan"), "", 0, nil))
		}
		stream := o.Output == "ndjson"
		if stream {
			// Keep durable acceptance visible before following. It does not imply
			// completion, and an interrupted stream never resubmits this request.
			resp.Data = job
			if e = emit(resp); e != nil {
				return e
			}
		}
		return followOperation(ctx, client, o.Connection, job.ID, 0, &job, stream, emit)
	}
	add("vm console open", consoleCommand(client, o))
	add("plan apply", apply)
	completion := &cobra.Command{Use: "completion SHELL", Short: "Generate shell completions", Args: cobra.ExactArgs(1), ValidArgs: []string{"bash", "zsh", "fish", "powershell"}, RunE: func(c *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(out)
		case "zsh":
			return root.GenZshCompletion(out)
		case "fish":
			return root.GenFishCompletion(out, true)
		case "powershell":
			return root.GenPowerShellCompletion(out)
		default:
			return domain.Fail("INVALID_INPUT", "unknown shell")
		}
	}}
	add("backup receipt export", backupReceiptExportCommand(client, o))
	root.AddCommand(completion)
	ref := &cobra.Command{Use: "reference", Hidden: true, RunE: func(c *cobra.Command, args []string) error { return Reference(root, out) }}
	root.AddCommand(ref)
	return root
}
func Reference(root *cobra.Command, w io.Writer) error {
	fmt.Fprint(w, "# Virmill generated CLI reference\n\nDevelopment build. Only implemented commands appear here; the full 1.0 contract remains mandatory.\n\n")
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		if c.Runnable() {
			fmt.Fprintf(w, "## `%s`\n\n%s\n\n```text\n%s```\n\n", c.CommandPath(), c.Short, c.UsageString())
		}
		children := c.Commands()
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			visit(child)
		}
	}
	visit(root)
	return nil
}
