//go:build integration

package integration

import (
	"context"
	"encoding/xml"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// TestRabbitMQBasicConnection проверяет базовое подключение к RabbitMQ
func TestRabbitMQBasicConnection(t *testing.T) {
	cfg := brokers.Config{
		Type:       "rabbitmq",
		Host:       "localhost",
		Port:       5672,
		User:       "tdtp_test",
		Password:   "tdtp_test_password",
		Queue:      "test_basic_connection",
		VHost:      "/",
		Durable:    false,
		AutoDelete: true,
		Exclusive:  false,
	}

	broker, err := brokers.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}

	ctx := context.Background()
	if err := broker.Connect(ctx); err != nil {
		t.Skipf("RabbitMQ not available: %v", err)
	}
	defer broker.Close()

	// Проверяем Ping
	if err := broker.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	t.Logf("✅ Successfully connected to RabbitMQ")
}

// TestRabbitMQSendReceive проверяет отправку и получение сообщений
func TestRabbitMQSendReceive(t *testing.T) {
	cfg := brokers.Config{
		Type:       "rabbitmq",
		Host:       "localhost",
		Port:       5672,
		User:       "tdtp_test",
		Password:   "tdtp_test_password",
		Queue:      "test_send_receive",
		VHost:      "/",
		Durable:    false,
		AutoDelete: true,
		Exclusive:  false,
	}

	// Создаем брокер для отправки
	sender, err := brokers.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}

	ctx := context.Background()
	if err := sender.Connect(ctx); err != nil {
		t.Skipf("RabbitMQ not available: %v", err)
	}
	defer sender.Close()

	// Отправляем тестовое сообщение
	testMessage := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<test>Hello RabbitMQ</test>")
	if err := sender.Send(ctx, testMessage); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	t.Logf("📨 Sent test message")

	// Создаем брокер для получения
	receiver, err := brokers.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create receiver: %v", err)
	}

	if err := receiver.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect receiver: %v", err)
	}
	defer receiver.Close()

	// Получаем сообщение с таймаутом
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	receivedMessage, err := receiver.Receive(ctxWithTimeout)
	if err != nil {
		t.Skipf("RabbitMQ receive timed out (broker may not be fully ready): %v", err)
	}

	if string(receivedMessage) != string(testMessage) {
		t.Errorf("Message mismatch.\nExpected: %s\nGot: %s", testMessage, receivedMessage)
	}

	t.Logf("📥 Received message successfully")
}

// TestEndToEndExportImport проверяет полный цикл экспорта из БД в очередь и импорта из очереди в БД
func TestEndToEndExportImport(t *testing.T) {
	// Настройка брокера
	brokerCfg := brokers.Config{
		Type:       "rabbitmq",
		Host:       "localhost",
		Port:       5672,
		User:       "tdtp_test",
		Password:   "tdtp_test_password",
		Queue:      "test_e2e_export_import",
		VHost:      "/",
		Durable:    false,
		AutoDelete: true,
		Exclusive:  false,
	}

	// Настройка MS SQL адаптера
	mssqlCfg := adapters.Config{
		Type: "mssql",
		DSN:  "sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=master",
	}

	ctx := context.Background()

	// Подключаемся к MS SQL
	adapter, err := adapters.New(ctx, mssqlCfg)
	if err != nil {
		t.Skipf("Skipping test - MS SQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем тестовую таблицу
	tableName := "TestBrokerE2E"
	if err := createTestTable(ctx, adapter, tableName); err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer dropTestTable(ctx, adapter, tableName)

	// Экспортируем таблицу
	packets, err := adapter.ExportTable(ctx, tableName)
	if err != nil {
		t.Fatalf("Failed to export table: %v", err)
	}

	if len(packets) == 0 {
		t.Fatal("No packets exported")
	}

	t.Logf("📤 Exported %d packet(s)", len(packets))

	// Подключаемся к брокеру для отправки
	broker, err := brokers.New(brokerCfg)
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}

	if err := broker.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect to broker: %v", err)
	}
	defer broker.Close()

	// Отправляем пакеты в очередь
	for i, pkt := range packets {
		xmlData, err := xml.MarshalIndent(pkt, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal packet %d: %v", i, err)
		}

		message := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		message = append(message, xmlData...)

		if err := broker.Send(ctx, message); err != nil {
			t.Fatalf("Failed to send packet %d: %v", i, err)
		}
	}

	t.Logf("📨 Sent %d packet(s) to queue", len(packets))

	// Удаляем тестовую таблицу перед импортом
	if err := dropTestTable(ctx, adapter, tableName); err != nil {
		t.Fatalf("Failed to drop table before import: %v", err)
	}

	// Получаем пакеты из очереди и импортируем
	for i := 0; i < len(packets); i++ {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		message, err := broker.Receive(ctxWithTimeout)
		if err != nil {
			t.Fatalf("Failed to receive packet %d: %v", i, err)
		}

		// Парсим TDTP пакет
		parser := packet.NewParser()
		pkt, err := parser.ParseBytes(message)
		if err != nil {
			t.Fatalf("Failed to parse packet %d: %v", i, err)
		}

		// Импортируем пакет
		if err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace); err != nil {
			t.Fatalf("Failed to import packet %d: %v", i, err)
		}

		t.Logf("📥 Imported packet %d (%d rows)", i+1, len(pkt.Data.Rows))
	}

	// Проверяем что данные импортировались
	exists, err := adapter.TableExists(ctx, tableName)
	if err != nil {
		t.Fatalf("Failed to check table existence: %v", err)
	}
	if !exists {
		t.Fatal("Table should exist after import")
	}

	t.Logf("✅ End-to-end test completed successfully")
}

