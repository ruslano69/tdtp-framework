// Package quota implements an atomic hourly credit system via a Redis Lua script.
//
// Each pipeline group has an hourly credit balance. On each request, cost credits
// are atomically checked and deducted. If balance is insufficient, the request
// is rejected with ErrQuotaExceeded.
//
// Key format: "quota:{group}:{YYYYMMDDHH}"
// TTL on the key is automatically set to 3600 seconds so balances expire hourly.
package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// luaQuota atomically checks and deducts credits.
//
// KEYS[1] = quota key (e.g. "quota:tdtp-pipeline-users:2026022614")
// ARGV[1] = cost (integer, credits to deduct)
// ARGV[2] = initial_balance (integer, used when the key doesn't exist yet)
//
// Returns remaining balance (>= 0) if approved, -1 if insufficient balance.
const luaQuota = `
local balance = tonumber(redis.call('GET', KEYS[1]))
if balance == nil then
    balance = tonumber(ARGV[2])
end
local cost = tonumber(ARGV[1])
if balance < cost then
    return -1
end
local remaining = balance - cost
redis.call('SET', KEYS[1], remaining, 'EX', 3600)
return remaining
`

// Manager runs the quota Lua script against Pipeline Redis.
type Manager struct {
	rdb          *redis.Client
	defaultQuota int
	script       *redis.Script
}

// New creates a Manager.
// defaultHourly is the credit balance assigned to a group on first use each hour.
func New(rdb *redis.Client, defaultHourly int) *Manager {
	return &Manager{
		rdb:          rdb,
		defaultQuota: defaultHourly,
		script:       redis.NewScript(luaQuota),
	}
}

// Check atomically verifies and deducts cost credits for the given group.
// Returns ErrQuotaExceeded if the balance is insufficient.
// The group key is scoped to the current UTC hour.
func (m *Manager) Check(ctx context.Context, group string, cost int) error {
	now := time.Now().UTC()
	key := fmt.Sprintf("quota:%s:%d%02d%02d%02d",
		group, now.Year(), int(now.Month()), now.Day(), now.Hour())

	remaining, err := m.script.Run(ctx, m.rdb, []string{key}, cost, m.defaultQuota).Int()
	if err != nil {
		return fmt.Errorf("quota: lua script: %w", err)
	}
	if remaining < 0 {
		quotaRejectedTotal.WithLabelValues(group).Inc()
		return ErrQuotaExceeded
	}
	quotaDeductedTotal.WithLabelValues(group).Inc()
	quotaRemaining.WithLabelValues(group).Set(float64(remaining))
	return nil
}

// ErrQuotaExceeded is returned when the group has exhausted its hourly credits.
var ErrQuotaExceeded = errors.New("hourly quota exceeded for this group")
