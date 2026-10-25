package main

import (
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

const adminEnvFile = "/etc/gotham/gotham.env"

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

func adminUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham admin

Usage:
  gotham admin create --email <email> [--password <pw> | --password-stdin | --generate-password] [--force]
                            Create the first account (the instance owner;
                            one account per instance; refuses with --force not
                            set otherwise).
  gotham admin exists        Print "admin-exists: true|false" and exit 0
                            (exit 1 on error). Lets the installer detect a
                            re-run without parsing the database.
  gotham admin reset-password --email <email> [--password <pw>]
                            Replace a password, revoke the account's sessions
                            and clear its two-factor authentication (the
                            recovery path for a lost authenticator device).
  gotham admin help         Show this help

The password is read with a hidden prompt when none of --password,
--password-stdin or --generate-password is given. --password is visible in
the process list; prefer the hidden prompt or --password-stdin (a single
piped line, refused on a terminal). --generate-password creates a random
password and prints it once as "generated-password: <value>".
Configuration comes from gotham.yaml / GOTHAM_* environment variables; when
%s exists it is loaded first (without overriding the environment).
`, adminEnvFile)
}

func runAdminCreate(args []string) int {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	email := fs.String("email", "", "account email (required)")
	password := fs.String("password", "", "account password (visible in ps output; prefer the hidden prompt)")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from stdin (single piped line; refused on a terminal)")
	generatePassword := fs.Bool("generate-password", false, "generate a random password and print it once as \"generated-password: <value>\"")
	force := fs.Bool("force", false, "create the account even when one already exists")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	passwordSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "password" {
			passwordSet = true
		}
	})
	if err := checkAdminPasswordFlags(passwordSet, *passwordStdin, *generatePassword); err != nil {
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
				if term.IsTerminal(int(os.Stdin.Fd())) {
					return "", errors.New("--password-stdin needs piped input; omit the flag for the hidden prompt")
				}
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
			printGeneratedPassword(os.Stdout, generated)
		}
		return exitOK
	})
}

func runAdminExists(args []string) int {
	fs := flag.NewFlagSet("admin exists", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "admin exists takes no arguments\n")
		return exitUsage
	}
	loadEnvFile()

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

		fmt.Printf("password updated for %s; refresh sessions were revoked and "+
			"two-factor authentication was cleared. Access tokens already issued "+
			"stay valid until they expire (15 minutes).\n", user.Email)
		return exitOK
	})
}

func checkAdminPasswordFlags(passwordSet, passwordStdin, generatePassword bool) error {
	n := 0
	if passwordSet {
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

func adminHashPlaintext(password string) (string, error) {
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return auth.HashPassword(password)
}

const maxPasswordStdinBytes = 1024

func readPasswordLine(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPasswordStdinBytes+1))
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	if len(data) > maxPasswordStdinBytes {
		return "", fmt.Errorf("password on stdin exceeds %d bytes", maxPasswordStdinBytes)
	}
	line, _, _ := strings.Cut(string(data), "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", fmt.Errorf("no password on stdin; pipe a single line or use --generate-password")
	}
	return line, nil
}

func printGeneratedPassword(w io.Writer, password string) {
	fmt.Fprintf(w, "generated-password: %s\n", password)
}

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

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(line), nil
}

func withAdminStore(fs *flag.FlagSet, fn func(ctx context.Context, st *store.Store) int) int {
	if fs.NFlag() == 0 && fs.NArg() == 0 {
		adminUsage(os.Stderr)
		return exitUsage
	}
	loadEnvFile()

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

func loadEnvFile() {
	data, err := os.ReadFile(adminEnvFile)
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
