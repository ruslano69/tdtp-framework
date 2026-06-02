// tdtp-certify — vendor-only CA administration tool.
//
// Manages the CA's license database and Ed25519 root key. NOT shipped to customers.
//
// Subcommands:
//
//	keygen             generate a CA Ed25519 keypair (one-time, keep offline/HSM)
//	issue-license      create a license; prints the raw key to give to the customer
//	issue-offline-cert issue a cert for an air-gapped env without challenge-response
//	issue-unsafe-cert  sign a CapabilityCert for a short-lived --unsafe operation
//	revoke-cert        revoke a single environment certificate
//	revoke-license     revoke a license and all its certificates
//	list-licenses      show all licenses with seat usage
//	list-active        show environments active in the last 24h (real usage)
//	list-certs         show all certificates (active and revoked)
//
// Examples:
//
//	tdtp-certify keygen --out ca.ed25519.priv
//	tdtp-certify issue-license --db ca.db --licensee "Contoso" \
//	    --permissions etl,enc,s3 --seat-limit 3 --expires 2027-06-01
//	tdtp-certify issue-offline-cert --db ca.db --key ca.ed25519.priv \
//	    --env-pub <hex> --license-key <key> --out offline-cert.json
//	tdtp-certify issue-unsafe-cert --key ca.ed25519.priv --to ops@acme.com \
//	    --op unsafe-sql --tables users,orders --ttl 8h --out op.cert
//	tdtp-certify revoke-cert  --db ca.db --cert-id <uuid>
//	tdtp-certify list-active  --db ca.db
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ruslano69/xzmercury/internal/ca"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "keygen":
		err = cmdKeygen(args)
	case "issue-license":
		err = cmdIssueLicense(args)
	case "issue-offline-cert":
		err = cmdIssueOfflineCert(args)
	case "issue-unsafe-cert":
		err = cmdIssueUnsafeCert(args)
	case "revoke-cert":
		err = cmdRevokeCert(args)
	case "revoke-license":
		err = cmdRevokeLicense(args)
	case "list-licenses":
		err = cmdListLicenses(args)
	case "list-active":
		err = cmdListActive(args)
	case "list-certs":
		err = cmdListCerts(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `tdtp-certify — CA administration tool

Usage: tdtp-certify <command> [flags]

Commands:
  keygen             --out <path>
  issue-license      --db <path> --licensee <name> [--license-key <key>]
                     [--permissions etl,enc,s3] [--seat-limit N] [--expires YYYY-MM-DD]
  issue-offline-cert --db <path> --key <ca-key> --env-pub <hex|file> --license-key <key>
                     [--not-after <YYYY-MM-DD>] [--out <path>]
  issue-unsafe-cert  --key <ca-key> --to <email> --op <operation>
                     [--tables <t1,t2>] [--db <dbname>]
                     [--host <hostname>] [--ttl <duration>] [--out <path>]
                     Operations: unsafe-sql, schema-write, cross-schema, drop-allowed
  revoke-cert        --db <path> --cert-id <uuid>
  revoke-license     --db <path> (--hash <hex> | --license-key <key>)
  list-licenses      --db <path>
  list-active        --db <path> [--window 24h]
  list-certs         --db <path>
`)
}

// ─── flag parsing (minimal, dependency-free) ──────────────────────────────────

// parseFlags parses --key value / --key=value into a map.
func parseFlags(args []string) map[string]string {
	out := make(map[string]string)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key := strings.TrimPrefix(a, "--")
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			out[key[:eq]] = key[eq+1:]
			continue
		}
		// --key value form
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out[key] = args[i+1]
			i++
		} else {
			out[key] = "true" // boolean flag
		}
	}
	return out
}

func requireFlag(f map[string]string, name string) (string, error) {
	v, ok := f[name]
	if !ok || v == "" {
		return "", fmt.Errorf("--%s is required", name)
	}
	return v, nil
}

// ─── keygen ───────────────────────────────────────────────────────────────────

