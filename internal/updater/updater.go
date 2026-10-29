// Package updater is the container side of self-update: it resolves the digest of the newest
// published image and leaves it as a hint for the host, and it never pulls, recreates or executes
// anything (decisions #20, #52, #64). The host re-validates every byte this package writes.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
	"github.com/autumnsgrove/meridian/internal/probe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// The bounds of one resolution. MaxWait has to clear the slowest multi-arch build this project sees
// plus a slow push, so a tap right after `git push` cannot grab the previous image and call it new.
const (
	MaxWait     = 4 * time.Minute
	DefaultHost = "https://ghcr.io"
	maxToken    = 4 << 10
)

// pollStart and pollMax bound the backoff while a build is still running.
const (
	pollStart = 2 * time.Second
	pollMax   = 15 * time.Second
)

// ErrRegistry means the registry or its protocol did not answer in a way this build understands.
// A malformed digest wraps this and wire.ErrInvalidDigest, so both checks work.
var ErrRegistry = errors.New("updater: registry")

// manifestAccept lists every index and manifest media type GHCR may serve for a multi-arch tag. The
// index is what carries the digest the box will pull, so it is asked for first.
const manifestAccept = "application/vnd.oci.image.index.v1+json," +
	"application/vnd.docker.distribution.manifest.list.v2+json," +
	"application/vnd.docker.distribution.manifest.v2+json"

// Source is where to look: the repository to resolve and the repository's own status endpoint.
// RegistryURL is a seam for tests, and empty means the real GHCR. Nothing here is hardcoded to a
// GitHub hostname (decision #38).
type Source struct {
	Repo        wire.ImageRepo
	RegistryURL string
	PublishURL  string
}

// Running is the build asking the question. Commit is the SHA this binary was built from; Digest is
// the image it is running, when the deployment exposes it. Either may be empty, which only makes the
// "is there an update" answer less certain, never wrong about the digest.
type Running struct {
	Commit string
	Digest wire.Digest
}

// Resolution is one answer: the newest digest, the commit the publish endpoint reported with it, and
// whether that digest differs from what is running.
type Resolution struct {
	Digest    wire.Digest
	Commit    string
	CheckedAt time.Time
	Available bool
}

// Clock is the time seam. Tests move Now and record Sleep instead of waiting four real minutes.
type Clock struct {
	Now   func() time.Time
	Sleep func(ctx context.Context, delay time.Duration) error
}

// Resolve asks the repository's status endpoint for the newest commit, waits out a build that is
// still running (or a tag that has not flipped yet), then reads the `:latest` manifest's digest from
// GHCR. A status endpoint that is rate limited, broken or simply absent never fails the resolution:
// the manifest is still authoritative, it just may be the previous build.
func Resolve(ctx context.Context, fetch probe.Fetcher, source Source, running Running, clock Clock) (Resolution, error) {
	if clock.Now == nil {
		clock.Now = time.Now
	}
	if clock.Sleep == nil {
		clock.Sleep = sleepContext
	}
	if source.RegistryURL == "" {
		source.RegistryURL = DefaultHost
	}
	if !source.Repo.IsValid() {
		return Resolution{}, fmt.Errorf("%w: repository %q is not ghcr.io/<owner>/<name>", ErrRegistry, source.Repo)
	}

	deadline := clock.Now().Add(MaxWait)
	interval := pollStart
	published := publishState{}

	// First wait for the build that this commit asked for: reporting the previous image as the newest
	// one is the failure this loop exists to prevent.
	for {
		state, err := readPublish(ctx, fetch, source.PublishURL)
		if err == nil {
			published = state
		}
		if err != nil || !published.InProgress || published.Commit == running.Commit {
			break
		}
		if !clock.Now().Before(deadline) {
			break
		}
		if err := backoff(ctx, clock, &interval, deadline); err != nil {
			return Resolution{}, err
		}
	}

	// Then resolve. A completed workflow can still be a moment ahead of the tag flip, so a digest
	// that has not moved while a newer commit exists is also worth waiting for, until the same cap.
	for {
		digest, err := readDigest(ctx, fetch, source)
		if err != nil {
			return Resolution{}, err
		}
		resolution := Resolution{
			Digest:    digest,
			Commit:    published.Commit,
			CheckedAt: clock.Now(),
			Available: available(digest, published.Commit, running),
		}
		if !waitingForATagFlip(resolution, published, running) {
			return resolution, nil
		}
		if !clock.Now().Before(deadline) {
			return resolution, nil
		}
		if err := backoff(ctx, clock, &interval, deadline); err != nil {
			return Resolution{}, err
		}
	}
}

// Request writes the one file the host side reads: update-signal/requested, containing only a
// validated digest. It is a hint, not a command. The host re-validates it against its own allowlist
// before anything is pulled.
func Request(dir string, digest wire.Digest) error {
	if !digest.IsValid() {
		return fmt.Errorf("%w: %q is not a digest", wire.ErrInvalidDigest, digest)
	}
	if err := atomicfile.Write(dir, "requested", []byte(digest.String()), 0o600); err != nil {
		return fmt.Errorf("updater: write the update signal: %w", err)
	}
	return nil
}

