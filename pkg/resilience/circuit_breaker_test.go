package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_Success(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 3
	config.Timeout = 100 * time.Millisecond

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Успешный вызов
	err = cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed, got %v", cb.State())
	}

	counts := cb.Counts()
	if counts.TotalSuccesses != 1 {
		t.Errorf("Expected 1 success, got %d", counts.TotalSuccesses)
	}
}

func TestCircuitBreaker_Failure(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 3
	config.Timeout = 100 * time.Millisecond

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Неудачный вызов
	err = cb.Execute(context.Background(), func(ctx context.Context) error {
		return testErr
	})

	if !errors.Is(err, testErr) {
		t.Errorf("Expected test error, got %v", err)
	}

	counts := cb.Counts()
	if counts.TotalFailures != 1 {
		t.Errorf("Expected 1 failure, got %d", counts.TotalFailures)
	}
}

func TestCircuitBreaker_OpenAfterMaxFailures(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 3
	config.Timeout = 100 * time.Millisecond

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Выполняем 3 неудачных вызова
	for i := 0; i < 3; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Circuit должен быть открыт
	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen, got %v", cb.State())
	}

	// Следующий вызов должен вернуть ErrCircuitOpen
	err = cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 2
	config.Timeout = 50 * time.Millisecond

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Открываем circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen, got %v", cb.State())
	}

	// Ждем timeout
	time.Sleep(60 * time.Millisecond)

	// Делаем успешный вызов - это должно перевести в Half-Open
	successCount := 0
	err = cb.Execute(context.Background(), func(ctx context.Context) error {
		successCount++
		return nil
	})

	// Вызов должен быть успешным (переход в Half-Open произошел)
	if err != nil {
		t.Errorf("Expected successful call after timeout, got error: %v", err)
	}

	if successCount != 1 {
		t.Error("Expected function to be called")
	}

	// Теперь должны быть в Half-Open
	if cb.State() != StateHalfOpen {
		t.Errorf("Expected StateHalfOpen after timeout, got %v", cb.State())
	}
}

func TestCircuitBreaker_ClosedAfterSuccessInHalfOpen(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 2
	config.Timeout = 50 * time.Millisecond
	config.SuccessThreshold = 2

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Открываем circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Ждем перехода в Half-Open
	time.Sleep(60 * time.Millisecond)

	// Выполняем 2 успешных вызова
	for i := 0; i < 2; i++ {
		err = cb.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Errorf("Unexpected error in half-open: %v", err)
		}
	}

	// Circuit должен закрыться
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed after success threshold, got %v", cb.State())
	}
}

func TestCircuitBreaker_OpenAgainAfterFailureInHalfOpen(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 2
	config.Timeout = 50 * time.Millisecond

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Открываем circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Ждем timeout
	time.Sleep(60 * time.Millisecond)

	// Неудачный вызов после timeout - это переведет в Half-Open, затем обратно в Open
	callCount := 0
	cb.Execute(context.Background(), func(ctx context.Context) error {
		callCount++
		return testErr
	})

	// Функция должна была быть вызвана (переход в Half-Open произошел)
	if callCount != 1 {
		t.Error("Expected function to be called in half-open")
	}

	// Должны вернуться в Open
	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_MaxConcurrentCalls(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxConcurrentCalls = 2

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Запускаем 2 одновременных вызова
	done1 := make(chan bool)
	done2 := make(chan bool)

	go func() {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		done1 <- true
	}()

	go func() {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		done2 <- true
	}()

	// Даем время запуститься
	time.Sleep(10 * time.Millisecond)

	// Третий вызов должен вернуть ErrTooManyCalls
	err = cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if !errors.Is(err, ErrTooManyCalls) {
		t.Errorf("Expected ErrTooManyCalls, got %v", err)
	}

	// Ждем завершения
	<-done1
	<-done2
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	stateChanges := []struct {
		from State
		to   State
	}{}

	config := DefaultConfig("test")
	config.MaxFailures = 2
	config.Timeout = 50 * time.Millisecond
	config.OnStateChange = func(name string, from State, to State) {
		stateChanges = append(stateChanges, struct {
			from State
			to   State
		}{from, to})
	}

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Открываем circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Даем время для callback
	time.Sleep(10 * time.Millisecond)

	// Проверяем что был вызван callback
	if len(stateChanges) == 0 {
		t.Error("Expected state change callback to be called")
	}

	if len(stateChanges) > 0 {
		change := stateChanges[0]
		if change.from != StateClosed || change.to != StateOpen {
			t.Errorf("Expected Closed→Open, got %v→%v", change.from, change.to)
		}
	}
}

func TestCircuitBreaker_ExecuteWithFallback(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 1

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Открываем circuit
	cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})

	fallbackCalled := false

	// Вызываем с fallback
	err = cb.ExecuteWithFallback(
		context.Background(),
		func(ctx context.Context) error {
			return nil
		},
		func(ctx context.Context) error {
			fallbackCalled = true
			return nil
		},
	)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !fallbackCalled {
		t.Error("Expected fallback to be called")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 2

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Открываем circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen, got %v", cb.State())
	}

	// Сбрасываем
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed after reset, got %v", cb.State())
	}

	counts := cb.Counts()
	if counts.TotalFailures != 0 {
		t.Errorf("Expected 0 failures after reset, got %d", counts.TotalFailures)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	config := DefaultConfig("test")
	config.MaxFailures = 3

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	// Выполняем несколько вызовов
	cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})

	stats := cb.Stats()

	if stats.Counts.TotalSuccesses != 1 {
		t.Errorf("Expected 1 success in stats, got %d", stats.Counts.TotalSuccesses)
	}

	if stats.Counts.TotalFailures != 1 {
		t.Errorf("Expected 1 failure in stats, got %d", stats.Counts.TotalFailures)
	}
}

func TestCircuitBreaker_Disabled(t *testing.T) {
	config := DefaultConfig("test")
	config.Enabled = false

	cb, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create circuit breaker: %v", err)
	}

	testErr := errors.New("test error")

	// Даже при многих ошибках circuit не открывается
	for i := 0; i < 10; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	// Состояние должно оставаться Closed (т.к. circuit disabled)
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed for disabled circuit, got %v", cb.State())
	}
}

func TestCircuitBreakerGroup(t *testing.T) {
	group := NewGroup()

	// Создаем несколько circuit breakers
	config1 := DefaultConfig("db")
	cb1, _ := New(config1)
	group.Add("db", cb1)

	config2 := DefaultConfig("api")
	cb2, _ := New(config2)
	group.Add("api", cb2)

	// Получаем по имени
	cb, ok := group.Get("db")
	if !ok {
		t.Error("Expected to find 'db' circuit breaker")
	}

	if cb.Name() != "db" {
		t.Errorf("Expected name 'db', got '%s'", cb.Name())
	}

	// Выполняем через группу
	err := group.Execute(context.Background(), "api", func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
