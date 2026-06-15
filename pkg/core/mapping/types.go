package mapping

import (
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// InputSource describes where the input TDTP packet comes from.
// When absent from the mapping YAML, --input must be a local file path.
type InputSource struct {
	Type   string            `yaml:"type"` // "s3" | "broker"
	S3     *storage.S3Config `yaml:"s3,omitempty"`
	Broker *brokers.Config   `yaml:"broker,omitempty"`
}

// MappingConfig is the top-level structure parsed from a mapping YAML file.
type MappingConfig struct {
	ID          string       `yaml:"id"`
	Version     string       `yaml:"version"`
	ApprovedBy  string       `yaml:"approved_by,omitempty"`
	LoopGuard   LoopGuard    `yaml:"loop_guard"`
	InputSource *InputSource `yaml:"input_source,omitempty"`
	TargetConn  ConnConfig   `yaml:"target_connection"`
	Targets     []Target     `yaml:"targets"`
}

// LoopGuard prevents recursive sync loops between systems.
type LoopGuard struct {
	SourceSystem string `yaml:"source_system"`
	TargetSystem string `yaml:"target_system"`
	MinInterval  string `yaml:"min_interval"` // e.g. "10s", "1m"
}

// ConnConfig describes a target database connection.
type ConnConfig struct {
	Type   string `yaml:"type"` // "postgres", "mssql", "sqlite"
	DSN    string `yaml:"dsn"`
	Schema string `yaml:"schema,omitempty"` // default schema; overridden by dotted table names
}

// Target describes one output table and its field mappings.
type Target struct {
	ID        string         `yaml:"id"`
	Table     string         `yaml:"table"`
	UpsertKey string         `yaml:"upsert_key"` // field name used for ON CONFLICT
	Fields    []FieldMapping `yaml:"fields"`
}

// FieldMapping describes a single field transformation.
type FieldMapping struct {
	From string            `yaml:"from"`
	To   string            `yaml:"to"`
	Enum map[string]string `yaml:"enum,omitempty"` // value remap: source value → target value
}
