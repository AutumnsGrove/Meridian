package cli

import (
	"os"
	"sync"
	"time"

	"github.com/autumnsgrove/meridian/internal/wire"
)

// hostReader reads the host snapshot the probe timer writes, re-parsing it only when the file has
// changed. It is the container's only view of systemd and the tailnet (decision #27), so a missing or
// unreadable file means "no reading", never a stale guess.
type hostReader struct {
	mu       sync.Mutex
	path     string
	modTime  time.Time
	size     int64
	snapshot wire.HostSnapshot
	ok       bool
}

func newHostReader() *hostReader {
	return &hostReader{}
}

// SetPath points the reader at the file the config names, and forgets what it read from the old one.
func (reader *hostReader) SetPath(path string) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.path == path {
		return
	}
	reader.path = path
	reader.modTime = time.Time{}
	reader.size = 0
	reader.snapshot = wire.HostSnapshot{}
	reader.ok = false
}

// Read returns the newest snapshot and whether there is one. A file that has not changed since the
// last call is not read again, which is what keeps a page load cheap.
func (reader *hostReader) Read() (wire.HostSnapshot, bool) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if reader.path == "" {
		return wire.HostSnapshot{}, false
	}

	info, err := os.Stat(reader.path)
	if err != nil || !info.Mode().IsRegular() {
		return reader.forget()
	}
	if info.Size() > wire.MaxHostSnapshotSize {
		return reader.forget()
	}
	if reader.ok && info.ModTime().Equal(reader.modTime) && info.Size() == reader.size {
		return reader.snapshot, true
	}

	data, err := os.ReadFile(reader.path) //nolint:gosec // the operator names this path in their config
	if err != nil {
		return reader.forget()
	}
	snapshot, err := wire.ParseHostSnapshot(data)
	if err != nil {
		return reader.forget()
	}
	reader.snapshot = snapshot
	reader.modTime = info.ModTime()
	reader.size = info.Size()
	reader.ok = true
	return snapshot, true
}

// forget drops the cached reading: the file is gone, or it no longer parses.
func (reader *hostReader) forget() (wire.HostSnapshot, bool) {
	reader.snapshot = wire.HostSnapshot{}
	reader.modTime = time.Time{}
	reader.size = 0
	reader.ok = false
	return wire.HostSnapshot{}, false
}
