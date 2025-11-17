package retry

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestRetryer_Success(t *testing.T) {
	config := EnableRetry(3, 100*time.Millisecond)
	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		return nil // Success on first attempt
	}

	err = retryer.Do(context.Background(), fn)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestRetryer_SuccessAfterRetries(t *testing.T) {
	config := EnableRetry(5, 10*time.Millisecond)
	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil // Success on 3rd attempt
	}

	start := time.Now()
	err = retryer.Do(context.Background(), fn)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	// Проверяем что были задержки
	if duration < 20*time.Millisecond {
		t.Errorf("Expected delays between retries, duration was too short: %v", duration)
	}
}

func TestRetryer_MaxAttemptsExceeded(t *testing.T) {
	config := EnableRetry(3, 10*time.Millisecond)
	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		return errors.New("persistent error")
	}

	err = retryer.Do(context.Background(), fn)
	if err == nil {
		t.Error("Expected error after max attempts")
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetryer_ExponentialBackoff(t *testing.T) {
	config := EnableRetry(4, 100*time.Millisecond)
	config.BackoffStrategy = BackoffExponential
	config.BackoffMultiplier = 2.0
	config.Jitter = 0 // Отключаем jitter для предсказуемости

	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	delays := []time.Duration{}
	attempts := 0
	lastAttempt := time.Now()

	fn := func(ctx context.Context) error {
		attempts++
		if attempts > 1 {
			delays = append(delays, time.Since(lastAttempt))
		}
		lastAttempt = time.Now()
		return errors.New("error")
	}

	retryer.Do(context.Background(), fn)

	// Проверяем что задержки увеличиваются экспоненциально
	// Ожидаем: 100ms, 200ms, 400ms
	if len(delays) < 2 {
		t.Fatalf("Expected at least 2 delays, got %d", len(delays))
	}

	// Проверяем что вторая задержка примерно в 2 раза больше первой
	ratio := float64(delays[1]) / float64(delays[0])
	if ratio < 1.8 || ratio > 2.2 {
		t.Errorf("Expected exponential backoff ratio ~2.0, got %.2f (delays: %v, %v)", ratio, delays[0], delays[1])
	}
}

func TestRetryer_ConstantBackoff(t *testing.T) {
	config := EnableRetry(3, 50*time.Millisecond)
	config.BackoffStrategy = BackoffConstant
	config.Jitter = 0

	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	delays := []time.Duration{}
	attempts := 0
	var lastTime time.Time

	fn := func(ctx context.Context) error {
		attempts++
		if attempts > 1 {
			delays = append(delays, time.Since(lastTime))
		}
		lastTime = time.Now()
		return errors.New("error")
	}

	retryer.Do(context.Background(), fn)

	// Проверяем что задержки постоянные
	for _, delay := range delays {
		if delay < 45*time.Millisecond || delay > 55*time.Millisecond {
			t.Errorf("Expected constant delay ~50ms, got %v", delay)
		}
	}
}

func TestRetryer_ContextCancellation(t *testing.T) {
	config := EnableRetry(10, 100*time.Millisecond)
	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		if attempts == 2 {
			cancel() // Cancel после второй попытки
		}
		return errors.New("error")
	}

	err = retryer.Do(ctx, fn)
	if err == nil {
		t.Error("Expected context cancellation error")
	}

	// Должно быть 2-3 попытки (вторая провалилась и cancel, возможно третья началась)
	if attempts > 3 {
		t.Errorf("Expected max 3 attempts with context cancellation, got %d", attempts)
	}
}

func TestRetryer_OnRetryCallback(t *testing.T) {
	callbackCalls := 0
	config := EnableRetry(3, 10*time.Millisecond)
	config.OnRetry = func(attempt int, err error, delay time.Duration) {
		callbackCalls++
	}

	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	}

	retryer.Do(context.Background(), fn)

	// OnRetry вызывается перед каждым retry (не перед первой попыткой)
	// 3 попытки = 2 retry = 2 callback calls
	expectedCallbacks := 2
	if callbackCalls != expectedCallbacks {
		t.Errorf("Expected %d callback calls, got %d", expectedCallbacks, callbackCalls)
	}
}

func TestRetryer_WithDLQ(t *testing.T) {
	dlqFile := "test_dlq.json"
	defer os.Remove(dlqFile)

	config := EnableRetryWithDLQ(2, 10*time.Millisecond, dlqFile)
	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}
	defer retryer.Close()

	fn := func(ctx context.Context) error {
		return errors.New("persistent error")
	}

	// Выполняем с данными
	testData := map[string]string{"order_id": "12345"}
	err = retryer.DoWithData(context.Background(), fn, testData)
	if err == nil {
		t.Error("Expected error")
	}

	// Проверяем что запись добавлена в DLQ
	dlq := retryer.GetDLQ()
	if dlq == nil {
		t.Fatal("DLQ should not be nil")
	}

	entries := dlq.Get()
	if len(entries) != 1 {
		t.Errorf("Expected 1 DLQ entry, got %d", len(entries))
	}

	if entries[0].Data == nil {
		t.Error("Expected data in DLQ entry")
	}
}

func TestRetryer_RetryableErrors(t *testing.T) {
	config := EnableRetry(3, 10*time.Millisecond)
	config.RetryableErrors = []string{"timeout", "connection refused"}

	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	// Retryable error
	attempts := 0
	fn1 := func(ctx context.Context) error {
		attempts++
		return errors.New("connection refused")
	}

	retryer.Do(context.Background(), fn1)
	if attempts != 3 {
		t.Errorf("Expected 3 retries for retryable error, got %d", attempts)
	}

	// Non-retryable error
	attempts = 0
	fn2 := func(ctx context.Context) error {
		attempts++
		return errors.New("invalid input")
	}

	retryer.Do(context.Background(), fn2)
	if attempts != 1 {
		t.Errorf("Expected 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestRetryer_Disabled(t *testing.T) {
	config := DefaultConfig() // disabled by default
	retryer, err := NewRetryer(config)
	if err != nil {
		t.Fatalf("Failed to create retryer: %v", err)
	}

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	}

	err = retryer.Do(context.Background(), fn)
	if err == nil {
		t.Error("Expected error when retry disabled")
	}

	if attempts != 1 {
		t.Errorf("Expected 1 attempt when retry disabled, got %d", attempts)
	}
}
