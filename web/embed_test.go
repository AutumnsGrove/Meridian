package web_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/autumnsgrove/meridian/web"
)

func TestFS_IsRootedInsideBuildDirectory(t *testing.T) {
	embedded := web.FS()
	if embedded == nil {
		t.Fatal("FS() = nil, want the embedded frontend")
	}
	if _, err := fs.Stat(embedded, "."); err != nil {
		t.Fatalf("Stat(%q) error = %v, want the build directory to be readable", ".", err)
	}
	if _, err := fs.Stat(embedded, "build"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(`Stat("build") error = %v, want fs.ErrNotExist: FS() must strip the build/ prefix`, err)
	}
}
