package processors

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// PacketProcessor обрабатывает DataPacket целиком (in-place).
// Уровень пакета: compact, compress, encrypt, hash.
// Порядок в цепочке: mask/normalize → compact → compress → encrypt → hash.
type PacketProcessor interface {
	Name() string
	ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error
}

// PacketChain последовательно выполняет цепочку PacketProcessor.
type PacketChain struct {
	procs []PacketProcessor
}

// NewPacketChain создаёт PacketChain.
func NewPacketChain(procs ...PacketProcessor) *PacketChain {
	return &PacketChain{procs: procs}
}

// Add добавляет процессор в конец цепочки.
func (c *PacketChain) Add(p PacketProcessor) {
	c.procs = append(c.procs, p)
}

// IsEmpty возвращает true если цепочка пуста.
func (c *PacketChain) IsEmpty() bool {
	return len(c.procs) == 0
}

// ProcessPacket прогоняет пакет через все процессоры цепочки последовательно.
func (c *PacketChain) ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error {
	for _, p := range c.procs {
		if err := p.ProcessPacket(ctx, pkt); err != nil {
			return fmt.Errorf("packet processor %q: %w", p.Name(), err)
		}
	}
	return nil
}
