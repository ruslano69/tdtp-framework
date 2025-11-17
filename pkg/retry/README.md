# Retry Package

A comprehensive retry mechanism with exponential backoff, Dead Letter Queue (DLQ), and configurable strategies for handling transient failures in TDTP Framework.

## Features

- **Multiple Backoff Strategies**: Constant, Linear, Exponential
- **Jitter Support**: Prevent thundering herd problem
- **Dead Letter Queue (DLQ)**: Store failed messages for later analysis or reprocessing
- **Context Awareness**: Properly handles context cancellation
- **Configurable Retryable Errors**: Retry only specific error types
- **Callback Support**: Hook into retry attempts for logging/monitoring
- **Zero Dependencies**: Uses only standard library

## Installation

```bash
go get github.com/queuebridge/tdtp/pkg/retry
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "time"
    "github.com/queuebridge/tdtp/pkg/retry"
)

func main() {
    // Create a simple retry configuration
    config := retry.EnableRetry(3, 100*time.Millisecond)
    retryer, err := retry.NewRetryer(config)
    if err != nil {
        panic(err)
    }

    // Retry a function
    err = retryer.Do(context.Background(), func(ctx context.Context) error {
        // Your code here that might fail
        return doSomethingThatMightFail()
    })
}
```

### With Exponential Backoff

```go
config := retry.EnableRetry(5, 100*time.Millisecond)
config.BackoffStrategy = retry.BackoffExponential
config.BackoffMultiplier = 2.0  // Each retry waits 2x longer
config.MaxDelay = 10 * time.Second
config.Jitter = 0.1  // Add ±10% randomness

retryer, _ := retry.NewRetryer(config)
```

### With Dead Letter Queue (DLQ)

```go
// Enable retry with DLQ for storing failed messages
config := retry.EnableRetryWithDLQ(3, 100*time.Millisecond, "failed_messages.json")
retryer, _ := retry.NewRetryer(config)
defer retryer.Close()

// Execute with data that will be saved to DLQ on failure
testData := map[string]interface{}{
    "order_id": "12345",
    "customer": "John Doe",
}

err := retryer.DoWithData(context.Background(), func(ctx context.Context) error {
    return processOrder(testData)
}, testData)

// Check DLQ entries
dlq := retryer.GetDLQ()
entries := dlq.Get()
for _, entry := range entries {
    fmt.Printf("Failed: %s, Attempts: %d\n", entry.LastError, entry.Attempts)
}
```

## Configuration

### RetryConfig

```go
type Config struct {
    // Enabled - включить retry механизм
    Enabled bool

    // MaxAttempts - максимальное количество попыток (0 = бесконечно)
    MaxAttempts int

    // InitialDelay - начальная задержка между попытками
    InitialDelay time.Duration

    // MaxDelay - максимальная задержка между попытками
    MaxDelay time.Duration

    // BackoffStrategy - стратегия увеличения задержки
    // Варианты: BackoffConstant, BackoffLinear, BackoffExponential
    BackoffStrategy BackoffStrategy

    // BackoffMultiplier - множитель для exponential backoff
    BackoffMultiplier float64

    // Jitter - случайность для предотвращения thundering herd (0.0-1.0)
    Jitter float64

    // RetryableErrors - список ошибок, для которых делать retry
    // Если пустой, retry для всех ошибок
    RetryableErrors []string

    // OnRetry - callback функция перед каждой попыткой retry
    OnRetry func(attempt int, err error, delay time.Duration)

    // DLQ - конфигурация Dead Letter Queue
    DLQ DLQConfig
}
```

### Backoff Strategies

#### Constant Backoff
Always wait the same amount of time between retries:
```go
config := retry.EnableRetry(5, 1*time.Second)
config.BackoffStrategy = retry.BackoffConstant
// Delays: 1s, 1s, 1s, 1s
```

#### Linear Backoff
Increase delay linearly:
```go
config := retry.EnableRetry(5, 1*time.Second)
config.BackoffStrategy = retry.BackoffLinear
// Delays: 1s, 2s, 3s, 4s
```

#### Exponential Backoff
Increase delay exponentially (recommended for most use cases):
```go
config := retry.EnableRetry(5, 100*time.Millisecond)
config.BackoffStrategy = retry.BackoffExponential
config.BackoffMultiplier = 2.0
// Delays: 100ms, 200ms, 400ms, 800ms
```

### Jitter

Jitter adds randomness to prevent multiple clients from retrying simultaneously:

```go
config.Jitter = 0.2  // Add ±20% randomness to delays
```

