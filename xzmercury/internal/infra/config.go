// Package infra handles configuration loading and infrastructure wiring.
package infra

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ruslano69/xzmercury/internal/ldap"
)

// Config is the top-level configuration structure for xzmercury.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Mercury  RedisConfig    `yaml:"mercury"`  // RAM-only: stores AES keys
	Pipeline RedisConfig    `yaml:"pipeline"` // stores quota, LDAP cache, request state
	Security SecurityConfig `yaml:"security"`
	LDAP     ldap.Config    `yaml:"ldap"`
	Quota    QuotaConfig    `yaml:"quota"`
	CA       CAConfig       `yaml:"ca"`       // CA authorization (prod only)
	KeyTTL   time.Duration  `yaml:"key_ttl"`  // how long a bound key lives; default 5m
	HashTTL  time.Duration  `yaml:"hash_ttl"` // how long a registered hash lives; default 24h
}

// CAConfig controls CA authorization at prod startup.
// Empty URL → CA authorization skipped (only valid in --dev mode).
type CAConfig struct {
	URL        string `yaml:"url"`         // CA server base URL, e.g. "https://ca.tdtp.io:8443"
	LicenseKey string `yaml:"license_key"` // raw license key; override via TDTPCA_LICENSE_KEY
	EnvKeyDir  string `yaml:"env_key_dir"` // directory for Ed25519 env keypair (TPM stub)
	CertPath   string `yaml:"cert_path"`   // path to persisted EnvCert (sealed in TPM mode)
	CAPubKey   string `yaml:"ca_pub_key"`  // path to CA Ed25519 public key PEM (for cert verification)
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Addr         string        `yaml:"addr"`          // default ":3000"
	ReadTimeout  time.Duration `yaml:"read_timeout"`  // default 10s
	WriteTimeout time.Duration `yaml:"write_timeout"` // default 10s
}

// RedisConfig is a minimal Redis connection spec.
type RedisConfig struct {
	Addr     string `yaml:"addr"`     // host:port
	Password string `yaml:"password"` // empty = no auth
	DB       int    `yaml:"db"`       // 0-based
}

// SecurityConfig holds secrets and rate-limit settings.
type SecurityConfig struct {
	ServerSecret string `yaml:"server_secret"` // HMAC secret; override via MERCURY_SERVER_SECRET
	RateLimit    int    `yaml:"rate_limit"`    // max requests/sec per IP (0 = disabled)
}

// QuotaConfig sets per-group credit defaults.
type QuotaConfig struct {
	DefaultHourly int    `yaml:"default_hourly"` // credits per hour when group has no balance
	ACLFile       string `yaml:"acl_file"`       // path to pipeline-acl.yaml
}

// LoadConfig reads and validates the YAML config at path, applying defaults.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}
	// Defaults
	cfg.Server.Addr = ":3000"
	cfg.Server.ReadTimeout = 10 * time.Second
	cfg.Server.WriteTimeout = 10 * time.Second
	cfg.LDAP.CacheTTL = 120 * time.Second
	cfg.Security.RateLimit = 100
	cfg.Quota.DefaultHourly = 1000
	cfg.KeyTTL = 5 * time.Minute
	cfg.HashTTL = 24 * time.Hour
	cfg.CA.EnvKeyDir = "./envkey"
	cfg.CA.CertPath = "env.cert"

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}

	// SERVER_SECRET: config file takes precedence; env var is the fallback
	if cfg.Security.ServerSecret == "" {
		if s := os.Getenv("MERCURY_SERVER_SECRET"); s != "" {
			cfg.Security.ServerSecret = s
		} else {
			return nil, fmt.Errorf("config: security.server_secret is required (or set MERCURY_SERVER_SECRET)")
		}
	}

	// LICENSE_KEY: config file takes precedence; env var is the fallback.
	if cfg.CA.LicenseKey == "" {
		if s := os.Getenv("TDTPCA_LICENSE_KEY"); s != "" {
			cfg.CA.LicenseKey = s
		}
	}
	return cfg, nil
}
