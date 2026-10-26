package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

// The control plane's wire contract (docs/CONTROL.md, decisions #14-#18, #36, #62-#66). The
// container writes a Request, the root executor writes a Result and an AuditLine, and both sides
// import these types instead of defining their own. Everything here is standard library only.

// Size caps. The request file is one action; results and audit lines are a few hundred bytes.
const (
	// MaxRequestSize is the largest request file the executor reads (decision #62).
	MaxRequestSize = 4 << 10
	// MaxResultSize is the largest result file either side accepts; the output tail alone is 4 KiB.
	MaxResultSize = 8 << 10
	// MaxAuditLineSize is the largest audit line the container accepts before it calls the line
	// corrupt and skips it.
	MaxAuditLineSize = 8 << 10
	// MaxAckSize bounds the acknowledgement token. Every real one is under 20 characters.
	MaxAckSize = 64
)

// Semantics the executor and the container must agree on.
const (
	// MaxClockSkew is how far a request's timestamp may be ahead of the reader's clock. A host
	// whose clock is a few seconds fast should not have its requests refused.
	MaxClockSkew = 5 * time.Second
	// AuditBy is the only value an audit line's by field ever holds: there is no authentication,
	// so every action is attributable to the tailnet and nothing more (decision #10).
	AuditBy = "tailnet"
)

// Sentinel errors, one per untrusted document. Validation failures are *Refusal instead, because a
// refusal is data the UI renders; these mean the bytes were not the document at all.
var (
	ErrInvalidRequest   = errors.New("wire: invalid control request")
	ErrInvalidResult    = errors.New("wire: invalid control result")
	ErrInvalidAuditLine = errors.New("wire: invalid audit line")
)

// Action is one thing the page may ask of a target.
type Action string

// The closed action set. Nothing outside this table reaches a command builder.
const (
	ActionStart             Action = "start"
	ActionStop              Action = "stop"
	ActionRestart           Action = "restart"
	ActionUpdate            Action = "update"
	ActionReboot            Action = "reboot"
	ActionCancelAutorestore Action = "cancel-autorestore"
)

// actionRule is what the table knows about an action: the acknowledgement a person must have been
// shown, and (for a DNS target) the stronger one.
type actionRule struct {
	ack    string
	dnsAck string
}

// actionTable is the one place an action is defined. Validation, the acknowledgement rule, the
// config vocabulary and the documentation all read it (decision #46).
var actionTable = map[Action]actionRule{
	ActionStart:             {},
	ActionRestart:           {},
	ActionUpdate:            {},
	ActionReboot:            {ack: "reboot-confirmed"},
	ActionStop:              {ack: "stop-confirmed", dnsAck: "dns-stop-confirmed"},
	ActionCancelAutorestore: {},
}

// actionOrder is the table's display order, so docs and validation messages are stable.
var actionOrder = []Action{
	ActionStart, ActionStop, ActionRestart, ActionUpdate, ActionReboot, ActionCancelAutorestore,
}

// targetActionOrder is the subset a service's control block in config.yaml may offer: a browser
// never starts a reboot or cancels an auto-restore on behalf of a single service.
var targetActionOrder = []Action{ActionStart, ActionStop, ActionRestart, ActionUpdate}

// IsAction reports whether text is one of the six known actions.
func IsAction(action Action) bool {
	_, known := actionTable[action]
	return known
}

// IsTargetAction reports whether a service's control block may offer the action. A browser never
// starts a reboot or a cancel-autorestore for one service; those come from the control page.
func IsTargetAction(action Action) bool {
	for _, allowed := range targetActionOrder {
		if action == allowed {
			return true
		}
	}
	return false
}

// KnownActions returns every action in display order. The slice is a copy; callers may not change
// the table.
func KnownActions() []Action {
	return append([]Action(nil), actionOrder...)
}

// TargetActions returns the actions a service's control block may offer, in display order.
func TargetActions() []Action {
	return append([]Action(nil), targetActionOrder...)
}

// RequiredAck is the token a person must have been shown before this action may run. dns is true
// for a target that serves DNS, whose stop additionally needs the auto-restore warning. An unknown
// action requires nothing, because it never gets that far.
func RequiredAck(action Action, dns bool) string {
	rule, known := actionTable[action]
	if !known {
		return ""
	}
	if dns && rule.dnsAck != "" {
		return rule.dnsAck
	}
	return rule.ack
}

