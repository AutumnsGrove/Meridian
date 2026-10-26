package hostfile_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/autumnsgrove/meridian/internal/hostfile"
)

// currentUID is the uid the tests own their own files with; a unix uid always fits in 32 bits.
func currentUID() uint32 {
	return uint32(os.Getuid()) //nolint:gosec // a unix uid fits in uint32
}

// trust is the trust the tests grant a file they wrote themselves: the current user owns it.
func trust(maxSize int64) hostfile.Trust {
	return hostfile.Trust{Owner: currentUID(), MaxSize: maxSize}
}

func mustWrite(t *testing.T, path string, mode os.FileMode, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	// WriteFile applies the umask, so set the mode again to make the test mean what it says.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%s): %v", path, err)
	}
}

func TestRead_ReturnsTheBytesOfATrustedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	mustWrite(t, path, 0o644, `{"targets":{}}`)

	content, err := hostfile.Read(path, trust(1<<10))

	if err != nil {
		t.Fatalf("Read() = %v, want the file", err)
	}
	if got, want := string(content), `{"targets":{}}`; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestRead_RefusesWhatItCannotTrust(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	mustWrite(t, target, 0o644, `{}`)

	symlink := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	tests := []struct {
		name string
		path string
		make func(*testing.T) string
		when hostfile.Trust
	}{
		{
			name: "a group-writable file",
			make: func(t *testing.T) string {
				path := filepath.Join(dir, "group-writable.json")
				mustWrite(t, path, 0o664, `{}`)
				return path
			},
			when: trust(1 << 10),
		},
		{
			name: "a world-writable file",
			make: func(t *testing.T) string {
				path := filepath.Join(dir, "world-writable.json")
				mustWrite(t, path, 0o646, `{}`)
				return path
			},
			when: trust(1 << 10),
		},
		{
			name: "a file owned by somebody else",
			make: func(*testing.T) string { return target },
			when: hostfile.Trust{Owner: currentUID() + 1, MaxSize: 1 << 10},
		},
		{
			name: "a symbolic link, even to a trusted file",
			make: func(*testing.T) string { return symlink },
			when: trust(1 << 10),
		},
		{
			name: "a directory",
			make: func(*testing.T) string { return dir },
			when: trust(1 << 10),
		},
		{
			name: "a file larger than the cap",
			make: func(t *testing.T) string {
				path := filepath.Join(dir, "big.json")
				mustWrite(t, path, 0o644, "0123456789")
				return path
			},
			when: trust(8),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := hostfile.Read(test.make(t), test.when); !errors.Is(err, hostfile.ErrUntrusted) {
				t.Fatalf("Read() error = %v, want ErrUntrusted", err)
			}
		})
	}
}

func TestRead_ReportsAMissingFileAsNotExist(t *testing.T) {
	_, err := hostfile.Read(filepath.Join(t.TempDir(), "absent.json"), trust(1<<10))

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read() error = %v, want fs.ErrNotExist", err)
	}
}
