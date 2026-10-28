package probe

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/autumnsgrove/meridian/internal/config"
)

// Request is one outbound GET or HEAD: the URL from the config, verbatim, the credential to attach,
// and any literal headers. A probe uses only the URL and Auth; the updater adds a method and headers
// for the registry protocol, which is why this seam is one client rather than two (decision #20).
type Request struct {
	URL     string
	Method  string
	Auth    config.Auth
	Headers []Header
}

// Header is one literal header a caller such as the updater needs. A probe gets its headers from
// config.Auth, where the value comes from the environment at request time; these values are already
// in memory, so they are set after auth and validated the same way.
type Header struct {
	Name  string
	Value string
}

// Response is what a fetch produced: the status line, the headers and the body, capped at the
// fetcher's limit. A HEAD has no body and the registry digest lives in the headers.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Fetcher performs one GET or HEAD. It is the seam tests replace with a function literal, and the
// only way a probe or the updater reaches a network service.
type Fetcher func(ctx context.Context, request Request) (Response, error)

// The closed set of fetch failures. Everything a probe can say about a network call is one of these
// (decision #55), so the reason codes in reasons.go stay a closed table too.
var (
	// ErrTimeout means the call did not finish inside the context's deadline.
	ErrTimeout = errors.New("probe: timeout")
	// ErrRefused means the connection was actively refused.
	ErrRefused = errors.New("probe: connection refused")
	// ErrUnreachable means the call failed for any other network reason, including a DNS failure.
	ErrUnreachable = errors.New("probe: unreachable")
	// ErrBodyTooLarge means the response body exceeded the fetcher's limit.
	ErrBodyTooLarge = errors.New("probe: body too large")
	// ErrBadRequest means the request could not be built safely, so nothing was sent.
	ErrBadRequest = errors.New("probe: invalid request")
)

// ErrEnvMissing means the config named a credential variable that is unset or empty, so no request
// was sent. It carries variable names and never values.
type ErrEnvMissing struct {
	Names []string
}

// Error names the missing variables: a person fixing the box needs the names, never the values.
func (missing *ErrEnvMissing) Error() string {
	return "probe: environment variable " + strings.Join(missing.Names, ", ") + " is not set"
}

// NewHTTPFetcher returns the real Fetcher: one shared client, no redirects, no cookie jar, no proxy
// from the environment, and every body capped at limit bytes. getenv is read at request time and may
// be nil, which means every credential variable is missing.
func NewHTTPFetcher(getenv func(string) string, userAgent string, limit int64) Fetcher {
	if limit < 0 {
		limit = 0
	}
	client := &http.Client{
		Transport: newTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// A 302 to a login page is a valid answer for http_status, and following one could
			// hand the configured credentials to another host (decision #55).
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context, request Request) (Response, error) {
		built, err := BuildRequest(ctx, request, getenv)
		if err != nil {
			return Response{}, err
		}
		if userAgent != "" {
			built.Header.Set("User-Agent", userAgent)
		}

		reply, err := client.Do(built)
		if err != nil {
			return Response{}, classify(err)
		}
		defer func() { _ = reply.Body.Close() }()

		body, err := io.ReadAll(io.LimitReader(reply.Body, limit+1))
		if err != nil {
			return Response{}, classify(err)
		}
		if int64(len(body)) > limit {
			return Response{Status: reply.StatusCode, Header: reply.Header.Clone()},
				fmt.Errorf("%w: response body exceeds %d bytes", ErrBodyTooLarge, limit)
		}
		return Response{Status: reply.StatusCode, Header: reply.Header.Clone(), Body: body}, nil
	}
}

// BuildRequest turns a probe request into the GET to send: the config's URL untouched, auth headers
// read from getenv now. It is the last check before the network, so a URL that is not absolute
// http/https, credentials in the URL, or a header name or value that could inject a second header
// is ErrBadRequest. Errors never repeat the URL, which may carry a token in its query.
func BuildRequest(ctx context.Context, request Request, getenv func(string) string) (*http.Request, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%w: not an absolute http or https URL", ErrBadRequest)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%w: the URL carries credentials", ErrBadRequest)
	}

	method := request.Method
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, fmt.Errorf("%w: method %q is not GET or HEAD", ErrBadRequest, method)
	}

	built, err := http.NewRequestWithContext(ctx, method, request.URL, http.NoBody)
	if err != nil {
		// The underlying error repeats the URL, so it is deliberately not wrapped.
		return nil, fmt.Errorf("%w: the URL could not become a request", ErrBadRequest)
	}
	// The check runs twice on purpose: NewRequest drops an empty port, so "http://:" has a host
	// when parsed and none afterwards, and a request with no host must never be sent.
	if built.URL.Host == "" || built.URL.Scheme != parsed.Scheme {
		return nil, fmt.Errorf("%w: not an absolute http or https URL", ErrBadRequest)
	}

	headers, err := authHeaders(request.Auth, getenv)
	if err != nil {
		return nil, err
	}
	for _, header := range headers {
		if !isToken(header.name) {
			return nil, fmt.Errorf("%w: %q is not a header name", ErrBadRequest, header.name)
		}
		if hasControlBytes(header.value) {
			return nil, fmt.Errorf("%w: the value of header %q has a control character", ErrBadRequest, header.name)
		}
		built.Header.Set(header.name, header.value)
	}
	for _, header := range request.Headers {
		if !isToken(header.Name) {
			return nil, fmt.Errorf("%w: %q is not a header name", ErrBadRequest, header.Name)
		}
		if hasControlBytes(header.Value) {
			return nil, fmt.Errorf("%w: the value of header %q has a control character", ErrBadRequest, header.Name)
		}
		built.Header.Set(header.Name, header.Value)
	}
	return built, nil
}

