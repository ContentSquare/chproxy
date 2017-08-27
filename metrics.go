package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	statusCodes       *prometheus.CounterVec
	initialTimeouts   *prometheus.CounterVec
	executionTimeouts *prometheus.CounterVec
	errors            *prometheus.CounterVec
	requestSum        *prometheus.CounterVec
	requestSuccess    *prometheus.CounterVec
)

func init() {
	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "status_codes",
			Help: "Distribution by status codes counter",
		},
		[]string{"host", "code"},
	)

	initialTimeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_timeouts",
			Help: "Number of timeouts",
		},
		[]string{"initial_user", "host"},
	)

	executionTimeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_timeouts",
			Help: "Number of timeouts",
		},
		[]string{"execution_user", "host"},
	)

	errors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_errors",
			Help: "Number of errors returned by target. Including amount of timeouts",
		},
		[]string{"host", "message"},
	)

	requestSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_sum",
			Help: "Total number of sent requests",
		},
		[]string{"initial_user", "execution_user", "host"},
	)

	requestSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_success",
			Help: "Total number of sent success requests",
		},
		[]string{"initial_user", "execution_user", "host"},
	)

	prometheus.MustRegister(statusCodes, initialTimeouts, executionTimeouts, errors,
		requestSum, requestSuccess)
}
