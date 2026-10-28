package wire_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/wire"
)

const (
	validDigest  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	validRepo    = "ghcr.io/autumnsgrove/meridian"
	validRepoDir = "autumnsgrove/meridian"
)

func TestParseDigest_AcceptsTheStrictGrammar(t *testing.T) {
	digest, err := wire.ParseDigest(validDigest)
	if err != nil {
		t.Fatalf("ParseDigest(%q) = %v", validDigest, err)
	}
	if got := digest.String(); got != validDigest {
		t.Errorf("String() = %q, want %q", got, validDigest)
	}
	if !digest.IsValid() {
		t.Error("IsValid() = false, want true")
	}
}

func TestParseDigest_RejectsEverythingElse(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"algorithm only", "sha256:"},
		{"hex too short", "sha256:0123"},
		{"hex too long", validDigest + "0"},
		{"uppercase hex", "sha256:0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef"},
		{"uppercase algorithm", "SHA256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{"other algorithm", "sha512:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{"no separator", "sha256" + strings.Repeat("a", 64)},
		{"leading space", " " + validDigest},
		{"trailing space", validDigest + " "},
		{"trailing newline", validDigest + "\n"},
		{"two digests", validDigest + validDigest},
		{"nul", validDigest[:10] + "\x00" + validDigest[11:]},
		{"unicode", "sha256:" + strings.Repeat("é", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest, err := wire.ParseDigest(test.text)
			if err == nil {
				t.Fatalf("ParseDigest(%q) = %q, want an error", test.text, digest)
			}
			if !errors.Is(err, wire.ErrInvalidDigest) {
				t.Errorf("error = %v, want ErrInvalidDigest", err)
			}
		})
	}
}

func TestDigest_ZeroValueIsInvalid(t *testing.T) {
	if wire.Digest("").IsValid() {
		t.Error("the zero Digest is valid")
	}
}

func TestParseImageRepo_AcceptsGHCRRepositories(t *testing.T) {
	tests := []struct {
		text string
		path string
	}{
		{validRepo, validRepoDir},
		{"ghcr.io/some-user/some.repo_name-2", "some-user/some.repo_name-2"},
		{"ghcr.io/a/b", "a/b"},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			repo, err := wire.ParseImageRepo(test.text)
			if err != nil {
				t.Fatalf("ParseImageRepo(%q) = %v", test.text, err)
			}
			if got := repo.String(); got != test.text {
				t.Errorf("String() = %q, want %q", got, test.text)
			}
			if got := repo.Path(); got != test.path {
				t.Errorf("Path() = %q, want %q", got, test.path)
			}
			if got, want := repo.Reference(wire.Digest(validDigest)), test.text+"@"+validDigest; got != want {
				t.Errorf("Reference() = %q, want %q", got, want)
			}
		})
	}
}

func TestParseImageRepo_RejectsAnythingElse(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"no registry", "autumnsgrove/meridian"},
		{"other registry", "docker.io/autumnsgrove/meridian"},
		{"uppercase registry", "GHCR.IO/autumnsgrove/meridian"},
		{"uppercase owner", "ghcr.io/Autumnsgrove/meridian"},
		{"uppercase name", "ghcr.io/autumnsgrove/Meridian"},
		{"one component", "ghcr.io/meridian"},
		{"three components", "ghcr.io/autumnsgrove/team/meridian"},
		{"trailing slash", "ghcr.io/autumnsgrove/"},
		{"double slash", "ghcr.io//meridian"},
		{"leading dash", "ghcr.io/-autumnsgrove/meridian"},
		{"dash before separator", "ghcr.io/autumnsgrove/-meridian"},
		{"double dot", "ghcr.io/autumnsgrove/mer..idian"},
		{"dot dot", "ghcr.io/autumnsgrove/.."},
		{"tag", "ghcr.io/autumnsgrove/meridian:latest"},
		{"digest", validRepo + "@" + validDigest},
		{"query", validRepo + "?x=1"},
		{"fragment", validRepo + "#x"},
		{"space", validRepo + " "},
		{"newline", validRepo + "\n"},
		{"nul", "ghcr.io/autumnsgrove/meri\x00dian"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, err := wire.ParseImageRepo(test.text)
			if err == nil {
				t.Fatalf("ParseImageRepo(%q) = %q, want an error", test.text, repo)
			}
			if !errors.Is(err, wire.ErrInvalidImageRepo) {
				t.Errorf("error = %v, want ErrInvalidImageRepo", err)
			}
		})
	}
}

func TestImageRepo_ZeroValueIsInvalid(t *testing.T) {
	if wire.ImageRepo("").IsValid() {
		t.Error("the zero ImageRepo is valid")
	}
}

// FuzzParseDigest is the digest grammar the update path pins: an accepted value is `sha256:` plus 64
// lowercase hex characters, and it renders back byte for byte.
func FuzzParseDigest(f *testing.F) {
	f.Add(validDigest)
	f.Add("")
	f.Add("sha256:")
	f.Add("sha256:" + strings.Repeat("a", 64))
	f.Add("sha256:" + strings.Repeat("A", 64))
	f.Add("sha512:" + strings.Repeat("a", 64))
	f.Add(validDigest + "\n")
	f.Fuzz(func(t *testing.T, text string) {
		digest, err := wire.ParseDigest(text)
		if err != nil {
			if !errors.Is(err, wire.ErrInvalidDigest) {
				t.Fatalf("ParseDigest(%q) error = %v, want ErrInvalidDigest", text, err)
			}
			return
		}
		if got := digest.String(); got != text {
			t.Fatalf("ParseDigest(%q) accepted a value that renders as %q", text, got)
		}
		if len(text) != len("sha256:")+64 || !strings.HasPrefix(text, "sha256:") {
			t.Fatalf("ParseDigest accepted %q, which is not sha256: plus 64 hex characters", text)
		}
		for _, character := range text[len("sha256:"):] {
			isDigit := '0' <= character && character <= '9'
			isLowerHexLetter := 'a' <= character && character <= 'f'
			if !isDigit && !isLowerHexLetter {
				t.Fatalf("ParseDigest accepted %q, which has a non-lowercase-hex character", text)
			}
		}
	})
}

// FuzzParseImageRepo is the repository grammar the update path trusts: an accepted value is exactly
// `ghcr.io/<owner>/<name>` with no tag, digest or extra slash, and it renders back byte for byte.
func FuzzParseImageRepo(f *testing.F) {
	f.Add(validRepo)
	f.Add("")
	f.Add("ghcr.io/a/b")
	f.Add("ghcr.io/A/b")
	f.Add("ghcr.io/a/b:latest")
	f.Add(validRepo + "@" + validDigest)
	f.Fuzz(func(t *testing.T, text string) {
		repo, err := wire.ParseImageRepo(text)
		if err != nil {
			if !errors.Is(err, wire.ErrInvalidImageRepo) {
				t.Fatalf("ParseImageRepo(%q) error = %v, want ErrInvalidImageRepo", text, err)
			}
			return
		}
		if got := repo.String(); got != text {
			t.Fatalf("ParseImageRepo(%q) accepted a value that renders as %q", text, got)
		}
		if !strings.HasPrefix(text, "ghcr.io/") || strings.Count(text, "/") != 2 {
			t.Fatalf("ParseImageRepo accepted %q, which is not ghcr.io/<owner>/<name>", text)
		}
	})
}
