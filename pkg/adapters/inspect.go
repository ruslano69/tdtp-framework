package adapters

// ColumnReport describes a single column in a live DB table.
type ColumnReport struct {
	Name       string `yaml:"name"`
	NativeType string `yaml:"native_type"`
	TDTPType   string `yaml:"tdtp_type"`
	Nullable   bool   `yaml:"nullable"`
	PrimaryKey bool   `yaml:"primary_key"`
	Identity   bool   `yaml:"identity,omitempty"`  // auto-increment / IDENTITY column
	Computed   bool   `yaml:"computed,omitempty"`  // computed / generated column
	Default    string `yaml:"default,omitempty"`   // default expression; empty if none
	Length     int    `yaml:"length,omitempty"`    // char/varchar max length
	Precision  int    `yaml:"precision,omitempty"` // numeric precision
	Scale      int    `yaml:"scale,omitempty"`     // numeric scale
}

// ForeignKeyReport describes a single FK constraint column reference.
type ForeignKeyReport struct {
	Column           string `yaml:"column"`
	ReferencesTable  string `yaml:"references_table"`
	ReferencesColumn string `yaml:"references_column"`
	OnDelete         string `yaml:"on_delete,omitempty"`
}

// TableStats contains table-level statistics.
type TableStats struct {
	TotalRows int64 `yaml:"total_rows"`
}

// TableReport is the full introspection result of a live DB table,
// returned by Adapter.InspectTable.
type TableReport struct {
	Table       string             `yaml:"table"`
	DBType      string             `yaml:"db_type"`
	DBVersion   string             `yaml:"db_version"`
	Schema      string             `yaml:"schema,omitempty"`
	Columns     []ColumnReport     `yaml:"columns"`
	ForeignKeys []ForeignKeyReport `yaml:"foreign_keys,omitempty"`
	Stats       TableStats         `yaml:"stats"`
	Sample      map[string]string  `yaml:"sample,omitempty"`
}
