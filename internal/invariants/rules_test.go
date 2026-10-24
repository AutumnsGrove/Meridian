package invariants_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// modulePath is the module in go.mod, used to tell our own packages from third-party ones.
const modulePath = "github.com/autumnsgrove/meridian"

// goFile is one parsed non-test Go file under the root being checked.
type goFile struct {
	rel  string
	file *ast.File
	fset *token.FileSet
}

func (f goFile) pos(node ast.Node) string {
	return f.fset.Position(node.Pos()).String()
}

// walkGoFiles parses every non-test .go file under root. Directories that are generated, vendored or
// fixtures are skipped, except the root itself: a fixture directory stands in for the repository so
// the same rule code can be run over it.
func walkGoFiles(root string, visit func(goFile) error) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		return visit(goFile{rel: filepath.ToSlash(rel), file: file, fset: fset})
	})
}

// walkText visits the contents of every non-test file under root with one of the given extensions.
func walkText(root string, extensions []string, visit func(rel, content string) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !hasExtension(path, extensions) || isTestFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path) //nolint:gosec // the path comes from the tree being walked, not from input
		if err != nil {
			return err
		}
		return visit(filepath.ToSlash(rel), string(content))
	})
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".svelte-kit", "build", "dist", "node_modules", "testdata", "vendor":
		return true
	}
	return false
}

func hasExtension(path string, extensions []string) bool {
	ext := filepath.Ext(path)
	for _, want := range extensions {
		if ext == want {
			return true
		}
	}
	return false
}

