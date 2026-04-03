//go:build windows

package access

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// InspectTable is not yet implemented for Microsoft Access.
// Implements adapters.Adapter.
func (a *Adapter) InspectTable(_ context.Context, tableName string) (*adapters.TableReport, error) {
	return nil, fmt.Errorf("--inspect-table is not yet implemented for Access adapter (table: %s)", tableName)
}
