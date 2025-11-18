package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	Database   DatabaseConfig   `yaml:"database"`
	Export     ExportConfig     `yaml:"export,omitempty"`
	Tables     []string         `yaml:"tables,omitempty"`
	Broker     BrokerConfig     `yaml:"broker,omitempty"`
	Resilience ResilienceConfig `yaml:"resilience,omitempty"`
	Audit      AuditConfig      `yaml:"audit,omitempty"`
	Processors ProcessorsConfig `yaml:"processors,omitempty"`
}

// ExportConfig contains export settings
type ExportConfig struct {
	Compress      bool `yaml:"compress"`       // Enable zstd compression by default
	CompressLevel int  `yaml:"compress_level"` // Compression level: 1-19 (default: 3)
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	Type        string `yaml:"type"`                   // sqlite, postgres, mssql
	Host        string `yaml:"host,omitempty"`         // For network databases
	Port        int    `yaml:"port,omitempty"`         // Database port
	Database    string `yaml:"database"`               // Database name or file path
	User        string `yaml:"user,omitempty"`         // Username
	Password    string `yaml:"password,omitempty"`     // Password
	Schema      string `yaml:"schema,omitempty"`       // PostgreSQL schema (default: public)
	WindowsAuth bool   `yaml:"windows_auth,omitempty"` // MS SQL Windows authentication
	SSLMode     string `yaml:"sslmode,omitempty"`      // PostgreSQL SSL mode
}

// BrokerConfig contains message broker settings
type BrokerConfig struct {
	Type     string `yaml:"type"`               // rabbitmq, msmq, kafka
	Host     string `yaml:"host,omitempty"`     // Broker host
	Port     int    `yaml:"port,omitempty"`     // Broker port
	User     string `yaml:"user,omitempty"`     // Username
	Password string `yaml:"password,omitempty"` // Password
	Queue    string `yaml:"queue,omitempty"`    // Queue/topic name
	VHost    string `yaml:"vhost,omitempty"`    // RabbitMQ vhost
}

// ResilienceConfig contains circuit breaker and retry settings
type ResilienceConfig struct {
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker,omitempty"`
	Retry          RetryConfig          `yaml:"retry,omitempty"`
}

// CircuitBreakerConfig for circuit breaker settings
type CircuitBreakerConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Threshold        uint32 `yaml:"threshold"`         // Failure threshold
	Timeout          int    `yaml:"timeout"`           // Timeout in seconds
	MaxConcurrent    int    `yaml:"max_concurrent"`    // Max concurrent calls
	SuccessThreshold uint32 `yaml:"success_threshold"` // Success threshold for recovery
}

// RetryConfig for retry mechanism settings
type RetryConfig struct {
	Enabled     bool   `yaml:"enabled"`
	MaxAttempts int    `yaml:"max_attempts"`
	Strategy    string `yaml:"strategy"` // constant, linear, exponential
	InitialWait int    `yaml:"initial_wait_ms"`
	MaxWait     int    `yaml:"max_wait_ms"`
	Jitter      bool   `yaml:"jitter"`
}

// AuditConfig for audit logging settings
type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	Level   string `yaml:"level"` // minimal, standard, full
	File    string `yaml:"file,omitempty"`
	MaxSize int    `yaml:"max_size_mb,omitempty"` // Max file size in MB
	Console bool   `yaml:"console,omitempty"`     // Log to console
}

// ProcessorsConfig for data processing settings
type ProcessorsConfig struct {
	Mask      []MaskRule      `yaml:"mask,omitempty"`
	Validate  []ValidateRule  `yaml:"validate,omitempty"`
	Normalize []NormalizeRule `yaml:"normalize,omitempty"`
}

// MaskRule for field masking
type MaskRule struct {
	Field    string `yaml:"field"`
	Strategy string `yaml:"strategy"` // email, phone, card, partial
}

// ValidateRule for field validation
type ValidateRule struct {
	Field   string `yaml:"field"`
	Type    string `yaml:"type"`  // regex, range, format
	Pattern string `yaml:"pattern,omitempty"`
	Min     string `yaml:"min,omitempty"`
	Max     string `yaml:"max,omitempty"`
}

// NormalizeRule for field normalization
type NormalizeRule struct {
	Field    string `yaml:"field"`
	Strategy string `yaml:"strategy"` // email, phone, date, trim, uppercase, lowercase
}

// LoadConfig loads configuration from YAML file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig saves configuration to YAML file
func SaveConfig(filename string, config *Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// CreateSampleConfig creates sample configuration for different database types
func CreateSampleConfig(dbType string) *Config {
	config := &Config{
		Database: DatabaseConfig{
			Type: dbType,
		},
		Export: ExportConfig{
			Compress:      true, // Enable compression by default
			CompressLevel: 3,    // Balanced speed/ratio
		},
		Resilience: ResilienceConfig{
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				Threshold:        5,
				Timeout:          60,
				MaxConcurrent:    100,
				SuccessThreshold: 2,
			},
			Retry: RetryConfig{
				Enabled:     true,
				MaxAttempts: 3,
				Strategy:    "exponential",
				InitialWait: 1000,
				MaxWait:     30000,
				Jitter:      true,
			},
		},
		Audit: AuditConfig{
			Enabled: true,
			Level:   "standard",
			File:    "audit.log",
			MaxSize: 100,
			Console: false,
		},
	}

	switch dbType {
	case "postgres", "postgresql":
		config.Database.Host = "localhost"
		config.Database.Port = 5432
		config.Database.Database = "mydb"
		config.Database.User = "postgres"
		config.Database.Password = "password"
		config.Database.Schema = "public"
		config.Database.SSLMode = "disable"

	case "mssql", "sqlserver":
		config.Database.Host = "localhost"
		config.Database.Port = 1433
		config.Database.Database = "mydb"
		config.Database.User = "sa"
		config.Database.Password = "YourPassword123"
		config.Database.WindowsAuth = false

	case "sqlite":
		config.Database.Database = "database.db"

	case "mysql":
		config.Database.Host = "localhost"
		config.Database.Port = 3306
		config.Database.Database = "mydb"
		config.Database.User = "root"
		config.Database.Password = "password"
	}

	return config
}

// BuildDSN constructs database connection string from config
func (c *DatabaseConfig) BuildDSN() string {
	switch c.Type {
	case "postgres", "postgresql":
		sslMode := c.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		schema := c.Schema
		if schema == "" {
			schema = "public"
		}
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s&search_path=%s",
			c.User, c.Password, c.Host, c.Port, c.Database, sslMode, schema)

	case "mssql", "sqlserver":
		if c.WindowsAuth {
			return fmt.Sprintf("sqlserver://%s:%d?database=%s&integrated security=SSPI",
				c.Host, c.Port, c.Database)
		}
		return fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			c.User, c.Password, c.Host, c.Port, c.Database)

	case "sqlite":
		return c.Database

	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			c.User, c.Password, c.Host, c.Port, c.Database)

	default:
		return ""
	}
}
