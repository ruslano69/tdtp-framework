package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"gopkg.in/yaml.v3"
)

// ProcessRequestOptions holds options for process-request operation
type ProcessRequestOptions struct {
	RequestFile  string           // Путь к входящему request.tdtp
	OutputFile   string           // Куда писать response (опционально, иначе авто)
	ConfigsDir   string           // Директория с конфигами вида {Recipient}.yaml
	DefaultConfig *adapters.Config // Fallback если {Recipient}.yaml не найден
}

// adapterConfigFromYAML загружает adapters.Config из yaml-файла конфига tdtpcli
func adapterConfigFromYAML(path string) (*adapters.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Минимальная структура для чтения секции database
	var raw struct {
		Database struct {
			Type        string `yaml:"type"`
			Host        string `yaml:"host"`
			Port        int    `yaml:"port"`
			Database    string `yaml:"database"`
			User        string `yaml:"user"`
			Password    string `yaml:"password"`
			Schema      string `yaml:"schema"`
			WindowsAuth bool   `yaml:"windows_auth"`
			SSLMode     string `yaml:"sslmode"`
		} `yaml:"database"`
	}

	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	db := raw.Database
	if db.Type == "" {
		return nil, fmt.Errorf("database.type is missing in config %s", path)
	}

	// Строим DSN по типу БД
	var dsn string
	switch db.Type {
	case "postgres", "postgresql":
		sslMode := db.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		schema := db.Schema
		if schema == "" {
			schema = "public"
		}
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s&search_path=%s",
			db.User, db.Password, db.Host, db.Port, db.Database, sslMode, schema)
	case "mssql", "sqlserver":
		encrypt := ""
		if db.SSLMode == "disable" {
			encrypt = "&encrypt=disable"
		}
		if db.WindowsAuth {
			dsn = fmt.Sprintf("sqlserver://%s:%d?database=%s%s&trusted_connection=true",
				db.Host, db.Port, db.Database, encrypt)
		} else {
			dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s%s",
				db.User, db.Password, db.Host, db.Port, db.Database, encrypt)
		}
	case "sqlite":
		dsn = db.Database
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			db.User, db.Password, db.Host, db.Port, db.Database)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.Type)
	}

	return &adapters.Config{Type: db.Type, DSN: dsn}, nil
}

// resolveAdapterConfig ищет конфиг {Recipient}.yaml в configsDir, затем использует defaultConfig
func resolveAdapterConfig(recipient, configsDir string, defaultConfig *adapters.Config) (*adapters.Config, error) {
	if recipient != "" && configsDir != "" {
		candidates := []string{
			filepath.Join(configsDir, recipient+".yaml"),
			filepath.Join(configsDir, strings.ToLower(recipient)+".yaml"),
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				cfg, err := adapterConfigFromYAML(path)
				if err != nil {
					return nil, fmt.Errorf("failed to load config for recipient '%s': %w", recipient, err)
				}
				fmt.Printf("  Config: %s\n", path)
				return cfg, nil
			}
		}
		fmt.Printf("  Warning: config '%s.yaml' not found in '%s', using default config\n", recipient, configsDir)
	}

	if defaultConfig == nil {
		return nil, fmt.Errorf("no config found for recipient '%s' and no default config provided", recipient)
	}
	return defaultConfig, nil
}

// ProcessRequest читает TDTP request-пакет, выполняет запрос и генерирует response
func ProcessRequest(ctx context.Context, opts ProcessRequestOptions) error {
	// 1. Парсим request файл
	parser := packet.NewParser()
	reqPacket, err := parser.ParseFile(opts.RequestFile)
	if err != nil {
		return fmt.Errorf("failed to parse request file: %w", err)
	}

	if reqPacket.Header.Type != packet.TypeRequest {
		return fmt.Errorf("expected request packet, got: %s", reqPacket.Header.Type)
	}

	tableName := reqPacket.Header.TableName
	recipient := reqPacket.Header.Recipient // имя конфига
	sender := reqPacket.Header.Sender
	messageID := reqPacket.Header.MessageID

	fmt.Printf("Processing request:\n")
	fmt.Printf("  MessageID:  %s\n", messageID)
	fmt.Printf("  Table:      %s\n", tableName)
	fmt.Printf("  Sender:     %s\n", sender)
	fmt.Printf("  Recipient:  %s\n", recipient)

	// 2. Находим конфиг адаптера по Recipient
	adapterConfig, err := resolveAdapterConfig(recipient, opts.ConfigsDir, opts.DefaultConfig)
	if err != nil {
		return err
	}

	// 3. Создаём адаптер
	adapter, err := adapters.New(ctx, *adapterConfig)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	// 4. Выполняем запрос
	fmt.Printf("  Executing query on table '%s'...\n", tableName)
	var packets []*packet.DataPacket
	if reqPacket.Query != nil {
		// Экспорт с фильтрами из request
		// sender/recipient в response переворачиваются: отвечает recipient → sender запроса
		packets, err = adapter.ExportTableWithQuery(ctx, tableName, reqPacket.Query, recipient, sender)
	} else {
		// Экспорт без фильтров
		packets, err = adapter.ExportTable(ctx, tableName)
	}
	if err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}

	if len(packets) == 0 {
		fmt.Println("  Warning: no data returned")
		return nil
	}

	// 5. Проставляем InReplyTo и корректируем Sender/Recipient во всех пакетах
	for _, pkt := range packets {
		pkt.Header.InReplyTo = messageID
		pkt.Header.Sender = recipient
		pkt.Header.Recipient = sender
	}

	fmt.Printf("  Generated %d response packet(s)\n", len(packets))

	// 6. Определяем выходной файл
	outputFile := opts.OutputFile
	if outputFile == "" {
		base := strings.TrimSuffix(opts.RequestFile, filepath.Ext(opts.RequestFile))
		outputFile = base + "_response.tdtp.xml"
	}

	// 7. Записываем ответ
	generator := packet.NewGenerator()
	if len(packets) == 1 {
		xml, err := generator.ToXML(packets[0], true)
		if err != nil {
			return fmt.Errorf("failed to marshal response: %w", err)
		}
		if err := os.WriteFile(outputFile, xml, 0o600); err != nil {
			return fmt.Errorf("failed to write response: %w", err)
		}
		fmt.Printf("  Response written to: %s\n", outputFile)
	} else {
		for i, pkt := range packets {
			ext := filepath.Ext(outputFile)
			base := outputFile[:len(outputFile)-len(ext)]
			partFile := fmt.Sprintf("%s_part_%d_of_%d%s", base, i+1, len(packets), ext)

			xml, err := generator.ToXML(pkt, true)
			if err != nil {
				return fmt.Errorf("failed to marshal response part %d: %w", i+1, err)
			}
			if err := os.WriteFile(partFile, xml, 0o600); err != nil {
				return fmt.Errorf("failed to write response part %d: %w", i+1, err)
			}
			fmt.Printf("  Response part %d/%d written to: %s\n", i+1, len(packets), partFile)
		}
	}

	fmt.Printf("Done. InReplyTo: %s\n", messageID)
	return nil
}
