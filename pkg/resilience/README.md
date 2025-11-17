# Resilience Package

Circuit Breaker pattern implementation for protecting against cascading failures in TDTP Framework.

## Features

- **Three States**: Closed, Half-Open, Open
- **Automatic Recovery**: Transitions to Half-Open after timeout
- **Concurrent Call Limiting**: Prevent resource exhaustion
- **Success Threshold**: Configurable recovery criteria
- **State Change Callbacks**: Monitor state transitions
- **Custom Trip Logic**: Flexible failure detection
- **Circuit Breaker Groups**: Manage multiple circuits
- **Zero Dependencies**: Uses only standard library

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/queuebridge/tdtp/pkg/resilience"
)

func main() {
    // Create circuit breaker
    config := resilience.DefaultConfig("database")
    cb, err := resilience.New(config)
    if err != nil {
        panic(err)
    }

    // Execute with protection
    err = cb.Execute(context.Background(), func(ctx context.Context) error {
        return connectToDatabase()
    })

    if err != nil {
        fmt.Printf("Failed: %v\n", err)
    }
}
```

### With Custom Configuration

```go
config := resilience.Config{
    Enabled:              true,
    Name:                 "api-service",
    MaxFailures:          5,                 // Open after 5 failures
    Timeout:              60 * time.Second,   // Wait 60s before Half-Open
    MaxConcurrentCalls:   100,                // Limit concurrent calls
    SuccessThreshold:     2,                  // 2 successes to close
}

cb, _ := resilience.New(config)
```

## States

Circuit Breaker has 3 states:

### Closed (Normal Operation)
- All requests are allowed through
- Failures are counted
- Opens when MaxFailures is reached

### Open (Fail Fast)
- All requests are rejected immediately
- Returns `ErrCircuitOpen`
- After Timeout, transitions to Half-Open

### Half-Open (Testing Recovery)
- Limited requests are allowed
- Success transitions to Closed
- Failure transitions back to Open

```
┌─────────┐                    ┌──────────┐
│ Closed  │───(MaxFailures)──→ │   Open   │
└─────────┘                    └──────────┘
     ↑                              │
     │                              │ (Timeout)
     │                              ↓
     │                         ┌──────────┐
     └───(SuccessThreshold)──  │Half-Open │
                               └──────────┘
```

## Configuration

### DefaultConfig

```go
config := resilience.DefaultConfig("my-service")
// MaxFailures: 5
// Timeout: 60s
// MaxConcurrentCalls: unlimited
// SuccessThreshold: 2
```

### AggressiveConfig (Fast Tripping)

```go
config := resilience.AggressiveConfig("api")
// MaxFailures: 3
// Timeout: 30s
// MaxConcurrentCalls: 100
// SuccessThreshold: 3
```

### ConservativeConfig (Slow Tripping)

```go
config := resilience.ConservativeConfig("background-job")
// MaxFailures: 10
// Timeout: 120s
// MaxConcurrentCalls: unlimited
// SuccessThreshold: 5
```

### WithCallbacks

```go
config := resilience.WithCallbacks("db", func(name string, from, to resilience.State) {
    log.Printf("Circuit %s: %v → %v", name, from, to)
})
```

## Advanced Usage

### With Fallback

```go
err := cb.ExecuteWithFallback(
    ctx,
    func(ctx context.Context) error {
        return callPrimaryService()
    },
    func(ctx context.Context) error {
        return callBackupService() // Fallback
    },
)
```

### Custom Trip Logic

```go
config := resilience.DefaultConfig("api")
config.ShouldTrip = func(counts resilience.Counts) bool {
    // Custom logic: trip if error rate > 50%
    if counts.Requests < 10 {
        return false // Need minimum requests
    }
    errorRate := float64(counts.TotalFailures) / float64(counts.Requests)
    return errorRate > 0.5
}
```

### State Monitoring

```go
stats := cb.Stats()
fmt.Printf("State: %v\n", stats.State)
fmt.Printf("Failures: %d/%d\n",
    stats.Counts.ConsecutiveFailures,
    config.MaxFailures)