// ManifestDigest reads the digest GHCR puts on a manifest response. Exactly one header is accepted:
// two disagreeing values are a protocol problem, not a value to pick from.
func ManifestDigest(header http.Header) (wire.Digest, error) {
	values := header.Values("Docker-Content-Digest")
	if len(values) != 1 {
		return "", fmt.Errorf("%w: got %d Docker-Content-Digest headers, want exactly one",
			wire.ErrInvalidDigest, len(values))
	}
	return wire.ParseDigest(values[0])
}

// publishState is what the repository's status endpoint said: the newest commit and whether its
// build is still queued or running.
type publishState struct {
	Commit     string
	InProgress bool
}

// readPublish reads the status endpoint. Its response shape is the repository API's, but its host is
// config, so no GitHub knowledge is in the code beyond these two fields.
func readPublish(ctx context.Context, fetch probe.Fetcher, publishURL string) (publishState, error) {
	if publishURL == "" {
		return publishState{}, nil
	}
	response, err := fetch(ctx, probe.Request{URL: publishURL})
	if err != nil {
		return publishState{}, fmt.Errorf("updater: read the publish status: %w", err)
	}
	if response.Status != http.StatusOK {
		return publishState{}, fmt.Errorf("updater: publish status answered %d", response.Status)
	}
	var document struct {
		WorkflowRuns []struct {
			HeadSHA string `json:"head_sha"`
			Status  string `json:"status"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(response.Body, &document); err != nil {
		return publishState{}, fmt.Errorf("updater: decode the publish status: %w", err)
	}
	if len(document.WorkflowRuns) == 0 {
		return publishState{}, nil
	}
	run := document.WorkflowRuns[0]
	state := publishState{InProgress: run.Status == "queued" || run.Status == "in_progress"}
	if isCommitSHA(run.HeadSHA) {
		state.Commit = run.HeadSHA
	}
	return state, nil
}

// readDigest runs GHCR's anonymous two-step exchange: a pull token, then a manifest HEAD. The token
// is used for this one request and never logged or stored.
func readDigest(ctx context.Context, fetch probe.Fetcher, source Source) (wire.Digest, error) {
	base := strings.TrimSuffix(source.RegistryURL, "/")
	path := source.Repo.Path()

	tokenURL := base + "/token?service=ghcr.io&scope=repository:" + path + ":pull"
	response, err := fetch(ctx, probe.Request{URL: tokenURL})
	if err != nil {
		return "", fmt.Errorf("%w: token request: %w", ErrRegistry, err)
	}
	if response.Status != http.StatusOK {
		return "", fmt.Errorf("%w: token request answered %d", ErrRegistry, response.Status)
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body, &token); err != nil {
		return "", fmt.Errorf("%w: decode the token response: %w", ErrRegistry, err)
	}
	if token.Token == "" || len(token.Token) > maxToken {
		return "", fmt.Errorf("%w: the token response carried no usable token", ErrRegistry)
	}

	manifestURL := base + "/v2/" + path + "/manifests/latest"
	response, err = fetch(ctx, probe.Request{
		URL:    manifestURL,
		Method: http.MethodHead,
		Headers: []probe.Header{
			{Name: "Authorization", Value: "Bearer " + token.Token},
			{Name: "Accept", Value: manifestAccept},
		},
	})
	if err != nil {
		return "", fmt.Errorf("%w: manifest request: %w", ErrRegistry, err)
	}
	if response.Status != http.StatusOK {
		return "", fmt.Errorf("%w: manifest request answered %d", ErrRegistry, response.Status)
	}
	digest, err := ManifestDigest(response.Header)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRegistry, err)
	}
	return digest, nil
}

// available is whether the resolved digest is something other than what is running. The digest is
// authoritative when the deployment exposes it; the commit is the fallback.
func available(digest wire.Digest, newestCommit string, running Running) bool {
	if running.Digest != "" {
		return digest != running.Digest
	}
	if running.Commit != "" && newestCommit != "" {
		return newestCommit != running.Commit
	}
	return digest != ""
}

// waitingForATagFlip is the second wait: the build finished and there is a newer commit, but the tag
// still points at the running image. That is the moment between a completed push and GHCR serving it.
func waitingForATagFlip(resolution Resolution, published publishState, running Running) bool {
	return !resolution.Available &&
		running.Digest != "" &&
		published.Commit != "" &&
		running.Commit != "" &&
		published.Commit != running.Commit
}

// backoff sleeps one step, doubled for next time, capped at pollMax and never past the deadline.
func backoff(ctx context.Context, clock Clock, interval *time.Duration, deadline time.Time) error {
	remaining := deadline.Sub(clock.Now())
	if remaining <= 0 {
		return nil
	}
	delay := *interval
	if delay > remaining {
		delay = remaining
	}
	if err := clock.Sleep(ctx, delay); err != nil {
		return err
	}
	*interval *= 2
	if *interval > pollMax {
		*interval = pollMax
	}
	return nil
}

// sleepContext is the real Sleep: a timer that gives up when the request does.
func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isCommitSHA accepts the lowercase hexadecimal SHA a repository API reports, in either the SHA-1
// or the emerging SHA-256 length. Anything else is treated as "no commit known", which only skips
// the wait.
func isCommitSHA(text string) bool {
	if len(text) != 40 && len(text) != 64 {
		return false
	}
	for index := 0; index < len(text); index++ {
		character := text[index]
		isDigit := '0' <= character && character <= '9'
		isLowerHexLetter := 'a' <= character && character <= 'f'
		if !isDigit && !isLowerHexLetter {
			return false
		}
	}
	return true
}
