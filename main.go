package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	jiracmd "github.com/cgrossde/jiracli/cmd"
	"github.com/cgrossde/jiracli/internal/output"
)

//go:embed .claude/skills/jira/SKILL.md
var skillMD []byte

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

// run is the testable entry point. stdout receives presenter-formatted output;
// stderr receives progress messages and slog output.
//
// Any error returned by Cobra (flag errors, unknown commands) is formatted
// through the presenter so the caller always sees a [exit:N | Xms] footer.
// jiracmd.ErrAlreadyPresented is returned by commands that write their own output.

func run(args []string, stdout, stderr io.Writer) error {
	slog.SetDefault(slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	output.SetToolName("jiracli")

	start := time.Now()
	root := buildRoot(stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return nil
	}
	if errors.Is(err, jiracmd.ErrAlreadyPresented) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return err
	}

	// Errors that reach here come from Cobra's own validation (flag parse
	// errors, arg-count validation) — i.e. the user invoked the command wrong.
	// Runtime errors from RunE are presented inside WrapWithPresenter and never
	// reach this point (they return ErrAlreadyPresented, handled above).
	//
	// Two shapes of user error:
	//   - Invoked with no positional args because a required arg is missing:
	//     show the full help (description + usage + flags) — the description is
	//     the most useful thing to surface when someone types a bare command to
	//     discover it.
	//   - Invoked with the wrong argument/flag: show the terse usage block and
	//     point at --help for the full reference.
	usageStr := ""
	if found, _, findErr := root.Find(args); findErr == nil && found != nil {
		if isMissingRequiredArgErr(found, err) {
			usageStr = renderHelp(found)
		} else {
			usageStr = found.UsageString() +
				fmt.Sprintf("\nRun '%s --help' for more details.\n", found.CommandPath())
		}
	}
	output.Print(stdout, stderr, output.Result{
		Stdout:   usageStr,
		Stderr:   err.Error(),
		ExitCode: 1,
		Elapsed:  time.Since(start),
	})
	return err
}

// isMissingRequiredArgErr reports whether err is the command's own argument
// validation error triggered by receiving zero positional args. It re-runs the
// command's Args validator against the parsed positional args so the detection
// stays accurate regardless of cobra's error wording.
func isMissingRequiredArgErr(cmd *cobra.Command, err error) bool {
	if cmd == nil || cmd.Args == nil || !cmd.Runnable() {
		return false
	}
	posArgs := cmd.Flags().Args()
	if len(posArgs) != 0 {
		return false
	}
	argErr := cmd.Args(cmd, posArgs)
	return argErr != nil && argErr.Error() == err.Error()
}

// renderHelp captures the command's full help output (using the shared help
// template: usage + flags + description) into a string.
func renderHelp(cmd *cobra.Command) string {
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if helpErr := cmd.Help(); helpErr != nil {
		return cmd.UsageString()
	}
	return buf.String()
}

