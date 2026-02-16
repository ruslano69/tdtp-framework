# Первичный аудит готовности к тестовой эксплуатации

Дата: 2026-02-13  
Репозиторий: `tdtp-framework`

## 1) Цель и рамки аудита

Провести первичную (high-level) оценку готовности проекта к **тестовой эксплуатации**: проверить базовую исполнимость, состояние автотестов, наличие CI/security-процессов, а также выявить очевидные блокеры.

## 2) Что проверено

- Структура репозитория и документация (README, тестовые гайды, integration docs).
- Наличие CI/Lint/Security workflows в GitHub Actions.
- Локальный запуск ключевых тестов по пакетам.
- Локальная проверка `go vet` для core-пакетов.
- Проверка инициализации benchmark-данных SQLite через штатный скрипт из `scripts/`.
- Проверка доступности docker-инструментов для интеграционных тестов.

## 3) Положительные сигналы

1. В репозитории есть зрелые CI-пайплайны:
   - `ci.yml` (build + test + coverage)
   - `lint.yml` (golangci-lint, gofmt, go vet)
   - `security.yml` (govulncheck + Nancy)
2. Базовые модульные тесты core-части проходят (`pkg/core/...`).
3. CLI-пакеты (`cmd/tdtpcli/...`) компилируются и тестируются успешно.
4. После генерации benchmark БД через `python3 scripts/create_benchmark_db.py` тесты SQLite-адаптера проходят (`TestBenchmarkSetup` и пакет `pkg/adapters/sqlite` целиком).
5. Есть отдельная документация для интеграционных тестов и тест-плана с внешней инфраструктурой (Docker, RabbitMQ, PostgreSQL, MSSQL).

## 4) Обнаруженные проблемы и риски

### Блокер A: красные тесты в `pkg/etl`

Локально воспроизводится падение `TestLoadConfig`:

- Тест ожидает одни сообщения об ошибках/валидном конфиге,
- но фактическая валидация требует `transform.result_table` и возвращает другие ошибки.

Это указывает на рассинхронизацию между логикой валидации (`pkg/etl/config.go`) и тестовыми сценариями (`pkg/etl/config_test.go`).

**Риск для тестовой эксплуатации:** средний/высокий (конфигурационный контур ETL нестабилен в regression-проверках).

### Подтверждено: SQLite benchmark-контур работоспособен при штатной подготовке

Изначальное падение `pkg/adapters/sqlite` (`table Users not found or has no columns`) воспроизводилось из-за отсутствия подготовленной benchmark БД.
После запуска штатного скрипта `scripts/create_benchmark_db.py` тесты проходят.

**Риск:** низкий при соблюдении precondition (генерация benchmark БД перед запуском тестов).

### Ограничение окружения: нет Docker

В текущем окружении отсутствует `docker`, поэтому интеграционные сценарии из `tests/integration` в полном объёме не проверялись.

**Риск:** средний (не подтверждён end-to-end контур с брокерами и СУБД).

## 5) Предварительный вывод о готовности

**Статус: условно не готов к тестовой эксплуатации “как есть”** (Go/No-Go: **No-Go** до устранения блокера ETL и подтверждения E2E-интеграций).

Проект выглядит функционально зрелым по архитектуре и процессам. Основной технический блокер на текущем этапе — рассинхрон в ETL-конфигурации/тестах (`pkg/etl`). SQLite-контур не является блокером при корректной подготовке данных.

## 6) Рекомендуемый план доработок (минимум перед Go)

1. Синхронизировать ETL-конфиг и тесты:
   - либо сделать `transform.result_table` опциональным (с default),
   - либо обновить тест-кейсы/fixtures под текущий контракт.
2. Явно документировать precondition для SQLite benchmark-тестов:
   - перед запуском выполнять `python3 scripts/create_benchmark_db.py`.
3. Разделить “быстрые unit” и “env-dependent/integration” тесты тегами/пакетами, чтобы базовый `go test` был предсказуемым.
4. Прогнать интеграционный smoke-run в Docker (RabbitMQ + PostgreSQL + MSSQL) по `tests/integration/README.md`.
5. Зафиксировать “критерии входа в тестовую эксплуатацию” (checklist):
   - 0 failing unit tests,
   - green lint/security,
   - минимум один green E2E pipeline.

## 7) Фактически выполненные команды

```bash
python3 scripts/create_benchmark_db.py
go test ./pkg/core/...
go test ./cmd/tdtpcli/...
go test ./pkg/etl -run TestLoadConfig -count=1
go test ./pkg/adapters/sqlite -run TestBenchmarkSetup -count=1
go test ./pkg/adapters/sqlite -count=1
go vet ./pkg/core/...
docker --version
```


## 8) Проверка наличия конфигураций для локальной тестовой среды (Docker)

Да, в репозитории есть готовые конфигурации и шаблоны для поднятия локальной тестовой среды в Docker:

- `deployments/docker-compose.example.yml` — базовая среда (PostgreSQL, MySQL, RabbitMQ, Adminer).
- `deployments/docker-compose.mssql.yml` — сценарии MS SQL (dev/prod-sim/2022).
- `tests/integration/docker-compose.yml` — compose для интеграционных тестов (RabbitMQ + MSSQL + PostgreSQL).
- `scripts/test_setup/generate_docker_compose.py` — генератор root `docker-compose.yml` (PostgreSQL + RabbitMQ + Kafka + Zookeeper).
- `scripts/test_setup/setup_all.py` и `scripts/test_setup/README.md` — автоматизированный сценарий подготовки тестовой среды.
- `tests/integration/Makefile` — команды запуска/остановки контейнеров и запуска integration tests.

Итого: конфигурационные файлы для формирования локальной тестовой среды через Docker **присутствуют**, покрывают как общий dev/test контур, так и специализированные сценарии интеграционных тестов.
