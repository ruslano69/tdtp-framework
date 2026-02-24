package resultlog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ruslano69/tdtp-framework/pkg/etl"
)

// PipelineResult представляет состояние пайплайна, публикуемое в Redis
// после завершения выполнения (успешного или с ошибкой).
//
// Redis-ключи:
//
//	SET  tdtp:pipeline:<name>:state  <JSON>  EX <ttl>  — для GET-запросов оркестратора
//	PUB  tdtp:pipeline:<name>                          — для event-driven маршрутизации
type PipelineResult struct {
	PipelineName string    `json:"pipeline_name"`
	ResultName   string    `json:"result_name"`
	Status       string    `json:"status"`        // "success" | "failed"
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMs   int64     `json:"duration_ms"`
	RowsLoaded   int       `json:"rows_loaded"`
	RowsExported int       `json:"rows_exported"`
	Error        *string   `json:"error,omitempty"`
}

// RedisPublisher публикует результат выполнения пайплайна в Redis
type RedisPublisher struct {
	client *redis.Client
	config etl.ResultLogConfig
}

// NewRedisPublisher создает новый Redis publisher на основе конфигурации
func NewRedisPublisher(config etl.ResultLogConfig) *RedisPublisher {
	client := redis.NewClient(&redis.Options{
		Addr:     config.Address,
		Password: config.Password,
		DB:       config.DB,
	})
	return &RedisPublisher{client: client, config: config}
}

// Publish публикует результат выполнения пайплайна:
//   - SET tdtp:pipeline:<name>:state <JSON> EX <ttl>  → для опроса (polling)
//   - PUBLISH tdtp:pipeline:<name> <JSON>              → для подписки (pub/sub)
//
// Вызывается независимо от результата выполнения (success или failed).
// execErr == nil означает успешное выполнение.
func (p *RedisPublisher) Publish(ctx context.Context, pipelineName string, stats etl.ProcessorStats, execErr error) error {
	result := PipelineResult{
		PipelineName: pipelineName,
		ResultName:   p.config.Name,
		StartedAt:    stats.StartTime,
		FinishedAt:   stats.EndTime,
		DurationMs:   stats.Duration.Milliseconds(),
		RowsLoaded:   stats.TotalRowsLoaded,
		RowsExported: stats.TotalRowsExported,
	}

	if execErr != nil {
		result.Status = "failed"
		errStr := execErr.Error()
		result.Error = &errStr
	} else {
		result.Status = "success"
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	stateKey := fmt.Sprintf("tdtp:pipeline:%s:state", p.config.Name)
	eventChannel := fmt.Sprintf("tdtp:pipeline:%s", p.config.Name)
	ttl := time.Duration(p.config.TTL) * time.Second

	// SET ключ с TTL — оркестратор может GET для получения последнего состояния
	if err := p.client.Set(ctx, stateKey, payload, ttl).Err(); err != nil {
		return fmt.Errorf("redis SET failed: %w", err)
	}

	// PUBLISH событие — оркестратор может SUBSCRIBE для event-driven маршрутизации
	if err := p.client.Publish(ctx, eventChannel, payload).Err(); err != nil {
		return fmt.Errorf("redis PUBLISH failed: %w", err)
	}

	return nil
}

// Close закрывает соединение с Redis
func (p *RedisPublisher) Close() error {
	return p.client.Close()
}
