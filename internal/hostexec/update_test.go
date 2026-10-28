package hostexec_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostexec"
	"github.com/autumnsgrove/meridian/internal/wire"
)

const (
	digestA = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	digestB = "sha256:" + "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	image   = "ghcr.io/owner/meridian"
)

func TestSetVar_ReplacesOnlyTheNamedLine(t *testing.T) {
	contents := "# pinned by install.sh\nOTHER=one\nMERIDIAN_IMAGE=old\nTAIL=two\n"
	got, err := hostexec.SetVar(contents, "MERIDIAN_IMAGE", image+"@"+digestA)
	if err != nil {
		t.Fatalf("SetVar: %v", err)
	}
	want := "# pinned by install.sh\nOTHER=one\nMERIDIAN_IMAGE=" + image + "@" + digestA + "\nTAIL=two\n"
	if got != want {
		t.Fatalf("SetVar =\n%q\nwant\n%q", got, want)
	}
}

func TestSetVar_PreservesCRLFAndQuotedValues(t *testing.T) {
	contents := "# comment\r\nOTHER=\"quoted value\"\r\nMERIDIAN_IMAGE=\"old@sha256:abc\"\r\n\r\nTAIL=2\r\n"
	got, err := hostexec.SetVar(contents, "MERIDIAN_IMAGE", image+"@"+digestB)
	if err != nil {
		t.Fatalf("SetVar: %v", err)
	}
	want := "# comment\r\nOTHER=\"quoted value\"\r\nMERIDIAN_IMAGE=" + image + "@" + digestB + "\r\n\r\nTAIL=2\r\n"
	if got != want {
		t.Fatalf("SetVar =\n%q\nwant\n%q", got, want)
	}
}

func TestSetVar_AppendsWhenTheKeyIsMissing(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{"empty file", "", "MERIDIAN_IMAGE=new\n"},
		{"trailing newline", "OTHER=one\n", "OTHER=one\nMERIDIAN_IMAGE=new\n"},
		{"no trailing newline", "OTHER=one", "OTHER=one\nMERIDIAN_IMAGE=new"},
		{"CRLF", "OTHER=one\r\n", "OTHER=one\r\nMERIDIAN_IMAGE=new\r\n"},
		{"blank lines", "\n\n", "\n\nMERIDIAN_IMAGE=new\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := hostexec.SetVar(test.contents, "MERIDIAN_IMAGE", "new")
			if err != nil {
				t.Fatalf("SetVar: %v", err)
			}
			if got != test.want {
				t.Fatalf("SetVar = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSetVar_RefusesWhatWouldBreakTheFile(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		key      string
		value    string
		wantErr  error
	}{
		{"empty key", "A=1\n", "", "new", hostexec.ErrEnvKey},
		{"lowercase key", "A=1\n", "meridian_image", "new", hostexec.ErrEnvKey},
		{"key with a dash", "A=1\n", "MERIDIAN-IMAGE", "new", hostexec.ErrEnvKey},
		{"key with an equals", "A=1\n", "A=B", "new", hostexec.ErrEnvKey},
		{"value with a newline", "A=1\n", "MERIDIAN_IMAGE", "a\nb", hostexec.ErrEnvValue},
		{"value with a carriage return", "A=1\n", "MERIDIAN_IMAGE", "a\rb", hostexec.ErrEnvValue},
		{"value with a NUL", "A=1\n", "MERIDIAN_IMAGE", "a\x00b", hostexec.ErrEnvValue},
		{"value with a space", "A=1\n", "MERIDIAN_IMAGE", "a b", hostexec.ErrEnvValue},
		{"value with a tab", "A=1\n", "MERIDIAN_IMAGE", "a\tb", hostexec.ErrEnvValue},
		{"empty value", "A=1\n", "MERIDIAN_IMAGE", "", hostexec.ErrEnvValue},
		{"duplicate key", "MERIDIAN_IMAGE=one\nA=1\nMERIDIAN_IMAGE=two\n", "MERIDIAN_IMAGE", "new", hostexec.ErrEnvDuplicate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := hostexec.SetVar(test.contents, test.key, test.value)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("SetVar(%q, %q, %q) error = %v, want %v", test.contents, test.key, test.value, err, test.wantErr)
			}
			if got != "" {
				t.Errorf("SetVar returned %q with an error", got)
			}
		})
	}
}

func TestImageRef_AcceptsOnlyADigestForTheAllowlistedRepository(t *testing.T) {
	repo := wire.ImageRepo(image)
	ref, err := hostexec.ImageRef(repo, []byte(digestA))
	if err != nil {
		t.Fatalf("ImageRef: %v", err)
	}
	if want := image + "@" + digestA; ref != want {
		t.Errorf("ImageRef = %q, want %q", ref, want)
	}
}

