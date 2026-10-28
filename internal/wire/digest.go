package wire

import (
	"errors"
	"fmt"
	"strings"
)

// The self-update contract's two validated strings (decision #20). Both are shared with the host
// side, which re-validates everything the container writes, so they live here with the rest of the
// wire vocabulary and never in one binary only.

// ErrInvalidDigest means the text was not "sha256:" plus 64 lowercase hexadecimal characters.
var ErrInvalidDigest = errors.New("wire: invalid digest")

// ErrInvalidImageRepo means the text was not a GHCR repository in the form
// "ghcr.io/<owner>/<name>".
var ErrInvalidImageRepo = errors.New("wire: invalid image repository")

// MaxImageRepoSize is the distribution spec's cap on a repository name. A real one is well under it.
const MaxImageRepoSize = 255

// Digest is a content digest that ParseDigest has accepted. It renders exactly as it was parsed, so
// building an image reference from it cannot introduce a tag, a space or an escape.
type Digest string

// ParseDigest accepts only the strict content-addressed form this project pins: the sha256 algorithm
// and 64 lowercase hexadecimal characters. An uppercase or abbreviated digest, a tag, a whitespace
// character or any trailing byte is refused.
func ParseDigest(text string) (Digest, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(text, prefix) {
		return "", fmt.Errorf("%w: %q is not sha256:<64 hex>", ErrInvalidDigest, text)
	}
	hex := text[len(prefix):]
	if len(hex) != 64 {
		return "", fmt.Errorf("%w: %q has %d hexadecimal characters, want 64", ErrInvalidDigest, text, len(hex))
	}
	for index := 0; index < len(hex); index++ {
		if !isLowerHex(hex[index]) {
			return "", fmt.Errorf("%w: %q contains %q", ErrInvalidDigest, text, hex[index])
		}
	}
	return Digest(text), nil
}

// String returns the digest as it was parsed.
func (digest Digest) String() string {
	return string(digest)
}

// IsValid reports whether the digest passes ParseDigest. The zero Digest is not valid.
func (digest Digest) IsValid() bool {
	_, err := ParseDigest(string(digest))
	return err == nil
}

// ImageRepo is a GHCR repository that ParseImageRepo has accepted. It is a repository, never a tag
// and never a reference: the digest is added separately, which is what makes "never a tag" true by
// construction.
type ImageRepo string

// ParseImageRepo accepts only "ghcr.io/<owner>/<name>": the registry this project publishes to,
// exactly two lowercase path components, and no tag, digest, port, query or whitespace. The host
// side builds its final reference from this same grammar, so the container cannot widen it.
func ParseImageRepo(text string) (ImageRepo, error) {
	const registry = "ghcr.io/"
	if len(text) > MaxImageRepoSize {
		return "", fmt.Errorf("%w: %d bytes exceeds %d", ErrInvalidImageRepo, len(text), MaxImageRepoSize)
	}
	if !strings.HasPrefix(text, registry) {
		return "", fmt.Errorf("%w: %q is not ghcr.io/<owner>/<name>", ErrInvalidImageRepo, text)
	}
	parts := strings.Split(text[len(registry):], "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: %q is not ghcr.io/<owner>/<name>", ErrInvalidImageRepo, text)
	}
	for _, part := range parts {
		if !isNameComponent(part) {
			return "", fmt.Errorf("%w: %q is not a lowercase repository component", ErrInvalidImageRepo, part)
		}
	}
	return ImageRepo(text), nil
}

// String returns the repository as it was parsed.
func (repo ImageRepo) String() string {
	return string(repo)
}

// Path is the repository without the registry, which is what GHCR's token and manifest URLs take.
func (repo ImageRepo) Path() string {
	return strings.TrimPrefix(string(repo), "ghcr.io/")
}

// Reference is the digest-pinned image reference: "ghcr.io/<owner>/<name>@sha256:...".
func (repo ImageRepo) Reference(digest Digest) string {
	return string(repo) + "@" + digest.String()
}

// IsValid reports whether the repository passes ParseImageRepo.
func (repo ImageRepo) IsValid() bool {
	_, err := ParseImageRepo(string(repo))
	return err == nil
}

// isNameComponent is the distribution name grammar for one path component: lowercase letters and
// digits, with a separator only between them, so a component can never be empty, start with a dash
// (a command-line flag), or end with a separator.
func isNameComponent(text string) bool {
	if text == "" || text[0] == '.' || text[0] == '-' || text[0] == '_' {
		return false
	}
	lastWasSeparator := false
	for index := 0; index < len(text); index++ {
		character := text[index]
		switch {
		case 'a' <= character && character <= 'z', '0' <= character && character <= '9':
			lastWasSeparator = false
		case character == '.' || character == '-' || character == '_':
			if lastWasSeparator {
				return false
			}
			lastWasSeparator = true
		default:
			return false
		}
	}
	return !lastWasSeparator
}
