//go:build !unix

package deploy

import "os/exec"

// configureGitCommand is the non-Unix fallback: no process groups or
// parent-death signal, so cancellation kills only the direct child (the
// default exec.CommandContext behaviour).
func configureGitCommand(*exec.Cmd) func() { return func() {} }
