package main

import "github.com/prometheus/client_golang/prometheus"

var (
	requestSum      *prometheus.CounterVec
	statusCodes     *prometheus.CounterVec
	requestSuccess  *prometheus.CounterVec
	userTimeouts    *prometheus.CounterVec
	clusterTimeouts *prometheus.CounterVec
	limitExcess     *prometheus.CounterVec

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

	userTimeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_timeouts",
			Help: "Number of timeouts for initial user",
		},
		[]string{"user", "cluster_user", "host"},
	)

	clusterTimeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cluster_user_timeouts",
			Help: "Number of timeouts for execution user",
		},
		[]string{"user", "cluster_user", "host"},
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

	limitExcess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "limit_excess",
			Help: "Total number of limit excess",
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

	prometheus.MustRegister(statusCodes, userTimeouts, clusterTimeouts,
		requestSum, requestSuccess, limitExcess, badRequest, goodRequest)
}
