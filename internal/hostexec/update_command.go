package hostexec

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/autumnsgrove/meridian/internal/hostfile"
)

// UpdateMain runs `meridian-host update`: the self-update command the executor starts as the deploy
// user. It takes no target argument because the allowlist may only name one self-update target; the
// command finds it, re-reads every field from /etc/meridian, and never trusts the container's hint.
func UpdateMain(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	host := flags.String("host", DefaultHostFile, "the host configuration (root-owned)")
	targets := flags.String("targets", DefaultTargetsFile, "the allowlist (root-owned)")
	flags.Usage = func() {
		fmt.Fprint(stderr, "usage: meridian-host update [--host PATH] [--targets PATH]\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "meridian-host update: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return 2
	}
	// The compose commands need the deploy user's docker group and its ownership of the project
	// directory. Root would leave root-owned files that the next update cannot write (decision #31).
	if os.Geteuid() == 0 {
		fmt.Fprintln(stderr, "meridian-host update: run as the deploy user, not root")
		return 1
	}

	logger := log.New(stderr, "meridian-host update: ", 0)
	trust := hostfile.Trust{Owner: 0, MaxSize: MaxTrustedSize}

	hostData, err := hostfile.Read(*host, trust)
	if err != nil {
		return updateRefused(logger, err)
	}
	hostConfig, err := LoadHost(hostData)
	if err != nil {
		return updateRefused(logger, err)
	}
	targetsData, err := hostfile.Read(*targets, trust)
	if err != nil {
		return updateRefused(logger, err)
	}
	loaded, err := LoadTargets(targetsData, hostConfig.DeployUser)
	if err != nil {
		return updateRefused(logger, err)
	}
	target, err := SelfUpdateTarget(loaded)
	if err != nil {
		return updateRefused(logger, err)
	}

	updater := &Updater{
		Target:     target,
		DeployUser: hostConfig.DeployUser,
		Run:        Runner(logger.Printf),
		Log:        logger,
	}
	if err := updater.Apply(context.Background()); err != nil {
		logger.Print(err)
		return 1
	}
	return 0
}

// SelfUpdateTarget finds the one allowlist entry whose update is Meridian itself. Validation allows
// at most one, so a second here means the file changed under a running binary and is refused.
func SelfUpdateTarget(targets Targets) (Target, error) {
	var found Target
	foundOne := false
	for id := range targets {
		target := targets[id]
		if target.Update == nil || target.Update.Kind != UpdateSelf {
			continue
		}
		if foundOne {
			return Target{}, errors.New("hostexec: the allowlist names more than one self update target")
		}
		found, foundOne = target, true
	}
	if !foundOne {
		return Target{}, errors.New("hostexec: the allowlist names no self update target")
	}
	return found, nil
}

// updateRefused logs why the command will not touch anything and returns the process status.
func updateRefused(logger *log.Logger, err error) int {
	logger.Print(err)
	return 1
}
