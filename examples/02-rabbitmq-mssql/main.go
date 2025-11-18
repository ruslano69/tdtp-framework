package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/audit"
	"github.com/ruslano69/tdtp-framework-main/pkg/brokers"
	"github.com/ruslano69/tdtp-framework-main/pkg/processors"
	"github.com/ruslano69/tdtp-framework-main/pkg/resilience"
	"github.com/ruslano69/tdtp-framework-main/pkg/retry"
)

// RabbitMQ + MSSQL Integration Example
//
// Сценарий: Экспорт данных из MSSQL в RabbitMQ с аудитом, маскированием
// и защитой от сбоев (circuit breaker + retry)
//
// Реальный use case:
// - MSSQL база данных с заказами
// - RabbitMQ для отправки в другие системы
// - Маскирование PII данных (email, phone)
// - Аудит всех операций (GDPR compliance)
// - Circuit breaker для защиты от перегрузки RabbitMQ
// - Retry с exponential backoff при сбоях

func main() {
	ctx := context.Background()

	// 1. Настройка Audit Logger
	auditLogger := setupAuditLogger()
	defer auditLogger.Close()

	log.Println("=== RabbitMQ + MSSQL Integration Example ===")
	auditLogger.LogSuccess(ctx, audit.OpAuthenticate).
		WithUser("system").
		WithMetadata("example", "rabbitmq-mssql")

	// 2. Подключение к MSSQL
	mssqlAdapter, err := setupMSSQLAdapter(auditLogger)
	if err != nil {
		log.Fatalf("Failed to setup MSSQL adapter: %v", err)
	}
	defer mssqlAdapter.Close()

	// 3. Подключение к RabbitMQ с Circuit Breaker
	rabbitMQAdapter, circuitBreaker := setupRabbitMQWithCircuitBreaker(auditLogger)
	defer rabbitMQAdapter.Close()

	// 4. Настройка процессоров (маскирование данных)
	processors := setupProcessors()

	// 5. Экспорт данных из MSSQL
	log.Println("\n--- Step 1: Export from MSSQL ---")
	startTime := time.Now()

	data, err := exportFromMSSQL(ctx, mssqlAdapter, auditLogger)
	if err != nil {
		log.Fatalf("Export failed: %v", err)
	}

	auditLogger.LogSuccess(ctx, audit.OpExport).
		WithUser("system").
		WithSource("mssql://orders-db").
		WithResource("orders").
		WithRecordsAffected(int64(len(data))).
		WithDuration(time.Since(startTime))

	log.Printf("Exported %d records from MSSQL\n", len(data))

	// 6. Применение процессоров (маскирование)
	log.Println("\n--- Step 2: Apply Data Masking ---")
	maskedData := applyProcessors(ctx, data, processors, auditLogger)
	log.Printf("Masked %d records\n", len(maskedData))

	// 7. Отправка в RabbitMQ с Circuit Breaker и Retry
	log.Println("\n--- Step 3: Send to RabbitMQ with Protection ---")
	err = sendToRabbitMQWithProtection(
		ctx,
		rabbitMQAdapter,
		circuitBreaker,
		maskedData,
		auditLogger,
	)

	if err != nil {
		log.Printf("Send failed: %v\n", err)
		auditLogger.LogFailure(ctx, audit.OpSync, err).
			WithTarget("rabbitmq://orders-queue")
	} else {
		log.Println("✓ Successfully sent to RabbitMQ")
		auditLogger.LogSuccess(ctx, audit.OpSync).
			WithUser("system").
			WithTarget("rabbitmq://orders-queue").
			WithRecordsAffected(int64(len(maskedData))).
			WithDuration(time.Since(startTime))
	}

	// 8. Статистика Circuit Breaker
	printCircuitBreakerStats(circuitBreaker)

	log.Println("\n=== Integration Complete ===")
}

// setupAuditLogger - настройка audit logger с file + console
func setupAuditLogger() *audit.AuditLogger {
	// File appender для постоянного хранения
	fileAppender, err := audit.NewFileAppender(audit.FileAppenderConfig{
		FilePath:   "./logs/rabbitmq-mssql.log",
		MaxSize:    50, // 50 MB
		MaxBackups: 10,
		Level:      audit.LevelStandard, // Без sensitive data
		FormatJSON: true,
	})
	if err != nil {
		log.Fatalf("Failed to create file appender: %v", err)
	}

	// Console appender для отладки
	consoleAppender := audit.NewConsoleAppender(audit.LevelMinimal, false)

	// Multi appender
	multiAppender := audit.NewMultiAppender(fileAppender, consoleAppender)

	// Logger с async режимом
	config := audit.DefaultConfig()
	config.AsyncMode = true
	config.BufferSize = 1000
	config.DefaultUser = "system"
	config.DefaultSource = "rabbitmq-mssql-integration"

	return audit.NewLogger(config, multiAppender)
}

