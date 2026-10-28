package hostexec

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/autumnsgrove/meridian/internal/atomicfile"
)

// UnitValues are the installer's values for the deploy templates. They are a closed set: a template
// that invents a new placeholder fails the render instead of shipping an @…@ to systemd.
type UnitValues struct {
	ControlDir string
	DeployUser string
	StatusDir  string
}

// placeholderPattern finds an @NAME@ the replacer did not know. A bare @ in a path or an email-like
// comment is allowed; only the template form is a failure.
var placeholderPattern = regexp.MustCompile(`@[A-Z][A-Z0-9_]*@`)

// RenderUnits renders the systemd and sudoers templates under templatesDir into outputDir. The shell
// installer calls this instead of sed, so the placeholders are one closed list and a unit test can
// prove every template is complete.
func RenderUnits(templatesDir, outputDir string, values UnitValues) error {
	if err := values.validate(); err != nil {
		return err
	}
	if outputDir == "" {
		return errors.New("hostexec: render-units: no output directory")
	}

	rendered := 0
	for _, sub := range []string{"systemd", "sudoers"} {
		entries, err := os.ReadDir(filepath.Join(templatesDir, sub))
		if err != nil {
			return fmt.Errorf("hostexec: render-units: read %s templates: %w", sub, err)
		}
		targetDir := filepath.Join(outputDir, sub)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("hostexec: render-units: create %s: %w", targetDir, err)
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			body, err := os.ReadFile(filepath.Join(templatesDir, sub, entry.Name())) //nolint:gosec // the templates are the repo's own files
			if err != nil {
				return fmt.Errorf("hostexec: render-units: read %s: %w", entry.Name(), err)
			}
			complete, err := renderTemplate(body, values)
			if err != nil {
				return fmt.Errorf("hostexec: render-units: %s/%s: %w", sub, entry.Name(), err)
			}
			// Systemd units are world-readable by design; the installer gives the sudoers file 0440.
			if err := atomicfile.Write(targetDir, entry.Name(), complete, 0o644); err != nil {
				return fmt.Errorf("hostexec: render-units: write %s/%s: %w", sub, entry.Name(), err)
			}
			rendered++
		}
	}
	if rendered == 0 {
		return errors.New("hostexec: render-units: no templates found")
	}
	return nil
}

// renderTemplate replaces the closed placeholder list and refuses anything left in the template form.
func renderTemplate(body []byte, values UnitValues) ([]byte, error) {
	replacer := strings.NewReplacer(
		"@CONTROL_DIR@", values.ControlDir,
		"@DEPLOY_USER@", values.DeployUser,
		"@STATUS_DIR@", values.StatusDir,
	)
	complete := replacer.Replace(string(body))
	if leftover := placeholderPattern.FindString(complete); leftover != "" {
		return nil, fmt.Errorf("%s has no value", leftover)
	}
	return []byte(complete), nil
}

// validate refuses a value that would escape its slot: a relative path, a newline that would become a
// second unit directive, or a user name that is not one.
func (values UnitValues) validate() error {
	for name, path := range map[string]string{
		"control dir": values.ControlDir,
		"status dir":  values.StatusDir,
	} {
		if !isCleanAbs(path) || strings.ContainsAny(path, "\r\n") {
			return fmt.Errorf("%s %q must be an absolute, clean path without a newline", name, path)
		}
	}
	if !userNameGrammar.MatchString(values.DeployUser) {
		return fmt.Errorf("deploy user %q is not a user name", values.DeployUser)
	}
	return nil
}

// RenderUnitsMain runs `meridian-host render-units`. The installer passes every value explicitly;
// there are no defaults because a wrong default would install a unit pointing at the wrong directory.
func RenderUnitsMain(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("render-units", flag.ContinueOnError)
	flags.SetOutput(stderr)
	templates := flags.String("templates", "", "the deploy directory holding systemd/ and sudoers/")
	output := flags.String("output", "", "the directory to write the rendered files into")
	controlDir := flags.String("control-dir", "", "the host's control directory")
	deployUser := flags.String("deploy-user", "", "the account the units run as")
	statusDir := flags.String("status-dir", "", "the host's status directory")
	flags.Usage = func() {
		fmt.Fprint(stderr, "usage: meridian-host render-units --templates DIR --output DIR "+
			"--control-dir PATH --deploy-user NAME --status-dir PATH\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "meridian-host render-units: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return 2
	}
	if *templates == "" || *output == "" || *controlDir == "" || *deployUser == "" || *statusDir == "" {
		flags.Usage()
		return 2
	}

	err := RenderUnits(*templates, *output, UnitValues{
		ControlDir: *controlDir,
		DeployUser: *deployUser,
		StatusDir:  *statusDir,
	})
	if err != nil {
		fmt.Fprintf(stderr, "meridian-host render-units: %v\n", err)
		return 1
	}
	return 0
}
