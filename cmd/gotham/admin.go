package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/term"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/store"
)

// adminEnvFile is the service environment file the installer maintains. The
// admin CLI reads it when run outside systemd (systemd itself exports it via
// EnvironmentFile), so `sudo gotham admin create` finds GOTHAM_DATABASE_DSN.
const adminEnvFile = "/etc/gotham/gotham.env"

// runAdmin implements `gotham admin`: operator account management on the
// server host (P-A3).
func runAdmin(args []string) int {
	if len(args) == 0 {
		adminUsage(os.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "create":
		return runAdminCreate(args[1:])
	case "reset-password":
		return runAdminResetPassword(args[1:])
	case "help", "-h", "--help":
		adminUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown admin command %q\n\n", args[0])
		adminUsage(os.Stderr)
		return exitUsage
	}
}

// adminUsage prints the admin subcommand help.
func adminUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham admin

Usage:
  gotham admin create --email <email> [--password <pw>] [--force]
                            Create the first account (P-A2: one account per
                            instance; refuses with --force not set otherwise).
  gotham admin reset-password --email <email> [--password <pw>]
                            Replace a password and revoke the account's
                            sessions.
  gotham admin help         Show this help

The password is read with a hidden prompt when --password is omitted.
Configuration comes from gotham.yaml / GOTHAM_* environment variables; when
%s exists it is loaded first (without overriding the environment).
`, adminEnvFile)
}

// runAdminCreate creates an account, refusing once the instance has one
// unless --force is given (P-A2: exactly one admin account).
func runAdminCreate(args []string) int {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	email := fs.String("email", "", "account email (required)")
	password := fs.String("password", "", "account password (hidden prompt when omitted)")
	force := fs.Bool("force", false, "create the account even when one already exists")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	return withAdminStore(fs, func(ctx context.Context, st *store.Store) int {
		normalized, err := auth.NormalizeEmail(*email)
		if err != nil {
			fmt.Fprintf(os.Stderr, "admin create: %v\n", err)
			return exitError
		}

		count, err := st.CountUsers(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "admin create: count users: %v\n", err)
			return exitError
		}
		if count > 0 && !*force {
			fmt.Fprintf(os.Stderr, "admin create: this instance already has %d account(s); "+
				"registration is closed (invite members instead, or pass --force)\n", count)
			return exitError
		}

		hash, err := adminPassword(*password, "New password: ", "Confirm password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "admin create: %v\n", err)
			return exitError
		}

		if _, err := st.CreateUser(ctx, normalized, &hash); err != nil {
			if isUniqueViolation(err) {
				fmt.Fprintf(os.Stderr, "admin create: email already registered\n")
				return exitError
			}
			fmt.Fprintf(os.Stderr, "admin create: create user: %v\n", err)
			return exitError
		}

		fmt.Printf("created account %s\n", normalized)
		return exitOK
	})
}

// runAdminResetPassword replaces the argon2id hash of an existing account and
// deletes its sessions, so the old refresh chain dies with the password.
func runAdminResetPassword(args []string) int {
	fs := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	email := fs.String("email", "", "account email (required)")
	password := fs.String("password", "", "new password (hidden prompt when omitted)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	return withAdminStore(fs, func(ctx context.Context, st *store.Store) int {
		user, err := st.GetUserByEmail(ctx, strings.TrimSpace(*email))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				fmt.Fprintf(os.Stderr, "admin reset-password: no account with email %q\n", *email)
				return exitError
			}
			fmt.Fprintf(os.Stderr, "admin reset-password: get user: %v\n", err)
			return exitError
		}

		hash, err := adminPassword(*password, "New password: ", "Confirm password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "admin reset-password: %v\n", err)
			return exitError
		}

		if err := st.UpdateUserPasswordHash(ctx, user.Email, hash); err != nil {
			fmt.Fprintf(os.Stderr, "admin reset-password: update password: %v\n", err)
			return exitError
		}
		if err := st.DeleteUserSessions(ctx, user.ID); err != nil {
			fmt.Fprintf(os.Stderr, "admin reset-password: revoke sessions: %v\n", err)
			return exitError
		}

		fmt.Printf("password updated for %s; existing sessions were revoked\n", user.Email)
		return exitOK
	})
}

// adminPassword resolves the password: the flag value when given, otherwise a
// hidden double prompt on the terminal. It validates with the same policy as
// registration.
func adminPassword(password, prompt, confirm string) (string, error) {
	if password == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", fmt.Errorf("no terminal for the password prompt; pass --password")
		}
		first, err := readPassword(prompt)
		if err != nil {
			return "", err
		}
		if password, err = readPassword(confirm); err != nil {
			return "", err
		}
		if first != password {
			return "", errors.New("passwords do not match")
		}
	}
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}

// readPassword prints prompt and reads a hidden answer from the terminal.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(line), nil
}

// withAdminStore loads the configuration (including the service env file), and
// runs fn with an open store. It parses the flags first so --env-file and
// invalid flag errors short-circuit before any database work.
func withAdminStore(fs *flag.FlagSet, fn func(ctx context.Context, st *store.Store) int) int {
	if fs.NFlag() == 0 && fs.NArg() == 0 {
		adminUsage(os.Stderr)
		return exitUsage
	}
	loadEnvFile(adminEnvFile)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := store.Open(ctx, cfg.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "admin: connect to database: %v\n", err)
		return exitError
	}
	defer pool.Close()

	return fn(ctx, store.New(pool))
}

// loadEnvFile applies KEY=VALUE lines from the service environment file. It is
// silent when the file is missing and never overrides an already-set variable,
// so the shell always wins.
func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
	}
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
