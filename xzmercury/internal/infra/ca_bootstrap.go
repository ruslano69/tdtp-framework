package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/ca"
	"github.com/ruslano69/xzmercury/internal/caClient"
	"github.com/ruslano69/xzmercury/internal/envkey"
)

// clockIface abstracts time.Now() for testability.
type clockIface interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// CASession holds the live CA authorization state for a prod xZMercury instance.
// It is refreshed by an AutoRenew goroutine; the HTTP layer checks Valid() before
// serving key operations.
type CASession struct {
	mu          sync.RWMutex
	token       string
	permissions []string
	expiresAt   time.Time
	clk         clockIface
}

// SetClock replaces the clock used by Valid. Intended for tests.
func (s *CASession) SetClock(clk clockIface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clk = clk
}

// Valid reports whether the session token is still within its TTL.
// The HTTP layer returns 503 when this is false.
func (s *CASession) Valid() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clk := s.clk
	if clk == nil {
		clk = realClock{}
	}
	return s.token != "" && clk.Now().Before(s.expiresAt)
}

// Permissions returns the current permission set (snapshot).
func (s *CASession) Permissions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.permissions))
	copy(out, s.permissions)
	return out
}

func (s *CASession) update(token string, perms []string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
	s.permissions = perms
	s.expiresAt = expiresAt
}

// BootstrapCA performs the prod-startup CA flow:
//  1. Load (or generate) the env Ed25519 keypair.
//  2. Load a persisted cert, or enroll if none exists.
//  3. Authorize to obtain a fresh session token.
//  4. Start AutoRenew to keep the cert and session alive.
//
// ctx is the long-lived server context — it governs the AutoRenew goroutine
// and is cancelled on shutdown. The initial enroll/authorize calls apply their
// own bootTimeout so a hung CA does not block startup forever.
//
// Returns a *CASession that the HTTP layer guards every request with.
// In --dev mode this is skipped entirely (caller must not invoke it).
func BootstrapCA(ctx context.Context, cfg CAConfig) (*CASession, error) {
	const bootTimeout = 30 * time.Second

	if cfg.URL == "" {
		return nil, fmt.Errorf("ca: url is required in prod mode (use --dev to skip CA)")
	}
	if cfg.LicenseKey == "" {
		return nil, fmt.Errorf("ca: license_key is required (config ca.license_key or TDTPCA_LICENSE_KEY)")
	}

	// 1. Env keypair (TPM stub).
	identity, err := envkey.Load(cfg.EnvKeyDir)
	if err != nil {
		return nil, fmt.Errorf("ca: load env key: %w", err)
	}

	client := caClient.NewClient(cfg.URL, identity)
	session := &CASession{}

	// 2. Load persisted cert, or enroll.
	cert, err := loadCert(cfg.CertPath)
	if err != nil {
		return nil, fmt.Errorf("ca: load cert: %w", err)
	}

	bootCtx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()

	if cert == nil {
		// First run — enroll.
		log.Info().Str("ca_url", cfg.URL).Msg("ca: no cert found, enrolling")
		result, err := client.Enroll(bootCtx, cfg.LicenseKey)
		if err != nil {
			return nil, fmt.Errorf("ca: enroll failed: %w", err)
		}
		cert = result.Cert
		if err := saveCert(cfg.CertPath, cert); err != nil {
			return nil, fmt.Errorf("ca: save cert: %w", err)
		}
		session.update(result.SessionToken.Token, result.Permissions, result.SessionToken.ExpiresAt)
		log.Info().
			Str("cert_id", cert.CertID).
			Strs("permissions", result.Permissions).
			Time("cert_not_after", cert.NotAfter).
			Msg("ca: enrolled successfully")
	} else {
		// Subsequent run — authorize.
		log.Info().Str("cert_id", cert.CertID).Msg("ca: cert found, authorizing")
		result, err := client.Authorize(bootCtx, cert)
		if err != nil {
			return nil, fmt.Errorf("ca: authorize failed: %w", err)
		}
		session.update(result.SessionToken.Token, result.Permissions, result.SessionToken.ExpiresAt)
		log.Info().
			Str("cert_id", cert.CertID).
			Strs("permissions", result.Permissions).
			Time("cert_not_after", result.CertNotAfter).
			Msg("ca: authorized successfully")
	}

	// 4. Start auto-renewal on the long-lived ctx (survives past bootTimeout).
	startNotAfter := cert.NotAfter
	client.AutoRenew(ctx, cert, startNotAfter, func(result *caClient.AuthorizeResult) {
		session.update(result.SessionToken.Token, result.Permissions, result.SessionToken.ExpiresAt)
		log.Info().
			Time("cert_not_after", result.CertNotAfter).
			Msg("ca: session renewed")
	})

	return session, nil
}

// loadCert reads a persisted EnvCert from disk. Returns (nil, nil) if absent.
func loadCert(path string) (*ca.EnvCert, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cert ca.EnvCert
	if err := json.Unmarshal(data, &cert); err != nil {
		return nil, fmt.Errorf("unmarshal cert: %w", err)
	}
	return &cert, nil
}

// saveCert writes the EnvCert to disk (0600). In TPM mode this would be TPM-sealed.
func saveCert(path string, cert *ca.EnvCert) error {
	data, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cert: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}
