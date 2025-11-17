# Data Processors для TDTP Framework

Модуль процессоров обеспечивает трансформацию данных "на лету" при экспорте и импорте.

## 🎯 Назначение

**Pre-processors** (выполняются при экспорте):
- 🔒 Маскирование чувствительных данных
- 🎭 Анонимизация персональной информации
- 📏 Нормализация форматов
- ✅ Валидация данных

**Post-processors** (выполняются при импорте):
- 🔍 Обогащение данных
- 🔄 Трансформация под целевую систему
- 🎲 Присвоение значений по умолчанию
- 📊 Бизнес-логика на принимающей стороне

## 📦 Архитектура

```
┌─────────────────────────────────────────────────────┐
│  Экспорт (Export Flow)                              │
│  ─────────────────────                              │
│  БД → [Pre-processors] → TDTP пакет → Брокер        │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Импорт (Import Flow)                               │
│  ─────────────────────                              │
│  Брокер → TDTP пакет → [Post-processors] → БД       │
└─────────────────────────────────────────────────────┘
```

## 🔧 Встроенные процессоры

### 1. FieldMasker - Маскирование данных

Скрывает чувствительную информацию для защиты PII.

**Типы маскирования:**
- `partial` - маскирует середину (email: j***@example.com)
- `middle` - маскирует среднюю часть (phone: +1 (555) XXX-X567)
- `stars` - заменяет всё на звездочки (**** *****)
- `first2_last2` - показывает только первые 2 и последние 2 символа

**Примеры:**
```yaml
processors:
  pre_export:
    - type: field_masker
      params:
        fields:
          email: partial           # john@example.com → j***@example.com
          phone: middle            # +1 (555) 123-4567 → +1 (555) XXX-X567
          passport: first2_last2   # 1234 567890 → 12** ****90
          password: stars          # MyPass123 → *********
```

**Use cases:**
- Выгрузка данных для тестовых окружений
- Соответствие GDPR / 152-ФЗ
- Безопасный обмен данными с подрядчиками

### 2. FieldNormalizer - Нормализация данных

Приводит данные к единому формату.

**Правила нормализации:**
- `phone` - приводит к международному формату (только цифры)
- `email` - нижний регистр, без пробелов
- `whitespace` - убирает лишние пробелы
- `uppercase` - верхний регистр
- `lowercase` - нижний регистр
- `date` - формат YYYY-MM-DD

**Примеры:**
```yaml
processors:
  pre_export:
    - type: field_normalizer
      params:
        fields:
          phone: phone             # +1 (555) 123-4567 → 15551234567
          email: email             # John@EXAMPLE.com → john@example.com
          name: whitespace         # "John   Doe" → "John Doe"
          country_code: uppercase  # "ru" → "RU"
          created_at: date         # 01.12.2024 → 2024-12-01
```

**Use cases:**
- Очистка данных перед интеграцией
- Приведение к стандартам целевой системы
- Исправление ошибок ввода пользователей

## 🚀 Использование

### В конфигурации (config.yaml)

```yaml
database:
  # ... конфигурация БД ...

# Конфигурация процессоров
processors:
  # Процессоры для экспорта
  pre_export:
    - type: field_masker
      params:
        fields:
          email: partial
          phone: middle
          passport_series: stars

    - type: field_normalizer
      params:
        fields:
          phone: phone
          email: email

  # Процессоры для импорта
  post_import:
    - type: field_normalizer
      params:
        fields:
          created_at: date
          status: lowercase
```

### В коде

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/processors"
    "github.com/queuebridge/tdtp/pkg/core/packet"
)

func main() {
    ctx := context.Background()

    // Создание процессоров через фабрику
    factory := processors.NewFactory()

    config := processors.Config{
        Type: "field_masker",
        Params: map[string]interface{}{
            "fields": map[string]interface{}{
                "email": "partial",
                "phone": "middle",
            },
        },
    }

    masker, err := factory.Create(config)
    if err != nil {
        panic(err)
    }

    // Применение процессора
    data := [][]string{
        {"john.doe@example.com", "+1 (555) 123-4567"},
        {"jane.smith@test.com", "+1 (555) 987-6543"},
    }

    schema := packet.Schema{
        Fields: []packet.Field{
            {Name: "email", Type: "TEXT"},
            {Name: "phone", Type: "TEXT"},
        },
    }

    processed, err := masker.Process(ctx, data, schema)
    if err != nil {
        panic(err)
    }

    // processed:
    // [
    //   ["j***@example.com", "+1 (555) XXX-X567"],
    //   ["j***@test.com", "+1 (555) XXX-X543"]
    // ]
}
```

### Цепочка процессоров

```go
// Создание цепочки
chain := processors.NewChain()