func TestImageRef_RefusesEverythingElse(t *testing.T) {
	repo := wire.ImageRepo(image)
	tests := []struct {
		name     string
		repo     wire.ImageRepo
		contents string
	}{
		{"empty", repo, ""},
		{"a tag", repo, "latest"},
		{"a tag with the repo", repo, image + ":latest"},
		{"a full reference", repo, image + "@" + digestA},
		{"another repository", repo, "ghcr.io/owner/other@" + digestA},
		{"another registry", repo, "docker.io/owner/meridian@" + digestA},
		{"uppercase hex", repo, "sha256:" + strings.Repeat("A", 64)},
		{"short", repo, "sha256:" + strings.Repeat("a", 63)},
		{"trailing newline", repo, digestA + "\n"},
		{"trailing space", repo, digestA + " "},
		{"leading space", repo, " " + digestA},
		{"nul", repo, digestA[:10] + "\x00" + digestA[11:]},
		{"too long", repo, digestA + strings.Repeat("a", 128)},
		{"invalid repository", wire.ImageRepo("docker.io/owner/meridian"), digestA},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := hostexec.ImageRef(test.repo, []byte(test.contents))
			if !errors.Is(err, wire.ErrInvalidDigest) {
				t.Fatalf("ImageRef(%q) error = %v, want ErrInvalidDigest", test.contents, err)
			}
			if ref != "" {
				t.Errorf("ImageRef returned %q with an error", ref)
			}
		})
	}
}

func TestReadUpdateSignal_ReadsOneRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requested")
	if err := os.WriteFile(path, []byte(digestA), 0o600); err != nil {
		t.Fatalf("write the signal: %v", err)
	}
	data, err := hostexec.ReadUpdateSignal(path)
	if err != nil {
		t.Fatalf("ReadUpdateSignal: %v", err)
	}
	if got := string(data); got != digestA {
		t.Errorf("signal = %q, want %q", got, digestA)
	}
}

func TestReadUpdateSignal_RefusesSymlinksDirectoriesAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte(digestA), 0o600); err != nil {
		t.Fatalf("write the real file: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", 129)), 0o600); err != nil {
		t.Fatalf("write the big file: %v", err)
	}

	for _, path := range []string{link, dir, big, filepath.Join(dir, "missing")} {
		if _, err := hostexec.ReadUpdateSignal(path); err == nil {
			t.Errorf("ReadUpdateSignal(%q) accepted it", path)
		}
	}
}

func FuzzSetVar(f *testing.F) {
	f.Add("# comment\nA=1\nMERIDIAN_IMAGE=old\n", "MERIDIAN_IMAGE", "new")
	f.Add("", "MERIDIAN_IMAGE", "new")
	f.Add("A=1\r\n", "MERIDIAN_IMAGE", "new")
	f.Add("A=1", "MERIDIAN_IMAGE", "new")
	f.Add("A=1\nA=2\n", "A", "new")
	f.Add("A=1\n", "", "new")
	f.Add("A=1\n", "MERIDIAN_IMAGE", "a\nb")
	f.Fuzz(func(t *testing.T, contents, key, value string) {
		got, err := hostexec.SetVar(contents, key, value)
		if err != nil {
			if !errors.Is(err, hostexec.ErrEnvKey) && !errors.Is(err, hostexec.ErrEnvValue) &&
				!errors.Is(err, hostexec.ErrEnvDuplicate) {
				t.Fatalf("SetVar error = %v, want one of the .env sentinels", err)
			}
			return
		}
		// One assignment of the key, and no injected line inside its value.
		assignments := 0
		for _, line := range strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n") {
			body := strings.TrimSuffix(line, "\r")
			if body == key+"="+value {
				assignments++
			}
		}
		if assignments != 1 {
			t.Fatalf("SetVar = %q, want exactly one %s assignment", got, key)
		}
		if strings.Contains(key, "\n") || strings.Contains(value, "\n") {
			t.Fatalf("SetVar accepted a newline in %q or %q", key, value)
		}
	})
}

func FuzzImageRef(f *testing.F) {
	repo := wire.ImageRepo(image)
	f.Add(digestA)
	f.Add("")
	f.Add("latest")
	f.Add(image + "@" + digestA)
	f.Add("sha256:" + strings.Repeat("A", 64))
	f.Add(digestA + "\n")
	f.Add(strings.Repeat("a", 129))
	f.Fuzz(func(t *testing.T, contents string) {
		ref, err := hostexec.ImageRef(repo, []byte(contents))
		if err != nil {
			if !errors.Is(err, wire.ErrInvalidDigest) {
				t.Fatalf("ImageRef error = %v, want ErrInvalidDigest", err)
			}
			return
		}
		prefix := image + "@sha256:"
		if !strings.HasPrefix(ref, prefix) {
			t.Fatalf("ImageRef = %q, want the allowlisted prefix %q", ref, prefix)
		}
		digest := strings.TrimPrefix(ref, image+"@")
		if _, err := wire.ParseDigest(digest); err != nil {
			t.Fatalf("ImageRef = %q, whose digest does not parse: %v", ref, err)
		}
	})
}