// Request is one action the container asks the host to perform. A request file is named after its
// id and is never trusted (decision #62).
type Request struct {
	ID          string    `json:"id"`
	Action      Action    `json:"action"`
	Target      string    `json:"target"`
	Ack         string    `json:"ack"`
	Ref         string    `json:"ref,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

// Result is the host's answer to a request, written on every exit path (decision #62). InProgress
// marks the provisional result a self-restart, self-update or reboot writes before it acts, so a
// container that dies in the middle still leaves a story (decision #64).
type Result struct {
	ID         string     `json:"id"`
	Action     Action     `json:"action"`
	Target     string     `json:"target"`
	OK         bool       `json:"ok"`
	Code       Code       `json:"code"`
	ExitCode   int        `json:"exit_code"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
	DurationMS int64      `json:"duration_ms"`
	Output     string     `json:"output"`
	Note       string     `json:"note"`
	RestoreAt  *time.Time `json:"restore_at,omitempty"`
	Ref        string     `json:"ref,omitempty"`
	InProgress bool       `json:"in_progress,omitempty"`
}

// AuditLine is one appended row of outbox/audit.jsonl: what was asked, what happened, and by whom.
type AuditLine struct {
	At         time.Time `json:"at"`
	ID         string    `json:"id"`
	Action     Action    `json:"action"`
	Target     string    `json:"target"`
	OK         bool      `json:"ok"`
	Code       Code      `json:"code"`
	DurationMS int64     `json:"duration_ms"`
	By         string    `json:"by"`
	Output     string    `json:"output"`
}

// Code is a closed-set reason an action was refused or failed. Codes are what the page turns into
// a short sentence; they never carry free text from the host.
type Code string

// The closed code table. bad_* mean the request was malformed, the rest say what the host decided.
const (
	CodeBadRequest       Code = "bad_request"
	CodeBadID            Code = "bad_id"
	CodeBadAction        Code = "bad_action"
	CodeBadTarget        Code = "bad_target"
	CodeUnknownTarget    Code = "unknown_target"
	CodeActionNotAllowed Code = "action_not_allowed"
	CodeMissingAck       Code = "missing_ack"
	CodeBadAck           Code = "bad_ack"
	CodeStale            Code = "stale"
	CodeReplay           Code = "replay"
	CodeCoalesced        Code = "coalesced"
	CodeQueueFull        Code = "queue_full"
	CodeTimeout          Code = "timeout"
	CodeExecFailed       Code = "exec_failed"
	CodeNotActiveAfter   Code = "not_active_after"
	CodeRestoreFailed    Code = "restore_failed"
	CodeBadRef           Code = "bad_ref"
	CodeInternal         Code = "internal"
	CodeUnavailable      Code = "unavailable"
)

// codeOrder is the table's display order.
var codeOrder = []Code{
	CodeBadRequest, CodeBadID, CodeBadAction, CodeBadTarget, CodeUnknownTarget,
	CodeActionNotAllowed, CodeMissingAck, CodeBadAck, CodeStale, CodeReplay, CodeCoalesced,
	CodeQueueFull, CodeTimeout, CodeExecFailed, CodeNotActiveAfter, CodeRestoreFailed,
	CodeBadRef, CodeInternal, CodeUnavailable,
}

// codeSentences is the user-facing sentence for each code. They follow the copy rules: second
// person, present tense, no exclamation marks (CLAUDE.md).
var codeSentences = map[Code]string{
	CodeBadRequest:       "The request was malformed.",
	CodeBadID:            "The request id is not valid.",
	CodeBadAction:        "That is not an action this box knows.",
	CodeBadTarget:        "That target name is not valid.",
	CodeUnknownTarget:    "That target is not in the allowlist.",
	CodeActionNotAllowed: "That target does not allow this action.",
	CodeMissingAck:       "This action needs a confirmation.",
	CodeBadAck:           "The confirmation does not match.",
	CodeStale:            "The request is too old to run.",
	CodeReplay:           "That request was already handled.",
	CodeCoalesced:        "The same action is already running.",
	CodeQueueFull:        "The queue is full. Wait for the pending action to finish.",
	CodeTimeout:          "The action took too long and was stopped.",
	CodeExecFailed:       "The command failed.",
	CodeNotActiveAfter:   "The target did not come back after the action.",
	CodeRestoreFailed:    "The stop was undone because the auto-restore could not be scheduled.",
	CodeBadRef:           "The auto-restore reference is not valid.",
	CodeInternal:         "Something went wrong inside the executor.",
	CodeUnavailable:      "The control plane is not installed on this box.",
}

// fallbackSentence is what the page shows for a code a newer host invented. Showing something
// honest beats showing nothing.
const fallbackSentence = "The action failed."

// IsCode reports whether text is one of the closed-set codes.
func IsCode(code Code) bool {
	_, known := codeSentences[code]
	return known
}

// KnownCodes returns every code in display order. The slice is a copy.
func KnownCodes() []Code {
	return append([]Code(nil), codeOrder...)
}

// Sentence is the short user-facing sentence for this code, or a fallback for an unknown one.
func (code Code) Sentence() string {
	if sentence, known := codeSentences[code]; known {
		return sentence
	}
	return fallbackSentence
}

// Refusal is a request the sender or the host refused, carrying the code the page renders. Both
// sides build refusals out of the same closed table, so the wording cannot drift.
type Refusal struct {
	Code Code
}

