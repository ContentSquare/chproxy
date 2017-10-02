package main

import "github.com/prometheus/client_golang/prometheus"

var (
	requestSum      *prometheus.CounterVec
	hostHealth      *prometheus.GaugeVec
	statusCodes     *prometheus.CounterVec
	requestPerMin   *prometheus.GaugeVec
	hostPenalties   *prometheus.CounterVec
	requestSuccess  *prometheus.CounterVec
	requestDuration *prometheus.SummaryVec
	limitExcess     *prometheus.CounterVec

	goodRequest prometheus.Counter
	badRequest  prometheus.Counter
)

func init() {
	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "status_codes_total",
			Help: "Distribution by status codes",
		},
		[]string{"user", "cluster_user", "host", "code"},
	)
	requestSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_sum_total",
			Help: "Total number of sent requests",
		},
		[]string{"user", "cluster_user", "host"},
	)
	requestSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_success_total",
			Help: "Total number of sent success requests",
		},
		[]string{"user", "cluster_user", "host"},
	)
	requestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "request_duration_seconds",
			Help: "Request duration",
		},
		[]string{"user", "cluster_user", "host"},
	)
	limitExcess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "concurrent_limit_excess_total",
			Help: "Total number of max_concurrent_queries excess",
		},
		[]string{"user", "cluster_user", "host"},
	)
	hostPenalties = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "host_penalties_total",
			Help: "Total number of given penalties by host",
		},
		[]string{"host"},
	)
	hostHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "host_health",
			Help: "Health state of hosts by clusters",
		},
		[]string{"cluster", "host"},
	)
	requestPerMin = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "requests_per_minute",
			Help: "Requests per minute for user",
		},
		[]string{"user", "cluster"},
	)
	goodRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "good_requests_total",
		Help: "Total number of proxy requests",
	})
	badRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bad_requests_total",
		Help: "Total number of unsupported requests",
	})
	prometheus.MustRegister(statusCodes, requestDuration, requestSum, requestSuccess,
		limitExcess, hostPenalties, hostHealth, requestPerMin, badRequest, goodRequest)
}
