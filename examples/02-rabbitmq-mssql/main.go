package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/ruslano69/tdtp-framework/pkg/processors"

	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
)

// Example 02: MSSQL + RabbitMQ Integration Test
//
// Интеграционный тест проверяет:
// - Подключение к MSSQL с разными типами данных
// - Фильтрацию через TDTQL (SQL -> TDTQL -> SQL)
// - Маскирование PII данных
// - Отправку в RabbitMQ
// - Аудит всех операций

const (
	// MSSQL конфигурация
	mssqlHost     = "localhost"
	mssqlPort     = "1433"
	mssqlUser     = "sa"
	mssqlPassword = "Tdtp_Pass_123!"
	mssqlDatabase = "example02_db"

	// RabbitMQ конфигурация
	rabbitmqHost     = "localhost"
	rabbitmqPort     = 5672
	rabbitmqUser     = "tdtp"
	rabbitmqPassword = "tdtp_pass_123"
	rabbitmqQueue    = "tdtp-orders"
)

func main() {
	ctx := context.Background()

	log.Println("=== Example 02: MSSQL + RabbitMQ Integration Test ===")
	log.Println()

	// 1. Setup Audit Logger
	auditLogger := setupAuditLogger()
	defer auditLogger.Close()

	auditLogger.LogSuccess(ctx, audit.OpAuthenticate).
		WithUser("system").
		WithMetadata("example", "02-rabbitmq-mssql")

	// 2. Подключение к MSSQL
	log.Println("Step 1: Connecting to MSSQL...")
	mssqlAdapter, err := connectMSSQL(ctx)
	if err != nil {
		log.Fatalf("❌ Failed to connect to MSSQL: %v", err)
	}
	defer mssqlAdapter.Close(ctx)
	log.Println("✓ Connected to MSSQL")

	// 3. Проверка схемы таблицы
	log.Println("\nStep 2: Checking table schema...")
	schema, err := mssqlAdapter.GetTableSchema(ctx, "orders")
	if err != nil {
		log.Fatalf("❌ Failed to get schema: %v", err)
	}
	log.Printf("✓ Schema loaded: %d fields\n", len(schema.Fields))
	displaySchema(schema)

	// 4. Тест 1: Экспорт всех данных
	log.Println("\n=== TEST 1: Export ALL data ===")
	testExportAll(ctx, mssqlAdapter, auditLogger)

	// 5. Тест 2: Экспорт с простым фильтром
	log.Println("\n=== TEST 2: Export with simple filter (is_paid = 1) ===")
	testExportWithSimpleFilter(ctx, mssqlAdapter, auditLogger)

	// 6. Тест 3: Экспорт со сложным фильтром
	log.Println("\n=== TEST 3: Export with complex filter ===")
	testExportWithComplexFilter(ctx, mssqlAdapter, auditLogger)

	// 7. Тест 4: Маскирование PII данных
	log.Println("\n=== TEST 4: Data masking ===")
	testDataMasking(ctx, mssqlAdapter, schema, auditLogger)

	// 8. Тест 5: Отправка в RabbitMQ
	log.Println("\n=== TEST 5: Send to RabbitMQ ===")
	testRabbitMQ(ctx, mssqlAdapter, auditLogger)

	log.Println("\n✓ All tests completed successfully!")
	log.Println("\nCheck:")
	log.Println("  - ./output/*.xml for exported data")
	log.Println("  - ./logs/example02.log for audit trail")
	log.Println("  - http://localhost:15672 for RabbitMQ management (tdtp/tdtp_pass_123)")
}

