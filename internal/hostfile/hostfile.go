// Package hostfile reads the files the host trusts, with the checks every reader of /etc/meridian
// applies before it believes the bytes: a regular file, owned by the expected user, not writable by
// group or others, and not larger than the caller can use. It is its own package because both host
// commands need it, and neither may import the other: the probe reads its unit lists, the executor
// reads the allowlist (decision #62).
package hostfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// ErrUntrusted means the file exists but is not something the host may act on: a symlink, a
// directory, a file the wrong user owns, a file anyone can edit, or one that is too large.
var ErrUntrusted = errors.New("not a trusted file")

// Trust is what a caller requires of a file before it reads it.
type Trust struct {
	// Owner is the uid the file must belong to: 0 for anything the executor trusts, the deploy
	// user for files written by the installer.
	Owner uint32
	// MaxSize is the largest file worth reading, in bytes.
	MaxSize int64
}

// Read returns the contents of a file that passes the trust checks. A missing file comes back as an
// error wrapping fs.ErrNotExist, so a caller can treat "not there" differently from "not trusted".
func Read(path string, trust Trust) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("hostfile: %s: %w", path, err)
	}
	if err := check(before, trust); err != nil {
		return nil, fmt.Errorf("hostfile: %s: %w", path, err)
	}

	// O_NOFOLLOW closes the gap between the check and the open: a path that becomes a symlink in
	// between is refused rather than followed.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) //nolint:gosec // the operator names this path in a root-owned file
	if err != nil {
		return nil, fmt.Errorf("hostfile: %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("hostfile: %s: %w", path, err)
	}
	if err := check(opened, trust); err != nil {
		return nil, fmt.Errorf("hostfile: %s: %w", path, err)
	}

	content, err := io.ReadAll(io.LimitReader(file, trust.MaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("hostfile: %s: %w", path, err)
	}
	if int64(len(content)) > trust.MaxSize {
		return nil, fmt.Errorf("hostfile: %s: %w: grew past %d bytes while reading", path, ErrUntrusted, trust.MaxSize)
	}
	return content, nil
}

// check applies the trust rules to one file description.
func check(info fs.FileInfo, trust Trust) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrUntrusted)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: writable by group or others", ErrUntrusted)
	}
	if info.Size() > trust.MaxSize {
		return fmt.Errorf("%w: %d bytes is larger than %d", ErrUntrusted, info.Size(), trust.MaxSize)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: no ownership information", ErrUntrusted)
	}
	if stat.Uid != trust.Owner {
		return fmt.Errorf("%w: owned by uid %d, want %d", ErrUntrusted, stat.Uid, trust.Owner)
	}
	return nil
}
