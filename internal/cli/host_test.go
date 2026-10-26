package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const snapshotJSON = `{"at":"2026-10-25T12:00:00Z","host":{"uptime_s":86400,"load1":0.5},"units":{}}`

func writeSnapshot(t *testing.T, path, body string, modTime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestHostReader_ReadsTheSnapshotTheConfigNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	writeSnapshot(t, path, snapshotJSON, time.Now())

	reader := newHostReader()
	reader.SetPath(path)

	snapshot, ok := reader.Read()

	if !ok {
		t.Fatal("Read() = false, want the snapshot")
	}
	if snapshot.Host.Load1 != 0.5 || snapshot.Host.UptimeS != 86400 {
		t.Fatalf("snapshot = %+v, want the values in the file", snapshot.Host)
	}
}

func TestHostReader_RereadsWhenTheFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	first := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	writeSnapshot(t, path, snapshotJSON, first)

	reader := newHostReader()
	reader.SetPath(path)
	if _, ok := reader.Read(); !ok {
		t.Fatal("the first read failed")
	}

	writeSnapshot(t, path, strings.Replace(snapshotJSON, `"load1":0.5`, `"load1":2.5`, 1), first.Add(time.Minute))

	snapshot, ok := reader.Read()

	if !ok {
		t.Fatal("Read() after the change = false")
	}
	if snapshot.Host.Load1 != 2.5 {
		t.Fatalf("load1 = %v, want the re-read value 2.5", snapshot.Host.Load1)
	}
}

func TestHostReader_NoFileMeansNoReading(t *testing.T) {
	reader := newHostReader()
	reader.SetPath(filepath.Join(t.TempDir(), "absent.json"))

	if snapshot, ok := reader.Read(); ok || snapshot.At != (time.Time{}) {
		t.Fatalf("Read() = (%+v, %v), want the zero snapshot and false", snapshot, ok)
	}

	// Not configured at all reads the same way.
	unset := newHostReader()
	if _, ok := unset.Read(); ok {
		t.Fatal("a reader with no path reported a snapshot")
	}
}

func TestHostReader_CorruptFileForgetsTheLastGoodReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	first := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	writeSnapshot(t, path, snapshotJSON, first)
	reader := newHostReader()
	reader.SetPath(path)
	if _, ok := reader.Read(); !ok {
		t.Fatal("the good read failed")
	}

	writeSnapshot(t, path, `{"at":`, first.Add(time.Minute))

	if snapshot, ok := reader.Read(); ok || snapshot.At != (time.Time{}) {
		t.Fatalf("a corrupt file reported (%+v, %v), want nothing", snapshot, ok)
	}
}

func TestHostReader_RefusesAnOversizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	body := snapshotJSON + strings.Repeat(" ", 70<<10)
	writeSnapshot(t, path, body, time.Now())

	reader := newHostReader()
	reader.SetPath(path)

	if _, ok := reader.Read(); ok {
		t.Fatal("a file over the cap was read")
	}
}

func TestHostReader_SetPathDropsThePreviousFile(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "one.json")
	two := filepath.Join(dir, "two.json")
	writeSnapshot(t, one, snapshotJSON, time.Now())
	writeSnapshot(t, two, strings.Replace(snapshotJSON, `"load1":0.5`, `"load1":9`, 1), time.Now())

	reader := newHostReader()
	reader.SetPath(one)
	if _, ok := reader.Read(); !ok {
		t.Fatal("the first file did not read")
	}

	reader.SetPath(two)
	snapshot, ok := reader.Read()

	if !ok || snapshot.Host.Load1 != 9 {
		t.Fatalf("after SetPath: (%+v, %v), want the second file", snapshot.Host, ok)
	}
}

func TestConfigPath_PrefersTheFlagThenTheEnvironmentThenTheImageDefault(t *testing.T) {
	env := func(value string) func(string) string {
		return func(key string) string {
			if key == configEnvVar {
				return value
			}
			return ""
		}
	}

	tests := []struct {
		name  string
		flag  string
		value string
		want  string
	}{
		{name: "the flag wins", flag: "/etc/one.yaml", value: "/etc/two.yaml", want: "/etc/one.yaml"},
		{name: "then the environment", flag: "", value: "/etc/two.yaml", want: "/etc/two.yaml"},
		{name: "then the image default", flag: "", value: "", want: defaultConfigPath},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ConfigPath(test.flag, env(test.value)); got != test.want {
				t.Errorf("ConfigPath() = %q, want %q", got, test.want)
			}
		})
	}
}