// Добавление процессоров
masker := processors.NewFieldMasker(map[string]processors.MaskPattern{
    "email": processors.MaskPartial,
})
normalizer := processors.NewFieldNormalizer(map[string]processors.NormalizeRule{
    "phone": processors.NormalizePhone,
})

chain.Add(masker)
chain.Add(normalizer)

// Применение всей цепочки
processed, err := chain.Process(ctx, data, schema)
```

### Создание цепочки из конфигурации

```go
configs := []processors.Config{
    {
        Type: "field_masker",
        Params: map[string]interface{}{
            "fields": map[string]interface{}{
                "email": "partial",
            },
        },
    },
    {
        Type: "field_normalizer",
        Params: map[string]interface{}{
            "fields": map[string]interface{}{
                "phone": "phone",
            },
        },
    },
}

chain, err := processors.CreateChainFromConfigs(configs)
if err != nil {
    panic(err)
}

processed, err := chain.Process(ctx, data, schema)
```

## 🔌 Создание собственного процессора

```go
package myprocessors

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/core/packet"
    "github.com/queuebridge/tdtp/pkg/processors"
)

// MyCustomProcessor - пример кастомного процессора
type MyCustomProcessor struct {
    name string
    // ... параметры
}

func (p *MyCustomProcessor) Name() string {
    return p.name
}

func (p *MyCustomProcessor) Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error) {
    // Ваша логика обработки
    result := make([][]string, len(data))
    for i, row := range data {
        newRow := make([]string, len(row))
        copy(newRow, row)

        // Трансформация данных
        // ...

        result[i] = newRow
    }
    return result, nil
}

// Регистрация в фабрике
func init() {
    processors.DefaultFactory.Register("my_custom", func(params map[string]interface{}) (processors.Processor, error) {
        return &MyCustomProcessor{
            name: "my_custom",
            // ... парсинг параметров
        }, nil
    })
}
```

## 📊 Примеры use cases

### 1. Безопасный экспорт для тестирования

```yaml
processors:
  pre_export:
    - type: field_masker
      params:
        fields:
          email: partial
          phone: middle
          passport: stars
          card_number: stars
          ssn: stars
```

### 2. Интеграция с внешней системой

```yaml
processors:
  pre_export:
    - type: field_normalizer
      params:
        fields:
          phone: phone              # Международный формат (только цифры)
          email: email              # Lowercase
          country_code: uppercase   # ISO 3166-1 alpha-2
          created_at: date          # YYYY-MM-DD

  post_import:
    - type: field_normalizer
      params:
        fields:
          status: lowercase         # Приведение к enum
          priority: uppercase       # HIGH, MEDIUM, LOW
```

### 3. Очистка данных перед импортом

```yaml
processors:
  post_import:
    - type: field_normalizer
      params:
        fields:
          name: whitespace          # Убрать лишние пробелы
          email: email              # Lowercase + trim
          phone: phone              # Единый формат
```

## ⚙️ Производительность

- Процессоры работают **in-memory** - быстро и эффективно
- Регулярные выражения **предкомпилированы** при создании процессора
- Обработка данных **построчная** - не требует загрузки всей таблицы в память
- Цепочки процессоров выполняются **последовательно** - предсказуемый результат

## 🔐 Безопасность

- Маскирование данных происходит **до отправки** через сеть
- Оригинальные данные **не модифицируются** в исходной БД
- Процессоры не имеют **доступа к сети** - только к данным в памяти
- Конфигурация процессоров **хранится в config файле** - под контролем версий

## 📝 Roadmap

Планируемые процессоры:
- [ ] **field_validator** - валидация данных (regex, ranges, enums)
- [ ] **field_enricher** - обогащение данных из внешних источников
- [ ] **field_transformer** - математические/строковые трансформации
- [ ] **field_anonymizer** - замена на псевдонимы с сохранением ссылочной целостности
- [ ] **field_encryptor** - шифрование/дешифрование полей
- [ ] **conditional_processor** - условная обработка на основе значений других полей

## 🤝 Вклад

Процессоры легко расширяемы! Создавайте свои процессоры и делитесь ими с сообществом.

**Требования к процессору:**
1. Реализует интерфейс `Processor`
2. Не имеет побочных эффектов (pure function)
3. Покрыт unit-тестами
4. Документирован с примерами
