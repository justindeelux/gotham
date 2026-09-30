package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
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
		return runUpdateApply(ctx, svc, args[1:])
	case "rollback":
		if err := svc.Rollback(); err != nil {
			fmt.Fprintf(os.Stderr, "update rollback: %v\n", err)
			return exitError
		}
		fmt.Println("rolled back to the previous binary")
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
