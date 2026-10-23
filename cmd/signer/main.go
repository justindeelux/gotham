package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/justindeelux/gotham/updatecore"
)

var version = "dev"

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

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
	case "manifest":
		return runManifest(args[1:])
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

func runKeygen(args []string) int {
	out := "signing.key"
	flags := newFlagSet("keygen")
	flags.StringVar(&out, "out", out, "private key output path")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	publicKey, privateKey, err := updatecore.GenerateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: %v\n", err)
		return exitError
	}
	privatePEM, err := updatecore.MarshalPrivateKeyPEM(privateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: %v\n", err)
		return exitError
	}
	publicPEM, err := updatecore.MarshalPublicKeyPEM(publicKey)
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
	fmt.Printf("embed the public key with:\n  -ldflags \"-X github.com/justindeelux/gotham/updatecore.PublicKey=%s\"\n",
		updatecore.EncodePublicKeyBase64(publicKey))
	return exitOK
}

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
	privateKey, err := updatecore.ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		return exitError
	}
	signer, err := updatecore.NewSigner(privateKey)
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

func runManifest(args []string) int {
	var keyPath, inPath, version, channel, arch, outPath, helperPath string
	flags := newFlagSet("manifest")
	flags.StringVar(&keyPath, "key", "", "private key PEM path (required)")
	flags.StringVar(&inPath, "in", "", "artifact to describe (required)")
	flags.StringVar(&version, "version", "", "release version, e.g. v1.2.0 (required)")
	flags.StringVar(&channel, "channel", "stable", "release channel: stable or beta")
	flags.StringVar(&arch, "arch", "", "artifact architecture, e.g. amd64 (required)")
	flags.StringVar(&outPath, "out", "", "manifest output path (default gotham-manifest-<arch>.txt)")
	flags.StringVar(&helperPath, "helper", "", "optional host-helper payload archive to bind to the manifest")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if keyPath == "" || inPath == "" || version == "" || arch == "" {
		fmt.Fprintln(os.Stderr, "manifest: -key, -in, -version and -arch are required")
		return exitUsage
	}
	if outPath == "" {
		outPath = updatecore.ManifestName(arch)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest: read key: %v\n", err)
		return exitError
	}
	privateKey, err := updatecore.ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest: %v\n", err)
		return exitError
	}
	signer, err := updatecore.NewSigner(privateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest: %v\n", err)
		return exitError
	}
	artifact, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest: read %s: %v\n", inPath, err)
		return exitError
	}

	manifest := updatecore.BuildManifest(version, channel, arch, filepath.Base(inPath), artifact)
	if helperPath != "" {
		helper, err := os.ReadFile(helperPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "manifest: read %s: %v\n", helperPath, err)
			return exitError
		}
		manifest = manifest.WithHelper(filepath.Base(helperPath), helper)
	}
	body := manifest.Marshal()
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "manifest: write %s: %v\n", outPath, err)
		return exitError
	}
	signature := signer.SignBase64(body) + "\n"
	if err := os.WriteFile(outPath+updatecore.ManifestSigSuffix, []byte(signature), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "manifest: write signature: %v\n", err)
		return exitError
	}
	fmt.Printf("wrote %s and %s%s\n", outPath, outPath, updatecore.ManifestSigSuffix)
	return exitOK
}

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
	verifier, err := updatecore.NewVerifier(key)
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

func loadPublicKeyFile(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return updatecore.ParsePublicKey(string(raw))
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return flags
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `signer %s

Usage:
  signer keygen [-out signing.key]                        Generate a keypair
  signer sign   -key <priv> -in <file> [-out <sig>]       Sign a file (base64)
  signer verify -key <pub.pem> -in <file> [-sig <sig>]    Verify a signature
  signer manifest -key <priv> -in <artifact> -version <v> -arch <a> [-channel <c>] [-helper <tar.gz>]
                                                          Generate + sign a release manifest
                                                          (-helper binds a host-helper payload)
  signer version                                          Print the version
  signer help                                             Show this help
`, version)
}
