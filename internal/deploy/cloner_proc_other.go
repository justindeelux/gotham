//go:build !unix

package deploy

import "os/exec"

func configureGitCommand(*exec.Cmd) func() { return func() {} }
