package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
)

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(content)
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	names := make([]string, 0, len(dirEntries))
	for _, entry := range dirEntries {
		names = append(names, entry.Name())
	}
	return names
}

func TestWrite_CreatesTheFileWithContentAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")

	if err := atomicfile.Write(dir, "request.json", []byte(`{"action":"restart"}`), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, want := read(t, path), `{"action":"restart"}`; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("mode = %v, want %v", got, want)
	}
	if names := entries(t, dir); len(names) != 1 || names[0] != "request.json" {
		t.Errorf("directory contents = %v, want only request.json", names)
	}
}

func TestWrite_ReplacesExistingContentWholesale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")
	if err := os.WriteFile(path, []byte("old and longer than the new content"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := atomicfile.Write(dir, "request.json", []byte("new"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := read(t, path), "new"; got != want {
		t.Errorf("content = %q, want %q (old bytes must not trail the new ones)", got, want)
	}
}

func TestWrite_RejectsNamesThatCouldEscapeTheDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "escaped.json")
	for _, name := range []string{"", ".", "..", "a/b", "../escaped.json", "/escaped.json", `a\b`} {
		t.Run(name, func(t *testing.T) {
			if err := atomicfile.Write(dir, name, []byte("x"), 0o600); err == nil {
				t.Fatalf("Write(%q) = nil, want an error", name)
			}
		})
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("a rejected name still wrote %s", outside)
	}
}

func TestWrite_LeavesTheOldFileAloneWhenTheNewWriteCannotStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := atomicfile.Write(dir, "request.json", []byte("new"), 0o600); err == nil {
		t.Fatal("Write into a read-only directory = nil, want an error")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, path), "old"; got != want {
		t.Errorf("content after a failed write = %q, want the untouched %q", got, want)
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Errorf("directory contents = %v, want only the old file (no temp files left behind)", names)
	}
}

func TestWrite_ReplacesAPlantedSymlinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "request.json")); err != nil {
		t.Fatal(err)
	}

	if err := atomicfile.Write(dir, "request.json", []byte("new"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := read(t, outside), "do not touch"; got != want {
		t.Errorf("file behind the symlink = %q, want %q", got, want)
	}
	if info, err := os.Lstat(filepath.Join(dir, "request.json")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("destination is still a symlink (err %v)", err)
	}
}

func TestWriteVia_UsesTheTempDirectoryAndCleansItUp(t *testing.T) {
	root := t.TempDir()
	tmpDir := filepath.Join(root, "tmp")
	queueDir := filepath.Join(root, "queue")
	for _, dir := range []string{tmpDir, queueDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := atomicfile.WriteVia(tmpDir, queueDir, "request.json", []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteVia: %v", err)
	}
	if got, want := read(t, filepath.Join(queueDir, "request.json")), "payload"; got != want {
		t.Errorf("queued content = %q, want %q", got, want)
	}
	if names := entries(t, tmpDir); len(names) != 0 {
		t.Errorf("temp directory contents = %v, want empty", names)
	}
}

func TestWriteVia_CleansUpTheTempFileWhenTheRenameFails(t *testing.T) {
	root := t.TempDir()
	tmpDir := filepath.Join(root, "tmp")
	if err := os.Mkdir(tmpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	missingDir := filepath.Join(root, "missing")

	if err := atomicfile.WriteVia(tmpDir, missingDir, "request.json", []byte("payload"), 0o600); err == nil {
		t.Fatal("WriteVia into a missing directory = nil, want an error")
	}
	if names := entries(t, tmpDir); len(names) != 0 {
		t.Errorf("temp directory contents = %v, want empty after a failed rename", names)
	}
}
