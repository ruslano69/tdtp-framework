// Package main provides functionality for the TDTP framework.
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
	Lookups []LookupConfig     `yaml:"lookups,omitempty"` // параметризованные live-запросы по требованию (см. lookup.go)
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

// LookupConfig — параметризованный запрос, выполняемый вживую по требованию
// (GET /api/lookup/<name>?param=value), а не предзагружаемый при старте как
// sources. Для данных, которые дорого/бессмысленно тянуть заранее для всех
// строк — например фото сотрудника или историю проходов по одному коду.
//
// query использует нативный синтаксис плейсхолдеров своей БД (@p1 — mssql,
// ? — mysql/sqlite, $1 — postgres), как и sources.query уже требует нативный
// SQL под свой тип — никакой кросс-диалектной трансляции не делается.
type LookupConfig struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"` // sqlite | mysql | mssql | postgres
	DSN         string   `yaml:"dsn"`
	Query       string   `yaml:"query"`
	Params      []string `yaml:"params"`                 // имена URL query-параметров, в порядке позиционного биндинга
	Result      string   `yaml:"result"`                 // row | rows | binary
	MaxRows     int      `yaml:"max_rows,omitempty"`     // сервер-side cap для result: rows (по умолчанию 100)
	ContentType string   `yaml:"content_type,omitempty"` // обязателен для result: binary
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

	validLookupTypes := map[string]bool{"sqlite": true, "mysql": true, "mssql": true, "postgres": true}
	validResults := map[string]bool{"row": true, "rows": true, "binary": true}
	for i, lk := range cfg.Lookups {
		if lk.Name == "" {
			return nil, fmt.Errorf("lookup[%d]: name is required", i)
		}
		if !validLookupTypes[lk.Type] {
			return nil, fmt.Errorf("lookup %q: unknown type %q (sqlite/mysql/mssql/postgres)", lk.Name, lk.Type)
		}
		if lk.DSN == "" {
			return nil, fmt.Errorf("lookup %q: dsn is required", lk.Name)
		}
		if lk.Query == "" {
			return nil, fmt.Errorf("lookup %q: query is required", lk.Name)
		}
		if len(lk.Params) == 0 {
			return nil, fmt.Errorf("lookup %q: params must list at least one URL query parameter", lk.Name)
		}
		if !validResults[lk.Result] {
			return nil, fmt.Errorf("lookup %q: unknown result %q (row/rows/binary)", lk.Name, lk.Result)
		}
		if lk.Result == "binary" && lk.ContentType == "" {
			return nil, fmt.Errorf("lookup %q: content_type is required for result: binary", lk.Name)
		}
		if lk.MaxRows <= 0 {
			cfg.Lookups[i].MaxRows = 100
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