// connectMSSQL создает подключение к MSSQL
func connectMSSQL(ctx context.Context) (adapters.Adapter, error) {
	dsn := fmt.Sprintf(
		"sqlserver://%s:%s@%s:%s?database=%s&encrypt=disable",
		mssqlUser,
		mssqlPassword,
		mssqlHost,
		mssqlPort,
		mssqlDatabase,
	)

	adapter, err := adapters.New(ctx, adapters.Config{
		Type:     "mssql",
		DSN:      dsn,
		MaxConns: 5,
		MinConns: 1,
		Timeout:  30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}

	err = adapter.Ping(ctx)
	if err != nil {
		adapter.Close(ctx)
		return nil, fmt.Errorf("failed to ping: %w", err)
	}

	return adapter, nil
}

// setupAuditLogger настройка audit logger
func setupAuditLogger() *audit.AuditLogger {
	os.MkdirAll("./logs", 0755)

	fileAppender, err := audit.NewFileAppender(audit.FileAppenderConfig{
		FilePath:   "./logs/example02.log",
		MaxSize:    50,
		MaxBackups: 10,
		Level:      audit.LevelStandard,
		FormatJSON: true,
	})
	if err != nil {
		log.Printf("Warning: Failed to create file appender: %v\n", err)
		consoleAppender := audit.NewConsoleAppender(audit.LevelMinimal, false)
		config := audit.DefaultConfig()
		return audit.NewLogger(config, consoleAppender)
	}

	consoleAppender := audit.NewConsoleAppender(audit.LevelMinimal, false)
	multiAppender := audit.NewMultiAppender(fileAppender, consoleAppender)

	config := audit.DefaultConfig()
	config.AsyncMode = true
	config.DefaultUser = "system"

	return audit.NewLogger(config, multiAppender)
}

// displaySchema показывает схему таблицы
func displaySchema(schema packet.Schema) {
	log.Println("  Fields:")
	for i, field := range schema.Fields {
		keyStr := ""
		if field.Key {
			keyStr = " [PK]"
		}
		log.Printf("    %2d. %-20s %s%s", i+1, field.Name, field.Type, keyStr)
	}
}

// testExportAll тест экспорта всех данных
func testExportAll(ctx context.Context, adapter adapters.Adapter, logger *audit.AuditLogger) {
	startTime := time.Now()

	packets, err := adapter.ExportTable(ctx, "orders")
	if err != nil {
		log.Printf("❌ Export failed: %v\n", err)
		return
	}

	duration := time.Since(startTime)
	totalRecords := 0
	for _, pkt := range packets {
		totalRecords += pkt.Header.RecordsInPart
	}

	log.Printf("✓ Exported %d records in %d packet(s), time: %v\n", totalRecords, len(packets), duration)

	logger.LogSuccess(ctx, audit.OpExport).
		WithResource("orders").
		WithRecordsAffected(int64(totalRecords)).
		WithDuration(duration)

	// Сохраняем в файл
	savePackets(packets, "./output/test1-all-data.xml")
}

// testExportWithSimpleFilter тест с простым фильтром
func testExportWithSimpleFilter(ctx context.Context, adapter adapters.Adapter, logger *audit.AuditLogger) {
	startTime := time.Now()

	// SQL запрос
	sqlQuery := "SELECT * FROM orders WHERE is_paid = 1"
	log.Printf("SQL: %s\n", sqlQuery)

	// Транслируем SQL -> TDTQL
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sqlQuery)
	if err != nil {
		log.Printf("❌ Translation failed: %v\n", err)
		return
	}

	// Показываем TDTQL
	log.Println("TDTQL filters:")
	displayQuery(query)

	// Экспортируем
	packets, err := adapter.ExportTableWithQuery(ctx, "orders", query, "example02", "receiver")
	if err != nil {
		log.Printf("❌ Export failed: %v\n", err)
		return
	}

	duration := time.Since(startTime)
	totalRecords := 0
	for _, pkt := range packets {
		totalRecords += pkt.Header.RecordsInPart
	}

	log.Printf("✓ Exported %d records (filtered), time: %v\n", totalRecords, duration)

	logger.LogSuccess(ctx, audit.OpQuery).
		WithResource("orders").
		WithRecordsAffected(int64(totalRecords)).
		WithDuration(duration).
		WithMetadata("filter", "is_paid=1")

	savePackets(packets, "./output/test2-paid-orders.xml")
}

// testExportWithComplexFilter тест со сложным фильтром
func testExportWithComplexFilter(ctx context.Context, adapter adapters.Adapter, logger *audit.AuditLogger) {
	startTime := time.Now()

	// Сложный SQL запрос
	sqlQuery := "SELECT * FROM orders WHERE is_paid = 1 AND is_shipped = 0 AND quantity > 2 ORDER BY order_date DESC"
	log.Printf("SQL: %s\n", sqlQuery)

	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sqlQuery)
	if err != nil {
		log.Printf("❌ Translation failed: %v\n", err)
		return
	}

	log.Println("TDTQL filters:")
	displayQuery(query)

	packets, err := adapter.ExportTableWithQuery(ctx, "orders", query, "example02", "receiver")
	if err != nil {
		log.Printf("❌ Export failed: %v\n", err)
		return
	}

	duration := time.Since(startTime)
	totalRecords := 0
	for _, pkt := range packets {
		totalRecords += pkt.Header.RecordsInPart
	}

	log.Printf("✓ Exported %d records (complex filter), time: %v\n", totalRecords, duration)

	logger.LogSuccess(ctx, audit.OpQuery).
		WithResource("orders").
		WithRecordsAffected(int64(totalRecords)).
		WithDuration(duration).
		WithMetadata("filter", "complex")

	savePackets(packets, "./output/test3-complex-filter.xml")
}

