// Package cli implements the meridian command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	clog "github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// Set at link time with -ldflags "-X github.com/autumnsgrove/meridian/internal/cli.version=...".
// An unstamped build is "dev" in every field.
var (
	version = "dev"
	commit  = "dev"
	date    = "dev"
)

const (
	// defaultConfigPath is where the container image puts the config. It is last in the order
	// because a flag or the environment should win over a baked-in path.
	defaultConfigPath = "/config/config.yaml"
	// configEnvVar names the environment variable that overrides the default path.
	configEnvVar = "MERIDIAN_CONFIG"
)

const usage = `usage: meridian <command> [flags]

commands:
  run          serve the dashboard until asked to stop
  healthcheck  ask the running dashboard whether it is alive
  version      print the build version
  help         print this help

run flags:
  --config PATH  configuration file (default $MERIDIAN_CONFIG, then /config/config.yaml)
  --watch        also reload when the file changes, not only on SIGHUP

healthcheck flags:
  --config PATH  configuration file, to learn which address to ask
`

// Version reports the identity of this build: name, version, commit, build date and platform.
func Version() string {
	return fmt.Sprintf("meridian %s (commit %s, built %s, %s/%s)",
		version, commit, date, runtime.GOOS, runtime.GOARCH)
}

// ConfigPath is the configuration file to load: the flag when it is set, then MERIDIAN_CONFIG, then
// the image's default path.
func ConfigPath(flagValue string, getenv func(string) string) string {
	if flagValue != "" {
		return flagValue
	}
	if fromEnv := getenv(configEnvVar); fromEnv != "" {
		return fromEnv
	}
	return defaultConfigPath
}

// usageError is the caller's mistake: Main answers with the usage text and exit code 2. It hides the
// sentinel from the printed message so the sentence reads as one instruction, not two.
type usageError struct{ message string }

func (err usageError) Error() string { return err.message }

func (err usageError) Is(target error) bool { return target == errUsage }

var errUsage = errors.New("usage")

// NewLogger builds the process logger: structured key/value lines at the level MERIDIAN_LOG names
// (debug, info, warn or error; anything unreadable means info, because a typo should not silence the
// log). It writes to w, which is stderr for the real process.
func NewLogger(w io.Writer, getenv func(string) string) *clog.Logger {
	logger := clog.NewWithOptions(w, clog.Options{ReportTimestamp: true})
	if level, err := clog.ParseLevel(getenv("MERIDIAN_LOG")); err == nil {
		logger.SetLevel(level)
	}
	return logger
}

// Main runs the meridian command line and returns the process exit code: 0 for success, 1 for a
// failure while doing the work, 2 for a request that does not make sense. It writes to the writers it
// is given so the whole CLI can be tested without a real process.
func Main(args []string, stdout, stderr io.Writer) int {
	root := newRoot(stdout, stderr)
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if !knownCommand(root, args[0]) {
		fmt.Fprintf(stderr, "meridian: unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	root.SetArgs(args)
	switch err := root.Execute(); {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "meridian: %v\n\n%s", err, usage)
		return 2
	default:
		fmt.Fprintf(stderr, "meridian: %v\n", err)
		return 1
	}
}

// knownCommand reports whether the first argument is a command or a flag. Flags go to cobra, which
// knows what they mean and how to complain about them.
func knownCommand(root *cobra.Command, arg string) bool {
	// cobra adds the help command while it runs, so it is not in Commands() yet, and a flag is
	// cobra's to interpret either way.
	if arg == "help" || strings.HasPrefix(arg, "-") {
		return true
	}
	for _, command := range root.Commands() {
		if command.Name() == arg || command.HasAlias(arg) {
			return true
		}
	}
	return false
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "meridian",
		Short:         "the homelab dashboard",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return usageError{"no command given"}
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetHelpFunc(func(command *cobra.Command, _ []string) {
		if command == root {
			fmt.Fprint(command.OutOrStdout(), usage)
			return
		}
		fmt.Fprintf(command.OutOrStdout(), "usage: meridian %s [flags]\n\n%s\n\nflags:\n%s",
			command.Name(), command.Short, command.Flags().FlagUsages())
	})
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err.Error()}
	})
	root.AddCommand(newRunCommand(), newHealthcheckCommand(), newVersionCommand())
	return root
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the build version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			fmt.Fprintln(command.OutOrStdout(), Version())
			return nil
		},
	}
}
