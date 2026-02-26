package quota

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestManager(t *testing.T, defaultHourly int) *Manager {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(rdb, defaultHourly)
}

func TestCheck_FirstRequest_InitializesBalance(t *testing.T) {
	// Первый запрос инициализирует баланс из default_hourly и сразу вычитает cost
	m := newTestManager(t, 10)
	if err := m.Check(context.Background(), "tdtp-users", 1); err != nil {
		t.Errorf("Check() первый запрос должен пройти, got %v", err)
	}
}

func TestCheck_DeductsCreditsUntilExhausted(t *testing.T) {
	m := newTestManager(t, 3)
	ctx := context.Background()

	// Три успешных запроса по 1 кредиту
	for i := 0; i < 3; i++ {
		if err := m.Check(ctx, "group-a", 1); err != nil {
			t.Fatalf("Check() вызов %d завершился ошибкой: %v", i+1, err)
		}
	}

	// Четвёртый — баланс исчерпан
	if err := m.Check(ctx, "group-a", 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Check() после исчерпания = %v, want ErrQuotaExceeded", err)
	}
}

func TestCheck_CostExceedsBalance_Rejected(t *testing.T) {
	// Дорогой пайплайн (cost=10) при балансе 5 → немедленный отказ
	m := newTestManager(t, 5)
	if err := m.Check(context.Background(), "group-b", 10); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Check() cost > balance = %v, want ErrQuotaExceeded", err)
	}
}

func TestCheck_ExactBalance_Allowed(t *testing.T) {
	// cost == balance → разрешено (остаток = 0)
	m := newTestManager(t, 5)
	if err := m.Check(context.Background(), "group-c", 5); err != nil {
		t.Errorf("Check() cost == balance должен пройти: %v", err)
	}
}

func TestCheck_DifferentGroupsIsolated(t *testing.T) {
	// Квота группы A не влияет на группу B
	m := newTestManager(t, 2)
	ctx := context.Background()

	// Исчерпываем group-a полностью
	_ = m.Check(ctx, "group-a", 2)

	// group-b должна работать независимо
	if err := m.Check(ctx, "group-b", 1); err != nil {
		t.Errorf("Check() group-b после исчерпания group-a: %v", err)
	}
}

func TestCheck_ZeroDefaultHourly_AlwaysExceeded(t *testing.T) {
	// default_hourly=0 → любой cost > 0 превышает квоту
	m := newTestManager(t, 0)
	if err := m.Check(context.Background(), "group-zero", 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Check() с нулевым балансом = %v, want ErrQuotaExceeded", err)
	}
}

func TestCheck_LargeDefaultHourly(t *testing.T) {
	// Большой баланс — много запросов проходит
	m := newTestManager(t, 1000)
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		if err := m.Check(ctx, "group-large", 1); err != nil {
			t.Fatalf("Check() вызов %d завершился ошибкой при большом балансе: %v", i+1, err)
		}
	}
}
