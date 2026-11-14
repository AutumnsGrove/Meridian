package hostexec

import "testing"

// TestComposeRunAs_DropsPrivilegesOnlyWhenRoot pins the rule a self update broke on a real box. The
// executor is root and drops to the deploy user so no root-owned files appear in the project
// (decision #31), but builders.go's updateBySelf re-executes `meridian-host update` *as* the deploy
// user already, and asking syscall to setuid into the account we are already in fails with EPERM
// before docker is ever forked. It surfaced as "fork/exec /usr/bin/docker: operation not permitted"
// inside an update that had already pinned the digest.
func TestComposeRunAs_DropsPrivilegesOnlyWhenRoot(t *testing.T) {
	tests := []struct {
		name string
		euid int
		want string
	}{
		{name: "the executor itself is root", euid: 0, want: "autumn"},
		{name: "already re-executed as the deploy user", euid: 1000, want: ""},
		{name: "some other unprivileged uid", euid: 4242, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := composeRunAs("autumn", test.euid); got != test.want {
				t.Errorf("composeRunAs(\"autumn\", %d) = %q, want %q", test.euid, got, test.want)
			}
		})
	}
}
