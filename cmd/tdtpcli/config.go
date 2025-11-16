package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config представляет полную конфигурацию приложения
type Config struct {
	Database DatabaseConfig `yaml:"database"`
	Tables   []string       `yaml:"tables,omitempty"` // Список таблиц для экспорта (если пусто - все таблицы)
	Broker   BrokerConfig   `yaml:"broker,omitempty"` // Message broker конфигурация (опционально)
}

// DatabaseConfig содержит параметры подключения к БД
type DatabaseConfig struct {
	Type     string `yaml:"type"`               // postgres, sqlite, mssql, mysql
	Host     string `yaml:"host,omitempty"`     // Хост БД
	Port     int    `yaml:"port,omitempty"`     // Порт БД
	User     string `yaml:"user,omitempty"`     // Пользователь (не используется при Windows Auth)
	Password string `yaml:"password,omitempty"` // Пароль (не используется при Windows Auth)
	DBName   string `yaml:"dbname,omitempty"`   // Имя базы данных
	Schema   string `yaml:"schema,omitempty"`   // Схема (для PostgreSQL, MS SQL)
	SSLMode  string `yaml:"sslmode,omitempty"`  // SSL режим (для PostgreSQL)
	Path     string `yaml:"path,omitempty"`     // Путь к файлу (для SQLite)

	// MS SQL специфичные параметры
	WindowsAuth bool `yaml:"windows_auth,omitempty"` // Использовать Windows доменную авторизацию
	// Если WindowsAuth=true, то User и Password игнорируются
	// Драйвер go-mssqldb поддерживает "Integrated Security=SSPI" или "Integrated Security=true"
}

// BrokerConfig содержит параметры message broker
type BrokerConfig struct {
	Type     string `yaml:"type"`               // rabbitmq, msmq
	Host     string `yaml:"host,omitempty"`     // Хост (для RabbitMQ)
	Port     int    `yaml:"port,omitempty"`     // Порт (для RabbitMQ)
	User     string `yaml:"user,omitempty"`     // Пользователь (для RabbitMQ)
	Password string `yaml:"password,omitempty"` // Пароль (для RabbitMQ)
	Queue    string `yaml:"queue"`              // Имя очереди
	VHost    string `yaml:"vhost,omitempty"`    // Virtual host (для RabbitMQ, по умолчанию "/")

	// RabbitMQ специфичные параметры очереди
	// ВАЖНО: Параметры должны совпадать с существующей очередью!
	Durable    bool `yaml:"durable"`     // Очередь переживает перезапуск RabbitMQ (по умолчанию true)
	AutoDelete bool `yaml:"auto_delete"` // Очередь удаляется когда нет consumer'ов (по умолчанию false)
	Exclusive  bool `yaml:"exclusive"`   // Очередь доступна только одному соединению (по умолчанию false)

	// MSMQ специфичные параметры (Windows only)
	QueuePath string `yaml:"queue_path,omitempty"` // Путь к очереди MSMQ (например: ".\\private$\\tdtp_export")
}

// LoadConfig загружает конфигурацию из YAML файла
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Валидация
	if config.Database.Type == "" {
		return nil, fmt.Errorf("database.type is required")
	}

	return &config, nil
}

// ToDSN преобразует DatabaseConfig в DSN строку
func (d *DatabaseConfig) ToDSN() string {
	switch d.Type {
	case "postgres":
		sslmode := d.SSLMode
		if sslmode == "" {
			sslmode = "disable"
		}
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			d.Host, d.Port, d.User, d.Password, d.DBName, sslmode)

	case "sqlite":
		return d.Path

	case "mssql":
		// MS SQL Server connection string
		// Формат: server=host,port;user id=user;password=pass;database=db;encrypt=disable
		if d.WindowsAuth {
			// Windows доменная авторизация (Integrated Security)
			// Формат: server=host,port;database=db;Integrated Security=SSPI;encrypt=disable
			return fmt.Sprintf("server=%s,%d;database=%s;Integrated Security=SSPI;encrypt=disable",
				d.Host, d.Port, d.DBName)
		}
		// SQL Server авторизация
		return fmt.Sprintf("server=%s,%d;user id=%s;password=%s;database=%s;encrypt=disable",
			d.Host, d.Port, d.User, d.Password, d.DBName)

	case "mysql":
		// MySQL connection string
		// Формат: user:password@tcp(host:port)/dbname
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
			d.User, d.Password, d.Host, d.Port, d.DBName)

	default:
		return ""
	}
}