// buildRoot constructs the full Cobra command tree and returns the root command.
// stdout/stderr are injected so every command's output is testable.
func buildRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "jiracli",
		Short:         "Easy, agent-friendly & scriptable Jira access",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	// Preserve AddCommand registration order in help output.
	cobra.EnableCommandSorting = false

	// Help template: Usage+Flags first, then Long description.
	root.SetHelpTemplate(
		"{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}" +
			"{{with (or .Long .Short)}}{{if not (or $.Runnable $.HasSubCommands)}}" +
			"{{. | trimTrailingWhitespaces}}\n\n{{else}}\n{{. | trimTrailingWhitespaces}}\n" +
			"{{end}}{{end}}")
	// Command groups — "main" for everyday use, "util" for admin/meta.
	root.AddGroup(
		&cobra.Group{ID: "main", Title: "Commands:"},
		&cobra.Group{ID: "util", Title: "Additional Commands:"},
	)
	root.SetCompletionCommandGroupID("util")
	root.SetHelpCommandGroupID("util")

	// wrapGroup attaches the presenter to every leaf under a group command.
	wrapGroup := func(g *cobra.Command) {
		for _, sub := range g.Commands() {
			WrapWithPresenter(sub, stdout, stderr)
		}
	}

	// ── main group (registration order = display order) ──────────────────────

	// show — read an issue or attachment by ref.
	showCmd := jiracmd.NewShowCmd(stdout)
	WrapWithPresenter(showCmd, stdout, stderr)
	wrapGroup(showCmd)
	showCmd.GroupID = "main"
	root.AddCommand(showCmd)

	// search — JQL search.
	searchCmd := jiracmd.NewSearchCmd()
	WrapWithPresenter(searchCmd, stdout, stderr)
	searchCmd.GroupID = "main"
	root.AddCommand(searchCmd)

	// hierarchy — locate an issue in its Initiative/Epic tree and list descendants.
	hierarchyCmd := jiracmd.NewHierarchyCmd()
	WrapWithPresenter(hierarchyCmd, stdout, stderr)
	hierarchyCmd.GroupID = "main"
	root.AddCommand(hierarchyCmd)

	// effort — aggregate time + story points across a hierarchy or result set.
	// The top-level command has its own RunE (effort <KEY>) and two subcommands
	// (jql, sprint); wrap all three so every path gets the presenter footer,
	// overflow handling, and runtime-error presentation.
	effortCmd := jiracmd.NewEffortCmd()
	WrapWithPresenter(effortCmd, stdout, stderr)
	wrapGroup(effortCmd)
	effortCmd.GroupID = "main"
	root.AddCommand(effortCmd)

	// create — new issue.
	createCmd := jiracmd.NewCreateCmd()
	WrapWithPresenter(createCmd, stdout, stderr)
	createCmd.GroupID = "main"
	root.AddCommand(createCmd)

	// edit group — scalar mutations.
	editCmd := jiracmd.NewEditCmd()
	wrapGroup(editCmd)
	editCmd.GroupID = "main"
	root.AddCommand(editCmd)

	// board group — agile board inspection.
	boardCmd := jiracmd.NewBoardCmd()
	wrapGroup(boardCmd)
	boardCmd.GroupID = "main"
	root.AddCommand(boardCmd)

	// sprint group — agile sprint inspection and mutation.
	sprintCmd := jiracmd.NewSprintCmd()
	wrapGroup(sprintCmd)
	sprintCmd.GroupID = "main"
	root.AddCommand(sprintCmd)

	// add group — attach sub-objects to an issue.
	addCmd := jiracmd.NewAddCmd()
	wrapGroup(addCmd)
	addCmd.GroupID = "main"
	root.AddCommand(addCmd)

	// delete group — delete an issue or sub-object. Aliased as "rm".
	deleteCmd := jiracmd.NewDeleteCmd()
	wrapGroup(deleteCmd)
	deleteCmd.GroupID = "main"
	root.AddCommand(deleteCmd)

	// open — browser launcher.
	openCmd := jiracmd.NewOpenCmd()
	WrapWithPresenter(openCmd, stdout, stderr)
	openCmd.GroupID = "main"
	root.AddCommand(openCmd)

	// ── util group (registration order = display order) ──────────────────────

	// auth group (includes `me`).
	authCmd := jiracmd.NewAuthCmd(stdout)
	wrapGroup(authCmd)
	authCmd.GroupID = "util"
	root.AddCommand(authCmd)

	// setup: no WrapWithPresenter — wizard writes directly to stdout.
	setupCmd := jiracmd.NewSetupCmd(stdout, skillMD)
	setupCmd.GroupID = "util"
	root.AddCommand(setupCmd)

	// lookup group.
	lookupCmd := jiracmd.NewLookupCmd()
	wrapGroup(lookupCmd)
	lookupCmd.GroupID = "util"
	root.AddCommand(lookupCmd)

	// cache group.
	cacheCmd := jiracmd.NewCacheCmd()
	wrapGroup(cacheCmd)
	cacheCmd.GroupID = "util"
	root.AddCommand(cacheCmd)

	// config group.
	configCmd := jiracmd.NewConfigCmd(stdout)
	wrapGroup(configCmd)
	configCmd.GroupID = "util"
	root.AddCommand(configCmd)

	return root
}

