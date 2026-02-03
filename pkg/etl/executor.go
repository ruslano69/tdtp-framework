package etl

import (
	"context"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// ExecutionResult представляет результат выполнения SQL трансформации
type ExecutionResult struct {
	SQL          string
	RowsAffected int
	Duration     time.Duration
	Packet       *packet.DataPacket
	Error        error
}

// Executor отвечает за выполнение SQL трансформаций в workspace
type Executor struct {
	workspace *Workspace
}

// NewExecutor создает новый исполнитель SQL
func NewExecutor(workspace *Workspace) *Executor {
	return &Executor{
		workspace: workspace,
	}
}

// Execute выполняет SQL трансформацию и возвращает результат
func (e *Executor) Execute(ctx context.Context, sql string, resultTableName string) (*ExecutionResult, error) {
	if e.workspace == nil {
		return nil, fmt.Errorf("workspace is not initialized")
	}

	if sql == "" {
		return nil, fmt.Errorf("SQL query is empty")
	}

	// Засекаем время выполнения
	startTime := time.Now()

	// Выполняем SQL в workspace
	packet, err := e.workspace.ExecuteSQL(ctx, sql, resultTableName)

	// Вычисляем длительность
	duration := time.Since(startTime)

	if err != nil {
		return &ExecutionResult{
			SQL:      sql,
			Duration: duration,
			Error:    err,
		}, err
	}

	// Подсчитываем количество строк
	rowsAffected := len(packet.Data.Rows)

	return &ExecutionResult{
		SQL:          sql,
		RowsAffected: rowsAffected,
		Duration:     duration,
		Packet:       packet,
	}, nil
}

// GetWorkspace возвращает workspace (для тестирования)
func (e *Executor) GetWorkspace() *Workspace {
	return e.workspace
}
