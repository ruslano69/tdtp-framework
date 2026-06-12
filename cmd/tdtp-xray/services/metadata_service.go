package services

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

type MetadataService struct{}

// ColumnInfo represents database column metadata
type ColumnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	IsNullable   bool   `json:"isNullable"`
	IsPrimaryKey bool   `json:"isPrimaryKey"`
	MaxLength    int    `json:"maxLength,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// TableSchema represents complete table schema
type TableSchema struct {
	TableName string       `json:"tableName"`
	Columns   []ColumnInfo `json:"columns"`
	Error     string       `json:"error,omitempty"`
}

func NewMetadataService() *MetadataService {
	return &MetadataService{}
}

// GetTableSchema retrieves schema for a specific table via pkg/adapters.
func (ms *MetadataService) GetTableSchema(dbType, dsn, tableName string) TableSchema {
	if dbType == "tdtp" {
		return ms.InferTDTPSchema(dsn)
	}

	ctx := context.Background()
	cs := NewConnectionService()
	adapter, err := adapters.New(ctx, adapters.Config{
		Type: cs.normalizeDBType(dbType),
		DSN:  dsn,
	})
	if err != nil {
		return TableSchema{TableName: tableName, Error: fmt.Sprintf("Connection failed: %v", err)}
	}
	defer func() { _ = adapter.Close(ctx) }()

	schema, err := adapter.GetTableSchema(ctx, tableName)
	if err != nil {
		return TableSchema{TableName: tableName, Error: fmt.Sprintf("GetTableSchema failed: %v", err)}
	}

	columns := make([]ColumnInfo, len(schema.Fields))
	for i, f := range schema.Fields {
		columns[i] = ColumnInfo{
			Name:         f.Name,
			DataType:     f.Type,
			MaxLength:    f.Length,
			IsNullable:   true,
			IsPrimaryKey: f.Key,
		}
	}

	return TableSchema{TableName: tableName, Columns: columns}
}

// InferTDTPSchema attempts to infer schema from TDTP XML file
func (ms *MetadataService) InferTDTPSchema(filePath string) TableSchema {
	fmt.Printf("📄 InferTDTPSchema: Parsing TDTP file: %s\n", filePath)

	// Use the framework's TDTP parser
	parser := packet.NewParser()
	dataPacket, err := parser.ParseFile(filePath)
	if err != nil {
		fmt.Printf("❌ Failed to parse TDTP file: %v\n", err)
		return TableSchema{
			TableName: "tdtp_source",
			Error:     fmt.Sprintf("Failed to parse TDTP file: %v", err),
		}
	}

	fmt.Printf("✅ TDTP parsed successfully. TableName: %s, Fields: %d\n",
		dataPacket.Header.TableName, len(dataPacket.Schema.Fields))

	// Convert packet.Field to ColumnInfo
	columns := make([]ColumnInfo, len(dataPacket.Schema.Fields))
	for i, field := range dataPacket.Schema.Fields {
		columns[i] = ColumnInfo{
			Name:         field.Name,
			DataType:     field.Type,
			MaxLength:    field.Length,
			IsNullable:   true, // TDTP doesn't have explicit nullable flag, assume true
			IsPrimaryKey: field.Key,
		}
		fmt.Printf("  📋 Field %d: %s (%s)\n", i, field.Name, field.Type)
	}

	return TableSchema{
		TableName: dataPacket.Header.TableName,
		Columns:   columns,
	}
}
