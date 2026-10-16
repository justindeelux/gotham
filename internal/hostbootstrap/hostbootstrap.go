// Package hostbootstrap installs and refreshes the fixed, root-owned host
// helper (gotham-hostctl) and host-tool installer (gotham-toolctl) together with
// their verb-pinned sudoers drop-ins. It is the single source of truth for that
// bootstrap: the gotham binary runs it as root at control-plane start
// (ExecStartPre=+), so an instance needs no manual step and repairs itself on
// every start, including boxes whose agent was never installed with
// install-agent.sh.
//
// Everything it writes is fixed: fixed paths, fixed embedded content, and fixed
// sudoers rules for two service users. It reads no environment or request data.
// Each file is written to a temp file in the destination directory and renamed
// into place, a symlinked target is refused, and a sudoers drop-in is validated
// with visudo before it is moved, so a broken sudoers file is never left behind.
package hostbootstrap

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/justindeelux/gotham/deploy"
)

// Fixed absolute paths. The sudoers content names these same paths, because
// sudo matches the command line exactly.
const (
	HostctlPath = "/usr/libexec/gotham/gotham-hostctl"
	ToolctlPath = "/usr/libexec/gotham/gotham-toolctl"

	hostctlDistPath    = "/usr/libexec/gotham/gotham-hostctl.dist"
	hostctlSudoersSrc  = "/usr/libexec/gotham/gotham-hostctl.sudoers"
	toolctlSudoersSrc  = "/usr/libexec/gotham/gotham-toolctl.sudoers"
	HostctlSudoersDrop = "/etc/sudoers.d/gotham-hostctl"
	ToolctlSudoersDrop = "/etc/sudoers.d/gotham-toolctl"

	helperDir      = "/usr/libexec/gotham"
	sudoersDropDir = "/etc/sudoers.d"
)

// Default users. CpUser runs the control plane and invokes the host helper;
// AgentUser runs the node agent and invokes the tool installer.
const (
	DefaultCpUser    = "gotham"
	DefaultAgentUser = "gotham-agent"
)

// hostctlVerbs is the closed verb set the control plane may run hostctl with.
var hostctlVerbs = []string{"status", "apply-network", "confirm-network", "revert-network", "apply-system"}

// Tool ids the node agent may install/remove through the tool installer. They
// mirror deploy/gotham-toolctl.sh; gotham-hostctl is deliberately agent-refused
// (the control plane owns its install).
var (
	agentInstallIDs   = []string{"railpack", "buildkit", "pack", "docker", "git"}
	agentUninstallIDs = []string{"railpack", "buildkit", "pack", "git"}
)

// Options configures one bootstrap run.
type Options struct {
	// CpUser is the control-plane service user granted the host-helper verbs
	// (and the single pinned repair verb). Defaults to DefaultCpUser.
	CpUser string
	// AgentUser is the node-agent service user granted the tool-installer
	// verbs. Defaults to DefaultAgentUser.
	AgentUser string
	// Root is a test/alternate root prefix; "" writes to the real "/".
	Root string
	// Visudo is the validator path; empty resolves "visudo" on PATH.
	Visudo string
	// Logf receives progress lines; nil is silent.
	Logf func(string, ...any)
}

// Run installs or refreshes every artifact. It is idempotent: a second run with
// the same options writes nothing new and returns nil.
func Run(opts Options) error {
	cpUser := orDefault(opts.CpUser, DefaultCpUser)
	agentUser := orDefault(opts.AgentUser, DefaultAgentUser)
	for name, user := range map[string]string{"control-plane user": cpUser, "agent user": agentUser} {
		if !validUser(user) {
			return fmt.Errorf("host-bootstrap: invalid %s %q", name, user)
		}
	}
	visudo, err := resolveVisudo(opts.Visudo)
	if err != nil {
		return err
	}

	for _, dir := range []string{helperDir, sudoersDropDir} {
		if err := ensureDir(opts.Root, dir); err != nil {
			return err
		}
	}

	// helpers: the live host helper, its staged repair copy, and the tool
	// installer. All root-owned 0755.
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{HostctlPath, deploy.HostctlScript, 0o755},
		{hostctlDistPath, deploy.HostctlScript, 0o755},
		{ToolctlPath, deploy.ToolctlScript, 0o755},
		{hostctlSudoersSrc, hostctlSudoers(cpUser), 0o644},
		{toolctlSudoersSrc, toolctlSudoers(agentUser), 0o644},
	}
	for _, f := range files {
		if err := installFile(opts.Root, f.path, f.data, f.mode, ""); err != nil {
			return err
		}
		opts.logf("installed %s", f.path)
	}

	// sudoers drop-ins: validated by visudo before the rename.
	drops := []struct {
		path    string
		content []byte
	}{
		{HostctlSudoersDrop, hostctlSudoers(cpUser)},
		{ToolctlSudoersDrop, toolctlSudoers(agentUser)},
	}
	for _, d := range drops {
		if err := installFile(opts.Root, d.path, d.content, 0o440, visudo); err != nil {
			return err
		}
		opts.logf("installed %s", d.path)
	}
	return nil
}

