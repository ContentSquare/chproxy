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

type mockResponseWriter struct {
	http.ResponseWriter
	statusCode int
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

func (m *mockResponseWriter) StatusCode() int {
	return m.statusCode
}

func (m *mockResponseWriter) Header() http.Header {
	return m.ResponseWriter.Header()
}

func (m *mockResponseWriter) Write(i []byte) (int, error) {
	return m.ResponseWriter.Write(i)
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
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

func TestRunQueryFail(t *testing.T) {
	// run request fail because it cannot establish the connection with the host
	// the request will be retried 1 time in the same replica with a different host

	req := newRequest("http://localhost:8080")

	hs := []string{"localhost:8080", "localhost:8081"}

	s := newMockScope(hs)

	srw := mockStatResponseWriter(s)

	mrw := &mockResponseWriter{
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

	if err == nil {
		t.Errorf("the retry should be failed: %v", err)
	}
}

func TestRunQuerySuccessOnce(t *testing.T) {
	// run request succeeded without retry
	// the establishment with the host succeeded at the first time

	req := newRequest("http://localhost:8090")

	hs := []string{"localhost:8080", "localhost:8090"}

	s := newMockScope(hs)

	srw := mockStatResponseWriter(s)

	mrw := &mockResponseWriter{
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
	if err != nil {
		t.Errorf("the retry is failed: %v", err)
	}
}

func TestRunQuerySuccess(t *testing.T) {
	// run request succeeded because it can establish the connection with the host
	// the request wiexecuteWithRetryretried 1 time in the same replica with a different host

	req := newRequest("http://localhost:8080")

	hs := []string{"localhost:8080", "localhost:8090"}

	s := newMockScope(hs)

	srw := mockStatResponseWriter(s)

	mrw := &mockResponseWriter{
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
	if err != nil {
		t.Errorf("the retry is failed: %v", err)
	}
}

func mockReverseProxy(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Host != "localhost:8090" {
		fmt.Println("unvalid host")
		rw.WriteHeader(http.StatusBadGateway)
	} else {
		fmt.Println("valid host")
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
