package gateway

import (
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The status contract (decision #59). Every value the page shows arrives already formatted; the
// browser computes only ages from `at` and substitutes {q} into search templates. The shape is pinned
// by the golden files in testdata/api/, which the TypeScript client tests read too.

// StatusResponse is the whole of GET /api/status.
type StatusResponse struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Config      ConfigStatus    `json:"config"`
	Summary     Summary         `json:"summary"`
	Search      SearchStatus    `json:"search"`
	Control     ControlStatus   `json:"control"`
	Host        *HostStatus     `json:"host"`
	Sections    []SectionStatus `json:"sections"`
}

// ConfigStatus says whether the configuration being served was accepted, and when it loaded.
type ConfigStatus struct {
	OK       bool      `json:"ok"`
	LoadedAt time.Time `json:"loaded_at"`
	Error    string    `json:"error"`
}

// Summary counts the tiles: up includes degraded, total excludes disabled.
type Summary struct {
	Up    int `json:"up"`
	Total int `json:"total"`
}

// SearchStatus is the buttons above the tiles, in config order.
type SearchStatus struct {
	Buttons []SearchButton `json:"buttons"`
}

// SearchButton is one destination; {q} is replaced by the browser with the encoded query.
type SearchButton struct {
	Label       string `json:"label"`
	URLTemplate string `json:"url_template"`
}

// ControlStatus says whether this box has a control plane at all.
type ControlStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// HostStatus is the host snapshot's age, so the page can say how old the unit readings are.
type HostStatus struct {
	At    time.Time `json:"at"`
	AgeS  int64     `json:"age_s"`
	Stale bool      `json:"stale"`
}

// SectionStatus is one declared group of tiles, with its services in config order.
type SectionStatus struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Services []ServiceStatus `json:"services"`
}

// ServiceStatus is one tile.
type ServiceStatus struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Icon        IconStatus      `json:"icon"`
	URL         string          `json:"url"`
	Status      probe.Status    `json:"status"`
	Reason      string          `json:"reason"`
	CheckedAt   time.Time       `json:"checked_at"`
	Vitals      []VitalStatus   `json:"vitals"`
	Control     *ServiceControl `json:"control"`
}

// IconStatus is a lucide name or a served image path, never both.
type IconStatus struct {
	Lucide string `json:"lucide,omitempty"`
	Image  string `json:"image,omitempty"`
}

// VitalStatus is one already-formatted reading and how old it is.
type VitalStatus struct {
	Label   string    `json:"label"`
	Value   string    `json:"value"`
	At      time.Time `json:"at"`
	AgeS    int64     `json:"age_s"`
	Failing bool      `json:"failing"`
}

// ServiceControl is what the control page may offer for a tile, absent on a read-only one.
type ServiceControl struct {
	Target               string   `json:"target"`
	Actions              []string `json:"actions"`
	UpdateDisabledReason string   `json:"update_disabled_reason"`
	DNS                  bool     `json:"dns"`
}

// StatusInputs is everything BuildStatus needs besides the config, the states and the host snapshot.
type StatusInputs struct {
	// Load is how the last configuration load went.
	Load LoadStatus
	// ControlAvailable reports whether the control plane exists on this box.
	ControlAvailable bool
	// ControlReason explains why it does not, when it does not.
	ControlReason string
}

// BuildStatus assembles the contract. It is pure: no clock, no network, no store, so every rule the
// page depends on is a table test. A nil config produces the "nothing loaded" answer rather than a
// panic, because the banner is the point of that state.
func BuildStatus(cfg *config.Config, states map[string]probe.State, host *wire.HostSnapshot, now time.Time, in StatusInputs) StatusResponse {
	response := StatusResponse{
		GeneratedAt: now.UTC(),
		Config: ConfigStatus{
			OK:       in.Load.Error == "",
			LoadedAt: in.Load.LoadedAt.UTC(),
			Error:    in.Load.Error,
		},
		Control:  ControlStatus{Available: in.ControlAvailable, Reason: in.ControlReason},
		Sections: []SectionStatus{},
	}
	if cfg == nil {
		return response
	}

	response.Search = SearchStatus{Buttons: searchButtons(cfg.Search.Buttons)}
	response.Host = hostStatus(cfg.HostStatus, host, now)

	for _, section := range cfg.Sections {
		services := make([]ServiceStatus, 0, len(cfg.Services))
		for index := range cfg.Services {
			service := cfg.Services[index]
			if service.Section != section.ID {
				continue
			}
			status := serviceStatus(service, states[service.ID], now)
			services = append(services, status)
			// A disabled service is configured off, so it is neither up nor part of the total.
			if status.Status != probe.StatusDisabled {
				response.Summary.Total++
			}
			if status.Status == probe.StatusOK || status.Status == probe.StatusDegraded {
				response.Summary.Up++
			}
		}
		if len(services) == 0 {
			// An empty section renders as nothing at all, never as a heading over blank space.
			continue
		}
		response.Sections = append(response.Sections, SectionStatus{ID: section.ID, Title: section.Title, Services: services})
	}
	return response
}

