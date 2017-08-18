package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	statusCodes    *prometheus.CounterVec
	timeouts       *prometheus.CounterVec
	errors         *prometheus.CounterVec
	requestSum     *prometheus.CounterVec
	requestSuccess *prometheus.CounterVec
)

func init() {
	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "status_codes",
			Help: "Distribution by status codes counter",
		},
		[]string{"target", "code"},
	)

	timeouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_timeouts",
			Help: "Number of timeouts",
		},
		[]string{"user", "target"},
	)

	errors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_errors",
			Help: "Number of errors returned by target. Including amount of timeouts",
		},
		[]string{"target", "message"},
	)

	requestSum = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_sum",
			Help: "Total number of sent requests",
		},
		[]string{"user", "target"},
	)

	requestSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_success",
			Help: "Total number of sent success requests",
		},
		[]string{"user", "target"},
	)

	prometheus.MustRegister(statusCodes, timeouts, errors,
		requestSum, requestSuccess)
}
