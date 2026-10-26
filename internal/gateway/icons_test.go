package gateway_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autumnsgrove/meridian/internal/gateway"
)

// pngBytes is a one-pixel PNG, so the icon route serves something a browser could actually decode.
var pngBytes = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
}

// withIcons gives the gateway a config directory whose icons/ holds the given files.
func (hub *testGateway) withIcons(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	icons := filepath.Join(dir, "icons")
	if err := os.Mkdir(icons, 0o755); err != nil {
		t.Fatalf("create the icons directory: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(icons, name), content, 0o600); err != nil {
			t.Fatalf("write icon %s: %v", name, err)
		}
	}
	hub.deps.ConfigDir = dir
	return icons
}

func TestHandler_ServesAnIconWithAShortCache(t *testing.T) {
	gw := newTestGateway(t)
	gw.withIcons(t, map[string][]byte{"polaris.png": pngBytes})

	response := gw.hostRequest(t, "hub.example.org", "/icons/polaris.png")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("Cache-Control = %q, want public, max-age=3600", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestHandler_RefusesAnIconThatIsNotThere(t *testing.T) {
	gw := newTestGateway(t)
	gw.withIcons(t, map[string][]byte{"polaris.png": pngBytes})

	for _, target := range []string{"/icons/missing.png", "/icons/polaris.webp"} {
		if response := gw.hostRequest(t, "hub.example.org", target); response.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", target, response.Code)
		}
	}
}

func TestHandler_RefusesIconNamesThatCannotBeImages(t *testing.T) {
	gw := newTestGateway(t)
	gw.withIcons(t, map[string][]byte{"polaris.png": pngBytes})

	names := []string{
		"polaris.txt",
		"polaris.PNG",
		"polaris",
		".png",
		"%2e%2e%2fpolaris.png",
		"..%2fpolaris.png",
		"sub%2fpolaris.png",
	}
	for _, name := range names {
		response := gw.hostRequest(t, "hub.example.org", "/icons/"+name)
		if response.Code != http.StatusNotFound {
			t.Errorf("/icons/%s = %d, want 404", name, response.Code)
		}
	}
}

func TestHandler_RefusesAnIconSymlinkThatLeavesTheDirectory(t *testing.T) {
	gw := newTestGateway(t)
	icons := gw.withIcons(t, map[string][]byte{"polaris.png": pngBytes})

	secret := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(secret, []byte("the secret"), 0o600); err != nil {
		t.Fatalf("write the file outside the icons directory: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(icons, "escape.png")); err != nil {
		t.Skipf("this filesystem does not do symlinks: %v", err)
	}

	response := gw.hostRequest(t, "hub.example.org", "/icons/escape.png")
	if response.Code != http.StatusNotFound {
		t.Fatalf("a symlink out of the icons directory = %d, want 404", response.Code)
	}
	if strings.Contains(response.Body.String(), "the secret") {
		t.Fatal("the icon route read a file outside its directory")
	}
}

func TestHandler_NeverReadsOutsideTheIconsDirectory(t *testing.T) {
	gw := newTestGateway(t)
	gw.withIcons(t, map[string][]byte{"polaris.png": pngBytes})
	shell := "<!doctype html><title>launcher</title>"

	// Plain traversal is cleaned by the mux; an encoded separator reaches the handler with a name that
	// is not an image. Either way the answer must never be a file from outside icons/.
	for _, target := range []string{"/icons/..%2fpolaris.png", "/icons/%2e%2e%2fsecret", "/icons/../../secret"} {
		response := gw.hostRequest(t, "hub.example.org", target)
		switch response.Code {
		case http.StatusNotFound, http.StatusMovedPermanently, http.StatusTemporaryRedirect:
		case http.StatusOK:
			if body := response.Body.String(); body != shell {
				t.Fatalf("%s answered %q, want 404 or the app shell", target, body)
			}
		default:
			t.Fatalf("%s = %d, want 404 or a redirect", target, response.Code)
		}
	}
}

func TestIconNameAllowed(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "polaris.png", want: true},
		{name: "my-icon_2.webp", want: true},
		{name: "a.jpg", want: true},
		{name: "a.jpeg", want: true},
		{name: "a.gif", want: true},
		{name: "a.png.png", want: true},
		{name: "a.PNG", want: false},
		{name: "a.svg", want: false},
		{name: "a", want: false},
		{name: ".png", want: false},
		{name: "", want: false},
		{name: "../a.png", want: false},
		{name: "a/b.png", want: false},
		{name: "a\\b.png", want: false},
		{name: "a b.png", want: false},
	}
	for _, test := range tests {
		if got := gateway.IconNameAllowed(test.name); got != test.want {
			t.Errorf("IconNameAllowed(%q) = %v, want %v", test.name, got, test.want)
		}
	}
}

// FuzzIconName asserts the property the icon route relies on: an accepted name is a file name in
// one directory, so it can never name a path elsewhere.
func FuzzIconName(f *testing.F) {
	f.Add("polaris.png")
	f.Add("../../etc/passwd")
	f.Add("a/b.png")
	f.Add(".png")

	f.Fuzz(func(t *testing.T, name string) {
		if !gateway.IconNameAllowed(name) {
			return
		}
		if strings.ContainsAny(name, `/\`) {
			t.Fatalf("IconNameAllowed(%q) accepted a name with a path separator", name)
		}
		if name == "." || name == ".." {
			t.Fatalf("IconNameAllowed(%q) accepted a directory", name)
		}
		if filepath.Base(name) != name {
			t.Fatalf("IconNameAllowed(%q) is not a base name", name)
		}
	})
}
