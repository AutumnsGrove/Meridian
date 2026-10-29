package gateway

import (
	"context"
	"net/http"
	"time"
)

// The self-update report. It is read only: tapping update is the ordinary control request for
// the Meridian target, so this route can never pull, recreate or write anything.

// updateResolveTimeout bounds one check. A GET cannot hold a phone's connection open for the four
// minutes a tap may wait out an in-flight build; the tap path gets the longer budget.
const updateResolveTimeout = 10 * time.Second

// UpdateReport is GET /api/update. Configured is false on a box with no update block, which is every
// deployment that never opted into self-update; the rest is present exactly when a check ran.
type UpdateReport struct {
	Configured bool       `json:"configured"`
	Version    string     `json:"version"`
	Digest     string     `json:"digest"`
	Commit     string     `json:"commit"`
	Available  bool       `json:"available"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// updateStatus reports the running version, the newest digest and whether it differs. A failed check
// is data, not an HTTP failure: the page can say "could not check" and keep working.
func updateStatus(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		report := UpdateReport{Version: deps.Version}
		cfg, _ := deps.current()
		if cfg == nil || cfg.Update == nil || deps.Update == nil {
			writeJSON(w, http.StatusOK, report)
			return
		}
		report.Configured = true

		ctx, cancel := context.WithTimeout(r.Context(), updateResolveTimeout)
		defer cancel()
		resolution, err := deps.Update(ctx, cfg.Update)
		if err != nil {
			// The underlying error names internals and a registry status; neither belongs on a page.
			report.Error = "The newest image could not be checked."
			writeJSON(w, http.StatusOK, report)
			return
		}
		checkedAt := resolution.CheckedAt.UTC()
		report.Digest = resolution.Digest.String()
		report.Commit = resolution.Commit
		report.Available = resolution.Available
		report.CheckedAt = &checkedAt
		writeJSON(w, http.StatusOK, report)
	})
}
