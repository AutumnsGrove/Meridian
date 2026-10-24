package probe_test

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
)

func TestHealthRunners_MatchTheConfigKinds(t *testing.T) {
	runners := slices.Sorted(maps.Keys(probe.HealthRunners))
	kinds := slices.Sorted(maps.Keys(config.HealthKinds))
	if !slices.Equal(runners, kinds) {
		t.Errorf("HealthRunners = %q, config.HealthKinds = %q: every kind needs a runner and the other way round", runners, kinds)
	}
}

func TestVitalRunners_MatchTheConfigKinds(t *testing.T) {
	runners := slices.Sorted(maps.Keys(probe.VitalRunners))
	kinds := slices.Sorted(maps.Keys(config.VitalKinds))
	if !slices.Equal(runners, kinds) {
		t.Errorf("VitalRunners = %q, config.VitalKinds = %q: every kind needs a runner and the other way round", runners, kinds)
	}
}

// TestRepositoryConfigs_HaveARunnerForEveryProbe is the other half of the registry test: the kind
// tables match, and every probe in the configs this repo ships can actually be run.
func TestRepositoryConfigs_HaveARunnerForEveryProbe(t *testing.T) {
	paths := map[string]string{
		"template":       filepath.Join("..", "..", "config.yaml.example"),
		"worked example": filepath.Join("..", "..", "examples", "homelab.yaml"),
		"portable":       filepath.Join("..", "..", "testdata", "portable", "config.yaml"),
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			parsed, _, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load(%s): %v", path, err)
			}
			for _, service := range parsed.Services {
				if _, known := probe.HealthRunners[service.Probes.Health.Kind]; !known {
					t.Errorf("service %s: no health runner for kind %q", service.ID, service.Probes.Health.Kind)
				}
				for _, vital := range service.Probes.Vitals {
					if _, known := probe.VitalRunners[vital.Kind]; !known {
						t.Errorf("service %s: no vital runner for kind %q", service.ID, vital.Kind)
					}
				}
			}
		})
	}
}
