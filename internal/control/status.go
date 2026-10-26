package control

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// StatusView is GET /api/control/status: the executor's health, what is queued, what came back, the
// audit history and any auto-restore still counting down. Times arrive raw so the browser can age
// and count down from them (decision #76); sentences arrive from the wire's closed code table
// (decision #59).
type StatusView struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Executor    ExecutorView  `json:"executor"`
	Pending     []PendingView `json:"pending"`
	Results     []ResultView  `json:"results"`
	Audit       []AuditView   `json:"audit"`
	Autorestore []RestoreView `json:"autorestore"`
}

// ExecutorView says whether the host side is alive. State is one of ok, missing or unknown.
type ExecutorView struct {
	State string `json:"state"`
	// Since is when the trigger unit entered its state, when the host snapshot knows.
	Since time.Time `json:"since,omitzero"`
}

// PendingView is one request in the queue: sent, not yet answered.
type PendingView struct {
	ID          string      `json:"id"`
	Action      wire.Action `json:"action"`
	Target      string      `json:"target"`
	RequestedAt time.Time   `json:"requested_at"`
	AgeS        int64       `json:"age_s"`
	// Unclaimed is true when nothing has picked the request up, which is what a dead executor
	// looks like from here.
	Unclaimed bool `json:"unclaimed"`
}

// ResultView is one result file, with the sentence its code renders. A successful result has an
// empty code and so an empty sentence.
type ResultView struct {
	wire.Result
	Sentence string `json:"sentence"`
}

// AuditView is one audit line, with the sentence its code renders.
type AuditView struct {
	wire.AuditLine
	Sentence string `json:"sentence"`
}

// RestoreView is a DNS stop whose auto-restore is still ahead. Ref is the stop request's id, which is
// what a cancel names.
type RestoreView struct {
	Ref        string    `json:"ref"`
	Target     string    `json:"target"`
	RestoreAt  time.Time `json:"restore_at"`
	RemainingS int64     `json:"remaining_s"`
}

// Status reads the inbox and outbox as they are. It never fails: a corrupt file is skipped, because
// one bad line must not hide the audit log or the results around it. A missing plane yields empty
// lists and an unknown executor.
func (plane Plane) Status(ctx context.Context) StatusView {
	now := plane.now()
	view := StatusView{
		GeneratedAt: now,
		Executor:    ExecutorView{State: ExecutorUnknown},
		Pending:     []PendingView{},
		Results:     []ResultView{},
		Audit:       []AuditView{},
		Autorestore: []RestoreView{},
	}
	cfg := plane.config()
	if cfg == nil {
		return view
	}

	queueDir := filepath.Join(cfg.Control.InboxDir, "queue")
	if names, err := queuedNames(queueDir); err == nil {
		view.Pending = pendingViews(decodeQueued(queueDir, names), now)
	}

	results := readResults(ctx, filepath.Join(cfg.Control.OutboxDir, "results"))
	view.Results = resultViews(results)
	view.Audit = auditViews(filepath.Join(cfg.Control.OutboxDir, "audit.jsonl"), cfg, now)
	view.Autorestore = restoreViews(results, now)
	view.Executor = plane.executorView(cfg, view.Pending)
	return view
}

// executorView derives the host side's health from data already on disk: the trigger unit in the host
// snapshot, and whether anything is picking requests up. No spinner outlives the queue.
func (plane Plane) executorView(cfg *config.Config, pending []PendingView) ExecutorView {
	view := ExecutorView{State: ExecutorUnknown}
	if plane.Host != nil && cfg.HostStatus != nil {
		snapshot, ok := plane.Host()
		if ok && snapshot.IsFresh(plane.now(), time.Duration(cfg.HostStatus.MaxAge)) {
			if unit, present := snapshot.Units[ExecutorUnit]; present {
				view.Since = unit.Since
				if unit.Active {
					view.State = ExecutorOK
				} else {
					view.State = ExecutorMissing
				}
			}
		}
	}
	// A request nothing claimed is proof, whatever the snapshot says.
	for _, request := range pending {
		if request.Unclaimed {
			view.State = ExecutorMissing
			break
		}
	}
	return view
}

// pendingViews is the queue, oldest first, with the age a person thinks in.
func pendingViews(requests []wire.Request, now time.Time) []PendingView {
	views := make([]PendingView, 0, len(requests))
	for _, request := range requests {
		age := now.Sub(request.RequestedAt)
		if age < 0 {
			age = 0 // a clock a few seconds fast reads as brand new
		}
		views = append(views, PendingView{
			ID:          request.ID,
			Action:      request.Action,
			Target:      request.Target,
			RequestedAt: request.RequestedAt,
			AgeS:        int64(age.Seconds()),
			Unclaimed:   age > unclaimedAfter,
		})
	}
	return views
}