// setupMSSQLAdapter - настройка MSSQL адаптера
func setupMSSQLAdapter(auditLogger *audit.AuditLogger) (*adapter.MSSQLAdapter, error) {
	// В реальном сценарии используйте переменные окружения
	dsn := "sqlserver://sa:YourPassword@localhost:1433?database=OrdersDB"

	// Для примера создадим mock adapter
	// В production используйте реальный DSN
	log.Println("📊 Connecting to MSSQL: OrdersDB")
	log.Println("   (using mock data for example)")

	// mssqlAdapter, err := adapter.NewMSSQLAdapter(dsn)
	// return mssqlAdapter, err

	// Mock для примера
	return &adapter.MSSQLAdapter{}, nil
}

// setupRabbitMQWithCircuitBreaker - RabbitMQ с circuit breaker
func setupRabbitMQWithCircuitBreaker(auditLogger *audit.AuditLogger) (*brokers.RabbitMQBroker, *resilience.CircuitBreaker) {
	// RabbitMQ connection
	rabbitURL := "amqp://guest:guest@localhost:5672/"

	log.Println("🐰 Connecting to RabbitMQ")
	log.Println("   (using mock for example)")

	// broker, err := brokers.NewRabbitMQBroker(rabbitURL, "orders-queue")
	// if err != nil {
	// 	log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	// }

	// Circuit Breaker для защиты RabbitMQ
	cbConfig := resilience.DefaultConfig("rabbitmq")
	cbConfig.MaxFailures = 5
	cbConfig.Timeout = 30 * time.Second
	cbConfig.SuccessThreshold = 2
	cbConfig.OnStateChange = func(name string, from, to resilience.State) {
		log.Printf("⚡ Circuit Breaker [%s]: %s → %s\n", name, from, to)

		// Логируем в audit
		auditLogger.LogOperation(context.Background(), audit.OpUpdate, audit.StatusSuccess).
			WithMetadata("circuit_breaker", name).
			WithMetadata("state_from", from.String()).
			WithMetadata("state_to", to.String())
	}

	circuitBreaker, err := resilience.New(cbConfig)
	if err != nil {
		log.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Mock для примера
	return &brokers.RabbitMQBroker{}, circuitBreaker
}

// setupProcessors - настройка data processors
func setupProcessors() *processor.Chain {
	chain := processor.NewChain()

	// FieldMasker для PII данных
	masker := processor.NewFieldMasker()
	masker.AddMaskRule("customer_email", processor.MaskEmail)
	masker.AddMaskRule("customer_phone", processor.MaskPhone)
	masker.AddMaskRule("billing_card", processor.MaskFirst2Last2)

	// FieldNormalizer
	normalizer := processor.NewFieldNormalizer()
	normalizer.AddNormalization("customer_email", processor.NormalizeEmail)
	normalizer.AddNormalization("customer_phone", processor.NormalizePhone)

	// FieldValidator
	validator := processor.NewFieldValidator()
	validator.AddValidation("customer_email", processor.ValidateEmail)
	validator.AddValidation("order_total", processor.ValidatePositiveNumber)

	chain.Add(normalizer)
	chain.Add(validator)
	chain.Add(masker)

	return chain
}

// exportFromMSSQL - экспорт данных из MSSQL
func exportFromMSSQL(
	ctx context.Context,
	mssqlAdapter *adapter.MSSQLAdapter,
	auditLogger *audit.AuditLogger,
) ([]map[string]interface{}, error) {
	// В реальном сценарии:
	// query := "SELECT order_id, customer_email, customer_phone, billing_card, order_total FROM orders WHERE created_at > @last_sync"
	// return mssqlAdapter.QueryContext(ctx, query)

	// Mock данные для примера
	mockData := []map[string]interface{}{
		{
			"order_id":       "ORD-001",
			"customer_email": "john.doe@company.com",
			"customer_phone": "+1-555-123-4567",
			"billing_card":   "4532-1234-5678-9010",
			"order_total":    "150.00",
			"created_at":     time.Now().Add(-1 * time.Hour),
		},
		{
			"order_id":       "ORD-002",
			"customer_email": "jane.smith@example.com",
			"customer_phone": "+1-555-987-6543",
			"billing_card":   "5412-9876-5432-1098",
			"order_total":    "75.50",
			"created_at":     time.Now().Add(-30 * time.Minute),
		},
		{
			"order_id":       "ORD-003",
			"customer_email": "bob.wilson@test.org",
			"customer_phone": "+1-555-456-7890",
			"billing_card":   "3782-8224-6310-005",
			"order_total":    "225.75",
			"created_at":     time.Now().Add(-15 * time.Minute),
		},
	}

	log.Printf("Query: SELECT * FROM orders (last 3 records)\n")
	for _, record := range mockData {
		log.Printf("  • Order: %s, Email: %s, Total: %s\n",
			record["order_id"],
			record["customer_email"],
			record["order_total"],
		)
	}

	return mockData, nil
}

// applyProcessors - применение процессоров к данным
func applyProcessors(
	ctx context.Context,
	data []map[string]interface{},
	processors *processor.Chain,
	auditLogger *audit.AuditLogger,
) []map[string]interface{} {
	maskedData := make([]map[string]interface{}, 0, len(data))

	for _, record := range data {
		// Применяем цепочку процессоров
		processedRecord, err := processors.Process(ctx, record)
		if err != nil {
			log.Printf("⚠️  Processing error for order %s: %v\n", record["order_id"], err)
			auditLogger.LogFailure(ctx, audit.OpTransform, err).
				WithMetadata("order_id", record["order_id"])
			continue
		}

		maskedData = append(maskedData, processedRecord)

		// Показываем результат маскирования
		log.Printf("  • Order: %s, Masked Email: %s, Masked Card: %s\n",
			processedRecord["order_id"],
			processedRecord["customer_email"],
			processedRecord["billing_card"],
		)
	}

	return maskedData
}

// sendToRabbitMQWithProtection - отправка в RabbitMQ с защитой
func sendToRabbitMQWithProtection(
	ctx context.Context,
	rabbitMQ *brokers.RabbitMQBroker,
	circuitBreaker *resilience.CircuitBreaker,
	data []map[string]interface{},
	auditLogger *audit.AuditLogger,
) error {
	// Retry configuration
	retryConfig := retry.Config{
		MaxAttempts: 3,
		Delay:       1 * time.Second,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
		Jitter:      true,
		OnRetry: func(attempt int, err error) {
			log.Printf("🔄 Retry attempt %d: %v\n", attempt, err)
		},
	}

	retryer := retry.NewRetryer(retryConfig)

	// Отправка каждой записи
	for i, record := range data {
		orderID := record["order_id"]
		log.Printf("Sending order %s (%d/%d)...\n", orderID, i+1, len(data))

		// Используем Circuit Breaker + Retry
		err := circuitBreaker.Execute(ctx, func(ctx context.Context) error {
			return retryer.Do(ctx, func() error {
				// В реальном сценарии:
				// return rabbitMQ.Publish(ctx, record)

				// Mock для примера
				// Имитируем случайные сбои
				if i == 1 {
					return fmt.Errorf("temporary network error")
				}

				time.Sleep(100 * time.Millisecond) // Имитация отправки
				return nil
			})
		})

		if err != nil {
			log.Printf("❌ Failed to send order %s: %v\n", orderID, err)
			auditLogger.LogFailure(ctx, audit.OpExport, err).
				WithTarget("rabbitmq://orders-queue").
				WithMetadata("order_id", orderID)

			return fmt.Errorf("failed to send order %s: %w", orderID, err)
		}

		log.Printf("✓ Order %s sent successfully\n", orderID)
	}

	return nil
}

// printCircuitBreakerStats - вывод статистики circuit breaker
func printCircuitBreakerStats(cb *resilience.CircuitBreaker) {
	stats := cb.Stats()

	log.Println("\n--- Circuit Breaker Statistics ---")
	log.Printf("State: %s\n", stats.State)
	log.Printf("Total Requests: %d\n", stats.Counts.Requests)
	log.Printf("Total Successes: %d\n", stats.Counts.TotalSuccesses)
	log.Printf("Total Failures: %d\n", stats.Counts.TotalFailures)
	log.Printf("Consecutive Successes: %d\n", stats.Counts.ConsecutiveSuccesses)
	log.Printf("Consecutive Failures: %d\n", stats.Counts.ConsecutiveFailures)
	log.Printf("Max Running Calls: %d\n", stats.MaxRunningCalls)

	if stats.State == resilience.StateOpen {
		log.Printf("Time Until Half-Open: %s\n", stats.TimeUntilHalfOpen)
	}
}
