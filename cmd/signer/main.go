// Command signer manages the Ed25519 release-signing keypair and signs release
// artifacts. It is the only place the private key is used; the control plane
// only ever embeds the matching public key.
//
// Usage:
//
//	signer keygen [-out signing.key]
//	signer sign   -key signing.key -in gotham-linux-amd64 [-out gotham-linux-amd64.sig]
//	signer verify -key signing.key.pub -in gotham-linux-amd64 [-sig gotham-linux-amd64.sig]
//
// The signature is written as standard base64 (with a trailing newline), which
// is the format the Applier verifies.
package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/justindeelux/gotham/internal/updates"
)

// version is the reported tool version, overridable with -ldflags.
var version = "dev"

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the subcommand and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:])
	case "sign":
		return runSign(args[1:])
	case "verify":
		return runVerify(args[1:])
	case "version", "-v", "--version":
		fmt.Printf("signer %s\n", version)
		return exitOK
	case "help", "-h", "--help":
		usage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return exitUsage
	}
}

// runKeygen writes a new Ed25519 keypair and prints the ldflags value that
// embeds the public key in the control plane.
func runKeygen(args []string) int {
	out := "signing.key"
	flags := newFlagSet("keygen")
	flags.StringVar(&out, "out", out, "private key output path")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	publicKey, privateKey, err := updates.GenerateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: %v\n", err)
		return exitError
	}
	privatePEM, err := updates.MarshalPrivateKeyPEM(privateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: %v\n", err)
		return exitError
	}
	publicPEM, err := updates.MarshalPublicKeyPEM(publicKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: %v\n", err)
		return exitError
	}

	if err := os.WriteFile(out, privatePEM, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "keygen: write %s: %v\n", out, err)
		return exitError
	}
	if err := os.WriteFile(out+".pub", publicPEM, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "keygen: write %s.pub: %v\n", out, err)
		return exitError
	}

	fmt.Printf("wrote %s and %s.pub\n", out, out)
	fmt.Printf("embed the public key with:\n  -ldflags \"-X github.com/justindeelux/gotham/internal/updates.PublicKey=%s\"\n",
		updates.EncodePublicKeyBase64(publicKey))
	return exitOK
}

// runSign signs a file with the private key and writes the base64 signature.
func runSign(args []string) int {
	var keyPath, inPath, outPath string
	flags := newFlagSet("sign")
	flags.StringVar(&keyPath, "key", "", "private key PEM path (required)")
	flags.StringVar(&inPath, "in", "", "file to sign (required)")
	flags.StringVar(&outPath, "out", "", "signature output path (default <in>.sig; - for stdout)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if keyPath == "" || inPath == "" {
		fmt.Fprintln(os.Stderr, "sign: -key and -in are required")
		return exitUsage
	}
	if outPath == "" {
		outPath = inPath + ".sig"
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: read key: %v\n", err)
		return exitError
	}
	privateKey, err := updates.ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		return exitError
	}
	signer, err := updates.NewSigner(privateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		return exitError
	}
	data, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: read %s: %v\n", inPath, err)
		return exitError
	}
	signature := signer.SignBase64(data) + "\n"

	if outPath == "-" {
		fmt.Print(signature)
		return exitOK
	}
	if err := os.WriteFile(outPath, []byte(signature), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sign: write %s: %v\n", outPath, err)
		return exitError
	}
	fmt.Printf("wrote %s\n", outPath)
	return exitOK
}

// runVerify verifies a detached signature; it exits 1 when verification fails.
func runVerify(args []string) int {
	var keyPath, inPath, sigPath string
	flags := newFlagSet("verify")
	flags.StringVar(&keyPath, "key", "", "public key path (required)")
	flags.StringVar(&inPath, "in", "", "file to check (required)")
	flags.StringVar(&sigPath, "sig", "", "signature path (default <in>.sig)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if keyPath == "" || inPath == "" {
		fmt.Fprintln(os.Stderr, "verify: -key and -in are required")
		return exitUsage
	}
	if sigPath == "" {
		sigPath = inPath + ".sig"
	}

	key, err := loadPublicKeyFile(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		return exitError
	}
	verifier, err := updates.NewVerifier(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		return exitError
	}
	data, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: read %s: %v\n", inPath, err)
		return exitError
	}
	signature, err := os.ReadFile(sigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: read %s: %v\n", sigPath, err)
		return exitError
	}
	if err := verifier.Verify(data, signature); err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		return exitError
	}
	fmt.Printf("OK %s\n", inPath)
	return exitOK
}

// loadPublicKeyFile reads a PEM or base64/hex public key file.
func loadPublicKeyFile(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return updates.ParsePublicKey(string(raw))
}

// newFlagSet builds a subcommand flag set that reports errors to stderr.
func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return flags
}

// usage prints the top-level help.
func usage(w io.Writer) {
	fmt.Fprintf(w, `signer %s

Usage:
  signer keygen [-out signing.key]                        Generate a keypair
  signer sign   -key <priv> -in <file> [-out <sig>]       Sign a file (base64)
  signer verify -key <pub.pem> -in <file> [-sig <sig>]    Verify a signature
  signer version                                          Print the version
  signer help                                             Show this help
`, version)
}
