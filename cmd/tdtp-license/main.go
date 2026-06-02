// tdtp-license — vendor tool to issue and verify tdtp.lic license files.
//
// Licenses are Ed25519-signed JSON gating tdtpcli capabilities offline.
// The vendor private key signs them; the matching public key is embedded in
// tdtpcli (pkg/license/pubkey.go). This is the OFFLINE trust branch — separate
// from the xZMercury/CA env-cert key.
//
// Subcommands:
//
//	keygen   --out vendor.priv          generate the vendor signing keypair
//	issue    --key vendor.priv --out tdtp.lic --licensee "X" \
//	         --tier professional --adapters postgres,mssql --features etl,enc \
//	         --expires 2027-06-01 [--rows 1000000] [--pipelines 10]
//	verify   --in tdtp.lic [--pubkey vendor.priv.pub]
//
// After keygen, embed the printed public key into pkg/license/pubkey.go and rebuild tdtpcli.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/license"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "issue":
		err = cmdIssue(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `tdtp-license — issue and verify tdtp.lic files

Usage: tdtp-license <command> [flags]

  keygen  --out <path>
  issue   --key <vendor.priv> --out <tdtp.lic> --licensee <name>
          --tier community|professional|enterprise
          --adapters a,b,c --features f1,f2 --expires YYYY-MM-DD
          [--rows N] [--pipelines N] [--issued YYYY-MM-DD]
  verify  --in <tdtp.lic> [--pubkey <vendor.priv.pub>]
`)
}

// ─── minimal flag parsing ─────────────────────────────────────────────────────

func parseFlags(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		k := strings.TrimPrefix(a, "--")
		if eq := strings.IndexByte(k, '='); eq >= 0 {
			out[k[:eq]] = k[eq+1:]
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out[k] = args[i+1]
			i++
		} else {
			out[k] = "true"
		}
	}
	return out
}

func req(f map[string]string, name string) (string, error) {
	if v := f[name]; v != "" {
		return v, nil
	}
	return "", fmt.Errorf("--%s is required", name)
}

// ─── keygen ───────────────────────────────────────────────────────────────────

func cmdKeygen(args []string) error {
	f := parseFlags(args)
	out, err := req(f, "out")
	if err != nil {
		return err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	// PKCS8 private key (OpenSSL-compatible).
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(out, privPEM, 0o600); err != nil {
		return err
	}

	// PKIX public key.
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(out+".pub", pubPEM, 0o644); err != nil {
		return err
	}

	fmt.Printf("Vendor signing key: %s (keep offline / HSM)\n", out)
	fmt.Printf("Public key:         %s\n\n", out+".pub")
	fmt.Println("Embed this public key into pkg/license/pubkey.go and rebuild tdtpcli:")
	fmt.Println(string(pubPEM))
	return nil
}

// ─── issue ────────────────────────────────────────────────────────────────────

func cmdIssue(args []string) error {
	f := parseFlags(args)
	keyPath, err := req(f, "key")
	if err != nil {
		return err
	}
	out, err := req(f, "out")
	if err != nil {
		return err
	}
	licensee, err := req(f, "licensee")
	if err != nil {
		return err
	}
	expires, err := req(f, "expires")
	if err != nil {
		return err
	}

	priv, err := loadPrivateKey(keyPath)
	if err != nil {
		return err
	}

	tier := license.Tier(orDefault(f["tier"], "professional"))
	issued := orDefault(f["issued"], "")
	rows := atoiDefault(f["rows"], 0)
	pipelines := atoiDefault(f["pipelines"], 0)

	lic := license.New(
		licensee, issued, expires, tier,
		splitCSV(f["adapters"]), splitCSV(f["features"]),
		license.Limits{RowsPerExport: rows, Pipelines: pipelines},
	)
	if err := lic.Sign(priv); err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	data, err := json.MarshalIndent(lic, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}

	fmt.Printf("License issued: %s\n", out)
	fmt.Printf("  %s\n", lic.Summary())
	return nil
}

// ─── verify ───────────────────────────────────────────────────────────────────

func cmdVerify(args []string) error {
	f := parseFlags(args)
	in, err := req(f, "in")
	if err != nil {
		return err
	}

	lic, err := license.Load(in)
	if err != nil {
		return err
	}

	if pubPath := f["pubkey"]; pubPath != "" {
		pub, err := loadPublicKey(pubPath)
		if err != nil {
			return err
		}
		if err := lic.VerifyWith(pub); err != nil {
			return err
		}
	} else {
		if err := lic.Verify(); err != nil {
			return err
		}
	}

	fmt.Printf("VALID: %s\n", lic.Summary())
	return nil
}

// ─── key loading ──────────────────────────────────────────────────────────────

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not Ed25519")
	}
	return priv, nil
}

func loadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not Ed25519")
	}
	return pub, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func splitCSV(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
