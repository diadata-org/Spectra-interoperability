package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	ArchIntentUpdates = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dia_arch_intent_updates_total",
			Help: "Per-symbol count of DIA_ORACLE.INTENT_UPDATE events emitted by the receiver.",
		},
		[]string{"router", "chain_id", "symbol"},
	)
	ArchIntentStale = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dia_arch_intent_stale_total",
			Help: "Per-symbol count of DIA_ORACLE.INTENT_STALE events (stored value newer than incoming intent).",
		},
		[]string{"router", "chain_id", "symbol"},
	)
	ArchIntentRejected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dia_arch_intent_rejected_total",
			Help: "Per-reason count of DIA_ORACLE.INTENT_REJECTED events (UnauthorizedSigner/AlreadyProcessed/InvalidSignature).",
		},
		[]string{"router", "chain_id", "reason"},
	)
	ArchTxConfirmationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dia_arch_tx_confirmation_seconds",
			Help:    "Time from SendTransaction to processed status.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"router", "chain_id", "outcome"},
	)
	ArchFeeVaultLamports = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dia_arch_fee_vault_lamports",
			Help: "Current lamport balance of the fee-hook vault PDA.",
		},
		[]string{"router", "chain_id"},
	)
	ArchPayerBalanceLamports = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dia_arch_payer_balance_lamports",
			Help: "Current lamport balance of the relayer payer account.",
		},
		[]string{"router", "chain_id"},
	)
)

// RegisterArchMetrics registers all six collectors against reg. Call once at
// startup. Idempotent: re-registration after the first call is a no-op.
func RegisterArchMetrics(reg prometheus.Registerer) {
	for _, c := range []prometheus.Collector{
		ArchIntentUpdates, ArchIntentStale, ArchIntentRejected,
		ArchTxConfirmationSeconds, ArchFeeVaultLamports, ArchPayerBalanceLamports,
	} {
		// MustRegister panics on duplicate; use Register and swallow AlreadyRegisteredError.
		if err := reg.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}