fmt.Printf("Running calls: %d\n", stats.RunningCalls)
fmt.Printf("Time until Half-Open: %v\n", stats.TimeUntilHalfOpen)
```

### Manual Control

```go
// Check state
if cb.IsOpen() {
    log.Println("Circuit is open")
}

// Reset circuit
cb.Reset()

// Wait until ready
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := cb.WaitUntilReady(ctx)
if err != nil {
    log.Printf("Circuit not ready: %v", err)
}
```

## Circuit Breaker Groups

Manage multiple circuit breakers:

```go
group := resilience.NewGroup()

// Add circuits
dbConfig := resilience.DefaultConfig("database")
dbCircuit, _ := resilience.New(dbConfig)
group.Add("database", dbCircuit)

apiConfig := resilience.AggressiveConfig("api")
apiCircuit, _ := resilience.New(apiConfig)
group.Add("api", apiCircuit)

// Execute by name
err := group.Execute(ctx, "database", func(ctx context.Context) error {
    return queryDatabase()
})

// Get or create
circuit, _ := group.GetOrCreate("cache", resilience.DefaultConfig("cache"))

// Stats for all circuits
allStats := group.StatsAll()
for name, stats := range allStats {
    fmt.Printf("%s: %v\n", name, stats.State)
}

// Reset all
group.ResetAll()
```

## Real-World Examples

### Database Connection Pool

```go
type DatabaseService struct {
    pool    *sql.DB
    circuit *resilience.CircuitBreaker
}

func NewDatabaseService(dsn string) (*DatabaseService, error) {
    pool, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }

    config := resilience.DefaultConfig("postgres")
    config.MaxFailures = 5
    config.Timeout = 30 * time.Second
    config.OnStateChange = func(name string, from, to resilience.State) {
        log.Printf("DB Circuit: %v → %v", from, to)
    }

    circuit, err := resilience.New(config)
    if err != nil {
        return nil, err
    }

    return &DatabaseService{
        pool:    pool,
        circuit: circuit,
    }, nil
}

func (ds *DatabaseService) Query(ctx context.Context, query string) (*sql.Rows, error) {
    var rows *sql.Rows

    err := ds.circuit.Execute(ctx, func(ctx context.Context) error {
        var err error
        rows, err = ds.pool.QueryContext(ctx, query)
        return err
    })

    return rows, err
}
```

### HTTP Client with Retry

```go
import (
    "github.com/queuebridge/tdtp/pkg/resilience"
    "github.com/queuebridge/tdtp/pkg/retry"
)

type APIClient struct {
    client  *http.Client
    circuit *resilience.CircuitBreaker
    retryer *retry.Retryer
}

func NewAPIClient() (*APIClient, error) {
    // Circuit Breaker
    cbConfig := resilience.AggressiveConfig("api")
    circuit, _ := resilience.New(cbConfig)

    // Retry mechanism
    retryConfig := retry.EnableRetry(3, 1*time.Second)
    retryConfig.BackoffStrategy = retry.BackoffExponential
    retryConfig.BackoffMultiplier = 2.0
    retryer, _ := retry.NewRetryer(retryConfig)

    return &APIClient{
        client:  &http.Client{Timeout: 10 * time.Second},
        circuit: circuit,
        retryer: retryer,
    }, nil
}

func (ac *APIClient) Get(ctx context.Context, url string) (*http.Response, error) {
    var resp *http.Response

    // Circuit Breaker + Retry
    err := ac.circuit.Execute(ctx, func(ctx context.Context) error {
        return ac.retryer.Do(ctx, func(ctx context.Context) error {
            req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
            var err error
            resp, err = ac.client.Do(req)

            if err != nil {
                return err
            }

            if resp.StatusCode >= 500 {
                return fmt.Errorf("server error: %d", resp.StatusCode)
            }

            return nil
        })
    })

    return resp, err
}
```

### Message Queue Consumer

```go
type Consumer struct {
    kafka   *kafka.Reader
    circuit *resilience.CircuitBreaker
}

