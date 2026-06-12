package mapping

// MappingConfig is the top-level structure parsed from a mapping YAML file.
type MappingConfig struct {
	ID          string     `yaml:"id"`
	Version     string     `yaml:"version"`
	ApprovedBy  string     `yaml:"approved_by,omitempty"`
	LoopGuard   LoopGuard  `yaml:"loop_guard"`
	TargetConn  ConnConfig `yaml:"target_connection"`
	Targets     []Target   `yaml:"targets"`
}

// LoopGuard prevents recursive sync loops between systems.
type LoopGuard struct {
	SourceSystem string `yaml:"source_system"`
	TargetSystem string `yaml:"target_system"`
	MinInterval  string `yaml:"min_interval"` // e.g. "10s", "1m"
}

// ConnConfig describes a target database connection.
type ConnConfig struct {
	Type string `yaml:"type"` // "postgres", "mssql", "sqlite"
	DSN  string `yaml:"dsn"`
}

// Target describes one output table and its field mappings.
type Target struct {
	ID         string         `yaml:"id"`
	Table      string         `yaml:"table"`
	UpsertKey  string         `yaml:"upsert_key"` // field name used for ON CONFLICT
	Fields     []FieldMapping `yaml:"fields"`
}

// FieldMapping describes a single field transformation.
type FieldMapping struct {
	From string            `yaml:"from"`
	To   string            `yaml:"to"`
	Enum map[string]string `yaml:"enum,omitempty"` // value remap: source value → target value
}
