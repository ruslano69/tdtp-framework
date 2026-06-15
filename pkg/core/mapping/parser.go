package mapping

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParseFile reads and validates a mapping YAML file.
func ParseFile(path string) (*MappingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mapping file %q: %w", path, err)
	}
	var cfg MappingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mapping YAML %q: %w", path, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid mapping %q: %w", path, err)
	}
	return &cfg, nil
}

func validate(cfg *MappingConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("mapping id is required")
	}
	if cfg.TargetConn.Type == "" || cfg.TargetConn.DSN == "" {
		return fmt.Errorf("target_connection.type and target_connection.dsn are required")
	}
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	for i, t := range cfg.Targets {
		if t.Table == "" {
			return fmt.Errorf("targets[%d].table is required", i)
		}
		if t.UpsertKey == "" {
			return fmt.Errorf("targets[%d].upsert_key is required", i)
		}
		if len(t.Fields) == 0 {
			return fmt.Errorf("targets[%d].fields is empty", i)
		}
	}
	return nil
}
