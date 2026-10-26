package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/doctor"
	"github.com/autumnsgrove/meridian/internal/probe"
)

// errProblemsFound ends the doctor command with exit code 1 after the report has already said what
// is wrong. Printing a second sentence on top of the report would only repeat it.
var errProblemsFound = fmt.Errorf("doctor found problems")

// Doctor builds the report for the config at configPath. A config that did not load becomes a report
// of its problems rather than an error, because explaining a broken config is the whole job. An
// allowlist path of "-" reads standard input, which is how the host's file reaches the container
// without mounting /etc/meridian (decision #15).
func Doctor(ctx context.Context, configPath, allowlistPath string, stdin io.Reader) doctor.Report {
	cfg, _, err := config.Load(configPath)
	if err != nil {
		return doctor.InvalidConfig(err)
	}

	var allow *doctor.Allowlist
	if allowlistPath != "" {
		parsed, readErr := readAllowlist(allowlistPath, stdin)
		if readErr != nil {
			return doctor.InvalidAllowlist(readErr)
		}
		allow = parsed
	}

	host := newHostReader()
	host.SetPath(hostPath(cfg))

	return doctor.Run(ctx, cfg, probe.Env{
		Fetch:   probe.NewHTTPFetcher(os.Getenv, probeUserAgent, maxProbeBody),
		Resolve: probe.NewDNSResolver(),
		Host:    host.Read,
		Now:     time.Now,
	}, allow)
}

// readAllowlist reads the allowlist doctor compares the config with, from a file or from stdin.
func readAllowlist(path string, stdin io.Reader) (*doctor.Allowlist, error) {
	if path == "-" {
		return doctor.ReadAllowlist(stdin)
	}
	file, err := os.Open(path) //nolint:gosec // the operator names the file on their own command line
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	return doctor.ReadAllowlist(file)
}

func newDoctorCommand() *cobra.Command {
	var configPath, allowlistPath string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "explain the config against the world, read only",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			report := Doctor(command.Context(), ConfigPath(configPath, os.Getenv), allowlistPath, command.InOrStdin())
			fmt.Fprint(command.OutOrStdout(), doctor.Render(report))
			if report.Failed() {
				return errProblemsFound
			}
			return nil
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file")
	command.Flags().StringVar(&allowlistPath, "allowlist", "", "targets.json from the host, or - for stdin")
	return command
}
