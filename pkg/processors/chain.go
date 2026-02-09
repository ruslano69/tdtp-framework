package processors

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Chain представляет цепочку процессоров
type Chain struct {
	processors []Processor
}

// NewChain создает новую цепочку процессоров
func NewChain(processors ...Processor) *Chain {
	return &Chain{
		processors: processors,
	}
}

// Process выполняет все процессоры в цепочке последовательно
func (c *Chain) Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error) {
	if len(c.processors) == 0 {
		return data, nil
	}

	result := data
	for i, proc := range c.processors {
		var err error
		result, err = proc.Process(ctx, result, schema)
		if err != nil {
			return nil, fmt.Errorf("processor %d (%s) failed: %w", i, proc.Name(), err)
		}
	}

	return result, nil
}

// Add добавляет процессор в цепочку
func (c *Chain) Add(processor Processor) {
	c.processors = append(c.processors, processor)
}

// Len возвращает количество процессоров в цепочке
func (c *Chain) Len() int {
	return len(c.processors)
}

// IsEmpty проверяет, пуста ли цепочка
func (c *Chain) IsEmpty() bool {
	return len(c.processors) == 0
}
