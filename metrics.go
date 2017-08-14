package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	connOpen       *prometheus.GaugeVec
	statusCodes    *prometheus.CounterVec
	errorMessages  *prometheus.CounterVec
	timeouts       *prometheus.CounterVec
	errors         *prometheus.CounterVec
	requestSum     *prometheus.CounterVec
	requestSuccess *prometheus.CounterVec
)

func init() {
	connOpen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "conn_open",
			Help: "Number of open connections",
		},
		[]string{"user", "target"},
	)

	statusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "status_codes",
			Help: "Distribution by status codes counter",
		},
		[]string{"user", "target", "code"},
	)

	errorMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors",
			Help: "Distribution by error messages",
		},
		[]string{"user", "target", "message"},
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
		[]string{"user", "target"},
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

	prometheus.MustRegister(connOpen, statusCodes, errorMessages, timeouts, errors,
		requestSum, requestSuccess)
}
