package topology

// TODO this is only here to avoid recursive imports. We should have a separate package for metrics.
import (
	"github.com/contentsquare/chproxy/config"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	HostHealth    *prometheus.GaugeVec
	HostPenalties *prometheus.CounterVec
)

func initMetrics(cfg *config.Config) {
	namespace := cfg.Server.Metrics.Namespace
	HostHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "host_health",
			Help:      "Health state of hosts by clusters",
		},
		[]string{"cluster", "replica", "cluster_node"},
	)
	HostPenalties = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "host_penalties_total",
			Help:      "Total number of given penalties by host",
		},
		[]string{"cluster", "replica", "cluster_node"},
	)
}

func RegisterMetrics(cfg *config.Config) {
	initMetrics(cfg)
	prometheus.MustRegister(HostHealth, HostPenalties)
}
