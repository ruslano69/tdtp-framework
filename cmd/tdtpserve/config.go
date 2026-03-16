package main

import (
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/etl"
	"gopkg.in/yaml.v3"
)

// ServeConfig — конфигурация tdtpserve
type ServeConfig struct {
	Server  ServerSection      `yaml:"server"`
	Sources []etl.SourceConfig `yaml:"sources"` // те же типы что и в ETL: tdtp, postgres, mssql, mysql, sqlite
	Views   []ViewConfig       `yaml:"views"`
}

// ServerSection — параметры HTTP сервера
type ServerSection struct {
	Name string `yaml:"name"` // заголовок в UI
	Port int    `yaml:"port"` // HTTP порт, по умолчанию 8080
}

// ViewConfig — SQL-вид поверх загруженных источников
// SQL выполняется в SQLite workspace (JOIN источников), результат кешируется при старте
type ViewConfig struct {
	Name        string `yaml:"name"`
	SQL         string `yaml:"sql"`
	Description string `yaml:"description"`
}

// loadConfig читает и валидирует YAML конфиг
func loadConfig(path string) (*ServeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %w", path, err)
	}

	var cfg ServeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("no sources configured")
	}

	for i, src := range cfg.Sources {
		if src.Name == "" {
			return nil, fmt.Errorf("source[%d]: name is required", i)
		}
		if src.Type == "" {
			return nil, fmt.Errorf("source %q: type is required", src.Name)
		}
		if src.DSN == "" {
			return nil, fmt.Errorf("source %q: dsn is required", src.Name)
		}
		validTypes := map[string]bool{"postgres": true, "mssql": true, "mysql": true, "sqlite": true, "tdtp": true, "tdtp-enc": true}
		if !validTypes[src.Type] {
			return nil, fmt.Errorf("source %q: unknown type %q (postgres/mssql/mysql/sqlite/tdtp/tdtp-enc)", src.Name, src.Type)
		}
		if src.Type != "tdtp" && src.Type != "tdtp-enc" && src.Query == "" {
			return nil, fmt.Errorf("source %q: query is required for type %q", src.Name, src.Type)
		}
		if src.Type == "tdtp-enc" && src.MercuryURL == "" {
			return nil, fmt.Errorf("source %q: mercury_url is required for type tdtp-enc", src.Name)
		}
	}

	for i, v := range cfg.Views {
		if v.Name == "" {
			return nil, fmt.Errorf("view[%d]: name is required", i)
		}
		if v.SQL == "" {
			return nil, fmt.Errorf("view %q: sql is required", v.Name)
		}
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Name == "" {
		cfg.Server.Name = "TDTP Serve"
	}

	return &cfg, nil
}
