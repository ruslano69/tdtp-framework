//go:build windows

package access

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ImportPacket is not implemented for Access (read-only source).
func (a *Adapter) ImportPacket(_ context.Context, _ *packet.DataPacket, _ adapters.ImportStrategy) error {
	return fmt.Errorf("access: import not supported")
}

// ImportPackets is not implemented for Access (read-only source).
func (a *Adapter) ImportPackets(_ context.Context, _ []*packet.DataPacket, _ adapters.ImportStrategy) error {
	return fmt.Errorf("access: import not supported")
}
