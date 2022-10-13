package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type mockResponseWriterWithCode struct {
	http.ResponseWriter
	statusCode int
}

// TestRunQueryFail function statResponseWriter's statusCode will not be 200, but the query has been proxied
// The request will be retried 1 time with a different host in the same replica
func TestRunQueryFail(t *testing.T) {
	req := newRequest("http://localhost:8080")

	hs := []string{"localhost:8080", "localhost:8081"}

	s := newMockScope(hs)

	srw := mockStatResponseWriter(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	proxiedResponseDuration := mockProxiedResponseDuration()

	err := executeWithRetry(
		context.Background(),
		s,
		time.Now(),
		retryNum,
		mockReverseProxy,
		mrw,
		srw,
		req,
		proxiedResponseDuration,
	)

	if srw.statusCode == 200 {
		t.Errorf("the retry should be failed: %v", err)
	}

	if err != nil {
		t.Errorf("the query should be proxied")
	}
}

// TestRunQuerySuccessOnce function statResponseWriter's statusCode will be StatusOK after executeWithRetry, the query has been proxied
// The execution will succeeded without retry
func TestRunQuerySuccessOnce(t *testing.T) {
	req := newRequest("http://localhost:8090")

	hs := []string{"localhost:8080", "localhost:8090"}

	s := newMockScope(hs)

	srw := mockStatResponseWriter(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	proxiedResponseDuration := mockProxiedResponseDuration()

	err := executeWithRetry(
		context.Background(),
		s,
		time.Now(),
		retryNum,
		mockReverseProxy,
		mrw,
		srw,
		req,
		proxiedResponseDuration,
	)
	if srw.statusCode != 200 {
		t.Errorf("the retry is failed: %v", err)
	}
}

// TestRunQuerySuccess function statResponseWriter's statusCode will be StatusOK after executeWithRetry, the query has been proxied
// The execution will succeeded after retry
func TestRunQuerySuccess(t *testing.T) {
	req := newRequest("http://localhost:8080")

	hs := []string{"localhost:8080", "localhost:8090"}

	s := newMockScope(hs)

	srw := mockStatResponseWriter(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	proxiedResponseDuration := mockProxiedResponseDuration()

	err := executeWithRetry(
		context.Background(),
		s,
		time.Now(),
		retryNum,
		mockReverseProxy,
		mrw,
		srw,
		req,
		proxiedResponseDuration,
	)
	if srw.statusCode != 200 {
		t.Errorf("the retry is failed: %v", err)
	}
}

func mockReverseProxy(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Host != "localhost:8090" {
		rw.WriteHeader(http.StatusBadGateway)
	} else {
		rw.WriteHeader(http.StatusOK)
	}
}

func newRequest(host string) *http.Request {
	// create a new req
	req := httptest.NewRequest(http.MethodGet, host, nil)

	ctx := context.Background()

	req = req.WithContext(ctx)

	return req
}

func newHostsCluster(hs []string) ([]*host, *cluster) {
	// set up cluster, replicas, hosts
	cluster1 := &cluster{
		name: "cluster1",
	}

	var urls []*url.URL

	var replicas []*replica

	var hosts []*host

	for i := 0; i < len(hs); i++ {
		urli := &url.URL{
			Scheme: "http",
			Host:   hs[i],
		}
		replicai := &replica{
			cluster:     cluster1,
			name:        fmt.Sprintf("replica%d", i+1),
			nextHostIdx: 0,
		}
		urls = append(urls, urli)
		replicas = append(replicas, replicai)
	}

	cluster1.replicas = replicas

	for i := 0; i < len(hs); i++ {
		hosti := &host{
			replica: replicas[i],
			penalty: 1000,
			active:  1,
			addr:    urls[i],
		}
		hosts = append(hosts, hosti)
	}

	replicas[0].hosts = hosts

	return hosts, cluster1
}

func newMockScope(hs []string) *scope {
	hosts, c := newHostsCluster(hs)

	return &scope{
		startTime: time.Now(),
		host:      hosts[0],
		cluster:   c,
		labels: prometheus.Labels{
			"user":         "default",
			"cluster":      "default",
			"cluster_user": "default",
			"replica":      "default",
			"cluster_node": "default",
		},
	}
}

func mockProxiedResponseDuration() *prometheus.SummaryVec {
	proxiedResponseDuration := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  "mockNamespace",
			Name:       "proxied_response_duration_seconds",
			Help:       "Response duration proxied from clickhouse",
			Objectives: map[float64]float64{0.5: 1e-1, 0.9: 1e-2, 0.99: 1e-3, 0.999: 1e-4, 1: 1e-5},
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)

	return proxiedResponseDuration
}

func mockStatResponseWriter(s *scope) *statResponseWriter {
	responseBodyBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mockNamespace",
			Name:      "mockName",
			Help:      "mockHelp",
		},
		[]string{"user", "cluster", "cluster_user", "replica", "cluster_node"},
	)
	return &statResponseWriter{
		ResponseWriter: httptest.NewRecorder(),
		bytesWritten:   responseBodyBytes.With(s.labels),
	}
}

func (m *mockResponseWriterWithCode) StatusCode() int {
	return m.statusCode
}

func (m *mockResponseWriterWithCode) Header() http.Header {
	return m.ResponseWriter.Header()
}

func (m *mockResponseWriterWithCode) Write(i []byte) (int, error) {
	return m.ResponseWriter.Write(i)
}

func (m *mockResponseWriterWithCode) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}