func cmdKeygen(args []string) error {
	f := parseFlags(args)
	out, err := requireFlag(f, "out")
	if err != nil {
		return err
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: priv})
	if err := os.WriteFile(out, privPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	pubPath := out + ".pub"
	pub := priv.Public().(ed25519.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PUBLIC KEY", Bytes: pub})
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	fmt.Printf("CA private key: %s (keep offline / HSM)\n", out)
	fmt.Printf("CA public key:  %s (embed in xZMercury / orchestrator)\n", pubPath)
	return nil
}

// ─── issue-license ────────────────────────────────────────────────────────────

func cmdIssueLicense(args []string) error {
	f := parseFlags(args)
	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}
	licensee, err := requireFlag(f, "licensee")
	if err != nil {
		return err
	}

	// License key: use provided, or generate a random one.
	licenseKey := f["license-key"]
	if licenseKey == "" {
		licenseKey, err = generateLicenseKey()
		if err != nil {
			return err
		}
	}

	permissions := []string{"etl"}
	if p := f["permissions"]; p != "" {
		permissions = splitCSV(p)
	}

	seatLimit := 1
	if s := f["seat-limit"]; s != "" {
		_, _ = fmt.Sscanf(s, "%d", &seatLimit)
	}

	paidUntil := time.Now().UTC().Add(365 * 24 * time.Hour)
	if e := f["expires"]; e != "" {
		t, err := time.Parse("2006-01-02", e)
		if err != nil {
			return fmt.Errorf("--expires must be YYYY-MM-DD: %w", err)
		}
		paidUntil = t.UTC()
	}

	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	lic := &ca.License{
		Hash:        ca.HashLicenseKey(licenseKey),
		Permissions: permissions,
		SeatLimit:   seatLimit,
		Status:      ca.LicenseActive,
		PaidUntil:   paidUntil,
	}
	if err := db.InsertLicense(lic); err != nil {
		return fmt.Errorf("insert license (already exists?): %w", err)
	}

	fmt.Println("License issued.")
	fmt.Printf("  Licensee:    %s\n", licensee)
	fmt.Printf("  Permissions: %s\n", strings.Join(permissions, ", "))
	fmt.Printf("  Seat limit:  %d\n", seatLimit)
	fmt.Printf("  Expires:     %s\n", paidUntil.Format("2006-01-02"))
	fmt.Printf("  Hash:        %s\n", lic.Hash)
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	fmt.Printf("  │ LICENSE KEY (give to customer, store nowhere else):           │\n")
	fmt.Printf("  │   %-60s│\n", licenseKey)
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println("  The CA stored only the hash; the raw key is shown once.")
	return nil
}

// ─── revoke-cert ──────────────────────────────────────────────────────────────

func cmdRevokeCert(args []string) error {
	f := parseFlags(args)
	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}
	certID, err := requireFlag(f, "cert-id")
	if err != nil {
		return err
	}

	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := db.RevokeCert(certID); err != nil {
		return err
	}
	fmt.Printf("Cert %s revoked. Next authorize will fail; Mercury loses session within session TTL.\n", certID)
	return nil
}

// ─── revoke-license ───────────────────────────────────────────────────────────

func cmdRevokeLicense(args []string) error {
	f := parseFlags(args)
	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}

	hash := f["hash"]
	if hash == "" {
		if key := f["license-key"]; key != "" {
			hash = ca.HashLicenseKey(key)
		} else {
			return fmt.Errorf("either --hash or --license-key is required")
		}
	}

	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := db.RevokeLicense(hash); err != nil {
		return err
	}
	fmt.Printf("License %s and all its certs revoked.\n", hash)
	return nil
}

// ─── list-licenses ────────────────────────────────────────────────────────────