Example with 100ms delay and 0.2 jitter:
- Actual delays: 80-120ms (randomly distributed)

### Retryable Errors

Specify which errors should trigger retries:

```go
config := retry.EnableRetry(3, 100*time.Millisecond)
config.RetryableErrors = []string{
    "timeout",
    "connection refused",
    "temporary failure",
}

// Now only errors containing these strings will be retried
// Other errors will fail immediately
```

### Retry Callbacks

Monitor retry attempts with callbacks:

```go
config := retry.EnableRetry(3, 100*time.Millisecond)
config.OnRetry = func(attempt int, err error, delay time.Duration) {
    log.Printf("Retry attempt %d after %v: %v", attempt, delay, err)
}
```

## Dead Letter Queue (DLQ)

DLQ stores messages that failed after maximum retry attempts for later analysis or reprocessing.

### DLQ Configuration

```go
type DLQConfig struct {
    // Enabled - включить DLQ
    Enabled bool

    // FilePath - путь к файлу для хранения DLQ
    FilePath string

    // MaxSize - максимальное количество записей (0 = без ограничений)
    // При превышении удаляются самые старые
    MaxSize int

    // RetentionPeriod - период хранения записей (0 = вечно)
    RetentionPeriod time.Duration
}
```

### Working with DLQ

```go
dlq := retryer.GetDLQ()

// Get all entries
entries := dlq.Get()

// Get specific entry
entry := dlq.GetByID("dlq-1234567890-1")

// Get statistics
stats := dlq.GetStats()
fmt.Printf("Total entries: %d\n", stats.TotalEntries)
fmt.Printf("Failure types: %v\n", stats.FailureTypes)

// Remove processed entry
dlq.Remove("dlq-1234567890-1")

// Cleanup old entries
removed := dlq.CleanupOld()
fmt.Printf("Removed %d old entries\n", removed)

// Clear all entries
dlq.Clear()

// Manual save
dlq.Save()
```

### DLQ Entry Structure

```go
type DLQEntry struct {
    ID          string      // Unique identifier
    Timestamp   time.Time   // When the failure occurred
    Attempts    int         // Number of retry attempts
    LastError   string      // Last error message
    FailureType string      // Type of failure (max_attempts_exceeded, etc.)
    Data        interface{} // Original data (only with DoWithData)
}
```

## Real-World Examples

### Database Connection with Retry

```go
func connectToDatabase() error {
    config := retry.EnableRetry(5, 1*time.Second)
    config.BackoffStrategy = retry.BackoffExponential
    config.BackoffMultiplier = 2.0
    config.MaxDelay = 30 * time.Second
    config.Jitter = 0.1
    config.RetryableErrors = []string{
        "connection refused",
        "timeout",
        "no such host",
    }
    config.OnRetry = func(attempt int, err error, delay time.Duration) {
        log.Printf("DB connection attempt %d failed: %v, retrying in %v",
            attempt, err, delay)
    }

    retryer, _ := retry.NewRetryer(config)

    return retryer.Do(context.Background(), func(ctx context.Context) error {
        return db.Connect()
    })
}
```

### HTTP Request with Context Timeout

```go
func fetchDataWithRetry(url string) ([]byte, error) {
    config := retry.EnableRetry(3, 500*time.Millisecond)
    config.BackoffStrategy = retry.BackoffExponential
    config.BackoffMultiplier = 2.0
    config.RetryableErrors = []string{"timeout", "503", "502", "500"}

    retryer, _ := retry.NewRetryer(config)

    // Context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    var data []byte
    err := retryer.Do(ctx, func(ctx context.Context) error {
        resp, err := http.Get(url)
        if err != nil {
            return err
        }
        defer resp.Body.Close()

        if resp.StatusCode >= 500 {
            return fmt.Errorf("server error: %d", resp.StatusCode)
        }

        data, err = io.ReadAll(resp.Body)
        return err
    })

    return data, err
}
```

### Message Queue Consumer with DLQ

```go
func processMessages(broker *kafka.Kafka) {
    config := retry.EnableRetryWithDLQ(3, 1*time.Second, "failed_messages.json")
    config.BackoffStrategy = retry.BackoffExponential
    config.BackoffMultiplier = 1.5
    config.DLQ.MaxSize = 1000
    config.DLQ.RetentionPeriod = 24 * time.Hour

    retryer, _ := retry.NewRetryer(config)
    defer retryer.Close()

    for {
        msg, err := broker.Consume(context.Background())
        if err != nil {
            log.Printf("Consumer error: %v", err)
            continue
        }

        // Process with retry and DLQ
        err = retryer.DoWithData(context.Background(), func(ctx context.Context) error {
            return processMessage(msg)
        }, map[string]interface{}{
            "topic":     msg.Topic,
            "partition": msg.Partition,
            "offset":    msg.Offset,
            "value":     string(msg.Value),
        })

        if err != nil {
            log.Printf("Message processing failed after retries: %v", err)
            // Message is now in DLQ for manual inspection
        } else {
            broker.CommitLast(context.Background())
        }
    }
}
```