// GetDefaultConfigPath возвращает путь к конфигу рядом с исполняемым файлом
func GetDefaultConfigPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "config.yaml"
	}
	exeDir := filepath.Dir(exePath)
	return filepath.Join(exeDir, "config.yaml")
}

// EnsureConfigExists проверяет существование конфига и возвращает путь
func EnsureConfigExists() (string, error) {
	path := GetDefaultConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("config file not found: %s", path)
	}
	return path, nil
}

// CreateConfigTemplate создает шаблон конфига для указанного типа БД
func CreateConfigTemplate(path string, dbType string) error {
	var template string

	switch dbType {
	case "postgres":
		template = `# PostgreSQL Configuration
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: your_password
  dbname: tdtp_db
  schema: public
  sslmode: disable

# Список таблиц для экспорта (опционально, если не указан - экспортируются все)
# tables:
#   - Customers
#   - Products
#   - Orders

# Message Broker (опционально, для интеграции с СЭД)
# broker:
#   type: rabbitmq
#   host: localhost
#   port: 5672
#   user: guest
#   password: guest
#   queue: tdtp_export
#   vhost: /
`

	case "sqlite":
		template = `# SQLite Configuration
database:
  type: sqlite
  path: ./database.db

# Список таблиц для экспорта (опционально, если не указан - экспортируются все)
# tables:
#   - Customers
#   - Products

# Message Broker (опционально, для интеграции с СЭД)
# broker:
#   type: rabbitmq
#   host: localhost
#   port: 5672
#   user: guest
#   password: guest
#   queue: tdtp_export
`

	case "mssql":
		template = `# MS SQL Server Configuration
database:
  type: mssql
  host: localhost
  port: 1433
  # SQL Server авторизация
  user: sa
  password: YourPassword123!
  dbname: MyDatabase
  schema: dbo

  # Или Windows доменная авторизация (раскомментировать, если нужна)
  # windows_auth: true
  # При windows_auth=true параметры user и password игнорируются

# Список таблиц для экспорта (опционально, если не указан - экспортируются все)
# Примеры справочников для СЭД:
tables:
  - Customers      # Справочник контрагентов
  - Departments    # Справочник подразделений
  - Employees      # Справочник сотрудников
  - DocumentTypes  # Справочник типов документов

# Message Broker для интеграции с СЭД
broker:
  # RabbitMQ (кросс-платформенный, рекомендуется для удаленного доступа)
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_export
  vhost: /

  # Параметры очереди RabbitMQ (ВАЖНО: должны совпадать с существующей очередью!)
  durable: true      # Очередь переживает перезапуск RabbitMQ
  auto_delete: false # Очередь НЕ удаляется когда нет consumer'ов
  exclusive: false   # Очередь доступна нескольким соединениям

  # Или MSMQ (только Windows, для локальной интеграции)
  # type: msmq
  # queue_path: ".\\private$\\tdtp_export"
`

	case "mysql":
		template = `# MySQL Configuration (under development)
database:
  type: mysql
  host: localhost
  port: 3306
  user: tdtp_user
  password: your_password
  dbname: tdtp_db

# Список таблиц для экспорта
# tables:
#   - Customers

# Message Broker
# broker:
#   type: rabbitmq
#   host: localhost
#   port: 5672
#   user: guest
#   password: guest
#   queue: tdtp_export
`

	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	// Создаем файл
	return os.WriteFile(path, []byte(template), 0644)
}
