package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	case "exists":
		return runAdminExists(args[1:])
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
  gotham admin create --email <email> [--password <pw> | --password-stdin | --generate-password] [--force]
                            Create the first account (P-A2: one account per
                            instance; refuses with --force not set otherwise).
  gotham admin exists        Print "admin-exists: true|false" and exit 0
                            (exit 1 on error). Lets the installer detect a
                            re-run without parsing the database.
  gotham admin reset-password --email <email> [--password <pw>]
                            Replace a password and revoke the account's
                            sessions.
  gotham admin help         Show this help

The password is read with a hidden prompt when none of --password,
--password-stdin or --generate-password is given. --password is visible in
the process list; prefer the hidden prompt or --password-stdin (a single
line on stdin). --generate-password creates a random password and prints it
once as "generated-password: <value>".
Configuration comes from gotham.yaml / GOTHAM_* environment variables; when
%s exists it is loaded first (without overriding the environment).
`, adminEnvFile)
}

// runAdminCreate creates an account, refusing once the instance has one
// unless --force is given (P-A2: exactly one admin account).
func runAdminCreate(args []string) int {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	email := fs.String("email", "", "account email (required)")
	password := fs.String("password", "", "account password (visible in ps; prefer --password-stdin or the hidden prompt)")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from stdin (single line, no echo)")
	generatePassword := fs.Bool("generate-password", false, "generate a random password and print it once as \"generated-password: <value>\"")
	force := fs.Bool("force", false, "create the account even when one already exists")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := checkAdminPasswordFlags(*password, *passwordStdin, *generatePassword); err != nil {
		fmt.Fprintf(os.Stderr, "admin create: %v\n", err)
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

		var generated string
		hash, err := func() (string, error) {
			if *generatePassword {
				generated, err = generateAdminPassword()
				if err != nil {
					return "", err
				}
				return adminHashPlaintext(generated)
			}
			if *passwordStdin {
				plaintext, err := readPasswordLine(os.Stdin)
				if err != nil {
					return "", err
				}
				return adminHashPlaintext(plaintext)
			}
			return adminPasswordHash(*password, "New password: ", "Confirm password: ")
		}()
		if err != nil {
			fmt.Fprintf(os.Stderr, "admin create: %v\n", err)
			return exitError
		}

		// The count above is a friendly pre-check; the insert itself carries the
		// same atomic first-account guard as web registration, so two operators
		// (or an operator racing a registration) cannot both win without
		// --force.
		if *force {
			_, err = st.CreateUser(ctx, normalized, &hash)
		} else {
			_, err = st.CreateFirstUser(ctx, normalized, &hash)
			if errors.Is(err, store.ErrInstanceHasAccount) {
				fmt.Fprintf(os.Stderr, "admin create: this instance already has an account; "+
					"invite members instead, or pass --force\n")
				return exitError
			}
		}
		if err != nil {
			if isUniqueViolation(err) {
				fmt.Fprintf(os.Stderr, "admin create: email already registered\n")
				return exitError
			}
			fmt.Fprintf(os.Stderr, "admin create: create user: %v\n", err)
			return exitError
		}

		fmt.Printf("created account %s\n", normalized)
		if generated != "" {
			fmt.Printf("generated-password: %s\n", generated)
		}
		return exitOK
	})
}

// runAdminExists reports whether the instance already has an account, so the
// installer can skip creation on a re-run without parsing the database. It
// prints "admin-exists: true|false" and exits 0; any failure exits 1.
func runAdminExists(args []string) int {
	fs := flag.NewFlagSet("admin exists", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "admin exists takes no arguments\n")
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

	count, err := store.New(pool).CountUsers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "admin exists: count users: %v\n", err)
		return exitError
	}
	if count > 0 {
		fmt.Println("admin-exists: true")
	} else {
		fmt.Println("admin-exists: false")
	}
	return exitOK
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

		hash, err := adminPasswordHash(*password, "New password: ", "Confirm password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "admin reset-password: %v\n", err)
			return exitError
		}

		if err := st.ResetUserPassword(ctx, user.ID, user.Email, hash); err != nil {
			fmt.Fprintf(os.Stderr, "admin reset-password: %v\n", err)
			return exitError
		}

		fmt.Printf("password updated for %s; refresh sessions were revoked. "+
			"Access tokens already issued stay valid until they expire (15 minutes).\n", user.Email)
		return exitOK
	})
}

// checkAdminPasswordFlags rejects combining --password, --password-stdin and
// --generate-password: they are mutually exclusive password sources.
func checkAdminPasswordFlags(password string, passwordStdin, generatePassword bool) error {
	n := 0
	if password != "" {
		n++
	}
	if passwordStdin {
		n++
	}
	if generatePassword {
		n++
	}
	if n > 1 {
		return errors.New("--password, --password-stdin and --generate-password are mutually exclusive")
	}
	return nil
}

// adminHashPlaintext validates an already-resolved password with the same
// policy as registration and returns its argon2id-encoded hash. Unlike
// adminPasswordHash it never prompts: an empty value fails validation.
func adminHashPlaintext(password string) (string, error) {
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return auth.HashPassword(password)
}

// readPasswordLine reads a single password line from r without echo handling
// (the caller pipes it, so nothing is displayed). Only the trailing newline
// is stripped; every other byte is significant.
func readPasswordLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" && errors.Is(err, io.EOF) {
		return "", fmt.Errorf("no password on stdin; pipe a single line or use --generate-password")
	}
	return line, nil
}

// generateAdminPassword returns a 32-character random password (24 bytes from
// crypto/rand, base64url without padding). It satisfies the shared password
// policy (length bounds) and never touches the process list or disk.
func generateAdminPassword() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}

// adminPasswordHash resolves the password (the flag value when given, otherwise
// a hidden double prompt on the terminal), validates it with the same policy as
// registration, and returns the argon2id-encoded hash the store expects. The
// plaintext never leaves this function.
func adminPasswordHash(password, prompt, confirm string) (string, error) {
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
	return auth.HashPassword(password)
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