// Memo wraps a Fetcher so that identical requests within one refresh cycle cost one call: a health
// probe and two vitals on the same endpoint fetch once. Create a new Memo per cycle; never share one
// across cycles. Failures are remembered too, because a cycle should not retry a broken endpoint.
func Memo(fetch Fetcher) Fetcher {
	var (
		mutex   sync.Mutex
		entries = make(map[string]*memoEntry)
	)
	return func(ctx context.Context, request Request) (Response, error) {
		key := requestKey(request)

		mutex.Lock()
		if entry, found := entries[key]; found {
			mutex.Unlock()
			select {
			case <-entry.done:
				return entry.response, entry.err
			case <-ctx.Done():
				return Response{}, classify(ctx.Err())
			}
		}
		entry := &memoEntry{done: make(chan struct{})}
		entries[key] = entry
		mutex.Unlock()

		entry.response, entry.err = fetch(ctx, request)
		close(entry.done)
		return entry.response, entry.err
	}
}

// memoEntry is one remembered fetch. done is closed after the two fields are written, so a waiter
// reads them safely.
type memoEntry struct {
	response Response
	err      error
	done     chan struct{}
}

// header is one header an auth block contributes.
type header struct {
	name  string
	value string
}

// authHeaders reads the credential the config names, at request time, from getenv. A variable that
// is unset or empty is ErrEnvMissing with the names, and nothing is sent.
func authHeaders(auth config.Auth, getenv func(string) string) ([]header, error) {
	switch {
	case len(auth.BasicEnv) > 0:
		if len(auth.BasicEnv) != 2 {
			return nil, fmt.Errorf("%w: basic_env needs two variable names", ErrBadRequest)
		}
		if missing := unset(auth.BasicEnv, getenv); len(missing) > 0 {
			return nil, &ErrEnvMissing{Names: missing}
		}
		user, password := getenv(auth.BasicEnv[0]), getenv(auth.BasicEnv[1])
		pair := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
		return []header{{name: "Authorization", value: "Basic " + pair}}, nil

	case auth.BearerEnv != "":
		token := getenv(auth.BearerEnv)
		if token == "" {
			return nil, &ErrEnvMissing{Names: []string{auth.BearerEnv}}
		}
		return []header{{name: "Authorization", value: "Bearer " + token}}, nil

	default:
		names := make([]string, 0, len(auth.Header))
		for name := range auth.Header {
			names = append(names, name)
		}
		sort.Strings(names)
		headers := make([]header, 0, len(names))
		for _, name := range names {
			headers = append(headers, header{name: name, value: auth.Header[name]})
		}
		return headers, nil
	}
}

// unset lists which of names produce an empty value, in the order the config gave them.
func unset(names []string, getenv func(string) string) []string {
	var missing []string
	for _, name := range names {
		if getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// requestKey identifies a request by URL and credential names. Length prefixes keep a URL that
// contains a separator from colliding with a different request.
func requestKey(request Request) string {
	var key strings.Builder
	writeField := func(value string) {
		key.WriteString(strconv.Itoa(len(value)))
		key.WriteByte(':')
		key.WriteString(value)
	}
	writeField(request.Method)
	writeField(request.URL)
	for _, header := range request.Headers {
		writeField("literal:" + header.Name)
		writeField(header.Value)
	}
	for _, name := range request.Auth.BasicEnv {
		writeField("basic:" + name)
	}
	writeField("bearer:" + request.Auth.BearerEnv)
	names := make([]string, 0, len(request.Auth.Header))
	for name := range request.Auth.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeField("header:" + name)
		writeField(request.Auth.Header[name])
	}
	return key.String()
}

// newTransport builds the one transport the fetcher shares: no environment proxy (the container is
// on the host's network and must not tunnel probes), bounded idle connections, no compression
// surprises, and no dial timeout of its own because the request context carries the deadline.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// classify maps a transport or body error onto the closed set. Everything unrecognised is
// ErrUnreachable rather than a leaked, unbounded message.
func classify(err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	case errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("%w: %w", ErrRefused, err)
	default:
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
}

// isToken reports whether value is a non-empty RFC 7230 token, which is what a header field name
// must be. textproto.CanonicalMIMEHeaderKey returns invalid names unchanged, so it cannot be used as
// the check.
func isToken(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		character := value[i]
		isAlphaNumeric := 'a' <= character && character <= 'z' ||
			'A' <= character && character <= 'Z' ||
			'0' <= character && character <= '9'
		if !isAlphaNumeric && !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			return false
		}
	}
	return true
}

// hasControlBytes reports whether value holds anything a header field may not: control characters
// other than a tab, or DEL.
func hasControlBytes(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 && value[i] != '\t' || value[i] == 0x7f {
			return true
		}
	}
	return false
}
