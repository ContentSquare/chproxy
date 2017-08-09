package main


import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
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

func initMetrics() {
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

var m = &dto.Metric{}

/*// Errors returns value of errors-metric
func (*Client) Errors() uint64 {
	errors.Write(m)
	return uint64(*m.Counter.Value)
}

// Timeouts returns value of timeouts-metric
func (*Client) Timeouts() uint64 {
	timeouts.Write(m)
	return uint64(*m.Counter.Value)
}

// RequestSum returns value of requestSum-metric
func (*Client) RequestSum() uint64 {
	requestSum.Write(m)
	return uint64(*m.Counter.Value)
}

// RequestSuccess returns value of requestSuccess-metric
func (*Client) RequestSuccess() uint64 {
	requestSuccess.Write(m)
	return uint64(*m.Counter.Value)
}

// BytesWritten returns value of bytesWritten-metric
func (*Client) BytesWritten() uint64 {
	bytesWritten.Write(m)
	return uint64(*m.Counter.Value)
}

// BytesRead returns value of bytesRead-metric
func (*Client) BytesRead() uint64 {
	bytesRead.Write(m)
	return uint64(*m.Counter.Value)
}

// ConnOpen returns value of connOpen-metric
func (*Client) ConnOpen() uint64 {
	connOpen.Write(m)
	return uint64(*m.Gauge.Value)
}

// RequestDuration returns map quantile:value for requestDuration-metric
func (*Client) RequestDuration() map[float64]float64 {
	requestDuration.Write(m)
	result := make(map[float64]float64, len(m.Summary.Quantile))
	for _, v := range m.Summary.Quantile {
		result[*v.Quantile] = *v.Value
	}

	return result
}*/