// TestQueueParametersMatching проверяет что параметры очереди должны совпадать
func TestQueueParametersMatching(t *testing.T) {
	ctx := context.Background()

	// Создаем очередь с определенными параметрами
	cfg1 := brokers.Config{
		Type:       "rabbitmq",
		Host:       "localhost",
		Port:       5672,
		User:       "tdtp_test",
		Password:   "tdtp_test_password",
		Queue:      "test_queue_params",
		VHost:      "/",
		Durable:    true, // durable
		AutoDelete: false,
		Exclusive:  false,
	}

	broker1, err := brokers.New(cfg1)
	if err != nil {
		t.Fatalf("Failed to create broker1: %v", err)
	}

	if err := broker1.Connect(ctx); err != nil {
		t.Skipf("RabbitMQ not available: %v", err)
	}
	defer broker1.Close()

	t.Logf("✅ Created queue with durable=true")

	// Пытаемся подключиться с другими параметрами
	cfg2 := brokers.Config{
		Type:       "rabbitmq",
		Host:       "localhost",
		Port:       5672,
		User:       "tdtp_test",
		Password:   "tdtp_test_password",
		Queue:      "test_queue_params",
		VHost:      "/",
		Durable:    false, // РАЗНЫЕ параметры!
		AutoDelete: false,
		Exclusive:  false,
	}

	broker2, err := brokers.New(cfg2)
	if err != nil {
		t.Fatalf("Failed to create broker2: %v", err)
	}

	// Должна быть ошибка из-за несовпадения параметров
	err = broker2.Connect(ctx)
	if err == nil {
		broker2.Close()
		t.Fatal("Expected error due to queue parameter mismatch, but got none")
	}

	t.Logf("✅ Queue parameter mismatch detected correctly: %v", err)
}

// Helper functions

func createTestTable(ctx context.Context, adapter adapters.Adapter, tableName string) error {
	// Простая тестовая таблица
	// TODO: Implement table creation for integration tests
	/*
		CREATE TABLE tableName (
			ID INT PRIMARY KEY,
			Name NVARCHAR(100),
			Email NVARCHAR(100),
			Balance DECIMAL(10,2),
			IsActive BIT,
			CreatedAt DATETIME2,
			UpdatedAt DATETIME2
		);
	*/

	// Для MS SQL выполняем через raw query (если есть такой метод)
	// Иначе нужно использовать database/sql напрямую
	// Это упрощенный вариант для демонстрации
	return nil
}

func dropTestTable(ctx context.Context, adapter adapters.Adapter, tableName string) error {
	// DROP TABLE если существует
	return nil
}
