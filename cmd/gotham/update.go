package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/updates"
)

// runUpdate implements `gotham update <check|apply|rollback>`: the operator
// path for the same check/apply/rollback chain the HTTP routes expose.
func runUpdate(args []string) int {
	if len(args) == 0 {
		updateUsage(os.Stderr)
		return exitUsage
	}

	if _, err := config.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	updateConfig, keyErr := updates.FromEnv(version, logger)
	if keyErr != nil {
		logger.Warn("updates: release public key not configured; apply disabled", "reason", keyErr)
	}
	svc, err := updates.NewService(updateConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: %v\n", err)
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "check":
		return runUpdateCheck(ctx, svc)
	case "apply":
		if err := requireBinaryOwner("apply"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitError
		}
		return runUpdateApply(ctx, svc, args[1:])
	case "rollback":
		if err := requireBinaryOwner("rollback"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitError
		}
		if err := svc.Rollback(); err != nil {
			fmt.Fprintf(os.Stderr, "update rollback: %v\n", err)
			return exitError
		}
		fmt.Println("rolled back to the previous binary")
		fmt.Println("the running process keeps the current binary until it restarts; run `systemctl restart gotham` to run the rolled-back binary")
		return exitOK
	case "reset":
		if err := svc.Reset(); err != nil {
			fmt.Fprintf(os.Stderr, "update reset: %v\n", err)
			return exitError
		}
		fmt.Println("cleared the pending update marker")
		return exitOK
	case "help", "-h", "--help":
		updateUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown update command %q\n\n", args[0])
		updateUsage(os.Stderr)
		return exitUsage
	}
}

// runUpdateCheck reports the available version, if any.
func runUpdateCheck(ctx context.Context, svc updates.Service) int {
	release, err := svc.Check(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check: %v\n", err)
		return exitError
	}
	if release == nil {
		fmt.Printf("gotham %s is up to date\n", svc.Current())
		return exitOK
	}
	fmt.Printf("gotham %s available (current %s, channel %s)\n", release.Version, svc.Current(), release.Channel)
	return exitOK
}

// runUpdateApply applies the newest release, optionally on a chosen channel.
func runUpdateApply(ctx context.Context, svc updates.Service, args []string) int {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	channel := flags.String("channel", "", "release channel: stable or beta")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	result, err := svc.Apply(ctx, updates.Channel(*channel))
	if err != nil {
		fmt.Fprintf(os.Stderr, "update apply: %v\n", err)
		return exitError
	}
	if !result.Applied {
		fmt.Printf("gotham %s is up to date\n", result.Version)
		return exitOK
	}
	if result.Staged {
		fmt.Printf("staged %s; the restart wrapper will health-check it and record the outcome\n", result.Version)
		return exitOK
	}
	fmt.Printf("updated to %s\n", result.Version)
	return exitOK
}

// requireBinaryOwner refuses to swap a binary the invoking user does not own.
// Run as root, the atomic swap installs a root:root 0755 binary over the
// service user's binary, and the next service-run apply then fails at the
// hardlink backup step (fs.protected_hardlinks=1 -> EPERM). Refusing is safer
// than silently poisoning ownership; run as the service user instead:
//
//	sudo -u gotham /var/lib/gotham/bin/gotham update apply
//
// A missing binary (fresh install) is allowed: there is nothing to compare yet.
func requireBinaryOwner(command string) error {
	return checkBinaryOwner(updates.BinaryPathFromEnv(), os.Geteuid(), command)
}

// checkBinaryOwner is the testable core of requireBinaryOwner: it compares the
// binary owner to euid and returns a clear message on a mismatch.
func checkBinaryOwner(path string, euid int, command string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) == euid {
		return nil
	}
	owner := strconv.Itoa(int(stat.Uid))
	if u, lookupErr := user.LookupId(owner); lookupErr == nil {
		owner = u.Username
	}
	return fmt.Errorf(
		"refusing to update %s: it is owned by uid %d (%s) but this process runs as uid %d; "+
			"run as the service user, e.g. sudo -u %s %s update %s",
		path, stat.Uid, owner, euid, owner, path, command)
}

// updateUsage prints the `gotham update` help.
func updateUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham update <command>

Commands:
  check                 Report whether a newer release is available
  apply [-channel ...]  Download, verify and swap in the newest release
  rollback              Restore the previous binary (kept as <binary>.old)
  reset                 Clear a stale pending-update marker (operator reset)

Configuration is read from the environment (see gotham.example.yaml).
`)
}