### Data Import with Batch Retry

```go
func importRecords(adapter adapters.Adapter, packets []*packet.DataPacket) error {
    config := retry.EnableRetryWithDLQ(5, 2*time.Second, "import_failures.json")
    config.BackoffStrategy = retry.BackoffExponential
    config.BackoffMultiplier = 2.0
    config.MaxDelay = 1 * time.Minute

    retryer, _ := retry.NewRetryer(config)
    defer retryer.Close()

    for _, pkt := range packets {
        err := retryer.DoWithData(context.Background(), func(ctx context.Context) error {
            return adapter.ImportPacket(ctx, pkt)
        }, map[string]interface{}{
            "table":   pkt.TableName,
            "records": len(pkt.Rows),
        })

        if err != nil {
            log.Printf("Failed to import packet for table %s: %v", pkt.TableName, err)
        }
    }

    // Check DLQ for failed imports
    dlq := retryer.GetDLQ()
    if dlq.Size() > 0 {
        log.Printf("WARNING: %d packets failed to import, check DLQ", dlq.Size())
        stats := dlq.GetStats()
        log.Printf("Failure types: %v", stats.FailureTypes)
    }

    return nil
}
```

## Performance Considerations

### Delay Calculations

| Strategy | Attempts | Delays (InitialDelay=100ms, Multiplier=2.0) |
|----------|----------|---------------------------------------------|
| Constant | 5 | 100ms, 100ms, 100ms, 100ms |
| Linear | 5 | 100ms, 200ms, 300ms, 400ms |
| Exponential | 5 | 100ms, 200ms, 400ms, 800ms |

### Memory Usage

- **Retryer**: ~200 bytes per instance
- **DLQ Entry**: ~150 bytes + size of Data field
- **DLQ with 1000 entries**: ~150-500 KB (depending on data size)

### Recommendations

1. **Use Exponential Backoff** for most scenarios to quickly back off from overloaded services
2. **Add Jitter (0.1-0.2)** when multiple clients retry simultaneously
3. **Set MaxDelay** to prevent excessive waiting times
4. **Limit DLQ size** to prevent unbounded memory growth
5. **Use RetryableErrors** to fail fast on non-transient errors
6. **Set context timeouts** to prevent infinite retries

## Testing

Run tests:
```bash
go test ./pkg/retry/... -v
```

Run with coverage:
```bash
go test ./pkg/retry/... -cover
```

## Thread Safety

All components are thread-safe and can be used concurrently:
- `Retryer.Do()` and `Retryer.DoWithData()` are safe to call from multiple goroutines
- `DLQ` operations use `sync.RWMutex` for concurrent access
- `StateManager` is thread-safe for concurrent reads and writes

## Error Handling

The retry package wraps errors with context:
```go
err := retryer.Do(ctx, fn)
// Possible errors:
// - "non-retryable error: <original error>"
// - "max retry attempts (N) exceeded: <last error>"
// - "context cancelled: <context error>"
```

## Best Practices

1. **Always use context** to allow cancellation and timeouts
2. **Set appropriate MaxAttempts** (typically 3-5 for most scenarios)
3. **Use DLQ for critical operations** where data loss is unacceptable
4. **Monitor DLQ size** and set up alerts for failures
5. **Implement idempotency** in your retry functions
6. **Log retry attempts** using OnRetry callback for debugging
7. **Set RetentionPeriod** for DLQ to prevent disk space issues
8. **Clean up DLQ entries** after manual intervention

## Integration with TDTP Adapters

The retry package integrates seamlessly with TDTP adapters:

```go
import (
    "github.com/queuebridge/tdtp/pkg/adapters/postgres"
    "github.com/queuebridge/tdtp/pkg/retry"
)

func importWithRetry(adapter *postgres.Adapter, pkt *packet.DataPacket) error {
    config := retry.EnableRetry(3, 1*time.Second)
    retryer, _ := retry.NewRetryer(config)

    return retryer.Do(context.Background(), func(ctx context.Context) error {
        return adapter.ImportPacket(ctx, pkt)
    })
}
```

## License

MIT License - see LICENSE file for details
