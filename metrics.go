package main

import "github.com/prometheus/client_golang/prometheus"

var (
	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "status_codes_total",
			Help: "Distribution by status codes",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node", "code"},
	)
	requestSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_sum_total",
			Help: "Total number of sent requests",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	requestSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_success_total",
			Help: "Total number of sent success requests",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	requestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "request_duration_seconds",
			Help:       "Request duration",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	limitExcess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "concurrent_limit_excess_total",
			Help: "Total number of max_concurrent_queries excess",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	hostPenalties = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "host_penalties_total",
			Help: "Total number of given penalties by host",
		},
		[]string{"cluster", "cluster_node"},
	)
	hostHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "host_health",
			Help: "Health state of hosts by clusters",
		},
		[]string{"cluster", "cluster_node"},
	)
	concurrentQueries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "concurrent_queries",
			Help: "The number of concurrent queries at current time",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	requestQueueSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "request_queue_size",
			Help: "Request queue sizes at the current time",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	userQueueOverflow = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_queue_overflow_total",
			Help: "The number of overflows for per-user request queues",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	clusterUserQueueOverflow = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cluster_user_queue_overflow_total",
			Help: "The number of overflows for per-cluster_user request queues",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	requestBodyBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_body_bytes_total",
			Help: "The amount of bytes read from request bodies",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	responseBodyBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "response_body_bytes_total",
			Help: "The amount of bytes written to response bodies",
		},
		[]string{"user", "cluster", "cluster_user", "cluster_node"},
	)
	badRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bad_requests_total",
		Help: "Total number of unsupported requests",
	})
	cacheHit = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "The amount of successful cache hits",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	cacheMiss = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_miss_total",
			Help: "The amount of cache miss",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	cacheSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_size",
			Help: "Cache size at the current time",
		},
		[]string{"cache"},
	)
	cacheItems = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_items",
			Help: "Cache items at the current time",
		},
		[]string{"cache"},
	)
	cachedResponseDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "cached_response_duration_seconds",
			Help:       "Response duration served from the cache",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"user", "cluster", "cluster_user"},
	)
)

func init() {
	prometheus.MustRegister(statusCodes, requestDuration, requestSum, requestSuccess,
		limitExcess, hostPenalties, hostHealth, concurrentQueries,
		requestQueueSize, userQueueOverflow, clusterUserQueueOverflow,
		requestBodyBytes, responseBodyBytes, badRequest,
		cacheHit, cacheMiss, cacheSize, cacheItems, cachedResponseDuration)
}
