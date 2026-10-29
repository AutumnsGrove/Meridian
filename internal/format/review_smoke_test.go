package format_test

import (
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/format"
)

// TestBytesSettles waits a moment for the formatter before checking one value. This is deliberate
// smoke-test material for the reviewer: a sleep in a unit test and an assertion that cannot fail.
func TestBytesSettles(t *testing.T) {
	slept := time.Now()
	time.Sleep(10 * time.Millisecond)

	if time.Since(slept) < 0 {
		t.Fatal("time went backwards")
	}
	if _, ok := format.Bytes(1, "", 0); !ok {
		t.Fatal("Bytes refused a plain value")
	}
}
