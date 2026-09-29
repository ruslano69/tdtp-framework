package adapters

// ColumnReport describes a single column in a live DB table.
type ColumnReport struct {
	Name       string `yaml:"name" json:"name"`
	NativeType string `yaml:"native_type" json:"native_type"`
	TDTPType   string `yaml:"tdtp_type" json:"tdtp_type"`
	Nullable   bool   `yaml:"nullable" json:"nullable"`
	PrimaryKey bool   `yaml:"primary_key" json:"primary_key"`
	Identity   bool   `yaml:"identity,omitempty" json:"identity,omitempty"`  // auto-increment / IDENTITY column
	Computed   bool   `yaml:"computed,omitempty" json:"computed,omitempty"`  // computed / generated column
	Default    string `yaml:"default,omitempty" json:"default,omitempty"`   // default expression; empty if none
	Length     int    `yaml:"length,omitempty" json:"length,omitempty"`    // char/varchar max length
	Precision  int    `yaml:"precision,omitempty" json:"precision,omitempty"` // numeric precision
	Scale      int    `yaml:"scale,omitempty" json:"scale,omitempty"`     // numeric scale
}

// ForeignKeyReport describes a single FK constraint column reference.
type ForeignKeyReport struct {
	Column           string `yaml:"column" json:"column"`
	ReferencesTable  string `yaml:"references_table" json:"references_table"`
	ReferencesColumn string `yaml:"references_column" json:"references_column"`
	OnDelete         string `yaml:"on_delete,omitempty" json:"on_delete,omitempty"`
}

// TableStats contains table-level statistics.
type TableStats struct {
	TotalRows int64 `yaml:"total_rows" json:"total_rows"`
}

// TableReport is the full introspection result of a live DB table,
// returned by Adapter.InspectTable.
type TableReport struct {
	Table       string             `yaml:"table" json:"table"`
	DBType      string             `yaml:"db_type" json:"db_type"`
	DBVersion   string             `yaml:"db_version" json:"db_version"`
	Schema      string             `yaml:"schema,omitempty" json:"schema,omitempty"`
	Columns     []ColumnReport     `yaml:"columns" json:"columns"`
	ForeignKeys []ForeignKeyReport `yaml:"foreign_keys,omitempty" json:"foreign_keys,omitempty"`
	Stats       TableStats         `yaml:"stats" json:"stats"`
	Sample      map[string]string  `yaml:"sample,omitempty" json:"sample,omitempty"`
}

