package resultlog

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ruslano69/tdtp-framework/pkg/etl"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
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
	Status       string    `json:"status"`        // "success" | "failed" | "completed_with_errors"
	PackageUUID  string    `json:"package_uuid,omitempty"` // UUID зашифрованного пакета (если encryption: true)
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

// PublishOptions содержит дополнительные параметры публикации.
type PublishOptions struct {
	PackageUUID string // UUID зашифрованного пакета (если encryption: true)
}

// Publish публикует результат выполнения пайплайна:
//   - SET tdtp:pipeline:<name>:state <JSON> EX <ttl>  → для опроса (polling)
//   - PUBLISH tdtp:pipeline:<name> <JSON>              → для подписки (pub/sub)
//
// Graceful degradation: если Redis недоступен — логирует предупреждение и возвращает nil.
// Pipeline не должен падать из-за недоступности resultlog.
// execErr == nil означает успешное выполнение.
func (p *RedisPublisher) Publish(ctx context.Context, pipelineName string, stats etl.ProcessorStats, execErr error, opts ...PublishOptions) error {
	result := PipelineResult{
		PipelineName: pipelineName,
		ResultName:   p.config.Name,
		StartedAt:    stats.StartTime,
		FinishedAt:   stats.EndTime,
		DurationMs:   stats.Duration.Milliseconds(),
		RowsLoaded:   stats.TotalRowsLoaded,
		RowsExported: stats.TotalRowsExported,
	}

	if len(opts) > 0 && opts[0].PackageUUID != "" {
		result.PackageUUID = opts[0].PackageUUID
	}

	switch {
	case execErr == nil:
		result.Status = string(mercury.StatusSuccess)
	case isCompletedWithErrors(execErr):
		// Pipeline завершился, но с управляемой ошибкой (например, xZMercury недоступен)
		result.Status = string(mercury.StatusCompletedWithErrors)
		errStr := execErr.Error()
		result.Error = &errStr
	default:
		result.Status = string(mercury.StatusFailed)
		errStr := execErr.Error()
		result.Error = &errStr
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
		// Graceful degradation: Redis недоступен — логируем, не падаем
		log.Printf("[resultlog] WARNING: redis SET failed (degraded mode): %v", err)
		return nil
	}

	// PUBLISH событие — оркестратор может SUBSCRIBE для event-driven маршрутизации
	if err := p.client.Publish(ctx, eventChannel, payload).Err(); err != nil {
		log.Printf("[resultlog] WARNING: redis PUBLISH failed (degraded mode): %v", err)
		return nil
	}

	return nil
}

// isCompletedWithErrors определяет, является ли ошибка управляемой (pipeline завершён с error-пакетом).
// Это позволяет отличить "pipeline упал" от "pipeline завершился, но записал error-пакет".
func isCompletedWithErrors(err error) bool {
	if err == nil {
		return false
	}
	// Error-пакет записан успешно (xZMercury недоступен, но pipeline не упал)
	// Идентифицируем по sentinel errors из mercury пакета
	for _, code := range []string{
		mercury.ErrCodeMercuryUnavailable,
		mercury.ErrCodeMercuryError,
		mercury.ErrCodeHMACVerificationFailed,
		mercury.ErrCodeKeyBindRejected,
	} {
		if containsString(err.Error(), code) {
			return true
		}
	}
	return false
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Close закрывает соединение с Redis
func (p *RedisPublisher) Close() error {
	return p.client.Close()
}
