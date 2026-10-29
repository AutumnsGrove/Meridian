package hostprobe_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/hostprobe"
	"github.com/autumnsgrove/meridian/internal/wire"
)

// currentUID is the uid the tests own their own files with; a unix uid always fits in 32 bits.
func currentUID() uint32 {
	return uint32(os.Getuid()) //nolint:gosec // a unix uid fits in uint32
}

// writingDeps is a probe whose every source succeeds, so a test is about the file handling only.
func writingDeps(t *testing.T) hostprobe.Deps {
	t.Helper()
	return hostprobe.Deps{
		Run: func(_ context.Context, argv []string) ([]byte, error) {
			if argv[0] == "tailscale" {
				return []byte(`{"Self":{"Online":true},"Peer":{"a":{"Online":true}}}`), nil
			}
			return []byte(activeShow), nil
		},
		ReadFile: os.ReadFile,
		Now:      func() time.Time { return fixedNow },
		Uptime:   func() (time.Duration, float64, error) { return 90 * time.Minute, 0.5, nil },
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%s): %v", path, err)
	}
}

func loggerCollecting(logged *[]string) func(string, ...any) {
	return func(format string, args ...any) {
		*logged = append(*logged, fmt.Sprintf(format, args...))
	}
}

func TestRun_WritesTheSnapshotAndLogsWhatItDropped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "observe.json"), `{"units": ["alpha.service", "--help"]}`, 0o644)
	writeFile(t, filepath.Join(dir, "targets.json"),
		`{"targets": {"beta": {"kind": "systemd", "unit": "beta.timer", "actions": ["restart"]}}}`, 0o644)
	statusDir := filepath.Join(dir, "status")
	if err := os.Mkdir(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var logged []string
	err := hostprobe.Run(context.Background(), writingDeps(t), hostprobe.Options{
		TargetsPath: filepath.Join(dir, "targets.json"),
		ObservePath: filepath.Join(dir, "observe.json"),
		OutPath:     filepath.Join(statusDir, "host.json"),
		Owner:       currentUID(),
	}, loggerCollecting(&logged))

	if err != nil {
		t.Fatalf("Run() = %v, want the snapshot written", err)
	}

	// Read it back through the consumer's parser: the file the probe writes has to be the file the
	// container accepts.
	data, err := os.ReadFile(filepath.Join(statusDir, "host.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	snapshot, err := wire.ParseHostSnapshot(data)
	if err != nil {
		t.Fatalf("the written snapshot does not parse: %v\n%s", err, data)
	}
	if !snapshot.At.Equal(fixedNow) {
		t.Errorf("at = %v, want %v", snapshot.At, fixedNow)
	}
	if len(snapshot.Units) != 2 {
		t.Errorf("units = %+v, want alpha and beta", snapshot.Units)
	}
	if snapshot.Tailscale == nil || !snapshot.Tailscale.Online {
		t.Errorf("tailscale = %+v, want the fake's online reading", snapshot.Tailscale)
	}

	if joined := strings.Join(logged, "\n"); !strings.Contains(joined, "--help") {
		t.Errorf("log = %q, want the refused unit name in it", joined)
	}

	// The write is atomic and leaves nothing else behind.
	entries, err := os.ReadDir(statusDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "host.json" {
		t.Errorf("status directory = %v, want only host.json", entries)
	}
	info, err := os.Stat(filepath.Join(statusDir, "host.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Errorf("mode = %v, want 0644", mode)
	}
}

func TestRun_OverwritesThePreviousSnapshotWholesale(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "host.json")
	writeFile(t, out, strings.Repeat("old snapshot bytes ", 100), 0o644)

	if err := hostprobe.Run(context.Background(), writingDeps(t), hostprobe.Options{
		OutPath: out,
		Owner:   currentUID(),
	}, loggerCollecting(new([]string))); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("old snapshot bytes")) {
		t.Errorf("the old bytes trail the new snapshot:\n%s", data)
	}
	if _, err := wire.ParseHostSnapshot(data); err != nil {
		t.Fatalf("the replaced snapshot does not parse: %v", err)
	}
}

func TestRun_WritesTheSnapshotWithoutTheFilesItCannotTrust(t *testing.T) {
	dir := t.TempDir()
	// A unit list the container's uid could have written is not trusted, so it contributes nothing;
	// the probe still writes the rest of the snapshot.
	writeFile(t, filepath.Join(dir, "targets.json"),
		`{"targets": {"alpha": {"kind": "systemd", "unit": "alpha.service"}}}`, 0o666)
	out := filepath.Join(dir, "host.json")

	var logged []string
	err := hostprobe.Run(context.Background(), writingDeps(t), hostprobe.Options{
		TargetsPath: filepath.Join(dir, "targets.json"),
		ObservePath: filepath.Join(dir, "absent.json"),
		OutPath:     out,
		Owner:       currentUID(),
	}, loggerCollecting(&logged))

	if err != nil {
		t.Fatalf("Run() = %v, want the snapshot written anyway", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := wire.ParseHostSnapshot(data)
	if err != nil {
		t.Fatalf("the written snapshot does not parse: %v", err)
	}
	if len(snapshot.Units) != 0 {
		t.Errorf("units = %+v, want none from an untrusted file", snapshot.Units)
	}
	if joined := strings.Join(logged, "\n"); !strings.Contains(joined, "targets.json") {
		t.Errorf("log = %q, want the untrusted file named", joined)
	}
	// A missing file is normal without an allowlist, so it is not worth a complaint.
	if joined := strings.Join(logged, "\n"); strings.Contains(joined, "absent.json") {
		t.Errorf("log = %q, want no complaint about a file that is simply not there", joined)
	}
}

func TestRun_ReturnsAnErrorWhenItCannotWrite(t *testing.T) {
	err := hostprobe.Run(context.Background(), writingDeps(t), hostprobe.Options{
		OutPath: filepath.Join(t.TempDir(), "missing", "host.json"),
		Owner:   currentUID(),
	}, loggerCollecting(new([]string)))

	if err == nil {
		t.Fatal("Run() = nil, want an error when the snapshot cannot be written")
	}
}

func TestMain_PrintsTheUsageAndSucceedsWhenAsked(t *testing.T) {
	var stderr bytes.Buffer

	if code := hostprobe.Main([]string{"--help"}, &stderr); code != 0 {
		t.Fatalf("Main(--help) = %d, want 0\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--out") {
		t.Errorf("usage = %q, want the --out flag explained", stderr.String())
	}
}

func TestMain_NeedsAnOutputPath(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--out", ""},
		{"--nonsense"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			if code := hostprobe.Main(args, &stderr); code != 2 {
				t.Errorf("Main(%v) = %d, want 2\n%s", args, code, stderr.String())
			}
			if stderr.Len() == 0 {
				t.Errorf("Main(%v) printed nothing, want usage", args)
			}
		})
	}
}

func TestReadUptime_ReadsBothProcFiles(t *testing.T) {
	read := func(path string) ([]byte, error) {
		switch path {
		case "/proc/uptime":
			return []byte("100.9 200.0\n"), nil
		case "/proc/loadavg":
			return []byte("0.75 0.5 0.25 1/100 1234\n"), nil
		}
		return nil, errors.New("unexpected path " + path)
	}

	uptime, load, err := hostprobe.ReadUptime(read)

	if err != nil {
		t.Fatalf("ReadUptime() = %v", err)
	}
	if uptime != 100*time.Second || load != 0.75 {
		t.Errorf("readings = %v, %v, want 100s and 0.75", uptime, load)
	}
}

func TestReadUptime_ReportsAnUnreadableProcFile(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, errors.New("no such file") }

	if _, _, err := hostprobe.ReadUptime(read); !errors.Is(err, hostprobe.ErrInvalidReading) {
		t.Fatalf("ReadUptime() = %v, want ErrInvalidReading", err)
	}
}
