package gateway

import (
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxRequestBody caps a mutating request body. A control request is a few hundred bytes.
const MaxRequestBody = 8 << 10

// RecoverPanic answers a panicking handler with a 500 and no detail, so an internal failure never
// reaches the browser. The panic itself is logged with its stack: it is the only clue there is.
func RecoverPanic(logger *log.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = discardLogger()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, recovered, debug.Stack())
				writeError(w, http.StatusInternalServerError, "the server could not answer this request")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets the headers every response carries, including errors and 404s, and keeps the
// API out of every cache (STANDARDS section 4).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		if isAPIPath(r.URL.Path) {
			header.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// HostCheck refuses a request addressed to a name this page does not answer to. It is the
// DNS-rebinding guard of decision #54, not authentication: loopback and server.allowed_hosts are the
// whole rule, and there is no token, no cookie and no session behind it.
func HostCheck(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !HostAllowed(r.Host, allowed) {
			writeError(w, http.StatusMisdirectedRequest, "this page does not answer to that host")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HostAllowed reports whether a request carrying this Host header may be answered: a loopback name
// on any port, or an entry of server.allowed_hosts. An entry without a port matches any port, an
// entry with one must match exactly, and names are compared without case.
func HostAllowed(host string, allowed []string) bool {
	name, port, hasPort := splitHost(host)
	if name == "" {
		return false
	}
	if name == "localhost" || name == "127.0.0.1" || name == "::1" {
		return true
	}
	for _, entry := range allowed {
		entryName, entryPort, entryHasPort := splitHost(entry)
		if entryName != name {
			continue
		}
		if !entryHasPort || (hasPort && entryPort == port) {
			return true
		}
	}
	return false
}

// splitHost separates a Host header into its name and port. It reports an empty name for anything
// that is not a plausible host, so a malformed header matches nothing.
func splitHost(host string) (name, port string, hasPort bool) {
	if !utf8.ValidString(host) {
		// Lower casing maps every invalid byte to the same replacement character, so two different
		// malformed names would compare equal.
		return "", "", false
	}
	lowered := strings.ToLower(host)
	if strings.HasPrefix(lowered, "[") {
		end := strings.LastIndex(lowered, "]")
		if end < 0 {
			return "", "", false
		}
		rest := lowered[end+1:]
		switch {
		case rest == "":
			return lowered[1:end], "", false
		case strings.HasPrefix(rest, ":"):
			return lowered[1:end], rest[1:], true
		default:
			return "", "", false
		}
	}
	if strings.Count(lowered, ":") > 1 {
		// An IPv6 literal has to be bracketed in a Host header.
		return "", "", false
	}
	if index := strings.LastIndex(lowered, ":"); index >= 0 {
		return lowered[:index], lowered[index+1:], true
	}
	return lowered, "", false
}

// BrowserGuard refuses the requests a page on another origin, or a DNS-rebound host, could make: a
// body that is not JSON, a cross-site fetch, an origin outside the allowlist, or a body over the cap.
// It is not authentication (decision #10) and GET and HEAD requests pass through untouched, because
// reading is not a thing a hostile page can turn into an action.
func BrowserGuard(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			writeError(w, http.StatusForbidden, "a request from this page must be application/json")
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeError(w, http.StatusForbidden, "this page does not accept a request from another site")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !HostAllowed(parsed.Host, allowed) {
				writeError(w, http.StatusForbidden, "this page does not answer to that origin")
				return
			}
		}
		if r.ContentLength > MaxRequestBody {
			writeError(w, http.StatusRequestEntityTooLarge, "the request body is larger than 8 KiB")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBody)
		next.ServeHTTP(w, r)
	})
}

// AccessLog writes one line per request: method, path, status and duration. It never logs the query
// string, the body or a header (STANDARDS section 4).
func AccessLog(logger *log.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = discardLogger()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Printf("%s %s %d %s", r.Method, r.URL.Path, recorder.status, time.Since(started).Round(time.Millisecond))
	})
}

// statusRecorder remembers the status a handler wrote so the access log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the writer underneath, so a future streaming handler can
// still flush.
func (recorder *statusRecorder) Unwrap() http.ResponseWriter {
	return recorder.ResponseWriter
}