func cmdListLicenses(args []string) error {
	f := parseFlags(args)
	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}
	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	licenses, err := db.ListLicenses()
	if err != nil {
		return err
	}
	if len(licenses) == 0 {
		fmt.Println("(no licenses)")
		return nil
	}
	fmt.Printf("%-18s %-8s %-6s %-10s %s\n", "HASH(prefix)", "STATUS", "SEATS", "EXPIRES", "PERMISSIONS")
	for _, l := range licenses {
		active, _ := db.CountActiveCerts(l.Hash)
		fmt.Printf("%-18s %-8s %d/%-4d %-10s %s\n",
			l.Hash[:16]+"…", l.Status, active, l.SeatLimit,
			l.PaidUntil.Format("2006-01-02"), strings.Join(l.Permissions, ","))
	}
	return nil
}

// ─── list-active ──────────────────────────────────────────────────────────────

func cmdListActive(args []string) error {
	f := parseFlags(args)
	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}
	window := 24 * time.Hour
	if w := f["window"]; w != "" {
		if d, err := time.ParseDuration(w); err == nil {
			window = d
		}
	}

	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	since := time.Now().UTC().Add(-window)
	certs, err := db.ListActiveCerts(since)
	if err != nil {
		return err
	}
	fmt.Printf("Active environments (last_seen within %s): %d\n\n", window, len(certs))
	if len(certs) == 0 {
		return nil
	}
	fmt.Printf("%-38s %-18s %-20s %s\n", "CERT_ID", "LICENSE(prefix)", "LAST_SEEN", "NOT_AFTER")
	for _, c := range certs {
		last := "never"
		if c.LastSeen != nil {
			last = c.LastSeen.Format(time.RFC3339)
		}
		fmt.Printf("%-38s %-18s %-20s %s\n",
			c.CertID, c.LicenseHash[:16]+"…", last, c.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// ─── list-certs ───────────────────────────────────────────────────────────────

func cmdListCerts(args []string) error {
	f := parseFlags(args)
	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}
	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	certs, err := db.ListAllCerts()
	if err != nil {
		return err
	}
	if len(certs) == 0 {
		fmt.Println("(no certs)")
		return nil
	}
	fmt.Printf("%-38s %-9s %-18s %s\n", "CERT_ID", "STATUS", "LICENSE(prefix)", "NOT_AFTER")
	for _, c := range certs {
		fmt.Printf("%-38s %-9s %-18s %s\n",
			c.CertID, c.Status, c.LicenseHash[:16]+"…", c.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// ─── issue-offline-cert ───────────────────────────────────────────────────────

// cmdIssueOfflineCert issues an EnvCert for an air-gapped environment without
// the normal challenge-response flow. The vendor runs this tool on their own
// machine (with access to the CA DB and private key) and ships the resulting
// JSON file to the customer.
func cmdIssueOfflineCert(args []string) error {
	f := parseFlags(args)

	dbPath, err := requireFlag(f, "db")
	if err != nil {
		return err
	}
	envPubRaw, err := requireFlag(f, "env-pub")
	if err != nil {
		return err
	}
	licenseKey, err := requireFlag(f, "license-key")
	if err != nil {
		return err
	}
	keyPath, err := requireFlag(f, "key")
	if err != nil {
		return err
	}

	outPath := f["out"]
	if outPath == "" {
		outPath = "offline-cert.json"
	}

	// Parse --env-pub: file path or 64-char hex string.
	var envIDPub []byte
	if strings.ContainsAny(envPubRaw, `/\`) ||
		strings.HasSuffix(envPubRaw, ".pub") ||
		strings.HasSuffix(envPubRaw, ".hex") ||
		strings.HasSuffix(envPubRaw, ".bin") {
		data, err := os.ReadFile(envPubRaw)
		if err != nil {
			return fmt.Errorf("read env-pub file: %w", err)
		}
		envIDPub, err = hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return fmt.Errorf("parse env-pub hex from file: %w", err)
		}
	} else {
		envIDPub, err = hex.DecodeString(strings.TrimSpace(envPubRaw))
		if err != nil {
			return fmt.Errorf("parse env-pub hex: %w", err)
		}
	}
	if len(envIDPub) != ed25519.PublicKeySize {
		return fmt.Errorf("env-pub must be 32 bytes (64 hex chars), got %d bytes", len(envIDPub))
	}

	// Load CA private key.
	caPrivKey, err := loadPrivKey(keyPath)
	if err != nil {
		return fmt.Errorf("load CA key: %w", err)
	}

	// Open DB and look up license.
	db, err := ca.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	licenseHash := ca.HashLicenseKey(licenseKey)
	lic, err := db.GetLicense(licenseHash)
	if err != nil {
		return fmt.Errorf("db: get license: %w", err)
	}
	if lic == nil {
		return fmt.Errorf("license not found (hash %s…)", licenseHash[:16])
	}
	if lic.Status != ca.LicenseActive {
		return fmt.Errorf("license is revoked")
	}

	// Determine not-after date.
	notAfter := lic.PaidUntil
	if na := f["not-after"]; na != "" {
		t, err := time.Parse("2006-01-02", na)
		if err != nil {
			return fmt.Errorf("--not-after must be YYYY-MM-DD: %w", err)
		}
		notAfter = t.UTC()
	}
	if notAfter.After(lic.PaidUntil) {
		return fmt.Errorf("not-after cannot exceed license paid_until (%s)", lic.PaidUntil.Format("2006-01-02"))
	}

	// Build and sign the cert.
	certID := uuid.New().String()
	now := time.Now().UTC()
	payload := &ca.CertPayload{
		CertID:      certID,
		LicenseHash: licenseHash,
		EnvIDPub:    envIDPub,
		Permissions: lic.Permissions,
		IssuedAt:    now,
		NotAfter:    notAfter,
		Offline:     true,
	}
	sig, err := ca.Sign(payload, caPrivKey)
	if err != nil {
		return fmt.Errorf("sign cert: %w", err)
	}

	cert := &ca.EnvCert{
		CertID:      certID,
		LicenseHash: licenseHash,
		EnvIDPub:    envIDPub,
		Permissions: lic.Permissions,
		IssuedAt:    now,
		NotAfter:    notAfter,
		Status:      ca.CertActive,
		Offline:     true,
		Signature:   sig,
	}

	// Persist in DB.
	if err := db.InsertCert(cert); err != nil {
		return fmt.Errorf("insert cert: %w", err)
	}

	// Write JSON output file.
	certJSON, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cert: %w", err)
	}
	if err := os.WriteFile(outPath, certJSON, 0o644); err != nil {
		return fmt.Errorf("write cert file: %w", err)
	}

	fmt.Printf("Offline cert issued: cert_id=%s not_after=%s out=%s\n",
		cert.CertID, notAfter.Format("2006-01-02"), outPath)
	fmt.Printf("WARNING: offline cert has no live-revocation. Revoke via: "+
		"tdtp-certify revoke-cert --db %s --cert-id %s\n", dbPath, cert.CertID)
	return nil
}

// ─── issue-unsafe-cert ────────────────────────────────────────────────────────

// unsafeCertScope mirrors pkg/license.CertScope for local signing (no cross-module import).
type unsafeCertScope struct {
	Database string   `json:"database"`
	Tables   []string `json:"tables"`
}

// unsafeCertPayload is the canonical payload used for Ed25519 signing.
// Field order is alphabetical to match pkg/license.certPayload exactly.
type unsafeCertPayload struct {
	Expires   time.Time       `json:"expires"`
	HostLock  string          `json:"host_lock"`
	IssuedAt  time.Time       `json:"issued_at"`
	IssuedTo  string          `json:"issued_to"`
	Nonce     string          `json:"nonce"`
	Operation string          `json:"operation"`
	Scope     unsafeCertScope `json:"scope"`
}

// unsafeCert is the full on-disk structure (payload + signature).
type unsafeCert struct {
	IssuedTo  string          `json:"issued_to"`
	Operation string          `json:"operation"`
	Scope     unsafeCertScope `json:"scope"`
	IssuedAt  time.Time       `json:"issued_at"`
	Expires   time.Time       `json:"expires"`
	HostLock  string          `json:"host_lock"`
	Nonce     string          `json:"nonce"`
	Signature string          `json:"signature"`
}

func cmdIssueUnsafeCert(args []string) error {
	f := parseFlags(args)

	keyPath, err := requireFlag(f, "key")
	if err != nil {
		return err
	}
	issuedTo, err := requireFlag(f, "to")
	if err != nil {
		return err
	}
	operation, err := requireFlag(f, "op")
	if err != nil {
		return err
	}

	// Validate operation value.
	validOps := map[string]bool{
		"unsafe-sql":   true,
		"schema-write": true,
		"cross-schema": true,
		"drop-allowed": true,
	}
	if !validOps[operation] {
		return fmt.Errorf("--op must be one of: unsafe-sql, schema-write, cross-schema, drop-allowed")
	}

	// Optional flags.
	var tables []string
	if t := f["tables"]; t != "" {
		tables = splitCSV(t)
	}
	dbName := f["db"]

	hostname := f["host"]
	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("cannot determine hostname: %w", err)
		}
	}

	ttl := 8 * time.Hour
	if s := f["ttl"]; s != "" {
		ttl, err = time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("--ttl: %w", err)
		}
	}

	outPath := f["out"]
	if outPath == "" {
		outPath = "unsafe-op.cert"
	}

	// Load Ed25519 private key.
	priv, err := loadPrivKey(keyPath)
	if err != nil {
		return fmt.Errorf("load key: %w", err)
	}

	// Generate nonce: 16 random bytes → hex.
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	now := time.Now().UTC()
	payload := unsafeCertPayload{
		Expires:   now.Add(ttl),
		HostLock:  hostname,
		IssuedAt:  now,
		IssuedTo:  issuedTo,
		Nonce:     nonce,
		Operation: operation,
		Scope: unsafeCertScope{
			Database: dbName,
			Tables:   tables,
		},
	}

	// Sign the canonical JSON payload.
	canonical, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	sig := ed25519.Sign(priv, canonical)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	cert := unsafeCert{
		IssuedTo:  payload.IssuedTo,
		Operation: payload.Operation,
		Scope:     payload.Scope,
		IssuedAt:  payload.IssuedAt,
		Expires:   payload.Expires,
		HostLock:  payload.HostLock,
		Nonce:     payload.Nonce,
		Signature: sigB64,
	}

	data, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cert: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}

	fmt.Printf("Unsafe capability cert issued.\n")
	fmt.Printf("  Nonce:     %s\n", nonce)
	fmt.Printf("  Operation: %s\n", operation)
	fmt.Printf("  Issued to: %s\n", issuedTo)
	fmt.Printf("  Expires:   %s\n", payload.Expires.Format(time.RFC3339))
	fmt.Printf("  Output:    %s\n", outPath)
	return nil
}

// ─── shared helpers ───────────────────────────────────────────────────────────

// loadPrivKey reads an Ed25519 private key from a PEM file (PKCS8 or raw 64-byte seed).
func loadPrivKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %q", path)
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8: %w", err)
		}
		priv, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not Ed25519 (got %T)", key)
		}
		return priv, nil
	case "ED25519 PRIVATE KEY":
		if len(block.Bytes) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("unexpected raw key size %d (want %d)",
				len(block.Bytes), ed25519.PrivateKeySize)
		}
		return ed25519.PrivateKey(block.Bytes), nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// generateLicenseKey returns a human-handleable random key: TDTP-XXXX-XXXX-XXXX-XXXX.
func generateLicenseKey() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate license key: %w", err)
	}
	h := strings.ToUpper(hex.EncodeToString(raw)) // 20 hex chars
	return fmt.Sprintf("TDTP-%s-%s-%s-%s", h[0:5], h[5:10], h[10:15], h[15:20]), nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