// WrapWithPresenter wraps a *cobra.Command's RunE so its output passes through
// the Layer 2 presenter.
//
// The flow:
//  1. cmd.OutOrStdout() is redirected to an in-memory buffer.
//  2. RunE executes and writes raw output to that buffer.
//  3. On return, the buffer contents are passed to output.Format together with
//     elapsed time, exit code, and any error string.
//  4. The formatted result (including the [exit:N | Xms] footer) is written to
//     finalOut.
//
// JSON bypass: when the command has a --json flag and it is set, the buffer is
// written verbatim to finalOut without any formatting. The footer is suppressed
// because it would corrupt the NDJSON stream. Errors go to stderr only.
//
// Help bypass: Cobra's --help writes to cmd.OutOrStdout(). Since we redirect
// that to a buffer, help would be swallowed. We override HelpFunc to write
// directly to finalOut so --help always reaches the caller.
//
// Error path: when RunE returns a non-nil error, only the [stderr] error and
// the [exit:1] footer are emitted — runtime API errors (issue not found,
// forbidden, network) must NOT dump the command's help. The sole exception is a
// completely bare invocation (no positional args and no flags set), which is
// treated as a discovery attempt and answered with the full help. Arg-count and
// flag-parse errors are validated by Cobra before RunE runs and are handled in
// run() instead.
func WrapWithPresenter(c *cobra.Command, finalOut io.Writer, finalErr io.Writer) {
	original := c.RunE
	if original == nil {
		return
	}

	// buf is a pointer so both closures (HelpFunc and RunE) always reference
	// the same buffer for a given invocation, even though the buffer itself is
	// allocated fresh per RunE call below.
	var buf *bytes.Buffer

	defaultHelp := c.HelpFunc()
	c.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		cmd.SetOut(finalOut)
		defaultHelp(cmd, args)
		if buf != nil {
			cmd.SetOut(buf)
		}
	})

	c.RunE = func(cmd *cobra.Command, args []string) error {
		// Allocate a fresh buffer for every invocation — no cross-call bleed.
		buf = &bytes.Buffer{}
		cmd.SetOut(buf)

		start := time.Now()
		err := original(cmd, args)
		elapsed := time.Since(start)

		// Machine-output bypass: --json and --keys-only both produce output for
		// programs/pipes; the [exit:N | Xms] footer would corrupt both streams.
		jsonMode, _ := cmd.Flags().GetBool("json")
		keysOnly, _ := cmd.Flags().GetBool("keys-only")
		if jsonMode || keysOnly {
			if buf.Len() > 0 {
				fmt.Fprint(finalOut, buf.String())
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err.Error())
				return jiracmd.ErrAlreadyPresented
			}
			return nil
		}

		// Inner command already wrote its own output (e.g. streamed binary to stdout).
		// Skip the presenter entirely — no footer, no help, no error block.
		if errors.Is(err, jiracmd.ErrAlreadyPresented) {
			return nil
		}

		exitCode := 0
		stderrStr := ""
		if err != nil {
			exitCode = 1
			stderrStr = err.Error()
			// A completely bare invocation (no positional args, no flags set) is
			// a discovery attempt — emit the full help before the error. Genuine
			// runtime errors (args or flags present) show only the corrective
			// error, never the help dump.
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				cmd.HelpFunc()(cmd, args)
				fmt.Fprintln(finalOut)
			}
		}

		output.Print(finalOut, finalErr, output.Result{
			Stdout:   buf.String(),
			Stderr:   stderrStr,
			ExitCode: exitCode,
			Elapsed:  elapsed,
		})

		if err != nil {
			return jiracmd.ErrAlreadyPresented
		}
		return nil
	}
}