// testDataMasking тест маскирования данных
func testDataMasking(ctx context.Context, adapter adapters.Adapter, schema packet.Schema, logger *audit.AuditLogger) {
	startTime := time.Now()

	// Экспортируем все данные
	packets, err := adapter.ExportTable(ctx, "orders")
	if err != nil {
		log.Printf("❌ Export failed: %v\n", err)
		return
	}

	if len(packets) == 0 {
		log.Println("❌ No packets to mask")
		return
	}

	// Создаем процессор маскирования
	masker := processors.NewFieldMasker(map[string]processors.MaskPattern{
		"customer_email": processors.MaskPartial,
		"customer_phone": processors.MaskMiddle,
		"credit_card":    processors.MaskFirst2Last2,
		"ssn":            processors.MaskStars,
	})

	chain := processors.NewChain()
	chain.Add(masker)

	// Используем API фреймворка для извлечения данных
	originalData := packets[0].GetRows()

	// Применяем маскирование
	maskedData, err := chain.Process(ctx, originalData, schema)
	if err != nil {
		log.Printf("❌ Masking failed: %v\n", err)
		return
	}

	// Используем API фреймворка для установки данных
	packets[0].SetRows(maskedData)

	duration := time.Since(startTime)
	log.Printf("✓ Masked %d records in %v\n", len(maskedData), duration)

	logger.LogSuccess(ctx, audit.OpMask).
		WithResource("orders").
		WithRecordsAffected(int64(len(maskedData))).
		WithDuration(duration)

	savePackets(packets, "./output/test4-masked-data.xml")

	// Показываем примеры маскированных данных
	if len(maskedData) > 0 {
		log.Println("  Sample masked row:")
		emailIdx := findFieldIndex(schema, "customer_email")
		cardIdx := findFieldIndex(schema, "credit_card")
		if emailIdx >= 0 && cardIdx >= 0 && len(maskedData[0]) > cardIdx {
			log.Printf("    Email: %s", maskedData[0][emailIdx])
			log.Printf("    Card:  %s", maskedData[0][cardIdx])
		}
	}
}

// testRabbitMQ тест отправки в RabbitMQ
func testRabbitMQ(ctx context.Context, adapter adapters.Adapter, logger *audit.AuditLogger) {
	startTime := time.Now()

	// Подключаемся к RabbitMQ
	broker, err := brokers.New(brokers.Config{
		Type:     "rabbitmq",
		Host:     rabbitmqHost,
		Port:     rabbitmqPort,
		User:     rabbitmqUser,
		Password: rabbitmqPassword,
		Queue:    rabbitmqQueue,
	})
	if err != nil {
		log.Printf("❌ Failed to create broker: %v\n", err)
		return
	}

	err = broker.Connect(ctx)
	if err != nil {
		log.Printf("❌ Failed to connect to RabbitMQ: %v\n", err)
		return
	}
	defer broker.Close()

	log.Println("✓ Connected to RabbitMQ")

	// Экспортируем данные
	packets, err := adapter.ExportTable(ctx, "orders")
	if err != nil {
		log.Printf("❌ Export failed: %v\n", err)
		return
	}

	// Отправляем каждый пакет в RabbitMQ
	gen := packet.NewGenerator()
	sentCount := 0

	for _, pkt := range packets {
		xmlData, err := gen.ToXML(pkt, false) // compact XML
		if err != nil {
			log.Printf("❌ XML serialization failed: %v\n", err)
			continue
		}

		err = broker.Send(ctx, xmlData)
		if err != nil {
			log.Printf("❌ Send to RabbitMQ failed: %v\n", err)
			continue
		}
		sentCount++
	}

	duration := time.Since(startTime)
	log.Printf("✓ Sent %d packet(s) to RabbitMQ queue '%s', time: %v\n", sentCount, rabbitmqQueue, duration)

	logger.LogSuccess(ctx, audit.OpSync).
		WithTarget(fmt.Sprintf("rabbitmq://%s:%d/%s", rabbitmqHost, rabbitmqPort, rabbitmqQueue)).
		WithRecordsAffected(int64(sentCount)).
		WithDuration(duration)
}

// savePackets сохраняет пакеты в XML файл
func savePackets(packets []*packet.DataPacket, filename string) {
	os.MkdirAll("./output", 0755)

	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Warning: Failed to create file %s: %v\n", filename, err)
		return
	}
	defer file.Close()

	gen := packet.NewGenerator()
	for i, pkt := range packets {
		xmlData, err := gen.ToXML(pkt, true)
		if err != nil {
			log.Printf("Warning: Failed to marshal packet %d: %v\n", i+1, err)
			continue
		}
		file.Write(xmlData)
		if i < len(packets)-1 {
			file.WriteString("\n\n")
		}
	}

	log.Printf("  Saved to: %s\n", filename)
}

// displayQuery показывает TDTQL запрос
func displayQuery(query *packet.Query) {
	if query.Filters != nil {
		data, _ := json.MarshalIndent(query.Filters, "  ", "  ")
		log.Printf("  %s\n", string(data))
	}
	if query.OrderBy != nil && len(query.OrderBy.Fields) > 0 {
		log.Printf("  ORDER BY: %+v\n", query.OrderBy.Fields)
	}
	if query.Limit > 0 {
		log.Printf("  LIMIT: %d\n", query.Limit)
	}
}

// findFieldIndex находит индекс поля в схеме
func findFieldIndex(schema packet.Schema, fieldName string) int {
	for i, field := range schema.Fields {
		if field.Name == fieldName {
			return i
		}
	}
	return -1
}
