package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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

// WriteRoot is WriteVia through an os.Root: the temporary file is tmpDir/name and the destination is
// dir/name, both relative to root. The container writes its request files this way, because it shares
// that directory with the host and a name it is handed must never resolve outside it.
func WriteRoot(root *os.Root, tmpDir, dir, name string, data []byte, mode fs.FileMode) error {
	if err := validateName(name); err != nil {
		return err
	}

	// A random suffix rather than a counter: the directory outlives one process, and two writers
	// must not pick the same temporary name.
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("atomicfile: random suffix: %w", err)
	}
	temporaryName := path.Join(tmpDir, "."+name+".tmp-"+hex.EncodeToString(suffix[:]))

	temporary, err := root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("atomicfile: create temporary file %s: %w", temporaryName, err)
	}
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = root.Remove(temporaryName)
		}
	}()

	// O_CREATE's mode is filtered by the umask; set it exactly, as WriteVia does.
	if err = root.Chmod(temporaryName, mode); err != nil {
		return fmt.Errorf("atomicfile: chmod %s: %w", temporaryName, err)
	}
	if _, err = temporary.Write(data); err != nil {
		return fmt.Errorf("atomicfile: write %s: %w", temporaryName, err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("atomicfile: sync %s: %w", temporaryName, err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("atomicfile: close %s: %w", temporaryName, err)
	}
	if err = root.Rename(temporaryName, path.Join(dir, name)); err != nil {
		return fmt.Errorf("atomicfile: rename %s into %s: %w", temporaryName, dir, err)
	}
	if err = syncRootDir(root, dir); err != nil {
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

// syncRootDir is syncDir through a root.
func syncRootDir(root *os.Root, dir string) error {
	handle, err := root.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return handle.Sync()
}
