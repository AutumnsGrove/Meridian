package invariants_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// composeVolumes is the complete list of paths the container may see (decision #57). Everything on it
// is read-only; anything else is a new decision, not a one-line edit.
var composeVolumes = []string{
	"config.yaml:/config/config.yaml:ro",
	"icons:/config/icons:ro",
	"status:/status:ro",
}

// composeService is only the fields the invariants look at. A field that is not here is not checked
// by this rule, which is why the forbidden-substring scan below exists as well.
type composeService struct {
	Image       string   `yaml:"image"`
	NetworkMode string   `yaml:"network_mode"`
	User        string   `yaml:"user"`
	ReadOnly    bool     `yaml:"read_only"`
	Tmpfs       []string `yaml:"tmpfs"`
	CapDrop     []string `yaml:"cap_drop"`
	SecurityOpt []string `yaml:"security_opt"`
	Restart     string   `yaml:"restart"`
	PidsLimit   int      `yaml:"pids_limit"`
	MemLimit    string   `yaml:"mem_limit"`
	EnvFile     []string `yaml:"env_file"`
	Volumes     []string `yaml:"volumes"`
	Healthcheck struct {
		Test []string `yaml:"test"`
	} `yaml:"healthcheck"`
}

// checkComposeInvariants is T23: the compose file is the container's entire privilege story, and the
// most expensive mistakes in this design are one-line edits to it, so they get a test.
func checkComposeInvariants(root string) error {
	const name = "deploy/docker-compose.yml"

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil // not built yet
	}
	if err != nil {
		return err
	}

	for _, forbidden := range []string{
		"docker.sock",
		"/etc/meridian",
		"privileged:",
		"ports:",
		"pid: host",
		"network_mode: bridge",
	} {
		if strings.Contains(string(data), forbidden) {
			return fmt.Errorf("%s contains %q; the container's privilege story is this file (decision #57)", name, forbidden)
		}
	}

	var document struct {
		Services map[string]*composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if len(document.Services) != 1 {
		return fmt.Errorf("%s: %d services, want exactly one", name, len(document.Services))
	}
	var service composeService
	for _, only := range document.Services {
		if only != nil {
			service = *only
		}
	}

	properties := []struct {
		holds  bool
		detail string
	}{
		{service.NetworkMode == "host", "network_mode must be host, or every probe URL means the container instead of the box (decision #26)"},
		{service.User == "${MERIDIAN_UID}:${MERIDIAN_GID}", "user must be the deploy uid and gid from .env"},
		{service.ReadOnly, "read_only must be true"},
		{len(service.CapDrop) == 1 && strings.EqualFold(service.CapDrop[0], "ALL"), "cap_drop must drop ALL capabilities"},
		{slices.Contains(service.SecurityOpt, "no-new-privileges:true"), "security_opt must set no-new-privileges"},
		{service.Restart == "unless-stopped", "restart must be unless-stopped"},
		{service.PidsLimit > 0, "pids_limit must be set"},
		{service.MemLimit != "", "mem_limit must be set"},
		{service.Image == "${MERIDIAN_IMAGE}", "image must be ${MERIDIAN_IMAGE} so Phase 4 can pin a digest in .env"},
		{slices.Contains(service.EnvFile, ".env"), "env_file must name .env"},
		{slices.Contains(service.Tmpfs, "/tmp"), "tmpfs must provide /tmp, the only writable path"},
		{slices.Equal(service.Healthcheck.Test, []string{"CMD", "/meridian", "healthcheck"}), "the healthcheck must be the binary's own healthcheck"},
	}
	for _, property := range properties {
		if !property.holds {
			return fmt.Errorf("%s: %s", name, property.detail)
		}
	}

	allowed := make(map[string]bool, len(composeVolumes))
	for _, volume := range composeVolumes {
		allowed[volume] = true
	}
	mounted := make(map[string]bool, len(service.Volumes))
	for _, volume := range service.Volumes {
		trimmed := strings.TrimPrefix(volume, "./")
		if !allowed[trimmed] {
			return fmt.Errorf("%s: volume %q is not on the allowlist %v", name, volume, composeVolumes)
		}
		mounted[trimmed] = true
	}
	for _, volume := range composeVolumes {
		if !mounted[volume] {
			return fmt.Errorf("%s: volume %q is missing", name, volume)
		}
	}
	return nil
}