// resultViews lists the newest results, newest first.
func resultViews(results []wire.Result) []ResultView {
	limit := min(len(results), maxResults)
	views := make([]ResultView, 0, limit)
	for index := 0; index < limit; index++ {
		result := &results[index]
		views = append(views, ResultView{Result: *result, Sentence: sentenceFor(result.Code)})
	}
	return views
}

// auditViews reads the tail of the audit log and returns the newest lines inside the retention
// window. A line that does not decode is skipped, never fatal.
func auditViews(path string, cfg *config.Config, now time.Time) []AuditView {
	retention := time.Duration(cfg.Control.AuditRetention)
	lines, err := readTail(path, maxAuditBytes)
	if err != nil {
		return []AuditView{}
	}

	views := make([]AuditView, 0, len(lines))
	for _, line := range lines {
		audit, err := wire.DecodeAuditLine(line)
		if err != nil {
			continue
		}
		if retention > 0 && now.Sub(audit.At) > retention {
			continue
		}
		views = append(views, AuditView{AuditLine: audit, Sentence: sentenceFor(audit.Code)})
	}
	sort.SliceStable(views, func(left, right int) bool {
		return views[left].At.After(views[right].At)
	})
	if limit := cfg.Control.AuditLines; len(views) > limit {
		views = views[:limit]
	}
	return views
}

// restoreViews lists DNS stops whose auto-restore is still ahead and has not been cancelled or
// undone by a start, soonest first.
func restoreViews(results []wire.Result, now time.Time) []RestoreView {
	views := make([]RestoreView, 0, 1)
	for index := range results {
		stop := &results[index]
		if stop.Action != wire.ActionStop || !stop.OK || stop.RestoreAt == nil {
			continue
		}
		if !stop.RestoreAt.After(now) {
			continue
		}
		if superseded(results, stop) {
			continue
		}
		views = append(views, RestoreView{
			Ref:        stop.ID,
			Target:     stop.Target,
			RestoreAt:  *stop.RestoreAt,
			RemainingS: int64(stop.RestoreAt.Sub(now).Seconds()),
		})
	}
	sort.SliceStable(views, func(left, right int) bool {
		return views[left].RestoreAt.Before(views[right].RestoreAt)
	})
	return views
}

// superseded reports whether a stop's auto-restore was cancelled or pre-empted: a successful cancel
// that names it, or a successful start of the same target after the stop finished.
func superseded(results []wire.Result, stop *wire.Result) bool {
	for index := range results {
		later := &results[index]
		if later.ID == stop.ID || !later.OK {
			continue
		}
		switch later.Action {
		case wire.ActionCancelAutorestore:
			if later.Ref == stop.ID {
				return true
			}
		case wire.ActionStart:
			if later.Target == stop.Target && later.StartedAt.After(stop.FinishedAt) {
				return true
			}
		}
	}
	return false
}

// readResults decodes the newest result files, newest first. Only the newest maxResultsScanned are
// read: the view lists 20, but an auto-restore's stop can be older than that.
func readResults(ctx context.Context, dir string) []wire.Result {
	names, err := queuedNames(dir)
	if err != nil {
		return nil
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) > maxResultsScanned {
		names = names[:maxResultsScanned]
	}

	results := make([]wire.Result, 0, len(names))
	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		result, err := wire.DecodeResult(file)
		_ = file.Close()
		if err != nil {
			continue
		}
		results = append(results, result)
	}
	return results
}

// readTail returns the lines in the last limit bytes of a file. When the file is longer, the partial
// first line is dropped: it is not a line, and decoding it would only produce a skipped entry.
func readTail(path string, limit int64) ([][]byte, error) {
	file, err := os.Open(path) //nolint:gosec // a path from the operator's config, inside the mounted outbox
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	var truncated bool
	if info.Size() > limit {
		if _, err := file.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return nil, err
		}
		truncated = true
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, err
	}
	if truncated {
		if end := bytes.IndexByte(data, '\n'); end >= 0 {
			data = data[end+1:]
		}
	}

	raw := bytes.Split(data, []byte{'\n'})
	lines := make([][]byte, 0, len(raw))
	for _, line := range raw {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// sentenceFor is what the page shows for a code. An empty code means nothing was refused, so there is
// nothing to say; an unknown one still gets an honest sentence rather than a blank row.
func sentenceFor(code wire.Code) string {
	if code == "" {
		return ""
	}
	return code.Sentence()
}
