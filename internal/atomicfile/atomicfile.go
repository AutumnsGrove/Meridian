package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Write replaces dir/name with data so that a concurrent reader sees either the old file or the
// complete new one, never a partial write. The temporary file lives in dir so the final rename
// cannot cross a mount boundary.
func Write(dir, name string, data []byte, mode fs.FileMode) error {
	return WriteVia(dir, dir, name, data, mode)
}

// WriteVia is Write with the temporary file in tmpDir, for a queue whose writers and readers watch
// different directories on the same mount (decision #36). Both directories must be on the same
// filesystem; rename cannot cross one.
func WriteVia(tmpDir, dir, name string, data []byte, mode fs.FileMode) error {
	if err := validateName(name); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(tmpDir, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("atomicfile: create temporary file in %s: %w", tmpDir, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	if err = temporary.Chmod(mode); err != nil {
		return fmt.Errorf("atomicfile: chmod %s: %w", temporaryPath, err)
	}
	if _, err = temporary.Write(data); err != nil {
		return fmt.Errorf("atomicfile: write %s: %w", temporaryPath, err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("atomicfile: sync %s: %w", temporaryPath, err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("atomicfile: close %s: %w", temporaryPath, err)
	}
	if err = os.Rename(temporaryPath, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("atomicfile: rename %s into %s: %w", temporaryPath, dir, err)
	}
	if err = syncDir(dir); err != nil {
		return fmt.Errorf("atomicfile: sync directory %s: %w", dir, err)
	}
	return nil
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." {
		return errors.New("atomicfile: name must be a bare file name")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("atomicfile: name %q must not contain a path separator", name)
	}
	return nil
}

// syncDir makes the rename itself durable, not just the file contents.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return handle.Sync()
}