// Refuse builds a *Refusal for code.
func Refuse(code Code) *Refusal {
	return &Refusal{Code: code}
}

// Error describes the refusal, with its code, for a log line. It is never shown to the browser.
func (refusal *Refusal) Error() string {
	return "wire: refused: " + string(refusal.Code)
}

// ValidateRequest checks a request's structure: field grammar, id and ref formats, an acknowledgement
// token that could have come from this page, and a timestamp that is not from the future. Policy
// (is the target allowlisted, is the action allowed, is the ack right, is the request stale) belongs
// to the executor, which has the allowlist; this function deliberately has none of it.
func ValidateRequest(request Request, now time.Time) error {
	if _, err := ParseID(request.ID); err != nil {
		return Refuse(CodeBadID)
	}
	if !IsAction(request.Action) {
		return Refuse(CodeBadAction)
	}
	if !IsSlug(request.Target) {
		return Refuse(CodeBadTarget)
	}
	if !isAck(request.Ack) {
		return Refuse(CodeBadAck)
	}
	if request.Action == ActionCancelAutorestore {
		if _, err := ParseID(request.Ref); err != nil {
			return Refuse(CodeBadRef)
		}
	} else if request.Ref != "" {
		return Refuse(CodeBadRef)
	}
	if request.RequestedAt.IsZero() {
		return Refuse(CodeBadRequest)
	}
	if request.RequestedAt.After(now.Add(MaxClockSkew)) {
		return Refuse(CodeBadRequest)
	}
	return nil
}

// isAck reports whether text could be an acknowledgement token: short, printable ASCII, no control
// characters. Whether it is the *right* token is the executor's decision.
func isAck(text string) bool {
	if len(text) > MaxAckSize {
		return false
	}
	for index := 0; index < len(text); index++ {
		if character := text[index]; character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

// slugGrammar is what an id or target may look like. It is the same grammar everywhere a slug is
// checked, so a name that validates in config cannot be refused by the executor.
var slugGrammar = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// IsSlug reports whether text is a lowercase slug of at most 32 characters.
func IsSlug(text string) bool {
	return slugGrammar.MatchString(text)
}

// DecodeRequest reads one request file strictly: at most MaxRequestSize bytes, no unknown fields, no
// trailing data. It does not validate the request; a decoded request is still untrusted, so call
// ValidateRequest next.
func DecodeRequest(reader io.Reader) (Request, error) {
	var request Request
	if err := decodeStrict(reader, MaxRequestSize, &request); err != nil {
		return Request{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return request, nil
}

// DecodeResult reads one result file strictly. It requires a canonical id and timestamps, because a
// result without them cannot be matched to anything. Action and Code are not checked against the
// closed tables: a result from a newer host is still worth showing.
func DecodeResult(reader io.Reader) (Result, error) {
	var result Result
	if err := decodeStrict(reader, MaxResultSize, &result); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidResult, err)
	}
	if _, err := ParseID(result.ID); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidResult, err)
	}
	if result.StartedAt.IsZero() {
		return Result{}, fmt.Errorf("%w: no started_at", ErrInvalidResult)
	}
	if result.FinishedAt.IsZero() && !result.InProgress {
		return Result{}, fmt.Errorf("%w: no finished_at", ErrInvalidResult)
	}
	if result.DurationMS < 0 {
		return Result{}, fmt.Errorf("%w: negative duration_ms", ErrInvalidResult)
	}
	return result, nil
}

// DecodeAuditLine reads one line of audit.jsonl strictly. A caller scanning the file treats an
// error as a corrupt line and skips it, so one bad line cannot hide the rest.
func DecodeAuditLine(line []byte) (AuditLine, error) {
	var audit AuditLine
	if len(line) > MaxAuditLineSize {
		return AuditLine{}, fmt.Errorf("%w: %d bytes exceeds %d", ErrInvalidAuditLine, len(line), MaxAuditLineSize)
	}
	if err := decodeStrict(bytes.NewReader(line), MaxAuditLineSize, &audit); err != nil {
		return AuditLine{}, fmt.Errorf("%w: %w", ErrInvalidAuditLine, err)
	}
	if audit.At.IsZero() {
		return AuditLine{}, fmt.Errorf("%w: no at timestamp", ErrInvalidAuditLine)
	}
	if _, err := ParseID(audit.ID); err != nil {
		return AuditLine{}, fmt.Errorf("%w: %w", ErrInvalidAuditLine, err)
	}
	return audit, nil
}

// decodeStrict reads at most limit bytes and decodes exactly one JSON value from them. The limit is
// enforced before decoding so a huge document costs nothing.
func decodeStrict(reader io.Reader, limit int, target any) error {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return fmt.Errorf("%d bytes exceeds the %d byte cap", len(data), limit)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the value")
	}
	return nil
}