// isTestFile reports whether path is a Go test or a frontend test file, both of which are allowed to
// name services (fixtures and assertions are how the rules themselves are tested).
func isTestFile(path string) bool {
	if strings.HasSuffix(path, "_test.go") {
		return true
	}
	for _, suffix := range []string{".test.ts", ".spec.ts", ".test.js", ".spec.js"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return strings.Contains(filepath.ToSlash(path), "/__tests__/")
}

// underAny reports whether the repo-relative path rel is one of dirs or inside one of them.
func underAny(rel string, dirs []string) bool {
	for _, dir := range dirs {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// qualifiedName turns os.Exit into "os.Exit" and returns false for anything else.
func qualifiedName(expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return pkg.Name + "." + selector.Sel.Name, true
}

// hostSidePackages are the packages that may talk to systemd, Docker and the tailnet. Everything in
// the container-side code must not.
var hostSidePackages = []string{"cmd/meridian-host", "internal/hostexec", "internal/hostprobe"}

// serviceNameExempt are the packages that must name host-side facts: the host probe talks to systemd
// and Tailscale, and the host snapshot contract carries their field names. Everywhere else, service
// vocabulary stays in config.yaml (decision #38).
var serviceNameExempt = []string{"cmd/meridian-host", "internal/hostexec", "internal/hostprobe", "internal/wire"}

// serviceWords returns the lowercase words that must not appear in code: the hard-coded names this
// project was built for, plus every id, name and URL host under services: in the example config.
// Loopback hosts and the project's own name are not service knowledge and are left out.
func serviceWords(configPath string) ([]string, error) {
	words := []string{"polaris", "ivy", "bwmon", "adguard", "searxng", "vaultwarden", "unbound", "her-go"}
	seen := map[string]bool{}

	data, err := os.ReadFile(configPath) //nolint:gosec // a fixed path under the root being checked
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	inServices := false
	for _, line := range strings.Split(string(data), "\n") {
		if match := sectionID.FindStringSubmatch(line); match != nil {
			words = append(words, strings.ToLower(match[1]))
		}
		if strings.TrimSpace(line) == "services:" {
			inServices = true
			continue
		}
		if inServices && line != "" && !strings.HasPrefix(line, " ") {
			inServices = false
		}
		if !inServices {
			continue
		}
		if match := serviceIDLine.FindStringSubmatch(line); match != nil {
			words = append(words, unquote(match[1]))
		}
		if match := serviceNameLine.FindStringSubmatch(line); match != nil {
			words = append(words, unquote(match[1]))
		}
		if match := serviceURLLine.FindStringSubmatch(line); match != nil {
			if host := urlHost(match[1]); host != "" {
				words = append(words, host)
			}
		}
	}

	unique := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.ToLower(strings.TrimSpace(word))
		if word == "" || word == "meridian" || seen[word] {
			continue
		}
		seen[word] = true
		unique = append(unique, word)
	}
	return unique, nil
}

var (
	serviceIDLine   = regexp.MustCompile(`^\s*- id:\s*(\S+)\s*$`)
	serviceNameLine = regexp.MustCompile(`^ {4}name:\s*(.+?)\s*$`)
	serviceURLLine  = regexp.MustCompile(`^ {4}url:\s*(\S+)\s*$`)
	sectionID       = regexp.MustCompile(`\{\s*id:\s*([a-z0-9][a-z0-9-]*)`)
)

func unquote(value string) string {
	return strings.Trim(value, `"'`)
}

// urlHost returns the host of a URL, or "" for an empty URL or a loopback host.
func urlHost(raw string) string {
	parsed, err := url.Parse(unquote(raw))
	if err != nil {
		return ""
	}
	switch host := parsed.Hostname(); host {
	case "", "localhost", "127.0.0.1", "::1":
		return ""
	default:
		return host
	}
}

// checkNoServiceNames is rule 38: no service name, host, port, icon or quirk in Go or Svelte.
func checkNoServiceNames(root string) error {
	words, err := serviceWords(filepath.Join(root, "examples", "homelab.yaml"))
	if err != nil {
		return err
	}
	patterns := make([]*regexp.Regexp, 0, len(words))
	for _, word := range words {
		pattern, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`)
		if err != nil {
			return fmt.Errorf("compile %q: %w", word, err)
		}
		patterns = append(patterns, pattern)
	}

	return walkText(root, []string{".go", ".js", ".svelte", ".ts"}, func(rel, content string) error {
		if !strings.HasSuffix(rel, ".go") && !strings.HasPrefix(rel, "web/src/") {
			return nil
		}
		if underAny(rel, serviceNameExempt) {
			return nil
		}
		for lineNumber, line := range strings.Split(content, "\n") {
			for i, pattern := range patterns {
				if pattern.MatchString(line) {
					return fmt.Errorf("%s:%d: %q is service-specific, so it belongs in config.yaml (decision #38)",
						rel, lineNumber+1, words[i])
				}
			}
		}
		return nil
	})
}

// checkNoShell is the no-shell rule: no exec.Command of sh, bash or a -c flag, anywhere.
func checkNoShell(root string) error {
	shells := map[string]bool{"-c": true, "bash": true, "dash": true, "ksh": true, "sh": true, "zsh": true}

	return walkGoFiles(root, func(f goFile) error {
		var violation error
		ast.Inspect(f.file, func(node ast.Node) bool {
			if violation != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := qualifiedName(call.Fun)
			if !ok || (name != "exec.Command" && name != "exec.CommandContext") {
				return true
			}
			for _, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				if shells[filepath.Base(value)] {
					violation = fmt.Errorf("%s: %s runs a shell (%q); commands are argv arrays (decision #52)",
						f.pos(call), name, value)
					return false
				}
			}
			return true
		})
		return violation
	})
}

// checkExecConfinement is the os/exec rule: only the host side may run subprocesses.
func checkExecConfinement(root string) error {
	return walkGoFiles(root, func(f goFile) error {
		for _, imported := range f.file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil || path != "os/exec" {
				continue
			}
			if underAny(f.rel, hostSidePackages) {
				continue
			}
			return fmt.Errorf("%s imports os/exec; only %v may run subprocesses", f.rel, hostSidePackages)
		}
		return nil
	})
}

// checkHostLiterals keeps systemd and Docker vocabulary out of the container side.
func checkHostLiterals(root string) error {
	forbidden := []string{"docker.sock", "systemctl", "tailscaled.sock"}

	return walkGoFiles(root, func(f goFile) error {
		if underAny(f.rel, hostSidePackages) {
			return nil
		}
		var violation error
		ast.Inspect(f.file, func(node ast.Node) bool {
			if violation != nil {
				return false
			}
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			for _, word := range forbidden {
				if strings.Contains(value, word) {
					violation = fmt.Errorf("%s: %q belongs on the host side, not in the container (decision #27)",
						f.pos(literal), word)
					return false
				}
			}
			return true
		})
		return violation
	})
}

// checkHostStdlib is the stdlib-only rule for the host binary and the packages it shares with it.
func checkHostStdlib(root string) error {
	hostPackages := []string{"cmd/meridian-host", "internal/atomicfile", "internal/hostexec", "internal/hostprobe", "internal/jsonpath", "internal/wire"}

	return walkGoFiles(root, func(f goFile) error {
		if !underAny(f.rel, hostPackages) {
			return nil
		}
		for _, imported := range f.file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if isStandardLibrary(path) {
				continue
			}
			if internal, ok := strings.CutPrefix(path, modulePath+"/"); ok && underAny(internal, hostPackages) {
				// Our own packages in this set are checked by this same rule, so the set is closed.
				continue
			}
			return fmt.Errorf("%s imports %q; the host side is standard library only (decision #56)", f.rel, path)
		}
		return nil
	})
}

// isStandardLibrary uses the Go convention that a standard library import path has no dot in its
// first element. go/build is deliberately not used: the invariants package stays dependency-free.
func isStandardLibrary(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// checkDependencyAllowlist is STANDARDS section 9: go.mod requires only allowlisted modules.
func checkDependencyAllowlist(root string) error {
	allowed := []string{
		"github.com/charmbracelet/log",
		"github.com/spf13/cobra",
		"go.yaml.in/yaml/v3",
		"golang.org/x/net",
	}

	data, err := os.ReadFile(filepath.Join(root, "go.mod")) //nolint:gosec // a fixed path under the root being checked
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "require (") {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}

		var module string
		switch {
		case inBlock && line != "" && !strings.HasPrefix(line, "//"):
			module = strings.Fields(line)[0]
		case strings.HasPrefix(line, "require "):
			module = strings.Fields(strings.TrimPrefix(line, "require "))[0]
		default:
			continue
		}

		// Indirect requirements come with an allowed module; only what we choose is our decision.
		if strings.Contains(line, "// indirect") {
			continue
		}
		if !moduleAllowed(module, allowed) {
			return fmt.Errorf("go.mod requires %q, which is not on the allowlist in docs/STANDARDS.md section 9", module)
		}
	}
	return nil
}

func moduleAllowed(module string, allowed []string) bool {
	for _, entry := range allowed {
		if module == entry || strings.HasPrefix(module, entry+"/") {
			return true
		}
	}
	return false
}

// checkNoAuthSurface is decision #10: the gateway and control packages never grow authentication.
func checkNoAuthSurface(root string) error {
	terms := []string{"cookie", "csrf", "login", "password", "session"}
	guarded := []string{"internal/control", "internal/gateway"}

	return walkGoFiles(root, func(f goFile) error {
		if !underAny(f.rel, guarded) {
			return nil
		}
		var violation error
		ast.Inspect(f.file, func(node ast.Node) bool {
			if violation != nil {
				return false
			}
			var text string
			switch node := node.(type) {
			case *ast.Ident:
				text = node.Name
			case *ast.BasicLit:
				if node.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(node.Value)
				if err != nil {
					return true
				}
				text = value
			default:
				return true
			}
			for _, term := range terms {
				if strings.Contains(strings.ToLower(text), term) {
					violation = fmt.Errorf("%s: %q looks like authentication, which is ruled out by decision #10",
						f.pos(node), text)
					return false
				}
			}
			return true
		})
		return violation
	})
}

// checkNoInPlaceWrites is decision #36: JSON handoffs are written through internal/atomicfile.
func checkNoInPlaceWrites(root string) error {
	guarded := []string{"internal/control", "internal/hostexec", "internal/hostprobe"}

	return walkGoFiles(root, func(f goFile) error {
		if !underAny(f.rel, guarded) {
			return nil
		}
		var violation error
		ast.Inspect(f.file, func(node ast.Node) bool {
			if violation != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := qualifiedName(call.Fun)
			if !ok || (name != "os.WriteFile" && name != "ioutil.WriteFile") {
				return true
			}
			violation = fmt.Errorf("%s: %s writes in place; use internal/atomicfile (decision #36)", f.pos(call), name)
			return false
		})
		return violation
	})
}

// checkOneMutatingRoute is decision #34: the launcher has no state-changing routes, and control has
// exactly one.
func checkOneMutatingRoute(root string) error {
	routeLiteral := regexp.MustCompile(`^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS) (/\S*)$`)
	const onlyMutatingRoute = "POST /api/control"

	return walkGoFiles(root, func(f goFile) error {
		if !underAny(f.rel, []string{"internal/gateway"}) {
			return nil
		}
		var violation error
		ast.Inspect(f.file, func(node ast.Node) bool {
			if violation != nil {
				return false
			}
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			match := routeLiteral.FindStringSubmatch(value)
			if match == nil {
				return true
			}
			method, path := match[1], match[2]
			if method == "GET" || method == "HEAD" || (method == "POST" && path == "/api/control") {
				return true
			}
			violation = fmt.Errorf("%s: route %q is not allowed; the only mutating route is %q (decision #34)",
				f.pos(literal), value, onlyMutatingRoute)
			return false
		})
		return violation
	})
}

// checkExitOnlyInCmd is STANDARDS section 5: only cmd/ decides to exit the process.
func checkExitOnlyInCmd(root string) error {
	return walkGoFiles(root, func(f goFile) error {
		if underAny(f.rel, []string{"cmd"}) {
			return nil
		}
		var violation error
		ast.Inspect(f.file, func(node ast.Node) bool {
			if violation != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := qualifiedName(call.Fun)
			if !ok {
				return true
			}
			switch name {
			case "log.Fatal", "log.Fatalf", "log.Fatalln", "os.Exit":
				violation = fmt.Errorf("%s: %s outside cmd/; return an error or an exit code instead", f.pos(call), name)
				return false
			}
			return true
		})
		return violation
	})
}
