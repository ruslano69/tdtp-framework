package resilience

import (
	"fmt"
	"sync"
	"time"
)

// State - состояние Circuit Breaker
type State int

const (
	// StateClosed - нормальная работа, запросы проходят
	StateClosed State = iota

	// StateHalfOpen - тестирование восстановления
	StateHalfOpen

	// StateOpen - circuit открыт, запросы отклоняются
	StateOpen
)

// String - строковое представление состояния
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// stateManager - управление состоянием Circuit Breaker
type stateManager struct {
	mu              sync.RWMutex
	state           State
	generation      uint64 // Счетчик смены поколений состояний
	counts          Counts
	expiry          time.Time // Когда истекает Open состояние
	config          Config
	runningCalls    uint32
	maxRunningCalls uint32
	lastStateChange time.Time
	stateChanges    map[State]int // Статистика изменений состояний
}

// newStateManager - создать новый state manager
func newStateManager(config Config) *stateManager {
	return &stateManager{
		state:           StateClosed,
		generation:      0,
		counts:          Counts{},
		expiry:          time.Time{},
		config:          config,
		runningCalls:    0,
		maxRunningCalls: 0,
		lastStateChange: time.Now(),
		stateChanges:    make(map[State]int),
	}
}

// getState - получить текущее состояние
func (sm *stateManager) getState() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

// setState - установить новое состояние
func (sm *stateManager) setState(newState State, reset bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.state == newState {
		return
	}

	oldState := sm.state
	sm.state = newState
	sm.generation++
	sm.lastStateChange = time.Now()
	sm.stateChanges[newState]++

	if reset {
		sm.counts = Counts{}
	}

	// Устанавливаем expiry для Open состояния
	if newState == StateOpen {
		sm.expiry = time.Now().Add(sm.config.Timeout)
	}

	// Callback при изменении состояния
	if sm.config.OnStateChange != nil {
		// Вызываем callback без удержания lock
		go sm.config.OnStateChange(sm.config.Name, oldState, newState)
	}
}

// beforeRequest - вызывается перед выполнением запроса
func (sm *stateManager) beforeRequest() (uint64, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state := sm.state
	generation := sm.generation

	// Проверяем не истек ли timeout для Open состояния
	if state == StateOpen && time.Now().After(sm.expiry) {
		// Переходим в Half-Open
		sm.state = StateHalfOpen
		sm.generation++
		sm.counts = Counts{}
		sm.lastStateChange = time.Now()
		sm.stateChanges[StateHalfOpen]++

		state = StateHalfOpen
		generation = sm.generation

		// Callback при изменении состояния
		if sm.config.OnStateChange != nil {
			oldState := StateOpen
			newState := StateHalfOpen
			go sm.config.OnStateChange(sm.config.Name, oldState, newState)
		}
	}

	// В Open состоянии отклоняем запросы
	if state == StateOpen {
		return generation, ErrCircuitOpen
	}

	// Проверяем лимит одновременных вызовов
	if sm.config.MaxConcurrentCalls > 0 && sm.runningCalls >= sm.config.MaxConcurrentCalls {
		return generation, ErrTooManyCalls
	}

	sm.runningCalls++
	if sm.runningCalls > sm.maxRunningCalls {
		sm.maxRunningCalls = sm.runningCalls
	}

	return generation, nil
}

// afterRequest - вызывается после выполнения запроса
func (sm *stateManager) afterRequest(generation uint64, success bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Уменьшаем счетчик одновременных вызовов
	if sm.runningCalls > 0 {
		sm.runningCalls--
	}

	// Проверяем поколение
	if generation != sm.generation {
		// Состояние изменилось, игнорируем этот результат
		return
	}

	// Обновляем счетчики
	if success {
		sm.onSuccess()
	} else {
		sm.onFailure()
	}
}

