package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/autumnsgrove/meridian/internal/config"
)

// healthcheckTimeout bounds the whole check: Docker runs it every few seconds and a hanging probe is
// worse than a failed one.
const healthcheckTimeout = 2 * time.Second

// Healthcheck asks the running dashboard on the address the config listens on whether it is alive.
// It is 0 only for a 200 answer that says "status":"ok"; the distroless image has no curl, so this is
// what the container healthcheck runs.
func Healthcheck(ctx context.Context, configPath string) error {
	cfg, _, err := config.Load(configPath)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+checkAddress(cfg.Listen)+"/healthz", http.NoBody)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	client := &http.Client{Timeout: healthcheckTimeout}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s answered %s", cfg.Listen, response.Status)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&body); err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	if body.Status != "ok" {
		return fmt.Errorf("healthcheck: status %q", body.Status)
	}
	return nil
}

// checkAddress turns the configured listen address into one a client can dial: "0.0.0.0:8891" and
// "[::]:8891" mean "every interface", so the check asks loopback instead of an unspecified host.
func checkAddress(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), port)
}

func newHealthcheckCommand() *cobra.Command {
	var configPath string
	command := &cobra.Command{
		Use:   "healthcheck",
		Short: "ask the running dashboard whether it is alive",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), healthcheckTimeout)
			defer cancel()
			if err := Healthcheck(ctx, ConfigPath(configPath, os.Getenv)); err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), "ok")
			return nil
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file, to learn which address to ask")
	return command
}