func (c *Consumer) ProcessMessages(ctx context.Context) {
    for {
        msg, err := c.kafka.ReadMessage(ctx)
        if err != nil {
            log.Printf("Read error: %v", err)
            continue
        }

        err = c.circuit.ExecuteWithFallback(
            ctx,
            func(ctx context.Context) error {
                return c.processMessage(msg)
            },
            func(ctx context.Context) error {
                // Fallback: save to DLQ
                return c.saveToDLQ(msg)
            },
        )

        if err != nil {
            log.Printf("Processing failed: %v", err)
        } else {
            c.kafka.CommitMessages(ctx, msg)
        }
    }
}
```

### Microservices Communication

```go
type ServiceRegistry struct {
    circuits map[string]*resilience.CircuitBreaker
}

func (sr *ServiceRegistry) Call(ctx context.Context, service string, fn func() error) error {
    circuit, ok := sr.circuits[service]
    if !ok {
        config := resilience.DefaultConfig(service)
        config.MaxFailures = 3
        config.Timeout = 30 * time.Second

        var err error
        circuit, err = resilience.New(config)
        if err != nil {
            return err
        }

        sr.circuits[service] = circuit
    }

    return circuit.Execute(ctx, func(ctx context.Context) error {
        return fn()
    })
}

// Usage
registry := &ServiceRegistry{circuits: make(map[string]*resilience.CircuitBreaker)}

err := registry.Call(ctx, "inventory-service", func() error {
    return callInventoryAPI()
})
```

## Error Handling

```go
err := cb.Execute(ctx, fn)

if errors.Is(err, resilience.ErrCircuitOpen) {
    // Circuit is open, use fallback or return cached data
    return cachedData, nil
}

if errors.Is(err, resilience.ErrTooManyCalls) {
    // Too many concurrent calls, rate limit
    return nil, errors.New("rate limit exceeded")
}

// Other errors from your function
return nil, err
```

## Metrics & Monitoring

### Prometheus Integration

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    circuitState = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "circuit_breaker_state",
            Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
        },
        []string{"name"},
    )

    circuitFailures = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "circuit_breaker_failures_total",
            Help: "Total circuit breaker failures",
        },
        []string{"name"},
    )
)

config := resilience.DefaultConfig("api")
config.OnStateChange = func(name string, from, to resilience.State) {
    circuitState.WithLabelValues(name).Set(float64(to))
}

// Periodically export stats
go func() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        stats := cb.Stats()
        circuitFailures.WithLabelValues(cb.Name()).Add(
            float64(stats.Counts.TotalFailures),
        )
    }
}()
```

## Best Practices

1. **Set Appropriate Thresholds**
   - MaxFailures: 3-10 for most cases
   - Timeout: 30-120 seconds
   - SuccessThreshold: 2-5

2. **Use Callbacks for Monitoring**
   - Log state changes
   - Send metrics
   - Alert on prolonged Open state

3. **Combine with Retry**
   - Circuit Breaker prevents cascading failures
   - Retry handles transient errors
   - Together provide complete resilience

4. **Limit Concurrent Calls**
   - Prevents resource exhaustion
   - Protects downstream services

5. **Use Circuit Groups**
   - One circuit per external dependency
   - Isolate failures
   - Independent recovery

6. **Test Your Circuits**
   - Simulate failures
   - Verify fallback behavior
   - Monitor recovery time

## Performance

- **Overhead**: ~50-100ns per call (negligible)
- **Memory**: ~200 bytes per circuit breaker
- **Concurrent**: Thread-safe with minimal lock contention

## Thread Safety

All operations are thread-safe:
- `Execute()` - can be called concurrently
- `State()` - safe to read from multiple goroutines
- State transitions use atomic operations

## Testing

```bash
go test ./pkg/resilience/... -v
```

All 13 tests cover:
- State transitions
- Timeout behavior
- Concurrent call limiting
- Callbacks
- Fallback execution
- Manual reset
- Circuit groups

## License

MIT License

## See Also

- [Retry Package](../retry/README.md) - Retry mechanism with exponential backoff
- [USE_CASES.md](../../USE_CASES.md) - Real-world integration scenarios
