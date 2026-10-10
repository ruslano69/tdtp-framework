# GitHub Actions Workflows

Этот проект использует GitHub Actions для автоматизации CI/CD процессов.

## 📋 Доступные Workflows

### 🧪 CI (ci.yml)
**Триггеры:** Push и Pull Request в ветки `main`, `master`, `develop`

**Задачи:**
- **Test Job:** Запуск тестов на нескольких версиях Go (1.21, 1.22, 1.23)
  - Проверка зависимостей (`go mod verify`)
  - Сборка проекта (`go build`)
  - Запуск тестов с race detector и coverage (`go test -race -coverprofile`)
  - Загрузка coverage в Codecov (только для Go 1.23)
- **Build Job:** Сборка CLI утилиты `tdtpcli`
  - Компиляция бинарника
  - Тестирование работы CLI

**Статус:** ![CI](https://github.com/queuebridge/tdtp/actions/workflows/ci.yml/badge.svg)

---

### 🔍 Lint (lint.yml)
**Триггеры:** Push и Pull Request в ветки `main`, `master`, `develop`

**Задачи:**
- **golangci-lint Job:** Статический анализ кода
  - Использует последнюю версию golangci-lint
  - Конфигурация в `.golangci.yml`
  - Timeout: 5 минут
- **gofmt Job:** Проверка форматирования
  - Проверяет, что весь код отформатирован
  - Выводит diff для неотформатированных файлов
- **govet Job:** Анализ подозрительных конструкций
  - Запуск `go vet` для всех пакетов

**Статус:** ![Lint](https://github.com/queuebridge/tdtp/actions/workflows/lint.yml/badge.svg)

---

### 🔒 Security (security.yml)
**Триггеры:**
- Push и Pull Request в ветки `main`, `master`, `develop`
- Еженедельно по понедельникам в 00:00 UTC (cron)

**Задачи:**
- **govulncheck Job:** Проверка уязвимостей Go
  - Использует официальный инструмент `govulncheck`
  - Сканирует все зависимости на известные уязвимости
- **dependency-review Job:** Проверка зависимостей (только для PR)
  - Анализирует изменения в зависимостях
  - Предупреждает о проблемных зависимостях
- **nancy Job:** Сканирование безопасности с Nancy
  - Проверка зависимостей от Sonatype
  - Continue-on-error для избежания ложных срабатываний

**Статус:** ![Security](https://github.com/queuebridge/tdtp/actions/workflows/security.yml/badge.svg)

---

### 🎲 Fuzz (fuzz.yml)

**Триггеры:** раз в неделю (понедельник, 03:00 UTC) и вручную (`workflow_dispatch`, время на цель задаётся входом `fuzztime`, по умолчанию `3m`).

**Что делает:** фаззит по одной цели на job — разбор пакета (`pkg/core/packet`: быстрый путь против `encoding/xml`, `ParseBytes`, экранирование, compact, колонки, запись→разбор) и распаковку (`pkg/processors`). Затравки этих целей и так идут в каждом `go test`; здесь — настоящий фаззинг. Найденный вход выгружается артефактом: положите его в `testdata/fuzz/<Цель>/`, и он станет регрессионным тестом.

---

### 🚀 Release (release.yml)
**Триггеры:** Push тегов вида `v*.*.*` (например, `v1.2.0`)

**Задачи:**
- Запуск всех тестов
- Сборка бинарников для множества платформ:
  - Linux (amd64, arm64)
  - Windows (amd64)
  - macOS (amd64, arm64)
- Создание SHA256 checksums
- Автоматическое создание GitHub Release с:
  - Скомпилированными бинарниками
  - Checksums файлом
  - Автоматически сгенерированными release notes

**Как создать релиз:**
```bash
git tag -a v1.2.0 -m "Release v1.2.0"
git push origin v1.2.0
```

**Статус:** ![Release](https://github.com/queuebridge/tdtp/actions/workflows/release.yml/badge.svg)

---

## 🛠️ Конфигурация

### golangci-lint (.golangci.yml)
Конфигурация линтера включает следующие проверки:
- **Базовые:** errcheck, gosimple, govet, ineffassign, staticcheck, unused
- **Форматирование:** gofmt, goimports
- **Качество кода:** revive, gocritic, misspell
- **Безопасность:** gosec
- **Производительность:** prealloc
- **Прочее:** unconvert, unparam, exportloopref, nilerr, bodyclose

### Настройки проекта
- **Минимальная версия Go:** 1.21
- **Рекомендуемая версия Go:** 1.23
- **Timeout для тестов:** 2 минуты (по умолчанию в CI)
- **Timeout для линтера:** 5 минут

---

## 📊 Badges для README

Добавьте в ваш README.md:

```markdown
![CI](https://github.com/queuebridge/tdtp/actions/workflows/ci.yml/badge.svg)
![Lint](https://github.com/queuebridge/tdtp/actions/workflows/lint.yml/badge.svg)
![Security](https://github.com/queuebridge/tdtp/actions/workflows/security.yml/badge.svg)
[![codecov](https://codecov.io/gh/queuebridge/tdtp/branch/main/graph/badge.svg)](https://codecov.io/gh/queuebridge/tdtp)
```

---

## 🔧 Локальный запуск

### Запуск тестов локально
```bash
go test -v -race -coverprofile=coverage.txt ./...
```

### Запуск линтера локально
```bash
# Установка golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Запуск линтера
golangci-lint run
```

### Проверка форматирования
```bash
gofmt -s -l .
```

### Запуск govulncheck локально
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## 📝 Рекомендации

1. **Перед коммитом:**
   - Запустите `go fmt ./...`
   - Запустите `go vet ./...`
   - Запустите тесты `go test ./...`

2. **Перед созданием PR:**
   - Убедитесь, что все тесты проходят
   - Запустите линтер локально
   - Проверьте coverage (желательно > 70%)

3. **Создание релиза:**
   - Обновите CHANGELOG.md
   - Создайте тег с версией
   - GitHub Actions автоматически создаст релиз

---

## 🐛 Troubleshooting

**Проблема:** CI тесты падают, но локально всё работает
- Проверьте версию Go
- Проверьте зависимости (`go mod tidy`)
- Убедитесь, что нет race conditions (`go test -race`)

**Проблема:** Линтер выдает ошибки
- Запустите `golangci-lint run` локально
- Проверьте `.golangci.yml` конфигурацию
- При необходимости добавьте исключения в `issues.exclude-rules`

**Проблема:** Security workflow находит уязвимости
- Обновите зависимости: `go get -u ./...`
- Проверьте конкретные пакеты с `govulncheck`
- Если это false positive, добавьте в исключения

---

## 📚 Дополнительная информация

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)
- [Go Vulnerability Database](https://vuln.go.dev/)