// onSuccess - обработка успешного запроса
func (sm *stateManager) onSuccess() {
	sm.counts.Requests++
	sm.counts.TotalSuccesses++
	sm.counts.ConsecutiveSuccesses++
	sm.counts.ConsecutiveFailures = 0

	switch sm.state {
	case StateClosed:
		// Ничего не делаем, все хорошо

	case StateHalfOpen:
		// Проверяем достигли ли порога для закрытия
		if sm.counts.ConsecutiveSuccesses >= sm.config.SuccessThreshold {
			// Изменяем состояние напрямую, lock уже взят
			oldState := sm.state
			sm.state = StateClosed
			sm.generation++
			sm.lastStateChange = time.Now()
			sm.stateChanges[StateClosed]++
			sm.counts = Counts{}

			// Callback без удержания lock
			if sm.config.OnStateChange != nil {
				go sm.config.OnStateChange(sm.config.Name, oldState, StateClosed)
			}
		}
	}
}

// onFailure - обработка неудачного запроса
func (sm *stateManager) onFailure() {
	sm.counts.Requests++
	sm.counts.TotalFailures++
	sm.counts.ConsecutiveFailures++
	sm.counts.ConsecutiveSuccesses = 0

	var needStateChange bool
	var newState State

	switch sm.state {
	case StateClosed:
		// Проверяем нужно ли открыть circuit
		if sm.shouldTrip() {
			needStateChange = true
			newState = StateOpen
		}

	case StateHalfOpen:
		// При любой ошибке в Half-Open возвращаемся в Open
		needStateChange = true
		newState = StateOpen
	}

	if needStateChange {
		// Изменяем состояние напрямую, lock уже взят
		oldState := sm.state
		sm.state = newState
		sm.generation++
		sm.lastStateChange = time.Now()
		sm.stateChanges[newState]++
		sm.counts = Counts{}

		if newState == StateOpen {
			sm.expiry = time.Now().Add(sm.config.Timeout)
		}

		// Callback без удержания lock
		if sm.config.OnStateChange != nil {
			go sm.config.OnStateChange(sm.config.Name, oldState, newState)
		}
	}
}

// shouldTrip - проверка нужно ли открыть circuit
func (sm *stateManager) shouldTrip() bool {
	// Используем custom функцию если указана
	if sm.config.ShouldTrip != nil {
		return sm.config.ShouldTrip(sm.counts)
	}

	// Стандартная логика: количество последовательных ошибок
	return sm.counts.ConsecutiveFailures >= sm.config.MaxFailures
}

// getCounts - получить счетчики
func (sm *stateManager) getCounts() Counts {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.counts
}

// getStats - получить статистику
func (sm *stateManager) getStats() Stats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return Stats{
		State:             sm.state,
		Generation:        sm.generation,
		Counts:            sm.counts,
		RunningCalls:      sm.runningCalls,
		MaxRunningCalls:   sm.maxRunningCalls,
		LastStateChange:   sm.lastStateChange,
		StateChanges:      copyMap(sm.stateChanges),
		TimeUntilHalfOpen: sm.timeUntilHalfOpen(),
	}
}

// timeUntilHalfOpen - сколько времени до Half-Open
func (sm *stateManager) timeUntilHalfOpen() time.Duration {
	if sm.state != StateOpen {
		return 0
	}

	remaining := time.Until(sm.expiry)
	if remaining < 0 {
		return 0
	}

	return remaining
}

// reset - сбросить состояние
func (sm *stateManager) reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.state = StateClosed
	sm.generation++
	sm.counts = Counts{}
	sm.expiry = time.Time{}
	sm.runningCalls = 0
	sm.lastStateChange = time.Now()
}

// copyMap - копирование map
func copyMap(m map[State]int) map[State]int {
	result := make(map[State]int, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// Stats - статистика Circuit Breaker
type Stats struct {
	State             State
	Generation        uint64
	Counts            Counts
	RunningCalls      uint32
	MaxRunningCalls   uint32
	LastStateChange   time.Time
	StateChanges      map[State]int
	TimeUntilHalfOpen time.Duration
}
