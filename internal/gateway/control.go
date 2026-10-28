package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
	"github.com/autumnsgrove/meridian/internal/control"
	"github.com/autumnsgrove/meridian/internal/updater"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The control routes (T26). One mutating route exists in the whole gateway, it goes through the
// browser guard, and it can only express a choice among what config.yaml already declares: no id,
// no timestamp, no command and no file name come from the browser (decisions #14, #62).

// controlRequest is the body POST /api/control accepts. The fields are strings so a malformed action
// reaches wire.ValidateRequest and comes back as a code, not as a decode error.
type controlRequest struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Ack    string `json:"ack"`
	Ref    string `json:"ref,omitempty"`
}

// enqueue records one tap. It answers 202 immediately with the request id: the browser polls the
// status route for the result, so a slow action never holds a connection open.
func enqueue(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope, err := decodeControlRequest(r)
		if err != nil {
			writeCode(w, http.StatusBadRequest, wire.CodeBadRequest)
			return
		}
		if deps.Control == nil {
			writeCode(w, http.StatusServiceUnavailable, wire.CodeUnavailable)
			return
		}

		// A self-update is the one action whose meaning depends on the container: the digest is
		// resolved here, written for the host to re-validate, and only then is the request queued.
		// Everything else is a plain tap.
		if cfg, _ := deps.current(); isSelfUpdate(cfg, envelope.Action, envelope.Target) {
			if !writeUpdateHint(w, r, deps, cfg) {
				return
			}
		}

		id, err := deps.Control.Enqueue(r.Context(), control.Input{
			Action: wire.Action(envelope.Action),
			Target: envelope.Target,
			Ack:    envelope.Ack,
			Ref:    envelope.Ref,
		})
		if err != nil {
			status, code := refusalStatus(err)
			writeCode(w, status, code)
			return
		}
		writeJSON(w, http.StatusAccepted, struct {
			ID string `json:"id"`
		}{ID: id})
	})
}

// controlStatus answers the control page's poll: the executor's health, the queue, recent results, the
// audit tail and any running auto-restore.
func controlStatus(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Control == nil {
			writeCode(w, http.StatusServiceUnavailable, wire.CodeUnavailable)
			return
		}
		if available, _ := deps.Control.Available(); !available {
			writeCode(w, http.StatusServiceUnavailable, wire.CodeUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, deps.Control.Status(r.Context()))
	})
}

// isSelfUpdate reports whether this tap is the image's own update, which is the one request that has
// to resolve a digest first. The target is config, never a name in code (decision #38).
func isSelfUpdate(cfg *config.Config, action, target string) bool {
	return cfg != nil && cfg.Update != nil && cfg.Update.Target != "" &&
		action == string(wire.ActionUpdate) && target == cfg.Update.Target
}

// writeUpdateHint resolves the newest digest and writes the hint the host re-validates. It answers
// the request itself on failure and reports whether the caller may queue the request. The write
// deadline is extended because this check may wait out an in-flight build, which the server's
// probe-sized timeout would otherwise cut off.
func writeUpdateHint(w http.ResponseWriter, r *http.Request, deps Deps, cfg *config.Config) bool {
	if deps.Update == nil {
		writeCode(w, http.StatusServiceUnavailable, wire.CodeUpdateFailed)
		return false
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(updater.MaxWait + time.Minute))

	resolution, err := deps.Update(r.Context(), cfg.Update)
	if err != nil {
		writeCode(w, http.StatusServiceUnavailable, wire.CodeUpdateFailed)
		return false
	}
	if err := updater.Request(cfg.Update.SignalDir, resolution.Digest); err != nil {
		writeCode(w, http.StatusServiceUnavailable, wire.CodeUpdateFailed)
		return false
	}
	return true
}

// decodeControlRequest reads the one body shape this route accepts: at most 8 KiB (also enforced by
// the browser guard), no unknown fields, no trailing data.
func decodeControlRequest(r *http.Request) (controlRequest, error) {
	decoder := json.NewDecoder(io.LimitReader(r.Body, MaxRequestBody+1))
	decoder.DisallowUnknownFields()
	var envelope controlRequest
	if err := decoder.Decode(&envelope); err != nil {
		return controlRequest{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return controlRequest{}, errors.New("trailing data after the request")
	}
	return envelope, nil
}

// refusalStatus maps a refusal code to the status the browser sees. A conflict means "nothing is
// wrong with what you asked, the queue is in the way"; 429 means wait; everything else is the
// request's own fault.
func refusalStatus(err error) (int, wire.Code) {
	var refusal *wire.Refusal
	if errors.As(err, &refusal) {
		switch refusal.Code {
		case wire.CodeCoalesced:
			return http.StatusConflict, refusal.Code
		case wire.CodeQueueFull:
			return http.StatusTooManyRequests, refusal.Code
		case wire.CodeUnavailable:
			return http.StatusServiceUnavailable, refusal.Code
		default:
			return http.StatusBadRequest, refusal.Code
		}
	}
	if errors.Is(err, control.ErrUnavailable) {
		return http.StatusServiceUnavailable, wire.CodeUnavailable
	}
	return http.StatusInternalServerError, wire.CodeInternal
}

// writeCode answers with the control error shape the page parses: the closed-set code and the
// sentence the wire table gives it, so a refusal reads the same as the result it would have become
// and the page keeps no second copy of the wording (decision #86). It never carries the underlying
// error.
func writeCode(w http.ResponseWriter, status int, code wire.Code) {
	writeJSON(w, status, struct {
		Code     wire.Code `json:"code"`
		Sentence string    `json:"sentence"`
	}{Code: code, Sentence: code.Sentence()})
}
