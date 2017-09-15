package main

import "github.com/prometheus/client_golang/prometheus"

var (
	requestSum            *prometheus.CounterVec
	statusCodes           *prometheus.CounterVec
	requestSuccess        *prometheus.CounterVec
	requestDuration       *prometheus.SummaryVec
	concurrentLimitExcess *prometheus.CounterVec

	goodRequest prometheus.Counter
	badRequest  prometheus.Counter
)

func init() {
	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "status_codes",
			Help: "Distribution by status codes counter",
		},
		[]string{"user", "cluster_user", "host", "code"},
	)

	requestSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_sum",
			Help: "Total number of sent requests",
		},
		[]string{"user", "cluster_user", "host"},
	)

	requestSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_success",
			Help: "Total number of sent success requests",
		},
		[]string{"user", "cluster_user", "host"},
	)

	requestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "request_duration",
			Help: "Shows request duration",
		},
		[]string{"user", "cluster_user", "host"},
	)

	concurrentLimitExcess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "concurrent_limit_excess",
			Help: "Total number of max_concurrent_queries excess",
		},
		[]string{"user", "cluster_user", "host"},
	)

	goodRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "good_request",
		Help: "Total number of proxy requests",
	})

	badRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bad_request",
		Help: "Total number of unsupported requests",
	})

	prometheus.MustRegister(statusCodes, requestDuration, requestSum, requestSuccess,
		concurrentLimitExcess, badRequest, goodRequest)
}
