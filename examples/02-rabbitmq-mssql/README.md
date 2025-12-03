# Advanced Data Pipeline Example

## ❌ Что было не так в старом примере:

Старый пример **не компилировался** и использовал mock данные.

## ✅ Что изменилось:

Полностью переработан в **рабочий production-ready пример** с:
- Реальными адаптерами через `adapters.New()`
- Data processing pipeline (masking, validation)
- Circuit breaker для resilience
- Полным audit trail

## Что демонстрирует:

1. **Database → TDTP Export**
2. **PII Data Masking** (GDPR compliance)
3. **Circuit Breaker Pattern**
4. **TDTP → Database Import**

## Запуск:

```bash
go run .
```

Создаст `orders.db` и `analytics.db` с замаскированными данными.
