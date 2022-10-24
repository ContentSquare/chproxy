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
	"github.com/stretchr/testify/assert"
)

type mockResponseWriterWithCode struct {
	http.ResponseWriter
	statusCode int
}

type mockStatResponseWriter struct {
	http.ResponseWriter
	http.CloseNotifier
	statusCode int
}

type mockHosts struct {
	hs  []string
	hst []string
}

// TestQueryWithRetryFail function statResponseWriter's statusCode will not be 200, but the query has been proxied
// The request will be retried 1 time with a different host in the same replica
func TestQueryWithRetryFail(t *testing.T) {
	req := newRequest("http://localhost:8080")

	mhs := &mockHosts{
		hs: []string{"localhost:8080", "localhost:8081"},
	}

	s := newMockScope(mhs.hs)

	srw := mockStatRW(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	_, err := executeWithRetry(
		context.Background(),
		s,
		retryNum,
		mhs.mockReverseProxy,
		mrw,
		srw,
		req,
		func(f float64) {},
		func() {},
	)

	if err != nil {
		t.Errorf("the execution with retry failed: %v", err)
	}
	assert.Equal(t, srw.statusCode, http.StatusBadGateway)
	assert.Equal(t, mhs.hs, mhs.hst)

}

// TestRunQuerySuccessOnce function statResponseWriter's statusCode will be StatusOK after executeWithRetry, the query has been proxied
// The execution will succeeded without retry
func TestQuerySuccessOnce(t *testing.T) {
	req := newRequest("http://localhost:8090")

	mhs := &mockHosts{
		hs: []string{"localhost:8080", "localhost:8090"},
	}

	s := newMockScope(mhs.hs)

	srw := mockStatRW(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	_, err := executeWithRetry(
		context.Background(),
		s,
		retryNum,
		mhs.mockReverseProxy,
		mrw,
		srw,
		req,
		func(f float64) {},
		func() {},
	)
	if err != nil {
		t.Errorf("The execution with retry failed, %v", err)
	}
	assert.Equal(t, mhs.hst, []string{mhs.hs[1]})
	assert.Equal(t, srw.statusCode, 200)
}

// TestQueryWithRetrySuccess function statResponseWriter's statusCode will be StatusOK after executeWithRetry, the query has been proxied
// The execution will succeeded after retry
func TestQueryWithRetrySuccess(t *testing.T) {
	req := newRequest("http://localhost:8080")

	mhs := &mockHosts{
		hs: []string{"localhost:8080", "localhost:8090"},
	}

	s := newMockScope(mhs.hs)

	srw := mockStatRW(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	_, err := executeWithRetry(
		context.Background(),
		s,
		retryNum,
		mhs.mockReverseProxy,
		mrw,
		srw,
		req,
		func(f float64) {},
		func() {},
	)
	if err != nil {
		t.Errorf("The execution with retry failed, %v", err)
	}
	assert.Equal(t, srw.statusCode, 200)
	assert.Equal(t, mhs.hs, mhs.hst)
}

func (mhs *mockHosts) mockReverseProxy(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Host != "localhost:8090" {
		rw.WriteHeader(http.StatusBadGateway)
	} else {
		rw.WriteHeader(http.StatusOK)
	}
	mhs.hst = append(mhs.hst, req.URL.Host)
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

func mockStatRW(s *scope) *mockStatResponseWriter {

	return &mockStatResponseWriter{
		ResponseWriter: httptest.NewRecorder(),
		CloseNotifier:  &testCloseNotifier{},
		statusCode:     0,
	}
}

func (srw *mockStatResponseWriter) StatusCode() int {
	return srw.statusCode
}

func (srw *mockStatResponseWriter) SetStatusCode(code int) {
	srw.statusCode = code
}

func (srw *mockStatResponseWriter) Header() http.Header {
	return srw.ResponseWriter.Header()
}

func (srw *mockStatResponseWriter) Write(i []byte) (int, error) {
	return srw.ResponseWriter.Write(i)
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
