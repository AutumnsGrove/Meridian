package hostexec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// The self-update half of the host binary (docs/agent/07-update.md T35). It is the only code that
// pulls or recreates the Meridian container, and everything it believes comes from the root-owned
// allowlist or is re-validated after being read from the container's hint.

// MaxSignalSize is the largest update-signal file the host reads. A digest is 71 bytes; the cap
// leaves room for a mistake and none for a document.
const MaxSignalSize = 128

// The closed set of .env and signal failures. A caller can tell "this is not a digest" from "this
// file is a duplicate" without matching English.
var (
	ErrEnvKey       = errors.New("hostexec: invalid .env key")
	ErrEnvValue     = errors.New("hostexec: invalid .env value")
	ErrEnvDuplicate = errors.New("hostexec: duplicate .env key")
	ErrSignalFile   = errors.New("hostexec: update signal file")
)

// envKeyGrammar is the same shape config names environment variables with: an uppercase shell name.
var envKeyGrammar = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// SetVar returns contents with key set to value and every other line byte for byte unchanged. It
// keeps CRLF and the file's final-newline behaviour, replaces a quoted or spaced assignment in place,
// appends when the key is absent, and refuses a duplicate key, a bad key or a value that could inject
// a line. It is pure: the caller decides whether to write the result.
func SetVar(contents, key, value string) (string, error) {
	if !envKeyGrammar.MatchString(key) {
		return "", fmt.Errorf("%w: %q", ErrEnvKey, key)
	}
	if !validEnvValue(value) {
		return "", fmt.Errorf("%w: %q", ErrEnvValue, value)
	}

	lineEnding := "\n"
	if strings.Contains(contents, "\r\n") {
		lineEnding = "\r\n"
	}

	var out strings.Builder
	out.Grow(len(contents) + len(key) + len(value) + len(lineEnding))
	replaced := false
	for _, line := range envLines(contents) {
		body, ending := splitEnvLine(line)
		if envKeyOf(body) != key {
			out.WriteString(line)
			continue
		}
		if replaced {
			return "", fmt.Errorf("%w: %q", ErrEnvDuplicate, key)
		}
		out.WriteString(key + "=" + value + ending)
		replaced = true
	}
	if replaced {
		return out.String(), nil
	}

	if contents != "" && !strings.HasSuffix(contents, "\n") {
		out.WriteString(lineEnding)
		out.WriteString(key + "=" + value)
		return out.String(), nil
	}
	out.WriteString(key + "=" + value + lineEnding)
	return out.String(), nil
}

// ImageRef turns the untrusted signal contents into the one image reference the update may use: the
// allowlisted repository plus "@" plus a digest that passed the strict grammar. A tag, a different
// repository, whitespace, or anything longer than the cap is refused.
func ImageRef(repo wire.ImageRepo, contents []byte) (string, error) {
	if !repo.IsValid() {
		return "", fmt.Errorf("%w: repository %q is not ghcr.io/<owner>/<name>", wire.ErrInvalidDigest, repo)
	}
	if len(contents) > MaxSignalSize {
		return "", fmt.Errorf("%w: %d bytes exceeds %d", wire.ErrInvalidDigest, len(contents), MaxSignalSize)
	}
	digest, err := wire.ParseDigest(string(contents))
	if err != nil {
		return "", fmt.Errorf("%w: the update signal: %w", wire.ErrInvalidDigest, err)
	}
	return repo.Reference(digest), nil
}

// ReadUpdateSignal reads the container's hint without following a symlink, opening a device, or
// buffering more than the cap. The bytes are still untrusted; ImageRef is the validator.
func ReadUpdateSignal(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // path comes from the root-owned allowlist
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSignalFile, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSignalFile, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrSignalFile)
	}
	if info.Size() > MaxSignalSize {
		return nil, fmt.Errorf("%w: %d bytes exceeds %d", ErrSignalFile, info.Size(), MaxSignalSize)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxSignalSize+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSignalFile, err)
	}
	if len(data) > MaxSignalSize {
		return nil, fmt.Errorf("%w: grew past %d bytes while reading", ErrSignalFile, MaxSignalSize)
	}
	return data, nil
}

// validEnvValue accepts what this project ever writes: a digest-pinned reference with no whitespace,
// no control character and no empty value.
func validEnvValue(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] <= 0x20 || value[index] == 0x7f {
			return false
		}
	}
	return true
}

// envLines splits contents into lines that still carry their terminators. It never invents a
// trailing empty line for a file that ends with a newline.
func envLines(contents string) []string {
	lines := make([]string, 0, strings.Count(contents, "\n")+1)
	for contents != "" {
		index := strings.IndexByte(contents, '\n')
		if index < 0 {
			lines = append(lines, contents)
			break
		}
		lines = append(lines, contents[:index+1])
		contents = contents[index+1:]
	}
	return lines
}

// splitEnvLine separates a line's text from its "\n" or "\r\n".
func splitEnvLine(line string) (body, ending string) {
	body = strings.TrimSuffix(line, "\n")
	body = strings.TrimSuffix(body, "\r")
	return body, line[len(body):]
}

// envKeyOf is the key a line assigns, or "" for a comment, a blank line or a line without "=". It
// allows "KEY = value" so a hand-edited file is still found in place.
func envKeyOf(body string) string {
	trimmed := strings.TrimLeft(body, " \t")
	index := strings.IndexByte(trimmed, '=')
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[:index])
}