func searchButtons(buttons []config.SearchButton) []SearchButton {
	statuses := make([]SearchButton, 0, len(buttons))
	for _, button := range buttons {
		statuses = append(statuses, SearchButton{Label: button.Label, URLTemplate: button.URLTemplate})
	}
	return statuses
}

func hostStatus(settings *config.HostStatus, host *wire.HostSnapshot, now time.Time) *HostStatus {
	if settings == nil || host == nil {
		// Null means there is no host reading to show: either no host probe is configured, or it has
		// never written one.
		return nil
	}
	return &HostStatus{
		At:    host.At.UTC(),
		AgeS:  ageSeconds(now, host.At),
		Stale: !host.IsFresh(now, time.Duration(settings.MaxAge)),
	}
}

func serviceStatus(service config.Service, state probe.State, now time.Time) ServiceStatus {
	status := probe.Derive(state, !service.Disabled)
	return ServiceStatus{
		ID:          service.ID,
		Name:        service.Name,
		Description: service.Description,
		Icon:        iconStatus(service.Icon),
		URL:         service.URL,
		Status:      status,
		Reason:      reasonFor(status, state),
		CheckedAt:   state.CheckedAt.UTC(),
		Vitals:      vitalStatuses(state.Vitals, now),
		Control:     serviceControl(service.Control),
	}
}

// iconStatus serves a local image out of /icons/, which is where the gateway's icon route reads it.
func iconStatus(icon config.Icon) IconStatus {
	if icon.Image != "" {
		return IconStatus{Image: "/icons/" + strings.TrimPrefix(icon.Image, "icons/")}
	}
	return IconStatus{Lucide: icon.Lucide}
}

// reasonFor is the sentence the tile shows. A down or unknown tile explains its health code; a
// degraded one explains the vital that failed; everything else says nothing.
func reasonFor(status probe.Status, state probe.State) string {
	switch status {
	case probe.StatusDown, probe.StatusUnknown:
		return probe.Reason(state.Health.Code)
	case probe.StatusDegraded:
		for _, vital := range state.Vitals {
			if vital.Failing {
				return probe.Reason(vital.Code)
			}
		}
	}
	return ""
}

func vitalStatuses(vitals []probe.VitalState, now time.Time) []VitalStatus {
	statuses := make([]VitalStatus, 0, len(vitals))
	for _, vital := range vitals {
		statuses = append(statuses, VitalStatus{
			Label:   vital.Label,
			Value:   vital.Value,
			At:      vital.At.UTC(),
			AgeS:    ageSeconds(now, vital.At),
			Failing: vital.Failing,
		})
	}
	return statuses
}

func serviceControl(control *config.ServiceControl) *ServiceControl {
	if control == nil {
		return nil
	}
	actions := make([]string, 0, len(control.Actions))
	actions = append(actions, control.Actions...)
	return &ServiceControl{
		Target:               control.Target,
		Actions:              actions,
		UpdateDisabledReason: control.NoUpdateReason,
		DNS:                  control.DNS,
	}
}

// ageSeconds is how old a reading is, in whole seconds. A timestamp in the future is 0: the host's
// clock is wrong, but the page must not show a negative age.
func ageSeconds(now, at time.Time) int64 {
	age := now.Sub(at)
	if age < 0 {
		return 0
	}
	return int64(age / time.Second)
}
