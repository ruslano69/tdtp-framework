package quota

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// quotaRemaining tracks the current hourly credit balance per group after each deduction.
	quotaRemaining = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "xzmercury_quota_remaining",
			Help: "Remaining hourly quota credits per group (updated on each deduction)",
		},
		[]string{"group"},
	)

	// quotaDeductedTotal counts successful quota deductions (bind approved).
	quotaDeductedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xzmercury_quota_deducted_total",
			Help: "Total number of successful quota deductions",
		},
		[]string{"group"},
	)

	// quotaRejectedTotal counts quota rejections due to exhaustion.
	quotaRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xzmercury_quota_rejected_total",
			Help: "Total number of quota rejections due to exhausted hourly balance",
		},
		[]string{"group"},
	)
)
