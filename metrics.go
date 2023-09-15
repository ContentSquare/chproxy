package main

import (
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/internal/topology"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	statusCodes                    *prometheus.CounterVec
	requestSum                     *prometheus.CounterVec
	requestSuccess                 *prometheus.CounterVec
	limitExcess                    *prometheus.CounterVec
	concurrentQueries              *prometheus.GaugeVec
	requestQueueSize               *prometheus.GaugeVec
	userQueueOverflow              *prometheus.CounterVec
	clusterUserQueueOverflow       *prometheus.CounterVec
	requestBodyBytes               *prometheus.CounterVec
	responseBodyBytes              *prometheus.CounterVec
	cacheFailedInsert              *prometheus.CounterVec
	cacheCorruptedFetch            *prometheus.CounterVec
	cacheHit                       *prometheus.CounterVec
	cacheMiss                      *prometheus.CounterVec
	cacheSize                      *prometheus.GaugeVec
	cacheItems                     *prometheus.GaugeVec
	cacheSkipped                   *prometheus.CounterVec
	requestDuration                *prometheus.SummaryVec
	proxiedResponseDuration        *prometheus.SummaryVec
	cachedResponseDuration         *prometheus.SummaryVec
	canceledRequest                *prometheus.CounterVec
	cacheHitFromConcurrentQueries  *prometheus.CounterVec
	cacheMissFromConcurrentQueries *prometheus.CounterVec
	killedRequests                 *prometheus.CounterVec
	timeoutRequest                 *prometheus.CounterVec
	configSuccess                  prometheus.Gauge
	configSuccessTime              prometheus.Gauge
	badRequest                     prometheus.Counter
	retryRequest                   *prometheus.CounterVec
)

func initMetrics(cfg *config.Config) {
	namespace := cfg.Server.Metrics.Namespace
	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "status_codes_total",
			Help:      "Distribution by status codes",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node", "code"},
	)
	requestSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "request_sum_total",
			Help:      "Total number of sent requests",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	requestSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "request_success_total",
			Help:      "Total number of sent success requests",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	limitExcess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "concurrent_limit_excess_total",
			Help:      "Total number of max_concurrent_queries excess",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	concurrentQueries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "concurrent_queries",
			Help:      "The number of concurrent queries at current time",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	requestQueueSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "request_queue_size",
			Help:      "Request queue sizes at the current time",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	userQueueOverflow = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "user_queue_overflow_total",
			Help:      "The number of overflows for per-user request queues",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	clusterUserQueueOverflow = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cluster_user_queue_overflow_total",
			Help:      "The number of overflows for per-cluster_user request queues",
		},
		[]string{"user", "cluster", "cluster_user"},
	)
	requestBodyBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "request_body_bytes_total",
			Help:      "The amount of bytes read from request bodies",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	responseBodyBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "response_body_bytes_total",
			Help:      "The amount of bytes written to response bodies",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	cacheFailedInsert = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_insertion_failures_total",
			Help:      "The number of insertion in the cache that didn't work out",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	cacheCorruptedFetch = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_get_corrutpion_total",
			Help:      "The number of time a data fetching from redis was corrupted",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	cacheHit = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hits_total",
			Help:      "The amount of cache hits",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	cacheMiss = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_miss_total",
			Help:      "The amount of cache misses",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	cacheSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cache_size",
			Help:      "Cache size at the current time",
		},
		[]string{"cache"},
	)
	cacheItems = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cache_items",
			Help:      "Cache items at the current time",
		},
		[]string{"cache"},
	)
	cacheSkipped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_payloadsize_too_big_total",
			Help:      "The amount of too big payloads to be cached",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	requestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "request_duration_seconds",
			Help:       "Request duration. Includes possible wait time in the queue",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	proxiedResponseDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "proxied_response_duration_seconds",
			Help:       "Response duration proxied from clickhouse",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	cachedResponseDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "cached_response_duration_seconds",
			Help:       "Response duration served from the cache",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	canceledRequest = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "canceled_request_total",
			Help:      "The number of requests canceled by remote client",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	cacheHitFromConcurrentQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hit_concurrent_query_total",
			Help:      "The amount of cache hits after having awaited concurrently executed queries",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)

	cacheMissFromConcurrentQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_miss_concurrent_query_total",
			Help:      "The amount of cache misses, even if previously reported as queries available in the cache, after having awaited concurrently executed queries",
		},
		[]string{"cache", "user", "cluster", "cluster_user"},
	)
	killedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "killed_request_total",
			Help:      "The number of requests killed by proxy",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	timeoutRequest = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "timeout_request_total",
			Help:      "The number of timed out requests",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	configSuccess = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "config_last_reload_successful",
		Help:      "Whether the last configuration reload attempt was successful.",
	})
	configSuccessTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "config_last_reload_success_timestamp_seconds",
		Help:      "Timestamp of the last successful configuration reload.",
	})
	badRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "bad_requests_total",
		Help:      "Total number of unsupported requests",
	})
	retryRequest = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "retry_request_total",
			Help:      "The number of retry requests",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
}

func registerMetrics(cfg *config.Config) {
	topology.RegisterMetrics(cfg)

	initMetrics(cfg)
	prometheus.MustRegister(statusCodes, requestSum, requestSuccess,
		limitExcess, concurrentQueries,
		requestQueueSize, userQueueOverflow, clusterUserQueueOverflow,
		requestBodyBytes, responseBodyBytes, cacheFailedInsert, cacheCorruptedFetch,
		cacheHit, cacheMiss, cacheSize, cacheItems, cacheSkipped,
		requestDuration, proxiedResponseDuration, cachedResponseDuration,
		canceledRequest, timeoutRequest,
		configSuccess, configSuccessTime, badRequest, retryRequest)
}