// hostctlSudoers renders the control-plane drop-in: the closed host-helper verb
// set plus the single pinned tool-installer verb used to repair a missing or
// denied helper in-product.
func hostctlSudoers(cpUser string) []byte {
	var b strings.Builder
	b.WriteString("# Allow the Gotham control plane to run its fixed host helper verbs,\n")
	b.WriteString("# plus the one pinned tool-installer verb used to repair the helper.\n")
	for _, verb := range hostctlVerbs {
		fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s %s\n", cpUser, HostctlPath, verb)
	}
	fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s install-tool gotham-hostctl\n", cpUser, ToolctlPath)
	return []byte(b.String())
}

// toolctlSudoers renders the node-agent drop-in: one rule per verb and tool id,
// no wildcards.
func toolctlSudoers(agentUser string) []byte {
	var b strings.Builder
	b.WriteString("# Allow the Gotham node agent to run its fixed host-tool helper.\n")
	for _, id := range agentInstallIDs {
		fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s install-tool %s\n", agentUser, ToolctlPath, id)
	}
	for _, id := range agentUninstallIDs {
		fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s uninstall-tool %s\n", agentUser, ToolctlPath, id)
	}
	return []byte(b.String())
}

// installFile writes data to path via a temp file in the same directory and an
// atomic rename. A symlinked target or directory is refused. When validator is
// non-empty it is run as "<validator> -cf <temp>" and a failure aborts before
// the rename. An already-current regular file is left untouched.
func installFile(root, path string, data []byte, mode os.FileMode, validator string) error {
	dir := filepath.Dir(path)
	if err := refuseSymlink(rooted(root, dir)); err != nil {
		return err
	}
	target := rooted(root, path)
	if fi, err := os.Lstat(target); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("host-bootstrap: refusing to replace symlinked %s", path)
		}
		if fi.Mode().IsRegular() && fi.Mode().Perm() == mode {
			if old, err := os.ReadFile(target); err == nil && bytes.Equal(old, data) {
				return nil
			}
		}
	}
	tmp, err := os.CreateTemp(rooted(root, dir), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("host-bootstrap: temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("host-bootstrap: write %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("host-bootstrap: chmod %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("host-bootstrap: sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("host-bootstrap: close %s: %w", path, err)
	}
	if validator != "" {
		if out, err := exec.Command(validator, "-cf", tmpName).CombinedOutput(); err != nil {
			return fmt.Errorf("host-bootstrap: %s rejected %s: %s", filepath.Base(validator), path, strings.TrimSpace(string(out)))
		}
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("host-bootstrap: install %s: %w", path, err)
	}
	return nil
}

// ensureDir creates dir (under root) mode 0755 unless it already exists, and
// refuses a symlinked path component it would create.
func ensureDir(root, dir string) error {
	path := rooted(root, dir)
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("host-bootstrap: refusing symlinked directory %s", dir)
		}
		if !fi.IsDir() {
			return fmt.Errorf("host-bootstrap: %s exists and is not a directory", dir)
		}
		return nil
	}
	mode := os.FileMode(0o755)
	if dir == sudoersDropDir {
		// sudoers(5): the drop-in directory is root-owned 0750.
		mode = 0o750
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("host-bootstrap: create %s: %w", dir, err)
	}
	return nil
}

// refuseSymlink fails when path is a symlink. A root prefix adds ancestors a
// test created itself, so only the final component is checked.
func refuseSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("host-bootstrap: refusing symlinked directory %s", path)
	}
	return nil
}

// resolveVisudo returns the visudo path, or an error when none is available:
// without visudo a sudoers drop-in is never written.
func resolveVisudo(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	path, err := exec.LookPath("visudo")
	if err != nil {
		return "", fmt.Errorf("host-bootstrap: visudo is required to validate the sudoers drop-ins: %w", err)
	}
	return path, nil
}

// rooted joins root and an absolute path; "" root keeps the absolute path.
func rooted(root, abs string) string {
	if root == "" {
		return abs
	}
	return filepath.Join(root, abs)
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// validUser matches the charset install-sudoers.sh accepts.
func validUser(user string) bool {
	if user == "" {
		return false
	}
	for _, r := range user {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}
