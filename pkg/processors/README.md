# Data processors

Processors transform data in flight, on export and on import.

## Purpose

**Pre-processors** run on export:
- masking sensitive data
- anonymising personal information
- normalising formats
- validating data

**Post-processors** run on import:
- enriching data
- reshaping it for the target system
- filling in defaults
- business logic on the receiving side

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  Export flow                                        │
│  ─────────────────────                              │
│  DB → [pre-processors] → TDTP packet → broker       │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Import flow                                        │
│  ─────────────────────                              │
│  Broker → TDTP packet → [post-processors] → DB      │
└─────────────────────────────────────────────────────┘
```

## The built-in processors

### 1. FieldMasker — masking

Hides sensitive information, to protect PII.

**Masking modes:**
- `partial` — masks the middle (email: j***@example.com)
- `middle` — masks the central part (phone: +1 (555) XXX-X567)
- `stars` — replaces everything with asterisks (**** *****)
- `first2_last2` — shows only the first two and last two characters

**Examples:**
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
- exporting data for a test environment
- GDPR compliance, and the Russian 152-FZ equivalent
- sharing data safely with a contractor

### 2. FieldNormalizer — normalisation

Brings data to one consistent format.

**Normalisation rules:**
- `phone` — international format, digits only
- `email` — lower-cased, spaces stripped
- `whitespace` — collapses redundant spaces
- `uppercase`
- `lowercase`
- `date` — YYYY-MM-DD

**Examples:**
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
- cleaning data before an integration
- conforming to the target system's conventions
- fixing user-entry mistakes

### 3. FieldValidator — validation

Checks data quality before an export or an import.

**Validation rules:**
- `regex:pattern` — match a regular expression
- `range:min-max` — a numeric range, for example `range:0-150`
- `enum:val1,val2,...` — one of a list of permitted values
- `required` — the field must not be empty
- `length:min-max` — string length, for example `length:3-50`
- `email` — a valid email address
- `phone` — a valid telephone number
- `url` — a valid URL
- `date` — a valid date, YYYY-MM-DD

**Examples:**
```yaml
processors:
  pre_export:
    - type: field_validator
      params:
        stop_on_first_error: false  # false = collect every error; true = stop at the first
        rules:
          email: email                         # john@example.com ✓
          age: range:18-65                     # 25 ✓, 17 ✗
          status: enum:active,inactive,pending # active ✓, deleted ✗
          username: length:3-20                # john_doe ✓, ab ✗
          phone: phone                         # +1 (555) 123-4567 ✓
          website: url                         # https://example.com ✓
          birth_date: date                     # 1990-05-15 ✓

  post_import:
    - type: field_validator
      params:
        stop_on_first_error: true  # stop at the first error
        rules:
          name:
            - required              # must be present
            - length:2-100          # between 2 and 100 characters
          price:
            - required
            - range:0-1000000       # a price from 0 to a million
```

**Custom regexes and error messages:**
```yaml
processors:
  pre_export:
    - type: field_validator
      params:
        rules:
          sku: regex:^[A-Z]{3}-\d{5}$          # SKU format: ABC-12345
          postal_code: regex:^\d{5}(-\d{4})?$  # US ZIP code
```

**Use cases:**
- checking data quality before an export
- stopping invalid data from being imported
- enforcing business rules
- catching data problems early

## Usage

### In the configuration (config.yaml)

```yaml
database:
  # ... database configuration ...

# Processor configuration
processors:
  # Processors for export
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

  # Processors for import
  post_import:
    - type: field_normalizer
      params:
        fields:
          created_at: date
          status: lowercase
```

### In code

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/processors"
    "github.com/queuebridge/tdtp/pkg/core/packet"
)

func main() {
    ctx := context.Background()

    // Build the processors through the factory
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

    // Apply one
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

### A chain of processors

```go
// Create the chain
chain := processors.NewChain()

// Add processors
masker := processors.NewFieldMasker(map[string]processors.MaskPattern{
    "email": processors.MaskPartial,
})
normalizer := processors.NewFieldNormalizer(map[string]processors.NormalizeRule{
    "phone": processors.NormalizePhone,
})

chain.Add(masker)
chain.Add(normalizer)

// Apply the whole chain
processed, err := chain.Process(ctx, data, schema)
```

### Building a chain from configuration

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

## Writing your own processor

```go
package myprocessors

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/core/packet"
    "github.com/queuebridge/tdtp/pkg/processors"
)

// MyCustomProcessor is an example
type MyCustomProcessor struct {
    name string
    // ... parameters
}

func (p *MyCustomProcessor) Name() string {
    return p.name
}

func (p *MyCustomProcessor) Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error) {
    // Your processing logic
    result := make([][]string, len(data))
    for i, row := range data {
        newRow := make([]string, len(row))
        copy(newRow, row)

        // Transform the value
        // ...

        result[i] = newRow
    }
    return result, nil
}

// Register it with the factory
func init() {
    processors.DefaultFactory.Register("my_custom", func(params map[string]interface{}) (processors.Processor, error) {
        return &MyCustomProcessor{
            name: "my_custom",
            // ... parse the parameters
        }, nil
    })
}
```

## Worked use cases

### 1. A safe export for testing

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

### 2. Integrating with an external system

```yaml
processors:
  pre_export:
    - type: field_normalizer
      params:
        fields:
          phone: phone              # international format, digits only
          email: email              # Lowercase
          country_code: uppercase   # ISO 3166-1 alpha-2
          created_at: date          # YYYY-MM-DD

  post_import:
    - type: field_normalizer
      params:
        fields:
          status: lowercase         # to match the target's enum
          priority: uppercase       # HIGH, MEDIUM, LOW
```

### 3. Cleaning data before an import

```yaml
processors:
  post_import:
    - type: field_normalizer
      params:
        fields:
          name: whitespace          # collapse redundant spaces
          email: email              # Lowercase + trim
          phone: phone              # one consistent format
```

## Performance

- processors work **in memory** — fast, and cheap
- regular expressions are **compiled once**, when the processor is created
- data is handled **row by row** — the whole table never needs to be in memory
- chains run **in order**, so the result is predictable

## Security

- masking happens **before** anything crosses the network
- the original data in the source database is **never modified**
- processors have **no network access** — only the data in memory
- processor configuration **lives in the config file**, under version control

## 📝 Roadmap

Planned processors:
- [ ] **field_validator** — validation (regex, ranges, enums)
- [ ] **field_enricher** — enrichment from an external source
- [ ] **field_transformer** — arithmetic and string transformations
- [ ] **field_anonymizer** — pseudonyms that preserve referential integrity
- [ ] **field_encryptor** — encrypting and decrypting individual fields
- [ ] **conditional_processor** — behaviour conditional on other fields' values

## Contributing

Processors are easy to extend. Write your own and share them.

**What a processor must do:**
1. implement the `Processor` interface
2. have no side effects — a pure function
3. come with unit tests
4. be documented, with examples
